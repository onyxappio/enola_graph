package tsextractor

import (
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestServerRoutes_FastifyRegisterCallbackReceiver(t *testing.T) {
	src := `
import { FastifyInstance } from 'fastify'
export async function registerWebhookRoutes(app: FastifyInstance) {
  await app.register(async (webhookApp) => {
    webhookApp.post('/v1/payment/webhooks/solidgate', async () => {})
    webhookApp.post('/v1/payment/webhooks/stripe', async () => {})
  })
}
`
	ff := extractTS(t, src, "services/payment-api/src/routes/webhooks.ts")
	got := serverRoutes(ff)
	if got["/v1/payment/webhooks/solidgate"] != "POST" || got["/v1/payment/webhooks/stripe"] != "POST" {
		t.Fatalf("register callback routes: %+v", got)
	}
	if clients := clientRoutes(ff); len(clients) != 0 {
		t.Fatalf("server registrations stolen as client: %+v", clients)
	}
}

func TestServerRoutes_UnknownRegisterCallbackIsNotServer(t *testing.T) {
	src := `
export function wrap(obj: any) {
  obj.register(async (webhookApp) => {
    webhookApp.post('/v1/payment/webhooks/solidgate', async () => {})
  })
}
`
	ff := extractTS(t, src, "src/unknown.ts")
	if got := serverRoutes(ff); len(got) != 0 {
		t.Fatalf("unknown register must not emit server routes: %+v", got)
	}
}

func TestServerRoutes_RegisterCallbackParamShadow(t *testing.T) {
	src := `
import { FastifyInstance } from 'fastify'
export async function registerWebhookRoutes(app: FastifyInstance) {
  await app.register(async (webhookApp) => {
    webhookApp.post('/outer', async () => {})
    function inner(webhookApp: any) {
      webhookApp.post('/inner', async () => {})
    }
    inner(webhookApp)
  })
}
`
	got := serverRoutes(extractTS(t, src, "src/webhooks.ts"))
	if got["/outer"] != "POST" {
		t.Fatalf("outer callback lost: %+v", got)
	}
	if _, ok := got["/inner"]; ok {
		t.Fatalf("shadowed callback param must not inherit server role: %+v", got)
	}
}

func TestExtract_NamespaceImportMemberCalls(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/mobile/src/state/mobileAppMachine.updates.ts": `
export function onboardingStateFrom() { return 1 }
export function protectStateFrom() { return 2 }
`,
		"apps/mobile/src/behavior/mobileAppInterpreter.ts": `
import * as update from '../state/mobileAppMachine.updates'
export function createInitialMobileAppSnapshot() {
  return update.onboardingStateFrom() + update.protectStateFrom()
}
`,
	}, false)
	var caller facts.Fact
	for _, f := range ff {
		if f.Name == "apps/mobile/src/behavior.createInitialMobileAppSnapshot" {
			caller = f
		}
	}
	if caller.Name == "" {
		t.Fatalf("caller missing: %v", factNames(ff))
	}
	for _, target := range []string{
		"apps/mobile/src/state.onboardingStateFrom",
		"apps/mobile/src/state.protectStateFrom",
	} {
		if !hasRelation(caller, facts.RelCalls, target) {
			t.Errorf("missing namespace call %s in %+v", target, caller.Relations)
		}
	}
	for _, r := range caller.Relations {
		if r.Kind == facts.RelCalls && strings.HasSuffix(r.Target, "protectStateFrom") && r.TargetFile != "apps/mobile/src/state/mobileAppMachine.updates.ts" {
			t.Fatalf("target_file=%q relations=%+v", r.TargetFile, caller.Relations)
		}
	}
}

func TestExtract_NamespaceLocalShadowDoesNotBind(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"lib/update.ts": `export function onboardingStateFrom() { return 1 }`,
		"src/call.ts": `
import * as update from '../lib/update'
export function run() {
  const update = { onboardingStateFrom() { return 0 } }
  return update.onboardingStateFrom()
}
`,
	}, false)
	var run facts.Fact
	for _, f := range ff {
		if f.Name == "src.run" {
			run = f
		}
	}
	if hasRelation(run, facts.RelCalls, "lib.onboardingStateFrom") {
		t.Fatalf("local namespace shadow bound imported member: %+v", run.Relations)
	}
}

func TestExtract_NamedReexportSubdirLeafName(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/shared/package.json": `{"name":"shared-lands-components","exports":{".":{"types":"./src/index.ts","import":"./src/index.ts"}}}`,
		"packages/shared/src/index.ts": `export { useStep } from './Stepper/useStep'`,
		"packages/shared/src/Stepper/useStep.ts": `export function useStep() { return 1 }`,
		"packages/app/src/store.ts": `
import { useStep } from 'shared-lands-components'
export function boot() { return useStep() }
`,
	}, false)
	var boot facts.Fact
	for _, f := range ff {
		if f.Name == "packages/app/src.boot" {
			boot = f
		}
	}
	if !hasRelation(boot, facts.RelCalls, "packages/shared/src/Stepper.useStep") {
		t.Fatalf("want leaf name packages/shared/src/Stepper.useStep, got %+v", boot.Relations)
	}
	for _, r := range boot.Relations {
		if r.Kind == facts.RelCalls && r.Target == "packages/shared/src/Stepper.useStep" && r.TargetFile != "packages/shared/src/Stepper/useStep.ts" {
			t.Fatalf("target_file=%q", r.TargetFile)
		}
	}
}

func TestExtract_ClassDataFieldsAreNotMethods(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/machineDefinitionError.ts": `
export class MachineDefinitionError extends Error {
  readonly _tag = 'MachineDefinitionError' as const
  readonly code: string
  ruleIds: string[]
  from: string
  on: string
  handler = () => 1
  constructor() { super(); this.name = 'x' }
  get volume() { return 1 }
}
`,
	}, false)
	check := map[string]string{
		"src.MachineDefinitionError._tag":    facts.SymbolConstant,
		"src.MachineDefinitionError.code":    facts.SymbolConstant,
		"src.MachineDefinitionError.ruleIds": facts.SymbolVariable,
		"src.MachineDefinitionError.from":    facts.SymbolVariable,
		"src.MachineDefinitionError.on":      facts.SymbolVariable,
		"src.MachineDefinitionError.handler": facts.SymbolMethod,
		"src.MachineDefinitionError.volume":  facts.SymbolGetter,
	}
	for name, kind := range check {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != kind {
			t.Errorf("%s kind=%v want %s", name, f.Props["symbol_kind"], kind)
		}
		if kind == facts.SymbolVariable || kind == facts.SymbolConstant {
			if _, ok := f.Props["cyclomatic"]; ok {
				t.Errorf("%s must not carry cyclomatic", name)
			}
		}
	}
}

func TestExtract_NitroFileAndAddServerHandlerRoutes(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"nuxt.config.ts": `export default {}`,
		"pages/index.vue": `<template><div /></template>`,
		"server/api/geo.get.ts": `export default defineEventHandler(() => ({}))`,
		"src/registerServerProxy.ts": `
import { addServerHandler, createResolver } from '@nuxt/kit'
export function registerServerProxy() {
  const resolver = createResolver(import.meta.url)
  addServerHandler({ route: '/land/**', handler: resolver.resolve('./runtime/server/handler') })
  addServerHandler({ route: '/land/**', handler: resolver.resolve('./runtime/server/mockHandler') })
  addServerHandler({ route: '/api/geo', handler: resolver.resolve('./runtime/server/geo.get') })
  addServerHandler({ route: '/sw-web-push.js', handler: resolver.resolve('./runtime/server/webPushHandler') })
}
`,
		"src/runtime/server/handler.ts": `export default function handler() {}`,
		"src/runtime/server/mockHandler.ts": `export default function mockHandler() {}`,
		"src/runtime/server/geo.get.ts": `export default function geo() {}`,
		"src/runtime/server/webPushHandler.ts": `export default function webPushHandler() {}`,
		"src/fake.ts": `
function addServerHandler(opts: { route: string }) {}
addServerHandler({ route: '/invented' })
`,
		"server/middleware/auth.ts": `export default defineEventHandler(() => {})`,
	}, false)
	var names []string
	byFile := map[string][]facts.Fact{}
	for _, f := range ff {
		if f.Kind != facts.KindRoute {
			continue
		}
		names = append(names, f.Name+"@"+f.File)
		byFile[f.File] = append(byFile[f.File], f)
	}
	foundGeo := false
	for _, f := range byFile["server/api/geo.get.ts"] {
		if f.Name == "/api/geo" && f.Props["method"] == "GET" && f.Props["role"] == facts.RoleServer {
			foundGeo = true
		}
	}
	if !foundGeo {
		t.Fatalf("missing file route GET /api/geo: %v", names)
	}
	foundPage := false
	for _, f := range ff {
		if f.Kind == facts.KindRoute && f.Name == "/" && f.Props["router"] == "pages" {
			foundPage = true
		}
	}
	if !foundPage {
		t.Fatalf("missing nuxt page /: %v", names)
	}
	reg := byFile["src/registerServerProxy.ts"]
	got := map[string]bool{}
	for _, f := range reg {
		got[f.Name] = true
	}
	for _, p := range []string{"/land/**", "/api/geo", "/sw-web-push.js"} {
		if !got[p] {
			t.Errorf("missing registered %s in %+v", p, got)
		}
	}
	landCount := 0
	for _, f := range reg {
		if f.Name == "/land/**" {
			landCount++
			if f.Props["handler"] != nil {
				t.Errorf("conditional alternate handlers must not collapse to one handler: %+v", f.Props)
			}
		}
	}
	if landCount != 1 {
		t.Errorf("want one /land/** declaration, got %d", landCount)
	}
	for _, f := range ff {
		if f.Kind == facts.KindRoute && f.Name == "/invented" {
			t.Fatal("fake addServerHandler fabricated a route")
		}
		if f.Kind == facts.KindRoute && f.File == "server/middleware/auth.ts" {
			t.Fatal("middleware must not emit a route")
		}
	}
}

func TestExtract_VueOwnFileTemplateCallHasTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"nuxt.config.ts": `export default {}`,
		"components/QuizLayout.vue": `<script setup lang="ts">
function handleNextClick() {}
</script>
<template><button @click="handleNextClick">ok</button></template>
`,
		"components/FeatureShowcaseLayout.vue": `<script setup lang="ts">
function handleNextClick() {}
</script>
<template><button @click="handleNextClick">ok</button></template>
`,
	}, false)
	for _, file := range []string{"components/QuizLayout.vue", "components/FeatureShowcaseLayout.vue"} {
		var comp facts.Fact
		for _, f := range ff {
			if f.Kind == facts.KindSymbol && f.File == file && f.Props["web_component"] == "component" {
				comp = f
			}
		}
		if comp.Name == "" {
			t.Fatalf("component missing for %s", file)
		}
		found := false
		for _, r := range comp.Relations {
			if r.Kind == facts.RelCalls && strings.HasSuffix(r.Target, "handleNextClick") {
				found = true
				if r.TargetFile != file {
					t.Fatalf("%s call target_file=%q want own file relations=%+v", file, r.TargetFile, comp.Relations)
				}
			}
		}
		if !found {
			t.Fatalf("%s missing handleNextClick call: %+v", file, comp.Relations)
		}
	}
}
