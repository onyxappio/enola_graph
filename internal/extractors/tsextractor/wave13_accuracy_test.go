package tsextractor

import (
	"bytes"
	"context"
	"fmt"
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
const spacedTypeResult = wrap < typeof import('./spaced-types').Options > (actual)
const awaited = async () => left<(await import('./runtime'))>(right)
function createDomRect(width: number) { return { width } }
function afterGenericCalls() { return createDomRect(1) }
const callback = () => actual
const routeTable = { mountStripe: callback, key: 'actual object key' }
const runtime = import('./runtime-module')
`
	js := `
const left = 1, right = 2
export const spaced = left < import('./spaced-runtime') > (right)
export const tight = left<import('./tight-runtime')>(right)
export async function awaitedComparison() { return left<(await import('./js-awaited-runtime'))>(right) }
const pattern = /call<typeof import("regex-only")>()/
const text = 'call<typeof import("string-only")>()'
// call<typeof import("comment-only")>()
export function afterRuntimeComparisons() { return 1 }
`
	jsx := `
const left = 1, right = 2
export const jsxComparison = left < import('./jsx-runtime') > (right)
export const view = <div />
`
	ff := extractAll(t, map[string]string{
		"src/provider.ts":   `export function helper() { return 1 }`,
		"src/candidate.ts":  src,
		"src/candidate.js":  js,
		"src/candidate.jsx": jsx,
		"src/runtime.ts":    `export const loaded = true`,
	}, false)
	for _, name := range []string{"src.actual", "src.result", "src.spacedTypeResult", "src.awaited", "src.createDomRect", "src.afterGenericCalls", "src.callback", "src.routeTable", "src.spaced", "src.tight", "src.awaitedComparison", "src.afterRuntimeComparisons", "src.jsxComparison", "src.view"} {
		if _, ok := findFact(ff, name); !ok {
			t.Errorf("missing declaration %s after generic import types; facts=%v", name, factNames(ff))
		}
	}
	for _, phantom := range []string{"src.mountStripe", "src.key", "src.actual#2"} {
		if _, ok := findFact(ff, phantom); ok {
			t.Errorf("parser emitted phantom declaration %s", phantom)
		}
	}
	var runtimeDependency, awaitedDependency, spacedDependency, tightDependency, jsAwaitedDependency, jsxDependency bool
	for _, f := range ff {
		if f.Kind != facts.KindDependency {
			continue
		}
		spec := f.PropString(facts.PropImportSpec)
		switch f.File + ":" + spec {
		case "src/candidate.ts:src/runtime-module":
			runtimeDependency = true
		case "src/candidate.ts:src/runtime":
			awaitedDependency = f.PropString(facts.PropTargetFile) == "src/runtime.ts"
		case "src/candidate.js:src/spaced-runtime":
			spacedDependency = true
		case "src/candidate.js:src/tight-runtime":
			tightDependency = true
		case "src/candidate.js:src/js-awaited-runtime":
			jsAwaitedDependency = true
		case "src/candidate.jsx:src/jsx-runtime":
			jsxDependency = true
		}
	}
	if !runtimeDependency {
		t.Fatal("runtime import() outside generic type arguments was lost")
	}
	if !awaitedDependency {
		t.Fatal("unambiguous awaited runtime import() in TypeScript was lost")
	}
	if !spacedDependency || !tightDependency || !jsAwaitedDependency || !jsxDependency {
		t.Fatalf("JavaScript runtime imports were lost: spaced=%v tight=%v awaited=%v jsx=%v", spacedDependency, tightDependency, jsAwaitedDependency, jsxDependency)
	}
	for _, file := range []string{"src/candidate.ts", "src/candidate.js"} {
		for _, f := range ff {
			if f.Kind == facts.KindDependency && f.File == file {
				spec := f.PropString(facts.PropImportSpec)
				if strings.Contains(spec, "regex-only") || strings.Contains(spec, "string-only") || strings.Contains(spec, "comment-only") {
					t.Errorf("import-like literal text emitted a dependency from %s: %+v", file, f)
				}
			}
		}
	}
}

func TestExtract_Wave13ImportTypeModesInVueAndSvelteScripts(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/Component.vue": `<script>
const left = 1, right = 2
export const vueRuntime = left < import('./vue-runtime') > (right)
</script>
<script lang="ts">
const awaited = async () => left < (await import('./vue-awaited-runtime')) > (right)
const typed = wrap < typeof import('./vue-types').Options > (1)
export function afterVueTypeImport() { return typed }
</script>`,
		"src/Widget.svelte": `<script>
const left = 1, right = 2
export const svelteRuntime = left < import('./svelte-runtime') > (right)
</script>
<script lang="ts">
const awaited = async () => left < (await import('./svelte-awaited-runtime')) > (right)
const typed = wrap < typeof import('./svelte-types').Options > (1)
export function afterSvelteTypeImport() { return typed }
</script>`,
		"src/vue-runtime.ts":            `export const loaded = true`,
		"src/vue-awaited-runtime.ts":    `export const loaded = true`,
		"src/svelte-runtime.ts":         `export const loaded = true`,
		"src/svelte-awaited-runtime.ts": `export const loaded = true`,
	}, false)
	for file, decl := range map[string]string{
		"src/Component.vue": "afterVueTypeImport",
		"src/Widget.svelte": "afterSvelteTypeImport",
	} {
		found := false
		for _, fact := range ff {
			if fact.Kind == facts.KindSymbol && fact.File == file && strings.HasSuffix(fact.Name, "."+decl) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s lost declaration %s following a typed script block", file, decl)
		}
	}
	want := map[string]map[string]bool{
		"src/Component.vue": {"vue-runtime": false, "vue-awaited-runtime": false},
		"src/Widget.svelte": {"svelte-runtime": false, "svelte-awaited-runtime": false},
	}
	for _, fact := range ff {
		if fact.Kind != facts.KindDependency {
			continue
		}
		for file, specs := range want {
			if fact.File == file {
				for spec := range specs {
					if strings.Contains(fact.PropString(facts.PropImportSpec), spec) {
						specs[spec] = true
					}
				}
			}
		}
	}
	for file, specs := range want {
		for spec, found := range specs {
			if !found {
				t.Errorf("%s lost runtime import dependency %s: facts=%v", file, spec, factNames(ff))
			}
		}
	}
}

func TestParseInputForTypeScriptProtectsLiteralsAndHandlesIncompleteInput(t *testing.T) {
	src := []byte("const pattern = /call<typeof import(\"regex-only\")>()/;\n" +
		"const text = 'call<typeof import(\"string-only\")>()';\n" +
		"// call<typeof import(\"comment-only\")>()\n" +
		"const template = `literal import('./template-text') ${wrap<typeof import('./interpolation')>()}`;\n" +
		"const result = wrap<typeof import('./types').Options>();\n")
	got := parseInputForTypeScript(src)
	if len(got) != len(src) {
		t.Fatalf("parser repair changed byte length: got=%d want=%d", len(got), len(src))
	}
	for _, literal := range []string{
		`/call<typeof import("regex-only")>()/`,
		`'call<typeof import("string-only")>()'`,
		`import('./template-text')`,
	} {
		if !bytes.Contains(got, []byte(literal)) {
			t.Errorf("literal text %q was changed: %s", literal, got)
		}
	}
	if bytes.Contains(got, []byte("import('./interpolation')")) || bytes.Contains(got, []byte("import('./types')")) {
		t.Fatalf("TypeScript ImportType spans were not repaired: %s", got)
	}
	if bytes.Contains(got, []byte("comment-only")) && !bytes.Contains(got, []byte(`import("comment-only")`)) {
		t.Fatalf("comment source was mutated: %s", got)
	}
	if _, err := safeParseInputForTypeScript([]byte{'\'', 'a', '\\'}); err != nil {
		t.Fatalf("incomplete editor input panicked: %v", err)
	}
	if _, err := safeParseInputForTypeScript([]byte("const value = `trailing\\")); err != nil {
		t.Fatalf("incomplete template input panicked: %v", err)
	}
}

