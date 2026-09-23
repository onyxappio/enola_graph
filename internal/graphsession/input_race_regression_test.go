package graphsession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/graphstream"
)

// readErr is the error the operating system itself returns, not a synthetic
// one: the classification has to survive the exact wrapping os.ReadFile does.
func readErr(t *testing.T, path string) error {
	t.Helper()
	_, err := os.ReadFile(path)
	if err == nil {
		t.Fatalf("reading %s was expected to fail", path)
	}
	return err
}

// Only a disappearance is evidence that the inputs changed. Everything else a
// read can fail with is a fault the operator has to see, because the watch
// loop turns ErrInputsChanged into an immediate retry: misclassifying a
// permission or I/O failure there would spin forever instead of reporting.
func TestInputRaceClassifiesOnlyDisappearance(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "present.ts", "export const a=1;\n")
	writeFile(t, root, "notdir.ts", "export const b=2;\n")
	writeFile(t, root, "locked.ts", "export const c=3;\n")
	locked := filepath.Join(root, "locked.ts")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o644)
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("this user can read a mode-0 file; the permission half cannot be proven here")
	}

	cases := []struct {
		name    string
		err     error
		changed bool
	}{
		{"deleted file", readErr(t, filepath.Join(root, "gone.ts")), true},
		{"parent replaced by a file", readErr(t, filepath.Join(root, "notdir.ts", "inner.ts")), true},
		{"wrapped not-exist", errors.Join(errors.New("context"), os.ErrNotExist), true},
		{"unreadable file", readErr(t, locked), false},
		{"unrelated failure", errors.New("transport closed"), false},
		{"no failure", nil, false},
	}
	for _, c := range cases {
		if got := vanishedDuringRun(c.err); got != c.changed {
			t.Fatalf("%s: vanishedDuringRun=%v want %v (err=%v)", c.name, got, c.changed, c.err)
		}
		if c.err == nil {
			continue
		}
		wrapped := classifyVanished(c.err, "source", "x.ts", "refusing successful EndReplace")
		if errors.Is(wrapped, ErrInputsChanged) != c.changed {
			t.Fatalf("%s: classified as retryable=%v want %v (%v)", c.name, !c.changed, c.changed, wrapped)
		}
		if !errors.Is(wrapped, c.err) {
			t.Fatalf("%s: classification dropped the original error: %v", c.name, wrapped)
		}
	}
}

// The composition context is read before BeginReplace, from the dirty files
// themselves. A file that is deleted between the walk and that read is the same
// race as one deleted before the final rehash, and it has to reach the watch as
// a reconcile rather than as a reason to stop.
func TestInputRaceCompositionContextDeletionIsRetryable(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1;\n"})
	eng := testEngine(t, root)
	newSession := func() *session {
		s := &session{abs: root, eng: eng, state: newState("repo", "default", root, "test")}
		s.state.FrameworkSig = "previous-composition-signature"
		return s
	}
	files := []string{"a.ts", "gone.ts"}

	_, err := newSession().frameworkDirtyRequiresFullScope(files, map[string]*FileState{}, map[string]string{}, false)
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a file deleted while the composition context was read ended the run with %v, want ErrInputsChanged", err)
	}

	// The same call over a file that is present but unreadable must not become
	// a retry: nothing about the inputs changed, and the watch must surface it.
	writeFile(t, root, "locked.ts", "export const c=3;\n")
	locked := filepath.Join(root, "locked.ts")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o644)
	if _, rerr := os.ReadFile(locked); rerr == nil {
		t.Skip("this user can read a mode-0 file; the permission half cannot be proven here")
	}
	_, err = newSession().frameworkDirtyRequiresFullScope([]string{"a.ts", "locked.ts"}, map[string]*FileState{}, map[string]string{}, false)
	if err == nil {
		t.Fatal("an unreadable composition input completed the plan")
	}
	if errors.Is(err, ErrInputsChanged) {
		t.Fatalf("an unreadable composition input was classified as a retryable input change: %v", err)
	}
}

// revalidateCapturedInputs is the other side of the same rule: the bytes this
// run compiled from have to still be the tree. A vanished capture is a change;
// a capture that is still there and cannot be read is not.
func TestInputRaceCapturedRevalidationSeparatesLossFromFault(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "kept.ts", "export const a=1;\n")
	writeFile(t, root, "lost.ts", "export const b=2;\n")
	writeFile(t, root, "locked.ts", "export const c=3;\n")
	body := func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	withCaptured := func(m map[string][]byte) *session {
		return &session{abs: root, capturedSources: m, state: newState("repo", "default", root, "test")}
	}

	if err := withCaptured(map[string][]byte{"kept.ts": body("kept.ts")}).revalidateCapturedInputs("refusing"); err != nil {
		t.Fatalf("an unchanged capture failed revalidation: %v", err)
	}

	lost := map[string][]byte{"lost.ts": body("lost.ts")}
	if err := os.Remove(filepath.Join(root, "lost.ts")); err != nil {
		t.Fatal(err)
	}
	if err := withCaptured(lost).revalidateCapturedInputs("refusing"); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a deleted capture ended revalidation with %v, want ErrInputsChanged", err)
	}

	locked := filepath.Join(root, "locked.ts")
	captured := map[string][]byte{"locked.ts": body("locked.ts")}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o644)
	if _, rerr := os.ReadFile(locked); rerr == nil {
		t.Skip("this user can read a mode-0 file; the permission half cannot be proven here")
	}
	err := withCaptured(captured).revalidateCapturedInputs("refusing")
	if err == nil {
		t.Fatal("an unreadable capture passed revalidation")
	}
	if errors.Is(err, ErrInputsChanged) {
		t.Fatalf("an unreadable capture was classified as a retryable input change: %v", err)
	}
}

