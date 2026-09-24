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

				modified := `import type {AppEvent} from './machine';import type {Intent} from './converter';
const send=(_event:AppEvent)=>{};function sendUi(event:AppEvent){send(event);}
export function dispatchIntent(intent:Intent){sendUi(intentToEvent(intent));}`
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
