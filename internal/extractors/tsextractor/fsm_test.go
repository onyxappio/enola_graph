package tsextractor

import (
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/fsm"
)

func TestFSMConfigurationChangesExtractorCacheKey(t *testing.T) {
	e := New()
	without := e.ConfigKey()
	e.SetStateMachineSpecs([]fsm.Spec{{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "src/jobs.ts",
		Factory: fsm.SymbolRef{Module: "src/kernel.ts", Export: "defineMachine"}, Registration: "createRegistration",
		DispatchFiles: []string{"src/worker.ts"},
	}})
	with := e.ConfigKey()
	if with == without {
		t.Fatal("adding an FSM adapter did not invalidate TypeScript extractor contributions")
	}
	e.SetStateMachineSpecs([]fsm.Spec{{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "src/jobs.ts",
		Factory: fsm.SymbolRef{Module: "src/kernel.ts", Export: "defineMachine"}, Registration: "createRegistration",
		DispatchFiles: []string{"src/other-worker.ts"},
	}})
	if e.ConfigKey() == with {
		t.Fatal("changing FSM dispatch scope did not invalidate TypeScript extractor contributions")
	}
}

func TestFSMInputHashesRetainModelDependenciesAfterRebind(t *testing.T) {
	sources := map[string][]byte{
		"machine.ts": []byte(`import type { AppState, AppEvent } from './types';
function enterState(state: AppState) { return state; }
function rejected(state: AppState) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'GO') return enterState('idle');
  return rejected(state);
}`),
		"types.ts": []byte(`export type AppState = 'idle';
export type AppEvent = { type: 'GO' };`),
	}
	resolve := func(from, specifier string) (string, bool) {
		if specifier == "./types" && filepath.Dir(from) == "." {
			return "types.ts", true
		}
		return "", false
	}
	specs := []fsm.Spec{{ID: "app", Adapter: fsm.AdapterReducerInterpreter, File: "machine.ts",
		Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent"}}
	analyzer := fsm.NewAnalyzer(specs, []string{"machine.ts", "types.ts"}, func(rel string) ([]byte, error) {
		return sources[filepath.ToSlash(rel)], nil
	}, resolve)
	rebound := analyzer.Rebind(func(rel string) ([]byte, error) { return sources[filepath.ToSlash(rel)], nil }, resolve)
	inputs := fsmAnalyzerInputFiles(specs, rebound)
	for _, file := range inputs {
		if file == "types.ts" {
			return
		}
	}
	t.Fatalf("model type dependency disappeared from cache input hashes after Rebind: %v", inputs)
}

func TestMergeFSMFactsPreservesPerSiteEvidence(t *testing.T) {
	base := []facts.Fact{{Kind: facts.KindSymbol, Name: "send", File: "src/ui.ts", Props: map[string]any{"symbol_kind": facts.SymbolFunc}}}
	overlay := facts.Fact{
		Kind: facts.KindSymbol, Name: "send", File: "src/ui.ts", Line: 27, EndLine: 27,
		Props:     map[string]any{"language": "typescript", "dispatch_status": "resolved", "event_tag": "SUBMIT"},
		Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "app/event:SUBMIT"}},
	}
	merged := mergeFSMFacts(base, []facts.Fact{overlay})
	if len(merged) != 1 {
		t.Fatalf("merged %d facts, want one symbol", len(merged))
	}
	sites, ok := merged[0].Props["fsm_evidence_sites"].([]map[string]any)
	if !ok || len(sites) != 1 {
		t.Fatalf("per-site evidence = %#v, want one evidence site", merged[0].Props["fsm_evidence_sites"])
	}
	if sites[0]["line"] != 27 {
		t.Fatalf("evidence line = %#v, want 27", sites[0]["line"])
	}
	rels, ok := sites[0]["relations"].([]map[string]string)
	if !ok || len(rels) != 1 || rels[0]["target"] != "app/event:SUBMIT" {
		t.Fatalf("evidence relations = %#v", sites[0]["relations"])
	}
	if len(merged[0].Relations) != 1 || merged[0].Relations[0].Kind != facts.RelFSMDispatches {
		t.Fatalf("base symbol lost relation: %#v", merged[0].Relations)
	}
}
