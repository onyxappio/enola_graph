package fsm

import (
	"path"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
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
	calleeFacts, _ := a.ExtractFile("converter.ts", sources["converter.ts"])
	for _, event := range []string{"A", "B"} {
		target := "app/event:" + event
		if !hasSourceDispatch(appFacts, "App.tsx", "dispatchIntent", target) {
			t.Errorf("resolved converter result was not dispatched: %s", target)
		}
		if hasConstructedEvent(appFacts, target) {
			t.Errorf("caller contribution contains a foreign callee construction: %s", target)
		}
		if !hasConstructedEvent(calleeFacts, target) {
			t.Errorf("callee-owned event construction was not represented separately: %s", target)
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
	if hasSourceDispatch(unknownFacts, "App.tsx", "dispatchIntent", "app/event:A") || hasSourceDispatch(unknownFacts, "App.tsx", "dispatchIntent", "app/event:B") {
		t.Fatal("dynamic event return produced a concrete dispatch edge")
	}
}

func TestConfiguredWrapperCallsRespectLexicalShadowing(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
		wantDispatch        bool
	}{
		{name: "baseline-bound-wrapper", wantDispatch: true},
		{name: "local-wrapper-shadow", before: "export function dispatchIntent(intent: Intent) { sendUi(intentToEvent(intent)); }", after: "export function dispatchIntent(intent: Intent) { const sendUi = (_event: AppEvent) => {}; sendUi(intentToEvent(intent)); }"},
		{name: "parameter-wrapper-shadow", before: "dispatchIntent(intent: Intent)", after: "dispatchIntent(intent: Intent, sendUi: (event: AppEvent) => void)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources := map[string][]byte{
				"machine.ts": []byte(`export type AppState = 'idle'; export type AppEvent = { type: 'A' } | { type: 'B' };
function enterState(state: AppState) { return state; }
function rejected(state: AppState, _event: string) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'A') return enterState('idle');
  return rejected(state, event.type);
}`),
				"runtime.ts": []byte(`import type { AppEvent } from './machine'; export function createRuntime() { return { send(event: AppEvent) {} }; }`),
				"converter.ts": []byte(`import type { AppEvent } from './machine';
export type Intent = { type: 'one' } | { type: 'two' };
export function intentToEvent(intent: Intent): AppEvent {
  switch (intent.type) { case 'one': return { type: 'A' }; case 'two': return { type: 'B' }; }
}`),
				"App.tsx": []byte(`import { createRuntime } from './runtime';
import { intentToEvent, type Intent } from './converter';
import type { AppEvent } from './machine';
const runtime = createRuntime();
const send = runtime.send;
function sendUi(event: AppEvent) { send(event); }
export function dispatchSibling(intent: Intent) { sendUi(intentToEvent(intent)); }
export function dispatchIntent(intent: Intent) { sendUi(intentToEvent(intent)); }`),
			}
			if tc.before != "" {
				app := string(sources["App.tsx"])
				if strings.Count(app, tc.before) != 1 {
					t.Fatalf("mutation anchor %q is not unique", tc.before)
				}
				sources["App.tsx"] = []byte(strings.Replace(app, tc.before, tc.after, 1))
			}
			spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts",
				Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
				DispatchFiles: []string{"App.tsx"},
				DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
			}
			a, _ := analyzerForSources([]Spec{spec}, sources)
			facts, _ := a.ExtractFile("App.tsx", sources["App.tsx"])
			if got := hasSourceDispatch(facts, "App.tsx", "dispatchIntent", "app/event:A"); got != tc.wantDispatch {
				t.Fatalf("shadowed caller dispatch = %v, want %v", got, tc.wantDispatch)
			}
			if !hasSourceDispatch(facts, "App.tsx", "dispatchSibling", "app/event:A") {
				t.Fatal("unshadowed sibling lost its proven wrapper dispatch")
			}
		})
	}
}

