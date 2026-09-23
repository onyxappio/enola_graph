package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestWave12ExtendPagesTargetMembershipMatchesCold(t *testing.T) {
	for _, authoritative := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "frozen-v2"}[authoritative], func(t *testing.T) {
			original := wave12RegisterRoutes("./runtime/pages/images.vue")
			root := setupTSRepo(t, map[string]string{
				"nuxt.config.ts":                                  `export default {}`,
				"packages/mod/src/registerRoutes.ts":              original,
				"packages/mod/src/runtime/pages/images.vue":       `<template><div>images</div></template>`,
				"packages/mod/src/runtime/pages/imagesLoader.vue": `<template><div>loader</div></template>`,
			})
			eng := testEngine(t, root)
			state := filepath.Join(root, ".enola", "state")
			opts := Options{StateDir: state, AuthoritativeFiles: authoritative}
			applied := NewConsumer()
			run := func() (*Result, *graphstream.MemorySink) {
				t.Helper()
				sink := &graphstream.MemorySink{}
				res, err := Run(context.Background(), eng, root, sink, opts)
				if err != nil {
					t.Fatal(err)
				}
				applyRun(t, applied, sink)
				return res, sink
			}
			checkCold := func() {
				t.Helper()
				sink := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative, ForceInitial: true}); err != nil {
					t.Fatal(err)
				}
				assertAppliedEqualsCold(t, applied, applyGraph(t, sink))
			}
			assertRegistrarInScope := func(sink *graphstream.MemorySink) {
				t.Helper()
				if authoritative && !beginOwnerSet(t, sink)["packages/mod/src/registerRoutes.ts"] {
					t.Fatal("frozen Begin omitted the resolver-owning registration file")
				}
			}

			first, _ := run()
			if first.TargetGeneration != 1 {
				t.Fatalf("initial generation=%d, want 1", first.TargetGeneration)
			}
			if !wave12GraphHasRouteTarget(applied, "/__images", "packages/mod/src/registerRoutes.ts", "packages/mod/src/runtime/pages.Images") {
				t.Fatal("initial /__images route did not resolve to images.vue")
			}
			checkCold()

			write := func(rel, body string) {
				t.Helper()
				full := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			remove := func(rel string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, rel)); err != nil {
					t.Fatal(err)
				}
			}
			rename := func(from, to string) {
				t.Helper()
				if err := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); err != nil {
					t.Fatal(err)
				}
			}

			// Renaming the target away and back changes membership while leaving
			// the registration source untouched. The resolver owner must be
			// included before the frozen replacement scope in both directions.
			rename("packages/mod/src/runtime/pages/images.vue", "packages/mod/src/runtime/pages/imagesRenamed.vue")
			_, renamedAway := run()
			assertRegistrarInScope(renamedAway)
			if wave12GraphHasRoute(applied, "/__images") {
				t.Fatal("route survived after its registered target was renamed away")
			}
			checkCold()
			rename("packages/mod/src/runtime/pages/imagesRenamed.vue", "packages/mod/src/runtime/pages/images.vue")
			_, renamedBack := run()
			assertRegistrarInScope(renamedBack)
			if !wave12GraphHasRouteTarget(applied, "/__images", "packages/mod/src/registerRoutes.ts", "packages/mod/src/runtime/pages.Images") {
				t.Fatal("route was not restored after its target was renamed back")
			}
			checkCold()

			write("packages/mod/src/registerRoutes.ts", wave12RegisterRoutes("./runtime/pages/imagesLoader.vue"))
			_, retarget := run()
			assertRegistrarInScope(retarget)
			if !wave12GraphHasRouteTarget(applied, "/__images", "packages/mod/src/registerRoutes.ts", "packages/mod/src/runtime/pages.ImagesLoader") {
				t.Fatal("retargeted /__images route did not resolve to imagesLoader.vue")
			}
			checkCold()

			// Restore the source registration while deleting its old target. The
			// route must disappear and agree with a fresh extraction.
			write("packages/mod/src/registerRoutes.ts", original)
			remove("packages/mod/src/runtime/pages/images.vue")
			_, deletedWithRetarget := run()
			assertRegistrarInScope(deletedWithRetarget)
			if wave12GraphHasRoute(applied, "/__images") {
				t.Fatal("route survived while its registered target was absent")
			}
			checkCold()

			// Now only the previously absent target returns. Its recorded resolver
			// candidate must take the clean registration owner into the delta.
			write("packages/mod/src/runtime/pages/images.vue", `<template><div>images restored</div></template>`)
			_, restored := run()
			assertRegistrarInScope(restored)
			if !wave12GraphHasRouteTarget(applied, "/__images", "packages/mod/src/registerRoutes.ts", "packages/mod/src/runtime/pages.Images") {
				t.Fatal("route was not restored when its missing target reappeared")
			}
			checkCold()

			// This path had no target at generation 1, so its resolver attempt tests
			// missing-to-present membership without editing the registration file.
			write("packages/mod/src/runtime/pages/late.vue", `<template><div>late</div></template>`)
			_, late := run()
			assertRegistrarInScope(late)
			if !wave12GraphHasRouteTarget(applied, "/late", "packages/mod/src/registerRoutes.ts", "packages/mod/src/runtime/pages.Late") {
				t.Fatal("missing-to-present /late resolver target did not bind")
			}
			checkCold()

			quiet := &graphstream.MemorySink{}
			before := applied.LastGeneration
			res, err := Run(context.Background(), eng, root, quiet, opts)
			if err != nil || len(quiet.CloneRecords()) != 0 || res.BaseGeneration != before || res.TargetGeneration != before {
				t.Fatalf("unchanged resolver inputs were not silent: result=%+v err=%v events=%d", res, err, len(quiet.CloneRecords()))
			}
		})
	}
}

