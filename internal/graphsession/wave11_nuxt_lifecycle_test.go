package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func wave11LandingsRepo(t *testing.T, moduleSetup string) string {
	t.Helper()
	return setupTSRepo(t, map[string]string{
		"apps/landings/package.json": `{"name":"landings","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0","landings-module":"workspace:*"}}`,
		"apps/landings/nuxt.config.ts": `
import landingModule from 'landings-module'
export default defineNuxtConfig({ modules: [landingModule] })
`,
		"apps/landings/pages/CommunityProof.vue": `<script setup lang="ts">
const { nextDelayed } = useStep()
setLandPageMetadata({ page: 'x' })
</script><template><p /></template>`,
		"apps/landings/composables/useLocalFlag.ts":                               `export function useLocalFlag() { return true }`,
		"apps/landings/pages/Local.vue":                                           `<script setup lang="ts">useLocalFlag()</script><template><p /></template>`,
		"packages/landings-module/package.json":                                   `{"name":"landings-module","dependencies":{"nuxt":"^3.0.0"}}`,
		"packages/landings-module/src/module.ts":                                  moduleSetup,
		"packages/landings-module/src/runtime/composables/useStep.ts":             `export { useStep } from 'shared-lands-components'`,
		"packages/landings-module/src/runtime/composables/setLandPageMetadata.ts": `export function setLandPageMetadata(meta: Record<string, string>) {}`,
		"packages/shared-lands-components/package.json":                           `{"name":"shared-lands-components"}`,
		"packages/shared-lands-components/src/index.ts":                           `export { useStep } from './Stepper/useStep'`,
		"packages/shared-lands-components/src/Stepper/useStep.ts":                 `export function useStep() { return { nextDelayed() {} } }`,
	})
}

func TestPublishedWave11NuxtRegisterLifecycleV1AndV2(t *testing.T) {
	registered := `export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
}
`
	unregistered := `export default function setup() {
}
`
	stepFile := "packages/shared-lands-components/src/Stepper/useStep.ts"
	stepName := "packages/shared-lands-components/src/Stepper.useStep"
	metaFile := "packages/landings-module/src/runtime/composables/setLandPageMetadata.ts"
	metaName := "packages/landings-module/src/runtime/composables.setLandPageMetadata"
	localFile := "apps/landings/composables/useLocalFlag.ts"
	localName := "apps/landings/composables.useLocalFlag"
	consumer := "apps/landings/pages/CommunityProof.vue"
	moduleRel := "packages/landings-module/src/module.ts"

	for _, auth := range []bool{false, true} {
		name := "v1"
		if auth {
			name = "v2"
		}
		t.Run(name, func(t *testing.T) {
			dir := wave11LandingsRepo(t, registered)
			eng := testEngine(t, dir)
			state := filepath.Join(dir, ".enola", name)
			opts := Options{StateDir: state, AuthoritativeFiles: auth, FreshEngine: true}
			if auth {
				opts.MaxBeginBytes = 1048576
			}
			live := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
				t.Fatal(err)
			}
			cons := applyGraph(t, live)
			assertCallResolvedToFile(t, cons, consumer, stepName, stepFile)
			assertCallResolvedToFile(t, cons, consumer, metaName, metaFile)
			assertCallResolvedToFile(t, cons, "apps/landings/pages/Local.vue", localName, localFile)

			warmSink := &graphstream.MemorySink{}
			warm, err := Run(context.Background(), eng, dir, warmSink, opts)
			if err != nil {
				t.Fatal(err)
			}
			if warm.ParsedFiles != 0 {
				t.Fatalf("warmup parsed=%d", warm.ParsedFiles)
			}

			body, err := os.ReadFile(filepath.Join(dir, consumer))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, consumer), append(body, []byte("\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			editSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, editSink, opts); err != nil {
				t.Fatal(err)
			}
			if err := cons.ApplyRecords(editSink.CloneRecords()); err != nil {
				t.Fatal(err)
			}
			assertCallResolvedToFile(t, cons, consumer, stepName, stepFile)
			coldEdit := &graphstream.MemorySink{}
			coldOpts := Options{StateDir: filepath.Join(dir, ".enola", name+"-cold-edit"), ForceInitial: true, AuthoritativeFiles: auth, FreshEngine: true}
			if auth {
				coldOpts.MaxBeginBytes = 1048576
			}
			if _, err := Run(context.Background(), eng, dir, coldEdit, coldOpts); err != nil {
				t.Fatal(err)
			}
			assertAppliedEqualsCold(t, cons, applyGraph(t, coldEdit))

			if err := os.WriteFile(filepath.Join(dir, moduleRel), []byte(unregistered), 0o644); err != nil {
				t.Fatal(err)
			}
			unregSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, unregSink, opts); err != nil {
				t.Fatal(err)
			}
			if err := cons.ApplyRecords(unregSink.CloneRecords()); err != nil {
				t.Fatal(err)
			}
			assertCallUnresolved(t, cons, consumer, stepName)
			coldUnreg := &graphstream.MemorySink{}
			coldUnregOpts := Options{StateDir: filepath.Join(dir, ".enola", name+"-cold-unreg"), ForceInitial: true, AuthoritativeFiles: auth, FreshEngine: true}
			if auth {
				coldUnregOpts.MaxBeginBytes = 1048576
			}
			if _, err := Run(context.Background(), eng, dir, coldUnreg, coldUnregOpts); err != nil {
				t.Fatal(err)
			}
			assertAppliedEqualsCold(t, cons, applyGraph(t, coldUnreg))

			if err := os.WriteFile(filepath.Join(dir, moduleRel), []byte(registered), 0o644); err != nil {
				t.Fatal(err)
			}
			restoreSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, restoreSink, opts); err != nil {
				t.Fatal(err)
			}
			if err := cons.ApplyRecords(restoreSink.CloneRecords()); err != nil {
				t.Fatal(err)
			}
			assertCallResolvedToFile(t, cons, consumer, stepName, stepFile)
			coldRest := &graphstream.MemorySink{}
			coldRestOpts := Options{StateDir: filepath.Join(dir, ".enola", name+"-cold-restore"), ForceInitial: true, AuthoritativeFiles: auth, FreshEngine: true}
			if auth {
				coldRestOpts.MaxBeginBytes = 1048576
			}
			if _, err := Run(context.Background(), eng, dir, coldRest, coldRestOpts); err != nil {
				t.Fatal(err)
			}
			assertAppliedEqualsCold(t, cons, applyGraph(t, coldRest))

			noop := &graphstream.MemorySink{}
			silent, err := Run(context.Background(), eng, dir, noop, opts)
			if err != nil {
				t.Fatal(err)
			}
			if silent.ParsedFiles != 0 {
				t.Fatalf("nochange parsed=%d", silent.ParsedFiles)
			}
		})
	}
}

