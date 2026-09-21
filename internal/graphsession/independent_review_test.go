package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

type reviewContentExt struct {
	name  string
	owner bool
}

func (e reviewContentExt) Name() string                { return e.name }
func (e reviewContentExt) Detect(string) (bool, error) { return true, nil }
func (e reviewContentExt) Extract(_ context.Context, root string, files []string) ([]facts.Fact, error) {
	var out []facts.Fact
	for _, f := range files {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(root, f))
		out = append(out, facts.Fact{Kind: facts.KindSymbol, Name: e.name + ":" + string(b), File: f})
	}
	return out, nil
}

type reviewOwnedExt struct{ reviewContentExt }

func (e reviewOwnedExt) OwnsFile(f string) bool { return strings.HasSuffix(f, ".md") }

func TestReviewOverlappingContent(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a = 1;", "note.md": "old"})
	eng := multiEngine(t, dir, reviewOwnedExt{reviewContentExt{name: "first"}}, reviewOwnedExt{reviewContentExt{name: "second"}})
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	s = &graphstream.MemorySink{}
	r, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Facts {
		if strings.HasPrefix(f.Name, "second:") {
			t.Logf("second contribution = %s", f.Name)
		}
	}
	applyRun(t, cons, s)
	cold := NewConsumer()
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cold, s)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestReviewNoOwnerContent(t *testing.T) {
	for _, op := range []string{"edit", "add", "delete"} {
		t.Run(op, func(t *testing.T) {
			dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a = 1;", "note.md": "old"})
			eng := multiEngine(t, dir, reviewContentExt{name: "bare"})
			state := filepath.Join(dir, ".enola", "state")
			cons := NewConsumer()
			s := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, s)
			switch op {
			case "edit":
				if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("new"), 0644); err != nil {
					t.Fatal(err)
				}
			case "add":
				if err := os.WriteFile(filepath.Join(dir, "added.md"), []byte("added"), 0644); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if err := os.Remove(filepath.Join(dir, "note.md")); err != nil {
					t.Fatal(err)
				}
			}
			s = &graphstream.MemorySink{}
			r, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("delta generation=%d events=%d", r.TargetGeneration, len(s.CloneRecords()))
			applyRun(t, cons, s)
			cold := NewConsumer()
			s = &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
				t.Fatal(err)
			}
			applyRun(t, cold, s)
			assertAppliedEqualsCold(t, cons, cold)
		})
	}
}

func TestReviewMarkdownOnlyChange(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a = 1;", "note.md": "old"})
	eng := multiEngine(t, dir, reviewOwnedExt{reviewContentExt{name: "docs"}})
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)
	cold := NewConsumer()
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cold, s)
	assertAppliedEqualsCold(t, cons, cold)
}

type reviewRelExt struct{ reviewOwnedExt }

func (e reviewRelExt) Extract(ctx context.Context, root string, files []string) ([]facts.Fact, error) {
	ff, err := e.reviewOwnedExt.Extract(ctx, root, files)
	for i := range ff {
		ff[i].Relations = []facts.Relation{{Kind: "references", Target: "src"}}
	}
	return ff, err
}

func TestReviewSyntheticResolution(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a = 1;", "note.md": "old"})
	eng := multiEngine(t, dir, reviewRelExt{reviewOwnedExt{reviewContentExt{name: "docs"}}})
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)
	cold := NewConsumer()
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cold, s)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestReviewDirectIOContract(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"a.ts": "export function get(){ return fetch('/api'); }"})
	r, err := Run(context.Background(), testEngine(t, dir), dir, &graphstream.MemorySink{}, Options{StateDir: filepath.Join(dir, ".enola", "state")})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Facts {
		if f.Kind == facts.KindSymbol {
			if d, _ := f.Props["io_direct"].(bool); d {
				if p, _ := f.Props["performs_io"].(bool); !p {
					t.Fatalf("direct symbol %s loses performs_io: %v", f.Name, f.Props)
				}
				return
			}
		}
	}
	t.Fatal("no direct IO symbol")
}

func TestReviewMinifiedNoop(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"a.ts": "export const a=1;", "bundle.js": strings.Repeat("var x=1;", 2000)})
	state := filepath.Join(dir, ".enola", "state")
	eng := testEngine(t, dir)
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	s := &graphstream.MemorySink{}
	r, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if r.ParsedFiles != 0 || r.TargetGeneration != 1 || len(s.CloneRecords()) != 0 {
		t.Fatalf("minified noop parsed=%d gen=%d events=%d", r.ParsedFiles, r.TargetGeneration, len(s.CloneRecords()))
	}
}

func TestReviewRemoveAmbiguousCandidate(t *testing.T) {
	for _, op := range []string{"delete", "rename-symbol"} {
		t.Run(op, func(t *testing.T) {
			dir := setupTSRepo(t, map[string]string{
				"src/a.ts": "export function helper(){return 1;}",
				"src/b.ts": "export function helper(){return 2;}",
				"src/u.ts": "import {helper} from './a'; export function use(){return helper();}",
			})
			eng := testEngine(t, dir)
			state := filepath.Join(dir, ".enola", "state")
			cons := NewConsumer()
			s := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, s)
			if op == "delete" {
				if err := os.Remove(filepath.Join(dir, "src/b.ts")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("export function renamed(){return 2;}"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			s = &graphstream.MemorySink{}
			r, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("parsed=%d", r.ParsedFiles)
			applyRun(t, cons, s)
			cold := NewConsumer()
			s = &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
				t.Fatal(err)
			}
			applyRun(t, cold, s)
			assertAppliedEqualsCold(t, cons, cold)
		})
	}
}
