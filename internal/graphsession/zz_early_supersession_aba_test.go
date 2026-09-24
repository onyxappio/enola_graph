package graphsession

import (
	"context"
	"errors"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// TestEarlySupersessionRefusesTheRevertedWrite records the one behaviour the
// fence before Begin changes rather than only adds: a write that lands after
// the run captured its bytes and is reverted before the fence at End looks.
//
// The fence at End compares the tree against the hashes each parse recorded, so
// a value that leaves and comes back is invisible to it and such a run commits
// today. The earlier fence asks the same question while the second write has
// not happened yet, sees the difference, and refuses.
//
// The capture here is a real one, taken the way the resident takes it, and the
// away-write happens before the transaction is entered; only the revert uses
// the existing parse hook, because a revert has to land after the announcement
// to be the case under test. Nothing is published either way and the retry
// converges to the same graph, so the whole cost is one extra attempt in an
// interleaving that has to be built deliberately. It is asserted here so that
// cost is a measured fact with a name rather than a footnote.
func TestEarlySupersessionRefusesTheRevertedWrite(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":   "export const a=1;\n",
		"src/use.ts": "import {a} from './a'; export const use=a;\n",
	})
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	sink := &graphstream.MemorySink{}
	resident, err := OpenSession(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	initial, err := resident.reconcile(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	offset := len(sink.CloneRecords())

	writeFile(t, root, "src/a.ts", "export const a=2;\n")
	resident.mu.Lock()
	input, reason := resident.contentInputs([]string{"src/a.ts"}, &WorkCounters{})
	resident.mu.Unlock()
	if reason != "" || input == nil || string(input.sources["src/a.ts"]) != "export const a=2;\n" {
		t.Fatalf("expected the changed bytes to be captured: reason=%s", reason)
	}

	// Away before the transaction is entered, back before the fence at End reads.
	writeFile(t, root, "src/a.ts", "export const a=3;\n")
	reverted := false
	resident.opts.OnBeforeParse = func(string) {
		if !reverted {
			reverted = true
			writeFile(t, root, "src/a.ts", "export const a=2;\n")
		}
	}

	resident.mu.Lock()
	_, _, err = resident.transaction(ctx, input, true)
	resident.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("want a retryable refusal for the reverted write, got %v", err)
	}
	interrupted := sink.CloneRecords()[offset:]
	if len(interrupted) != 0 {
		t.Fatalf("refused attempt published %d record(s)", len(interrupted))
	}
	st, err := loadCommittedState(opts.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Generation != initial.TargetGeneration {
		t.Fatalf("refusal advanced generation to %d", st.Generation)
	}

	resident.opts.OnBeforeParse = nil
	result, err := resident.reconcile(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if result.TargetGeneration != initial.TargetGeneration+1 {
		t.Fatalf("retry generation=%d, want %d", result.TargetGeneration, initial.TargetGeneration+1)
	}
	if !reverted {
		t.Log("note: the revert never fired, because the refusal landed before any parse; that is the point")
	}
}