// The behaviour the classification exists for: a real watcher, a real deletion
// landing inside a transaction, and no process exit. The run that raced is
// refused without an End, the watch reconciles on its own, sustained edits
// afterwards keep being published, and what a consumer of the whole stream ends
// up holding is exactly what a cold run over the final tree publishes.
// coldConsumerUnderContract is coldConsumer with the owner contract made
// explicit. Both contracts are live: --authoritative-scope defaults to false,
// so a watch can run either one, and session.go drops every synthetic-owned
// fact only under the frozen v2 contract. Comparing a legacy-contract stream
// against a frozen-contract oracle would report a divergence that is only the
// two contracts disagreeing, so the oracle has to match the run under test.
func coldConsumerUnderContract(t *testing.T, root string, authoritative bool) *Consumer {
	t.Helper()
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), testEngine(t, root), root, sink, Options{
		StateDir:           t.TempDir(),
		AuthoritativeFiles: authoritative,
	}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	if err := cold.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	return cold
}

// A captured source that disappears mid-run must not end the watch, and what
// the watch leaves on the tape afterwards must be exactly what a cold run over
// the same tree publishes. The deletion is armed only for the delta that
// follows a committed baseline, so the run that loses the source is a run that
// had already published it: deleting during the initial transaction would make
// the first End the recovered baseline and prove nothing about the retry.
func TestInputRaceWatchSurvivesDeletionAndConvergesToCold(t *testing.T) {
	for _, authoritative := range []bool{false, true} {
		name := "legacy"
		if authoritative {
			name = "authoritative"
		}
		t.Run(name, func(t *testing.T) {
			watchDeletionConvergence(t, authoritative)
		})
	}
}

func watchDeletionConvergence(t *testing.T, authoritative bool) {
	t.Helper()
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a=1;\n",
		"src/doomed.ts": "export const doomed=1;\n",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sink := &watchEndSink{ends: make(chan struct{}, 32)}
	done := make(chan error, 1)
	deleted := make(chan error, 1)
	var armed atomic.Bool
	state := t.TempDir()
	go func() {
		done <- Watch(ctx, testEngine(t, root), root, sink, Options{
			StateDir:           state,
			WatchEvery:         time.Millisecond,
			AuthoritativeFiles: authoritative,
			OnBeforeParse: func(string) {
				if armed.CompareAndSwap(true, false) {
					deleted <- os.Remove(filepath.Join(root, "src/doomed.ts"))
				}
			},
		})
	}()
	waitEnd := func(what string) {
		t.Helper()
		select {
		case <-sink.ends:
		case err := <-done:
			t.Fatalf("watch exited during %s: %v", what, err)
		case <-ctx.Done():
			t.Fatalf("watch published nothing for %s", what)
		}
	}
	// An End is published before the state it describes is committed, so the
	// generation has to be read from the store, not inferred from the End.
	committedAtLeast := func(want int64) *State {
		t.Helper()
		for {
			st, err := loadCommittedState(state)
			if err != nil {
				t.Fatal(err)
			}
			if st != nil && st.Generation >= want {
				return st
			}
			select {
			case <-ctx.Done():
				t.Fatal("the watch never committed the generation under test")
			case <-time.After(time.Millisecond):
			}
		}
	}
	waitEnd("the baseline")
	initial := committedAtLeast(1)

	armed.Store(true)
	independentWrite(t, root, "src/a.ts", "export const a=2;\n")
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatal(err)
		}
	case err := <-done:
		t.Fatalf("watch exited before the deletion: %v", err)
	case <-ctx.Done():
		t.Fatal("the deletion hook never ran")
	}
	// A watch that classified the deletion as a hard failure exits here instead
	// of reconciling, which is the defect this guards.
	waitEnd("the reconcile after the deletion")
	reconciled := committedAtLeast(initial.Generation + 1)
	if reconciled.Generation != initial.Generation+1 {
		t.Fatalf("the aborted attempt advanced the generation: baseline=%d reconciled=%d", initial.Generation, reconciled.Generation)
	}

	// Sustained mutation after the recovery: a watch that survived the deletion
	// but stopped tracking the tree would still converge on the deletion alone.
	for i := 0; i < 3; i++ {
		independentWrite(t, root, "src/a.ts", "export const a="+string(rune('3'+i))+";\n")
		waitEnd("a sustained edit")
	}
	committedAtLeast(reconciled.Generation + 1)
	// Drain whatever the debounce still has in flight before reading the tape.
	for draining := true; draining; {
		select {
		case <-sink.ends:
		case err := <-done:
			t.Fatalf("watch exited while settling: %v", err)
		case <-time.After(2 * time.Second):
			draining = false
		}
	}

	records := sink.CloneRecords()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("watch ended with %v, want context.Canceled", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("watch did not stop")
	}

	applied := NewConsumer()
	if err := applied.ApplyRecords(records); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src/doomed.ts")); !os.IsNotExist(err) {
		t.Fatalf("the deleted file is back: %v", err)
	}
	assertAppliedEqualsCold(t, applied, coldConsumerUnderContract(t, root, authoritative))
}
