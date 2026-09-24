package fsm

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

const productPin = "a609c19f3861971930fae7b33dcb2950598953c5"

type pinnedSourceManifest struct {
	SourcePin string `json:"source_pin"`
	Files     map[string]struct {
		GitBlob string `json:"git_blob"`
		SHA256  string `json:"sha256"`
		Bytes   int    `json:"bytes"`
	} `json:"files"`
}

func pinnedProduct(t *testing.T) (*Analyzer, []facts.Fact) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate pinned Product fixtures")
	}
	base := filepath.Join(filepath.Dir(file), "testdata", "product-a609c19")
	manifestBytes, err := os.ReadFile(filepath.Join(base, "SOURCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest pinnedSourceManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourcePin != productPin {
		t.Fatalf("fixture source pin = %q, want %q", manifest.SourcePin, productPin)
	}
	known := make([]string, 0, len(manifest.Files))
	for rel, record := range manifest.Files {
		b, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != record.SHA256 {
			t.Fatalf("fixture %s SHA-256 = %s, want %s", rel, got, record.SHA256)
		}
		gitObject := append([]byte("blob "+strconv.Itoa(len(b))+"\x00"), b...)
		blob := sha1.Sum(gitObject)
		if got := hex.EncodeToString(blob[:]); got != record.GitBlob {
			t.Fatalf("fixture %s Git blob = %s, want %s", rel, got, record.GitBlob)
		}
		known = append(known, rel)
	}
	sort.Strings(known)
	read := func(rel string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(slash(rel))))
		if os.IsNotExist(err) {
			return nil, err
		}
		return b, err
	}
	knownSet := map[string]bool{}
	for _, rel := range known {
		knownSet[rel] = true
	}
	resolve := func(from, specifier string) (string, bool) {
		if specifier == "@onyx/state-machine-kernel" {
			return "packages/state-machine-kernel/src/index.ts", knownSet["packages/state-machine-kernel/src/index.ts"]
		}
		if !strings.HasPrefix(specifier, ".") {
			return "", false
		}
		basePath := path.Clean(path.Join(path.Dir(slash(from)), specifier))
		for _, candidate := range []string{basePath, basePath + ".ts", basePath + ".tsx", basePath + ".js", basePath + "/index.ts", basePath + "/index.tsx"} {
			if knownSet[candidate] {
				return candidate, true
			}
		}
		return "", false
	}
	provider := Spec{
		ID: "scan-provider-job", Adapter: AdapterRuleTable,
		File:           "packages/scan-engine/src/machines/providerJob.ts",
		Factory:        SymbolRef{Module: "packages/state-machine-kernel/src/defineMachine.ts", Export: "defineMachine"},
		InstanceExport: "providerJobMachine", Registration: "createProviderJobRegistration",
		StateType: "ProviderJobState", EventType: "ProviderJobEvent", CommandType: "ProviderJobCommand",
		HandlerFiles: []string{"packages/scan-engine/src/providerJobRepository.ts"},
		DispatchFiles: []string{
			"services/workers/src/application/providerJobClaimStep.ts",
			"services/workers/src/application/providerJobFailureStep.ts",
			"services/workers/src/application/providerJobSkipStep.ts",
			"services/workers/src/application/providerJobStaleLeaseReclaimStep.ts",
		},
		DispatchSinks: []DispatchSink{{
			Factory: SymbolRef{Module: "packages/state-machine-runtime/src/step.ts", Export: "step"}, Method: "step",
			MachineTag:      &SymbolRef{Module: "services/workers/src/application/providerJobMachineRuntime.ts", Export: "ProviderJobRuntime"},
			MachineArgument: 0, EventArgument: 2, EventPath: "event",
		}},
	}
	scanRun := Spec{
		ID: "scan-run", Adapter: AdapterRuleTable,
		File:           "packages/scan-engine/src/machines/scanRun.ts",
		Factory:        SymbolRef{Module: "packages/state-machine-kernel/src/defineMachine.ts", Export: "defineMachine"},
		InstanceExport: "scanRunMachine", Registration: "createScanRunRegistration",
		StateType: "ScanRunState", EventType: "ScanRunEvent", CommandType: "ScanRunCommand",
	}
	mobile := Spec{
		ID: "mobile-app", Adapter: AdapterReducerInterpreter,
		File:       "apps/mobile/src/behavior/mobileAppInterpreter.ts",
		Dispatcher: "sendMobileAppEventTransition", Start: "startMobileAppInterpreter",
		Enter: "enterState", Stay: "stay", Reject: "rejected", Entry: "enterState",
		StateType: "MobileAppStateValue", EventType: "MobileAppEvent", EffectType: "MobileAppEffectRequest",
		CommandDiscriminant: "type", Settlements: []string{"settleMobileAppEffectTransition", "settleMobileAppEffectError"},
		EffectRunner:  &SymbolRef{Module: "apps/mobile/src/effects/mobileAppServices.ts", Export: "runMobileAppEffect"},
		HandlerFiles:  []string{"apps/mobile/src/effects/mobileAppServices.ts"},
		DispatchFiles: []string{"apps/mobile/src/App.tsx", "apps/mobile/src/screens/protect/live/ProtectTaskResolutionVisualHost.tsx"},
		DispatchSinks: []DispatchSink{{Factory: SymbolRef{Module: "apps/mobile/src/effects/mobileAppRuntime.ts", Export: "createMobileAppRuntime"}, Method: "send"}},
	}
	a := NewAnalyzer([]Spec{provider, scanRun, mobile}, known, read, resolve)
	var all []facts.Fact
	for _, rel := range known {
		src, err := read(rel)
		if err != nil {
			t.Fatal(err)
		}
		ff, _ := a.ExtractFile(rel, src)
		all = append(all, ff...)
	}
	return a, all
}

