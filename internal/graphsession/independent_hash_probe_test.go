package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/plugin"
)

type independentOpaque struct {
	name, input string
	calls       int
}

func (e *independentOpaque) Name() string                { return e.name }
func (e *independentOpaque) Detect(string) (bool, error) { return true, nil }
func (e *independentOpaque) OwnsFile(p string) bool      { return p == "note.md" }
func (e *independentOpaque) Extract(_ context.Context, root string, _ []string) ([]facts.Fact, error) {
	e.calls++
	b, err := os.ReadFile(filepath.Join(root, e.input))
	if err != nil {
		return nil, err
	}
	return []facts.Fact{{Kind: facts.KindSymbol, Name: "opaque", File: "note.md", Props: map[string]any{"value": string(b)}}}, nil
}
func independentEngine(t *testing.T, dir string, ignores []string, ext plugin.Extractor) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.Ignore = append(cfg.Ignore, ignores...)
	cfg.Extractors = []string{ext.Name()}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(ext)
	return eng
}
func independentRun(t *testing.T, eng *engine.Engine, dir, state string, c *Consumer) (*Result, int) {
	t.Helper()
	sink := &graphstream.MemorySink{}
	r, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", state)})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, c, sink)
	return r, len(sink.CloneRecords())
}
func independentWrite(t *testing.T, dir, p, b string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, p), []byte(b), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentOpaqueInputs(t *testing.T) {
	for _, name := range []string{"opaque", "mdintent"} {
		for _, ignored := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/normal", true: "/ignored"}[ignored], func(t *testing.T) {
				dir := setupTSRepo(t, map[string]string{"note.md": "# Note\n", "payload.dat": "old"})
				var ignore []string
				if ignored {
					ignore = []string{"payload.dat"}
				}
				ext := &independentOpaque{name: name, input: "payload.dat"}
				eng := independentEngine(t, dir, ignore, ext)
				inv, err := eng.Inventory(dir)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(inv.AllNames, "payload.dat") || slices.Contains(inv.Files, "payload.dat") == ignored {
					t.Fatalf("bad fixture inventory: %+v", inv)
				}
				applied := NewConsumer()
				first, _ := independentRun(t, eng, dir, "state", applied)
				independentWrite(t, dir, "payload.dat", "new")
				delta, n := independentRun(t, eng, dir, "state", applied)
				calls := ext.calls
				cold := NewConsumer()
				independentRun(t, eng, dir, "cold", cold)
				t.Logf("generation %d->%d delta events=%d extractor calls after delta=%d", first.TargetGeneration, delta.TargetGeneration, n, calls)
				assertAppliedEqualsCold(t, applied, cold)
			})
		}
	}
}

func TestIndependentBuiltinMarkdownTarget(t *testing.T) {
	for _, op := range []string{"content", "rename"} {
		t.Run(op, func(t *testing.T) {
			dir := setupTSRepo(t, map[string]string{"README.md": "# Title\n[link](target.txt)\n", "target.txt": "old"})
			eng := mdintentEngine(t, dir, false)
			applied := NewConsumer()
			first, _ := independentRun(t, eng, dir, "state", applied)
			before := applied.Canonical()
			if op == "content" {
				independentWrite(t, dir, "target.txt", "new")
			} else {
				if err := os.Rename(filepath.Join(dir, "target.txt"), filepath.Join(dir, "renamed.txt")); err != nil {
					t.Fatal(err)
				}
			}
			delta, n := independentRun(t, eng, dir, "state", applied)
			cold := NewConsumer()
			independentRun(t, eng, dir, "cold", cold)
			assertAppliedEqualsCold(t, applied, cold)
			if op == "content" && (n != 0 || delta.TargetGeneration != first.TargetGeneration) {
				t.Errorf("target content should be graph noop: events=%d generation=%d", n, delta.TargetGeneration)
			}
			if op == "rename" && (n == 0 || before == cold.Canonical()) {
				t.Error("target rename fixture must change graph and publish")
			}
			t.Logf("operation=%s delta events=%d generation %d->%d", op, n, first.TargetGeneration, delta.TargetGeneration)
		})
	}
}

func TestIndependentPrunedConfig(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;", "private/tsconfig.json": `{"compilerOptions":{"strict":true}}`})
	eng := independentEngine(t, dir, []string{"private/**"}, tsextractor.New())
	inv, err := eng.Inventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	full := tsextractor.ConfigInputPaths(dir)
	narrow := tsextractor.ConfigInputPathsFromNames(dir, inv.AllNames)
	if !slices.Contains(full, "private/tsconfig.json") || slices.Contains(inv.AllNames, "private/tsconfig.json") {
		t.Fatal("bad pruned-directory fixture")
	}
	if !slices.Equal(full, narrow) {
		t.Errorf("config discovery changed: full=%v fromNames=%v", full, narrow)
	}
	before, _, err := analysisFingerprint(dir, eng, inv.AllNames)
	if err != nil {
		t.Fatal(err)
	}
	applied := NewConsumer()
	independentRun(t, eng, dir, "state", applied)
	independentWrite(t, dir, "private/tsconfig.json", `{"compilerOptions":{"strict":false}}`)
	after, _, err := analysisFingerprint(dir, eng, inv.AllNames)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("config content change omitted from fingerprint")
	}
	delta, n := independentRun(t, eng, dir, "state", applied)
	cold := NewConsumer()
	independentRun(t, eng, dir, "cold", cold)
	assertAppliedEqualsCold(t, applied, cold)
	t.Logf("graph remains cold-equivalent for this inert config fixture; delta events=%d parsed=%d", n, delta.ParsedFiles)
}

func TestIndependentCustomMarkdownHashedDependency(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"note.md": "# Note\n", "src/a.ts": "export const a=1;"})
	ext := &independentOpaque{name: "mdintent", input: "src/a.ts"}
	eng := multiEngine(t, dir, ext)
	applied := NewConsumer()
	independentRun(t, eng, dir, "state", applied)
	independentWrite(t, dir, "src/a.ts", "export const a=2;")
	delta, n := independentRun(t, eng, dir, "state", applied)
	t.Logf("TS dependency definitely hashed and parsed=%d, events=%d, custom mdintent calls after delta=%d", delta.ParsedFiles, n, ext.calls)
	cold := NewConsumer()
	independentRun(t, eng, dir, "cold", cold)
	assertAppliedEqualsCold(t, applied, cold)
}
