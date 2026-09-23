package tsextractor

import (
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_LongCommentDoesNotSkipHandWrittenModule(t *testing.T) {
	long := strings.Repeat("M1.2 3.4", 400)
	files := map[string]string{
		"apps/mobile/src/components/figma/FigmaIcon.tsx": `
export type FigmaIconName = 'a'
function IconEyeOff() { return null
  /* Historical reconstruction
` + long + `
  */
}
export function FigmaIcon() { return null }
`,
		"apps/mobile/src/components/figma/SemanticStatusIcon.tsx": `
import { FigmaIcon } from './FigmaIcon'
export function SemanticStatusIcon() { return <FigmaIcon /> }
`,
	}
	got := extractAll(t, files, false)
	if _, ok := findFact(got, "apps/mobile/src/components/figma.FigmaIcon"); !ok {
		t.Fatalf("FigmaIcon missing: %v", factNames(got))
	}
	sem, ok := findFact(got, "apps/mobile/src/components/figma.SemanticStatusIcon")
	if !ok {
		t.Fatal("SemanticStatusIcon missing")
	}
	if !hasRelation(sem, facts.RelCalls, "apps/mobile/src/components/figma.FigmaIcon") {
		t.Fatalf("JSX call missing: %+v", sem.Relations)
	}
}

func TestExtract_LongStringLiteralDoesNotSkipHandWrittenModule(t *testing.T) {
	src := "export const PATH = \"" + strings.Repeat("M", minifiedLineThreshold+80) + "\";\nexport function draw() { return PATH.length }\n"
	got := extractAll(t, map[string]string{"src/icon.ts": src}, false)
	if _, ok := findFact(got, "src.draw"); !ok {
		t.Fatalf("draw missing: %v", factNames(got))
	}
}

func TestExtract_JSXSymbolOwnedCallsAndControls(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/DetailField.tsx": `export function DetailField(props: any) { return null }`,
		"src/DetailsGroup.tsx": `
import { DetailField } from './DetailField'
import { View } from 'react-native'
export function DetailsGroup() {
  return (
    <View>
      <DetailField label="a" />
      <DetailField label="b"></DetailField>
    </View>
  )
}
export function Outer() {
  const DetailField = () => null
  return <DetailField />
}
`,
		"src/ns.tsx": `
import * as UI from './DetailField'
export function Wrap() { return <UI.DetailField /> }
`,
	}, false)
	group, _ := findFact(ff, "src.DetailsGroup")
	n := 0
	for _, r := range group.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.DetailField" {
			n++
			if r.TargetFile != "src/DetailField.tsx" {
				t.Fatalf("target_file=%q", r.TargetFile)
			}
		}
	}
	if n != 1 {
		t.Fatalf("DetailsGroup DetailField calls=%d rels=%+v", n, group.Relations)
	}
	if !hasRelation(group, facts.RelCalls, "src.DetailField") {
		t.Fatal("paired/self-closing tags")
	}
	outer, _ := findFact(ff, "src.Outer")
	if hasRelation(outer, facts.RelCalls, "src.DetailField") {
		t.Fatalf("local shadow leaked: %+v", outer.Relations)
	}
	wrap, _ := findFact(ff, "src.Wrap")
	if !hasRelation(wrap, facts.RelCalls, "src.DetailField") {
		t.Fatalf("namespace member tag: %+v", wrap.Relations)
	}
	targets := fileRefTargets(ff, "src/DetailsGroup.tsx")
	if !hasTarget(targets, "src.DetailField") {
		t.Fatalf("file_ref lost: %v", targets)
	}
}

func TestExtract_ImportThenExportBridge(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/confettiPieces.ts": `export const CONFETTI_STATIC_PROGRESS = 1`,
		"src/Confetti.tsx": `
import { CONFETTI_STATIC_PROGRESS } from './confettiPieces'
export function Confetti() { return CONFETTI_STATIC_PROGRESS }
export { CONFETTI_STATIC_PROGRESS }
`,
		"src/TrainProjectedScreen.tsx": `
import { CONFETTI_STATIC_PROGRESS } from './Confetti'
export function TrainProjectedScreen() { return CONFETTI_STATIC_PROGRESS }
`,
		"src/local.ts": `
import { CONFETTI_STATIC_PROGRESS as imported } from './confettiPieces'
const CONFETTI_STATIC_PROGRESS = 2
export { CONFETTI_STATIC_PROGRESS }
export function useLocal() { return imported }
`,
		"src/fromLocal.ts": `
import { CONFETTI_STATIC_PROGRESS } from './local'
export function read() { return CONFETTI_STATIC_PROGRESS }
`,
	}, false)
	found := false
	for _, f := range ff {
		if f.Kind != facts.KindFileRef || f.File != "src/TrainProjectedScreen.tsx" {
			continue
		}
		for _, r := range f.Relations {
			if r.Kind == facts.RelCalls && strings.Contains(r.Target, "CONFETTI_STATIC_PROGRESS") {
				found = true
				if r.TargetFile != "src/confettiPieces.ts" {
					t.Fatalf("bridge target_file=%q rels=%+v", r.TargetFile, f.Relations)
				}
			}
		}
	}
	if !found {
		t.Fatalf("unresolved bridge file_ref: %v", fileRefTargets(ff, "src/TrainProjectedScreen.tsx"))
	}
	read, _ := findFact(ff, "src.read")
	for _, r := range read.Relations {
		if r.Kind == facts.RelCalls && strings.Contains(r.Target, "CONFETTI_STATIC_PROGRESS") && r.TargetFile == "src/confettiPieces.ts" {
			t.Fatalf("local declaration lost to import: %+v", read.Relations)
		}
	}
}

