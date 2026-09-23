package tsextractor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_Wave12LoopHeaderBindingsShadowOnlyInsideLoop(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/token.ts": `export function token() { return 1 }`,
		"src/loop.ts": `
import { token } from './token'
export function forOf(raw: string[]) {
  for (const token of splitTokenList(raw)) {
    token()
    consume(token)
  }
}
export function afterForOf(raw: string[]) {
  for (let [token] of splitTokenList(raw)) { token() }
  token()
}
export function afterForIn(source: Record<string, unknown>) {
  for (let { token } in source) token()
  token()
}
export function forIn(source: Record<string, unknown>) {
  for (const { token } in source) token()
}
export function cStyle() {
  for (let { token } = { token: 0 }; token < 1; token++) token()
}
export function splitTokenList(raw: string[]) { return raw }
export function consume(_token: unknown) {}
`,
		"src/tdz.ts": `
import { token } from './token'
export function forOfRhs() { for (const token of token()) {} }
export function forInRhs() { for (const token in token) {} }
`,
	}, false)

	for _, name := range []string{"src.forOf", "src.afterForOf", "src.forOfRhs", "src.afterForIn", "src.forIn", "src.forInRhs", "src.cStyle"} {
		if _, ok := findFact(ff, name); !ok {
			t.Fatalf("missing %s; facts=%v", name, factNames(ff))
		}
	}
	for _, name := range []string{"src.forOf", "src.forIn", "src.cStyle", "src.forOfRhs", "src.forInRhs"} {
		f, _ := findFact(ff, name)
		if hasRelation(f, facts.RelCalls, "src.token") {
			t.Errorf("%s resolved a loop-local or TDZ token to the imported function: %+v", name, f.Relations)
		}
	}
	for _, name := range []string{"src.afterForOf", "src.afterForIn"} {
		after, _ := findFact(ff, name)
		if !hasRelation(after, facts.RelCalls, "src.token") {
			t.Errorf("loop binding leaked past its scope and hid imported token in %s: %+v", name, after.Relations)
		}
	}
	for _, name := range []string{"src.forOfRhs", "src.forInRhs"} {
		rhs, _ := findFact(ff, name)
		if hasRelation(rhs, facts.RelCalls, "src.token") {
			t.Errorf("same-name %s RHS is in the loop binding's TDZ, not the outer import scope: %+v", name, rhs.Relations)
		}
	}
	for _, name := range []string{"src.forOf", "src.afterForOf"} {
		f, _ := findFact(ff, name)
		if !hasRelation(f, facts.RelCalls, "src.splitTokenList") {
			t.Errorf("genuine splitTokenList call was suppressed in %s: %+v", name, f.Relations)
		}
	}
	forOf, _ := findFact(ff, "src.forOf")
	var splitCalls int
	for _, rel := range forOf.Relations {
		if rel.Kind == facts.RelCalls && rel.Target == "src.splitTokenList" {
			splitCalls++
		}
	}
	if splitCalls != 1 {
		t.Errorf("for-of RHS call should produce one call relation, got %d: %+v", splitCalls, forOf.Relations)
	}
	if calls := tsStrSlice(forOf, "calls_in_loop"); !tsContains(calls, "src.consume") || tsContains(calls, "src.splitTokenList") {
		t.Errorf("RHS executes once outside repeated-body metrics; calls_in_loop = %v, want consume only", calls)
	}
	if got := tsIntProp(t, forOf, "loop_count"); got != 1 {
		t.Errorf("loop_count = %d, want one loop after visiting the RHS", got)
	}
	fileRef, ok := findFact(ff, "src/loop.ts")
	if !ok || fileRef.Kind != facts.KindFileRef {
		t.Fatalf("missing file_ref for src/loop.ts; facts=%v", factNames(ff))
	}
	if !hasTargetFileRelation(fileRef, facts.RelCalls, "src.splitTokenList", "src/loop.ts") {
		t.Errorf("file_ref lost genuine unrelated splitTokenList RHS calls: %+v", fileRef.Relations)
	}
	tdzFileRef, ok := findFact(ff, "src/tdz.ts")
	if !ok || tdzFileRef.Kind != facts.KindFileRef {
		t.Fatalf("missing file_ref for src/tdz.ts; facts=%v", factNames(ff))
	}
	if hasRelation(tdzFileRef, facts.RelCalls, "src.token") {
		t.Errorf("file_ref resolved same-name for-of/in TDZ references to the imported token: %+v", tdzFileRef.Relations)
	}
}