func TestParseInputForTypeScriptVisitsInterpolationAfterTemplateEscape(t *testing.T) {
	tests := []struct {
		name          string
		src           []byte
		preserve      string
		maskedImport  string
		wantUnchanged bool
	}{
		{
			name:         "escaped newline before interpolation",
			src:          []byte("const value = `\\n${wrap<typeof import('./types').Options>()}`;"),
			preserve:     "`\\n${",
			maskedImport: "import('./types')",
		},
		{
			name:         "escaped backtick before interpolation",
			src:          []byte("const value = `\\`${wrap<typeof import('./backtick-types').Options>()}`;"),
			preserve:     "`\\`${",
			maskedImport: "import('./backtick-types')",
		},
		{
			name:          "escaped interpolation marker stays literal",
			src:           []byte("const value = `\\${wrap<typeof import('./literal-only').Options>()}`;"),
			preserve:      "\\${wrap<typeof import('./literal-only').Options>()}",
			wantUnchanged: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseInputForTypeScript(tt.src)
			if len(got) != len(tt.src) {
				t.Fatalf("parser repair changed byte length: got=%d want=%d", len(got), len(tt.src))
			}
			if !bytes.Contains(got, []byte(tt.preserve)) {
				t.Fatalf("escaped template text changed; want %q in %s", tt.preserve, got)
			}
			if tt.maskedImport != "" && bytes.Contains(got, []byte(tt.maskedImport)) {
				t.Fatalf("ImportType in real interpolation was not masked: %s", got)
			}
			if tt.wantUnchanged && !bytes.Equal(got, tt.src) {
				t.Fatalf("escaped literal interpolation was rewritten: %s", got)
			}
		})
	}

	// A final backslash is incomplete template text. The lexer must stop exactly
	// at EOF instead of returning an index past the input.
	incomplete := []byte("`trailing\\")
	var tokens []parserToken
	if end := lexTemplateLiteral(incomplete, 0, &tokens); end != len(incomplete) {
		t.Fatalf("incomplete template ended at %d, want EOF %d", end, len(incomplete))
	}
}

