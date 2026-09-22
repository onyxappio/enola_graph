package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func wave1ImportFixture() map[string]string {
	return map[string]string{
		"packages/contracts/package.json":                             `{"name":"@onyx/contracts","main":"./src/index.ts","types":"./src/index.ts"}`,
		"packages/contracts/src/index.ts":                             "export const Token = 1;\nexport function catalogTrackingEventSchema() { return 1; }\n",
		"packages/tracking-client/package.json":                       `{"name":"@onyx/tracking-client"}`,
		"packages/tracking-client/tsconfig.json":                      `{"compilerOptions":{"paths":{"@onyx/contracts":["../contracts/src/index.ts"]}}}`,
		"packages/tracking-client/src/core/consumer.ts":               "import { catalogTrackingEventSchema } from '@onyx/contracts';\nexport function consume() { return catalogTrackingEventSchema(); }\n",
		"services/product-api/src/productUsersSync.repair.ts":         "export type ProductUsersRepairPorts = { run(): Promise<string> };\nexport async function runProductUsersMismatchRepair(_input: unknown): Promise<'matched'> { return 'matched'; }\n",
		"services/product-api/src/productUsersSync.reconciliation.ts": "import type { ProductUsersRepairPorts } from './productUsersSync.repair';\nexport async function invokeRepair(ports: ProductUsersRepairPorts): Promise<string> {\n  const { runProductUsersMismatchRepair } = await import('./productUsersSync.repair');\n  return runProductUsersMismatchRepair(ports);\n}\n",
		"apps/mobile/src/state/mobileAppMachine.updates.ts":           "export function applyUpdate() { return 1; }\n",
		"apps/mobile/src/behavior/mobileAppInterpreter.ts":            "import * as update from '../state/mobileAppMachine.updates';\nexport function interpret() { return update.applyUpdate(); }\n",
		"packages/tracking-server/src/publisher.ts":                   "export function createTrackingPublisher() { return {}; }\n",
		"packages/tracking-server/src/index.ts":                       "export * from './publisher';\n",
	}
}

func applyGraph(t *testing.T, sink *graphstream.MemorySink) *Consumer {
	t.Helper()
	c := NewConsumer()
	if err := c.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	return c
}

func ownerKey(file string) string {
	return graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: file}.String()
}

func fileNodeByName(c *Consumer, name string) (graphstream.Node, bool) {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind == facts.KindFileRef && n.Name == name {
				return n, true
			}
		}
	}
	return graphstream.Node{}, false
}

func moduleNode(c *Consumer, name string) (graphstream.Node, bool) {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind == facts.KindModule && n.Name == name {
				return n, true
			}
		}
	}
	return graphstream.Node{}, false
}

// assertFileImportResolved checks the authoritative (resolved-phase) import edge
// from importer to destFile. TargetID must be the destination file_ref, not a
// parent directory module.
func assertFileImportResolved(t *testing.T, c *Consumer, importer, destFile string) graphstream.Edge {
	t.Helper()
	dest, ok := fileNodeByName(c, destFile)
	if !ok {
		t.Fatalf("%s: no file_ref node named %s", importer, destFile)
	}
	if dest.Owner.Kind != graphstream.OwnerFile || dest.Owner.ID != destFile || dest.File != destFile {
		t.Fatalf("%s: file_ref for %s is not file-owned: %+v", importer, destFile, dest)
	}
	var hits []graphstream.Edge
	for _, e := range c.Edges[ownerKey(importer)] {
		if e.Kind == facts.RelImports && e.TargetName == destFile {
			hits = append(hits, e)
		}
	}
	if len(hits) == 0 {
		t.Fatalf("%s: no imports edge targeting %s; edges=%v", importer, destFile, c.Edges[ownerKey(importer)])
	}
	for _, e := range hits {
		if e.Resolution != graphstream.ResResolved || e.TargetID == "" {
			t.Fatalf("%s -> %s resolution=%s target_id=%q", importer, destFile, e.Resolution, e.TargetID)
		}
		if e.TargetID != dest.ID {
			t.Fatalf("%s -> %s target_id=%s want file_ref %s", importer, destFile, e.TargetID, dest.ID)
		}
		if dir, ok := moduleNode(c, filepath.ToSlash(filepath.Dir(destFile))); ok && e.TargetID == dir.ID {
			t.Fatalf("%s -> %s collapsed onto directory module %s", importer, destFile, dir.Name)
		}
	}
	return hits[0]
}

