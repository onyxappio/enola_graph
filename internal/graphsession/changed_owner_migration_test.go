package graphsession

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
)

func TestChangedOwnerScopeMigratesLegacyAndRenames(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":   "export function a(){return 1}",
		"src/use.ts": "import {a} from './a'; export function use(){return a()}",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, root, opts, cons)
	opts.ChangedOwnersOnly = true
	// Legacy state has no contribution fingerprints. A graph-neutral source edit
	// must establish them conservatively, never treating missing data as equality.
	writeFile(t, root, "src/a.ts", "export function a(){return 1}\n// first edit\n")
	_, sink := admissionRun(t, eng, root, opts, cons)
	begins, _, _, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(begins) != 1 {
		t.Fatalf("migration envelopes: %v", err)
	}
	if begins[0].OwnerScopeCount == 0 {
		t.Fatal("unknown legacy contributions suppressed")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	// A subsequent neutral edit now has a confirmed contribution to compare.
	writeFile(t, root, "src/a.ts", "export function a(){return 1}\n// second edit\n")
	result, sink := admissionRun(t, eng, root, opts, cons)
	begins, _, _, err = DecodeRun(sink.CloneRecords())
	if err != nil || len(begins) != 1 {
		t.Fatalf("neutral envelopes: %v", err)
	}
	if begins[0].OwnerScopeCount != 0 {
		t.Fatalf("neutral edit published %d owners", begins[0].OwnerScopeCount)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	unchanged, quiet := admissionRun(t, eng, root, opts, cons)
	assertNoPublication(t, unchanged, quiet, result.TargetGeneration, "migrated no-op")
	if err := os.Rename(filepath.Join(root, "src/a.ts"), filepath.Join(root, "src/renamed.ts")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "src/use.ts", "import {a} from './renamed'; export function use(){return a()}")
	_, sink = admissionRun(t, eng, root, opts, cons)
	begins, _, _, err = DecodeRun(sink.CloneRecords())
	if err != nil || len(begins) != 1 {
		t.Fatalf("rename envelopes: %v", err)
	}
	found := map[string]bool{}
	for _, owner := range begins[0].OwnerScope {
		found[owner.ID] = true
	}
	for _, path := range []string{"src/a.ts", "src/renamed.ts", "src/use.ts"} {
		if !found[path] {
			t.Errorf("rename omitted %s", path)
		}
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