func TestExtract_Wave13TemplateEscapesDoNotEmitTypeImportsAsRuntimeDependencies(t *testing.T) {
	for _, tc := range []struct {
		name           string
		templateEscape string
	}{
		{name: "plain interpolation"},
		{name: "escaped newline before interpolation", templateEscape: `\n`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumer := "import { wrap } from './provider'\n" +
				fmt.Sprintf("export const value = `%s${wrap<typeof import('./types').Options>()}`\n", tc.templateEscape) +
				"export const escapedInterpolation = `\\${wrap<typeof import('./literal-only').Options>()}`\n" +
				"export function afterTemplate() { return wrap<string>() }\n"
			ff := extractAll(t, map[string]string{
				"src/consumer.ts": consumer,
				"src/provider.ts": `export function wrap<T>() { return 'ok' }`,
				"src/types.ts":    `export interface Options { value: string }`,
			}, false)

			var dependencies []facts.Fact
			for _, fact := range ff {
				if fact.Kind == facts.KindDependency && fact.File == "src/consumer.ts" {
					dependencies = append(dependencies, fact)
				}
			}
			if len(dependencies) != 1 {
				t.Fatalf("consumer dependencies=%+v, want only its static provider import", dependencies)
			}
			provider := dependencies[0]
			if provider.PropString(facts.PropImportSpec) != "src/provider" || provider.PropString(facts.PropTargetFile) != "src/provider.ts" {
				t.Errorf("provider dependency changed: %+v", provider)
			}
			if provider.Props["dynamic"] == true {
				t.Errorf("static provider import was marked dynamic: %+v", provider)
			}
			if _, ok := findFact(ff, "src.afterTemplate"); !ok {
				t.Errorf("declaration after escaped templates is missing: facts=%v", factNames(ff))
			}
		})
	}
}

