package tsextractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func discoveryRepo(t *testing.T, pkg string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"package.json":              pkg,
		"tsconfig.json":             `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["./src/*"]}}}`,
		"src/index.ts":              "export const index = 1;\n",
		"packages/ui/package.json":  `{"name":"@acme/ui","types":"./src/index.ts"}`,
		"packages/ui/src/index.ts":  "export const ui = 1;\n",
		"packages/ui/tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@ui/*":["./src/*"]}}}`,
		"src/consumer.ts":           "import { ui } from '@acme/ui';\nimport { index } from '@app/index';\nexport const used = ui + index;\n",
	}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func discoveryFiles() []string {
	return []string{"src/index.ts", "src/consumer.ts", "packages/ui/src/index.ts"}
}

// sessionFacts renders the extracted facts as a sorted multiset. The
// extractor does not promise an emission order - two runs of the same tree can
// hand back the same directory facts in either order - so comparing the
// rendered slice directly would make this guard fail on a difference it is not
// asking about.
func sessionFacts(t *testing.T, res *SessionResult) string {
	t.Helper()
	lines := make([]string, 0, len(res.Facts))
	for _, f := range res.Facts {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(b))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// A shared snapshot is only worth having if it is the same answer the caller
// would have read for itself, so both halves are asserted here: an extraction
// handed a snapshot taken over its own capture does not read the tree again,
// and one handed a snapshot taken over different bytes produces exactly what it
// produces with no snapshot at all. The counter alone would pass if reuse
// quietly answered from the wrong tree; the fact comparison is what rules that
// out.
func TestDiscoveryReuseRequiresTheCapturedBytesItWasTakenOver(t *testing.T) {
	const declared = `{"name":"app","workspaces":["packages/*"],"dependencies":{"vue":"^3.0.0"}}`
	const retargeted = `{"name":"app","workspaces":["packages/*"]}`
	dir := discoveryRepo(t, declared)
	ext := New()
	ctx := context.Background()
	files := discoveryFiles()
	captured := map[string][]byte{"package.json": []byte(declared)}

	disc := ext.NewDiscovery(ctx, dir, captured)

	shared, err := ext.ExtractSession(ctx, dir, files, nil, nil, SessionHooks{Sources: captured, Discovery: disc})
	if err != nil {
		t.Fatal(err)
	}
	if shared.Stats.DiscoveryPasses != 0 {
		t.Fatalf("an extraction handed the snapshot taken over its own capture built %d more, want none", shared.Stats.DiscoveryPasses)
	}
	unshared, err := ext.ExtractSession(ctx, dir, files, nil, nil, SessionHooks{Sources: captured})
	if err != nil {
		t.Fatal(err)
	}
	if unshared.Stats.DiscoveryPasses != 1 {
		t.Fatalf("an extraction handed no snapshot built %d, want exactly 1", unshared.Stats.DiscoveryPasses)
	}
	if sessionFacts(t, shared) != sessionFacts(t, unshared) {
		t.Fatal("sharing the run's discovery changed the extracted graph")
	}

	// The same snapshot against a capture it was not taken over: the package
	// declaration it read is no longer the one this extraction will read, so it
	// is refused rather than trusted.
	other := map[string][]byte{"package.json": []byte(retargeted)}
	stale, err := ext.ExtractSession(ctx, dir, files, nil, nil, SessionHooks{Sources: other, Discovery: disc})
	if err != nil {
		t.Fatal(err)
	}
	if stale.Stats.DiscoveryPasses != 1 {
		t.Fatalf("a snapshot taken over different captured bytes was reused (%d rebuild(s))", stale.Stats.DiscoveryPasses)
	}
	fresh, err := ext.ExtractSession(ctx, dir, files, nil, nil, SessionHooks{Sources: other})
	if err != nil {
		t.Fatal(err)
	}
	if sessionFacts(t, stale) != sessionFacts(t, fresh) {
		t.Fatal("refusing a stale snapshot did not leave the extraction exactly as it is without one")
	}
}

// The projected context keys are what decide invalidation, so reading them from
// the run's snapshot has to leave every one of them identical to reading the
// tree directly - including the per-file alias and package-name projection,
// whose known-file set is genuinely not the extraction's.
func TestSessionContextFromSharedDiscoveryMatchesDirectReads(t *testing.T) {
	dir := discoveryRepo(t, `{"name":"app","workspaces":["packages/*"],"dependencies":{"vue":"^3.0.0"}}`)
	ext := New()
	files := append(discoveryFiles(), "package.json", "tsconfig.json", "packages/ui/package.json", "packages/ui/tsconfig.json")
	paths := []string{"package.json", "tsconfig.json", "packages/ui/package.json", "packages/ui/tsconfig.json"}
	raw := map[string][]byte{}
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		raw[p] = b
	}

	direct, directPerFile, _, _ := ext.SessionContext(dir, raw, paths, files, nil)
	shared, sharedPerFile, _, _ := ext.SessionContext(dir, raw, paths, files, ext.NewDiscovery(context.Background(), dir, nil))

	if len(direct) == 0 || len(directPerFile) == 0 {
		t.Fatal("the fixture projected no context at all")
	}
	for k, v := range direct {
		if shared[k] != v {
			t.Fatalf("context key %q became %q with a shared discovery, want %q", k, shared[k], v)
		}
	}
	if len(shared) != len(direct) {
		t.Fatalf("a shared discovery projected %d context keys, want %d", len(shared), len(direct))
	}
	for k, v := range directPerFile {
		if sharedPerFile[k] != v {
			t.Fatalf("per-file context for %q became %q with a shared discovery, want %q", k, sharedPerFile[k], v)
		}
	}
	if len(sharedPerFile) != len(directPerFile) {
		t.Fatalf("a shared discovery projected %d per-file keys, want %d", len(sharedPerFile), len(directPerFile))
	}
}
