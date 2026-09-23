package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_DefaultAsSFCBarrelBridge(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/index.ts": "export { default as Stepper } from './Stepper/Stepper.vue'\nexport { default as StepperGroup } from './Stepper/StepperGroup.vue'\nexport { default as Foo } from './Stepper/Stepper.vue'\n",
		"src/Stepper/Stepper.vue": `<script setup lang="ts">
export function unused() {}
</script>
<template><div /></template>
`,
		"src/Stepper/StepperGroup.vue": `<script setup lang="ts"></script><template><div /></template>`,
		"src/plain.ts":                 "export const onlyNamed = 1\n",
		"src/withDefault.ts":           "export default function Helper() { return 1 }\n",
		"src/data_easy.vue": `<script setup lang="ts">
import { Stepper, StepperGroup, Foo } from './index'
import Direct from './Stepper/Stepper.vue'
import { default as Missing } from './plain'
import Helper from './withDefault'
export function DataEasy() {
  return [Stepper, StepperGroup, Foo, Direct, Missing, Helper]
}
</script>
<template>
  <Stepper />
  <StepperGroup />
</template>
`,
	}, false)
	easy, ok := findFact(ff, "src.DataEasy")
	if !ok {
		t.Fatalf("DataEasy missing: %v", factNames(ff))
	}
	want := []struct{ target, file string }{
		{"src/Stepper.Stepper", "src/Stepper/Stepper.vue"},
		{"src/Stepper.StepperGroup", "src/Stepper/StepperGroup.vue"},
	}
	for _, w := range want {
		found := false
		for _, r := range easy.Relations {
			if r.Kind == facts.RelCalls && r.Target == w.target {
				found = true
				if r.TargetFile != w.file {
					t.Fatalf("%s target_file=%q want %s rels=%+v", w.target, r.TargetFile, w.file, easy.Relations)
				}
			}
		}
		if !found {
			t.Fatalf("missing call %s: %+v", w.target, easy.Relations)
		}
	}
	for _, r := range easy.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.Stepper" && r.TargetFile == "src/index.ts" {
			t.Fatalf("barrel must not be the definition: %+v", easy.Relations)
		}
		if r.Kind == facts.RelCalls && (r.Target == "src.plain" || r.Target == "src.onlyNamed") && r.TargetFile == "src/plain.ts" && r.Target == "src.plain" {
			t.Fatalf("non-SFC without default must not invent a default: %+v", easy.Relations)
		}
	}
	stepper, ok := findFact(ff, "src/Stepper.Stepper")
	if !ok {
		t.Fatal("Stepper component missing")
	}
	if stepper.File != "src/Stepper/Stepper.vue" {
		t.Fatalf("Stepper file=%s", stepper.File)
	}
	refs := fileRefTargets(ff, "src/data_easy.vue")
	if !hasTarget(refs, "src.Helper") {
		t.Fatalf("direct default import missing from file_ref: %v", refs)
	}
}

