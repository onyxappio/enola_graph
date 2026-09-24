package graphsession

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestFreshNoopRetainsResultFacts(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/io.ts":   "export function get(){return fetch('/x')}\n",
		"src/call.ts": "import {get} from './io'; export function run(){return get()}\n",
	})
	opts := Options{StateDir: filepath.Join(root, ".enola", "contract")}
	sink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), testEngine(t, root), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Facts) == 0 {
		t.Fatal("fixture has no facts")
	}
	before, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	events := len(sink.CloneRecords())
	noop, err := Run(context.Background(), testEngine(t, root), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || noop.TargetGeneration != initial.TargetGeneration || len(sink.CloneRecords()) != events {
		t.Fatal("unchanged input was not a silent no-op")
	}
	after, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("no-op changed state bytes")
	}
	if len(noop.Facts) == 0 || factsFingerprint(noop.Facts) != factsFingerprint(initial.Facts) {
		t.Fatalf("no-op result lost or changed facts: initial=%d no-op=%d", len(initial.Facts), len(noop.Facts))
	}
}

func TestReconciledNoopRetainsResidentSnapshot(t *testing.T) {
	_, r, _, sink := residentFixture(t, map[string]string{"src/io.ts": "export function get(){return fetch('/x')}"}, Options{})
	before := r.Snapshot()
	if len(before) == 0 {
		t.Fatal("fixture has no snapshot")
	}
	events := len(sink.CloneRecords())
	res, err := r.reconcile(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Facts != nil || res.ParsedFiles != 0 || res.BaseGeneration != res.TargetGeneration || len(sink.CloneRecords()) != events {
		t.Fatal("summary reconcile was not a silent no-op")
	}
	after := r.Snapshot()
	if len(after) == 0 || factsFingerprint(before) != factsFingerprint(after) {
		t.Fatalf("no-op summary erased resident snapshot: before=%d after=%d", len(before), len(after))
	}
}
