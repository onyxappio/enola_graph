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