func TestExtract_TopLevelDestructuredBindings(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/useCoreComponents.ts": `
function createInjectionState(_a: unknown, _b: unknown) { return [() => 1, () => 2] }
const [useCoreComponentsSettings, useCoreComponents] = createInjectionState(null, {})
export { useCoreComponentsSettings, useCoreComponents }
const [, holeKept] = [1, 2]
const { lock, nested: { inner }, rest: leftover, renamed: aliasName = 1, skipKey } = { lock: 1, nested: { inner: 2 }, rest: 3, renamed: 4, skipKey: 5 }
export { holeKept, lock, inner, leftover, aliasName }
function localFn() {
  const { hidden } = { hidden: 1 }
  return hidden
}
export { localFn }
`,
		"src/app.ts": `
import { useCoreComponentsSettings } from './useCoreComponents'
export function boot() { return useCoreComponentsSettings() }
`,
	}, false)
	settings, ok := findFact(ff, "src.useCoreComponentsSettings")
	if !ok {
		t.Fatalf("missing destructured binding: %v", factNames(ff))
	}
	if settings.Props["symbol_kind"] != facts.SymbolVariable {
		t.Fatalf("symbol_kind=%v", settings.Props["symbol_kind"])
	}
	if settings.Props["exported"] != true {
		t.Fatal("export clause must mark exported")
	}
	if _, ok := findFact(ff, "src.useCoreComponents"); !ok {
		t.Fatal("useCoreComponents missing")
	}
	if _, ok := findFact(ff, "src.hidden"); ok {
		t.Fatal("function-local destructuring leaked")
	}
	if _, ok := findFact(ff, "src.skipKey"); !ok {
		t.Fatal("shorthand skipKey is a binding")
	}
	if _, ok := findFact(ff, "src.renamed"); ok {
		t.Fatal("object property key is not a binding")
	}
	if _, ok := findFact(ff, "src.aliasName"); !ok {
		t.Fatal("aliased binding missing")
	}
	boot, _ := findFact(ff, "src.boot")
	found := false
	for _, r := range boot.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.useCoreComponentsSettings" {
			found = true
			if r.TargetFile != "src/useCoreComponents.ts" {
				t.Fatalf("target_file=%q", r.TargetFile)
			}
		}
	}
	if !found {
		t.Fatalf("call unresolved: %+v", boot.Relations)
	}
}

func TestExtract_EmptyParsedFileEmitsFileRef(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/health.ts": "export async function registerHealthRoutes() { return 1 }\n",
		"src/scans.ts":  "import { registerHealthRoutes } from './health'\nexport function registerScanRoutes() { return registerHealthRoutes() }\n",
		"docs/INFO.md":  "see `src/health.ts` and `src/scans.ts`\n",
	}, false)
	var healthRef, scansRef bool
	for _, f := range ff {
		if f.Kind != facts.KindFileRef {
			continue
		}
		if f.Name == "src/health.ts" && f.File == "src/health.ts" {
			healthRef = true
		}
		if f.Name == "src/scans.ts" && f.File == "src/scans.ts" {
			scansRef = true
		}
	}
	if !healthRef {
		t.Fatal("health.ts must emit file_ref with no file-scope references")
	}
	if !scansRef {
		t.Fatal("scans.ts file_ref missing")
	}
}

func TestExtract_DeclarationSiblingDoesNotShadowImplementation(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/environmentProfile.ts": `
import { assertPaymentDemoRuntime } from './paymentDemoRuntime.mjs'
export function createEnvironmentProfileFromPulumiConfig() {
  return assertPaymentDemoRuntime()
}
`,
		"src/paymentDemoRuntime.mjs":   "export function assertPaymentDemoRuntime() { return 1 }\n",
		"src/paymentDemoRuntime.d.mts": "export declare function assertPaymentDemoRuntime(): void\nexport type PaymentDemoRuntimePrerequisites = { ok: true }\n",
		"src/onlyDecl.ts": `
import { ghost } from './ghost.mjs'
export function useGhost() { return ghost() }
`,
		"src/ghost.d.mts": "export declare function ghost(): void\n",
		"src/fromTs.ts": `
import { srcFn } from './impl.js'
export function useSrc() { return srcFn() }
`,
		"src/impl.ts": "export function srcFn() { return 1 }\n",
		"src/impl.js": "export function srcFn() { return 2 }\n",
	}, false)
	create, _ := findFact(ff, "src.createEnvironmentProfileFromPulumiConfig")
	found := false
	for _, r := range create.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.assertPaymentDemoRuntime" {
			found = true
			if r.TargetFile != "src/paymentDemoRuntime.mjs" {
				t.Fatalf("implementation shadowed, target_file=%q rels=%+v", r.TargetFile, create.Relations)
			}
		}
	}
	if !found {
		t.Fatalf("runtime call unresolved: %+v", create.Relations)
	}
	useSrc, _ := findFact(ff, "src.useSrc")
	ok := false
	for _, r := range useSrc.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.srcFn" && r.TargetFile == "src/impl.ts" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("ts substitution lost: %+v", useSrc.Relations)
	}
}