func TestExtract_Wave12ProductLoopBindingDoesNotCrossBindFileRef(t *testing.T) {
	const fixtureRoot = "testdata/wave12/candidate56"
	const owner = "apps/mobile/scripts/require-figma-api-token.mjs"
	const sibling = "apps/mobile/scripts/refresh-figma-train-copy.mjs"
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(fixtureRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read exact Product fixture %s: %v", rel, err)
		}
		return string(b)
	}
	ff := extractAll(t, map[string]string{
		owner:   read(owner),
		sibling: read(sibling),
	}, false)

	var fileRef *facts.Fact
	for i := range ff {
		if ff[i].Kind == facts.KindFileRef && ff[i].File == owner {
			fileRef = &ff[i]
			break
		}
	}
	if fileRef == nil {
		t.Fatalf("missing Product file_ref for %s; facts=%v", owner, factNames(ff))
	}
	for _, rel := range fileRef.Relations {
		if rel.Kind == facts.RelCalls && rel.Target == "apps/mobile/scripts.token" {
			t.Errorf("loop-local token was cross-bound from the file_ref pass to the private sibling: %+v; all refs: %+v", rel, fileRef.Relations)
		}
	}
	if !hasTargetFileRelation(*fileRef, facts.RelCalls, "apps/mobile/scripts.splitTokenList", owner) {
		t.Errorf("genuine Product splitTokenList call was lost from file_ref: %+v", fileRef.Relations)
	}
}

