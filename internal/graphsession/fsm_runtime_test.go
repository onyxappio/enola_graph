package graphsession

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/fsm"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestPinnedProductFSMRuntimePublicationAndDelta(t *testing.T) {
	const sourcePin = "a609c19f3861971930fae7b33dcb2950598953c5"
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate pinned Product FSM fixtures")
	}
	fixtureDir := filepath.Join(filepath.Dir(testFile), "..", "fsm", "testdata", "product-a609c19")
	manifestBytes, err := os.ReadFile(filepath.Join(fixtureDir, "SOURCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SourcePin string `json:"source_pin"`
		Files     map[string]struct {
			GitBlob string `json:"git_blob"`
			SHA256  string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourcePin != sourcePin {
		t.Fatalf("fixture source pin = %q, want %q", manifest.SourcePin, sourcePin)
	}
	dir := t.TempDir()
	for rel, entry := range manifest.Files {
		source, err := os.ReadFile(filepath.Join(fixtureDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(source)
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			t.Fatalf("pinned source %s SHA-256 = %s, want %s", rel, got, entry.SHA256)
		}
		gitObject := append([]byte("blob "+strconv.Itoa(len(source))+"\x00"), source...)
		blob := sha1.Sum(gitObject)
		if got := hex.EncodeToString(blob[:]); got != entry.GitBlob {
			t.Fatalf("pinned source %s Git blob = %s, want %s", rel, got, entry.GitBlob)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsconfig := `{"compilerOptions":{"baseUrl":".","paths":{
  "@onyx/state-machine-kernel":["packages/state-machine-kernel/src/index.ts"],
  "@onyx/state-machine-runtime":["packages/state-machine-runtime/src/index.ts"],
  "@onyx/scan-engine":["packages/scan-engine/src/index.ts"]
}}}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"product-fsm-fixture","type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.StateMachines = []fsm.Spec{
		{
			ID: "scan-provider-job", Adapter: fsm.AdapterRuleTable,
			File:           "packages/scan-engine/src/machines/providerJob.ts",
			Factory:        fsm.SymbolRef{Module: "packages/state-machine-kernel/src/defineMachine.ts", Export: "defineMachine"},
			InstanceExport: "providerJobMachine", Registration: "createProviderJobRegistration",
			StateType: "ProviderJobState", EventType: "ProviderJobEvent", CommandType: "ProviderJobCommand",
			HandlerFiles: []string{"packages/scan-engine/src/providerJobRepository.ts"},
			DispatchFiles: []string{
				"services/workers/src/application/providerJobClaimStep.ts",
				"services/workers/src/application/providerJobFailureStep.ts",
				"services/workers/src/application/providerJobSkipStep.ts",
				"services/workers/src/application/providerJobStaleLeaseReclaimStep.ts",
			},
			DispatchSinks: []fsm.DispatchSink{{
				Factory: fsm.SymbolRef{Module: "packages/state-machine-runtime/src/step.ts", Export: "step"},
				Method:  "step", MachineTag: &fsm.SymbolRef{Module: "services/workers/src/application/providerJobMachineRuntime.ts", Export: "ProviderJobRuntime"},
				MachineArgument: 0, EventArgument: 2, EventPath: "event",
			}},
		},
		{
			ID: "mobile-app", Adapter: fsm.AdapterReducerInterpreter,
			File:       "apps/mobile/src/behavior/mobileAppInterpreter.ts",
			Dispatcher: "sendMobileAppEventTransition", Start: "startMobileAppInterpreter",
			Enter: "enterState", Stay: "stay", Reject: "rejected", Entry: "enterState",
			StateType: "MobileAppStateValue", EventType: "MobileAppEvent", EffectType: "MobileAppEffectRequest",
			CommandDiscriminant: "type",
			Settlements:         []string{"settleMobileAppEffectTransition", "settleMobileAppEffectError"},
			EffectRunner:        &fsm.SymbolRef{Module: "apps/mobile/src/effects/mobileAppServices.ts", Export: "runMobileAppEffect"},
			HandlerFiles:        []string{"apps/mobile/src/effects/mobileAppServices.ts"},
			DispatchFiles:       []string{"apps/mobile/src/App.tsx", "apps/mobile/src/screens/protect/live/ProtectTaskResolutionVisualHost.tsx"},
			DispatchSinks: []fsm.DispatchSink{{
				Factory: fsm.SymbolRef{Module: "apps/mobile/src/effects/mobileAppRuntime.ts", Export: "createMobileAppRuntime"},
				Method:  "send",
			}},
		},
	}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	state := filepath.Join(dir, ".enola", "product-fsm-state")
	options := Options{StateDir: state, AuthoritativeFiles: true}
	consumer := NewConsumer()

	started := time.Now()
	initialSink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), eng, dir, initialSink, options)
	initialElapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	initialEvents := len(initialSink.CloneRecords())
	if initial.TargetGeneration != 1 || initial.ParsedFiles == 0 || initial.OwnersPublished == 0 || initialEvents == 0 {
		t.Fatalf("pinned Product initial FSM graph was not published: result=%+v events=%d", initial, initialEvents)
	}
	applyRun(t, consumer, initialSink)
	for _, name := range []string{
		"scan-provider-job/transition:queued-claim",
		"mobile-app/transition:transitionForgotPassword/forgotPassword.*/FORGOT_PASSWORD_RESEND_EMAIL|FORGOT_PASSWORD_SUBMIT_EMAIL->forgotPassword.requestingReset",
	} {
		if !consumerHasNode(consumer, facts.KindFSMTransition, name) {
			t.Errorf("pinned Product transition was not published: %s", name)
		}
	}
	if !consumerHasResolvedRelation(consumer, "services/workers/src/application.stepClaimRequested", "services/workers/src/application/providerJobClaimStep.ts",
		facts.RelFSMDispatches, "scan-provider-job/event:ClaimRequested", facts.KindFSMEvent, "packages/scan-engine/src/machines/providerJob.ts") {
		t.Fatal("resolved Effect sink did not publish the ClaimRequested dispatch")
	}
	if !consumerHasResolvedRelation(consumer, "apps/mobile/src.OnyxApp.renderForgotPasswordScreen.sendForgotPasswordIntent", "apps/mobile/src/App.tsx",
		facts.RelFSMDispatches, "mobile-app/event:FORGOT_PASSWORD_RESEND_EMAIL", facts.KindFSMEvent, "apps/mobile/src/state/mobileAppMachine.types.ts") {
		t.Fatal("resolved mobile runtime did not publish the forgot-password event dispatch")
	}
	if !consumerHasRelation(consumer, "mobile-app/state:forgotPassword.requestingReset", facts.RelFSMEntryEmits, "mobile-app/command:requestPasswordReset") {
		t.Fatal("mobile entry effect did not reach the runtime graph")
	}

	started = time.Now()
	quietSink := &graphstream.MemorySink{}
	quiet, err := Run(context.Background(), eng, dir, quietSink, options)
	quietElapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if quiet.ParsedFiles != 0 || quiet.OwnersPublished != 0 || len(quietSink.CloneRecords()) != 0 || quiet.TargetGeneration != initial.TargetGeneration {
		t.Fatalf("pinned Product no-change run was not silent: result=%+v events=%d", quiet, len(quietSink.CloneRecords()))
	}
	quietEngine, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	quietEngine.RegisterExtractor(tsextractor.New())
	started = time.Now()
	freshQuietSink := &graphstream.MemorySink{}
	freshQuiet, err := Run(context.Background(), quietEngine, dir, freshQuietSink, options)
	freshQuietElapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if freshQuiet.ParsedFiles != 0 || freshQuiet.OwnersPublished != 0 || len(freshQuietSink.CloneRecords()) != 0 || freshQuiet.TargetGeneration != initial.TargetGeneration {
		t.Fatalf("fresh-engine no-change run was not silent: result=%+v events=%d", freshQuiet, len(freshQuietSink.CloneRecords()))
	}

	converterPath := filepath.Join(dir, "apps/mobile/src/screens/forgot-password/forgotPasswordRuntimeEvents.ts")
	converterSource, err := os.ReadFile(converterPath)
	if err != nil {
		t.Fatal(err)
	}
	changedSource := strings.Replace(string(converterSource),
		"return { type: 'FORGOT_PASSWORD_RESEND_EMAIL' } as const;",
		"return { type: 'FORGOT_PASSWORD_SUBMIT_EMAIL' } as const;", 1)
	if changedSource == string(converterSource) {
		t.Fatal("pinned forgot-password converter return was not found for the delta scenario")
	}
	if err := os.WriteFile(converterPath, []byte(changedSource), 0o644); err != nil {
		t.Fatal(err)
	}
	// A fresh Engine/TSExtractor models a new CLI invocation: no in-memory FSM
	// analyzer survives, while the durable graph state and FileRecords do.
	deltaEngine, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	deltaEngine.RegisterExtractor(tsextractor.New())
	started = time.Now()
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), deltaEngine, dir, deltaSink, options)
	deltaElapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	deltaEvents := len(deltaSink.CloneRecords())
	if delta.TargetGeneration != initial.TargetGeneration+1 || delta.ParsedFiles == 0 || delta.OwnersPublished == 0 || deltaEvents == 0 {
		t.Fatalf("pinned Product FSM edit did not publish a delta: result=%+v events=%d", delta, deltaEvents)
	}
	applyRun(t, consumer, deltaSink)
	if consumerHasResolvedRelation(consumer, "apps/mobile/src.OnyxApp.renderForgotPasswordScreen.sendForgotPasswordIntent", "apps/mobile/src/App.tsx",
		facts.RelFSMDispatches, "mobile-app/event:FORGOT_PASSWORD_RESEND_EMAIL", facts.KindFSMEvent, "apps/mobile/src/state/mobileAppMachine.types.ts") ||
		!consumerHasResolvedRelation(consumer, "apps/mobile/src.OnyxApp.renderForgotPasswordScreen.sendForgotPasswordIntent", "apps/mobile/src/App.tsx",
			facts.RelFSMDispatches, "mobile-app/event:FORGOT_PASSWORD_SUBMIT_EMAIL", facts.KindFSMEvent, "apps/mobile/src/state/mobileAppMachine.types.ts") {
		t.Fatal("changed one-callee converter return was not reflected in the dispatch graph")
	}
	if !consumerHasNode(consumer, facts.KindFSMTransition, "mobile-app/transition:transitionForgotPassword/forgotPassword.*/FORGOT_PASSWORD_RESEND_EMAIL|FORGOT_PASSWORD_SUBMIT_EMAIL->forgotPassword.requestingReset") {
		t.Fatal("converter return edit unexpectedly changed the declaration transition")
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{
		StateDir: filepath.Join(dir, ".enola", "product-fsm-cold"), AuthoritativeFiles: true, ForceInitial: true,
	}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, consumer, cold)

	t.Logf("Product FSM runtime (source %s; partial mobile syntax adapter; converter-return edit): initial=%s scope=%d parsed=%d events=%d; warm-nochange=%s scope=%d parsed=%d events=0; fresh-engine-nochange=%s scope=%d parsed=%d events=0; fresh-engine delta=%s scope=%d parsed=%d events=%d; cold-equivalent=true",
		sourcePin, initialElapsed, initial.OwnersPublished, initial.ParsedFiles, initialEvents,
		quietElapsed, quiet.OwnersPublished, quiet.ParsedFiles,
		freshQuietElapsed, freshQuiet.OwnersPublished, freshQuiet.ParsedFiles,
		deltaElapsed, delta.OwnersPublished, delta.ParsedFiles, deltaEvents)
}

func TestFSMRuntimePublicationDeltaColdAndRetirement(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"impl.ts": `export function defineMachine(value: unknown) { return value; }
`,
		"kernel.ts": `export { defineMachine } from './impl';
`,
		"machine.ts": `import { defineMachine } from './kernel';
type JobState = 'Idle' | 'Done';
type JobEvent = { type: 'START' } | { type: 'STOP' };
type JobCommand = { type: 'Save' };
export function createRegistration() {
  return {
    definition: defineMachine({
      id: 'jobs',
      stateTags: ['Idle', 'Done'],
      eventTags: ['START', 'STOP'],
      initialState: JobState.Idle(),
      rules: [{ id: 'start', from: 'Idle', on: 'START', to: 'Done', reduce: (s, e) => ({ next: s, commands: [JobCommand.Save()] }) }],
    }),
    commandTags: ['Save'],
  };
}

	`,
	})
	spec := fsm.Spec{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "machine.ts",
		Factory:      fsm.SymbolRef{Module: "impl.ts", Export: "defineMachine"},
		Registration: "createRegistration", StateType: "JobState", EventType: "JobEvent", CommandType: "JobCommand",
	}
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.StateMachines = []fsm.Spec{spec}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	state := filepath.Join(dir, ".enola", "graph-state")
	options := Options{StateDir: state, AuthoritativeFiles: true}
	consumer := NewConsumer()
	initialSink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), eng, dir, initialSink, options)
	if err != nil {
		t.Fatal(err)
	}
	if initial.TargetGeneration != 1 || initial.ParsedFiles == 0 || len(initialSink.CloneRecords()) == 0 {
		t.Fatalf("initial FSM graph was not published: result=%+v records=%d", initial, len(initialSink.CloneRecords()))
	}
	applyRun(t, consumer, initialSink)
	if !consumerHasNode(consumer, facts.KindFSMMachine, "jobs") || !consumerHasNode(consumer, facts.KindFSMTransition, "jobs/transition:start") {
		t.Fatal("resolved FSM declarations and transition did not reach the v2 graph consumer")
	}
	if !consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMOn, "jobs/event:START") || !consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMEmits, "jobs/command:Save") {
		t.Fatal("transition relations were not published to the runtime graph")
	}

	quietSink := &graphstream.MemorySink{}
	quiet, err := Run(context.Background(), eng, dir, quietSink, options)
	if err != nil {
		t.Fatal(err)
	}
	if quiet.ParsedFiles != 0 || len(quietSink.CloneRecords()) != 0 || quiet.TargetGeneration != initial.TargetGeneration || quiet.BaseGeneration != initial.TargetGeneration {
		t.Fatalf("no-change FSM run was not silent: result=%+v records=%d", quiet, len(quietSink.CloneRecords()))
	}

	machinePath := filepath.Join(dir, "machine.ts")
	before, err := os.ReadFile(machinePath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(before), "on: 'START'", "on: 'STOP'", 1)
	if changed == string(before) {
		t.Fatal("test machine source did not contain its expected transition trigger")
	}
	if err := os.WriteFile(machinePath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, deltaSink, options)
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles == 0 || len(deltaSink.CloneRecords()) == 0 || delta.TargetGeneration != initial.TargetGeneration+1 {
		t.Fatalf("machine edit did not publish a generation: result=%+v records=%d", delta, len(deltaSink.CloneRecords()))
	}
	applyRun(t, consumer, deltaSink)
	if consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMOn, "jobs/event:START") || !consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMOn, "jobs/event:STOP") {
		t.Fatal("delta left the stale transition event or omitted the new event")
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "fsm-cold"), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, consumer, cold)

	if err := os.Remove(machinePath); err != nil {
		t.Fatal(err)
	}
	retiredSink := &graphstream.MemorySink{}
	retired, err := Run(context.Background(), eng, dir, retiredSink, options)
	if err != nil {
		t.Fatal(err)
	}
	if retired.TargetGeneration != delta.TargetGeneration+1 || len(retiredSink.CloneRecords()) == 0 {
		t.Fatalf("machine deletion did not retire its owned graph: result=%+v records=%d", retired, len(retiredSink.CloneRecords()))
	}
	applyRun(t, consumer, retiredSink)
	if consumerHasNode(consumer, facts.KindFSMMachine, "jobs") || consumerHasNode(consumer, facts.KindFSMTransition, "jobs/transition:start") {
		t.Fatal("deleted machine facts remained in the applied graph")
	}
	retiredColdSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, retiredColdSink, Options{StateDir: filepath.Join(dir, ".enola", "fsm-retired-cold"), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	retiredCold := NewConsumer()
	applyRun(t, retiredCold, retiredColdSink)
	assertAppliedEqualsCold(t, consumer, retiredCold)
}

func TestFSMRuntimeV1PublishesColdEquivalentMachineDelta(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"impl.ts": `export function defineMachine(value: unknown) { return value; }
`,
		"machine.ts": `import { defineMachine } from './impl';
type JobState = 'Idle' | 'Done';
type JobEvent = { type: 'GO' } | { type: 'NEXT' };
export function createRegistration() { return { definition: defineMachine({
  id: 'jobs', stateTags: ['Idle', 'Done'], eventTags: ['GO', 'NEXT'],
  rules: [{ id: 'start', from: 'Idle', on: 'GO', to: 'Done' }],
}) }; }
`,
	})
	spec := fsm.Spec{ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "machine.ts",
		Factory: fsm.SymbolRef{Module: "impl.ts", Export: "defineMachine"}, Registration: "createRegistration",
		StateType: "JobState", EventType: "JobEvent"}
	cfg := config.Default()
	cfg.Repo, cfg.Output.Dir, cfg.StateMachines = dir, ".enola", []fsm.Spec{spec}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	state := filepath.Join(dir, ".enola", "graph-state-v1")
	options := Options{StateDir: state} // default protocol is v1
	consumer := NewConsumer()
	initialSink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), eng, dir, initialSink, options)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, consumer, initialSink)
	if !consumerHasNode(consumer, facts.KindFSMMachine, "jobs") || !consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMOn, "jobs/event:GO") {
		t.Fatal("v1 graph publication omitted the configured FSM declaration or transition")
	}
	quietSink := &graphstream.MemorySink{}
	quiet, err := Run(context.Background(), eng, dir, quietSink, options)
	if err != nil {
		t.Fatal(err)
	}
	if quiet.ParsedFiles != 0 || quiet.OwnersPublished != 0 || len(quietSink.CloneRecords()) != 0 || quiet.TargetGeneration != initial.TargetGeneration {
		t.Fatalf("v1 FSM no-change run was not silent: result=%+v", quiet)
	}
	machinePath := filepath.Join(dir, "machine.ts")
	changed, err := os.ReadFile(machinePath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(changed), "on: 'GO'", "on: 'NEXT'", 1)
	if updated == string(changed) {
		t.Fatal("v1 test machine did not contain its expected trigger")
	}
	if err := os.WriteFile(machinePath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, deltaSink, options)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, consumer, deltaSink)
	if delta.TargetGeneration != initial.TargetGeneration+1 || !consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMOn, "jobs/event:NEXT") ||
		consumerHasRelation(consumer, "jobs/transition:start", facts.RelFSMOn, "jobs/event:GO") {
		t.Fatalf("v1 FSM delta did not replace its transition: result=%+v", delta)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{
		StateDir: filepath.Join(dir, ".enola", "fsm-v1-cold"), ForceInitial: true,
	}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, consumer, cold)
}

func TestFSMRuntimeInvalidatesReexportCalleeChanges(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"impl.ts": `export function defineMachine(value: unknown) { return value; }
`,
		"kernel.ts": `export { defineMachine } from './impl';
`,
		"machine.ts": `import { defineMachine } from './kernel';
export type JobState = 'Idle' | 'Done';
export type JobEvent = { type: 'START' };
export function createRegistration() {
  return { definition: defineMachine({
    id: 'jobs', stateTags: ['Idle', 'Done'], eventTags: ['START'],
    initialState: JobState.Idle(), rules: [{ id: 'start', from: 'Idle', on: 'START', to: 'Done' }],
  }) };
}
`,
	})
	spec := fsm.Spec{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "machine.ts",
		Factory:      fsm.SymbolRef{Module: "impl.ts", Export: "defineMachine"},
		Registration: "createRegistration", StateType: "JobState", EventType: "JobEvent",
	}
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.StateMachines = []fsm.Spec{spec}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	state := filepath.Join(dir, ".enola", "graph-state")
	consumer := NewConsumer()
	firstSink := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, firstSink, Options{StateDir: state, AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.ParsedFiles == 0 {
		t.Fatal("initial source files were not parsed")
	}
	applyRun(t, consumer, firstSink)
	if !consumerHasNode(consumer, facts.KindFSMMachine, "jobs") {
		t.Fatal("configured re-exported factory was not admitted")
	}

	// The importer and re-export barrel stay byte-identical. The changed callee
	// removes the configured export and must invalidate the machine contribution.
	if err := os.WriteFile(filepath.Join(dir, "impl.ts"), []byte(`export function buildMachine(value: unknown) { return value; }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	parsedMachine := false
	changedSink := &graphstream.MemorySink{}
	changed, err := Run(context.Background(), eng, dir, changedSink, Options{
		StateDir: state, AuthoritativeFiles: true,
		OnBeforeParse: func(rel string) { parsedMachine = parsedMachine || rel == "machine.ts" },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !parsedMachine || changed.ParsedFiles == 0 || len(changedSink.CloneRecords()) == 0 {
		t.Fatalf("re-exported callee change skipped its machine owner: parsed_machine=%v result=%+v records=%d", parsedMachine, changed, len(changedSink.CloneRecords()))
	}
	applyRun(t, consumer, changedSink)
	if consumerHasNode(consumer, facts.KindFSMMachine, "jobs") || consumerHasNode(consumer, facts.KindFSMTransition, "jobs/transition:start") {
		t.Fatal("changed configured factory retained stale machine contributions")
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "fsm-cold"), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, consumer, cold)
}

func TestFSMRuntimeConverterReturnDeltaMatchesCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"machine.ts": `export type AppState = 'idle';
export type AppEvent = { type: 'A' } | { type: 'B' };
function enterState(state: AppState) { return state; }
function rejected(state: AppState, _event: string) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'A') return enterState('idle');
  return rejected(state, event.type);
}
`,
		"runtime.ts": `import type { AppEvent } from './machine';
export function createRuntime() { return { send(_event: AppEvent) {} }; }
`,
		"converter.ts": `import type { AppEvent } from './machine';
export function intentToEvent(_intent: string): AppEvent { return { type: 'A' } as const; }
`,
		"App.tsx": `import { createRuntime } from './runtime';
import type { AppEvent } from './machine';
import { intentToEvent } from './converter';
const runtime = createRuntime();
const send = runtime.send;
function sendUi(event: AppEvent) { send(event); }
export function dispatchIntent(intent: string) { sendUi(intentToEvent(intent)); }
`,
	})
	spec := fsm.Spec{
		ID: "app", Adapter: fsm.AdapterReducerInterpreter, File: "machine.ts",
		Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
		DispatchFiles: []string{"App.tsx"},
		DispatchSinks: []fsm.DispatchSink{{Factory: fsm.SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
	}
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.StateMachines = []fsm.Spec{spec}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	state := filepath.Join(dir, ".enola", "graph-state")
	options := Options{StateDir: state, AuthoritativeFiles: true}
	consumer := NewConsumer()
	firstSink := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, firstSink, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.ParsedFiles == 0 {
		t.Fatal("initial machine and dispatch files were not parsed")
	}
	applyRun(t, consumer, firstSink)
	if !consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:A", facts.KindFSMEvent, "machine.ts") {
		t.Fatal("configured runtime did not publish the literal converter event")
	}

	converter := filepath.Join(dir, "converter.ts")
	changedSource, err := os.ReadFile(converter)
	if err != nil {
		t.Fatal(err)
	}
	changedSource = []byte(strings.Replace(string(changedSource), "type: 'A'", "type: 'B'", 1))
	if err := os.WriteFile(converter, changedSource, 0o644); err != nil {
		t.Fatal(err)
	}
	var parsedApp atomic.Bool
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, deltaSink, Options{
		StateDir: state, AuthoritativeFiles: true,
		OnBeforeParse: func(rel string) {
			if rel == "App.tsx" {
				parsedApp.Store(true)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !parsedApp.Load() || delta.ParsedFiles == 0 || len(deltaSink.CloneRecords()) == 0 {
		t.Fatalf("converter return edit skipped the dispatch owner: parsed_app=%v result=%+v records=%d", parsedApp.Load(), delta, len(deltaSink.CloneRecords()))
	}
	applyRun(t, consumer, deltaSink)
	if consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:A", facts.KindFSMEvent, "machine.ts") ||
		!consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, "app/event:B", facts.KindFSMEvent, "machine.ts") {
		t.Fatal("dispatch delta retained A or omitted the callee's current B return")
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "fsm-cold"), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, consumer, cold)
}

func TestFSMRuntimeCommandHandlerRenameAndDeletion(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"impl.ts": `export function defineMachine(value: unknown) { return value; }
`,
		"kernel.ts": `export { defineMachine } from './impl';
`,
		"machine.ts": `import { defineMachine } from './kernel';
type JobState = 'Idle' | 'Done';
type JobEvent = { type: 'START' };
export type JobCommand = { type: 'Save' };
export function createRegistration() {
  return { definition: defineMachine({
    id: 'jobs', stateTags: ['Idle', 'Done'], eventTags: ['START'],
    initialState: JobState.Idle(), commandTags: ['Save'],
    rules: [{ id: 'start', from: 'Idle', on: 'START', to: 'Done' }],
  }) };
}


`,
		"handlers.ts": `import type { JobCommand } from './machine';
export function oldHandler(command: JobCommand) {
  switch (command.type) {
    case 'Save': return persist();
  }
}
`,
	})
	spec := fsm.Spec{
		ID: "jobs", Adapter: fsm.AdapterRuleTable, File: "machine.ts",
		Factory: fsm.SymbolRef{Module: "impl.ts", Export: "defineMachine"}, Registration: "createRegistration",
		StateType: "JobState", EventType: "JobEvent", CommandType: "JobCommand",
		HandlerFiles: []string{"handlers.ts"}, CommandDiscriminant: "type",
	}
	cfg := config.Default()
	cfg.Repo, cfg.Output.Dir, cfg.StateMachines = dir, ".enola", []fsm.Spec{spec}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	state := filepath.Join(dir, ".enola", "graph-state")
	options := Options{StateDir: state, AuthoritativeFiles: true}
	consumer := NewConsumer()
	initialSink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), eng, dir, initialSink, options)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, consumer, initialSink)
	if !consumerHasResolvedRelation(consumer, "..oldHandler", "handlers.ts", facts.RelFSMHandlesCommand, "jobs/command:Save", facts.KindFSMCommand, "machine.ts") {
		t.Fatal("initial type-bound command handler was not published")
	}

	handlerPath := filepath.Join(dir, "handlers.ts")
	handler, err := os.ReadFile(handlerPath)
	if err != nil {
		t.Fatal(err)
	}
	renamed := strings.Replace(string(handler), "oldHandler", "newHandler", 1)
	if renamed == string(handler) {
		t.Fatal("test handler declaration was not found")
	}
	if err := os.WriteFile(handlerPath, []byte(renamed), 0o644); err != nil {
		t.Fatal(err)
	}
	renameSink := &graphstream.MemorySink{}
	rename, err := Run(context.Background(), eng, dir, renameSink, options)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, consumer, renameSink)
	if rename.TargetGeneration != initial.TargetGeneration+1 ||
		consumerHasResolvedRelation(consumer, "..oldHandler", "handlers.ts", facts.RelFSMHandlesCommand, "jobs/command:Save", facts.KindFSMCommand, "machine.ts") ||
		!consumerHasResolvedRelation(consumer, "..newHandler", "handlers.ts", facts.RelFSMHandlesCommand, "jobs/command:Save", facts.KindFSMCommand, "machine.ts") {
		t.Fatalf("handler rename retained stale evidence or omitted its replacement: result=%+v", rename)
	}
	assertFSMConsumerCold(t, consumer, eng, dir, "handler-rename-cold")

	if err := os.Remove(handlerPath); err != nil {
		t.Fatal(err)
	}
	deleteSink := &graphstream.MemorySink{}
	deleted, err := Run(context.Background(), eng, dir, deleteSink, options)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, consumer, deleteSink)
	if deleted.TargetGeneration != rename.TargetGeneration+1 ||
		consumerHasResolvedRelation(consumer, "..newHandler", "handlers.ts", facts.RelFSMHandlesCommand, "jobs/command:Save", facts.KindFSMCommand, "machine.ts") {
		t.Fatalf("handler deletion retained its owned evidence: result=%+v", deleted)
	}
	assertFSMConsumerCold(t, consumer, eng, dir, "handler-delete-cold")
}

