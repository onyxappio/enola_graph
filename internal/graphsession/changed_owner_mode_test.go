package graphsession

import (
	"github.com/enola-labs/enola/internal/graphinput"
	"path/filepath"
	"testing"
)

func TestChangedOwnerModeToggleDiscardsStaleDigests(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export function a(){return 1}"})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ChangedOwnersOnly: true}
	cons := NewConsumer()
	admissionRun(t, eng, root, opts, cons)
	opts.ChangedOwnersOnly = false
	writeFile(t, root, "a.ts", "export function a(){return 1}; export function b(){return 2}")
	admissionRun(t, eng, root, opts, cons)
	state, err := readStateFile(filepath.Join(opts.StateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.OwnerDigestVersion != "" || len(state.OwnerDigests) != 0 {
		t.Fatal("mode-off publish retained obsolete digest proof")
	}
	opts.ChangedOwnersOnly = true
	// Restore the original graph: stale fingerprints would incorrectly suppress it.
	writeFile(t, root, "a.ts", "export function a(){return 1}")
	result, sink := admissionRun(t, eng, root, opts, cons)
	begins, _, _, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(begins) != 1 {
		t.Fatalf("envelopes: %v", err)
	}
	if begins[0].OwnerScopeCount == 0 {
		t.Fatal("restored source was suppressed against an obsolete fingerprint")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	quiet, events := admissionRun(t, eng, root, opts, cons)
	assertNoPublication(t, quiet, events, result.TargetGeneration, "mode-toggle no-op")
}
