package tsextractor

import (
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_Wave11ConstructorParameterProperties(t *testing.T) {
	analytics := `
export class Semaphore {
  constructor(private readonly max: number) {}
  tryAcquire() { return this.max }
}
export class Mixed {
  constructor(public a: number, protected b: string, readonly c: boolean, plain: number) {
    void this.a; void this.b; void this.c; void plain
  }
}
export class Shadow {
  readonly max = 1
  constructor(private readonly max: number) {}
}
`
	ff := extractAll(t, map[string]string{
		"src/analytics.ts": analytics,
	}, false)
	sem, ok := findFact(ff, "src.Semaphore.max")
	if !ok {
		t.Fatal("missing Semaphore.max parameter property")
	}
	if sem.Props["symbol_kind"] != facts.SymbolConstant {
		t.Fatalf("Semaphore.max kind=%v want constant", sem.Props["symbol_kind"])
	}
	if sem.Props["exported"] != false {
		t.Fatal("private parameter property must not be exported")
	}
	if !hasRelation(sem, facts.RelDeclares, "src.Semaphore") {
		t.Fatal("member must declare its class")
	}
	mixed := map[string]string{
		"src.Mixed.a": facts.SymbolVariable,
		"src.Mixed.b": facts.SymbolVariable,
		"src.Mixed.c": facts.SymbolConstant,
	}
	for name, kind := range mixed {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != kind {
			t.Errorf("%s kind=%v want %s", name, f.Props["symbol_kind"], kind)
		}
	}
	if _, ok := findFact(ff, "src.Mixed.plain"); ok {
		t.Fatal("plain constructor parameter must not become a field")
	}
	sh, ok := findFact(ff, "src.Shadow.max")
	if !ok {
		t.Fatal("explicit field must remain")
	}
	fieldLine := 0
	for i, line := range strings.Split(analytics, "\n") {
		if strings.TrimSpace(line) == "readonly max = 1" {
			fieldLine = i + 1
			break
		}
	}
	if fieldLine == 0 || sh.Line != fieldLine {
		t.Fatalf("Shadow.max must be the explicit field at line %d, got %d", fieldLine, sh.Line)
	}
	count := 0
	for _, f := range ff {
		if f.Name == "src.Shadow.max" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("name collision emitted %d Shadow.max facts", count)
	}
}

func TestExtract_Wave11DefineEndpointFunctionURL(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/define.ts": `export function defineEndpoint(cfg: object) { return cfg }`,
		"src/image-set-as-main.endpoint.ts": `
import { defineEndpoint } from './define'
export const setImageAsMainEndpoint = defineEndpoint({
  name: 'setImageAsMain',
  method: 'POST',
  url: (id: string) => ` + "`/platform/image/set-as-main/${id}`" + `,
})
export const literal = defineEndpoint({
  method: 'GET',
  url: '/platform/image/control',
})
export const callback = {
  method: 'POST',
  onClick: (id: string) => ` + "`/not-a-route/${id}`" + `,
}
export const dynamic = defineEndpoint({
  method: 'POST',
  url: (id: string) => compute(id),
})
`,
	}, false)
	if _, ok := wave10Route(ff, "/platform/image/set-as-main/{}"); !ok {
		t.Fatalf("function-valued defineEndpoint url missing: %v", clientRouteNames(ff))
	}
	if _, ok := wave10Route(ff, "/platform/image/control"); !ok {
		t.Fatal("literal url control missing")
	}
	if _, ok := wave10Route(ff, "/not-a-route/{}"); ok {
		t.Fatal("unrelated callback must not emit a client route")
	}
}