func wave12RegisterRoutes(imagesTarget string) string {
	return `import { extendPages, createResolver } from '@nuxt/kit'
const resolver = createResolver(import.meta.url)
extendPages((pages) => {
  pages.push({ path: '/__images', file: resolver.resolve('` + imagesTarget + `') })
  pages.push({ path: '/late', file: resolver.resolve('./runtime/pages/late.vue') })
})
`
}

func wave12GraphHasRoute(c *Consumer, route string) bool {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind == facts.KindRoute && n.Name == route {
				return true
			}
		}
	}
	return false
}

func wave12GraphHasRouteTarget(c *Consumer, route, owner, target string) bool {
	for ownerKey, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Kind != facts.KindRoute || n.Name != route || n.File != owner {
				continue
			}
			for _, edge := range c.Edges[ownerKey] {
				if edge.FromID == n.ID && edge.Kind == facts.RelHandledBy && edge.TargetName == target {
					return true
				}
			}
		}
	}
	return false
}

func TestWave12DeletedNuxtAutoimportIsPlannedBeforeFrozenBegin(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"nuxt.config.ts":                 `export default {}`,
		"composables/cloudflareGeo.ts":   `export function useCloudflareGeo() { return 'geo' }`,
		"app.vue":                        `<script setup lang="ts">const geo = useCloudflareGeo()</script><template><div>{{ geo }}</div></template>`,
		"components/UnusedComponent.vue": `<template><span>before</span></template>`,
	})
	eng := testEngine(t, root)
	state := filepath.Join(root, ".enola", "state")
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	applied := NewConsumer()
	run := func() (*Result, *graphstream.MemorySink) {
		t.Helper()
		sink := &graphstream.MemorySink{}
		res, err := Run(context.Background(), eng, root, sink, opts)
		if err != nil {
			t.Fatal(err)
		}
		applyRun(t, applied, sink)
		return res, sink
	}
	coldCheck := func() {
		t.Helper()
		sink := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
			t.Fatal(err)
		}
		assertAppliedEqualsCold(t, applied, applyGraph(t, sink))
	}

	run()
	if err := os.Remove(filepath.Join(root, "composables/cloudflareGeo.ts")); err != nil {
		t.Fatal(err)
	}
	deleted, sink := run()
	if !beginOwnerSet(t, sink)["app.vue"] {
		t.Fatal("deleted autoimport did not include its clean Vue consumer in frozen Begin")
	}
	coldCheck()
	if deleted.ParsedFiles == 0 {
		t.Fatal("autoimport deletion parsed no TypeScript/SFC inputs")
	}

	// Restoring the provider changes the global autoimport composition again;
	// the consumer must be replaced and the result must equal a fresh target run.
	provider := filepath.Join(root, "composables/cloudflareGeo.ts")
	if err := os.MkdirAll(filepath.Dir(provider), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider, []byte(`export function useCloudflareGeo() { return 'geo restored' }`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, restoredSink := run()
	if !beginOwnerSet(t, restoredSink)["app.vue"] {
		t.Fatal("provider restoration omitted its clean Vue consumer from frozen Begin")
	}
	coldCheck()

	// A body edit to a different component leaves the autoimport name surface
	// unchanged, so it stays a one-file parse/scope instead of broadening all.
	component := filepath.Join(root, "components/UnusedComponent.vue")
	if err := os.WriteFile(component, []byte(`<template><span>after</span></template>`), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelated, unrelatedSink := run()
	if beginOwnerSet(t, unrelatedSink)["app.vue"] {
		t.Fatalf("unrelated component body edit broadened to the autoimport consumer: parsed=%d fallbacks=%+v owners=%v", unrelated.ParsedFiles, unrelated.Fallbacks, beginOwnerSet(t, unrelatedSink))
	}
	if unrelated.ParsedFiles != 1 {
		t.Fatalf("unrelated body edit parsed %d files, want 1", unrelated.ParsedFiles)
	}
	coldCheck()

	// Delete the provider again after the unrelated body edit. The clean
	// consumer must still be in the pre-Begin owner set, and the committed graph
	// must remain equal to a fresh analysis.
	if err := os.Remove(provider); err != nil {
		t.Fatal(err)
	}
	deletedAfterInertEdit, afterInertSink := run()
	if !beginOwnerSet(t, afterInertSink)["app.vue"] {
		t.Fatal("deletion after an inert edit omitted the clean Vue consumer from frozen Begin")
	}
	if deletedAfterInertEdit.ParsedFiles == 0 {
		t.Fatal("deletion after an inert edit parsed no TypeScript/SFC inputs")
	}
	coldCheck()
	if err := os.WriteFile(provider, []byte(`export function useCloudflareGeo() { return 'geo restored again' }`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, restoredAfterInertSink := run()
	if !beginOwnerSet(t, restoredAfterInertSink)["app.vue"] {
		t.Fatal("provider restoration after an inert edit omitted its clean Vue consumer")
	}
	coldCheck()
}

func TestPublishedWave12CachedUpgradeFromV316(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"nuxt.config.ts":                            `export default {}`,
		"server/api/geo.get.ts":                     `export default defineEventHandler(() => ({ ok: true }))`,
		"packages/mod/src/runtime/pages/images.vue": `<template><button /></template>`,
		"packages/mod/src/Blocks/index.ts":          `import Blocks from '../runtime/pages/images.vue'; export { Blocks as Images }`,
		"packages/mod/src/story.vue":                `<script setup lang="ts">import { Images } from './Blocks'</script><template><Images /></template>`,
		"packages/mod/src/registerRoutes.ts":        wave12RegisterRoutes("./runtime/pages/images.vue"),
	})
	eng := testEngine(t, root)
	state := filepath.Join(root, ".enola", "state")
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	first := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, first, opts); err != nil {
		t.Fatal(err)
	}
	committed := applyGraph(t, first)
	if !wave12GraphHasRouteTarget(committed, "/__images", "packages/mod/src/registerRoutes.ts", "packages/mod/src/runtime/pages.Images") {
		t.Fatal("wave12 baseline fixture did not contain its default component route")
	}
	st, err := loadCommittedState(state)
	if err != nil || st == nil {
		t.Fatalf("load committed state: %v %#v", err, st)
	}
	st.ExtractorVersion = "v316"
	for _, file := range st.Files {
		if file != nil && file.TS != nil {
			file.TS.ResolutionSpecs = nil
		}
	}
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	upgrade := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, upgrade, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v316 migration parsed no files")
	}
	if err := committed.ApplyRecords(upgrade.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, committed, applyGraph(t, cold))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, root, quiet, opts)
	if err != nil || again.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 {
		t.Fatalf("post-migration unchanged run parsed=%d events=%d err=%v", again.ParsedFiles, len(quiet.CloneRecords()), err)
	}
	if engine.ExtractorVersion() == "v316" {
		t.Fatal("cache upgrade test requires a version newer than v316")
	}
}
