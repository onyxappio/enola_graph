package graphsession

import (
	"context"
	"testing"
	"time"
)

// The watch loop used to open its fixed collection window before it knew
// anything about the batch waiting in the source, so a notification that
// carried no content change still cost a full window and, if a real burst
// landed inside that window, an attempt built from bytes that moved underneath
// it. These guard the pre-window discard: that it consumes a batch only when
// the same proof ApplyChanges uses says the batch changes nothing, that it
// consumes nothing at all otherwise, and that the source-level coverage gate is
// part of the question rather than something the queue answers on its own.

func probeFixture(t *testing.T) (string, *Resident, *ChangeQueue, int) {
	t.Helper()
	root, r, q, sink := residentFixture(t, map[string]string{
		"src/a.ts": "export function a(){return fetch('/a')}",
		"src/b.ts": "export function b(){return fetch('/b')}",
		"src/c.ts": "export function c(){return fetch('/c')}",
	}, Options{})
	return root, r, q, len(sink.CloneRecords())
}

func mustDiscard(t *testing.T, r *Resident, q *ChangeQueue, want bool) {
	t.Helper()
	got, err := r.discardUnchanged(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("discardUnchanged = %v, want %v", got, want)
	}
}

// An unchanged batch is answered without waiting and without publishing, and
// the watermark it acknowledges is the one the queue had reached, so the edit
// that follows it is still a continuous batch rather than a reconciliation.
func TestProbeDiscardsUnchangedBatchWithoutPublishing(t *testing.T) {
	root, r, q, published := probeFixture(t)
	generation := r.state.Generation
	q.Add("src/a.ts")
	q.Add("src/b.ts")
	seq := q.seq
	mustDiscard(t, r, q, true)

	if r.watermark != seq || q.drained != seq {
		t.Fatalf("watermark %d drained %d, want %d", r.watermark, q.drained, seq)
	}
	if r.state.Generation != generation {
		t.Fatalf("generation moved to %d", r.state.Generation)
	}
	if len(q.paths) != 0 {
		t.Fatalf("queue kept %d paths", len(q.paths))
	}
	if r.probeWork.EarlyProbeDiscards != 1 || r.probeWork.EarlyProbeReads != 2 {
		t.Fatalf("probe work %+v", r.probeWork)
	}

	// The next real edit still arrives as a continuous batch and still commits.
	writeFile(t, root, "src/a.ts", "export function a(){return fetch('/moved')}")
	q.Add("src/a.ts")
	mustDiscard(t, r, q, false)
	res, err := r.ApplyChanges(context.Background(), q.Drain())
	if err != nil {
		t.Fatal(err)
	}
	if res.Reconciled {
		t.Fatalf("edit after discard reconciled: %s", res.FallbackReason)
	}
	if res.TargetGeneration == generation {
		t.Fatal("edit after discard published nothing")
	}
	if res.Work.EarlyProbeDiscards != 1 {
		t.Fatalf("probe work not folded into the next result: %+v", res.Work)
	}
	if published == 0 {
		t.Fatal("fixture published nothing to compare against")
	}
}

