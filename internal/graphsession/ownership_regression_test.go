package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/plugin"
)

type stubExtractor struct {
	name   string
	detect bool
	suffix string
	fact   func(rel string) []facts.Fact
}

func (s stubExtractor) Name() string                { return s.name }
func (s stubExtractor) Detect(string) (bool, error) { return s.detect, nil }
func (s stubExtractor) OwnsFactFile(rel string) bool {
	return strings.HasSuffix(rel, s.suffix)
}
func (s stubExtractor) Extract(_ context.Context, _ string, files []string) ([]facts.Fact, error) {
	var out []facts.Fact
	for _, f := range files {
		if s.suffix != "" && !strings.HasSuffix(f, s.suffix) {
			continue
		}
		if s.fact != nil {
			out = append(out, s.fact(f)...)
		}
	}
	return out, nil
}

type fileOwnerStub struct{ stubExtractor }

func (s fileOwnerStub) OwnsFile(rel string) bool { return strings.HasSuffix(rel, s.suffix) }

type noOwnerExtractor struct {
	name string
	emit []facts.Fact
}

func (n noOwnerExtractor) Name() string                { return n.name }
func (n noOwnerExtractor) Detect(string) (bool, error) { return true, nil }
func (n noOwnerExtractor) Extract(context.Context, string, []string) ([]facts.Fact, error) {
	return n.emit, nil
}

func multiEngine(t *testing.T, dir string, extra ...plugin.Extractor) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	names := []string{"typescript"}
	for _, e := range extra {
		names = append(names, e.Name())
	}
	cfg.Extractors = names
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	for _, e := range extra {
		eng.RegisterExtractor(e)
	}
	return eng
}

func emptyResolvedCount(t *testing.T, sink *graphstream.MemorySink) int {
	t.Helper()
	_, batches, _, err := DecodeRun(sink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, b := range batches {
		if b.Phase == graphstream.PhaseResolved && len(b.Nodes) == 0 && len(b.Edges) == 0 && len(b.Owners) == 0 {
			n++
		}
	}
	return n
}

func TestOwnedFilesUsesFactOwnerNotWholeInventory(t *testing.T) {
	files := []string{"src/a.ts", "docs/a.md", "img/a.png"}
	got := ownedFiles(mdintent.New(), files)
	if len(got) != 1 || got[0] != "docs/a.md" {
		t.Fatalf("mdintent owned %v, want [docs/a.md]", got)
	}
	got = ownedFiles(noOwnerExtractor{name: "bare"}, files)
	if len(got) != 0 {
		t.Fatalf("extractor without ownership claimed %v", got)
	}
}

func TestHTMLDoesNotForceNoopReparse(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { missing } from './absent';\nexport function value() { return missing(); }\n",
		"src/page.html": "<p>not angular</p>",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	sink1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, sink1, Options{StateDir: state, ContextID: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ParsedFiles < 1 {
		t.Fatal("initial must parse the typescript file")
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink1.CloneRecords()); err != nil {
		t.Fatalf("html fixture End/scope rejected: %v", err)
	}
	if emptyResolvedCount(t, sink1) != 0 {
		t.Fatalf("initial emitted %d empty resolved batches", emptyResolvedCount(t, sink1))
	}
	sink2 := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink2, Options{StateDir: state, ContextID: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 {
		t.Fatalf("html phantom noop parsed %d, want 0; fallbacks=%v", second.ParsedFiles, second.Fallbacks)
	}
	if second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("noop advanced generation %d -> %d", first.TargetGeneration, second.TargetGeneration)
	}
	if len(sink2.CloneRecords()) != 0 {
		t.Fatalf("noop published %d messages", len(sink2.CloneRecords()))
	}
}

func TestUnresolvedOnlyImportIsRealNoop(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "import { missing } from './absent';\nexport function value() { return missing(); }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state, ContextID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state, ContextID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 || second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("unresolved-only noop parsed=%d msgs=%d gen %d->%d fallbacks=%v",
			second.ParsedFiles, len(sink.CloneRecords()), first.TargetGeneration, second.TargetGeneration, second.Fallbacks)
	}
}

