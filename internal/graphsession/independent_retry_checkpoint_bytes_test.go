package graphsession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestIndependentRetryRejectsSameMetadataCorruptCheckpoint(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;\n"})
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	initial, err := r.reconcile(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "src/a.ts", "export const a=2;\n")
	r.mu.Lock()
	input, reason := r.contentInputs([]string{"src/a.ts"}, &WorkCounters{})
	r.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture: %s", reason)
	}
	writeFile(t, root, "src/a.ts", "export const a=3;\n")
	r.mu.Lock()
	_, _, err = r.transaction(ctx, input, true)
	r.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("want refused attempt, got %v", err)
	}

	path := filepath.Join(opts.StateDir, "state.json")
	prior, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), prior...)
	corrupt[0] = '!'
	if err := os.WriteFile(path, corrupt, info.Mode()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("fixture must preserve checkpoint metadata: %v", err)
	}
	offset := len(sink.CloneRecords())
	_, err = r.reconcile(ctx, false)
	if err == nil || !strings.Contains(err.Error(), "graphsession state") {
		t.Fatalf("corrupt checkpoint was not rejected: %v", err)
	}
	_, _, ends, err := DecodeRun(sink.CloneRecords()[offset:])
	if err != nil || len(ends) != 0 {
		t.Fatalf("corrupt checkpoint permitted completion: ends=%d err=%v", len(ends), err)
	}
	if r.state.Generation != initial.TargetGeneration {
		t.Fatal("failed retry advanced the committed generation")
	}
	if err := os.WriteFile(path, prior, info.Mode()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
