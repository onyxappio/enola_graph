package fsm

import (
	"fmt"
	"path"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestConstructionContributionIsOwnedByReferencedCallee(t *testing.T) {
	files := map[string]string{
		"machine.ts": `export type AppState = 'idle'; export type AppEvent = {type:'A'} | {type:'B'};
function enterState(state:AppState){return state;} function rejected(state:AppState){return state;}
export function dispatch(state:AppState,event:AppEvent){if(event.type==='A') return enterState('idle');return rejected(state);}`,
		"runtime.ts": `import type {AppEvent} from './machine'; export function createRuntime(){return {send(event:AppEvent){}};}`,
		"converter.ts": `import type {AppEvent} from './machine'; export type Intent={type:'one'}|{type:'two'};
export function intentToEvent(intent:Intent):AppEvent {function uncalled(){return {type:'A'} as const;} switch(intent.type){case 'one':return {type:'A'};case 'two':return {type:'B'};}}`,
		"App.tsx": `import {createRuntime} from './runtime'; import {intentToEvent,type Intent} from './converter';
import type {AppEvent} from './machine'; const runtime=createRuntime(); const send=runtime.send; function sendUi(event:AppEvent){send(event);} export function dispatchIntent(intent:Intent){sendUi(intentToEvent(intent));}`,
	}
	read := func(file string) ([]byte, error) {
		source, ok := files[file]
		if !ok {
			return nil, fmt.Errorf("missing %s", file)
		}
		return []byte(source), nil
	}
	resolve := func(from, spec string) (string, bool) {
		base := path.Join(path.Dir(from), spec)
		for _, suffix := range []string{"", ".ts", ".tsx"} {
			if _, ok := files[base+suffix]; ok {
				return base + suffix, true
			}
		}
		return "", false
	}
	spec := Spec{
		ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts",
		Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
		DispatchFiles: []string{"App.tsx"},
		DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
	}
	a := NewAnalyzer([]Spec{spec}, []string{"machine.ts", "runtime.ts", "converter.ts", "App.tsx"}, read, resolve)

	caller, _ := a.ExtractFile("App.tsx", []byte(files["App.tsx"]))
	dispatch := false
	for _, fact := range caller {
		if fact.File != "App.tsx" {
			t.Errorf("caller contribution emits foreign file=%s kind=%s name=%s", fact.File, fact.Kind, fact.Name)
		}
		for _, relation := range fact.Relations {
			if relation.Kind == "fsm_dispatches" && relation.Target == "app/event:A" {
				dispatch = true
			}
		}
	}
	if !dispatch {
		t.Fatal("fixture lacks proven dispatch baseline")
	}

	callee, _ := a.ExtractFile("converter.ts", []byte(files["converter.ts"]))
	constructed := map[string]bool{}
	constructionSites := 0
	for _, fact := range callee {
		for _, relation := range fact.Relations {
			if relation.Kind == "fsm_constructs_event" {
				constructionSites++
				if fact.File != "converter.ts" {
					t.Errorf("callee construction has wrong owner: %s", fact.File)
				}
				constructed[relation.Target] = true
			}
		}
	}
	if constructionSites != 2 {
		t.Fatalf("callee emitted %d literal-return construction sites, want only the two executed branches", constructionSites)
	}
	for _, event := range []string{"app/event:A", "app/event:B"} {
		if !constructed[event] {
			t.Errorf("typed callee contribution lacks construction %s", event)
		}
	}
}

func TestTypedConstructorAdmissionResolvesImportedAliasesWithoutCallerUse(t *testing.T) {
	files := map[string]string{
		"machine.ts": `export type AppState='idle'; export type AppEvent={type:'A'}|{type:'B'};
function enterState(state:AppState){return state;} function rejected(state:AppState){return state;}
export function dispatch(state:AppState,event:AppEvent){if(event.type==='A')return enterState('idle');return rejected(state);}`,
		"converter.ts": `import type {AppEvent as MachineEvent} from './machine';
export function intentToEvent() /* comment before colon */
  : /* comment before alias */
  MachineEvent { return {type:'A'}; }`,
	}
	read := func(file string) ([]byte, error) {
		source, ok := files[file]
		if !ok {
			return nil, fmt.Errorf("missing %s", file)
		}
		return []byte(source), nil
	}
	resolve := func(from, spec string) (string, bool) {
		base := path.Join(path.Dir(from), spec)
		for _, suffix := range []string{"", ".ts", ".tsx"} {
			if _, ok := files[base+suffix]; ok {
				return base + suffix, true
			}
		}
		return "", false
	}
	spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts",
		Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent"}
	a := NewAnalyzer([]Spec{spec}, []string{"machine.ts", "converter.ts"}, read, resolve)
	if !sourceMayContainTypedEventReturn([]byte(files["converter.ts"])) {
		t.Fatal("byte prefilter rejected a commented imported return type")
	}
	root, kinds, done := parse("converter.ts", []byte(files["converter.ts"]))
	if done == nil {
		t.Fatal("converter source did not parse")
	}
	if exports := topLevelFunctionExports(root, []byte(files["converter.ts"]), kinds); len(exports) != 1 || exports[0] != "intentToEvent" {
		t.Fatalf("exported converter candidates = %v", exports)
	}
	function, _ := topLevelExportedFunction(root, "intentToEvent", []byte(files["converter.ts"]), kinds)
	returnType := functionReturnTypeName(function, []byte(files["converter.ts"]), kinds)
	done()
	resolvedFile, resolvedExport := a.resolveType("converter.ts", returnType)
	if returnType != "MachineEvent" || resolvedFile != "machine.ts" || resolvedExport != "AppEvent" {
		t.Fatalf("commented imported return resolution = %s -> %s#%s", returnType, resolvedFile, resolvedExport)
	}
	facts, _ := a.ExtractFile("converter.ts", []byte(files["converter.ts"]))
	if !hasOwnedConstruction(facts, "intentToEvent", "app/event:A") {
		t.Fatalf("aliased return type did not admit callee-owned event construction: %#v", facts)
	}
}

func TestSameModuleTypedConstructorAdmissionNeedsNoImport(t *testing.T) {
	files := map[string]string{
		"machine.ts": `export type AppState='idle'; export type AppEvent={type:'A'}|{type:'B'};
function enterState(state:AppState){return state;} function rejected(state:AppState){return state;}
export function dispatch(state:AppState,event:AppEvent){if(event.type==='A')return enterState('idle');return rejected(state);}
export function makeEvent(): AppEvent { return {type:'A'}; }`,
	}
	read := func(file string) ([]byte, error) {
		source, ok := files[file]
		if !ok {
			return nil, fmt.Errorf("missing %s", file)
		}
		return []byte(source), nil
	}
	resolve := func(from, spec string) (string, bool) { return "", false }
	spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts",
		Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent"}
	a := NewAnalyzer([]Spec{spec}, []string{"machine.ts"}, read, resolve)
	facts, _ := a.ExtractFile("machine.ts", []byte(files["machine.ts"]))
	if !hasOwnedConstruction(facts, "makeEvent", "app/event:A") {
		t.Fatalf("same-module typed return did not admit construction without imports: %#v", facts)
	}
}

func TestConstructorAdmissionRejectsUnrelatedSameSpellingReturnType(t *testing.T) {
	files := map[string]string{
		"machine.ts": `export type AppState='idle'; export type AppEvent={type:'A'}|{type:'B'};
function enterState(state:AppState){return state;} function rejected(state:AppState){return state;}
export function dispatch(state:AppState,event:AppEvent){if(event.type==='A')return enterState('idle');return rejected(state);}`,
		"converter.ts": `export type AppEvent={type:'A'};
export function fakeEvent(): AppEvent { return {type:'A'}; }`,
	}
	read := func(file string) ([]byte, error) {
		source, ok := files[file]
		if !ok {
			return nil, fmt.Errorf("missing %s", file)
		}
		return []byte(source), nil
	}
	resolve := func(from, spec string) (string, bool) {
		base := path.Join(path.Dir(from), spec)
		for _, suffix := range []string{"", ".ts", ".tsx"} {
			if _, ok := files[base+suffix]; ok {
				return base + suffix, true
			}
		}
		return "", false
	}
	spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts",
		Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent"}
	a := NewAnalyzer([]Spec{spec}, []string{"machine.ts", "converter.ts"}, read, resolve)
	facts, _ := a.ExtractFile("converter.ts", []byte(files["converter.ts"]))
	if hasOwnedConstruction(facts, "fakeEvent", "app/event:A") {
		t.Fatalf("same-spelling local type fabricated configured-machine construction: %#v", facts)
	}
}

func hasOwnedConstruction(all []facts.Fact, source, target string) bool {
	for _, fact := range all {
		if fact.Name != source {
			continue
		}
		for _, relation := range fact.Relations {
			if relation.Kind == "fsm_constructs_event" && relation.Target == target {
				return true
			}
		}
	}
	return false
}
