package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func mdTSEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	eng.RegisterExtractor(mdintent.New())
	return eng
}

func assertDeclaresModuleResolved(t *testing.T, c *Consumer, ownerFile, moduleName string) {
	t.Helper()
	mod, ok := moduleNode(c, moduleName)
	if !ok {
		t.Fatalf("%s: no module %s", ownerFile, moduleName)
	}
	var hits int
	for _, e := range c.Edges[ownerKey(ownerFile)] {
		if e.Kind != facts.RelDeclares || e.TargetName != moduleName {
			continue
		}
		hits++
		if e.Resolution != graphstream.ResResolved || e.TargetID != mod.ID {
			t.Fatalf("%s declares %s resolution=%s id=%q want %s", ownerFile, moduleName, e.Resolution, e.TargetID, mod.ID)
		}
	}
	if hits == 0 {
		t.Fatalf("%s: no declares edge to %s", ownerFile, moduleName)
	}
}

func TestPublishedMarkdownDirectoryModuleV1AndV2(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "docs/feedback-fixes/README.md", "# Onboarding feedback fixes — artifact index\n")
	writeFile(t, dir, "package.json", `{"name":"app"}`)
	run := func(auth bool, state string) *Consumer {
		eng, _ := mdScopeEngine(t, dir)
		sink := &graphstream.MemorySink{}
		opts := Options{StateDir: filepath.Join(dir, state), AuthoritativeFiles: auth}
		if auth {
			opts.MaxBeginBytes = 1048576
		}
		if _, err := Run(context.Background(), eng, dir, sink, opts); err != nil {
			t.Fatal(err)
		}
		return applyGraph(t, sink)
	}
	v1 := run(false, "v1")
	assertDeclaresModuleResolved(t, v1, "docs/feedback-fixes/README.md", "docs/feedback-fixes")
	v2 := run(true, "v2")
	if _, ok := moduleNode(v2, "docs/feedback-fixes"); ok {
		t.Fatal("v2 must not publish synthetic directory modules; see CF1/#30 protocol note")
	}
}

func TestPublishedMarkdownModuleDeltas(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "docs/a.md", "# A\n")
	writeFile(t, dir, "docs/b.md", "# B\n")
	writeFile(t, dir, "src/keep.ts", "export const K = 1\n")
	writeFile(t, dir, "package.json", `{"name":"app"}`)
	writeFile(t, dir, "tsconfig.json", `{}`)
	eng := mdTSEngine(t, dir)
	state := filepath.Join(dir, ".enola", "live")
	opts := Options{StateDir: state}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)
	assertDeclaresModuleResolved(t, cons, "docs/a.md", "docs")
	assertDeclaresModuleResolved(t, cons, "src/keep.ts", "src")

	noop := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, noop, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 0 {
		t.Fatalf("nochange parsed=%d", delta.ParsedFiles)
	}

	if err := os.Remove(filepath.Join(dir, "docs/b.md")); err != nil {
		t.Fatal(err)
	}
	delSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, delSink, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(delSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, applyGraph(t, coldSink))
	assertDeclaresModuleResolved(t, cons, "docs/a.md", "docs")
}

func TestPublishedConstantDeclaresFileOwnedModule(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/codes.ts": "export const CODE_A = 1\nexport const CODE_B = 2\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: t.TempDir(), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertDeclaresModuleResolved(t, applyGraph(t, sink), "src/codes.ts", "src")
}

func TestPublishedJSXAndBridgeDeltasEqualCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/DetailField.tsx":  "export function DetailField() { return null }\n",
		"src/DetailsGroup.tsx": "import { DetailField } from './DetailField'\nexport function DetailsGroup() { return <DetailField /> }\n",
		"src/leaf.ts":          "export const CONFETTI_STATIC_PROGRESS = 1\n",
		"src/bridge.ts":        "import { CONFETTI_STATIC_PROGRESS } from './leaf'\nexport { CONFETTI_STATIC_PROGRESS }\nexport function x() { return CONFETTI_STATIC_PROGRESS }\n",
		"src/use.ts":           "import { CONFETTI_STATIC_PROGRESS } from './bridge'\nexport function use() { return CONFETTI_STATIC_PROGRESS }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "live")
	opts := Options{StateDir: state}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)
	assertCallResolvedToFile(t, cons, "src/DetailsGroup.tsx", "src.DetailField", "src/DetailField.tsx")

	if err := os.WriteFile(filepath.Join(dir, "src/DetailField.tsx"), []byte("export function DetailField() { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/leaf.ts"), []byte("export const CONFETTI_STATIC_PROGRESS = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, d, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(d.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, cold, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, applyGraph(t, cold))
}
