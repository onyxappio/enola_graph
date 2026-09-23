package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// TestNamedExportCacheScansEachFileOnce pins the property SummaryScans claims to
// report. The index is reached from two directions now - the binder following a
// re-export chain, and the parse loop recording a file's own export surface - so
// a check-then-act cache lets two goroutines miss together, parse the same file
// twice and both increment the counter. The count then reports how the workers
// happened to interleave rather than how many files were scanned, which is what
// made the config-inventory comparison nondeterministic at 2 versus 3.
func TestNamedExportCacheScansEachFileOnce(t *testing.T) {
	src := []byte("export function round(){return 1}\nexport const ceil = 2\n")
	var reads int64
	var mu sync.Mutex
	readSrc := func(string) []byte {
		mu.Lock()
		reads++
		mu.Unlock()
		return src
	}
	cache := newNamedExportCache()
	known := map[string]bool{"src/origin.ts": true}

	const workers = 32
	got := make([]*namedExportIndex, workers)
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(workers)
	for i := range workers {
		go func(i int) {
			defer done.Done()
			start.Wait()
			got[i] = cache.index("src/origin.ts", readSrc, nil, known)
		}(i)
	}
	start.Done()
	done.Wait()

	if n := cache.summaryScans(); n != 1 {
		t.Fatalf("summary scans = %d, want 1 for one file", n)
	}
	if reads != 1 {
		t.Fatalf("source reads = %d, want 1: the file was parsed more than once", reads)
	}
	for i, idx := range got {
		if idx != got[0] {
			t.Fatalf("worker %d got a different index object; callers must share one", i)
		}
		if !idx.local["round"] || !idx.local["ceil"] {
			t.Fatalf("worker %d got an index missing exports: %v", i, idx.surface())
		}
	}
}

// buildIndexForTest parses file and returns what extractFile would offer the
// cache for it: the index derived from the tree, and whether it is context-free.
func buildIndexForTest(t *testing.T, file string, src []byte, aliases map[string]tsAlias, known map[string]bool) (*namedExportIndex, bool) {
	t.Helper()
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(typescript.LanguageTypescript())); err != nil {
		t.Fatalf("set language: %v", err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	return buildNamedExportIndex(file, src, tsKindsFor(false), tree.RootNode(), aliases, known)
}

// surfaceKey renders an index the way a consumer compares it: the sorted export
// surface plus the proven default.
func surfaceKey(idx *namedExportIndex) string {
	return strings.Join(idx.surface(), ",") + "|" + idx.defaultName
}

// TestDerivedIndexIsAdoptedWithoutAScan pins the whole point of deriving the
// index from the extractor's own tree: a file the extractor already parsed must
// cost no summary scan and no second read, and the adopted answer must be the
// one every later consumer sees.
func TestDerivedIndexIsAdoptedWithoutAScan(t *testing.T) {
	src := []byte("export function round(){return 1}\nexport const ceil = 2\nexport default class Shape {}\n")
	known := map[string]bool{"src/origin.ts": true}
	idx, contextFree := buildIndexForTest(t, "src/origin.ts", src, nil, known)
	if !contextFree {
		t.Fatalf("a file that imports nothing must build a context-free index")
	}

	cache := newNamedExportCache()
	if !cache.adopt("src/origin.ts", idx) {
		t.Fatalf("adopt did not fill an empty entry")
	}
	var reads int
	readSrc := func(string) []byte { reads++; return src }
	got := cache.index("src/origin.ts", readSrc, nil, known)

	if got != idx {
		t.Fatalf("index() returned a different object; the adopted index must be the shared one")
	}
	if reads != 0 {
		t.Fatalf("source reads = %d, want 0: the adopted index was re-parsed", reads)
	}
	if n := cache.summaryScans(); n != 0 {
		t.Fatalf("summary scans = %d, want 0: deriving an index is not a parse", n)
	}
	if n := cache.derivedIndexes(); n != 1 {
		t.Fatalf("derived indexes = %d, want 1: a derived index must still be counted, just not as a scan", n)
	}
	if !got.local["round"] || !got.local["ceil"] {
		t.Fatalf("derived index lost named exports: %v", got.surface())
	}
	if got.defaultName != "Shape" {
		t.Fatalf("derived default = %q, want Shape", got.defaultName)
	}
}

// TestDerivedIndexMatchesAScanOfTheSameBytes is the equivalence the reuse rests
// on: whatever the extractor's tree yields must be what a scan would have said.
func TestDerivedIndexMatchesAScanOfTheSameBytes(t *testing.T) {
	src := []byte("const hidden = 1\nexport const shown = hidden\nexport class Widget {}\nexport default function make(){return new Widget()}\n")
	known := map[string]bool{"src/origin.ts": true}
	derived, contextFree := buildIndexForTest(t, "src/origin.ts", src, nil, known)
	if !contextFree {
		t.Fatalf("no import or export-from is present; index must be context-free")
	}
	scanned := parseNamedExportIndexBytes("src/origin.ts", src, nil, known)

	if a, b := surfaceKey(derived), surfaceKey(scanned); a != b {
		t.Fatalf("derived surface %q != scanned surface %q", a, b)
	}
	if derived.defaultName != scanned.defaultName {
		t.Fatalf("derived default %q != scanned default %q", derived.defaultName, scanned.defaultName)
	}
}

// TestContextBearingIndexIsNotAdopted is the fence. An index that names a module
// - or that only failed to name one because a specifier did not resolve - is a
// function of the alias map and known-file set of whoever built it, while the
// cache is keyed by file alone. Those must keep paying for their own scan.
func TestContextBearingIndexIsNotAdopted(t *testing.T) {
	known := map[string]bool{"src/origin.ts": true, "src/leaf.ts": true}
	cases := []struct {
		name string
		src  string
	}{
		{"export from", "export { round } from './leaf'\n"},
		{"star export", "export * from './leaf'\n"},
		{"namespace export", "export * as leaf from './leaf'\n"},
		{"unresolved export from", "export { round } from '@nowhere/leaf'\n"},
		{"re-exported import", "import { round } from './leaf'\nexport { round }\n"},
		{"unresolved re-exported import", "import { round } from '@nowhere/leaf'\nexport { round }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, contextFree := buildIndexForTest(t, "src/origin.ts", []byte(tc.src), nil, known)
			if contextFree {
				t.Fatalf("%s was treated as context-free; it depends on how the specifier resolves", tc.name)
			}
		})
	}
}