func safeParseInputForTypeScript(src []byte) (out []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	return parseInputForTypeScript(src), nil
}

func TestParseInputForJavaScriptKeepsRuntimeComparisonSyntax(t *testing.T) {
	for _, src := range []string{
		`const x = left < import("./runtime") > (right);`,
		`const x = left<import("./runtime")>(right);`,
	} {
		got := parserInputForSyntax([]byte(src), parserSyntaxJavaScript)
		if !bytes.Equal(got, []byte(src)) {
			t.Errorf("JavaScript comparison was changed: %s", got)
		}
	}
}

func TestSourceSyntaxModesCoverTypeScriptJavaScriptAndComponents(t *testing.T) {
	src := []byte(`const value = wrap<typeof import("./types")>();`)
	for _, tc := range []struct {
		file       string
		typeScript bool
	}{
		{file: "src/file.ts", typeScript: true},
		{file: "src/file.tsx", typeScript: true},
		{file: "src/file.mts", typeScript: true},
		{file: "src/file.cts", typeScript: true},
		{file: "src/file.gts", typeScript: true},
		{file: "src/file.js"},
		{file: "src/file.jsx"},
		{file: "src/file.mjs"},
		{file: "src/file.cjs"},
		{file: "src/file.gjs"},
	} {
		got := parserInputForSyntax(src, sourceSyntaxForFile(tc.file))
		changed := !bytes.Equal(got, src)
		if changed != tc.typeScript {
			t.Errorf("%s used the wrong import-type parsing mode: changed=%v want=%v", tc.file, changed, tc.typeScript)
		}
	}
	for _, tc := range []struct {
		lang       string
		typeScript bool
	}{
		{lang: ""},
		{lang: "js"},
		{lang: "jsx"},
		{lang: "javascript"},
		{lang: "ts", typeScript: true},
		{lang: "tsx", typeScript: true},
		{lang: "typescript", typeScript: true},
	} {
		got := parserInputForSyntax(src, sourceSyntaxForEmbeddedScript(tc.lang))
		changed := !bytes.Equal(got, src)
		if changed != tc.typeScript {
			t.Errorf("embedded lang %q used the wrong import-type parsing mode: changed=%v want=%v", tc.lang, changed, tc.typeScript)
		}
	}
}

func BenchmarkParseInputForTypeScriptComparisonsWithUnrelatedImport(b *testing.B) {
	for _, count := range []int{1000, 2000, 4000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			src := []byte("import { runtime } from './unrelated-runtime';\n" + strings.Repeat("if (left < right) consume(left);\n", count))
			b.SetBytes(int64(len(src)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if got := parseInputForTypeScript(src); !bytes.Equal(got, src) {
					b.Fatal("runtime-only imports and comparisons should remain unchanged")
				}
			}
		})
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
		"apps/mobile/src/moduleResolution.controlledLocal",
		"apps/mobile/src/moduleResolution.lateLocal",
	} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Errorf("missing private/unexecuted local %s", name)
			continue
		}
		if f.Props["exported"] != false {
			t.Errorf("private/unexecuted local %s exported=%v, want false", name, f.Props["exported"])
		}
	}
	for _, name := range []string{
		"apps/mobile/src/moduleResolution.withOutputStyleEsmSourceResolver#2",
		"apps/mobile/src/moduleResolution.unknown",
		"apps/mobile.withOutputStyleEsmSourceResolver",
		"apps/mobile/src/moduleResolution.publicAlias",
	} {
		if f, ok := findFact(ff, name); ok {
			t.Errorf("phantom CommonJS alias symbol %s was emitted: %+v", name, f)
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
