package graphstream

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestEnvelopeMetadataMatchesIndependentProbes(t *testing.T) {
	for _, payload := range []string{
		`{"type":"begin_replace","run_id":"r"}`,
		`{"type":"batch","phase":"local","run_id":"r"}`,
		`{"type":"batch","phase":"scope","run_id":"r"}`,
		`{"type":"batch","phase":"resolved","run_id":"r"}`,
		`{"type":"batch","phase":"unknown"}`,
		`{"type":"end_replace","run_id":"r"}`,
		`{"type":"end_replace"}`,
		`{"type":"end_replace","run_id":123}`,
		`{"type":"end_replace","phase":123,"run_id":"r"}`,
		`{"type":"batch","phase":123}`,
		`{"type":"batch","run_id":123}`,
		`{"type":"batch","type":"end_replace","run_id":"r"}`,
		`{"type":"end_replace","type":"batch","phase":"local"}`,
		`{"type":"batch","nodes":[{"type":"end_replace","run_id":"fake"}]}`,
		`{"type":"end_replace","run_id":"r"} trailing`,
		`null`, `[]`, `plain bytes`,
	} {
		t.Run(payload, func(t *testing.T) {
			p := []byte(payload)
			got := inspectPayload(p)
			run := endRunID(p)
			want := envelopeMetadata{kind: classifyPayloadFallback(p), isEnd: run != "" || endType(p), endRun: run}
			if got != want {
				t.Fatalf("metadata %+v want %+v", got, want)
			}
		})
	}
}

// Compare every completion subset against the protocol predecessor rules,
// including out-of-order acknowledgments and jobs from a later epoch.
func TestOutstandingGatesAllCompletionOrders(t *testing.T) {
	kinds := []jobKind{kindBegin, kindLocal, kindScope, kindResolved, kindOther, kindEnd, kindBegin, kindScope, kindResolved, kindEnd}
	for mask := 0; mask < (1 << len(kinds)); mask++ {
		a := &asyncPub{unfinished: map[int64]jobKind{}}
		for i, k := range kinds {
			if mask&(1<<i) == 0 {
				a.unfinished[int64(i+1)] = k
			}
		}
		for i, k := range kinds {
			want := true
			if k != kindBegin {
				for j := 0; j < i; j++ {
					if mask&(1<<j) == 0 && (k == kindEnd || kinds[j] == kindBegin || (k == kindResolved && kinds[j] == kindScope)) {
						want = false
					}
				}
			}
			if got := a.predsOKLocked(asyncJob{seq: int64(i + 1), kind: k}); got != want {
				t.Fatalf("mask=%b seq=%d got=%v want=%v", mask, i+1, got, want)
			}
		}
	}
}

func TestAsyncMultipleEpochsRetireGateHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sink := &MemorySink{}
	p := &Publisher{Sink: sink}
	p.EnableAsync(4, 1<<20)
	defer p.CloseAsync()
	for epoch := 0; epoch < 3; epoch++ {
		run := fmt.Sprint(epoch)
		for seq, payload := range [][]byte{beginPayload(run), batchPayload(run, 1, PhaseResolved, nil, nil), endPayload(run)} {
			if err := p.Publish(ctx, fmt.Sprintf("%s:%d", run, seq), payload); err != nil {
				t.Fatal(err)
			}
		}
		if err := p.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		p.async.mu.Lock()
		outstanding := len(p.async.unfinished)
		p.async.mu.Unlock()
		if outstanding != 0 {
			t.Fatalf("retained %d acknowledged jobs", outstanding)
		}
	}
	if len(sink.CloneRecords()) != 9 {
		t.Fatal("lost epoch payloads")
	}
}