// TestAdoptNeverReplacesAScannedIndex keeps a consumer's answer stable. If a
// scan already ran for a file, that object is what someone may already hold;
// adoption must lose rather than swap it underneath them.
func TestAdoptNeverReplacesAScannedIndex(t *testing.T) {
	src := []byte("export const ceil = 2\n")
	known := map[string]bool{"src/origin.ts": true}
	cache := newNamedExportCache()
	first := cache.index("src/origin.ts", func(string) []byte { return src }, nil, known)

	derived, _ := buildIndexForTest(t, "src/origin.ts", src, nil, known)
	if cache.adopt("src/origin.ts", derived) {
		t.Fatalf("adopt reported filling an entry a scan had already filled")
	}
	if got := cache.index("src/origin.ts", nil, nil, known); got != first {
		t.Fatalf("the cached index changed identity after adoption")
	}
	if n := cache.derivedIndexes(); n != 0 {
		t.Fatalf("derived indexes = %d, want 0: adoption did not happen", n)
	}
	if n := cache.summaryScans(); n != 1 {
		t.Fatalf("summary scans = %d, want 1", n)
	}
}

// TestEmptyDerivedIndexMatchesAScan is root's independent finding, pinned here.
// Empty bytes parse to a valid childless root, so deriving from the tree used to
// produce a not-empty index while a scan short-circuits on length and returns
// the empty marker. surface() reports that marker, so the two disagreed on a
// recorded ExportSurface for any empty file the extractor happened to parse.
func TestEmptyDerivedIndexMatchesAScan(t *testing.T) {
	known := map[string]bool{"src/blank.ts": true}
	derived, contextFree := buildIndexForTest(t, "src/blank.ts", []byte(""), nil, known)
	scanned := parseNamedExportIndexBytes("src/blank.ts", []byte(""), nil, known)

	if !derived.empty {
		t.Fatalf("derived index of empty bytes is not marked empty; a scan marks it empty")
	}
	if derived.empty != scanned.empty {
		t.Fatalf("derived empty=%v != scanned empty=%v", derived.empty, scanned.empty)
	}
	if a, b := surfaceKey(derived), surfaceKey(scanned); a != b {
		t.Fatalf("derived surface %q != scanned surface %q", a, b)
	}
	// Nothing consulted a specifier, so the empty answer is still adoptable -
	// and adopting it is what keeps a later consumer from paying for a scan
	// that can only return the same marker.
	if !contextFree {
		t.Fatalf("an empty file names no module; its index must be context-free")
	}
}

// TestAdoptedEmptyIndexIsWhatConsumersSee closes the loop through the cache: the
// marker has to survive adoption, because absent/unreadable/empty are distinct
// and a consumer reads that distinction off the surface.
func TestAdoptedEmptyIndexIsWhatConsumersSee(t *testing.T) {
	known := map[string]bool{"src/blank.ts": true}
	derived, _ := buildIndexForTest(t, "src/blank.ts", []byte(""), nil, known)

	cache := newNamedExportCache()
	if !cache.adopt("src/blank.ts", derived) {
		t.Fatalf("adopt did not fill an empty entry")
	}
	got := cache.index("src/blank.ts", func(string) []byte { return []byte("") }, nil, known)
	if !got.empty {
		t.Fatalf("cache returned a non-empty index for an empty file")
	}
	if s := strings.Join(got.surface(), ","); s != "empty" {
		t.Fatalf("surface = %q, want %q", s, "empty")
	}
	if n := cache.summaryScans(); n != 0 {
		t.Fatalf("summary scans = %d, want 0", n)
	}
}

// TestExtractSessionRecordsEmptyExportSurface is the end-to-end form of the
// case above: an empty file is eligible (it binds no imports), so the extractor
// records a surface for it, and that surface must be the empty marker whether
// the index came from a scan or from the tree the parse loop had already built.
// A per-record assertion is what the invalidation proof actually compares.
func TestExtractSessionRecordsEmptyExportSurface(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"blank.ts": "",
		"leaf.ts":  "export const leaf = 1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := New().ExtractSession(context.Background(), dir, []string{"blank.ts", "leaf.ts"}, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := res.Records["blank.ts"]
	if rec == nil {
		t.Fatalf("no record for blank.ts; records: %v", res.Records)
	}
	if !rec.ExportSurfaceRecorded {
		t.Fatalf("empty file binds no imports, so its surface must be recorded, not unknown")
	}
	if got := strings.Join(rec.ExportSurface, ","); got != "empty" {
		t.Fatalf("blank.ts export surface = %q, want %q", got, "empty")
	}
	// The neighbour is the control: "empty" has to be the empty file's own
	// marker, not what every eligible file in this repo happens to record.
	if leaf := res.Records["leaf.ts"]; leaf == nil || !leaf.ExportSurfaceRecorded ||
		strings.Join(leaf.ExportSurface, ",") != "local:leaf" {
		t.Fatalf("leaf.ts surface = %v recorded=%v, want [local:leaf]", leaf.ExportSurface, leaf != nil && leaf.ExportSurfaceRecorded)
	}
}