func TestPinnedProductDeclarationsAndBackendTransition(t *testing.T) {
	a, all := pinnedProduct(t)
	for id, want := range map[string][3]int{
		"scan-provider-job": {5, 8, 5},
		"scan-run":          {7, 7, 3},
	} {
		m := a.models[id]
		if m == nil || len(m.facts) == 0 || m.facts[0].Props["admission_status"] != "configured_declaration_resolved" {
			t.Fatalf("%s was not admitted from its configured declaration", id)
		}
		got := [3]int{len(m.states), len(m.events), len(m.commands)}
		if got != want {
			t.Errorf("%s vocabulary states/events/commands = %v, want %v", id, got, want)
		}
	}
	queued, ok := factByKindAndName(all, facts.KindFSMTransition, "scan-provider-job/transition:queued-claim")
	if !ok {
		t.Fatal("missing source queued-claim branch")
	}
	for rel, target := range map[string]string{
		facts.RelFSMFrom:  "scan-provider-job/state:Queued",
		facts.RelFSMOn:    "scan-provider-job/event:ClaimRequested",
		facts.RelFSMTo:    "scan-provider-job/state:Running",
		facts.RelFSMEmits: "scan-provider-job/command:CallProvider",
	} {
		if !hasRelation(queued, rel, target) {
			t.Errorf("queued-claim missing %s -> %s", rel, target)
		}
	}
	if !hasRelation(queued, facts.RelFSMGuardCalls, "hasValidPayload") || !hasRelation(queued, facts.RelFSMGuardCalls, "isDue") {
		t.Errorf("queued-claim guard evidence = %#v", queued.Relations)
	}
	if queued.Props["guard_status"] != "declared" || queued.Props["availability"] != "declared" {
		t.Errorf("queued-claim statuses = %#v", queued.Props)
	}
	if !hasRelation(queued, facts.RelFSMDeclaredIn, "createProviderJobRegistration") {
		t.Errorf("queued-claim lacks registration evidence: %#v", queued.Relations)
	}
	if hasCommandHandler(all, "scan-provider-job/command:CallProvider") {
		t.Fatal("inferred an unproven CallProvider code handler")
	}
	mutant, ok := factByKindAndName(all, facts.KindFSMTransition, "scan-provider-job/transition:completed-admin-retry-mutant")
	if !ok || mutant.Props["availability"] != "conditional" {
		t.Fatalf("conditional source mutant was made unconditional: %#v", mutant)
	}
	if !hasCommandHandler(all, "scan-provider-job/command:ReopenScanRun") {
		t.Fatal("did not bind source-proven ReopenScanRun handler")
	}
	if !coverageHasStatus(all, "typescript:fsm:scan-provider-job:handlers:packages/scan-engine/src/providerJobRepository.ts", "handler_coverage", "partial") {
		t.Fatal("one proven provider handler was treated as exhaustive coverage")
	}
	assertPinnedProductMobile(t, a, all)
}

