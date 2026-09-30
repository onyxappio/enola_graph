package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/fsm"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestFSMCalleeConstructionSurvivesCallerRemovalColdAndRestore(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		for _, mutation := range []string{"remove-import", "replace-sink"} {
			name := "v1/" + mutation
			if v2 {
				name = "v2/" + mutation
			}
			t.Run(name, func(t *testing.T) {
				const original = `import {createRuntime} from './runtime'; import type {AppEvent} from './machine';
import {intentToEvent,type Intent} from './converter';
const runtime=createRuntime(); const send=runtime.send;
function sendUi(event:AppEvent){send(event);}
export function dispatchIntent(intent:Intent){sendUi(intentToEvent(intent));}`
				dir := setupTSRepo(t, map[string]string{
					"machine.ts": `export type AppState='idle'; export type AppEvent={type:'A'}|{type:'B'};
function enterState(state:AppState){return state;} function rejected(state:AppState){return state;}
export function dispatch(state:AppState,event:AppEvent){if(event.type==='A')return enterState('idle');return rejected(state);}`,
					"runtime.ts": `import type {AppEvent} from './machine';export function createRuntime(){return {send(event:AppEvent){}};}`,
					"converter.ts": `import type {AppEvent} from './machine';export type Intent={type:'one'}|{type:'two'};
export function intentToEvent(intent:Intent):AppEvent{switch(intent.type){case 'one':return {type:'A'};case 'two':return {type:'B'};}}`,
					"App.tsx": original,
				})
				cfg := config.Default()
				cfg.Repo = dir
				cfg.Output.Dir = ".enola"
				cfg.StateMachines = []fsm.Spec{{
					ID: "app", Adapter: fsm.AdapterReducerInterpreter, File: "machine.ts",
					Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
					DispatchFiles: []string{"App.tsx"},
					DispatchSinks: []fsm.DispatchSink{{Factory: fsm.SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
				}}
				fresh := func() *engine.Engine {
					e, err := engine.New(cfg)
					if err != nil {
						t.Fatal(err)
					}
					e.RegisterExtractor(tsextractor.New())
					return e
				}
				eng := fresh()
				opts := Options{StateDir: filepath.Join(dir, ".enola", "state"), AuthoritativeFiles: v2}
				consumer := NewConsumer()
				initial := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, dir, initial, opts); err != nil {
					t.Fatal(err)
				}
				applyRun(t, consumer, initial)
				if !consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:A", facts.KindFSMEvent, "machine.ts") {
					t.Fatal("missing proven initial caller dispatch")
				}
				assertConstruction := func() {
					t.Helper()
					for _, event := range []string{"app/event:A", "app/event:B"} {
						if !consumerHasResolvedRelation(consumer, "..intentToEvent", "converter.ts", facts.RelFSMConstructsEvent, event, facts.KindFSMEvent, "machine.ts") {
							t.Errorf("callee declaration does not construct %s", event)
						}
					}
				}
				assertConstruction()

				var modified string
				if mutation == "replace-sink" {
					modified = `import type {AppEvent} from './machine';import {intentToEvent,type Intent} from './converter';
const send=(_event:AppEvent)=>{};function sendUi(event:AppEvent){send(event);}
export function dispatchIntent(intent:Intent){sendUi(intentToEvent(intent));}`
				} else {
					modified = `import {createRuntime} from './runtime';import type {AppEvent} from './machine';import type {Intent} from './converter';
const runtime=createRuntime();const send=runtime.send;function sendUi(event:AppEvent){send(event);}
export function dispatchIntent(intent:Intent){sendUi(intentToEvent(intent));}`
				}
				if err := os.WriteFile(filepath.Join(dir, "App.tsx"), []byte(modified), 0o644); err != nil {
					t.Fatal(err)
				}
				deltaSink := &graphstream.MemorySink{}
				delta, err := Run(context.Background(), eng, dir, deltaSink, opts)
				if err != nil {
					t.Fatal(err)
				}
				applyRun(t, consumer, deltaSink)
				if consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:A", facts.KindFSMEvent, "machine.ts") {
					t.Fatal("stale proven caller dispatch after import or sink removal")
				}
				assertConstruction()

				coldSink := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), fresh(), dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), AuthoritativeFiles: v2, ForceInitial: true}); err != nil {
					t.Fatal(err)
				}
				cold := NewConsumer()
				applyRun(t, cold, coldSink)
				assertAppliedEqualsCold(t, consumer, cold)

				quietSink := &graphstream.MemorySink{}
				quiet, err := Run(context.Background(), eng, dir, quietSink, opts)
				if err != nil {
					t.Fatal(err)
				}
				if quiet.ParsedFiles != 0 || len(quietSink.CloneRecords()) != 0 || quiet.TargetGeneration != delta.TargetGeneration {
					t.Fatalf("no-change was not silent: result=%+v events=%d", quiet, len(quietSink.CloneRecords()))
				}

				if err := os.WriteFile(filepath.Join(dir, "App.tsx"), []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
				restored := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, dir, restored, opts); err != nil {
					t.Fatal(err)
				}
				applyRun(t, consumer, restored)
				baseline := NewConsumer()
				applyRun(t, baseline, initial)
				assertAppliedEqualsCold(t, consumer, baseline)
				assertConstruction()
			})
		}
	}
}

