package graphsession

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
)

func TestChangedOwnerScopeSkipsIdenticalDependents(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export function a(){return 1}",
		"src/barrel.ts": "export * from './a';",
		"src/use.ts":    "import {a} from './barrel'; export function use(){return a()}",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ChangedOwnersOnly: true}
	cons := NewConsumer()
	admissionRun(t, eng, root, opts, cons)
	writeFile(t, root, "src/a.ts", "export function a(){return 1}; export function added(){return 2}")
	result, sink := admissionRun(t, eng, root, opts, cons)
	bs, _, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) != 1 || len(ends) != 1 {
		t.Fatalf("envelopes: %v", err)
	}
	for _, o := range bs[0].OwnerScope {
		if o.ID == "src/use.ts" {
			t.Fatal("unchanged importer published")
		}
	}
	if bs[0].OwnerScopeCount == 0 {
		t.Fatal("changed declaration was omitted")
	}
	if bs[0].OwnerScopeDigest != ends[0].OwnerScopeDigest {
		t.Fatal("scope changed after Begin")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	unchanged, quiet := admissionRun(t, eng, root, opts, cons)
	assertNoPublication(t, unchanged, quiet, result.TargetGeneration, "identical source no-op")
	if err := os.Remove(filepath.Join(root, "src/a.ts")); err != nil {
		t.Fatal(err)
	}
	_, sink = admissionRun(t, eng, root, opts, cons)
	bs, _, _, err = DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) != 1 {
		t.Fatal(err)
	}
	found := false
	for _, o := range bs[0].OwnerScope {
		if o.ID == "src/a.ts" {
			found = true
		}
	}
	if !found {
		t.Fatal("deleted contribution was omitted")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