func TestImportedClosedUnionProvesGroupedConverterSwitchAndUnknownExits(t *testing.T) {
	caller := []byte(`import { toEvent } from './converter';
sendUi(toEvent(intent));`)
	converter := `import type { Intent } from './intent';
export function toEvent(intent: Intent) {
  switch (intent.type) {
    case 'A':
    case 'B':
      return { type: 'GO' } as const;
  }
}`
	typeSource := `export type Intent = { type: 'A' } | { type: 'B' };`
	conditionalConverter := strings.Replace(converter, "  switch (intent.type)", "  if (Date.now() > 0) {\n    switch (intent.type)", 1)
	conditionalConverter = strings.TrimSuffix(conditionalConverter, "}") + "  }\n}"
	cases := []struct {
		name, converter, typeSource string
		includeType, resolved       bool
	}{
		{"closed-grouped-union", converter, typeSource, true, true},
		{"expanded-union", converter, `export type Intent = { type: 'A' } | { type: 'B' } | { type: 'C' };`, true, false},
		{"missing-case", strings.Replace(converter, "    case 'B':\n", "", 1), typeSource, true, false},
		{"missing-type-dependency", converter, typeSource, false, false},
		{"nested-unused-return", strings.Replace(converter, "  switch (intent.type)", "  function unused() { return { type: 'DECOY' }; }\n  switch (intent.type)", 1), typeSource, true, true},
		{"conditional-exhaustive-switch", conditionalConverter, typeSource, true, false},
		{"bare-return", `export function toEvent(intent) { if (!intent) return; return { type: 'GO' }; }`, "", false, false},
		{"fallthrough", `export function toEvent(intent) { if (intent) return { type: 'GO' }; }`, "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := map[string][]byte{"caller.ts": caller, "converter.ts": []byte(tc.converter)}
			if tc.includeType {
				sources["intent.ts"] = []byte(tc.typeSource)
			}
			a, _ := analyzerForSources(nil, sources)
			root, kinds, done := parse("caller.ts", caller)
			if done == nil {
				t.Fatal("parse failed")
			}
			defer done()
			var args []*sitter.Node
			walk(root, func(n *sitter.Node) {
				if kinds.Of(n) == "call_expression" && calleeName(n, caller, kinds) == "sendUi" {
					args = callArguments(n, kinds)
				}
			})
			if len(args) != 1 {
				t.Fatal("dispatch argument missing")
			}
			tags, resolved, converterFile, reads := a.eventArgumentTagsWithReads(&machineModel{machine: "app"}, "caller.ts", args, caller, kinds)
			if resolved != tc.resolved {
				t.Fatalf("converter resolved=%v tags=%v, want %v", resolved, tags, tc.resolved)
			}
			if tc.resolved {
				if len(tags) != 1 || tags[0] != "GO" || converterFile != "converter.ts" {
					t.Fatalf("converter result = tags %v, dependency %q", tags, converterFile)
				}
				if !containsPath(reads, "intent.ts") {
					t.Errorf("closed union dependency missing from delta reads: %v", reads)
				}
			}
			if tc.name == "conditional-exhaustive-switch" && !containsPath(reads, "intent.ts") {
				t.Errorf("partial converter did not retain its imported type dependency: %v", reads)
			}
		})
	}
}

func TestWrapperDispatchRequiresValuePreservingArgument(t *testing.T) {
	app := `import { createRuntime } from './runtime';
import { withMachineEventSource } from './helper';
import type { AppEvent } from './machine';
const runtime = createRuntime();
const send = runtime.send;
function sendUi(event: AppEvent) { send(withMachineEventSource(event, testID)); }
export function dispatchIntent() { sendUi({ type: 'A' }); }`
	helpers := map[string]string{
		"decorator": `export function withMachineEventSource<E extends { type: string }>(event: E, testID: string) {
  if (!testID) return event;
  return { ...event, _source: { testID } };
}`,
		"metadata-only": `export function withMachineEventSource<E extends { type: string }>(event: E, testID: string) {
  return { type: 'BOOT', metadata: event, testID };
}`,
		"unknown-transform": `export function withMachineEventSource<E extends { type: string }>(event: E, testID: string) {
  return discard(event);
}`,
		"quoted-type-override": `export function withMachineEventSource<E extends { type: string }>(event: E, testID: string) {
  return { ...event, "type": "BOOT", _source: { testID } };
}`,
		"computed-type-override": `export function withMachineEventSource<E extends { type: string }>(event: E, testID: string) {
  const key: string = getKey();
  return { ...event, [key]: "BOOT", _source: { testID } };
}`,
	}
	sourcesFor := func(helper string, source string) map[string][]byte {
		return map[string][]byte{
			"machine.ts": []byte(`export type AppState = 'idle';
export type AppEvent = { type: 'A' };
function enterState(state: AppState) { return state; }
function rejected(state: AppState, _event: string) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'A') return enterState('idle');
  return rejected(state, event.type);
}`),
			"runtime.ts": []byte(`export function createRuntime() { return { send(_event: unknown) {} }; }`),
			"helper.ts":  []byte(source),
			"App.tsx":    []byte(strings.Replace(app, "withMachineEventSource", helper, 1)),
		}
	}
	spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts", Dispatcher: "dispatch",
		Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
		DispatchFiles: []string{"App.tsx"}, DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}}}
	for _, tc := range []struct {
		name, helperName string
		wantDispatch     bool
	}{
		{"source-grounded-decorator", "withMachineEventSource", true},
		{"fixed-tag-metadata", "withMachineEventSource", false},
		{"unknown-transformer", "withMachineEventSource", false},
		{"quoted-type-override", "withMachineEventSource", false},
		{"computed-type-override", "withMachineEventSource", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources := sourcesFor(tc.helperName, helpers[map[string]string{
				"source-grounded-decorator": "decorator",
				"fixed-tag-metadata":        "metadata-only",
				"unknown-transformer":       "unknown-transform",
				"quoted-type-override":      "quoted-type-override",
				"computed-type-override":    "computed-type-override",
			}[tc.name]])
			a, _ := analyzerForSources([]Spec{spec}, sources)
			appFacts, reads := a.ExtractFile("App.tsx", sources["App.tsx"])
			got := hasSourceDispatch(appFacts, "App.tsx", "dispatchIntent", "app/event:A")
			if got != tc.wantDispatch {
				t.Fatalf("caller event dispatch = %v, want %v; facts=%#v", got, tc.wantDispatch, appFacts)
			}
			if !containsPath(reads, "helper.ts") {
				t.Fatalf("forwarding proof did not retain helper source dependency: %v", reads)
			}
		})
	}
	commaSources := sourcesFor("withMachineEventSource", helpers["decorator"])
	commaSources["App.tsx"] = []byte(strings.Replace(string(commaSources["App.tsx"]),
		"send(withMachineEventSource(event, testID))", "send((discard(event), { type: 'BOOT' }))", 1))
	a, _ := analyzerForSources([]Spec{spec}, commaSources)
	commaFacts, _ := a.ExtractFile("App.tsx", commaSources["App.tsx"])
	if hasSourceDispatch(commaFacts, "App.tsx", "dispatchIntent", "app/event:A") {
		t.Fatal("comma-discard expression incorrectly forwarded the caller event")
	}
}

