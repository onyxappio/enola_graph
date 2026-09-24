package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// withoutRecordedSurface returns records the way a version that predates the
// context-free export surface proof would have written them: the recorded
// default identity is kept, the surface is not. Tests that must exercise the
// DefaultExportName fallback use it, because a provider that binds imports and
// exports only locals now carries the stronger surface proof instead.
func withoutRecordedSurface(records map[string]*FileRecord) map[string]*FileRecord {
	out := make(map[string]*FileRecord, len(records))
	for rel, rec := range records {
		if rec == nil {
			out[rel] = nil
			continue
		}
		clone := *rec
		clone.ExportSurface = nil
		clone.ExportSurfaceRecorded = false
		clone.ExportSurfaceContextFree = false
		out[rel] = &clone
	}
	return out
}

// TestStage15RecordedImportedSurfaceFeedsAccuracyComparison covers the seam
// between the two rules. A provider that binds imports of its own but exports
// only locals gets a context-free surface recorded for it, which
// compareRecordExportSurface then uses in place of the default-identity
// fallback. The answers it must give are the ones the fallback gave: a body
// edit spares the importer, a moved default does not, and a surface that starts
// naming another module loses the proof in both directions.
//
// Every step moves exactly one thing. The file set, including the module C is
// forwarded to, is fixed at setup. The default is moved and moved back in two
// separately asserted sessions of their own, so that it is already sitting on A
// and stays there across both forwarding transitions - a refresh there can then
// only be the changed origin of C. Each step is checked against a cold
// extraction before any count is trusted.
func TestStage15RecordedImportedSurfaceFeedsAccuracyComparison(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const (
		localC     = "export const C = 3;\n"
		forwardedC = "export { C } from './other';\n"
	)
	// A is the default throughout except for the two sessions that move it, and
	// it stays local in every state, so forwarding C never leaves the default
	// unbound.
	provider := func(body, defaultName, cLine string) string {
		return "import { util } from './util';\n" +
			"export const A = util + " + body + ";\n" +
			"export const B = 2;\n" +
			cLine +
			"export default " + defaultName + ";\n"
	}
	write("src/util.ts", "export const util = 1;\n")
	write("src/other.ts", "export const C = 3;\n")
	write("src/value.ts", provider("0", "A", localC))
	write("src/use.ts", "import selected from './value';\nimport { B, C } from './value';\nexport const result = selected + B + C;\n")
	files := []string{"src/util.ts", "src/other.ts", "src/value.ts", "src/use.ts"}
	ext := New()
	// OnBeforeParse names every file the session decides to parse, which is how
	// each step below checks who was refreshed and not only how many were.
	var mu sync.Mutex
	parsed := map[string]bool{}
	watch := SessionHooks{OnBeforeParse: func(rel string) {
		mu.Lock()
		parsed[filepath.ToSlash(rel)] = true
		mu.Unlock()
	}}
	session := func(prev map[string]*FileRecord, dirty map[string]bool) *SessionResult {
		t.Helper()
		mu.Lock()
		parsed = map[string]bool{}
		mu.Unlock()
		res, sessErr := ext.ExtractSession(context.Background(), root, files, prev, dirty, watch)
		if sessErr != nil {
			t.Fatal(sessErr)
		}
		return res
	}
	wasParsed := func(rel string) bool {
		mu.Lock()
		defer mu.Unlock()
		return parsed[rel]
	}
	assertCold := func(step string, result *SessionResult) {
		t.Helper()
		cold, coldErr := ext.ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{})
		if coldErr != nil {
			t.Fatal(coldErr)
		}
		if !reflect.DeepEqual(result.Facts, cold.Facts) {
			t.Fatalf("%s: delta facts differ from a fresh cold extraction", step)
		}
	}
	sortedDeclared := func(rec *FileRecord) []string {
		out := slices.Clone(rec.Declared)
		slices.Sort(out)
		return out
	}
	// The declarations this provider makes are what stays put while its default
	// and its export origins move; a transition that also moved them would prove
	// less than it claims to.
	assertProvider := func(step string, rec *FileRecord, proven bool, wantDefault string, wantDeclared []string) {
		t.Helper()
		if rec == nil {
			t.Fatalf("%s: provider record missing", step)
		}
		if rec.ExportSurfaceRecorded != proven || rec.ExportSurfaceContextFree != proven {
			t.Fatalf("%s: surface proof = (recorded %v, context-free %v), want both %v",
				step, rec.ExportSurfaceRecorded, rec.ExportSurfaceContextFree, proven)
		}
		if rec.DefaultExportName != wantDefault {
			t.Fatalf("%s: default export is %q, want %q - the step is not isolated",
				step, rec.DefaultExportName, wantDefault)
		}
		if got := sortedDeclared(rec); !slices.Equal(got, wantDeclared) {
			t.Fatalf("%s: declared = %v, want %v", step, got, wantDeclared)
		}
		wantForwarded := !proven
		if got := len(rec.Reexports) > 0; got != wantForwarded {
			t.Fatalf("%s: reexports = %v, want forwarding=%v", step, rec.Reexports, wantForwarded)
		}
	}

	first, err := ext.ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	rec := first.Records["src/value.ts"]
	if rec == nil || rec.BindsNoImports() {
		t.Fatalf("fixture must exercise a provider that binds an import of its own: %+v", rec)
	}
	allLocal := sortedDeclared(rec)
	if len(allLocal) != 3 {
		t.Fatalf("fixture provider declares %v, want the three locals A, B and C", allLocal)
	}
	// The one declaration that leaves when C is forwarded, held by name so the
	// forwarded state can be checked against a set that differs in exactly it.
	var forwardedName string
	for _, name := range allLocal {
		if strings.HasSuffix(name, "C") {
			forwardedName = name
		}
	}
	if forwardedName == "" {
		t.Fatalf("cannot identify the declaration for C among %v", allLocal)
	}
	withoutC := slices.DeleteFunc(slices.Clone(allLocal), func(name string) bool { return name == forwardedName })
	assertProvider("initial", rec, true, "A", allLocal)
	t.Logf("stage15 bridge initial: surface=%v default=%q declared=%v", rec.ExportSurface, rec.DefaultExportName, allLocal)

	// One thing moves: the body of A. Every exported name and the selected
	// default stay where they were, so the recorded surface is equal and the
	// importer keeps its cached facts.
	write("src/value.ts", provider("1", "A", localC))
	body := session(first.Records, map[string]bool{"src/value.ts": true})
	assertCold("body edit", body)
	if body.Stats.FilesParsed != 1 || wasParsed("src/use.ts") {
		t.Fatalf("body-only provider edit parsed %d files (importer parsed=%v), want only the provider",
			body.Stats.FilesParsed, wasParsed("src/use.ts"))
	}
	assertProvider("body edit", body.Records["src/value.ts"], true, "A", allLocal)

	// One thing moves: which local the default selects. The declarations are
	// untouched, so only the default:<name> item of the surface differs, and the
	// importer that bound the default must be parsed again.
	write("src/value.ts", provider("1", "B", localC))
	swapped := session(body.Records, map[string]bool{"src/value.ts": true})
	assertCold("default swap", swapped)
	if swapped.Stats.FilesParsed != 2 || !wasParsed("src/use.ts") {
		t.Fatalf("default swap parsed %d files (importer parsed=%v), want the provider and its importer",
			swapped.Stats.FilesParsed, wasParsed("src/use.ts"))
	}
	assertProvider("default swap", swapped.Records["src/value.ts"], true, "B", allLocal)

	// And the same move back, asserted on its own, so that the default is
	// already on A and can be held there while the origin of C changes.
	write("src/value.ts", provider("1", "A", localC))
	reset := session(swapped.Records, map[string]bool{"src/value.ts": true})
	assertCold("default reset", reset)
	if reset.Stats.FilesParsed != 2 || !wasParsed("src/use.ts") {
		t.Fatalf("default reset parsed %d files (importer parsed=%v), want the provider and its importer",
			reset.Stats.FilesParsed, wasParsed("src/use.ts"))
	}
	assertProvider("default reset", reset.Records["src/value.ts"], true, "A", allLocal)

	// One thing moves: C stops being declared here and starts being forwarded
	// from a module that has been in the file set, unedited, since setup. The
	// default is still A. The index now consults a specifier, so the proof is
	// gone and the importer is refreshed.
	write("src/value.ts", provider("1", "A", forwardedC))
	forwarded := session(reset.Records, map[string]bool{"src/value.ts": true})
	assertCold("local to forwarded", forwarded)
	assertProvider("local to forwarded", forwarded.Records["src/value.ts"], false, "A", withoutC)
	if forwarded.Stats.FilesParsed != 2 || !wasParsed("src/use.ts") {
		t.Fatalf("local to forwarded parsed %d files (importer parsed=%v), want the provider and its importer",
			forwarded.Stats.FilesParsed, wasParsed("src/use.ts"))
	}
	fwdRec := forwarded.Records["src/value.ts"]
	t.Logf("stage15 bridge forwarded: surface_recorded=%v reexports=%v provider_resolved=%v importer_resolved=%v",
		fwdRec.ExportSurfaceRecorded, fwdRec.Reexports, fwdRec.ResolvedFiles,
		forwarded.Records["src/use.ts"].ResolvedFiles)

	// And back, with the default still on A: the provider regains the proof and
	// its third declaration, and the importer is refreshed on the way there
	// rather than on the strength of a surface recorded while C was forwarded.
	write("src/value.ts", provider("1", "A", localC))
	restored := session(forwarded.Records, map[string]bool{"src/value.ts": true})
	assertCold("forwarded to local", restored)
	assertProvider("forwarded to local", restored.Records["src/value.ts"], true, "A", allLocal)
	if restored.Stats.FilesParsed != 2 || !wasParsed("src/use.ts") {
		t.Fatalf("forwarded to local parsed %d files (importer parsed=%v), want the provider and its importer",
			restored.Stats.FilesParsed, wasParsed("src/use.ts"))
	}

	// Nothing moves: no file is parsed, and the result is still the cold one.
	steady := session(restored.Records, map[string]bool{})
	assertCold("steady no-op", steady)
	if steady.Stats.FilesParsed != 0 {
		t.Fatalf("unchanged session parsed %d files, want 0", steady.Stats.FilesParsed)
	}
}