func assertPinnedProductMobile(t *testing.T, a *Analyzer, all []facts.Fact) {
	t.Helper()
	m := a.models["mobile-app"]
	if m == nil {
		t.Fatal("mobile interpreter was not configured")
	}
	if len(m.events) != 157 || len(m.commands) != 30 {
		t.Errorf("mobile declarations events/commands = %d/%d, want 157/30 (127 event tags plus 30 effect outcomes)", len(m.events), len(m.commands))
	}
	if len(m.states) != 60 {
		t.Errorf("mobile state declarations = %d, want 60 (8 root, 5 parent, 47 nested source states)", len(m.states))
	}
	outcomeCount := 0
	for _, command := range m.commands {
		for _, rel := range command.Relations {
			if rel.Kind == facts.RelFSMOutcome && strings.HasPrefix(rel.Target, "mobile-app/event:$effect-result:") {
				outcomeCount++
			}
		}
	}
	if outcomeCount != 30 {
		t.Errorf("mobile command outcome links = %d, want one direct outcome per command", outcomeCount)
	}
	if got := coverageInt(all, "typescript:fsm:mobile-app", "event_type_if_conditions_seen"); got != 68 {
		t.Errorf("syntax-only typed-event condition count = %d, want independent oracle 68", got)
	}
	if got := coverageInt(all, "typescript:fsm:mobile-app", "enter_state_sites_seen"); got != 167 {
		t.Errorf("enterState syntax count = %d, want independent oracle 167", got)
	}
	if got := coverageInt(all, "typescript:fsm:mobile-app", "selected_return_sites_seen"); got != 275 {
		t.Errorf("selected return syntax count = %d, want independent oracle 275", got)
	}
	if got := coverageInt(all, "typescript:fsm:mobile-app", "selected_return_sites_outside_dispatch_closure"); got == 0 {
		t.Error("expected explicit unsupported return sites outside proven dispatcher closure")
	}
	if !coverageHasStatus(all, "typescript:fsm:mobile-app", "coverage_status", "partial") {
		t.Fatal("partial interpreter extraction was advertised as complete")
	}
	if coverageInt(all, "typescript:fsm:mobile-app:dispatch:apps/mobile/src/App.tsx", "dispatch_unresolved_event") == 0 {
		t.Fatal("dynamic event values at a proven sink were not retained as unknown dispatch evidence")
	}
	forgotName := "mobile-app/transition:transitionForgotPassword/forgotPassword.*/FORGOT_PASSWORD_RESEND_EMAIL|FORGOT_PASSWORD_SUBMIT_EMAIL->forgotPassword.requestingReset"
	forgot, ok := factByKindAndName(all, facts.KindFSMTransition, forgotName)
	if !ok {
		t.Fatalf("missing forgot-password OR branch %q", forgotName)
	}
	for _, target := range []string{"mobile-app/event:FORGOT_PASSWORD_SUBMIT_EMAIL", "mobile-app/event:FORGOT_PASSWORD_RESEND_EMAIL"} {
		if !hasRelation(forgot, facts.RelFSMOn, target) {
			t.Errorf("forgot-password transition lacks trigger %s", target)
		}
	}
	if !hasRelation(forgot, facts.RelFSMFrom, "mobile-app/state:forgotPassword") || !hasRelation(forgot, facts.RelFSMTo, "mobile-app/state:forgotPassword.requestingReset") {
		t.Errorf("forgot-password transition endpoints = %#v", forgot.Relations)
	}
	if !hasRelation(forgot, facts.RelFSMGuardCalls, "canSubmitResetRequest") {
		t.Errorf("forgot-password transition lacks direct guard evidence: %#v", forgot.Relations)
	}
	requesting, ok := factByKindAndName(all, facts.KindFSMState, "mobile-app/state:forgotPassword.requestingReset")
	if !ok || !hasRelation(requesting, facts.RelFSMEntryEmits, "mobile-app/command:requestPasswordReset") {
		t.Errorf("requestingReset entry effect missing: %#v", requesting)
	}
	for _, target := range []string{"mobile-app/event:FORGOT_PASSWORD_SUBMIT_EMAIL", "mobile-app/event:FORGOT_PASSWORD_RESEND_EMAIL"} {
		if !hasConstruction(all, target) || !hasDispatch(all, "sendForgotPasswordIntent", target) {
			t.Errorf("event construction and configured sink dispatch should both be proven for %s", target)
		}
	}
	if hasDispatchFromFile(all, "apps/mobile/src/screens/protect/live/ProtectTaskResolutionVisualHost.tsx") {
		t.Fatal("local useReducer send was misbound to the configured main-machine runtime")
	}
	if got := coverageInt(all, "typescript:fsm:mobile-app:handlers:apps/mobile/src/effects/mobileAppServices.ts", "handler_cases_seen"); got != 30 {
		t.Errorf("mobile command handler cases = %d, want 30", got)
	}
	if !coverageHasStatus(all, "typescript:fsm:mobile-app:handlers:apps/mobile/src/effects/mobileAppServices.ts", "handler_coverage", "complete") {
		t.Fatal("typed mobile command cases not recognized as complete")
	}
}

