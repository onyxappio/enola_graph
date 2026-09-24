package graphsession

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestIndependentConsecutiveRefusalsPreserveReusableParses(t *testing.T) {
	root, _ := retryFixture(t, 40)
	eng := testEngine(t, root)
	ctx := context.Background()
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(ctx, eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	initial, err := r.reconcile(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("src/pkg%02d/m%03d.ts", i%20, i)
		writeFile(t, root, path, fmt.Sprintf("export const x%d=1;\n", i))
		changed = append(changed, path)
	}
	for attempt := 0; attempt < 2; attempt++ {
		r.mu.Lock()
		input, reason := r.contentInputs(changed, &WorkCounters{})
		r.mu.Unlock()
		if input == nil || reason != "" {
			t.Fatalf("capture %d: %s", attempt, reason)
		}
		writeFile(t, root, changed[0], fmt.Sprintf("export const x0=%d;\n", attempt+2))
		offset := len(sink.CloneRecords())
		r.mu.Lock()
		_, work, err := r.transaction(ctx, input, true)
		r.mu.Unlock()
		if !errors.Is(err, ErrInputsChanged) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if len(sink.CloneRecords()) != offset {
			t.Fatalf("attempt %d published before refusing", attempt)
		}
		if r.state.Generation != initial.TargetGeneration {
			t.Fatal("refusal advanced generation")
		}
		if attempt == 1 && work.RetryParsesReused != 9 {
			t.Fatalf("second attempt must exercise nine reuses, got %d", work.RetryParsesReused)
		}
	}
	r.mu.Lock()
	out, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if out.OwnersPublished != 10 {
		t.Fatalf("scope changed: %d", out.OwnersPublished)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if work.RetryParsesReused != 9 {
		t.Fatalf("correct graph, but consecutive refusal discarded reusable parses: got %d want 9", work.RetryParsesReused)
	}
}