func assertCallUnresolved(t *testing.T, c *Consumer, ownerFile, targetName string) {
	t.Helper()
	for _, e := range c.Edges[ownerKey(ownerFile)] {
		if e.Kind == facts.RelCalls && e.TargetName == targetName && e.Resolution == graphstream.ResResolved && e.TargetID != "" {
			t.Fatalf("%s unexpectedly resolved %s", ownerFile, targetName)
		}
	}
}

func TestFrozenNuxtRegisteredPageDeletePlansFrameworkFallback(t *testing.T) {
	root := wave11LandingsRepo(t, `import { registerRoutes } from "./registerRoutes"
export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
  registerRoutes()
}
`)
	target := "packages/landings-module/src/runtime/pages/images.vue"
	writeFile(t, root, "packages/landings-module/src/registerRoutes.ts", `import { createResolver, extendPages } from "@nuxt/kit";
export function registerRoutes() {
  const resolver = createResolver(import.meta.url);
  extendPages((pages) => pages.push({ name: "_all_images", path: "/__images", file: resolver.resolve("./runtime/pages/images.vue") }));
}
`)
	writeFile(t, root, target, `<script setup lang="ts">
const { nextDelayed } = useStep()
</script>
<template><button @click="nextDelayed()">next</button></template>
`)
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, FreshEngine: true, MaxBeginBytes: 1048576}
	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, initial)
	warm := &graphstream.MemorySink{}
	warmResult, err := Run(context.Background(), eng, root, warm, opts)
	if err != nil {
		t.Fatal(err)
	}
	if warmResult.ParsedFiles != 0 || len(warm.CloneRecords()) != 0 {
		t.Fatalf("unchanged warmup parsed %d files and published %d records", warmResult.ParsedFiles, len(warm.CloneRecords()))
	}
	if err := os.Remove(filepath.Join(root, target)); err != nil {
		t.Fatal(err)
	}
	delta := &graphstream.MemorySink{}
	deltaResult, err := Run(context.Background(), eng, root, delta, opts)
	if err != nil {
		t.Fatalf("deleting a registered Nuxt page must be planned before frozen Begin: %v", err)
	}
	closedCompositionFallback := false
	for _, fallback := range deltaResult.Fallbacks {
		if fallback.Reason == "graphql/grpc/nuxt composition context changed after dependency closure" {
			closedCompositionFallback = true
		}
	}
	if !closedCompositionFallback {
		t.Fatalf("frozen plan did not record the post-closure composition fallback: %+v", deltaResult.Fallbacks)
	}
	if !beginOwnerSet(t, delta)["apps/landings/pages/Local.vue"] {
		t.Fatal("changed Nuxt composition must freeze the full TypeScript domain before Begin")
	}
	if err := cons.ApplyRecords(delta.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	coldOpts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, FreshEngine: true, MaxBeginBytes: 1048576}
	if _, err := Run(context.Background(), eng, root, cold, coldOpts); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, applyGraph(t, cold))
}