func TestExtract_Wave11DefineEndpointURLExpressionBounds(t *testing.T) {
	src := `
import { defineEndpoint } from './define'
export const setImageAsMainEndpoint = defineEndpoint({
  name: 'setImageAsMain',
  method: 'POST',
  url: (id: string) => ` + "`/platform/image/set-as-main/${id}`" + `,
})
`
	ff := extractAll(t, map[string]string{
		"src/define.ts":                     `export function defineEndpoint(cfg: object) { return cfg }`,
		"src/image-set-as-main.endpoint.ts": src,
	}, false)
	if _, ok := wave10Route(ff, "/platform/image/set-as-main/{}"); !ok {
		t.Fatalf("literal template missing: %v", clientRouteNames(ff))
	}

	concat := strings.Replace(src, "`/platform/image/set-as-main/${id}`", "`/platform/image/set-as-main/${id}` + '/details'", 1)
	ff = extractAll(t, map[string]string{
		"src/define.ts":                     `export function defineEndpoint(cfg: object) { return cfg }`,
		"src/image-set-as-main.endpoint.ts": concat,
	}, false)
	if _, ok := wave10Route(ff, "/platform/image/set-as-main/{}"); ok {
		t.Fatalf("concat must not emit truncated path: %v", clientRouteNames(ff))
	}
	if _, ok := wave10Route(ff, "/platform/image/set-as-main/{}/details"); !ok {
		t.Fatalf("concat suffix route missing: %v", clientRouteNames(ff))
	}

	repl := strings.Replace(src, "`/platform/image/set-as-main/${id}`", "`/platform/image/set-as-main/${id}`.replace('/platform/image/', '/platform/photo/')", 1)
	ff = extractAll(t, map[string]string{
		"src/define.ts":                     `export function defineEndpoint(cfg: object) { return cfg }`,
		"src/image-set-as-main.endpoint.ts": repl,
	}, false)
	if _, ok := wave10Route(ff, "/platform/image/set-as-main/{}"); ok {
		t.Fatalf("replace must not emit pre-transform path: %v", clientRouteNames(ff))
	}
	if _, ok := wave10Route(ff, "/platform/photo/set-as-main/{}"); !ok {
		t.Fatalf("replaced route missing: %v", clientRouteNames(ff))
	}
}

func clientRouteNames(ff []facts.Fact) []string {
	var out []string
	for _, f := range ff {
		if f.Kind == facts.KindRoute {
			out = append(out, f.Name)
		}
	}
	return out
}

func TestExtractHTTPClientFacts_FetchAliasConst(t *testing.T) {
	src := []byte("const fetchImpl = input.fetchImpl ?? globalThis.fetch\n" +
		"await fetchImpl(`${base}/internal/billing/web2app-provision`, { method: 'POST' })\n")
	got := byNameMethod(extractHTTPClientFacts(src, "src/web2app.ts"))
	if got["/internal/billing/web2app-provision"] != "POST" {
		t.Fatalf("const alias: %+v aliases=%v decl=%q", got, provenFetchAliases(src), fetchAliasDecl.FindAllStringSubmatch(string(src), -1))
	}
	src2 := []byte("const fetcher = config.fetch ?? fetch\n" +
		"fetcher(`${base}/admin/v1/operations`, { method: 'POST' })\n")
	got2 := byNameMethod(extractHTTPClientFacts(src2, "src/ops.ts"))
	if got2["/admin/v1/operations"] != "POST" {
		t.Fatalf("fetcher alias: %+v aliases=%v", got2, provenFetchAliases(src2))
	}
}

