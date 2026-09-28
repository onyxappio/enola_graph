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

func TestAppendFSMReadsMarksBodySensitiveDependencies(t *testing.T) {
	rec := &FileRecord{File: "apps/mobile/src/App.tsx", SideReads: []string{"apps/mobile/src/reexport.ts"}}
	sources := map[string][]byte{
		"apps/mobile/src/screens/forgot-password/forgotPasswordRuntimeView.ts": []byte("export type Intent = { type: 'GO' };"),
	}
	appendFSMReads(rec, []string{"apps/mobile/src/screens/forgot-password/forgotPasswordRuntimeView.ts"}, "", func(file string) []byte {
		return sources[filepath.ToSlash(file)]
	})
	if len(rec.FSMReads) != 1 || rec.FSMReads[0] != "apps/mobile/src/screens/forgot-password/forgotPasswordRuntimeView.ts" {
		t.Fatalf("FSM body dependencies = %v, want the imported type file", rec.FSMReads)
	}
	if len(rec.SideReadHashes) != 1 || rec.SideReadHashes[rec.FSMReads[0]] == "" {
		t.Fatalf("body-sensitive dependency hash was not retained: %v", rec.SideReadHashes)
	}
	if !containsString(rec.SideReads, "apps/mobile/src/reexport.ts") || !containsString(rec.SideReads, rec.FSMReads[0]) {
		t.Fatalf("FSM read merge lost existing/source dependencies: %v", rec.SideReads)
	}
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

func TestMergeFSMFactsKeepsTargetFileAndBindsTheExactDeclaration(t *testing.T) {
	base := []facts.Fact{
		{Kind: facts.KindSymbol, Name: "f", File: "src/a.ts", Line: 1, EndLine: 10},
		{Kind: facts.KindSymbol, Name: "f", File: "src/a.ts", Line: 20, EndLine: 40},
	}
	overlays := []facts.Fact{
		{Kind: facts.KindSymbol, Name: "f", File: "src/a.ts", Line: 30, EndLine: 30, Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "event", TargetFile: "src/one.ts"}}},
		{Kind: facts.KindSymbol, Name: "f", File: "src/a.ts", Line: 30, EndLine: 30, Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "event", TargetFile: "src/two.ts"}}},
	}
	got := mergeFSMFacts(base, overlays)
	if len(got[0].Relations) != 0 || len(got[1].Relations) != 2 {
		t.Fatalf("overlay ownership/target files = first:%+v second:%+v, want both distinct edges on declaration20..40", got[0].Relations, got[1].Relations)
	}

	exact := mergeFSMFacts(base, []facts.Fact{{Kind: facts.KindSymbol, Name: "f", File: "src/a.ts", Line: 1, Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "first"}}}})
	if len(exact[0].Relations) != 1 || len(exact[1].Relations) != 0 {
		t.Fatalf("exact declaration line bound incorrectly: first=%+v second=%+v", exact[0].Relations, exact[1].Relations)
	}
}

func TestMergeFSMFactsDoesNotFabricateUnmatchedSourceSymbols(t *testing.T) {
	overlay := facts.Fact{Kind: facts.KindSymbol, Name: "missing", File: "src/a.ts", Line: 30,
		Props:     map[string]any{"fsm_evidence": "direct_source_binding"},
		Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "app/event:GO"}}}
	got := mergeFSMFacts(nil, []facts.Fact{overlay})
	if len(got) != 1 || got[0].Kind != facts.KindExtraction || got[0].Props["unresolved_source_binding"] != "missing" {
		t.Fatalf("unmatched overlay evidence = %+v, want extraction coverage without a symbol declaration", got)
	}
}

func TestMergeFSMFactsBindsLocalOverlayToCanonicalQualifiedSymbol(t *testing.T) {
	base := []facts.Fact{{Kind: facts.KindSymbol, Name: "..oldHandler", File: "handlers.ts", Line: 2}}
	overlay := facts.Fact{Kind: facts.KindSymbol, Name: "oldHandler", File: "handlers.ts", Line: 7,
		Relations: []facts.Relation{{Kind: facts.RelFSMHandlesCommand, Target: "machine/command:RUN"}}}
	got := mergeFSMFacts(base, []facts.Fact{overlay})
	if len(got) != 1 || got[0].Name != "..oldHandler" || len(got[0].Relations) != 1 {
		t.Fatalf("qualified source binding = %+v, want relation on canonical ..oldHandler fact", got)
	}

	for _, tc := range []struct {
		name string
		base facts.Fact
	}{
		{"contradictory-span", facts.Fact{Kind: facts.KindSymbol, Name: "src.f", File: "src/a.ts", Line: 1, EndLine: 5}},
		{"unrelated-class-scope", facts.Fact{Kind: facts.KindSymbol, Name: "src.Other.f", File: "src/a.ts", Line: 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := facts.Fact{Kind: facts.KindSymbol, Name: "f", File: "src/a.ts", Line: 20,
				Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "event"}}}
			merged := mergeFSMFacts([]facts.Fact{tc.base}, []facts.Fact{probe})
			if merged[0].Relations != nil || merged[len(merged)-1].Kind != facts.KindExtraction {
				t.Fatalf("unproven overlay bound to source: %+v", merged)
			}
		})
	}
}

func TestMergeFSMFactsBindsOnlyASTProvenNestedCallable(t *testing.T) {
	source := facts.Fact{Kind: facts.KindSymbol, Name: "src.App.sendForgotPasswordIntent", File: "src/App.tsx", Line: 1128, EndLine: 1147,
		Props: map[string]any{"symbol_kind": facts.SymbolFunc, "fsm_source_binding": "tree_sitter_nested_declaration"}}
	overlay := facts.Fact{Kind: facts.KindSymbol, Name: "sendForgotPasswordIntent", File: "src/App.tsx", Line: 1129, EndLine: 1129,
		Props:     map[string]any{"fsm_evidence": "direct_source_binding", "fsm_source_identity": "src.App.sendForgotPasswordIntent", "fsm_source_binding": "tree_sitter_nested_declaration"},
		Relations: []facts.Relation{{Kind: facts.RelFSMDispatches, Target: "mobile-app/event:FORGOT_PASSWORD_RESEND_EMAIL"}}}
	merged := mergeFSMFacts(nil, []facts.Fact{source, overlay})
	if len(merged) != 1 || merged[0].Name != source.Name || len(merged[0].Relations) != 1 || merged[0].Props["fsm_evidence_sites"] == nil {
		t.Fatalf("AST-proven nested source binding = %+v, want one source symbol owning the edge", merged)
	}

	// An identity string without the source declaration AST proof is not enough
	// to create or bind a symbol.
	unproven := mergeFSMFacts(nil, []facts.Fact{overlay})
	if len(unproven) != 1 || unproven[0].Kind != facts.KindExtraction {
		t.Fatalf("unproven nested identity fabricated a source symbol: %+v", unproven)
	}
}