// A batch whose content moved is left exactly where it was, so the ordinary
// timer and Drain still see every path the source had collected.
func TestProbeLeavesChangedBatchForTheWindow(t *testing.T) {
	root, r, q, _ := probeFixture(t)
	writeFile(t, root, "src/b.ts", "export function b(){return fetch('/moved')}")
	q.Add("src/a.ts")
	q.Add("src/b.ts")
	drained, seq := q.drained, q.seq

	mustDiscard(t, r, q, false)
	if q.drained != drained || q.seq != seq || len(q.paths) != 2 {
		t.Fatalf("declined probe consumed the queue: drained %d seq %d paths %d", q.drained, q.seq, len(q.paths))
	}
	if r.watermark != drained {
		t.Fatalf("declined probe moved the watermark to %d", r.watermark)
	}
	batch := q.Drain()
	if len(batch.Paths) != 2 {
		t.Fatalf("drain after declined probe saw %v", batch.Paths)
	}
	res, err := r.ApplyChanges(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reconciled {
		t.Fatalf("changed batch reconciled: %s", res.FallbackReason)
	}
}

// The probe stops reading at the first path whose bytes moved: proving that
// something changed does not require reading the rest of the batch, and the
// run that follows reads it once more rather than the probe reading it twice.
func TestProbeStopsAtFirstChangedPath(t *testing.T) {
	root, r, q, _ := probeFixture(t)
	writeFile(t, root, "src/a.ts", "export function a(){return fetch('/moved')}")
	q.Add("src/a.ts")
	q.Add("src/b.ts")
	q.Add("src/c.ts")
	mustDiscard(t, r, q, false)
	if r.probeWork.EarlyProbeReads != 1 {
		t.Fatalf("probe read %d paths, want 1", r.probeWork.EarlyProbeReads)
	}
	if r.probeWork.EarlyProbeDiscards != 0 {
		t.Fatalf("declined probe counted a discard: %+v", r.probeWork)
	}
	_ = root
}

// Any mutation between the proof and the commit invalidates the token, so the
// batch the probe proved is never the batch a later event has already changed.
func TestProbeCommitDeclinesAfterQueueMutation(t *testing.T) {
	_, r, q, _ := probeFixture(t)
	q.Add("src/a.ts")
	_, tok := q.Peek()

	q.Add("src/b.ts")
	if q.Discard(tok) {
		t.Fatal("discard accepted a token from before a later Add")
	}
	if q.drained != 0 || len(q.paths) != 2 {
		t.Fatalf("refused discard consumed the queue: drained %d paths %d", q.drained, len(q.paths))
	}

	_, tok = q.Peek()
	q.Lost("later loss")
	if q.Discard(tok) {
		t.Fatal("discard accepted a token from before a Lost")
	}
	if q.reason != "later loss" {
		t.Fatalf("refused discard cleared the reason: %q", q.reason)
	}
	_ = r
}

// Loss, overflow and close all reach the probe as a reason or as lost coverage,
// and none of them may be answered by discarding the batch that carries them.
func TestProbeDeclinesLostOverflowAndClose(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*ChangeQueue)
		reason  string
	}{
		{"lost", func(q *ChangeQueue) { q.Lost("watch coverage lost") }, "watch coverage lost"},
		{"overflow", func(q *ChangeQueue) {
			for i := 0; i < 64; i++ {
				q.Add(string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".ts")
			}
		}, "change queue overflow"},
		{"close", func(q *ChangeQueue) { q.Close() }, "change source closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r, q, _ := probeFixture(t)
			tc.arrange(q)
			drained := q.drained
			mustDiscard(t, r, q, false)
			if q.drained != drained {
				t.Fatalf("probe consumed a %s queue", tc.name)
			}
			batch, _ := q.Peek()
			if got := r.changeReason(&batch); got != tc.reason {
				t.Fatalf("reason %q, want %q", got, tc.reason)
			}
		})
	}
}

// The reason ladder the probe shares with ApplyChanges is the one that has to
// refuse these, and it has to refuse them through the same code so the two
// cannot drift into disagreeing about what is content-only.
func TestProbeDeclinesGapFailedAndUnknownPaths(t *testing.T) {
	t.Run("continuity gap", func(t *testing.T) {
		_, r, q, _ := probeFixture(t)
		q.Add("src/a.ts")
		q.Drain() // acknowledged by nobody: the watermark is now behind the queue.
		q.Add("src/b.ts")
		mustDiscard(t, r, q, false)
		batch, _ := q.Peek()
		if got := r.changeReason(&batch); got != "bootstrap or change-source continuity gap" {
			t.Fatalf("reason %q", got)
		}
	})
	t.Run("failed resident", func(t *testing.T) {
		_, r, q, _ := probeFixture(t)
		q.Add("src/a.ts")
		r.mu.Lock()
		r.failed = true
		r.mu.Unlock()
		mustDiscard(t, r, q, false)
		if q.drained != 0 {
			t.Fatal("probe consumed a batch while the resident was failed")
		}
		r.mu.Lock()
		r.failed = false
		r.mu.Unlock()
	})
	for _, p := range []string{"src/missing.ts", "package.json", "tsconfig.json", "README.md"} {
		t.Run(p, func(t *testing.T) {
			_, r, q, _ := probeFixture(t)
			q.Add(p)
			drained := q.drained
			mustDiscard(t, r, q, false)
			if q.drained != drained {
				t.Fatalf("probe consumed a batch carrying %s", p)
			}
		})
	}
}

// The queue reporting Covered is not the source reporting covered.
// FileChangeSource.Drain forces Covered false from a flag the queue knows
// nothing about, and a peek that skipped that gate would discard a batch the
// equivalent Drain would have refused. markUncertain raises the flag and
// releases the registration lock before its Lost reaches the queue, so there is
// a real interval in which the bare queue still looks clean.
func TestProbeHonoursSourceUncertaintyWhileQueueStillCovered(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export function a(){return 1}"})
	s := NewFileChangeSource(root, nil, 8)
	if err := s.ChangeQueue.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Add("src/a.ts")

	// Exactly markUncertain's interval: uncertain is set and registration is
	// released, and the Lost that would raise seq has not run yet.
	s.registration.Lock()
	s.uncertain = true
	s.registration.Unlock()

	bare, _ := s.ChangeQueue.Peek()
	if !bare.Covered {
		t.Fatal("fixture invalid: the bare queue should still report Covered")
	}
	gated, tok := s.Peek()
	if gated.Covered {
		t.Fatal("source peek reported covered while coverage was uncertain")
	}
	if gated.Reconcile != "symlink or incomplete watch coverage" {
		t.Fatalf("reconcile %q", gated.Reconcile)
	}
	if s.Discard(tok) {
		t.Fatal("discarded a batch whose source coverage was uncertain")
	}
	if s.ChangeQueue.drained != 0 || len(s.ChangeQueue.paths) != 1 {
		t.Fatal("refused discard consumed the queue")
	}
	if drained := s.Drain(); drained.Covered {
		t.Fatal("fixture invalid: the equivalent Drain should also refuse coverage")
	}
}