func TestExtract_Wave11FetchAliasClientRoute(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/web2app.ts": `
export async function provision(input: { fetchImpl?: typeof fetch; baseUrl: string }) {
  const fetchImpl = input.fetchImpl ?? globalThis.fetch
  const response = await fetchImpl(` + "`${input.baseUrl}/internal/billing/web2app-provision`" + `, { method: 'POST' })
  return response
}
export async function renamed(send: typeof fetch = fetch) {
  await send('/v1/direct-alias', { method: 'POST' })
}
`,
		"src/ops.ts": `
export async function ops(config: { fetch?: typeof fetch; baseUrl: string }) {
  const fetcher = config.fetch ?? fetch
  return fetcher(` + "`${config.baseUrl}/admin/v1/operations`" + `, { method: 'POST' })
}
`,
		"src/handoff.ts": `
export async function handoff(baseUrl: string, fetchImpl: typeof fetch = fetch) {
  await fetchImpl(` + "`${baseUrl}/internal/scans/x/finalized`" + `, { method: 'POST' })
}
`,
		"src/guard.ts": `
function recordOnly() {}
export async function guard(input: { fetchImpl?: typeof fetch }) {
  const fetchImpl = input.fetchImpl ?? recordOnly
  await fetchImpl('/internal/should-not', { method: 'POST' })
}
`,
		"src/reassigned.ts": `
function recordOnly() {}
export async function reassigned(baseUrl: string) {
  let fetchImpl = globalThis.fetch
  fetchImpl = recordOnly as any
  await fetchImpl('/internal/reassigned', { method: 'POST' })
}
`,
		"src/shadow.ts": `
export async function onlyParam(fetchImpl: any) {
  await fetchImpl('/internal/shadowed', { method: 'POST' })
}
`,
	}, false)
	want := []string{
		"/internal/billing/web2app-provision",
		"/admin/v1/operations",
		"/internal/scans/x/finalized",
		"/v1/direct-alias",
	}
	for _, p := range want {
		if _, ok := wave10Route(ff, p); !ok {
			t.Errorf("missing client route %s in %v", p, clientRouteNames(ff))
		}
	}
	for _, p := range []string{"/internal/should-not", "/internal/shadowed", "/internal/reassigned"} {
		if _, ok := wave10Route(ff, p); ok {
			t.Errorf("guard/shadow/reassign emitted %s", p)
		}
	}
}

func TestExtract_Wave11FetchAliasLexicalScope(t *testing.T) {
	base := `
export async function provision(input: { fetchImpl?: typeof fetch; baseUrl: string }) {
  const fetchImpl = input.fetchImpl ?? globalThis.fetch
  const response = await fetchImpl(` + "`${input.baseUrl}/internal/billing/web2app-provision`" + `, { method: 'POST' })
  return response
}
`
	cases := []struct {
		name    string
		extra   string
		replace string
		want    bool
		bad     string
	}{
		{name: "sibling_param", extra: "\nexport function localCallback(fetchImpl: (s: string, o: unknown) => unknown) { return fetchImpl(\"/guard/not-http\", {method:\"POST\"}); }\n", want: true, bad: "/guard/not-http"},
		{name: "sibling_local", extra: "\nexport function localCallback(helper: (s: string, o: unknown) => unknown) { const fetchImpl = helper; return fetchImpl(\"/guard/not-http\", {method:\"POST\"}); }\n", want: true, bad: "/guard/not-http"},
		{name: "same_spelling_member", extra: "\nexport function localCallback(obj: {fetchImpl: (s: string, o: unknown) => unknown}) { return obj.fetchImpl(\"/guard/not-http\", {method:\"POST\"}); }\n", want: true, bad: "/guard/not-http"},
		{name: "shadow_global", replace: "const globalThis = {fetch: (url: string, opts: unknown): any => ({})};\n  const fetchImpl = globalThis.fetch;", want: false},
		{name: "reassigned", replace: "let fetchImpl = globalThis.fetch;\n  fetchImpl = ((url: string, opts: unknown): any => ({}));", want: false},
		{name: "shadow_function_fetch", replace: "const fetchImpl = fetch;", extra: "\nfunction fetch(url: string, opts: unknown): any { return {}; }\n", want: false},
	}
	needle := "const fetchImpl = input.fetchImpl ?? globalThis.fetch"
	for _, tc := range cases {
		src := base
		if tc.replace != "" {
			src = strings.Replace(src, needle, tc.replace, 1)
		}
		src += tc.extra
		ff := extractAll(t, map[string]string{"src/web2app.ts": src}, false)
		_, has := wave10Route(ff, "/internal/billing/web2app-provision")
		if has != tc.want {
			t.Errorf("%s real route has=%v want=%v routes=%v", tc.name, has, tc.want, clientRouteNames(ff))
		}
		if tc.bad != "" {
			if _, ok := wave10Route(ff, tc.bad); ok {
				t.Errorf("%s emitted unrelated %s in %v", tc.name, tc.bad, clientRouteNames(ff))
			}
		}
	}
	restore := extractAll(t, map[string]string{"src/web2app.ts": base}, false)
	if _, ok := wave10Route(restore, "/internal/billing/web2app-provision"); !ok {
		t.Fatalf("restore missing real route: %v", clientRouteNames(restore))
	}
	imported := "import { readFile as fetch } from 'node:fs/promises';\n" + strings.Replace(base, needle, "const fetchImpl = fetch;", 1)
	ffImport := extractAll(t, map[string]string{"src/web2app.ts": imported}, false)
	if _, ok := wave10Route(ffImport, "/internal/billing/web2app-provision"); ok {
		t.Fatalf("imported fetch binding still emitted route: %v", clientRouteNames(ffImport))
	}
	directShadow := "import { readFile as fetch } from 'node:fs/promises';\nexport async function go() { await fetch('/internal/billing/web2app-provision', { method: 'POST' }) }\n"
	ffDirect := extractAll(t, map[string]string{"src/web2app.ts": directShadow}, false)
	if _, ok := wave10Route(ffDirect, "/internal/billing/web2app-provision"); ok {
		t.Fatalf("direct fetch spelling ignored import binding: %v", clientRouteNames(ffDirect))
	}
	aliasOk := extractAll(t, map[string]string{"src/web2app.ts": base + "\nexport async function other(send: typeof fetch = fetch) { await send('/v1/kept-alias', { method: 'POST' }) }\n"}, false)
	if _, ok := wave10Route(aliasOk, "/internal/billing/web2app-provision"); !ok {
		t.Fatalf("sibling default-param alias lost: %v", clientRouteNames(aliasOk))
	}
	if _, ok := wave10Route(aliasOk, "/v1/kept-alias"); !ok {
		t.Fatalf("valid alias lost: %v", clientRouteNames(aliasOk))
	}
}

func TestExtract_Wave11NuxtReexportAutoImport(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/landings/package.json": `{"name":"landings","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0","landings-module":"workspace:*"}}`,
		"apps/landings/nuxt.config.ts": `