func factByKindAndName(all []facts.Fact, kind, name string) (facts.Fact, bool) {
	for _, f := range all {
		if f.Kind == kind && f.Name == name {
			return f, true
		}
	}
	return facts.Fact{}, false
}

func hasRelation(f facts.Fact, kind, target string) bool {
	for _, r := range f.Relations {
		if r.Kind == kind && r.Target == target {
			return true
		}
	}
	return false
}

func hasCommandHandler(all []facts.Fact, command string) bool {
	for _, f := range all {
		if hasRelation(f, facts.RelFSMHandlesCommand, command) {
			return true
		}
	}
	return false
}

func coverageFactByName(all []facts.Fact, name string) (facts.Fact, bool) {
	for _, f := range all {
		if f.Kind == facts.KindExtraction && f.Name == name {
			return f, true
		}
	}
	return facts.Fact{}, false
}

func coverageInt(all []facts.Fact, name, key string) int {
	f, ok := coverageFactByName(all, name)
	if !ok {
		return -1
	}
	if v, ok := f.Props[key].(int); ok {
		return v
	}
	return -1
}

func coverageHasStatus(all []facts.Fact, name, key, want string) bool {
	f, ok := coverageFactByName(all, name)
	return ok && f.Props[key] == want
}

func hasConstruction(all []facts.Fact, event string) bool {
	for _, f := range all {
		if hasRelation(f, facts.RelFSMConstructsEvent, event) && f.File == "apps/mobile/src/screens/forgot-password/forgotPasswordRuntimeEvents.ts" {
			return true
		}
	}
	return false
}

func hasDispatch(all []facts.Fact, sourceName, event string) bool {
	for _, f := range all {
		if f.Name == sourceName && hasRelation(f, facts.RelFSMDispatches, event) {
			return true
		}
	}
	return false
}

func hasDispatchFromFile(all []facts.Fact, rel string) bool {
	for _, f := range all {
		if f.File != rel {
			continue
		}
		for _, r := range f.Relations {
			if r.Kind == facts.RelFSMDispatches || r.Kind == facts.RelFSMDispatchesUnknownEvent {
				return true
			}
		}
	}
	return false
}
