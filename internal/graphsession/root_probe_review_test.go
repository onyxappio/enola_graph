package graphsession

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// Inject at the proof/commit boundary without production hooks. This models
// a producer winning the race after all file reads have completed.
type rootProbeInterleave struct {
	peekableSource
	beforeCommit func()
}

func (s rootProbeInterleave) Discard(tok changeToken) bool {
	s.beforeCommit()
	return s.peekableSource.Discard(tok)
}

func TestRootProbeNewSaveBetweenProofAndCommit(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{
		"src/a.ts": "export function a(){return 1}",
		"src/b.ts": "import {a} from './a'; export function b(){return a()}",
	}, Options{})
	q.Add("src/a.ts")
	watermark, drained := r.watermark, q.drained
	inputs, generation, published := r.inputs, r.state.Generation, len(sink.CloneRecords())
	s := rootProbeInterleave{peekableSource: q, beforeCommit: func() {
		independentWrite(t, root, "src/a.ts", "export function a(){return fetch('/changed')}")
		q.Add("src/a.ts") // same path: cardinality alone cannot detect this
	}}
	if ok, err := r.discardUnchanged(context.Background(), s); err != nil || ok {
		t.Fatalf("racing save was discarded: ok=%v err=%v", ok, err)
	}
	if r.watermark != watermark || q.drained != drained || r.inputs != inputs || r.state.Generation != generation || len(sink.CloneRecords()) != published {
		t.Fatal("rejected proof altered resident state, consumed queue or published")
	}
	res := residentApply(t, r, q)
	if res.TargetGeneration == generation || res.Reconciled {
		t.Fatalf("pending save not handled as continuous edit: %+v", res)
	}
	residentCold(t, root, r, sink)
}

func TestRootProbeSourceUncertaintyBetweenProofAndCommit(t *testing.T) {
	_, r, q, sink := residentFixture(t, map[string]string{"src/a.ts": "export const a=1"}, Options{})
	source := &FileChangeSource{ChangeQueue: q}
	q.Add("src/a.ts")
	before, _ := q.Peek()
	watermark, published := r.watermark, len(sink.CloneRecords())
	s := rootProbeInterleave{peekableSource: source, beforeCommit: func() {
		// Raw queue remains covered; the source gate must independently veto.
		source.registration.Lock()
		source.uncertain = true
		source.registration.Unlock()
	}}
	if ok, err := r.discardUnchanged(context.Background(), s); err != nil || ok {
		t.Fatalf("source uncertainty bypassed: ok=%v err=%v", ok, err)
	}
	after, _ := q.Peek()
	if !reflect.DeepEqual(before, after) || r.watermark != watermark || len(sink.CloneRecords()) != published {
		t.Fatal("source uncertainty rejection consumed work")
	}
}

func TestRootProbeSilentDuplicateThenColdEqualEdit(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{"src/a.ts": "export function a(){return 1}"}, Options{})
	inputs, generation, published := r.inputs, r.state.Generation, len(sink.CloneRecords())
	q.Add("src/a.ts")
	if ok, err := r.discardUnchanged(context.Background(), q); err != nil || !ok {
		t.Fatalf("duplicate not skipped: %v %v", ok, err)
	}
	if r.inputs != inputs || r.state.Generation != generation || len(sink.CloneRecords()) != published {
		t.Fatal("duplicate mutated graph state or published")
	}
	independentWrite(t, root, "src/a.ts", "export function a(){return fetch('/new')}")
	q.Add("src/a.ts")
	if ok, err := r.discardUnchanged(context.Background(), q); err != nil || ok {
		t.Fatalf("edit skipped: %v %v", ok, err)
	}
	residentApply(t, r, q)
	residentCold(t, root, r, sink)
}

func TestRootProbeBusyRegistrationDeclinesPromptly(t *testing.T) {
	_, r, q, _ := residentFixture(t, map[string]string{"src/a.ts": "export const a=1"}, Options{})
	source := &FileChangeSource{ChangeQueue: q}
	q.Add("src/a.ts")
	watermark, drained := r.watermark, q.drained
	source.registration.Lock()
	done := make(chan bool, 1)
	go func() {
		ok, err := r.discardUnchanged(context.Background(), source)
		done <- !ok && err == nil
	}()
	select {
	case declined := <-done:
		source.registration.Unlock()
		if !declined {
			t.Fatal("busy registration did not decline")
		}
	case <-time.After(time.Second):
		source.registration.Unlock()
		<-done
		t.Fatal("optional probe blocked behind registration")
	}
	if r.watermark != watermark || q.drained != drained {
		t.Fatal("busy registration consumed work")
	}
}

type rootSlowPeek struct {
	*ChangeQueue
	entered chan struct{}
	release chan struct{}
}

func (s *rootSlowPeek) Peek() (ChangeBatch, changeToken) {
	close(s.entered)
	<-s.release
	return s.ChangeQueue.Peek()
}

func TestRootProbeElapsedWorkDoesNotRestartWindow(t *testing.T) {
	root, r, q, _ := residentFixture(t, map[string]string{"src/a.ts": "export const a=1"}, Options{})
	independentWrite(t, root, "src/a.ts", "export const a=2")
	q.Add("src/a.ts")
	s := &rootSlowPeek{ChangeQueue: q, entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	applied := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- watchLoop(ctx, r, s, 500*time.Millisecond, func(ChangeBatch) error {
			applied <- struct{}{}
			cancel()
			return nil
		})
	}()
	select {
	case <-s.entered:
	case <-time.After(3 * time.Second):
		close(s.release)
		t.Fatal("probe never entered")
	}
	// Spend more than the entire window inside the probe. Releasing it must
	// not start a new full collection window for an already pending edit.
	time.Sleep(600 * time.Millisecond)
	close(s.release)
	select {
	case <-applied:
	case <-time.After(300 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("declining probe restarted collection window")
	}
	<-done
}
