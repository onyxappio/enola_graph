package graphsession

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestExternalImportLocalSurfaceBodyEditKeepsSideReadersCached(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "import { randomBytes } from 'node:crypto';\nexport function pick(): string { return randomBytes(1).toString(); }\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const other = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)
	writeFile(t, root, "leaf.ts", "import { randomBytes } from 'node:crypto';\nexport function pick(): string { return randomBytes(2).toString(); }\n")
	first, _, _ := bodyScopeDelta(t, eng, root, opts, cons)
	if first.ParsedFiles != 1 {
		t.Fatalf("body edit parsed %d files, want only changed leaf", first.ParsedFiles)
	}
	writeFile(t, root, "other.ts", "export const other = 2;\n")
	second, _, _ := bodyScopeDelta(t, eng, root, opts, cons)
	if second.ParsedFiles != 1 {
		t.Fatalf("unrelated edit parsed %d files: side-read proof was not refreshed", second.ParsedFiles)
	}
}

func TestExternalImportSurfaceChangesRebindConsumers(t *testing.T) {
	const prefix = "import { randomBytes } from 'node:crypto';\n"
	const funcs = "function round() { return randomBytes(1); }\nfunction ceil() { return randomBytes(2); }\nround(); ceil();\n"
	for _, tc := range []struct{ name, before, after, barrel string }{
		{"default switches", funcs + "export default round;\n", funcs + "export default ceil;\n", "export { default as pick } from './origin';\n"},
		{"export removed", funcs + "export { round, ceil };\n", funcs + "export { ceil };\n", "export { round as pick } from './origin';\n"},
		{"repository binding switches", "import { value } from './left';\nexport { value };\n", "import { value } from './right';\nexport { value };\n", "export { value as pick } from './origin';\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"src/origin.ts": prefix + tc.before,
				"src/barrel.ts": tc.barrel,
				"src/use.ts":    "import { pick } from './barrel';\nexport function caller() { return pick(); }\n",
				"src/left.ts":   "export function value() { return 1; }\n",
				"src/right.ts":  "export function value() { return 2; }\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			cons := bodyScopeStart(t, eng, root, opts)
			for _, source := range []string{tc.after, tc.before} {
				writeFile(t, root, "src/origin.ts", prefix+source)
				bodyScopeDelta(t, eng, root, opts, cons)
			}
		})
	}
}

func TestExternalImportResidentProofRollsBackWithFailedEnd(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "import { randomBytes } from 'node:crypto';\nexport function pick() {\n  return randomBytes(1);\n}\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}

	sink := &endFailingSink{err: errors.New("broker unavailable")}
	r, err := OpenSession(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "initial"}); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	before := residentSideReads(t, r, "use.ts")
	if len(before) == 0 {
		t.Fatal("fixture records no side read for use.ts")
	}

	sink.armed.Store(true)
	writeFile(t, root, "leaf.ts", "import { randomBytes } from 'node:crypto';\nexport function pick() {\n  return randomBytes(2);\n}\n")
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "interrupted"}); err == nil {
		t.Fatal("interrupted generation succeeded")
	}
	if got := residentSideReads(t, r, "use.ts"); !maps.Equal(got, before) {
		t.Fatalf("interrupted generation rewrote the resident side-read proof: %v -> %v", before, got)
	}
	if got := committedSideReads(t, state, "use.ts"); !maps.Equal(got, before) {
		t.Fatalf("interrupted generation rewrote the committed side-read proof: %v -> %v", before, got)
	}

	// Recovery replays and reconciles in the same session, and only then is the
	// proof allowed to move.
	sink.armed.Store(false)
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "recovery"}); err != nil {
		t.Fatal(err)
	}
	if got := residentSideReads(t, r, "use.ts"); maps.Equal(got, before) {
		t.Fatalf("recovered generation did not record the proven side read: still %v", got)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	// An unrelated edit afterwards must stand on the recovered state, not on the
	// one the lost End would have written.
	writeFile(t, root, "other.ts", "export const o = 2;\n")
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	cold := opts
	cold.StateDir = t.TempDir()
	cold.ForceInitial = true
	if _, err := Run(context.Background(), eng, root, coldSink, cold); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
}
