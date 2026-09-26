package graphsession

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
)

func TestDeleteParseReasonsDoNotClaimSemanticContextChange(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export function a(){return 1}",
		"src/barrel.ts": "export * from './a';",
		"src/use.ts":    "import {a} from './barrel'; export function use(){return a()}",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ChangedOwnersOnly: true}
	cons := NewConsumer()
	admissionRun(t, eng, root, opts, cons)
	if err := os.Remove(filepath.Join(root, "src/a.ts")); err != nil {
		t.Fatal(err)
	}
	result, sink := admissionRun(t, eng, root, opts, cons)
	if result.ParsedFiles == 0 {
		t.Fatal("fixture did not exercise dependent reparsing")
	}
	reasons := result.Invalidation.ParsedByReason
	if reasons["file semantic context"] != 0 {
		t.Fatalf("dependency work mislabeled: %v", reasons)
	}
	if reasons["dependency invalidation"]+reasons["changed import resolution"] == 0 {
		t.Fatalf("no explicit dependency reason: %v", reasons)
	}
	_, _, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(ends) != 1 {
		t.Fatalf("End: %v", err)
	}
	if ends[0].Completeness.ParsedByReason["file semantic context"] != 0 {
		t.Fatal("stream retained misleading reason")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
