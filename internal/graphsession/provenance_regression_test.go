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
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func mdintentEngine(t *testing.T, dir string, withTS bool) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	if withTS {
		return multiEngine(t, dir, mdintent.New())
	}
	cfg.Extractors = []string{"mdintent"}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(mdintent.New())
	return eng
}

func coverageOccurrences(c *Consumer, name string) int {
	n := 0
	for _, ns := range c.Owners {
		for _, node := range ns {
			if node.Kind == facts.KindExtraction && node.Name == name {
				n++
			}
		}
	}
	return n
}

func ownerIDsFromRun(t *testing.T, sink *graphstream.MemorySink) []string {
	t.Helper()
	begins, batches, _, err := DecodeRun(sink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, b := range begins {
		for _, o := range b.OwnerScope {
			add(o.ID)
		}
	}
	for _, b := range batches {
		for _, o := range b.Owners {
			add(o.ID)
		}
		for _, n := range b.Nodes {
			add(n.Owner.ID)
		}
		for _, e := range b.Edges {
			add(e.Owner.ID)
		}
	}
	return out
}

func TestOwnedExtractorFileSetAddEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"README.md": "# Title\n[link](target.txt)\n",
	})
	eng := mdintentEngine(t, dir, false)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)

	sNoop := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, dir, sNoop, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || len(sNoop.CloneRecords()) != 0 || noop.TargetGeneration != first.TargetGeneration {
		t.Fatalf("fileset noop parsed=%d msgs=%d gen %d->%d",
			noop.ParsedFiles, len(sNoop.CloneRecords()), first.TargetGeneration, noop.TargetGeneration)
	}

	if err := os.WriteFile(filepath.Join(dir, "target.txt"), []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.TargetGeneration == first.TargetGeneration || len(s2.CloneRecords()) == 0 {
		t.Fatalf("adding in-scope link target skipped publish gen=%d events=%d",
			delta.TargetGeneration, len(s2.CloneRecords()))
	}
	for _, id := range ownerIDsFromRun(t, s2) {
		if id == "target.txt" || strings.HasSuffix(id, "/target.txt") {
			t.Fatalf("file-set context claimed output ownership of %s", id)
		}
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

func TestLastDetectedInputDeletionEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"a.ts":      "export const a = 1;\n",
		"README.md": "# Title\n",
	})
	eng := mdintentEngine(t, dir, true)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)

	sNoop := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, dir, sNoop, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || len(sNoop.CloneRecords()) != 0 || noop.TargetGeneration != first.TargetGeneration {
		t.Fatalf("delete-prep noop parsed=%d msgs=%d gen %d->%d",
			noop.ParsedFiles, len(sNoop.CloneRecords()), first.TargetGeneration, noop.TargetGeneration)
	}

	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 0 {
		t.Fatalf("README delete parsed %d TypeScript files, want 0", delta.ParsedFiles)
	}
	if delta.TargetGeneration == first.TargetGeneration || len(s2.CloneRecords()) == 0 {
		t.Fatalf("last detected input deletion skipped replacement gen=%d events=%d",
			delta.TargetGeneration, len(s2.CloneRecords()))
	}
	applyRun(t, cons, s2)
	if _, ok := factByName(consFacts(cons), "README.md"); ok {
		t.Fatal("deleted README facts were retained")
	}

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestSyntheticProvenanceReplaceEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"a.ts":      "export const a = 1;\n",
		"README.md": "# Title\n[link](missing.txt)\n",
	})
	eng := mdintentEngine(t, dir, true)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if coverageOccurrences(cons, "mdintent:links") != 1 {
		t.Fatalf("initial coverage occurrences=%d, want 1", coverageOccurrences(cons, "mdintent:links"))
	}

	sNoop := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, dir, sNoop, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || len(sNoop.CloneRecords()) != 0 || noop.TargetGeneration != first.TargetGeneration {
		t.Fatalf("synthetic-prep noop parsed=%d msgs=%d gen %d->%d",
			noop.ParsedFiles, len(sNoop.CloneRecords()), first.TargetGeneration, noop.TargetGeneration)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# New\n[link](a.ts)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 0 {
		t.Fatalf("README edit parsed %d TypeScript files, want 0", delta.ParsedFiles)
	}
	applyRun(t, cons, s2)
	if coverageOccurrences(cons, "mdintent:links") != 1 {
		t.Fatalf("delta retained stale coverage occurrences=%d, want 1", coverageOccurrences(cons, "mdintent:links"))
	}

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestEmptyInitialAfterLastMarkdownDelete(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"README.md": "# Title\n"})
	eng := mdintentEngine(t, dir, false)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s1)
	if first.TargetGeneration != 1 {
		t.Fatalf("initial generation=%d, want 1", first.TargetGeneration)
	}

	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.TargetGeneration == first.TargetGeneration || len(s2.CloneRecords()) == 0 {
		t.Fatalf("mdintent-only last-file delete skipped replacement gen=%d events=%d",
			delta.TargetGeneration, len(s2.CloneRecords()))
	}
	applyRun(t, cons, s2)

	sNoop := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, dir, sNoop, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || len(sNoop.CloneRecords()) != 0 || noop.TargetGeneration != delta.TargetGeneration {
		t.Fatalf("empty completed delta parsed=%d msgs=%d gen %d->%d",
			noop.ParsedFiles, len(sNoop.CloneRecords()), delta.TargetGeneration, noop.TargetGeneration)
	}

	coldSink := &graphstream.MemorySink{}
	coldRes, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(coldSink.CloneRecords()) == 0 {
		t.Fatal("empty initial published no epoch")
	}
	begins, _, ends, err := DecodeRun(coldSink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(begins) != 1 || len(ends) != 1 {
		t.Fatalf("empty initial begins=%d ends=%d", len(begins), len(ends))
	}
	if coldRes.TargetGeneration != 1 {
		t.Fatalf("empty initial generation=%d, want 1", coldRes.TargetGeneration)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}