import landingModule from 'landings-module'
export default defineNuxtConfig({ modules: [landingModule] })
`,
		"apps/landings/pages/CommunityProof.vue": `<script setup lang="ts">
const { nextDelayed } = useStep()
setLandPageMetadata({ page: 'x' })
</script><template><p /></template>`,
		"packages/landings-module/package.json": `{"name":"landings-module","dependencies":{"nuxt":"^3.0.0"}}`,
		"packages/landings-module/src/module.ts": `
export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
}
`,
		"packages/landings-module/src/runtime/composables/useStep.ts":             `export { useStep } from 'shared-lands-components'`,
		"packages/landings-module/src/runtime/composables/setLandPageMetadata.ts": `export function setLandPageMetadata(meta: Record<string, string>) {}`,
		"packages/shared-lands-components/package.json":                           `{"name":"shared-lands-components"}`,
		"packages/shared-lands-components/src/index.ts":                           `export { useStep } from './Stepper/useStep'`,
		"packages/shared-lands-components/src/Stepper/useStep.ts":                 `export function useStep() { return { nextDelayed() {} } }`,
		"apps/landings/pages/Explicit.vue": `<script setup lang="ts">
import { useStep } from 'shared-lands-components'
useStep()
</script><template><p /></template>`,
		"apps/other/package.json":    `{"name":"other","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0"}}`,
		"apps/other/nuxt.config.ts":  `export default defineNuxtConfig({})`,
		"apps/other/pages/index.vue": `<script setup lang="ts">useStep()</script><template><p /></template>`,
	}, false)
	want := "packages/shared-lands-components/src/Stepper.useStep"
	got := fileRefTargets(ff, "apps/landings/pages/CommunityProof.vue")
	if !hasTarget(got, want) {
		t.Errorf("re-export auto-import unresolved: %v", got)
	}
	if !hasTarget(got, "packages/landings-module/src/runtime/composables.setLandPageMetadata") {
		t.Errorf("local registered composable lost: %v", got)
	}
	exp := fileRefTargets(ff, "apps/landings/pages/Explicit.vue")
	if !hasTarget(exp, want) {
		t.Errorf("explicit import lost: %v", exp)
	}
	other := fileRefTargets(ff, "apps/other/pages/index.vue")
	if hasTarget(other, want) {
		t.Errorf("unregistered app bound re-export: %v", other)
	}
}

func wave11NuxtLandingsFiles(moduleSetup string) map[string]string {
	return map[string]string{
		"apps/landings/package.json": `{"name":"landings","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0","landings-module":"workspace:*"}}`,
		"apps/landings/nuxt.config.ts": `
