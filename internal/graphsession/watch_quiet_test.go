package graphsession

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchQuietResetsUntilDeadline(t *testing.T) {
	ready := make(chan struct{}, 1)
	start := time.Now()
	timer := time.AfterFunc(35*time.Millisecond, func() { ready <- struct{}{} })
	defer timer.Stop()
	if err := waitWatchWindow(context.Background(), ready, start.Add(time.Second), 70*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("did not reset quiet window: %v", elapsed)
	}
}

func TestWatchQuietContinuousSavesHitCap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}
	}()
	start := time.Now()
	if err := waitWatchWindow(ctx, ready, start.Add(80*time.Millisecond), time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("save burst postponed cycle past cap: %v", elapsed)
	}
}

func TestWatchQuietCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitWatchWindow(ctx, make(chan struct{}), time.Now().Add(time.Hour), time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

// Hide the optional no-op probe to exercise collection and apply boundaries
// independently of filesystem admission.
type quietQueueSource struct{ ChangeSource }

func TestWatchQuietRetainsSavesDuringApply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q := NewChangeQueue("quiet-test", 32)
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	q.Add("first.ts")
	stop := errors.New("test complete")
	calls := 0
	err := watchLoopWithQuiet(ctx, nil, quietQueueSource{q}, 100*time.Millisecond, 5*time.Millisecond, func(b ChangeBatch) error {
		calls++
		switch calls {
		case 1:
			if len(b.Paths) != 1 || b.Paths[0] != "first.ts" {
				t.Fatalf("first batch: %+v", b)
			}
			// These saves arrive while the first batch is being applied, after Drain.
			q.Add("second.ts")
			q.Add("third.ts")
			q.Add("second.ts")
		case 2:
			if len(b.Paths) != 2 || b.Paths[0] != "second.ts" || b.Paths[1] != "third.ts" {
				t.Fatalf("lost or repeated paths: %+v", b)
			}
			if b.From != 1 || b.Through != 4 {
				t.Fatalf("lost event coverage: %+v", b)
			}
			return stop
		default:
			t.Fatal("unexpected extra apply")
		}
		return nil
	})
	if !errors.Is(err, stop) || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
