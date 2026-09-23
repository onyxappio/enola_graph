package graphsession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentAuthoritativeWatchDeletionDuringDelta(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;\n", "src/doomed.ts": "export const doomed=1;\n"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sink := &watchEndSink{ends: make(chan struct{}, 16)}
	done := make(chan error, 1)
	deleted := make(chan error, 1)
	var armed atomic.Bool
	state := t.TempDir()
	go func() {
		done <- Watch(ctx, testEngine(t, root), root, sink, Options{StateDir: state, AuthoritativeFiles: true, WatchEvery: time.Millisecond, OnBeforeParse: func(string) {
			if armed.CompareAndSwap(true, false) {
				deleted <- os.Remove(filepath.Join(root, "src/doomed.ts"))
			}
		}})
	}()
	waitEnd := func() {
		t.Helper()
		select {
		case <-sink.ends:
		case err := <-done:
			t.Fatalf("watch exited: %v", err)
		case <-ctx.Done():
			t.Fatal("watch did not converge before deadline")
		}
	}
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
				t.Fatal("state commit deadline")
			case <-time.After(time.Millisecond):
			}
		}
	}
	waitEnd() // baseline must contain the soon-to-be-deleted contribution
	initial := committedAtLeast(1)
	armed.Store(true)
	writeFile(t, root, "src/a.ts", "export const a=2;\n")
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatal(err)
		}
	case err := <-done:
		t.Fatalf("watch exited before deletion: %v", err)
	case <-ctx.Done():
		t.Fatal("deletion hook not reached")
	}
	waitEnd()
	committed := committedAtLeast(initial.Generation + 1)
	if committed.Generation != initial.Generation+1 {
		t.Fatalf("aborted attempt advanced generation: initial=%d final=%d", initial.Generation, committed.Generation)
	}
	writeFile(t, root, "src/a.ts", "export const a=3;\n")
	waitEnd()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("watch exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch failed to stop")
	}
	applied := NewConsumer()
	if err := applied.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, applied, coldConsumer(t, testEngine(t, root), root))
}