func TestExtract_JSXBlockRecoveryAndImportExportAlias(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/leaf.tsx": "export function Widget(){return null}",
		"src/use.tsx":  "import {Widget} from './leaf';export function Render(){ {const Widget=()=>null;const ignored=<Widget/>;}return <Widget/>}",
		"src/orig.ts":  "export function original(){return 1}",
		"src/bridge.ts": "import {original as local} from './orig'; export {local as publicName};",
		"src/call.ts":  "import {publicName as selected} from './bridge';export function run(){return selected()}",
	}, false)
	render, _ := findFact(ff, "src.Render")
	if !hasRelation(render, facts.RelCalls, "src.Widget") {
		t.Fatalf("imported Widget after nested block missing: %+v", render.Relations)
	}
	run, _ := findFact(ff, "src.run")
	found := false
	for _, r := range run.Relations {
		if r.Kind == facts.RelCalls && r.TargetFile == "src/orig.ts" {
			found = true
			if r.Target != "src.original" {
				t.Fatalf("alias bridge target=%s want src.original", r.Target)
			}
		}
	}
	if !found {
		t.Fatalf("alias bridge unresolved: %+v", run.Relations)
	}
}

func TestExtract_NuxtModuleRuntimePagesAreRegistrations(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/land-test9/package.json":    `{"name":"land-test9","dependencies":{"nuxt":"3.0.0","vue":"3.0.0"}}`,
		"apps/land-test9/nuxt.config.ts":  `export default defineNuxtConfig({})`,
		"apps/land-test9/pages/index.vue": `<template><div /></template>`,
		"apps/land-test9/pages/data_easy.vue": `<script setup>
definePageMeta({ alias: ['/__debug/data_easy'] })
</script><template><div /></template>`,
		"packages/landings-module/package.json": `{"name":"landings-module","devDependencies":{"nuxt":"3.0.0"}}`,
		"packages/landings-module/src/registerRoutes.ts": `
import { extendPages, createResolver } from '@nuxt/kit'
export function registerRoutes() {
  const resolver = createResolver(import.meta.url)
  extendPages((pages) => {
    pages.push({
      name: '_all_images', path: '/__images', file: resolver.resolve('./runtime/pages/images.vue'), mode: 'client',
    }, {
      name: '_debug_step', path: '/__debug/:page/step/:step', file: resolver.resolve('./runtime/pages/debug.client.vue'), mode: 'client',
    }, {
      name: '_debug', path: '/__debug', file: resolver.resolve('./runtime/pages/debug.client.vue'),
    }, {
      name: '_all_images_loader', path: '/___images', file: resolver.resolve('./runtime/pages/allImagesLoader.vue'),
    })
  })
}
export function bogus(extendPages: any) {
  extendPages((pages: any) => { pages.push({ path: '/fake', file: './runtime/pages/images.vue' }) })
}
`,
		"packages/landings-module/src/runtime/pages/images.vue":          `<template><div /></template>`,
		"packages/landings-module/src/runtime/pages/debug.client.vue":    `<template><div /></template>`,
		"packages/landings-module/src/runtime/pages/allImagesLoader.vue": `<template><div /></template>`,
	}, false)
	routes := map[string]facts.Fact{}
	for _, f := range ff {
		if f.Kind == facts.KindRoute {
			routes[f.Name] = f
		}
	}
	if _, ok := routes["/debug"]; ok {
		t.Fatalf("module runtime filename route leaked: %+v", routes["/debug"])
	}
	if _, ok := routes["/images"]; ok {
		t.Fatal("wrong /images")
	}
	if _, ok := routes["/allImagesLoader"]; ok {
		t.Fatal("wrong /allImagesLoader")
	}
	if _, ok := routes["/"]; !ok {
		t.Fatalf("app root missing: %v", routeNames(ff))
	}
	if _, ok := routes["/data_easy"]; !ok {
		t.Fatal("file-convention page missing")
	}
	if alias, ok := routes["/__debug/data_easy"]; !ok {
		t.Fatalf("alias missing: %v", routeNames(ff))
	} else if alias.Props["declaration"] != "definePageMeta" {
		t.Fatalf("alias props %+v", alias.Props)
	}
	for _, p := range []string{"/__images", "/__debug/:page/step/:step", "/__debug", "/___images"} {
		r, ok := routes[p]
		if !ok {
			t.Fatalf("registered %s missing in %v", p, routeNames(ff))
		}
		if r.File != "packages/landings-module/src/registerRoutes.ts" {
			t.Fatalf("%s owned by %s", p, r.File)
		}
		if r.Props["mode"] == "client" && p == "/__images" {
			// ok
		}
		if r.Props["handler"] == nil && r.Props["file"] == nil {
			t.Fatalf("%s missing handler", p)
		}
	}
	if _, ok := routes["/fake"]; ok {
		t.Fatal("shadowed extendPages emitted")
	}
}

func routeNames(ff []facts.Fact) []string {
	var out []string
	for _, f := range ff {
		if f.Kind == facts.KindRoute {
			out = append(out, f.Name)
		}
	}
	return out
}