func TestNestedInteractionSourceCarriesExactCallableDeclaration(t *testing.T) {
	sources := map[string][]byte{
		"machine.ts": []byte(`export type AppState = 'idle';
export type AppEvent = { type: 'A' };
function enterState(state: AppState) { return state; }
function rejected(state: AppState, _event: string) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'A') return enterState('idle');
  return rejected(state, event.type);
}`),
		"runtime.ts": []byte(`export function createRuntime() { return { send(_event: unknown) {} }; }`),
		"App.tsx": []byte(`import { createRuntime } from './runtime';
import type { AppEvent } from './machine';
const runtime = createRuntime();
const send = runtime.send;
function sendUi(event: AppEvent) { send(event); }
export function App() {
  const dispatchIntent = () => sendUi({ type: 'A' });
  dispatchIntent();
}`),
	}
	spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts", Dispatcher: "dispatch",
		Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
		DispatchFiles: []string{"App.tsx"}, DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}}}
	a, _ := analyzerForSources([]Spec{spec}, sources)
	appFacts, _ := a.ExtractFile("App.tsx", sources["App.tsx"])
	if !hasSourceDispatch(appFacts, "App.tsx", "dispatchIntent", "app/event:A") {
		t.Fatal("nested caller did not retain the event dispatch evidence")
	}
	var sourceSymbol, overlay bool
	for _, fact := range appFacts {
		if fact.Kind == facts.KindSymbol && fact.Name == "..App.dispatchIntent" && fact.Props["fsm_source_binding"] == "tree_sitter_nested_declaration" {
			sourceSymbol = fact.Line > 0 && fact.EndLine >= fact.Line && fact.Props["symbol_kind"] == facts.SymbolFunc
		}
		if fact.Name == "dispatchIntent" && fact.Props["fsm_source_identity"] == "..App.dispatchIntent" {
			overlay = len(fact.Relations) > 0
		}
	}
	if !sourceSymbol || !overlay {
		t.Fatalf("nested source AST identity was not retained: source=%v overlay=%v facts=%#v", sourceSymbol, overlay, appFacts)
	}
}

func hasSourceDispatch(all []facts.Fact, file, source, event string) bool {
	for _, fact := range all {
		if fact.File != file || (fact.Name != source && fact.Props["fsm_source_identity"] != source) {
			continue
		}
		for _, relation := range fact.Relations {
			if relation.Kind == facts.RelFSMDispatches && relation.Target == event {
				return true
			}
		}
	}
	return false
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
	if !hasSourceDispatch(bound, "App.tsx", "dispatchIntent", "app/event:A") {
		t.Fatal("configured sink factory alias/re-export and returned send method were not proven")
	}

	decoy := cloneSourceMap(sources)
	decoy["runtime-impl.ts"] = []byte("export function makeRuntime() {\n" +
		"  function unrelated() { return { send(_event: unknown) {} }; }\n" +
		"  return {};\n" +
		"}")
	b, _ := analyzerForSources([]Spec{spec}, decoy)
	unbound, _ := b.ExtractFile("App.tsx", decoy["App.tsx"])
	if hasSourceDispatch(unbound, "App.tsx", "dispatchIntent", "app/event:A") {
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
