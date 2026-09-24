package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_Wave13ImportTypeGenericCallsKeepFollowingDeclarations(t *testing.T) {
	src := `
const actual = await importOriginal<typeof import('@onyxappio/checkout-vue')>()
const result = wrap<typeof import('./types').Options>(actual)
function createDomRect(width: number) { return { width } }
function afterGenericCalls() { return createDomRect(1) }
const callback = () => actual
const routeTable = { mountStripe: callback, key: 'actual object key' }
const runtime = import('./runtime-module')
`
	ff := extractAll(t, map[string]string{
		"src/provider.ts":  `export function helper() { return 1 }`,
		"src/candidate.ts": src,
	}, false)
	for _, name := range []string{"src.actual", "src.result", "src.createDomRect", "src.afterGenericCalls", "src.callback", "src.routeTable"} {
		if _, ok := findFact(ff, name); !ok {
			t.Errorf("missing declaration %s after generic import types; facts=%v", name, factNames(ff))
		}
	}
	for _, phantom := range []string{"src.mountStripe", "src.key", "src.actual#2"} {
		if _, ok := findFact(ff, phantom); ok {
			t.Errorf("parser emitted phantom declaration %s", phantom)
		}
	}
	var runtimeDependency bool
	for _, f := range ff {
		if f.Kind == facts.KindDependency && f.File == "src/candidate.ts" && f.PropString(facts.PropImportSpec) == "src/runtime-module" {
			runtimeDependency = true
		}
	}
	if !runtimeDependency {
		t.Fatal("runtime import() outside generic type arguments was lost")
	}
}