func assertImportUnresolved(t *testing.T, c *Consumer, importer, targetName string) {
	t.Helper()
	var hits []graphstream.Edge
	for _, e := range c.Edges[ownerKey(importer)] {
		if e.Kind == facts.RelImports && e.TargetName == targetName {
			hits = append(hits, e)
		}
	}
	if len(hits) == 0 {
		t.Fatalf("%s: missing imports edge for %s", importer, targetName)
	}
	for _, e := range hits {
		if e.Resolution != graphstream.ResUnresolved || e.TargetID != "" {
			t.Fatalf("%s -> %s want unresolved empty id, got %s %q", importer, targetName, e.Resolution, e.TargetID)
		}
	}
}

func TestPublishedImportEdgesResolveToFileFacts(t *testing.T) {
	dir := setupTSRepo(t, wave1ImportFixture())
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("initial analysis parsed no files")
	}
	c := applyGraph(t, sink)

	t.Run("tsconfig-package-alias", func(t *testing.T) {
		assertFileImportResolved(t, c, "packages/tracking-client/src/core/consumer.ts", "packages/contracts/src/index.ts")
	})
	t.Run("relative-import-type", func(t *testing.T) {
		assertFileImportResolved(t, c, "services/product-api/src/productUsersSync.reconciliation.ts", "services/product-api/src/productUsersSync.repair.ts")
	})
	t.Run("runtime-dynamic-import", func(t *testing.T) {
		n := 0
		dest, _ := fileNodeByName(c, "services/product-api/src/productUsersSync.repair.ts")
		for _, e := range c.Edges[ownerKey("services/product-api/src/productUsersSync.reconciliation.ts")] {
			if e.Kind == facts.RelImports && e.TargetName == "services/product-api/src/productUsersSync.repair.ts" {
				n++
				if e.Resolution != graphstream.ResResolved || e.TargetID != dest.ID {
					t.Fatalf("dynamic/type import resolution=%s id=%s want %s", e.Resolution, e.TargetID, dest.ID)
				}
			}
		}
		if n < 2 {
			t.Fatalf("import type + import() should publish two file edges, got %d", n)
		}
	})
	t.Run("namespace-import", func(t *testing.T) {
		assertFileImportResolved(t, c, "apps/mobile/src/behavior/mobileAppInterpreter.ts", "apps/mobile/src/state/mobileAppMachine.updates.ts")
	})
	t.Run("star-reexport", func(t *testing.T) {
		assertFileImportResolved(t, c, "packages/tracking-server/src/index.ts", "packages/tracking-server/src/publisher.ts")
	})

	t.Run("no-change-zero-events", func(t *testing.T) {
		noop := &graphstream.MemorySink{}
		second, err := Run(context.Background(), eng, dir, noop, Options{StateDir: state})
		if err != nil {
			t.Fatal(err)
		}
		if second.ParsedFiles != 0 || len(noop.CloneRecords()) != 0 || second.BaseGeneration != second.TargetGeneration {
			t.Fatalf("noop parsed=%d events=%d gen %d->%d", second.ParsedFiles, len(noop.CloneRecords()), second.BaseGeneration, second.TargetGeneration)
		}
	})
}

func TestPublishedReexportMatchesEvidenceShape(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"packages/tracking-server/src/publisher.ts": "export function createTrackingPublisher() { return {}; }\n",
		"packages/tracking-server/src/index.ts":     "export * from './publisher';\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertFileImportResolved(t, c, "packages/tracking-server/src/index.ts", "packages/tracking-server/src/publisher.ts")
}

func TestPublishedImportMissingAndExternalStayUnresolved(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/app.ts": "import { missing } from './absent';\nimport { z } from 'zod';\nexport const n = 1;\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	t.Run("missing", func(t *testing.T) {
		assertImportUnresolved(t, c, "src/app.ts", "src/absent")
	})
	t.Run("external", func(t *testing.T) {
		assertImportUnresolved(t, c, "src/app.ts", "zod")
	})
}

