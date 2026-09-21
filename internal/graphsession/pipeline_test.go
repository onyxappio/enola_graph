package graphsession

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func applyRun(t *testing.T, c *Consumer, sink *graphstream.MemorySink) {
	t.Helper()
	if err := c.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
}

func assertAppliedEqualsCold(t *testing.T, applied, cold *Consumer) {
	t.Helper()
	got, want := applied.Canonical(), cold.Canonical()
	if got != want {
		t.Fatalf("applied consumer != cold\n applied=%s\n cold=%s", got, want)
	}
}

func TestDeltaBodyEditPublishesBoundedOwners(t *testing.T) {
	files := map[string]string{
		"src/leaf.ts": "export function leaf() { return 1; }\n",
		"src/use.ts":  "import { leaf } from './leaf'; export function use() { return leaf(); }\n",
	}
	for i := 0; i < 30; i++ {
		files[fmt.Sprintf("src/ind_%d.ts", i)] = fmt.Sprintf("export const n%d = %d;\n", i, i)
	}
	dir := setupTSRepo(t, files)
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/leaf.ts"), []byte("export function leaf() { return fetch('/x'); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("body edit parsed %d, want 1", delta.ParsedFiles)
	}
	if delta.OwnersPublished > 4 {
		t.Fatalf("body-only edit published %d owners, want a small affected set", delta.OwnersPublished)
	}
}

func TestIncrementalEndRequiresManifest(t *testing.T) {
	c := NewConsumer()
	owner := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "ep", TargetGeneration: 1,
		Phase: graphstream.PhaseEpoch, ScopeMode: graphstream.ScopeModeIncremental,
	}
	braw, _ := graphstream.Marshal(begin)
	scope := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "ep", Seq: 1, Phase: graphstream.PhaseScope, Owners: []graphstream.OwnerRef{owner}}
	sraw, _ := graphstream.Marshal(scope)
	resolved := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "ep", Seq: 2, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: owner, ID: "a", Kind: facts.KindSymbol, Name: "a"}},
	}
	rraw, _ := graphstream.Marshal(resolved)
	endMissing := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "ep", BatchCount: 2,
		BatchDigest: graphstream.DigestBatches([][]byte{sraw, rraw}), OwnerScopeLen: 1,
		Completeness: graphstream.Completeness{Status: "success"},
	}
	eraw, _ := graphstream.Marshal(endMissing)
	if err := c.ApplyRecords([]graphstream.Recorded{
		{MsgID: "b", Payload: braw}, {MsgID: "s", Payload: sraw}, {MsgID: "r", Payload: rraw}, {MsgID: "e", Payload: eraw},
	}); err == nil {
		t.Fatal("incremental End without owner_scope_digest must not commit")
	}
}

func TestDeepReexportClosureParsesChainAndPreservesIndependents(t *testing.T) {
	files := map[string]string{
		"src/c.ts": "export function leaf() { return 1; }\n",
		"src/b.ts": "export { leaf } from './c';\n",
		"src/a.ts": "import { leaf } from './b'; export function a() { return leaf(); }\n",
	}
	for i := 0; i < 100; i++ {
		files[fmt.Sprintf("src/ind_%d.ts", i)] = fmt.Sprintf("export const n%d = %d;\n", i, i)
	}
	dir := setupTSRepo(t, files)
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if first.ParsedFiles < 103 {
		t.Fatalf("initial parsed %d", first.ParsedFiles)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/c.ts"), []byte("export function leaf() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s2)
	if delta.ParsedFiles > 10 {
		t.Fatalf("deep reexport parsed %d files, want the A->B->C chain not the 100 independents", delta.ParsedFiles)
	}
	if delta.ParsedFiles < 1 {
		t.Fatal("must reparse the changed file")
	}
	if delta.Stats.CachedFiles < 100 {
		t.Fatalf("cached %d, want the 100 independents preserved", delta.Stats.CachedFiles)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestDeleteAndRenameAppliedEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/c.ts": "export function leaf() { return 1; }\n",
		"src/b.ts": "export { leaf } from './c';\n",
		"src/a.ts": "import { leaf } from './b'; export function a() { return leaf(); }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)

	if err := os.Remove(filepath.Join(dir, "src/c.ts")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/d.ts"), []byte("export function leaf() { return 3; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("export { leaf } from './d';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s2)
	if _, ok := factByName(consFacts(cons), "src.leaf"); ok {
		// leaf still exists from d.ts; gone file owner must be empty in canonical compare.
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestNewSymbolAmbiguityAppliedEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export function helper() { return 1; }\n",
		"src/u.ts": "import { helper } from './a'; export function use() { return helper(); }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("export function helper() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s2)
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestMixedPrismaTSAppliedEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export function a() { return 1; }\n",
	})
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"app","dependencies":{"@prisma/client":"5.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prisma/schema.prisma"), []byte("model User { id Int @id }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	n := 0
	for _, f := range first.Facts {
		if f.Kind == facts.KindStorage {
			n++
		}
	}
	if n == 0 {
		t.Fatal("expected prisma storage facts on initial run")
	}
	if err := os.WriteFile(filepath.Join(dir, "prisma/schema.prisma"), []byte("model User { id Int @id }\nmodel Post { id Int @id }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s2)
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestUnreadablePrismaSchemaFails(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;\n"})
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"app","dependencies":{"@prisma/client":"5.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "prisma/schema.prisma")
	if err := os.WriteFile(p, []byte("model User { id Int @id }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p, 0o644)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state}); err == nil {
		t.Fatal("unreadable prisma schema must not complete a successful replacement")
	}
}

func consFacts(c *Consumer) []facts.Fact {
	var out []facts.Fact
	for _, ns := range c.Owners {
		for _, n := range ns {
			out = append(out, facts.Fact{Kind: n.Kind, Name: n.Name, File: n.File})
		}
	}
	return out
}
