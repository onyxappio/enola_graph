package graphsession

import (
	"github.com/enola-labs/enola/internal/graphinput"
	"testing"
)

// A file cannot bind an arbitrary sibling declaration without an import (the
// accepted #66 contract). Keep that unresolved negative through provider edits,
// while a separate explicit import proves useful replay when its export appears
// and disappears. Assert cold equality before the parse-budget check.
func TestExplicitImportExportReplayPreservesUnboundNegativeAcrossEdits(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/provider.ts": "export function known() { return 1; }\n",
		"src/consumer.ts": "export function consume() { return future(); }\n",
		"src/imported.ts": "import { future } from './provider';\nexport function consumeImported() { return future(); }\n",
		"src/other.ts":    "export function unrelated() { return 0; }\n",
	})
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, dir, opts, cons)
	assertCallResolution := func(owner string, wantResolved bool) {
		t.Helper()
		found := false
		for _, edge := range cons.Edges[ownerKey(owner)] {
			if edge.Kind == "calls" && edge.TargetName == "src.future" {
				found = true
				if got := edge.TargetID != ""; got != wantResolved {
					t.Fatalf("%s future() resolved=%v, want %v: %+v", owner, got, wantResolved, edge)
				}
			}
		}
		if !found {
			t.Fatalf("%s has no future() call edge: %+v", owner, cons.Edges[ownerKey(owner)])
		}
	}
	assertCallResolution("src/consumer.ts", false)
	assertCallResolution("src/imported.ts", false)
	for _, tc := range []struct{ name, source string }{
		{"add-export", "export function known() { return 1; }\nexport function future() { return 2; }\n"},
		{"rename-remove-export", "export function known() { return 1; }\nexport function renamed() { return 2; }\n"},
		{"restore-export", "export function known() { return 1; }\nexport function future() { return 3; }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, dir, "src/provider.ts", tc.source)
			res, _ := admissionRun(t, eng, dir, opts, cons)
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
			assertCallResolution("src/consumer.ts", false)
			wantImported := tc.name != "rename-remove-export"
			assertCallResolution("src/imported.ts", wantImported)
			if res.ParsedFiles < 2 || res.ParsedFiles > 3 {
				t.Fatalf("expected provider and explicit importer to be refreshed, with at most one global-name consumer: parsed=%d reasons=%+v", res.ParsedFiles, res.Invalidation)
			}
		})
	}
}