// TestStage15StableDeclarationForwardingKeepsDeclarationsAndLosesProof holds
// the declarations still. The provider keeps declaring C in both states and
// re-exports nothing in either, so neither the declared-name delta nor the
// re-export set can be what refreshes the importer: the only thing that moves
// is where the exported C comes from, and with it the context-free proof.
//
// This is the same seam the root graph-session test covers from the planner
// side, exercised here through ExtractSession with the planner hook off, which
// is the path where the accuracy pre-pass and the recorded surface meet.
//
// One limit worth stating: the dependency facts this extractor emits for the
// importer are module-granular - one fact per imported module, whose
// target_file stays src/value.ts in both states. The per-name walk to
// src/other.ts is the planner's, so what moves here is the provider record,
// whose resolved files gain and lose src/other.ts.
func TestStage15StableDeclarationForwardingKeepsDeclarationsAndLosesProof(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const (
		// C is declared in both states. In the second it is the imported alias
		// that gets exported under the name C, so the declaration stays and only
		// the origin of the exported name moves.
		localC  = "const C = 3;\nexport { C };\n"
		remoteC = "import { C as remoteC } from './other';\nconst C = 3;\nexport { remoteC as C };\n"
	)
	provider := func(cBlock string) string {
		return "import { util } from './util';\n" +
			"export const A = util + 1;\n" +
			"export const B = 2;\n" +
			cBlock +
			"export default A;\n"
	}
	write("src/util.ts", "export const util = 1;\n")
	write("src/other.ts", "export const C = 3;\n")
	write("src/value.ts", provider(localC))
	write("src/use.ts", "import selected from './value';\nimport { B, C } from './value';\nexport const result = selected + B + C;\n")
	files := []string{"src/util.ts", "src/other.ts", "src/value.ts", "src/use.ts"}
	ext := New()
	var mu sync.Mutex
	parsed := map[string]bool{}
	watch := SessionHooks{OnBeforeParse: func(rel string) {
		mu.Lock()
		parsed[filepath.ToSlash(rel)] = true
		mu.Unlock()
	}}
	session := func(prev map[string]*FileRecord, dirty map[string]bool) *SessionResult {
		t.Helper()
		mu.Lock()
		parsed = map[string]bool{}
		mu.Unlock()
		res, sessErr := ext.ExtractSession(context.Background(), root, files, prev, dirty, watch)
		if sessErr != nil {
			t.Fatal(sessErr)
		}
		return res
	}
	wasParsed := func(rel string) bool {
		mu.Lock()
		defer mu.Unlock()
		return parsed[rel]
	}
	assertCold := func(step string, result *SessionResult) {
		t.Helper()
		cold, coldErr := ext.ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{})
		if coldErr != nil {
			t.Fatal(coldErr)
		}
		if !reflect.DeepEqual(result.Facts, cold.Facts) {
			t.Fatalf("%s: delta facts differ from a fresh cold extraction", step)
		}
	}
	sorted := func(in []string) []string {
		out := slices.Clone(in)
		slices.Sort(out)
		return out
	}
	assertProvider := func(step string, rec *FileRecord, proven bool, wantDeclared, wantResolved []string) {
		t.Helper()
		if rec == nil {
			t.Fatalf("%s: provider record missing", step)
		}
		if rec.ExportSurfaceRecorded != proven || rec.ExportSurfaceContextFree != proven {
			t.Fatalf("%s: surface proof = (recorded %v, context-free %v), want both %v",
				step, rec.ExportSurfaceRecorded, rec.ExportSurfaceContextFree, proven)
		}
		if got := sorted(rec.Declared); !slices.Equal(got, wantDeclared) {
			t.Fatalf("%s: declared = %v, want %v - the declarations were supposed to hold still", step, got, wantDeclared)
		}
		if len(rec.Reexports) != 0 {
			t.Fatalf("%s: reexports = %v, want none in either state", step, rec.Reexports)
		}
		if got := sorted(rec.ResolvedFiles); !slices.Equal(got, wantResolved) {
			t.Fatalf("%s: resolved files = %v, want %v", step, got, wantResolved)
		}
		if rec.DefaultExportName != "A" {
			t.Fatalf("%s: default export is %q, want A in every state", step, rec.DefaultExportName)
		}
	}
	var (
		declared     = []string{"src.A", "src.B", "src.C"}
		localFiles   = []string{"src/util.ts"}
		remoteFiles  = []string{"src/other.ts", "src/util.ts"}
		importerDeps = func(res *SessionResult) []string {
			out := []string{}
			for _, f := range res.Records["src/use.ts"].Facts {
				if f.Kind == facts.KindDependency {
					out = append(out, f.PropString(facts.PropTargetFile))
				}
			}
			return sorted(out)
		}
	)

	first, err := ext.ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	assertProvider("initial", first.Records["src/value.ts"], true, declared, localFiles)
	baseDeps := importerDeps(first)

	// One thing moves: the exported C stops being the local declaration and
	// becomes the alias imported from other.ts. Declarations and re-exports are
	// identical on both sides, so the refresh can only be the lost proof.
	write("src/value.ts", provider(remoteC))
	remote := session(first.Records, map[string]bool{"src/value.ts": true})
	assertCold("local to remote alias", remote)
	assertProvider("local to remote alias", remote.Records["src/value.ts"], false, declared, remoteFiles)
	if remote.Stats.FilesParsed != 2 || !wasParsed("src/use.ts") {
		t.Fatalf("local to remote alias parsed %d files (importer parsed=%v), want the provider and its importer",
			remote.Stats.FilesParsed, wasParsed("src/use.ts"))
	}
	if got := importerDeps(remote); !slices.Equal(got, baseDeps) {
		t.Logf("importer module dependencies moved from %v to %v", baseDeps, got)
	}

	// And back: the declaration that never left is exported directly again, the
	// provider stops resolving other.ts, and the proof returns.
	write("src/value.ts", provider(localC))
	restored := session(remote.Records, map[string]bool{"src/value.ts": true})
	assertCold("remote alias to local", restored)
	assertProvider("remote alias to local", restored.Records["src/value.ts"], true, declared, localFiles)
	if restored.Stats.FilesParsed != 2 || !wasParsed("src/use.ts") {
		t.Fatalf("remote alias to local parsed %d files (importer parsed=%v), want the provider and its importer",
			restored.Stats.FilesParsed, wasParsed("src/use.ts"))
	}
	if got := importerDeps(restored); !slices.Equal(got, baseDeps) {
		t.Fatalf("remote alias to local: importer dependencies are %v, want the initial %v", got, baseDeps)
	}

	steady := session(restored.Records, map[string]bool{})
	assertCold("steady no-op", steady)
	if steady.Stats.FilesParsed != 0 {
		t.Fatalf("unchanged session parsed %d files, want 0", steady.Stats.FilesParsed)
	}
}