func TestFSMRuntimeImportedUnionBodyChangeMatchesColdAndRestores(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authoritative bool
	}{{"v1", false}, {"v2", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupTSRepo(t, map[string]string{
				"machine.ts": `export type AppState = 'idle';
export type AppEvent = { type: 'A' } | { type: 'B' };
function enterState(state: AppState) { return state; }
function rejected(state: AppState, _event: string) { return state; }
export function dispatch(state: AppState, event: AppEvent) {
  if (event.type === 'A') return enterState('idle');
  return rejected(state, event.type);
}`,
				"runtime.ts": `import type { AppEvent } from './machine';
export function createRuntime() { return { send(_event: AppEvent) {} }; }`,
				"intent.ts": `export type Intent = { type: 'one' } | { type: 'two' };`,
				"converter.ts": `import type { AppEvent } from './machine';
import type { Intent } from './intent';
export function intentToEvent(intent: Intent): AppEvent {
  switch (intent.type) {
    case 'one': return { type: 'A' };
    case 'two': return { type: 'B' };
  }
}`,
				"App.tsx": `import { createRuntime } from './runtime';
import type { AppEvent } from './machine';
import type { Intent } from './intent';
import { intentToEvent } from './converter';
const runtime = createRuntime();
const send = runtime.send;
function sendUi(event: AppEvent) { send(event); }
export function dispatchIntent(intent: Intent) { sendUi(intentToEvent(intent)); }`,
			})
			spec := fsm.Spec{ID: "app", Adapter: fsm.AdapterReducerInterpreter, File: "machine.ts",
				Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "AppState", EventType: "AppEvent",
				DispatchFiles: []string{"App.tsx"},
				DispatchSinks: []fsm.DispatchSink{{Factory: fsm.SymbolRef{Module: "runtime.ts", Export: "createRuntime"}, Method: "send"}},
			}
			cfg := config.Default()
			cfg.Repo, cfg.Output.Dir, cfg.StateMachines = dir, ".enola", []fsm.Spec{spec}
			newEngine := func() *engine.Engine {
				eng, err := engine.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				eng.RegisterExtractor(tsextractor.New())
				return eng
			}
			eng := newEngine()
			state := filepath.Join(dir, ".enola", "fsm-imported-union-"+tc.name)
			options := Options{StateDir: state, AuthoritativeFiles: tc.authoritative}
			consumer := NewConsumer()
			initialSink := &graphstream.MemorySink{}
			initial, err := Run(context.Background(), eng, dir, initialSink, options)
			if err != nil {
				t.Fatal(err)
			}
			if initial.TargetGeneration != 1 || initial.ParsedFiles == 0 {
				t.Fatalf("initial imported-union graph missing: %+v", initial)
			}
			applyRun(t, consumer, initialSink)
			for _, event := range []string{"app/event:A", "app/event:B"} {
				if !consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, event, facts.KindFSMEvent, "machine.ts") {
					t.Fatalf("initial converter dispatch %s is not source-bound/resolved", event)
				}
			}

			intentPath := filepath.Join(dir, "intent.ts")
			if err := os.WriteFile(intentPath, []byte(`export type Intent = { type: 'one' } | { type: 'two' } | { type: 'three' };`), 0o644); err != nil {
				t.Fatal(err)
			}
			var parsedApp atomic.Bool
			deltaSink := &graphstream.MemorySink{}
			delta, err := Run(context.Background(), eng, dir, deltaSink, Options{
				StateDir: state, AuthoritativeFiles: tc.authoritative,
				OnBeforeParse: func(rel string) {
					if rel == "App.tsx" {
						parsedApp.Store(true)
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !parsedApp.Load() || delta.ParsedFiles == 0 || len(deltaSink.CloneRecords()) == 0 {
				t.Fatalf("imported type-body change skipped the dispatch owner: parsedApp=%v result=%+v", parsedApp.Load(), delta)
			}
			applyRun(t, consumer, deltaSink)
			for _, event := range []string{"app/event:A", "app/event:B"} {
				if consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, event, facts.KindFSMEvent, "machine.ts") {
					t.Fatalf("non-exhaustive converter retained proven dispatch %s", event)
				}
			}
			if !consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatchesUnknownEvent, "app", facts.KindFSMMachine, "machine.ts") {
				t.Fatal("expanded imported union lost its unknown dispatch coverage")
			}
			coldSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), newEngine(), dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "fsm-imported-union-cold-"+tc.name), AuthoritativeFiles: tc.authoritative, ForceInitial: true}); err != nil {
				t.Fatal(err)
			}
			cold := NewConsumer()
			applyRun(t, cold, coldSink)
			assertAppliedEqualsCold(t, consumer, cold)

			nochangeSink := &graphstream.MemorySink{}
			nochange, err := Run(context.Background(), eng, dir, nochangeSink, options)
			if err != nil {
				t.Fatal(err)
			}
			if nochange.ParsedFiles != 0 || nochange.OwnersPublished != 0 || len(nochangeSink.CloneRecords()) != 0 || nochange.TargetGeneration != delta.TargetGeneration {
				t.Fatalf("unchanged expanded union did not stay silent: result=%+v records=%d", nochange, len(nochangeSink.CloneRecords()))
			}

			if err := os.WriteFile(intentPath, []byte(`export type Intent = { type: 'one' } | { type: 'two' };`), 0o644); err != nil {
				t.Fatal(err)
			}
			restoreSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, restoreSink, options); err != nil {
				t.Fatal(err)
			}
			applyRun(t, consumer, restoreSink)
			for _, event := range []string{"app/event:A", "app/event:B"} {
				if !consumerHasResolvedRelation(consumer, "..dispatchIntent", "App.tsx", facts.RelFSMDispatches, event, facts.KindFSMEvent, "machine.ts") {
					t.Fatalf("restoring closed union did not restore dispatch %s", event)
				}
			}
			restoreColdSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), newEngine(), dir, restoreColdSink, Options{StateDir: filepath.Join(dir, ".enola", "fsm-imported-union-restore-cold-"+tc.name), AuthoritativeFiles: tc.authoritative, ForceInitial: true}); err != nil {
				t.Fatal(err)
			}
			restoreCold := NewConsumer()
			applyRun(t, restoreCold, restoreColdSink)
			assertAppliedEqualsCold(t, consumer, restoreCold)
		})
	}
}