func TestFSMCalleeConstructionFirstProofAndRestoreColdAndNoChange(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		name := "v1"
		if v2 {
			name = "v2"
		}
		t.Run(name, func(t *testing.T) {
			const noProof = `import type {AppEvent} from './machine'; import type {Intent} from './converter';
const send=(_event:AppEvent)=>{}; function sendUi(event:AppEvent){send(event);}
export function dispatchIntent(_intent:Intent){}`
			const proof = `import {createRuntime} from './runtime'; import type {AppEvent} from './machine';
import {intentToEvent,type Intent} from './converter';
const runtime=createRuntime(); const send=runtime.send;
function sendUi(event:AppEvent){send(event);}
export function dispatchIntent(intent:Intent){sendUi(intentToEvent(intent));}`
			dir := setupTSRepo(t, map[string]string{
				"machine.ts": `export type AppState='idle'; export type AppEvent={type:'A'}|{type:'B'};
function enterState(state:AppState){return state;} function rejected(state:AppState){return state;}
export function dispatch(state:AppState,event:AppEvent){if(event.type==='A')return enterState('idle');return rejected(state);}`,
				"runtime.ts": `import type {AppEvent} from './machine';export function createRuntime(){return {send(event:AppEvent){}};}`,
				"converter.ts": `export type Intent={type:'one'}|{type:'two'};
export function intentToEvent(intent:Intent){switch(intent.type){case 'one':return {type:'A'};case 'two':return {type:'B'};}}`,
				"App.tsx": noProof,
			})
			cfg := config.Default()
			cfg.Repo = dir
			cfg.Output.Dir = ".enola"
			cfg.StateMachines = []fsm.Spec{{
				ID: "app", Adapter: fsm.AdapterReducerInterpreter, File: "machine.ts",
				Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
				DispatchFiles: []string{"App.tsx"},
				DispatchSinks: []fsm.DispatchSink{{Factory: fsm.SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
			}}
			fresh := func() *engine.Engine {
				e, err := engine.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				e.RegisterExtractor(tsextractor.New())
				return e
			}
			cold := func(label string, current *Consumer) {
				t.Helper()
				sink := &graphstream.MemorySink{}
				coldCfg := *cfg
				coldCfg.Output = cfg.Output
				coldCfg.Output.Dir = filepath.Join(".enola", "cold-output-"+label)
				coldEngine, err := engine.New(&coldCfg)
				if err != nil {
					t.Fatal(err)
				}
				coldEngine.RegisterExtractor(tsextractor.New())
				if _, err := Run(context.Background(), coldEngine, dir, sink, Options{
					StateDir: filepath.Join(dir, ".enola", "cold-state-"+label), AuthoritativeFiles: v2, ForceInitial: true,
				}); err != nil {
					t.Fatal(err)
				}
				baseline := NewConsumer()
				applyRun(t, baseline, sink)
				assertAppliedEqualsCold(t, current, baseline)
			}
			assertConstructions := func(consumer *Consumer, want bool) {
				t.Helper()
				if !want {
					if consumerHasFileRelation(consumer, "..intentToEvent", "converter.ts", facts.RelFSMConstructsEvent) {
						t.Error("untyped converter emitted an FSM construction without current caller proof")
					}
					return
				}
				for _, event := range []string{"app/event:A", "app/event:B"} {
					got := consumerHasResolvedRelation(consumer, "..intentToEvent", "converter.ts", facts.RelFSMConstructsEvent, event, facts.KindFSMEvent, "machine.ts")
					if got != want {
						t.Errorf("untyped converter construction %s present=%v, want %v", event, got, want)
					}
				}
			}
			assertCallerBinding := func(consumer *Consumer, want bool) {
				t.Helper()
				dispatch := consumerHasFileRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches)
				unknown := consumerHasFileRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatchesUnknownEvent)
				if want && (!dispatch || unknown) {
					t.Errorf("caller sink proof has dispatch=%v unknown=%v, want resolved dispatch only", dispatch, unknown)
				}
				if !want && (dispatch || unknown) {
					t.Errorf("caller has dispatch=%v unknown=%v without a configured sink proof", dispatch, unknown)
				}
			}
			writeApp := func(source string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "App.tsx"), []byte(source), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			eng := fresh()
			opts := Options{StateDir: filepath.Join(dir, ".enola", "state"), AuthoritativeFiles: v2}
			consumer := NewConsumer()
			initial := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, initial, opts); err != nil {
				t.Fatal(err)
			}
			applyRun(t, consumer, initial)
			assertConstructions(consumer, false)
			assertCallerBinding(consumer, false)
			cold("no-proof", consumer)

			writeApp(proof)
			proven := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, proven, opts); err != nil {
				t.Fatal(err)
			}
			applyRun(t, consumer, proven)
			assertConstructions(consumer, true)
			assertCallerBinding(consumer, true)
			if !consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:A", facts.KindFSMEvent, "machine.ts") {
				t.Fatal("first proven caller dispatch is missing")
			}
			cold("first-proof", consumer)

			writeApp(noProof)
			removed := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, removed, opts); err != nil {
				t.Fatal(err)
			}
			applyRun(t, consumer, removed)
			assertConstructions(consumer, false)
			assertCallerBinding(consumer, false)
			if consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:A", facts.KindFSMEvent, "machine.ts") {
				t.Fatal("stale dispatch survived proof removal")
			}
			cold("removed-proof", consumer)

			writeApp(proof)
			restored := &graphstream.MemorySink{}
			restoredResult, err := Run(context.Background(), eng, dir, restored, opts)
			if err != nil {
				t.Fatal(err)
			}
			applyRun(t, consumer, restored)
			assertConstructions(consumer, true)
			assertCallerBinding(consumer, true)
			cold("restored-proof", consumer)

			quietSink := &graphstream.MemorySink{}
			quiet, err := Run(context.Background(), eng, dir, quietSink, opts)
			if err != nil {
				t.Fatal(err)
			}
			if quiet.ParsedFiles != 0 || quiet.OwnersPublished != 0 || len(quietSink.CloneRecords()) != 0 || quiet.TargetGeneration != restoredResult.TargetGeneration {
				t.Fatalf("no-change after restore was not silent: result=%+v events=%d", quiet, len(quietSink.CloneRecords()))
			}
		})
	}
}

// consumerHasFileRelation includes unresolved edges: absence assertions must not
// accidentally accept a stale relation merely because its endpoint is missing.
func consumerHasFileRelation(c *Consumer, source, file, kind string) bool {
	for owner, nodes := range c.Owners {
		for _, node := range nodes {
			if node.Kind != facts.KindSymbol || node.Name != source || node.File != file {
				continue
			}
			for _, edge := range c.Edges[owner] {
				if edge.FromID == node.ID && edge.Kind == kind {
					return true
				}
			}
		}
	}
	return false
}