// Uncertainty that arrives after the proof was taken is the harder half: the
// flag lives outside the queue mutex, so nothing about epoch, seq or covered
// moves until the Lost that follows it, and only re-reading the flag under
// registration at commit time can tell the discard its answer is stale.
func TestProbeCommitDeclinesUncertaintyRaisedAfterPeek(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export function a(){return 1}"})
	s := NewFileChangeSource(root, nil, 8)
	if err := s.ChangeQueue.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Add("src/a.ts")
	batch, tok := s.Peek()
	if !batch.Covered {
		t.Fatal("fixture invalid: the peeked batch should be covered")
	}

	s.registration.Lock()
	s.uncertain = true
	s.registration.Unlock()

	if s.ChangeQueue.seq != tok.seq || s.ChangeQueue.covered != tok.covered {
		t.Fatal("fixture invalid: the queue should not have moved yet")
	}
	if s.Discard(tok) {
		t.Fatal("discarded a batch after coverage became uncertain")
	}
	if s.ChangeQueue.drained != 0 {
		t.Fatal("refused discard consumed the queue")
	}
}

// applied records what the loop drained, in order, with the time each batch
// reached apply, so a test can say both what was collected and whether the
// collection window it was collected in was the one the event opened.
type appliedBatch struct {
	batch ChangeBatch
	at    time.Time
}

func runWatchLoop(t *testing.T, r *Resident, q *ChangeQueue, delay time.Duration) (chan appliedBatch, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan appliedBatch, 8)
	go func() {
		_ = watchLoop(ctx, r, q, delay, func(b ChangeBatch) error {
			if _, err := r.ApplyChanges(ctx, b); err != nil {
				return err
			}
			out <- appliedBatch{batch: b, at: time.Now()}
			return nil
		})
	}()
	t.Cleanup(cancel)
	return out, cancel
}

func waitBatch(t *testing.T, out chan appliedBatch) appliedBatch {
	t.Helper()
	select {
	case b := <-out:
		return b
	case <-time.After(20 * time.Second):
		t.Fatal("no batch reached apply")
		return appliedBatch{}
	}
}

// A real edit still gets its whole collection window, measured from the moment
// the source became ready and not from the moment the probe finished, and the
// graph it produces is still the graph a cold run produces.
func TestWatchLoopKeepsCollectionWindowForRealEdits(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{
		"src/a.ts": "export function a(){return fetch('/a')}",
		"src/b.ts": "export function b(){return fetch('/b')}",
	}, Options{})
	delay := 400 * time.Millisecond
	out, _ := runWatchLoop(t, r, q, delay)

	writeFile(t, root, "src/a.ts", "export function a(){return fetch('/moved')}")
	start := time.Now()
	q.Add("src/a.ts")
	got := waitBatch(t, out)

	if elapsed := got.at.Sub(start); elapsed < delay {
		t.Fatalf("real edit applied after %s, window is %s", elapsed, delay)
	}
	if len(got.batch.Paths) != 1 || got.batch.Paths[0] != "src/a.ts" {
		t.Fatalf("collected %v", got.batch.Paths)
	}
	residentCold(t, root, r, sink)
}

// The case the trace showed: a save that changes nothing arrives first, and a
// real burst lands while the window that save opened is still running. With the
// batch discarded before any window opens, the burst opens its own window and
// nothing it wrote is left out of it.
func TestWatchLoopDiscardedDuplicateDoesNotSwallowLaterBurst(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{
		"src/a.ts": "export function a(){return fetch('/a')}",
		"src/b.ts": "export function b(){return fetch('/b')}",
		"src/c.ts": "export function c(){return fetch('/c')}",
	}, Options{})
	delay := 400 * time.Millisecond
	out, _ := runWatchLoop(t, r, q, delay)

	// The identical save. Nothing is written; the notification is real.
	q.Add("src/a.ts")

	// Wait for the loop to have answered it, which it does without a window.
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.mu.Lock()
		discards := r.probeWork.EarlyProbeDiscards
		r.mu.Unlock()
		if discards == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("identical save was never discarded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case b := <-out:
		t.Fatalf("identical save reached apply: %v", b.batch.Paths)
	default:
	}

	// The burst. Every one of these must survive into the batch that runs.
	for _, p := range []string{"src/a.ts", "src/b.ts", "src/c.ts"} {
		writeFile(t, root, p, "export function x(){return fetch('/burst')}")
		q.Add(p)
	}
	got := waitBatch(t, out)
	if len(got.batch.Paths) != 3 {
		t.Fatalf("burst collected %v, want all three saves", got.batch.Paths)
	}
	if got.batch.From != 1 {
		t.Fatalf("burst batch starts at %d, want the watermark the discard acknowledged", got.batch.From)
	}
	residentCold(t, root, r, sink)
}
