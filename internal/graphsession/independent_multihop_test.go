package graphsession

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestIndependentConsecutiveRefusalsPreserveMultiHopParses(t *testing.T) {
	root, _ := retryFixture(t, 40)
	writeFile(t, root, "src/consumer.ts", "import {x0} from './pkg00/m000'; export const used=x0;\n")
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
	for i := 0; i < 2; i++ {
		path := fmt.Sprintf("src/pkg%02d/m%03d.ts", i%20, i)
		writeFile(t, root, path, fmt.Sprintf("export const renamed%d=1;\n", i))
		changed = append(changed, path)
	}
	for attempt := 0; attempt < 2; attempt++ {
		r.mu.Lock()
		input, reason := r.contentInputs(changed, &WorkCounters{})
		r.mu.Unlock()
		if input == nil || reason != "" {
			t.Fatalf("capture %d: %s", attempt, reason)
		}
		writeFile(t, root, changed[1], fmt.Sprintf("export const renamed1=%d;\n", attempt+2))
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
		if attempt == 1 {
			if work.RetryParsesReused < 2 {
				t.Fatalf("need multiple reuse waves, got %d", work.RetryParsesReused)
			}
			for _, p := range []string{changed[0], "src/consumer.ts"} {
				if r.retryRecords[p] == nil {
					t.Fatalf("lost accepted multi-hop record %s", p)
				}
			}
		}
	}
	r.mu.Lock()
	out, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if out.OwnersPublished != 3 {
		t.Fatalf("scope changed: %d", out.OwnersPublished)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if work.RetryParsesReused < 2 {
		t.Fatalf("correct graph, but consecutive refusal discarded reusable parses: got %d want at least 2", work.RetryParsesReused)
	}
}
