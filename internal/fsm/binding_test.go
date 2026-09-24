package fsm

import (
	"path"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestAdmissionAndBranchIdentity(t *testing.T) {
	base := map[string][]byte{
		"kernel.ts": []byte(`export function defineMachine(value: unknown) { return value; }`),
		"machine.ts": []byte(`import { defineMachine } from './kernel';
export type JobState = 'Idle' | 'Done';
export type JobEvent = { type: 'GO' };
export type JobCommand = { type: 'Do' };
export function createRegistration() {
  return { definition: defineMachine({
    id: 'toy', stateTags: ['Idle', 'Done'], eventTags: ['GO'],
    initialState: JobState.Idle(), commandTags: ['Do'],
    rules: [
      { id: 'first', from: 'Idle', on: 'GO', to: 'Done', guard: () => firstGuard() },
      { id: 'second', from: 'Idle', on: 'GO', to: 'Done', guard: () => secondGuard() },
    ],
  }) };
}`),
	}
	spec := Spec{ID: "toy", Adapter: AdapterRuleTable, File: "machine.ts", Factory: SymbolRef{Module: "kernel.ts", Export: "defineMachine"}, Registration: "createRegistration", StateType: "JobState", EventType: "JobEvent", CommandType: "JobCommand"}
	a, factsOut := analyzerForSources([]Spec{spec}, base)
	if len(factsOut) == 0 || a.models["toy"].facts[0].Props["admission_status"] != "configured_declaration_resolved" {
		t.Fatal("resolved configured factory did not admit the machine")
	}
	first, firstOK := factByKindAndName(factsOut, facts.KindFSMTransition, "toy/transition:first")
	second, secondOK := factByKindAndName(factsOut, facts.KindFSMTransition, "toy/transition:second")
	if !firstOK || !secondOK || first.Name == second.Name {
		t.Fatalf("same endpoint/event branches collapsed: first=%#v second=%#v", first, second)
	}
	if first.Props["guard_text"] == second.Props["guard_text"] {
		t.Fatalf("branch-specific guards were lost: first=%#v second=%#v", first.Props, second.Props)
	}
	trivia := append([]byte("// formatting and comments must not enter branch identity\n\n"), base["machine.ts"]...)
	withTrivia := map[string][]byte{"kernel.ts": base["kernel.ts"], "machine.ts": trivia}
	_, triviaFacts := analyzerForSources([]Spec{spec}, withTrivia)
	for _, name := range []string{"toy/transition:first", "toy/transition:second"} {
		if _, ok := factByKindAndName(triviaFacts, facts.KindFSMTransition, name); !ok {
			t.Errorf("trivia changed structural transition identity %q", name)
		}
	}
	localOnly := map[string][]byte{"machine.ts": []byte(`function defineMachine(x) { return x; }
export function createRegistration() { return defineMachine({ id: 'toy', stateTags: ['Idle'], eventTags: ['GO'], rules: [] }); }`)}
	noAdmission := Spec{ID: "toy", Adapter: AdapterRuleTable, File: "machine.ts", Factory: spec.Factory, Registration: "createRegistration"}
	_, localFacts := analyzerForSources([]Spec{noAdmission}, localOnly)
	if hasFactKind(localFacts, facts.KindFSMMachine) {
		t.Fatal("same-spelled local defineMachine admitted an unbound machine")
	}
	disabled, _ := analyzerForSources(nil, base)
	if ff, _ := disabled.ExtractFile("machine.ts", base["machine.ts"]); len(ff) != 0 {
		t.Fatal("an unconfigured repository emitted FSM facts")
	}
}

func TestOneResolvedEventConverterSeparatesConstructionAndDispatch(t *testing.T) {
	sources := map[string][]byte{
		"machine.ts": []byte(`export type AppState = 'idle';
export type AppEvent = { type: 'A' } | { type: 'B' };
function enterState(state: AppState) { return state; }
function rejected(state: AppState, _event: string) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'A') return enterState('idle');
  return rejected(state, event.type);
}`),
		"runtime.ts": []byte(`export function createRuntime() { return { send(_event: unknown) {} }; }`),
		"converter.ts": []byte(`import type { AppEvent } from './machine';
export function intentToEvent(intent: string): AppEvent {
  if (intent === 'a') return { type: 'A' } as const;
  return { type: 'B' } as const;
}`),
		"App.tsx": []byte(`import { createRuntime } from './runtime';
import type { AppEvent } from './machine';
import { intentToEvent } from './converter';
const runtime = createRuntime();
const send = runtime.send;
function sendUi(event: AppEvent) { send(event); }
export function dispatchIntent(intent: string) { sendUi(intentToEvent(intent)); }`),
	}
	spec := Spec{
		ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts", Dispatcher: "dispatch",
		Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
		DispatchFiles: []string{"App.tsx"},
		DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
	}
	a, _ := analyzerForSources([]Spec{spec}, sources)
	appFacts, reads := a.ExtractFile("App.tsx", sources["App.tsx"])
	for _, event := range []string{"A", "B"} {
		target := "app/event:" + event
		if !hasDispatch(appFacts, "dispatchIntent", target) {
			t.Errorf("resolved converter result was not dispatched: %s", target)
		}
		if !hasConstructedEvent(appFacts, target) {
			t.Errorf("event construction was not represented separately: %s", target)
		}
	}
	if !containsPath(reads, "converter.ts") {
		t.Fatalf("resolved converter is missing from direct read dependencies: %v", reads)
	}

	dynamic := cloneSourceMap(sources)
	dynamic["converter.ts"] = []byte(`import type { AppEvent } from './machine';
export function intentToEvent(intent: string): AppEvent { return { type: intent } as AppEvent; }`)
	b, _ := analyzerForSources([]Spec{spec}, dynamic)
	unknownFacts, _ := b.ExtractFile("App.tsx", dynamic["App.tsx"])
	if !hasUnknownDispatch(unknownFacts, "dispatchIntent") {
		t.Fatal("dynamic event return was guessed or dropped instead of marked unknown")
	}
	if hasDispatch(unknownFacts, "dispatchIntent", "app/event:A") || hasDispatch(unknownFacts, "dispatchIntent", "app/event:B") {
		t.Fatal("dynamic event return produced a concrete dispatch edge")
	}
}

func TestObjectSinkFactoryAliasAndReturnedMethodBinding(t *testing.T) {
	sources := map[string][]byte{
		"machine.ts": []byte("export type AppState = 'idle';\n" +
			"export type AppEvent = { type: 'A' };\n" +
			"function enterState(state: AppState) { return state; }\n" +
			"function rejected(state: AppState, _event: string) { return state; }\n" +
			"export function dispatch(state: AppState, event: AppEvent) {\n" +
			"  if (event.type === 'A') return enterState('idle');\n" +
			"  return rejected(state, event.type);\n" +
			"}"),
		"runtime-impl.ts": []byte("export function makeRuntime() {\n" +
			"  return { send(_event: unknown) {} };\n" +
			"}"),
		"runtime.ts": []byte("export { makeRuntime as createRuntime } from './runtime-impl';"),
		"App.tsx": []byte("import { createRuntime as buildRuntime } from './runtime';\n" +
			"import type { AppEvent } from './machine';\n" +
			"const runtime = buildRuntime();\n" +
			"const send = runtime.send;\n" +
			"function sendUi(event: AppEvent) { send(event); }\n" +
			"export function dispatchIntent() { sendUi({ type: 'A' }); }"),
	}
	spec := Spec{
		ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts", Dispatcher: "dispatch",
		Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
		DispatchFiles: []string{"App.tsx"},
		DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
	}
	a, _ := analyzerForSources([]Spec{spec}, sources)
	bound, _ := a.ExtractFile("App.tsx", sources["App.tsx"])
	if !hasDispatch(bound, "dispatchIntent", "app/event:A") {
		t.Fatal("configured sink factory alias/re-export and returned send method were not proven")
	}

	decoy := cloneSourceMap(sources)
	decoy["runtime-impl.ts"] = []byte("export function makeRuntime() {\n" +
		"  function unrelated() { return { send(_event: unknown) {} }; }\n" +
		"  return {};\n" +
		"}")
	b, _ := analyzerForSources([]Spec{spec}, decoy)
	unbound, _ := b.ExtractFile("App.tsx", decoy["App.tsx"])
	if hasDispatch(unbound, "dispatchIntent", "app/event:A") {
		t.Fatal("a nested decoy send method proved the runtime factory sink")
	}
}

func TestGenericTypedCommandHandlerBinding(t *testing.T) {
	sources := map[string][]byte{
		"impl.ts":   []byte(`export function defineMachine(value: unknown) { return value; }`),
		"kernel.ts": []byte(`export { defineMachine } from './impl';`),
		"machine.ts": []byte(`import { defineMachine } from './kernel';
type JobState = 'Idle' | 'Done';
type JobEvent = { type: 'START' };
export type JobCommand = { type: 'Save' };
export function createRegistration() { return { definition: defineMachine({
  id: 'jobs', stateTags: ['Idle', 'Done'], eventTags: ['START'], commandTags: ['Save'],
  rules: [{ id: 'start', from: 'Idle', on: 'START', to: 'Done' }],
}) }; }`),
		"handlers.ts": []byte(`import type { JobCommand } from './machine';
export function oldHandler(command: JobCommand) {
  switch (command.type) { case 'Save': return persist(); }
}`),
	}
	spec := Spec{ID: "jobs", Adapter: AdapterRuleTable, File: "machine.ts",
		Factory: SymbolRef{Module: "impl.ts", Export: "defineMachine"}, Registration: "createRegistration",
		StateType: "JobState", EventType: "JobEvent", CommandType: "JobCommand", CommandDiscriminant: "type",
		HandlerFiles: []string{"handlers.ts"}}
	_, all := analyzerForSources([]Spec{spec}, sources)
	if !hasCommandHandler(all, "jobs/command:Save") {
		t.Fatal("typed command handler was not recognized")
	}
}

func analyzerForSources(specs []Spec, sources map[string][]byte) (*Analyzer, []facts.Fact) {
	known := make([]string, 0, len(sources))
	set := map[string]bool{}
	for rel := range sources {
		known = append(known, rel)
		set[rel] = true
	}
	resolve := func(from, spec string) (string, bool) {
		if !strings.HasPrefix(spec, ".") {
			return "", false
		}
		base := path.Clean(path.Join(path.Dir(slash(from)), spec))
		for _, candidate := range []string{base, base + ".ts", base + ".tsx", base + "/index.ts"} {
			if set[candidate] {
				return candidate, true
			}
		}
		return "", false
	}
	a := NewAnalyzer(specs, known, func(rel string) ([]byte, error) { return sources[slash(rel)], nil }, resolve)
	var all []facts.Fact
	for rel, src := range sources {
		ff, _ := a.ExtractFile(rel, src)
		all = append(all, ff...)
	}
	return a, all
}

func cloneSourceMap(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for path, src := range in {
		out[path] = append([]byte(nil), src...)
	}
	return out
}

func hasFactKind(all []facts.Fact, kind string) bool {
	for _, f := range all {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func hasUnknownDispatch(all []facts.Fact, source string) bool {
	for _, f := range all {
		if f.Name == source && hasRelation(f, facts.RelFSMDispatchesUnknownEvent, "app") {
			return true
		}
	}
	return false
}

func hasConstructedEvent(all []facts.Fact, target string) bool {
	for _, f := range all {
		if hasRelation(f, facts.RelFSMConstructsEvent, target) {
			return true
		}
	}
	return false
}
