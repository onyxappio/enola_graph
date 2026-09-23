package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestPublishedDefaultAsSFCBarrelAndDestructure(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/index.ts":                   "export { default as Stepper } from './Stepper/Stepper.vue'\nexport { useCoreComponentsSettings, useCoreComponents } from './utils/useCoreComponents'\n",
		"src/Stepper/Stepper.vue":        `<script setup lang="ts"></script><template><div/></template>`,
		"src/utils/useCoreComponents.ts": "const [useCoreComponentsSettings, useCoreComponents] = [() => 1, () => 2]\nexport { useCoreComponentsSettings, useCoreComponents }\n",
		"src/data_easy.vue": `<script setup lang="ts">
import { Stepper, useCoreComponentsSettings } from './index'
export function DataEasy() { return useCoreComponentsSettings(Stepper) }
</script>
<template><Stepper /></template>
`,
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "live")
	opts := Options{StateDir: state}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)
	assertCallResolvedToFile(t, cons, "src/data_easy.vue", "src/Stepper.Stepper", "src/Stepper/Stepper.vue")
	assertCallResolvedToFile(t, cons, "src/data_easy.vue", "src/utils.useCoreComponentsSettings", "src/utils/useCoreComponents.ts")

	noop := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, noop, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 0 {
		t.Fatalf("nochange parsed=%d", delta.ParsedFiles)
	}

	if err := os.WriteFile(filepath.Join(dir, "src/utils/useCoreComponents.ts"), []byte("const [useCoreComponentsSettings, useCoreComponents] = [() => 3, () => 4]\nexport { useCoreComponentsSettings, useCoreComponents }\n"), 0o644); err != nil {
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

func TestPublishedMarkdownNamesEmptyTSFileRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"app"}`)
	writeFile(t, dir, "tsconfig.json", `{}`)
	writeFile(t, dir, "src/routes/health.ts", "export async function registerHealthRoutes() { return 1 }\n")
	writeFile(t, dir, "src/routes/scans.ts", "import { registerHealthRoutes } from './health'\nexport function registerScanRoutes() { return registerHealthRoutes() }\n")
	writeFile(t, dir, "docs/INFO.md", "## Known false-positives\n\n- `src/routes/health.ts` is public.\n- `src/routes/scans.ts` is public.\n")
	eng := mdTSEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: t.TempDir(), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertFileRefExists(t, c, "src/routes/health.ts")
	assertFileRefExists(t, c, "src/routes/scans.ts")
	assertNamesResolvedToFileRef(t, c, "docs/INFO.md", "src/routes/health.ts")
	assertNamesResolvedToFileRef(t, c, "docs/INFO.md", "src/routes/scans.ts")
}

func TestPublishedDeclarationSiblingPrefersImplementation(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/environmentProfile.ts":    "import { assertPaymentDemoRuntime } from './paymentDemoRuntime.mjs'\nexport function create() { return assertPaymentDemoRuntime() }\n",
		"src/paymentDemoRuntime.mjs":   "export function assertPaymentDemoRuntime() { return 1 }\n",
		"src/paymentDemoRuntime.d.mts": "export declare function assertPaymentDemoRuntime(): void\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "live")
	opts := Options{StateDir: state}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)
	assertCallResolvedToFile(t, cons, "src/environmentProfile.ts", "src.assertPaymentDemoRuntime", "src/paymentDemoRuntime.mjs")

	if err := os.Remove(filepath.Join(dir, "src/paymentDemoRuntime.d.mts")); err != nil {
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
	assertCallResolvedToFile(t, cons, "src/environmentProfile.ts", "src.assertPaymentDemoRuntime", "src/paymentDemoRuntime.mjs")
}

func TestPublishedConstantDeclaresFileOwnedModuleV2(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/storybookArtifactAttestation.ts": `
export class RecaptureRequiredError extends Error {
  readonly code = 'recapture-required' as const
}
`,
	})
	eng := testEngine(t, dir)
	run := func(auth bool, state string) *Consumer {
		sink := &graphstream.MemorySink{}
		opts := Options{StateDir: filepath.Join(dir, state), ForceInitial: true, AuthoritativeFiles: auth}
		if auth {
			opts.MaxBeginBytes = 1048576
		}
		if _, err := Run(context.Background(), eng, dir, sink, opts); err != nil {
			t.Fatal(err)
		}
		return applyGraph(t, sink)
	}
	v1 := run(false, "v1")
	assertDeclaresModuleResolved(t, v1, "src/storybookArtifactAttestation.ts", "src")
	assertConstantClassResolved(t, v1, "src.RecaptureRequiredError.code", "src.RecaptureRequiredError")
	assertConstantResolvedDegree(t, v1, "src.RecaptureRequiredError.code", 2)

	v2 := run(true, "v2")
	assertConstantClassResolved(t, v2, "src.RecaptureRequiredError.code", "src.RecaptureRequiredError")
	assertConstantResolvedDegree(t, v2, "src.RecaptureRequiredError.code", 1)

	if err := os.WriteFile(filepath.Join(dir, "src/storybookArtifactAttestation.ts"), []byte("export class RecaptureRequiredError extends Error { readonly code = 'x' as const }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{StateDir: filepath.Join(dir, "v2-live"), AuthoritativeFiles: true, MaxBeginBytes: 1048576}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)
	d := &graphstream.MemorySink{}
	if err := os.WriteFile(filepath.Join(dir, "src/storybookArtifactAttestation.ts"), []byte("export class RecaptureRequiredError extends Error { readonly code = 'y' as const }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), eng, dir, d, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(d.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, cold, Options{StateDir: filepath.Join(dir, "v2-cold"), ForceInitial: true, AuthoritativeFiles: true, MaxBeginBytes: 1048576}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, applyGraph(t, cold))
}

func assertFileRefExists(t *testing.T, c *Consumer, file string) {
	t.Helper()
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind == facts.KindFileRef && n.Name == file && n.File == file {
				return
			}
		}
	}
	t.Fatalf("missing file_ref %s", file)
}

func assertNamesResolvedToFileRef(t *testing.T, c *Consumer, owner, targetFile string) {
	t.Helper()
	var fileRefID string
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind == facts.KindFileRef && n.Name == targetFile {
				fileRefID = n.ID
			}
		}
	}
	if fileRefID == "" {
		t.Fatalf("no file_ref node for %s", targetFile)
	}
	for _, e := range c.Edges[ownerKey(owner)] {
		if e.Kind == facts.RelNames && e.TargetName == targetFile {
			if e.Resolution != graphstream.ResResolved || e.TargetID != fileRefID {
				t.Fatalf("names %s -> %s resolution=%s id=%q want %s", owner, targetFile, e.Resolution, e.TargetID, fileRefID)
			}
			return
		}
	}
	t.Fatalf("missing names edge %s -> %s", owner, targetFile)
}

func assertConstantClassResolved(t *testing.T, c *Consumer, constName, className string) {
	t.Helper()
	var constID, classID string
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind != facts.KindSymbol {
				continue
			}
			if n.Name == constName {
				constID = n.ID
			}
			if n.Name == className {
				classID = n.ID
			}
		}
	}
	if constID == "" || classID == "" {
		t.Fatalf("missing %s or %s", constName, className)
	}
	for _, edges := range c.Edges {
		for _, e := range edges {
			if e.FromID == constID && e.Kind == facts.RelDeclares && e.TargetName == className {
				if e.Resolution != graphstream.ResResolved || e.TargetID != classID {
					t.Fatalf("class declares resolution=%s id=%q want %s", e.Resolution, e.TargetID, classID)
				}
				return
			}
		}
	}
	t.Fatalf("missing resolved declares %s -> %s", constName, className)
}

func assertConstantResolvedDegree(t *testing.T, c *Consumer, name string, want int) {
	t.Helper()
	var id string
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind == facts.KindSymbol && n.Name == name {
				id = n.ID
			}
		}
	}
	if id == "" {
		t.Fatalf("missing constant %s", name)
	}
	n := 0
	for _, edges := range c.Edges {
		for _, e := range edges {
			if e.FromID == id && e.Resolution == graphstream.ResResolved {
				n++
			}
		}
	}
	if n != want {
		t.Fatalf("%s resolved degree=%d want %d", name, n, want)
	}
}
