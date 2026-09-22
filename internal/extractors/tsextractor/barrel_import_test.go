package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_FolderIndexImportCallsOwnModule(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/mobile/src/scenario/oracles/index.ts": `
export * from './types';
export { mounted } from './mounted';
`,
		"apps/mobile/src/scenario/oracles/types.ts": `
export function createVerdict() { return {}; }
export function oracleOptions() { return {}; }
`,
		"apps/mobile/src/scenario/oracles/mounted.ts": `export function mounted() { return true; }`,
		"apps/mobile/src/scenario/journeys/contracts/helpers.ts": `
import { createVerdict, oracleOptions, mounted } from '../../oracles';
export function serviceCallCount() {
  return createVerdict(oracleOptions(), mounted());
}
`,
		"apps/mobile/src/scenario/createVerdict.ts": `export function createVerdict() { return 'sibling'; }`,
	}, false)

	var helper facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.File == "apps/mobile/src/scenario/journeys/contracts/helpers.ts" && f.Name == "apps/mobile/src/scenario/journeys/contracts.serviceCallCount" {
			helper = f
		}
	}
	if helper.Name == "" {
		t.Fatalf("helpers symbol missing; facts=%v", factNames(ff))
	}
	want := []string{
		"apps/mobile/src/scenario/oracles.createVerdict",
		"apps/mobile/src/scenario/oracles.oracleOptions",
		"apps/mobile/src/scenario/oracles.mounted",
	}
	for _, target := range want {
		if !hasRelation(helper, facts.RelCalls, target) {
			t.Errorf("missing call %s in %+v", target, helper.Relations)
		}
	}
	if hasRelation(helper, facts.RelCalls, "apps/mobile/src/scenario.createVerdict") {
		t.Fatal("folder import must not target the parent directory")
	}
}

func TestExtract_LocalSameFileCallHasTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"lib/costModel.ts": `
function round(value: number, digits: number): number { return value; }
export function toMoneyPoint(hourlyEur: number) { return round(hourlyEur, 4); }
`,
		"lib/billingExport.ts": `
function round(value: number, decimals: number): number { return value; }
export function dump() { return round(1, 0); }
`,
	}, false)
	var toMoney facts.Fact
	for _, f := range ff {
		if f.Name == "lib.toMoneyPoint" {
			toMoney = f
		}
	}
	for _, r := range toMoney.Relations {
		if r.Kind == facts.RelCalls && r.Target == "lib.round" && r.TargetFile != "lib/costModel.ts" {
			t.Fatalf("local call target_file=%q relations=%+v", r.TargetFile, toMoney.Relations)
		}
	}
}

func TestExtract_ImportAliasSameDirectoryCallHasTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/a.ts": "import { round as externalRound } from './b';\nfunction round(n: number) { return n; }\nexport function caller() { return externalRound(1); }\n",
		"src/b.ts": "export function round(n: number) { return n + 1; }\n",
	}, false)
	var caller facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.Name == "src.caller" {
			caller = f
		}
	}
	found := false
	for _, r := range caller.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.round" {
			found = true
			if r.TargetFile != "src/b.ts" {
				t.Fatalf("imported call target_file=%q want src/b.ts relations=%+v", r.TargetFile, caller.Relations)
			}
		}
	}
	if !found {
		t.Fatalf("missing imported call: %+v", caller.Relations)
	}
}

func TestExtract_NamedReexportBridgeCallHasLeafTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
	}, false)
	var caller facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.Name == "src.caller" {
			caller = f
		}
	}
	found := false
	for _, r := range caller.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.round" {
			found = true
			if r.TargetFile != "src/b.ts" {
				t.Fatalf("named reexport target_file=%q want src/b.ts relations=%+v", r.TargetFile, caller.Relations)
			}
		}
	}
	if !found {
		t.Fatalf("missing imported call: %+v", caller.Relations)
	}
}

func TestExtract_NamedReexportMissingKeepsImportFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round as renamed } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
	}, false)
	var caller facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.Name == "src.caller" {
			caller = f
		}
	}
	found := false
	for _, r := range caller.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.round" {
			found = true
			if r.TargetFile != "src/bridge.ts" {
				t.Fatalf("failed follow must keep import file, got target_file=%q relations=%+v", r.TargetFile, caller.Relations)
			}
		}
	}
	if !found {
		t.Fatalf("missing imported call: %+v", caller.Relations)
	}
}

func TestExtract_NamedReexportCollisionLeavesNoTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/a.ts":      "import { round } from './barrel';\nexport function caller() { return round(1); }\n",
		"src/barrel.ts": "export * from './b';\nexport * from './c';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
	}, false)
	var caller facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.Name == "src.caller" {
			caller = f
		}
	}
	for _, r := range caller.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.round" && r.TargetFile != "" {
			t.Fatalf("colliding star reexports must not pin target_file=%q relations=%+v", r.TargetFile, caller.Relations)
		}
	}
}

func TestExtract_FolderIndexImportTypeAndAlias(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"lib/oracles/index.ts": `export { createVerdict as make } from './types';`,
		"lib/oracles/types.ts": `export function createVerdict() { return 1; }`,
		"src/use.ts": `
import { make as build } from '../lib/oracles';
export function run() { return build(); }
`,
	}, false)
	var run facts.Fact
	for _, f := range ff {
		if f.Name == "src.run" {
			run = f
		}
	}
	if !hasRelation(run, facts.RelCalls, "lib/oracles.make") {
		t.Fatalf("aliased reexport call want lib/oracles.make, got %+v", run.Relations)
	}
}