import landingModule from 'landings-module'
export default defineNuxtConfig({ modules: [landingModule] })
`,
		"apps/landings/pages/CommunityProof.vue": `<script setup lang="ts">
const { nextDelayed } = useStep()
setLandPageMetadata({ page: 'x' })
</script><template><p /></template>`,
		"apps/landings/composables/useLocalFlag.ts": `export function useLocalFlag() { return true }`,
		"apps/landings/pages/Local.vue":             `<script setup lang="ts">useLocalFlag()</script><template><p /></template>`,
		"packages/landings-module/package.json":     `{"name":"landings-module","dependencies":{"nuxt":"^3.0.0"}}`,
		"packages/landings-module/src/module.ts":    moduleSetup,
		"packages/landings-module/src/runtime/composables/useStep.ts":             `export { useStep } from 'shared-lands-components'`,
		"packages/landings-module/src/runtime/composables/setLandPageMetadata.ts": `export function setLandPageMetadata(meta: Record<string, string>) {}`,
		"packages/shared-lands-components/package.json":                           `{"name":"shared-lands-components"}`,
		"packages/shared-lands-components/src/index.ts":                           `export { useStep } from './Stepper/useStep'`,
		"packages/shared-lands-components/src/Stepper/useStep.ts":                 `export function useStep() { return { nextDelayed() {} } }`,
	}
}

func TestExtract_Wave11NuxtRegisterUnregisterRestore(t *testing.T) {
	registered := `export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
}
`
	unregistered := `export default function setup() {
}
`
	origin := "packages/shared-lands-components/src/Stepper.useStep"
	local := "apps/landings/composables.useLocalFlag"
	meta := "packages/landings-module/src/runtime/composables.setLandPageMetadata"
	assertBound := func(label string, files map[string]string, wantStep, wantMeta bool) {
		t.Helper()
		ff := extractAll(t, files, false)
		got := fileRefTargets(ff, "apps/landings/pages/CommunityProof.vue")
		if hasTarget(got, origin) != wantStep {
			t.Errorf("%s useStep bound=%v want=%v refs=%v", label, hasTarget(got, origin), wantStep, got)
		}
		if hasTarget(got, meta) != wantMeta {
			t.Errorf("%s setLandPageMetadata bound=%v want=%v refs=%v", label, hasTarget(got, meta), wantMeta, got)
		}
		loc := fileRefTargets(ff, "apps/landings/pages/Local.vue")
		if !hasTarget(loc, local) {
			t.Errorf("%s locally declared composable lost: %v", label, loc)
		}
	}
	assertBound("register", wave11NuxtLandingsFiles(registered), true, true)
	assertBound("unregister", wave11NuxtLandingsFiles(unregistered), false, false)
	assertBound("restore", wave11NuxtLandingsFiles(registered), true, true)
	assertBound("initially-unregistered", wave11NuxtLandingsFiles(unregistered), false, false)
	assertBound("then-register", wave11NuxtLandingsFiles(registered), true, true)
}

func TestNuxtOracleVisibleWithPartialSources(t *testing.T) {
	files := wave11NuxtLandingsFiles(`export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
}
`)
	bytesOf := map[string][]byte{}
	known := map[string]bool{}
	for k, v := range files {
		bytesOf[k] = []byte(v)
		known[k] = true
	}
	nuxtPkgs := []string{"apps/landings", "packages/landings-module"}
	pkgDirs := map[string]bool{"apps/landings": true, "packages/landings-module": true, "packages/shared-lands-components": true}
	pkgDirByName := map[string]string{"landings": "apps/landings", "landings-module": "packages/landings-module"}
	partial := map[string][]byte{
		"packages/landings-module/src/module.ts": bytesOf["packages/landings-module/src/module.ts"],
		"apps/landings/pages/CommunityProof.vue": bytesOf["apps/landings/pages/CommunityProof.vue"],
	}
	read := func(rel string) []byte { return bytesOf[rel] }
	fullExtra := extraDirsByNuxtPackageRead(bytesOf, known, read, nuxtPkgs, pkgDirs)
	partExtra := extraDirsByNuxtPackageRead(partial, known, read, nuxtPkgs, pkgDirs)
	if len(fullExtra["packages/landings-module"]) == 0 || len(partExtra["packages/landings-module"]) == 0 {
		t.Fatalf("extra dirs missing full=%v partial=%v", fullExtra, partExtra)
	}
	fullC := nuxtModuleConsumersRead(bytesOf, known, read, nuxtPkgs, pkgDirByName, pkgDirs)
	partC := nuxtModuleConsumersRead(partial, known, read, nuxtPkgs, pkgDirByName, pkgDirs)
	if len(fullC["apps/landings"]) == 0 || len(partC["apps/landings"]) == 0 {
		t.Fatalf("consumers missing without nuxt.config in sources: full=%v partial=%v", fullC, partC)
	}
}

func TestExtract_Wave11NuxtReexportMissingAndUnregistered(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/landings/package.json": `{"name":"landings","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0","landings-module":"workspace:*"}}`,
		"apps/landings/nuxt.config.ts": `
