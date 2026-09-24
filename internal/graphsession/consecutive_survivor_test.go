package graphsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// A chain of refusals is the case the offer has to survive. The first refusal
// parses a tree that mostly did not move and hands it forward; the second
// re-proves what it may and parses nothing new, so if the offer it hands on is
// built only from its own parses it hands on nothing. These guard that the
// records a refusal actually took and re-proved travel with it, and that every
// guard they had to pass still applies to them on the way.

// refuseOnce captures inputs over changed, moves trigger underneath the capture
// so the attempt refuses, and returns what the refused attempt reused.
func refuseOnce(t *testing.T, ctx context.Context, r *Resident, sink *graphstream.MemorySink, changed []string, root, trigger, body string) WorkCounters {
	t.Helper()
	r.mu.Lock()
	input, reason := r.contentInputs(changed, &WorkCounters{})
	r.mu.Unlock()
	if input == nil || reason != "" {
		t.Fatalf("capture: %s", reason)
	}
	writeFile(t, root, trigger, body)
	offset := len(sink.CloneRecords())
	r.mu.Lock()
	_, work, err := r.transaction(ctx, input, true)
	r.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("expected refusal, got %v", err)
	}
	if len(sink.CloneRecords()) != offset {
		t.Fatal("refusal published events")
	}
	return work
}

// survivorFixture opens a session over a small TS tree, commits it cold, then
// dirties ten files. The first of them is the one later attempts move.
func survivorFixture(t *testing.T) (context.Context, string, *Resident, *graphstream.MemorySink, []string, func() *Consumer) {
	t.Helper()
	root, _ := retryFixture(t, 40)
	eng := testEngine(t, root)
	ctx := context.Background()
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(ctx, eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	if _, err := r.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	changed := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("src/pkg%02d/m%03d.ts", i%20, i)
		writeFile(t, root, path, fmt.Sprintf("export const x%d=1;\n", i))
		changed = append(changed, path)
	}
	return ctx, root, r, sink, changed, func() *Consumer { return coldConsumer(t, eng, root) }
}

// Three refusals in a row, each one re-proving the nine files that did not
// move. The third has no fresh parse of its own to offer, so it is the attempt
// that fails if the offer is rebuilt from parses alone.
func TestConsecutiveRefusalChainKeepsOfferingReprovenRecords(t *testing.T) {
	ctx, root, r, sink, changed, cold := survivorFixture(t)
	for attempt := 0; attempt < 3; attempt++ {
		work := refuseOnce(t, ctx, r, sink, changed, root, changed[0], fmt.Sprintf("export const x0=%d;\n", attempt+2))
		want := 0
		if attempt > 0 {
			want = 9
		}
		if work.RetryParsesReused != want {
			t.Fatalf("attempt %d: reused %d want %d", attempt, work.RetryParsesReused, want)
		}
	}
	r.mu.Lock()
	out, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if work.RetryParsesReused != 9 {
		t.Fatalf("commit after three refusals reused %d want 9", work.RetryParsesReused)
	}
	if out.OwnersPublished != 10 {
		t.Fatalf("scope changed: %d", out.OwnersPublished)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, cold())
}

// Membership is part of the identity a record was parsed under. A file that
// appears between two refusals changes the universe every one of them was
// resolved against, so the whole offer is withdrawn rather than some of it.
func TestConsecutiveRefusalOfferWithdrawnOnMembershipChange(t *testing.T) {
	ctx, root, r, sink, changed, cold := survivorFixture(t)
	for attempt := 0; attempt < 2; attempt++ {
		refuseOnce(t, ctx, r, sink, changed, root, changed[0], fmt.Sprintf("export const x0=%d;\n", attempt+2))
	}
	writeFile(t, root, "src/pkg00/appeared.ts", "export const appeared=1;\n")
	r.mu.Lock()
	_, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if work.RetryParsesReused != 0 {
		t.Fatalf("membership change must withdraw the whole offer, reused %d", work.RetryParsesReused)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, cold())
}

// A carried record is still a claim about bytes. One of the nine moving
// between the last refusal and the retry is declined on its own, and the eight
// that did not move are still taken.
func TestConsecutiveRefusalCarriedRecordStillProvenPerFile(t *testing.T) {
	ctx, root, r, sink, changed, cold := survivorFixture(t)
	for attempt := 0; attempt < 2; attempt++ {
		refuseOnce(t, ctx, r, sink, changed, root, changed[0], fmt.Sprintf("export const x0=%d;\n", attempt+2))
	}
	writeFile(t, root, changed[1], "export const x1=99;\n")
	r.mu.Lock()
	_, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if work.RetryParsesReused != 8 {
		t.Fatalf("a carried record whose bytes moved must be declined alone, reused %d want 8", work.RetryParsesReused)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, cold())
}

// A carried record must not outlive the file. Deleting one between refusals
// changes membership too, so this asserts the reachable property: the retry
// commits the graph a cold run produces and offers nothing for a path that is
// gone.
func TestConsecutiveRefusalCarriedRecordDroppedWhenFileRemoved(t *testing.T) {
	ctx, root, r, sink, changed, cold := survivorFixture(t)
	for attempt := 0; attempt < 2; attempt++ {
		refuseOnce(t, ctx, r, sink, changed, root, changed[0], fmt.Sprintf("export const x0=%d;\n", attempt+2))
	}
	if err := os.Remove(filepath.Join(root, changed[1])); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	_, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if work.RetryParsesReused != 0 {
		t.Fatalf("removal changes membership, so the offer is withdrawn whole: reused %d", work.RetryParsesReused)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, cold())
}
