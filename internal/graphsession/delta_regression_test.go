package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/extractors/openapiextractor"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestUnresolvedImportResolvesWhenFileAdded(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"a.ts": "import { missing } from './absent';\nexport function value() { return missing(); }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if err := os.WriteFile(filepath.Join(dir, "absent.ts"), []byte("export function missing() { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s2)
	cold := NewConsumer()
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestUnresolvedRootRelativeImportIsRealNoop(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"a.ts": "import { missing } from './absent';\nexport function value() { return missing(); }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if first.ParsedFiles < 1 {
		t.Fatal("initial must parse")
	}
	sink := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 || second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("root unresolved noop parsed=%d msgs=%d gen %d->%d fallbacks=%v",
			second.ParsedFiles, len(sink.CloneRecords()), first.TargetGeneration, second.TargetGeneration, second.Fallbacks)
	}
}

func TestExternalOnlyImportIsRealNoop(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "import express from 'express';\nexport const app = express();\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 || second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("external-only noop parsed=%d msgs=%d gen %d->%d fallbacks=%v",
			second.ParsedFiles, len(sink.CloneRecords()), first.TargetGeneration, second.TargetGeneration, second.Fallbacks)
	}
}

func TestMarkdownOnlyExtractorTrueNoop(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"README.md": "# Note\nHello.\n"})
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.Extractors = []string{"mdintent"}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(mdintent.New())
	state := filepath.Join(dir, ".enola", "state")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 || second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("mdintent-only noop parsed=%d msgs=%d gen %d->%d",
			second.ParsedFiles, len(sink.CloneRecords()), first.TargetGeneration, second.TargetGeneration)
	}
}

func openAPISpec(route string) string {
	return "openapi: 3.0.0\ninfo:\n  title: Test\n  version: 1.0.0\npaths:\n  /" + route + ":\n    get:\n      operationId: " + route + "\n      responses:\n        \"200\":\n          description: ok\n"
}

func TestOpenAPISpecEditAddEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":     "export const a = 1;\n",
		"openapi.yaml": openAPISpec("old"),
	})
	eng := multiEngine(t, dir, openapiextractor.New())
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)

	sNoop := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, dir, sNoop, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || len(sNoop.CloneRecords()) != 0 {
		t.Fatalf("openapi noop parsed=%d msgs=%d", noop.ParsedFiles, len(sNoop.CloneRecords()))
	}

	if err := os.WriteFile(filepath.Join(dir, "openapi.yaml"), []byte(openAPISpec("new")), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s2)

	if err := os.WriteFile(filepath.Join(dir, "another-openapi.yaml"), []byte(openAPISpec("added")), 0o644); err != nil {
		t.Fatal(err)
	}
	s3 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s3, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s3)

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)

	got := consFacts(cons)
	var routes []string
	for _, f := range got {
		if f.Kind == "route" {
			routes = append(routes, f.Name)
		}
	}
	joined := strings.Join(routes, ",")
	if !strings.Contains(joined, "/new") || !strings.Contains(joined, "/added") {
		t.Fatalf("missing updated OpenAPI routes in %v", routes)
	}
	if strings.Contains(joined, "/old") {
		t.Fatalf("retained stale OpenAPI route in %v", routes)
	}
}

func TestTSBodyEditDoesNotRerunMarkdown(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":  "export function a() { return 1; }\n",
		"README.md": "# Note\nHello.\n",
	})
	eng := mdintentEngine(t, dir, true)
	state := filepath.Join(dir, ".enola", "state")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/a.ts"), []byte("export function a() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("TS body parsed %d, want 1; fallbacks=%v", delta.ParsedFiles, delta.Fallbacks)
	}
	for _, fb := range delta.Fallbacks {
		if fb.Extractor == "mdintent" {
			t.Fatalf("TS body edit re-ran markdown: %v", delta.Fallbacks)
		}
	}
	if delta.TargetGeneration == first.TargetGeneration || len(sink.CloneRecords()) == 0 {
		t.Fatalf("body edit skipped publish gen %d→%d events=%d", first.TargetGeneration, delta.TargetGeneration, len(sink.CloneRecords()))
	}
}

func TestSameSizeRestoredMtimeStillDirty(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export const x = 'aa';\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	p := filepath.Join(dir, "src/a.ts")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	mtime := st.ModTime()
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("export const x = 'bb';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, time.Now(), mtime); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 || delta.TargetGeneration == first.TargetGeneration || len(sink.CloneRecords()) == 0 {
		t.Fatalf("same-size restored mtime treated as noop parsed=%d gen %d→%d events=%d",
			delta.ParsedFiles, first.TargetGeneration, delta.TargetGeneration, len(sink.CloneRecords()))
	}
}