import landingModule from 'landings-module'
export default defineNuxtConfig({ modules: [landingModule] })
`,
		"apps/landings/pages/A.vue":             `<script setup lang="ts">useStep()</script><template><p /></template>`,
		"packages/landings-module/package.json": `{"name":"landings-module","dependencies":{"nuxt":"^3.0.0"}}`,
		"packages/landings-module/src/module.ts": `
export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
}
`,
		"packages/landings-module/src/runtime/composables/useStep.ts": `export { useStepMissing as useStep } from 'shared-lands-components'`,
		"packages/shared-lands-components/package.json":               `{"name":"shared-lands-components"}`,
		"packages/shared-lands-components/src/index.ts":               `export function useStep() {}`,
	}, false)
	got := fileRefTargets(ff, "apps/landings/pages/A.vue")
	if hasTarget(got, "packages/shared-lands-components/src.useStep") {
		t.Errorf("missing re-export name was guessed: %v", got)
	}
}

func TestExtract_Wave11ExternalImportDoesNotBindSibling(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"tools/compare-screenshots.mjs": `
import pixelmatch from 'pixelmatch'
import { parseArgs as args } from 'node:util'
import * as ns from 'pixelmatch'
import { default as aliased } from 'pixelmatch'
pixelmatch(1, 2)
args()
ns.default()
aliased()
`,
		"tools/_t191-cursor1b-etalon-metrics.mjs": `
const pixelmatch = () => 1
function parseArgs() {}
export function aliased() {}
export default function ns() {}
`,
		"tools/local.ts": `
import { helper } from './util'
helper()
`,
		"tools/util.ts": `export function helper() {}`,
		"tools/shadow.ts": `
import pixelmatch from 'pixelmatch'
export function run(pixelmatch: () => void) { pixelmatch() }
`,
	}, false)
	cmp := fileRefFact(ff, "tools/compare-screenshots.mjs")
	for _, r := range cmp.Relations {
		if r.Kind != facts.RelCalls {
			continue
		}
		if r.TargetFile == "tools/_t191-cursor1b-etalon-metrics.mjs" {
			t.Fatalf("external import bound private sibling: %+v", r)
		}
		if strings.HasSuffix(r.Target, ".pixelmatch") && r.TargetFile != "" && strings.Contains(r.TargetFile, "_t191") {
			t.Fatalf("external pixelmatch bound sibling: %+v", r)
		}
	}
	local := fileRefFact(ff, "tools/local.ts")
	if !hasCallToFile(local, "tools.helper", "tools/util.ts") {
		t.Fatalf("relative import lost: %+v", local.Relations)
	}
}