func TestExtract_Wave12DefaultImportThenLocalBarrelExportKeepsVueOrigin(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/shared/src/RadioGroup/Blocks/BlocksRadioGroup.vue": `<script setup lang="ts">
export interface Props { disabled?: boolean }
</script><template><button /></template>`,
		"packages/shared/src/RadioGroup/Blocks/index.ts": `
import BlocksRadioGroup from './BlocksRadioGroup.vue'
import type { Props } from './BlocksRadioGroup.vue'
export { BlocksRadioGroup, Props }
`,
		"apps/story/src/RadioStory.vue": `<script setup lang="ts">
import { BlocksRadioGroup } from '../../../packages/shared/src/RadioGroup/Blocks'
</script><template><BlocksRadioGroup /></template>`,
		"apps/story/src/DirectStory.vue": `<script setup lang="ts">
import BlocksRadioGroup from '../../../packages/shared/src/RadioGroup/Blocks/BlocksRadioGroup.vue'
</script><template><BlocksRadioGroup /></template>`,
	}, false)

	for _, story := range []string{"apps/story/src.RadioStory", "apps/story/src.DirectStory"} {
		f, ok := findFact(ff, story)
		if !ok {
			t.Fatalf("missing story component %s; facts=%v", story, factNames(ff))
		}
		if !hasTargetFileRelation(f, facts.RelCalls, "packages/shared/src/RadioGroup/Blocks.BlocksRadioGroup", "packages/shared/src/RadioGroup/Blocks/BlocksRadioGroup.vue") {
			t.Errorf("%s did not resolve through the default import barrel to the SFC: %+v", story, f.Relations)
		}
	}
	if f, ok := findFact(ff, "packages/shared/src/RadioGroup/Blocks.BlocksRadioGroup"); !ok || f.PropString("web_component") != "component" {
		t.Fatalf("origin component fact missing: %+v, found=%v", f, ok)
	}
}

func TestExtract_Wave12DefaultImportReexportOriginLifecycle(t *testing.T) {
	files := map[string]string{
		"packages/a/src/BlocksRadioGroup.vue": `<template><button /></template>`,
		"packages/b/src/BlocksRadioGroup.vue": `<template><button /></template>`,
		"packages/a/src/index.ts":             `import Blocks from './BlocksRadioGroup.vue'; export { Blocks as BlocksRadioGroup }`,
		"packages/b/src/index.ts":             `import Blocks from './BlocksRadioGroup.vue'; export { Blocks as BlocksRadioGroup }`,
		"packages/shared/index.ts":            `import { BlocksRadioGroup as A } from '../a/src'; import { BlocksRadioGroup as B } from '../b/src'; export { A as Shared, B as Shared }`,
		"cycle-a.ts":                          `import B from './cycle-b'; export { B as default }`,
		"cycle-b.ts":                          `import A from './cycle-a'; export { A as default }`,
	}
	known := map[string]bool{}
	for file := range files {
		known[file] = true
	}
	read := func(file string) []byte { return []byte(files[file]) }
	leaf, name, kind := followNamedExportFile("packages/a/src/index.ts", "BlocksRadioGroup", read, nil, known, newNamedExportCache(), nil)
	if kind != followOne || leaf != "packages/a/src/BlocksRadioGroup.vue" || name != "BlocksRadioGroup" {
		t.Fatalf("package-local default origin = (%q, %q, %v), want package A's component", leaf, name, kind)
	}
	_, _, kind = followNamedExportFile("packages/shared/index.ts", "Shared", read, nil, known, newNamedExportCache(), nil)
	if kind != followMany {
		t.Fatalf("two default-import origins were not kept ambiguous: %v", kind)
	}
	_, _, kind = followNamedExportFile("cycle-a.ts", "default", read, nil, known, newNamedExportCache(), nil)
	if kind != followNone {
		t.Fatalf("cyclic default-import barrel produced an origin: %v", kind)
	}

	delete(known, "packages/a/src/BlocksRadioGroup.vue")
	_, _, kind = followNamedExportFile("packages/a/src/index.ts", "BlocksRadioGroup", read, nil, known, newNamedExportCache(), nil)
	if kind != followNone {
		t.Fatalf("deleted default origin was treated as a local barrel declaration: %v", kind)
	}
	files["packages/a/src/index.ts"] = `import Blocks from './RenamedRadioGroup.vue'; export { Blocks as BlocksRadioGroup }`
	files["packages/a/src/RenamedRadioGroup.vue"] = `<template><button /></template>`
	known["packages/a/src/RenamedRadioGroup.vue"] = true
	leaf, _, kind = followNamedExportFile("packages/a/src/index.ts", "BlocksRadioGroup", read, nil, known, newNamedExportCache(), nil)
	if kind != followOne || leaf != "packages/a/src/RenamedRadioGroup.vue" {
		t.Fatalf("renamed default origin = (%q, %v), want the new package-local SFC", leaf, kind)
	}
}

func TestExtract_Wave12NuxtRoutesHandleOnlyUniqueDefaultTargets(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"nuxt.config.ts": `export default {}`,
		"apps/landings/server/api/geo.get.ts": `
function helper() { return 'not the route handler' }
export default defineEventHandler(() => ({ ok: true }))
`,
		"packages/landings-module/src/registerServerProxy.ts": `
import { addServerHandler, createResolver } from '@nuxt/kit'
const resolver = createResolver(import.meta.url)
addServerHandler({ route: '/sw-web-push.js', handler: resolver.resolve('./runtime/server/webPushHandler') })
addServerHandler({ route: '/api/geo', handler: resolver.resolve('./runtime/server/geo.get') })
addServerHandler({ route: '/land-next/config/config.js', handler: resolver.resolve('./runtime/server/TestHandler') })
addServerHandler({ route: '/no-default', handler: resolver.resolve('./runtime/server/helperOnly') })
addServerHandler({ route: '/land/**', handler: resolver.resolve('./runtime/server/landHandler') })
addServerHandler({ route: '/land/**', handler: resolver.resolve('./runtime/server/mockHandler') })
addServerHandler({ route: '/platform/**', handler: resolver.resolve('./runtime/server/landHandler') })
addServerHandler({ route: '/platform/**', handler: resolver.resolve('./runtime/server/mockHandler') })
`,
		"packages/landings-module/src/runtime/server/webPushHandler.ts": `export default function WebPushHandler() { return null }`,
		"packages/landings-module/src/runtime/server/geo.get.ts":        `export default function GeoGet() { return null }`,
		"packages/landings-module/src/runtime/server/TestHandler.ts":    `export default function TestHandler() { return null }`,
		"packages/landings-module/src/runtime/server/helperOnly.ts":     `export function helperOnly() { return null }`,
		"packages/landings-module/src/runtime/server/landHandler.ts":    `export default function LandHandler() { return null }`,
		"packages/landings-module/src/runtime/server/mockHandler.ts":    `export default function MockHandler() { return null }`,
		"packages/landings-module/src/registerRoutes.ts": `
import { extendPages, createResolver } from '@nuxt/kit'
const resolver = createResolver(import.meta.url)
extendPages((pages) => {
  pages.push({ path: '/__images', file: resolver.resolve('./runtime/pages/images.vue') })
  pages.push({ path: '/___images', file: resolver.resolve('./runtime/pages/allImagesLoader.vue') })
  pages.push({ path: '/__debug', file: resolver.resolve('./runtime/pages/debug.client.vue') })
  pages.push({ path: '/__debug/:page/step/:step', file: resolver.resolve('./runtime/pages/debug.client.vue') })
})
`,
		"packages/landings-module/src/runtime/pages/images.vue":          `<template><div>images</div></template>`,
		"packages/landings-module/src/runtime/pages/allImagesLoader.vue": `<template><div>all images</div></template>`,
		"packages/landings-module/src/runtime/pages/debug.client.vue":    `<template><div>debug</div></template>`,
	}, true)

	wants := map[string]struct{ target, file string }{
		"/api/geo@apps/landings/server/api/geo.get.ts":                                    {"apps/landings/server/api.GeoGet", "apps/landings/server/api/geo.get.ts"},
		"/sw-web-push.js@packages/landings-module/src/registerServerProxy.ts":             {"packages/landings-module/src/runtime/server.WebPushHandler", "packages/landings-module/src/runtime/server/webPushHandler.ts"},
		"/api/geo@packages/landings-module/src/registerServerProxy.ts":                    {"packages/landings-module/src/runtime/server.GeoGet", "packages/landings-module/src/runtime/server/geo.get.ts"},
		"/land-next/config/config.js@packages/landings-module/src/registerServerProxy.ts": {"packages/landings-module/src/runtime/server.TestHandler", "packages/landings-module/src/runtime/server/TestHandler.ts"},
		"/__images@packages/landings-module/src/registerRoutes.ts":                        {"packages/landings-module/src/runtime/pages.Images", "packages/landings-module/src/runtime/pages/images.vue"},
		"/___images@packages/landings-module/src/registerRoutes.ts":                       {"packages/landings-module/src/runtime/pages.AllImagesLoader", "packages/landings-module/src/runtime/pages/allImagesLoader.vue"},
		"/__debug@packages/landings-module/src/registerRoutes.ts":                         {"packages/landings-module/src/runtime/pages.DebugClient", "packages/landings-module/src/runtime/pages/debug.client.vue"},
		"/__debug/:page/step/:step@packages/landings-module/src/registerRoutes.ts":        {"packages/landings-module/src/runtime/pages.DebugClient", "packages/landings-module/src/runtime/pages/debug.client.vue"},
	}
	for _, f := range ff {
		if f.Kind != facts.KindRoute {
			continue
		}
		key := f.Name + "@" + f.File
		want, ok := wants[key]
		if !ok {
			continue
		}
		if !hasTargetFileRelation(f, facts.RelHandledBy, want.target, want.file) {
			t.Errorf("%s missing handled_by %s -> %s: %+v", key, want.target, want.file, f.Relations)
		}
		delete(wants, key)
	}
	if len(wants) > 0 {
		t.Fatalf("missing expected route facts: %v", wants)
	}
	for _, f := range ff {
		if f.Kind != facts.KindRoute || f.File != "packages/landings-module/src/registerServerProxy.ts" {
			continue
		}
		if f.Name == "/land/**" || f.Name == "/platform/**" {
			if _, ok := f.Props["handler"]; ok || hasAnyRelation(f, facts.RelHandledBy) {
				t.Fatalf("conditional route handlers must remain ambiguous: %+v", f)
			}
		}
		if f.Name == "/no-default" && hasAnyRelation(f, facts.RelHandledBy) {
			t.Fatalf("non-default same-file helper was promoted to a route handler: %+v", f)
		}
	}
}

func hasTargetFileRelation(f facts.Fact, kind, target, targetFile string) bool {
	for _, r := range f.Relations {
		if r.Kind == kind && r.Target == target && r.TargetFile == targetFile {
			return true
		}
	}
	return false
}

func hasAnyRelation(f facts.Fact, kind string) bool {
	for _, r := range f.Relations {
		if r.Kind == kind {
			return true
		}
	}
	return false
}
