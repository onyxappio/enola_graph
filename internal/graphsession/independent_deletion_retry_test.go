package graphsession

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentDeletedCapturedSourceIsRetryable(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1;\n", "b.ts": "export const b=2;\n"})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	initial, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "a.ts", "export const a=3;\n")
	removed := false
	opts.OnBeforeParse = func(string) {
		if !removed {
			removed = true
			if e := os.Remove(filepath.Join(root, "b.ts")); e != nil {
				t.Fatal(e)
			}
		}
	}
	sink := &graphstream.MemorySink{}
	_, err = Run(context.Background(), eng, root, sink, opts)
	if !removed {
		t.Fatal("mutation hook did not run")
	}
	for _, r := range sink.CloneRecords() {
		var h struct {
			Type string `json:"type"`
		}
		if e := json.Unmarshal(r.Payload, &h); e != nil {
			t.Fatal(e)
		}
		if h.Type == "end_replace" {
			t.Fatal("failed attempt published successful End")
		}
	}
	committed, e := loadCommittedState(state)
	if e != nil {
		t.Fatal(e)
	}
	if committed.Generation != initial.TargetGeneration {
		t.Fatalf("failed attempt advanced generation: %d", committed.Generation)
	}
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("deleted captured source must trigger existing watch reconcile retry, got: %v", err)
	}
}