func TestMultiExtractorCacheReuseDoesNotDuplicateTSFacts(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":  "export const a = 1;\n",
		"docs/n.md": "# Note\nA document.\n",
		"img.png":   "x",
	})
	docs := stubExtractor{
		name:   "docs",
		detect: true,
		suffix: ".md",
		fact: func(rel string) []facts.Fact {
			return []facts.Fact{{Kind: facts.KindSymbol, Name: "doc:" + rel, File: rel}}
		},
	}
	bare := noOwnerExtractor{
		name: "bare",
		emit: []facts.Fact{{Kind: facts.KindSymbol, Name: "bare.mark", File: "docs/n.md"}},
	}
	eng := multiEngine(t, dir, docs, bare, mdintent.New())
	state := filepath.Join(dir, ".enola", "graphstate")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state, ContextID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if _, ok := factByName(first.Facts, "src.a"); !ok {
		t.Fatal("missing typescript symbol")
	}
	if _, ok := factByName(first.Facts, "doc:docs/n.md"); !ok {
		t.Fatal("missing docs extractor symbol")
	}
	tsCount := 0
	for _, f := range first.Facts {
		if f.Name == "src.a" {
			tsCount++
		}
	}
	s2 := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state, ContextID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 {
		t.Fatalf("multi-extractor noop parsed %d; fallbacks=%v", second.ParsedFiles, second.Fallbacks)
	}
	if len(s2.CloneRecords()) != 0 {
		t.Fatalf("multi-extractor noop published %d messages", len(s2.CloneRecords()))
	}
	if second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("noop advanced generation")
	}
	dup := 0
	for _, f := range second.Facts {
		if f.Name == "src.a" {
			dup++
		}
	}
	if dup != 0 && dup != tsCount {
		t.Fatalf("cached typescript facts duplicated: initial=%d noop-result=%d", tsCount, dup)
	}

	if err := os.WriteFile(filepath.Join(dir, "src/a.ts"), []byte("export const a = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s3 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s3, Options{StateDir: state, ContextID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("body edit parsed %d, want 1; fallbacks=%v", delta.ParsedFiles, delta.Fallbacks)
	}
	applyRun(t, cons, s3)
	if emptyResolvedCount(t, s3) != 0 {
		t.Fatalf("body edit emitted empty resolved batches")
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ContextID: "c", ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestNoOwnerExtractorDoesNotClaimInventoryOnBodyEdit(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export const a = 1;\n",
		"src/b.ts": "export const b = 1;\n",
		"x.png":    "img",
	})
	bare := noOwnerExtractor{
		name: "bare",
		emit: []facts.Fact{{Kind: facts.KindSymbol, Name: "bare.mark", File: "src/a.ts"}},
	}
	eng := multiEngine(t, dir, bare)
	state := filepath.Join(dir, ".enola", "s")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("export const b = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("parsed %d, want 1", delta.ParsedFiles)
	}
	begins, _, _, err := DecodeRun(s2.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(begins) != 1 {
		t.Fatalf("begins=%d", len(begins))
	}
	for _, o := range begins[0].OwnerScope {
		if o.ID == "x.png" || strings.HasSuffix(o.ID, ".png") {
			t.Fatalf("body edit scope claimed unowned inventory file %s", o.ID)
		}
	}
	applyRun(t, cons, s2)
	if _, ok := factByName(consFacts(cons), "bare.mark"); !ok {
		t.Fatal("unowned extractor contribution was dropped by a typescript body edit")
	}
}

func TestFullVsDeltaBodyDeleteRenameConfig(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/leaf.ts": "export function leaf() { return 1; }\n",
		"src/use.ts":  "import { leaf } from './leaf'; export function use() { return leaf(); }\n",
		"docs/n.md":   "# Doc\nHello.\n",
	})
	docs := stubExtractor{
		name:   "docs",
		detect: true,
		suffix: ".md",
		fact: func(rel string) []facts.Fact {
			return []facts.Fact{{Kind: facts.KindSymbol, Name: "doc:" + rel, File: rel}}
		},
	}
	eng := multiEngine(t, dir, docs)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)

	if err := os.WriteFile(filepath.Join(dir, "src/leaf.ts"), []byte("export function leaf() { return fetch('/x'); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	body, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if body.ParsedFiles != 1 {
		t.Fatalf("body parsed %d, want 1; fallbacks=%v", body.ParsedFiles, body.Fallbacks)
	}
	applyRun(t, cons, s2)

	if err := os.Remove(filepath.Join(dir, "src/leaf.ts")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/renamed.ts"), []byte("export function leaf() { return 3; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/use.ts"), []byte("import { leaf } from './renamed'; export function use() { return leaf(); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s3 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s3, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s3)

	cfgPath := filepath.Join(dir, "tsconfig.json")
	if err := os.WriteFile(cfgPath, []byte(`{"compilerOptions":{"strict":true,"paths":{"@x/*":["./src/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s4 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s4, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s4)

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestSharedPathKeepsBothExtractorFacts(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export const a = 1;\n",
	})
	extra := fileOwnerStub{stubExtractor{
		name:   "overlay",
		detect: true,
		suffix: ".ts",
		fact: func(rel string) []facts.Fact {
			return []facts.Fact{{Kind: facts.KindSymbol, Name: "overlay:" + rel, File: rel}}
		},
	}}
	eng := multiEngine(t, dir, extra)
	state := filepath.Join(dir, ".enola", "st")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if _, ok := factByName(first.Facts, "src.a"); !ok {
		t.Fatal("missing ts fact")
	}
	if _, ok := factByName(first.Facts, "overlay:src/a.ts"); !ok {
		t.Fatal("missing overlay fact on shared path")
	}
	s2 := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 || len(s2.CloneRecords()) != 0 {
		t.Fatalf("shared-path noop parsed=%d msgs=%d", second.ParsedFiles, len(s2.CloneRecords()))
	}
	if err := os.WriteFile(filepath.Join(dir, "src/a.ts"), []byte("export const a = 2;\n"), 0o644); err != nil {
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
}

var _ plugin.Extractor = stubExtractor{}
var _ plugin.Extractor = fileOwnerStub{}
var _ plugin.Extractor = noOwnerExtractor{}