func assertFSMConsumerCold(t *testing.T, got *Consumer, eng *engine.Engine, dir, stateName string) {
	t.Helper()
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{
		StateDir: filepath.Join(dir, ".enola", stateName), AuthoritativeFiles: true, ForceInitial: true,
	}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, sink)
	assertAppliedEqualsCold(t, got, cold)
}

func consumerHasNode(c *Consumer, kind, name string) bool {
	for _, nodes := range c.Owners {
		for _, node := range nodes {
			if node.Kind == kind && node.Name == name {
				return true
			}
		}
	}
	return false
}

func consumerHasRelation(c *Consumer, source, kind, target string) bool {
	for owner, nodes := range c.Owners {
		for _, node := range nodes {
			if node.Name != source {
				continue
			}
			for _, edge := range c.Edges[owner] {
				if edge.FromID == node.ID && edge.Kind == kind && edge.TargetName == target {
					return true
				}
			}
		}
	}
	return false
}

func consumerHasResolvedRelation(c *Consumer, source, sourceFile, relationKind, target, targetKind, targetFile string) bool {
	for owner, nodes := range c.Owners {
		for _, sourceNode := range nodes {
			if sourceNode.Kind != facts.KindSymbol || sourceNode.Name != source || sourceNode.File != sourceFile {
				continue
			}
			for _, edge := range c.Edges[owner] {
				if edge.FromID != sourceNode.ID || edge.Kind != relationKind || edge.TargetName != target || edge.Resolution != graphstream.ResResolved || edge.TargetID == "" {
					continue
				}
				for _, targetNodes := range c.Owners {
					for _, targetNode := range targetNodes {
						if targetNode.ID == edge.TargetID && targetNode.Kind == targetKind && targetNode.Name == target && targetNode.File == targetFile {
							return true
						}
					}
				}
			}
		}
	}
	return false
}