func TestExtract_Wave13CommonJSExportsMarkExistingLocals(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/mobile/src/moduleResolution/outputStyleEsmSourceResolver.js": `
const OUTPUT_STYLE_ESM_SOURCE_EXTENSIONS = ['ts']
function withOutputStyleEsmSourceResolver(config) { return config }
function pairLocal() { return 1 }
function nestedLocal() { return 2 }
function privateLocal() { return 3 }
function controlledLocal() { return 4 }
function lateLocal() { return 5 }
exports.withOutputStyleEsmSourceResolver = withOutputStyleEsmSourceResolver
module.exports = { pairAlias: pairLocal, nested: { nestedLocal } }
function lateExport() { exports.lateLocal = lateLocal }
if (false) { exports.controlledLocal = controlledLocal }
exports.inline = function inline(config) { return config }
exports.unknown = missingIdentifier
`,
		"apps/mobile/metroSentrySerializerCompat.js": `
function withSentryStaticSerializerCompatibility(expoSerializer, sentrySerializer) { return expoSerializer }
module.exports = { withSentryStaticSerializerCompatibility }
`,
	}, false)
	wantExported := []string{
		"apps/mobile/src/moduleResolution.withOutputStyleEsmSourceResolver",
		"apps/mobile/src/moduleResolution.pairLocal",
		"apps/mobile/src/moduleResolution.nestedLocal",
		"apps/mobile/src/moduleResolution.inline",
		"apps/mobile.withSentryStaticSerializerCompatibility",
	}
	for _, name := range wantExported {
		f, ok := findFact(ff, name)
		if !ok {
			t.Errorf("missing pre-existing CommonJS declaration %s", name)
			continue
		}
		if f.Props["exported"] != true {
			t.Errorf("%s exported=%v, want true", name, f.Props["exported"])
		}
	}
	for _, name := range []string{
		"apps/mobile/src/moduleResolution.OUTPUT_STYLE_ESM_SOURCE_EXTENSIONS",
		"apps/mobile/src/moduleResolution.privateLocal",
		"apps/mobile/src/moduleResolution.withOutputStyleEsmSourceResolver#2",
		"apps/mobile/src/moduleResolution.unknown",
		"apps/mobile.withOutputStyleEsmSourceResolver",
	} {
		if f, ok := findFact(ff, name); ok && f.Props["exported"] != true {
			// The final name is intentionally not a symbol alias: local declarations
			// keep their source identity and are marked by visibility metadata only.
			if strings.HasSuffix(name, ".withOutputStyleEsmSourceResolver") {
				t.Errorf("export alias was emitted as a separate local symbol: %+v", f)
			} else if strings.HasSuffix(name, ".OUTPUT_STYLE_ESM_SOURCE_EXTENSIONS") || strings.HasSuffix(name, ".privateLocal") {
				if f.Props["exported"] != false {
					t.Errorf("private local %s exported=%v, want false", name, f.Props["exported"])
				}
			} else {
				t.Errorf("unexpected symbol %s", name)
			}
		}
	}
	for _, name := range []string{"apps/mobile/src/moduleResolution.controlledLocal", "apps/mobile/src/moduleResolution.lateLocal"} {
		if f, ok := findFact(ff, name); !ok || f.Props["exported"] != false {
			t.Errorf("nested assignment changed %s export state: found=%v props=%v", name, ok, f.Props)
		}
	}
}

func TestExtract_Wave13ViteVersionQueryBindsSourceAndKeepsQuery(t *testing.T) {
	ff := extractAll(t, map[string]string{
		// A same-named local path must not turn the versioned package import below
		// into a repository source edge.
		"screenBackBus.ts": `export function packageShadow() { return 0 }`,
		"src/screenBackBus.ts": `export function provideScope() { return 1 }
export function useScope() { return 2 }
export function publishEvent() { return 3 }
`,
		"src/consumer.ts": `import { provideScope } from './screenBackBus?v=provider-copy'
import { useScope } from './screenBackBus?v=consumer-copy'
export function consume() { provideScope(); return useScope() }
`,
		"src/queryControls.ts": `import raw from './screenBackBus?raw'
import url from './screenBackBus?url'
import worker from './screenBackBus?worker'
import custom from './screenBackBus?custom=copy'
import packageCopy from 'screenBackBus?v=provider-copy'
import virtual from '#app?v=provider-copy'
export const controls = [raw, url, worker, custom, packageCopy, virtual]
`,
	}, false)
	consumer := fileRefFact(ff, "src/consumer.ts")
	for _, target := range []string{"src.provideScope", "src.useScope"} {
		if !hasCallToFile(consumer, target, "src/screenBackBus.ts") {
			t.Errorf("versioned relative import did not reference %s in source file: %+v", target, consumer.Relations)
		}
	}
	queries := 0
	for _, f := range ff {
		if f.Kind != facts.KindDependency || f.File != "src/consumer.ts" {
			continue
		}
		if strings.Contains(f.PropString(facts.PropImportSpec), "?v=") {
			queries++
			if f.PropString(facts.PropTargetFile) != "src/screenBackBus.ts" {
				t.Errorf("Vite query lost source target: %+v", f)
			}
		}
	}
	if queries != 2 {
		t.Fatalf("retained %d versioned import specs, want 2", queries)
	}
	for _, f := range ff {
		if f.Kind != facts.KindDependency || f.File != "src/queryControls.ts" {
			continue
		}
		spec := f.PropString(facts.PropImportSpec)
		if spec == "" || f.PropString(facts.PropTargetFile) == "src/screenBackBus.ts" {
			// Only a source-backed relative ?v= import strips its runtime instance key.
			t.Fatalf("unexpected query-control resolution: spec=%q target=%q", spec, f.PropString(facts.PropTargetFile))
		}
		if strings.Contains(spec, "?v=") && f.Props["source"] != "external" {
			t.Fatalf("package ?v= query lost external-package semantics: %+v", f)
		}
		if spec == "screenBackBus?v=provider-copy" && f.PropString(facts.PropTargetFile) != "" {
			t.Fatalf("versioned package import resolved to a same-named local file: %+v", f)
		}
	}
	if got, external := resolveImportPath("screenBackBus?v=provider-copy", "src", nil); !external || got != "screenBackBus?v=provider-copy" {
		t.Fatalf("package query changed: %q external=%v", got, external)
	}
	if got, external := resolveImportPath("#app?v=provider-copy", "src", nil); !external || got != "#app?v=provider-copy" {
		t.Fatalf("virtual query changed: %q external=%v", got, external)
	}
}

func TestExtract_Wave13ShorthandImportedValuesAreFileRefs(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/dep.ts": `export function helper() { return 1 }
export function helperType() { return 2 }
`,
		"src/shorthand.ts": `import { helper } from './dep'
export const options = { helper }
`,
		"src/pair.ts": `import { helper } from './dep'
export const options = { helper: helper }
`,
		"src/shadow.ts": `import { helper } from './dep'
export function local(helper: unknown) { return { helper } }
`,
		"src/typeOnly.ts": `import type { helper } from './dep'
export const options = { helper }
`,
		"src/destructure.ts": `import { helper } from './dep'
export function local(source: { helper: number }) {
  const { helper } = source
  return helper
}
`,
		"src/Options.vue": `<script setup lang="ts">
import { helper as vueHelper } from './dep'
const options = { vueHelper }
</script>
<template><div /></template>
`,
	}, false)
	for _, file := range []string{"src/shorthand.ts", "src/pair.ts", "src/Options.vue"} {
		fr := fileRefFact(ff, file)
		name := "src.helper"
		if file == "src/Options.vue" {
			// Aliased imports retain the source declaration identity, not the local name.
			name = "src.helper"
		}
		if !hasCallToFile(fr, name, "src/dep.ts") {
			t.Errorf("%s lost imported value reference %s: %+v", file, name, fr.Relations)
		}
	}
	for _, file := range []string{"src/shadow.ts", "src/typeOnly.ts", "src/destructure.ts"} {
		fr := fileRefFact(ff, file)
		if hasCallToFile(fr, "src.helper", "src/dep.ts") {
			t.Errorf("%s incorrectly treated a shadow, type-only binding, or destructuring binding as a value use: %+v", file, fr.Relations)
		}
	}
}

func TestExtractSession_Wave13DepthThreeTSConfigAlias(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"package.json":                                `{"name":"workspace"}`,
		"packages/landings-module/package.json":       `{"name":"@example/landings-module"}`,
		"packages/landings-module/test/tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["../src/*"]}}}`,
		"packages/landings-module/test/unit/composables/useDataLayer.test.ts": `import { useDataLayer } from '@/runtime/composables/useDataLayer'
export function exercise() { return useDataLayer({} as never) }
`,
		"packages/landings-module/src/runtime/composables/useDataLayer.ts": `export function useDataLayer(store: unknown) { return store }
`,
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := New().ExtractSession(context.Background(), root, []string{
		"packages/landings-module/test/unit/composables/useDataLayer.test.ts",
		"packages/landings-module/src/runtime/composables/useDataLayer.ts",
	}, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.ConfigPaths, "packages/landings-module/test/tsconfig.json") {
		t.Fatalf("depth-three tsconfig absent from semantic inputs: %v", result.ConfigPaths)
	}
	var resolved bool
	for _, fact := range result.Facts {
		if fact.Kind == facts.KindDependency && fact.File == "packages/landings-module/test/unit/composables/useDataLayer.test.ts" &&
			fact.PropString(facts.PropTargetFile) == "packages/landings-module/src/runtime/composables/useDataLayer.ts" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("depth-three paths alias did not bind useDataLayer to source file: %+v", result.Facts)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