func TestPublishedImportBindsFileNotDirectoryModule(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/foo.ts":       "export const fromFile = 1;\n",
		"src/foo/index.ts": "export const fromDir = 2;\n",
		"src/use.ts":       "import { fromFile } from './foo';\nexport const v = fromFile;\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	e := assertFileImportResolved(t, c, "src/use.ts", "src/foo.ts")
	if dirMod, ok := moduleNode(c, "src/foo"); ok && e.TargetID == dirMod.ID {
		t.Fatal("import ./foo bound to the folder module instead of foo.ts")
	}
}

func TestEncodeOwnerImportAmbiguityHasNoTargetID(t *testing.T) {
	a := facts.Fact{Kind: facts.KindFileRef, Name: "dup.ts", File: "a/dup.ts", Repo: "r"}
	b := facts.Fact{Kind: facts.KindFileRef, Name: "dup.ts", File: "b/dup.ts", Repo: "r"}
	dep := facts.Fact{
		Kind: facts.KindDependency, Name: "src -> dup.ts", File: "src/use.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelImports, Target: "dup.ts"}},
	}
	idx := buildIndex([]facts.Fact{a, b, dep})
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(dep), Facts: []facts.Fact{dep}}, idx, false)
	if len(edges) != 1 {
		t.Fatalf("edges=%d", len(edges))
	}
	if edges[0].Resolution != graphstream.ResAmbiguous || edges[0].TargetID != "" {
		t.Fatalf("ambiguous import guessed a target: %+v", edges[0])
	}
}

func TestPublishedImportTargetAddDeleteRename(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/index.ts": "export * from './publisher';\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	opts := Options{StateDir: state}

	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, opts); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	t.Run("missing-then-add", func(t *testing.T) {
		assertImportUnresolved(t, c, "src/index.ts", "src/publisher")
		if err := os.WriteFile(filepath.Join(dir, "src/publisher.ts"), []byte("export function createTrackingPublisher() { return {}; }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		addSink := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), eng, dir, addSink, opts); err != nil {
			t.Fatal(err)
		}
		if err := c.ApplyRecords(addSink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		assertFileImportResolved(t, c, "src/index.ts", "src/publisher.ts")
	})

	t.Run("delete", func(t *testing.T) {
		if err := os.Remove(filepath.Join(dir, "src/publisher.ts")); err != nil {
			t.Fatal(err)
		}
		delSink := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), eng, dir, delSink, opts); err != nil {
			t.Fatal(err)
		}
		if err := c.ApplyRecords(delSink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		assertImportUnresolved(t, c, "src/index.ts", "src/publisher")
	})

	t.Run("rename", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, "src/publisher.ts"), []byte("export function createTrackingPublisher() { return {}; }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		restore := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), eng, dir, restore, opts); err != nil {
			t.Fatal(err)
		}
		if err := c.ApplyRecords(restore.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "src/publisher.ts"), filepath.Join(dir, "src/shipped.ts")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "src/index.ts"), []byte("export * from './shipped';\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		renSink := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), eng, dir, renSink, opts); err != nil {
			t.Fatal(err)
		}
		if err := c.ApplyRecords(renSink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		assertFileImportResolved(t, c, "src/index.ts", "src/shipped.ts")
	})
}

func TestPublishedImportDeltaEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/publisher.ts": "export function createTrackingPublisher() { return 1; }\n",
		"src/index.ts":     "export * from './publisher';\n",
		"src/other.ts":     "export const other = 1;\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "live")
	opts := Options{StateDir: state}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)

	if err := os.WriteFile(filepath.Join(dir, "src/publisher.ts"), []byte("export function createTrackingPublisher() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles == 0 {
		t.Fatal("target-file edit parsed no files")
	}
	if err := cons.ApplyRecords(deltaSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	oracle := applyGraph(t, coldSink)
	assertAppliedEqualsCold(t, cons, oracle)
	assertFileImportResolved(t, cons, "src/index.ts", "src/publisher.ts")
	assertFileImportResolved(t, oracle, "src/index.ts", "src/publisher.ts")
}
