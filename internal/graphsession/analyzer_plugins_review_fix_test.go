package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/analyzerplugin"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestPluginRunRequestExposesOnlyDeclaredSummaries(t *testing.T) {
	units := []analyzerplugin.UnitDecl{{ID: "file:a", Kind: "file", Consumes: []string{"model:config"}}}
	all := map[string]any{"model:config": map[string]any{"v": 1}, "secret:other": map[string]any{"v": 2}}
	got := analyzerplugin.DeclaredSummaries(units, all)
	if len(got) != 1 {
		t.Fatalf("declared summary map = %#v", got)
	}
	if _, ok := got["model:config"]; !ok {
		t.Fatal("declared producer missing from filtered summaries")
	}
	if _, ok := got["secret:other"]; ok {
		t.Fatal("undeclared summary leaked into run request")
	}
}

func TestPluginAnchorValidationMatchesNodeRelationRules(t *testing.T) {
	p := analyzerplugin.Loaded{Manifest: analyzerplugin.Manifest{
		Name: "toy", Claims: analyzerplugin.Claims{Machines: []string{"toy"}}, OwnerDomain: []string{"src/*"},
	}}
	unit := analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file"}
	result := analyzerplugin.UnitResult{Unit: unit.ID, Owners: map[string]analyzerplugin.OwnerResult{
		"src/a.ts": {Anchors: []analyzerplugin.Anchor{{
			Symbol: "caller", Line: 1,
			Relations: []analyzerplugin.Relation{{Kind: "fsm_dispatches", Target: "toy/state:idle"}},
		}}},
	}}
	result = analyzerplugin.CanonicalizeResult(result)
	if err := analyzerplugin.ValidateResult(p, unit, result); err == nil {
		t.Fatal("fsm_dispatches to a state identity was accepted")
	}
	result.Owners["src/a.ts"] = analyzerplugin.OwnerResult{Anchors: []analyzerplugin.Anchor{{
		Symbol: "caller", Line: 1,
		Relations: []analyzerplugin.Relation{{Kind: "fsm_dispatches", Target: "toy/event:go"}},
	}}}
	result = analyzerplugin.CanonicalizeResult(result)
	if err := analyzerplugin.ValidateResult(p, unit, result); err != nil {
		t.Fatalf("valid event target rejected: %v", err)
	}
	if result.Owners["src/a.ts"].Anchors[0].Owner != "src/a.ts" {
		t.Fatalf("empty anchor owner was not inherited: %#v", result.Owners["src/a.ts"].Anchors[0])
	}
}

func TestPluginAnchorOwnersEnterFrozenContribIndex(t *testing.T) {
	s := &session{pluginContribs: map[string][]facts.Fact{}}
	records := map[string]analyzerplugin.PluginRecord{
		"alpha": {Identity: "id-a", Units: map[string]analyzerplugin.UnitRecord{
			"file:src/a.ts": {Owners: map[string]analyzerplugin.OwnerResult{
				"src/a.ts": {
					Nodes:   []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "alpha/state:idle", Owner: "src/a.ts", Line: 1}},
					Anchors: []analyzerplugin.Anchor{{Symbol: "dispatch", Line: 2, Relations: []analyzerplugin.Relation{{Kind: "fsm_dispatches", Target: "alpha/event:go"}}}},
				},
			}},
		}},
		"beta": {Identity: "id-b", Units: map[string]analyzerplugin.UnitRecord{
			"file:src/b.ts": {Owners: map[string]analyzerplugin.OwnerResult{
				"src/b.ts": {Anchors: []analyzerplugin.Anchor{{Symbol: "other", Line: 1, Relations: []analyzerplugin.Relation{{Kind: "fsm_dispatches", Target: "beta/event:x"}}}}},
			}},
		}},
	}
	if err := s.collectPluginContributions(records, map[string]bool{"src/a.ts": true, "src/b.ts": true}); err != nil {
		t.Fatal(err)
	}
	alpha := s.pluginContribs["plugin:alpha"]
	foundAnchor := false
	for _, f := range alpha {
		if f.Kind == facts.KindSymbol && f.Name == "dispatch" {
			foundAnchor = true
			if f.Props["plugin_anchor"] != true {
				t.Fatalf("anchor contrib missing marker: %#v", f.Props)
			}
		}
	}
	if !foundAnchor {
		t.Fatalf("anchor was not persisted into plugin contribs: %#v", alpha)
	}
	seeds := pluginOwnerSeeds(records)
	if len(seeds) < 2 || seeds[0] != "src/a.ts" || seeds[1] != "src/b.ts" {
		t.Fatalf("plugin owner seeds = %v", seeds)
	}
	merged := mergeAnalyzerPluginAnchors(nil, s.pluginAnchors)
	if len(merged) < 2 {
		t.Fatalf("expected deterministic unbound coverage facts, got %d", len(merged))
	}
	if merged[0].Props["plugin"].(string) > merged[1].Props["plugin"].(string) {
		t.Fatalf("multi-plugin anchor evidence order is not sorted: %v then %v", merged[0].Props["plugin"], merged[1].Props["plugin"])
	}
}

func inventoryReusePlugin(t *testing.T, root, counter string) analyzerplugin.Config {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for analyzer plugin integration tests")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "inventory-fixture")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: inventory-fixture
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files:
  - plugin.mjs
vocabularies:
  - enola.fsm@1
claims:
  machines:
    - inventory-fixture
owner_domain:
  - src/*
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	counterJSON, _ := json.Marshal(counter)
	script := fmt.Sprintf(`import { createInterface } from "node:readline";
import { appendFileSync } from "node:fs";
const counter = %s;
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    const listed = await callback("list", { unit: "@plan", glob: "src/tracked.ts" });
    send({ id: message.id, op: "plan_result", units: [
      { id: "file:src/tracked.ts", kind: "file", params: { file: "src/tracked.ts" } }
    ] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      appendFileSync(counter, unit.id + "\n");
      const source = await callback("read", { unit: unit.id, path: unit.params.file });
      const node = { kind: "fsm_state", name: "inventory-fixture/state:idle", owner: unit.params.file, line: 1, props: { body: source.text ?? "" } };
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [unit.params.file]: { nodes: [node] } }, summary: { body_length: (source.text ?? "").length } });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) { pending.delete(message.cb); resolve(message); }
    return;
  }
  void handle(message);
});
`, string(counterJSON))
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/analyzers/inventory-fixture"}
}

func TestRepositoryAnalyzerPluginUnrelatedFileAdditionReusesUnits(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/tracked.ts": `export const tracked = 1;`,
		"src/other.ts":   `export const other = 1;`,
	})
	counter := filepath.Join(t.TempDir(), "unit-runs.log")
	registration := inventoryReusePlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "inventory-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"inventory-fixture"}}
	cons := NewConsumer()
	firstSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, firstSink, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, firstSink)
	if got := pluginCounterLines(t, counter); len(got) != 1 || got[0] != "file:src/tracked.ts" {
		t.Fatalf("cold unit runs = %v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "src/unrelated.ts"), []byte(`export const unrelated = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, deltaSink)
	if got := pluginCounterLines(t, counter); len(got) != 1 {
		t.Fatalf("unrelated addition reran plugin units: %v stats=%+v", got, delta.AnalyzerPlugins)
	}
	if delta.AnalyzerPlugins.UnitsReused == 0 || delta.AnalyzerPlugins.UnitsExecuted != 0 {
		t.Fatalf("expected unit reuse on unrelated addition, got %+v", delta.AnalyzerPlugins)
	}
	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"inventory-fixture"}}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestRepositoryAnalyzerPluginVerifyModeDoesNotAdvance(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "verify-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir)
	verifyOpts := opts
	verifyOpts.PluginVerify = true
	verifySink := &graphstream.MemorySink{}
	verify, err := Run(context.Background(), eng, root, verifySink, verifyOpts)
	if err != nil {
		t.Fatal(err)
	}
	after := pluginState(t, stateDir)
	if after.Generation != before.Generation {
		t.Fatalf("plugin-verify advanced generation from %d to %d", before.Generation, after.Generation)
	}
	if len(verifySink.CloneRecords()) != 0 {
		t.Fatal("plugin-verify published graph records")
	}
	if verify.AnalyzerPlugins.ProcessesStarted == 0 || verify.AnalyzerPlugins.UnitsExecuted == 0 {
		t.Fatalf("plugin-verify did not re-run units: %+v", verify.AnalyzerPlugins)
	}
	if len(pluginCounterLines(t, counter)) < 2 {
		t.Fatal("plugin-verify did not spawn a verification process")
	}
}

func TestRepositoryAnalyzerPluginSlowPluginAllowsUnrelatedLocalStream(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/held.ts":     `export const held = 1;`,
		"outside/free.ts": `export const free = 1;`,
		"package.json":    `{"name":"slow-plugin","private":true}`,
		"tsconfig.json":   `{"compilerOptions":{"strict":true},"include":["src/**/*","outside/**/*"]}`,
	})
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "slow")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: slow
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files: [plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [slow]
owner_domain:
  - src/*
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	gate := filepath.Join(t.TempDir(), "gate")
	gateJSON, _ := json.Marshal(gate)
	script := fmt.Sprintf(`import { createInterface } from "node:readline";
import { existsSync } from "node:fs";
const gate = %s;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    while (!existsSync(gate)) { await sleep(20); }
    send({ id: message.id, op: "plan_result", units: [{ id: "file:src/held.ts", kind: "file", params: { file: "src/held.ts" } }] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const source = await callback("read", { unit: unit.id, path: unit.params.file });
      const node = { kind: "fsm_state", name: "slow/state:idle", owner: unit.params.file, line: 1, props: { body: source.text ?? "" } };
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [unit.params.file]: { nodes: [node] } }, summary: {} });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) { pending.delete(message.cb); resolve(message); }
    return;
  }
  void handle(message);
});
`, string(gateJSON))
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{{Path: "tools/analyzers/slow"}}
	sink := &graphstream.MemorySink{}
	var sawFreeLocal atomic.Bool
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), eng, root, sink, Options{
			StateDir: t.TempDir(), AllowRepoPlugins: []string{"slow"},
		})
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, rec := range sink.CloneRecords() {
			var probe struct {
				Type  string             `json:"type"`
				Phase string             `json:"phase"`
				Nodes []graphstream.Node `json:"nodes"`
			}
			if json.Unmarshal(rec.Payload, &probe) != nil {
				continue
			}
			if probe.Type != graphstream.TypeBatch || probe.Phase != graphstream.PhaseLocal {
				continue
			}
			for _, n := range probe.Nodes {
				owner := n.Owner.ID
				if strings.Contains(owner, "outside/free.ts") || strings.Contains(n.File, "outside/free.ts") {
					sawFreeLocal.Store(true)
				}
				if strings.Contains(owner, "src/held.ts") || strings.Contains(n.File, "src/held.ts") {
					t.Fatalf("held owner streamed before plugin settle: %#v", n)
				}
			}
		}
		if sawFreeLocal.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawFreeLocal.Load() {
		localBeforeGate := 0
		for _, rec := range sink.CloneRecords() {
			var probe struct {
				Type  string `json:"type"`
				Phase string `json:"phase"`
			}
			if json.Unmarshal(rec.Payload, &probe) != nil {
				continue
			}
			if probe.Type == graphstream.TypeBatch && probe.Phase == graphstream.PhaseLocal {
				localBeforeGate++
			}
		}
		if localBeforeGate == 0 {
			t.Fatal("no local batches streamed while slow plugin was blocked")
		}
	}
	if err := os.WriteFile(gate, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryAnalyzerPluginNoChangeUsesRuntimeStatCache(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "runtime-cache-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	state := pluginState(t, stateDir)
	if len(state.RuntimeFingerprints) == 0 {
		t.Fatal("expected runtime fingerprint cache after cold plugin run")
	}
	noop, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if noop.AnalyzerPlugins.ProcessesStarted != 0 || len(pluginCounterLines(t, counter)) != 1 {
		t.Fatalf("no-change spawned plugin work: %+v starts=%d", noop.AnalyzerPlugins, len(pluginCounterLines(t, counter)))
	}
}

func TestPluginCapturedSourcesIsolatedFromConcurrentTSMap(t *testing.T) {
	s := &session{
		capturedSources:       map[string][]byte{"src/a.ts": []byte("main")},
		pluginCapturedSources: map[string][]byte{},
	}
	const n = 2000
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			s.pluginCapturedSources[fmt.Sprintf("plugin-%d.ts", i%16)] = []byte("plugin")
		}
	}()
	for i := 0; i < n; i++ {
		_ = s.capturedSources["src/a.ts"]
		s.capturedSources[fmt.Sprintf("ts-%d.ts", i%16)] = []byte("ts")
	}
	<-done
	if err := s.mergePluginCapturedSources(); err != nil {
		t.Fatal(err)
	}
	if got := string(s.capturedSources["src/a.ts"]); got != "main" {
		t.Fatalf("shared map lost main capture: %q", got)
	}
	if _, ok := s.capturedSources["plugin-0.ts"]; !ok {
		t.Fatal("plugin captures were not merged after isolation")
	}
}

func TestPluginCallbackUsesRuntimeSourceSnapshotDuringConcurrentTSCapture(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a = 1;\n"})
	eng := admissionEngine(t, root, graphinput.Options{})
	source := []byte("export const a = 1;\n")
	sum := sha256.Sum256(source)
	unit := "file:src/a.ts"
	s := &session{
		abs:                   root,
		eng:                   eng,
		capturedSources:       map[string][]byte{},
		cfgCaptured:           map[string][]byte{},
		pluginCapturedSources: map[string][]byte{},
	}
	input := &runtimeInputs{
		policyIdentity: eng.GraphScope().Policy.Identity(),
		sources:        map[string][]byte{"src/a.ts": source},
	}
	hashes := map[string]string{"src/a.ts": hex.EncodeToString(sum[:])}
	obs := map[string]analyzerplugin.Observation{}
	plugin := analyzerplugin.Loaded{Manifest: analyzerplugin.Manifest{Name: "fixture"}}
	started := make(chan struct{})
	done := make(chan struct{})
	var writes atomic.Int32
	go func() {
		defer close(done)
		close(started)
		for i := 0; i < 20000; i++ {
			s.capturedSources[fmt.Sprintf("expanded/%d.ts", i)] = []byte("dependency")
			writes.Add(1)
			runtime.Gosched()
		}
	}()
	<-started
	for writes.Load() == 0 {
		runtime.Gosched()
	}
	for i := 0; i < 256; i++ {
		got, err := s.pluginCallback(context.Background(), plugin, map[string]any{
			"op": "read", "unit": unit, "path": "src/a.ts",
		}, unit, obs, input, []string{"src/a.ts"}, nil, hashes, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got["text"] != string(source) {
			t.Fatalf("plugin read = %#v, want immutable runtime source snapshot", got)
		}
	}
	<-done
}

func TestPluginContributionReplacementDoesNotMutateCommittedFileState(t *testing.T) {
	committed := &FileState{
		Hash:      "committed-hash",
		Extractor: "markdown",
		Facts:     []facts.Fact{{Kind: facts.KindSymbol, Name: "doc:readme"}},
		Contrib: map[string][]facts.Fact{
			"plugin:fixture": {{Kind: facts.KindFSMState, Name: "fixture/state:idle"}},
			"other":          {{Kind: facts.KindSymbol, Name: "other:fact"}},
		},
		ContribHash: map[string]string{"plugin:fixture": "committed-hash", "other": "committed-hash"},
	}
	newOwner := &FileState{
		Hash:        "new-owner-hash",
		Extractor:   "markdown",
		Contrib:     map[string][]facts.Fact{"other": {{Kind: facts.KindSymbol, Name: "new:other"}}},
		ContribHash: map[string]string{"other": "new-owner-hash"},
	}
	files := map[string]*FileState{"README.md": committed, "docs/new.md": newOwner}
	contributions := map[string][]facts.Fact{
		"plugin:fixture": {{Kind: facts.KindFSMState, Name: "fixture/state:active", File: "docs/new.md"}},
	}
	replacePluginContributions(files, contributions, map[string]string{
		"README.md": "next-hash", "docs/new.md": "new-owner-hash",
	}, admittedOwnerSet([]string{"README.md", "docs/new.md"}, map[string]string{
		"README.md": "next-hash", "docs/new.md": "new-owner-hash",
	}))
	if got := committed.Contrib["plugin:fixture"]; len(got) != 1 {
		t.Fatalf("committed plugin contribution changed before transaction commit: %#v", got)
	}
	if got := committed.ContribHash["plugin:fixture"]; got != "committed-hash" {
		t.Fatalf("committed plugin hash changed before transaction commit: %q", got)
	}
	staged := files["README.md"]
	if staged == committed {
		t.Fatal("replacement mutated committed file state in place")
	}
	if _, ok := staged.Contrib["plugin:fixture"]; ok {
		t.Fatal("staged old plugin contribution was not replaced")
	}
	if _, ok := staged.Contrib["other"]; !ok {
		t.Fatal("replacement dropped an unrelated committed contribution")
	}
	if files["docs/new.md"] == newOwner {
		t.Fatal("replacement added plugin output to a committed file state in place")
	}
	if _, ok := newOwner.Contrib["plugin:fixture"]; ok {
		t.Fatal("new plugin output corrupted committed state before transaction commit")
	}
	if got := files["docs/new.md"].Contrib["plugin:fixture"]; len(got) != 1 {
		t.Fatalf("new owner plugin contribution = %#v", got)
	}
}

func TestReplacePluginContributionsSkipsDeletedOwners(t *testing.T) {
	files := map[string]*FileState{
		"src/keep.ts": {Hash: "keep", Contrib: map[string][]facts.Fact{}, ContribHash: map[string]string{}},
	}
	contributions := map[string][]facts.Fact{
		"plugin:fixture": {
			{Kind: facts.KindFSMState, Name: "fixture-machine/state:idle", File: "src/keep.ts"},
			{Kind: facts.KindFSMState, Name: "fixture-machine/state:idle", File: "src/gone.ts"},
		},
	}
	hashes := map[string]string{"src/keep.ts": "keep"}
	replacePluginContributions(files, contributions, hashes, admittedOwnerSet([]string{"src/keep.ts"}, hashes))
	if _, ok := files["src/gone.ts"]; ok {
		t.Fatal("deleted owner was resurrected from cached plugin contributions")
	}
	if got := files["src/keep.ts"].Contrib["plugin:fixture"]; len(got) != 1 {
		t.Fatalf("kept owner contributions = %#v", got)
	}
}

func TestPluginVisibleNamesRespectsPolicyExclusions(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":       `export const a = 1;`,
		"artifacts/x.ts": `export const x = 1;`,
	})
	eng := admissionEngine(t, root, graphinput.Options{Exclude: []string{"artifacts/**"}})
	allNames := []string{"src/a.ts", "artifacts/x.ts", "package-lock.json", "package.json"}
	visible := pluginVisibleNames(eng, allNames)
	for _, name := range visible {
		if name == "artifacts/x.ts" || name == "package-lock.json" {
			t.Fatalf("excluded or lockfile name leaked into plan digest set: %v", visible)
		}
	}
	if len(visible) == 0 || visible[0] != "src/a.ts" && !containsSlashPath(visible, "src/a.ts") {
		t.Fatalf("expected admitted source in visible names: %v", visible)
	}
	without := nameSetDigest(pluginVisibleNames(eng, []string{"src/a.ts", "package.json"}))
	withExcluded := nameSetDigest(pluginVisibleNames(eng, allNames))
	if without != withExcluded {
		t.Fatalf("excluded inventory names changed PlanFilesDigest: %s vs %s", without, withExcluded)
	}
}

func TestClonePluginRecordIsolatesFailedTransactionWrites(t *testing.T) {
	original := analyzerplugin.PluginRecord{
		Identity: "id",
		Units: map[string]analyzerplugin.UnitRecord{
			"file:src/a.ts": {
				Decl: analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file"},
				Owners: map[string]analyzerplugin.OwnerResult{
					"src/a.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "fixture-machine/state:idle"}}},
				},
				OutputDigest: "old",
			},
		},
	}
	state := map[string]analyzerplugin.PluginRecord{"fixture": original}
	staged := analyzerplugin.ClonePluginRecord(state["fixture"])
	staged.Units["file:src/a.ts"] = analyzerplugin.UnitRecord{
		Decl:         analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file", Params: map[string]any{"file": "src/a.ts"}},
		OutputDigest: "partial-failure",
	}
	if state["fixture"].Units["file:src/a.ts"].OutputDigest != "old" {
		t.Fatal("partial plugin cache write mutated committed resident state")
	}
}

func TestUnitDeclChangeInvalidatesCachedUnit(t *testing.T) {
	old := analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file", Params: map[string]any{"file": "src/a.ts"}, Consumes: []string{"model:config"}}
	same := analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file", Params: map[string]any{"file": "src/a.ts"}, Consumes: []string{"model:config"}}
	changedParams := analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file", Params: map[string]any{"file": "src/a.ts", "mode": "strict"}, Consumes: []string{"model:config"}}
	changedConsumes := analyzerplugin.UnitDecl{ID: "file:src/a.ts", Kind: "file", Params: map[string]any{"file": "src/a.ts"}, Consumes: []string{"model:config", "model:extra"}}
	if !analyzerplugin.UnitDeclsEqual(old, same) {
		t.Fatal("identical declarations compared unequal")
	}
	if analyzerplugin.UnitDeclsEqual(old, changedParams) {
		t.Fatal("params change did not invalidate declaration equality")
	}
	if analyzerplugin.UnitDeclsEqual(old, changedConsumes) {
		t.Fatal("consumes change did not invalidate declaration equality")
	}
}

func TestEvidenceSiteKeyOrdersPropsAndRelations(t *testing.T) {
	left := evidenceSiteKey(map[string]any{
		"line": 1, "end_line": 1,
		"props":     map[string]any{"plugin": "a", "tag": "one"},
		"relations": []map[string]string{{"kind": "fsm_dispatches", "target": "a/event:go"}},
	})
	right := evidenceSiteKey(map[string]any{
		"line": 1, "end_line": 1,
		"props":     map[string]any{"plugin": "a", "tag": "two"},
		"relations": []map[string]string{{"kind": "fsm_dispatches", "target": "a/event:go"}},
	})
	if left == right {
		t.Fatal("evidence sites with distinct props shared a sort key")
	}
}

func TestPluginVerifySuppressesNonAuthoritativeLocalBatches(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      `export const a = 1;`,
		"outside/b.ts":  `export const b = 1;`,
		"package.json":  `{"name":"verify-local","private":true}`,
		"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["src/**/*","outside/**/*"]}`,
	})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "verify-local-state")
	opts := Options{StateDir: stateDir, AllowRepoPlugins: []string{"fixture"}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir).Generation
	verifySink := &graphstream.MemorySink{}
	verifyOpts := opts
	verifyOpts.PluginVerify = true
	if _, err := Run(context.Background(), eng, root, verifySink, verifyOpts); err != nil {
		t.Fatal(err)
	}
	for _, rec := range verifySink.CloneRecords() {
		var probe struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
		}
		if json.Unmarshal(rec.Payload, &probe) != nil {
			continue
		}
		if probe.Type == graphstream.TypeBatch && probe.Phase == graphstream.PhaseLocal {
			t.Fatal("plugin-verify published a local batch")
		}
		if probe.Type == graphstream.TypeBeginReplace || probe.Type == graphstream.TypeEndReplace {
			t.Fatalf("plugin-verify published %s", probe.Type)
		}
	}
	if pluginState(t, stateDir).Generation != before {
		t.Fatal("plugin-verify advanced generation")
	}
}

func TestResidentReloadsPluginIdentityEachTransaction(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "resident-reload-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	res, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir).AnalyzerPlugins["fixture"].Identity
	entry := filepath.Join(root, registration.Path, "plugin.mjs")
	body, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, append(body, []byte("\n// identity bump\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := res.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	after := pluginState(t, stateDir).AnalyzerPlugins["fixture"].Identity
	if after == "" || after == before {
		t.Fatalf("resident transaction reused stale plugin identity: before=%s after=%s", before, after)
	}

	if err := os.RemoveAll(filepath.Join(root, registration.Path)); err != nil {
		t.Fatal(err)
	}
	if _, err := res.reconcile(context.Background(), true); err == nil {
		t.Fatal("deleted configured plugin was accepted by resident transaction")
	}
}

func TestBoundAnalyzerPluginConfigsEmptyLiveRetiresPlugins(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{{Path: "tools/analyzers/fixture"}}
	s := &session{
		eng:  eng,
		opts: Options{analyzerPluginConfigs: []analyzerplugin.Config{{Path: "tools/analyzers/old"}}},
	}
	got := s.boundAnalyzerPluginConfigs()
	if len(got) != 1 || got[0].Path != "tools/analyzers/fixture" {
		t.Fatalf("live non-empty config ignored: %#v", got)
	}
	eng.Config().AnalyzerPlugins = nil
	got = s.boundAnalyzerPluginConfigs()
	if len(got) != 0 {
		t.Fatalf("explicit empty live config fell back to open-time registrations: %#v", got)
	}
}

func TestResidentExplicitEmptyPluginConfigRetiresContributions(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "retire-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	res, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if len(pluginState(t, stateDir).AnalyzerPlugins) == 0 {
		t.Fatal("expected committed plugin cache before explicit removal")
	}
	// Clear plugins on the resident's live engine (rebuild may have replaced the opener).
	res.eng.Config().AnalyzerPlugins = nil
	if _, err := res.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	after := pluginState(t, stateDir)
	if len(after.AnalyzerPlugins) != 0 {
		t.Fatalf("explicit empty live config left plugin cache: %#v", after.AnalyzerPlugins)
	}
}

func TestPluginOwnerHoldSnapshotIgnoresConcurrentReload(t *testing.T) {
	s := &session{}
	holdPlugins := []analyzerplugin.Loaded{{
		Manifest: analyzerplugin.Manifest{Name: "held", OwnerDomain: []string{"src/*"}},
	}}
	s.freezePluginOwnerHoldSnapshot(holdPlugins)
	s.setAnalyzerPlugins(nil, nil) // concurrent reload cleared live plugins
	if !s.pluginOwnerHeld("src/a.ts") {
		t.Fatal("owner_domain hold was lost while concurrent reload cleared analyzerPlugins")
	}
	if s.pluginOwnerHeld("outside/b.ts") {
		t.Fatal("hold snapshot matched outside owner_domain")
	}
}

func TestReloadAnalyzerPluginsAndOwnerHoldAreRaceFree(t *testing.T) {
	s := &session{}
	s.freezePluginOwnerHoldSnapshot([]analyzerplugin.Loaded{{
		Manifest: analyzerplugin.Manifest{Name: "held", OwnerDomain: []string{"src/*"}},
	}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			s.setAnalyzerPlugins(nil, []analyzerplugin.Loaded{{
				Manifest: analyzerplugin.Manifest{Name: "other", OwnerDomain: []string{"other/*"}},
			}})
			_ = s.analyzerPluginsLocked()
		}
	}()
	for i := 0; i < 1000; i++ {
		if !s.pluginOwnerHeld("src/a.ts") {
			t.Fatal("hold decision raced with reload")
		}
	}
	<-done
}

func TestResidentLaterPluginRegistrationRunsWithoutReopen(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	// Open with no plugins configured.
	stateDir := filepath.Join(root, ".enola", "late-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	res, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	first, err := res.reconcile(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if first.AnalyzerPlugins.ProcessesStarted != 0 || len(pluginState(t, stateDir).AnalyzerPlugins) != 0 {
		t.Fatalf("opened-without-plugins still ran plugin work: %+v state=%#v", first.AnalyzerPlugins, pluginState(t, stateDir).AnalyzerPlugins)
	}
	// Engine rebuild / config edit adds a plugin while the resident stays open.
	res.eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	second, err := res.reconcile(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if second.AnalyzerPlugins.ProcessesStarted == 0 || second.AnalyzerPlugins.UnitsExecuted == 0 {
		t.Fatalf("newly registered plugin was not prepared: %+v", second.AnalyzerPlugins)
	}
	state := pluginState(t, stateDir)
	if len(state.AnalyzerPlugins) == 0 {
		t.Fatal("newly registered plugin left no committed cache")
	}
	found := false
	for _, f := range second.Facts {
		if f.Props["plugin"] == "fixture" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("newly registered plugin contributed no facts: %+v", second.AnalyzerPlugins)
	}
}

func TestColdOwnerDomainSnapshotUsesReloadedManifest(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/held.ts":     `export const held = 1;`,
		"outside/free.ts": `export const free = 1;`,
		"package.json":    `{"name":"domain-reload","private":true}`,
		"tsconfig.json":   `{"compilerOptions":{"strict":true},"include":["src/**/*","outside/**/*"]}`,
	})
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "domain")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest := func(domain string) {
		t.Helper()
		manifest := fmt.Sprintf(`api: enola.plugin/v1
name: domain
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files: [plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [domain]
owner_domain:
  - %s
`, version, domain)
		if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest("outside/*")
	gate := filepath.Join(t.TempDir(), "gate")
	gateJSON, _ := json.Marshal(gate)
	script := fmt.Sprintf(`import { createInterface } from "node:readline";
import { existsSync } from "node:fs";
const gate = %s;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    while (!existsSync(gate)) { await sleep(20); }
    send({ id: message.id, op: "plan_result", units: [{ id: "file:src/held.ts", kind: "file", params: { file: "src/held.ts" } }] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const source = await callback("read", { unit: unit.id, path: unit.params.file });
      const node = { kind: "fsm_state", name: "domain/state:idle", owner: unit.params.file, line: 1, props: { body: source.text ?? "" } };
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [unit.params.file]: { nodes: [node] } }, summary: {} });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) { pending.delete(message.cb); resolve(message); }
    return;
  }
  void handle(message);
});
`, string(gateJSON))
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{{Path: "tools/analyzers/domain"}}
	opts := Options{StateDir: t.TempDir(), AllowRepoPlugins: []string{"domain"}}
	res, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	// After open, expand owner_domain so only a fresh reload snapshot holds src/*.
	writeManifest("src/*")
	sink := &graphstream.MemorySink{}
	res.sink = sink
	done := make(chan error, 1)
	go func() {
		_, err := res.reconcile(context.Background(), true)
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	sawFreeBeforeRelease := false
	var heldBeforeRelease graphstream.Node
	heldStreamedBeforeRelease := false
	for time.Now().Before(deadline) {
		for _, rec := range sink.CloneRecords() {
			var probe struct {
				Type  string             `json:"type"`
				Phase string             `json:"phase"`
				Nodes []graphstream.Node `json:"nodes"`
			}
			if json.Unmarshal(rec.Payload, &probe) != nil {
				continue
			}
			if probe.Type != graphstream.TypeBatch || probe.Phase != graphstream.PhaseLocal {
				continue
			}
			for _, n := range probe.Nodes {
				if strings.Contains(n.Owner.ID, "src/held.ts") || strings.Contains(n.File, "src/held.ts") {
					heldStreamedBeforeRelease = true
					heldBeforeRelease = n
				}
				if strings.Contains(n.Owner.ID, "outside/free.ts") || strings.Contains(n.File, "outside/free.ts") {
					sawFreeBeforeRelease = true
				}
			}
		}
		if sawFreeBeforeRelease {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.WriteFile(gate, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if heldStreamedBeforeRelease {
		t.Fatalf("newly covered owner streamed before plugin settle: %#v", heldBeforeRelease)
	}
	if !sawFreeBeforeRelease {
		t.Fatal("outside/free.ts did not stream as a local batch while the plugin plan gate was blocked; the hold assertion would be vacuous")
	}
}

func TestObservationsValidRejectsAdmittedPathAfterPolicyExclusion(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":       `export const a = 1;`,
		"artifacts/x.ts": `export const x = 1;`,
	})
	openEng := admissionEngine(t, root, graphinput.Options{})
	policy := openEng.GraphScope().Policy
	obs := analyzerplugin.Observation{
		Policy: policy.Identity(),
		Reads:  map[string]string{"artifacts/x.ts": "deadbeef"},
		Probes: map[string]string{"artifacts/x.ts": "exists"},
	}
	if !observationsValid(obs, root, openEng, []string{"src/a.ts", "artifacts/x.ts"}, map[string]string{"artifacts/x.ts": "deadbeef"}, nil, nil) {
		t.Fatal("admitted observation rejected under open policy")
	}
	closedEng := admissionEngine(t, root, graphinput.Options{Exclude: []string{"artifacts/**"}})
	if observationsValid(obs, root, closedEng, []string{"src/a.ts"}, map[string]string{}, nil, nil) {
		t.Fatal("admitted read/probe remained valid after policy excluded the path")
	}
}

func TestPluginCallbackRejectsWrongUnitAttribution(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	s := &session{abs: root, eng: admissionEngine(t, root, graphinput.Options{})}
	obs := map[string]analyzerplugin.Observation{}
	_, err := s.pluginCallback(context.Background(), analyzerplugin.Loaded{}, map[string]any{
		"op": "probe", "unit": "file:other.ts", "path": "src/a.ts",
	}, "file:src/a.ts", obs, &runtimeInputs{policyIdentity: "p"}, nil, nil, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match active unit") {
		t.Fatalf("wrong-unit callback error = %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("wrong-unit callback wrote observations: %#v", obs)
	}
}

func TestPluginVerifySuppressesBeginOnChangedGraphInputs(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      `export const a = 1;`,
		"outside/b.ts":  `export const b = 1;`,
		"package.json":  `{"name":"verify-begin","private":true}`,
		"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["src/**/*","outside/**/*"]}`,
	})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "verify-begin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir).Generation
	// Change a TypeScript file outside the plugin owner_domain so verify still
	// re-runs plugins while the graph input change would otherwise Begin.
	if err := os.WriteFile(filepath.Join(root, "outside/b.ts"), []byte(`export const b = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	verifySink := &graphstream.MemorySink{}
	verifyOpts := opts
	verifyOpts.PluginVerify = true
	if _, err := Run(context.Background(), eng, root, verifySink, verifyOpts); err != nil {
		t.Fatal(err)
	}
	for _, rec := range verifySink.CloneRecords() {
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(rec.Payload, &probe) != nil {
			continue
		}
		if probe.Type == graphstream.TypeBeginReplace || probe.Type == graphstream.TypeEndReplace || probe.Type == graphstream.TypeBatch {
			t.Fatalf("plugin-verify published %s on a changed graph input", probe.Type)
		}
	}
	if pluginState(t, stateDir).Generation != before {
		t.Fatal("plugin-verify advanced generation after a non-plugin input change")
	}
}

func TestColdPrepareSharesFrozenLoadedIdentityWithoutSecondReload(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	s := &session{eng: eng, abs: root, opts: Options{AllowRepoPlugins: []string{"fixture"}, analyzerPluginConfigs: []analyzerplugin.Config{registration}}}
	if err := s.reloadAnalyzerPlugins(); err != nil {
		t.Fatal(err)
	}
	loaded := s.analyzerPluginsLocked()
	if len(loaded) != 1 || len(loaded[0].EntryBytes) == 0 {
		t.Fatalf("expected loaded entry bytes, got %#v", loaded)
	}
	frozenIdentity := loaded[0].Identity
	frozenDigest := loaded[0].EntryDigest
	frozenBytes := append([]byte(nil), loaded[0].EntryBytes...)
	s.freezePluginOwnerHoldSnapshot(loaded)
	entry := filepath.Join(root, registration.Path, "plugin.mjs")
	// Replace the on-disk bundle after the freeze/reload shared by streaming.
	if err := os.WriteFile(entry, []byte("throw new Error('post-freeze replacement')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	still := s.analyzerPluginsLocked()
	if still[0].Identity != frozenIdentity || still[0].EntryDigest != frozenDigest {
		t.Fatal("in-memory loaded identity changed without reload")
	}
	client, err := analyzerplugin.Start(context.Background(), still[0], nil)
	if err != nil {
		t.Fatalf("Start should execute frozen EntryBytes after disk replacement: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	// A second reload (the race window) observes a post-freeze identity change.
	if err := os.WriteFile(entry, append(frozenBytes, []byte("\n// identity bump after freeze\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.reloadAnalyzerPlugins(); err != nil {
		t.Fatal(err)
	}
	if s.analyzerPluginsLocked()[0].Identity == frozenIdentity {
		t.Fatal("second reload did not observe a post-freeze identity change; the race would be invisible")
	}
	if !s.pluginOwnerHeld("src/a.ts") {
		t.Fatal("frozen owner_domain hold was lost across the second reload")
	}
}

func TestNonAuthoritativePluginOwnerChangeEntersReplacementScope(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      `export const a = 1;`,
		"src/b.ts":      `export const b = 1;`,
		"package.json":  `{"name":"plugin-scope","private":true}`,
		"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["src/**/*"]}`,
	})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "plugin-scope-state")
	opts := Options{StateDir: stateDir, AllowRepoPlugins: []string{"fixture"}} // non-authoritative
	cons := NewConsumer()
	sink := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	beforeFacts := 0
	for _, f := range first.Facts {
		if f.Props["plugin"] == "fixture" && f.File == "src/b.ts" {
			beforeFacts++
		}
	}
	// Change only the plugin-visible body of src/b.ts while leaving a.ts untouched.
	if err := os.WriteFile(filepath.Join(root, "src/b.ts"), []byte("export const b = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(deltaSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range delta.Facts {
		if f.Props["plugin"] == "fixture" && f.File == "src/b.ts" {
			found = true
			if body, _ := f.Props["body"].(string); !strings.Contains(body, "b = 2") {
				t.Fatalf("delta kept stale plugin body for src/b.ts: %#v", f.Props)
			}
		}
	}
	if !found {
		t.Fatal("changed plugin owner was missing from delta result facts")
	}
	state := pluginState(t, stateDir)
	unit := state.AnalyzerPlugins["fixture"].Units["file:src/b.ts"]
	if unit.OutputDigest == "" {
		t.Fatal("plugin cache missing updated unit digest")
	}
	coldEng := admissionEngine(t, root, graphinput.Options{})
	coldEng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEng, root, coldSink, Options{StateDir: t.TempDir(), AllowRepoPlugins: []string{"fixture"}}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	if err := cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, cold)
	_ = beforeFacts
}

func TestPluginVerifyWithoutPluginsPublishesNothingOnChangedInput(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	eng := admissionEngine(t, root, graphinput.Options{})
	stateDir := filepath.Join(root, ".enola", "verify-no-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir).Generation
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte(`export const a = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	verifyOpts := opts
	verifyOpts.PluginVerify = true
	if _, err := Run(context.Background(), eng, root, sink, verifyOpts); err != nil {
		t.Fatal(err)
	}
	if len(sink.CloneRecords()) != 0 {
		t.Fatalf("plugin-verify without plugins published %d records", len(sink.CloneRecords()))
	}
	if pluginState(t, stateDir).Generation != before {
		t.Fatal("plugin-verify without plugins advanced generation")
	}
}

func TestPluginVerifyFailsClosedWithoutCachedDigest(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "verify-new-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}, PluginVerify: true}
	sink := &graphstream.MemorySink{}
	_, err := Run(context.Background(), eng, root, sink, opts)
	if err == nil || !strings.Contains(err.Error(), "no cached digest") {
		t.Fatalf("expected no-cached-digest verification failure, got %v", err)
	}
	if len(sink.CloneRecords()) != 0 {
		t.Fatal("failed plugin-verify published graph records")
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.json")); err == nil {
		st := pluginState(t, stateDir)
		if len(st.AnalyzerPlugins) != 0 || st.Generation != 0 {
			t.Fatalf("failed plugin-verify committed plugin state: %#v", st.AnalyzerPlugins)
		}
	}
}

func TestExcludedPluginOwnerReincludedRestoresContribution(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":       `export const a = 1;`,
		"artifacts/x.ts": `export const x = 1;`,
	})
	// Custom plugin that always emits for both owners when planned.
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "dual")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: dual
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files: [plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [dual]
owner_domain:
  - src/*
  - artifacts/*
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    send({ id: message.id, op: "plan_result", units: [{ id: "model:all", kind: "model" }] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const owners = {};
      for (const file of ["src/a.ts", "artifacts/x.ts"]) {
        const read = await callback("read", { unit: unit.id, path: file });
        if (!read.not_admitted && !read.missing) {
          owners[file] = { nodes: [{ kind: "fsm_state", name: "dual/state:idle", owner: file, line: 1 }] };
        }
      }
      send({ id: message.id, op: "unit_result", unit: unit.id, owners, summary: {} });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) { pending.delete(message.cb); resolve(message); }
    return;
  }
  void handle(message);
});
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := analyzerplugin.Config{Path: "tools/analyzers/dual"}
	openEng := admissionEngine(t, root, graphinput.Options{})
	openEng.Config().AnalyzerPlugins = []analyzerplugin.Config{reg}
	stateDir := filepath.Join(root, ".enola", "exclude-reinclude-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"dual"}}
	cons := NewConsumer()
	openSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), openEng, root, openSink, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(openSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	st := pluginState(t, stateDir)
	if _, ok := st.AnalyzerPlugins["dual"].Units["model:all"].Owners["artifacts/x.ts"]; !ok {
		t.Fatal("expected artifacts owner retained in plugin cache after open admission")
	}
	closedEng := admissionEngine(t, root, graphinput.Options{Exclude: []string{"artifacts/**"}})
	closedEng.Config().AnalyzerPlugins = []analyzerplugin.Config{reg}
	closedSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), closedEng, root, closedSink, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(closedSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	st = pluginState(t, stateDir)
	if _, ok := st.AnalyzerPlugins["dual"].Units["model:all"].Owners["artifacts/x.ts"]; !ok {
		t.Fatalf("excluded owner was stripped from persisted unit cache: %#v", st.AnalyzerPlugins["dual"].Units["model:all"].Owners)
	}
	reopenEng := admissionEngine(t, root, graphinput.Options{})
	reopenEng.Config().AnalyzerPlugins = []analyzerplugin.Config{reg}
	reopenSink := &graphstream.MemorySink{}
	reopen, err := Run(context.Background(), reopenEng, root, reopenSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(reopenSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range reopen.Facts {
		if f.Props["plugin"] == "dual" && f.File == "artifacts/x.ts" {
			found = true
		}
	}
	if !found {
		t.Fatal("re-included plugin owner did not restore contributions")
	}
	coldEng := admissionEngine(t, root, graphinput.Options{})
	coldEng.Config().AnalyzerPlugins = []analyzerplugin.Config{reg}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEng, root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"dual"}}); err != nil {
		t.Fatal(err)
	}
	coldCons := NewConsumer()
	if err := coldCons.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, coldCons)
}

func TestPluginCallbackReadFailsWhenInventorySnapshotDiverges(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	s := &session{abs: root, eng: admissionEngine(t, root, graphinput.Options{}), pluginCapturedSources: map[string][]byte{}}
	obs := map[string]analyzerplugin.Observation{}
	// Inventory captured "a = 1" but disk was edited afterward.
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte(`export const a = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256Hex([]byte(`export const a = 1;`))
	_, err := s.pluginCallback(context.Background(), analyzerplugin.Loaded{}, map[string]any{
		"op": "read", "path": "src/a.ts",
	}, "file:src/a.ts", obs, &runtimeInputs{policyIdentity: "p"}, []string{"src/a.ts"}, nil, map[string]string{"src/a.ts": wantHash}, nil, nil)
	if err == nil || !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("expected ErrInputsChanged on inventory divergence, got %v", err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestPluginVerifyDoesNotPersistNeutralBookkeeping(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	eng := admissionEngine(t, root, graphinput.Options{})
	stateDir := filepath.Join(root, ".enola", "verify-neutral-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir)
	// Touch package.json so config/scan bookkeeping would otherwise refresh.
	pkg := filepath.Join(root, "package.json")
	body, err := os.ReadFile(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkg, append(append([]byte(nil), body...), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	verifyOpts := opts
	verifyOpts.PluginVerify = true
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, verifyOpts); err != nil {
		t.Fatal(err)
	}
	after := pluginState(t, stateDir)
	if after.ConfigHash != before.ConfigHash || after.ScanHash != before.ScanHash || after.Generation != before.Generation {
		t.Fatalf("plugin-verify persisted state: before=%+v after=%+v", before.ConfigHash, after.ConfigHash)
	}
	if len(sink.CloneRecords()) != 0 {
		t.Fatal("plugin-verify published records while refreshing neutral inputs")
	}
}

func TestPlanMembershipUnchangedDoesNotForcePluginChanged(t *testing.T) {
	prev := []analyzerplugin.UnitDecl{{ID: "file:a", Kind: "file"}, {ID: "file:b", Kind: "file"}}
	next := []analyzerplugin.UnitDecl{{ID: "file:b", Kind: "file"}, {ID: "file:a", Kind: "file"}}
	if planMembershipChanged(prev, next) {
		t.Fatal("same membership reported changed")
	}
	if !planMembershipChanged(prev, []analyzerplugin.UnitDecl{{ID: "file:a", Kind: "file"}}) {
		t.Fatal("removed unit was not detected")
	}
}

func TestReusablePlanRefreshDoesNotWholeDomainReplace(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      `export const a = 1;`,
		"package.json":  `{"name":"plan-refresh","private":true}`,
		"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["src/**/*","outside/**/*"]}`,
	})
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "fixedplan")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: fixedplan
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files: [plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [fixedplan]
owner_domain:
  - src/*
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fixed plan + single-file read: inventory growth changes PlanFilesDigest
	// without invalidating unit observations or membership.
	script := `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    send({ id: message.id, op: "plan_result", units: [{ id: "file:src/a.ts", kind: "file", params: { file: "src/a.ts" } }] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const file = unit.params.file;
      const source = await callback("read", { unit: unit.id, path: file });
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [file]: { nodes: [{ kind: "fsm_state", name: "fixedplan/state:idle", owner: file, line: 1, props: { body: source.text ?? "" } }] } }, summary: {} });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) { pending.delete(message.cb); resolve(message); }
    return;
  }
  void handle(message);
});
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := analyzerplugin.Config{Path: "tools/analyzers/fixedplan"}
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{reg}
	stateDir := filepath.Join(root, ".enola", "plan-refresh-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixedplan"}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir)
	beforeDigest := before.AnalyzerPlugins["fixedplan"].Units["file:src/a.ts"].OutputDigest
	if err := os.MkdirAll(filepath.Join(root, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside/c.ts"), []byte(`export const c = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}
	delta, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.AnalyzerPlugins.PlansRun == 0 {
		t.Fatal("expected plan refresh after inventory digest change")
	}
	if delta.AnalyzerPlugins.UnitsExecuted != 0 {
		t.Fatalf("reusable plugin units were re-executed: %+v", delta.AnalyzerPlugins)
	}
	if delta.AnalyzerPlugins.UnitsReused < 1 {
		t.Fatalf("expected reused plugin units after plan refresh: %+v", delta.AnalyzerPlugins)
	}
	after := pluginState(t, stateDir)
	if after.AnalyzerPlugins["fixedplan"].Units["file:src/a.ts"].OutputDigest != beforeDigest {
		t.Fatal("reusable unit output digest changed after membership-preserving plan refresh")
	}
	for _, fb := range delta.Fallbacks {
		if strings.Contains(fb.Reason, "analyzer plugin") {
			t.Fatalf("reusable plan refresh still forced plugin whole-domain fallback: %+v", fb)
		}
	}
}

func TestPluginUnitChangeSeedsPlAndResWithoutWholeDomain(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts": `export const a = 1;`,
		"src/b.ts": `export const b = 1;`,
		"src/c.ts": `export const c = 1;`,
	})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "plugin-pl-res-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	cons := NewConsumer()
	firstSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, firstSink, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(firstSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	beforeFiles := len(pluginState(t, stateDir).Files)
	// Change only src/a.ts plugin-visible body; b and c stay byte-identical.
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte("export const a = 99;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(deltaSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	for _, fb := range delta.Fallbacks {
		if strings.Contains(fb.Reason, "complete admitted owner domain") || strings.Contains(fb.Reason, "whole domain") && strings.Contains(fb.Reason, "analyzer plugin") {
			t.Fatalf("plugin unit change forced whole-domain fallback: %+v", fb)
		}
	}
	if delta.ReplacementOwners >= beforeFiles && beforeFiles >= 3 {
		t.Fatalf("plugin unit change replaced whole domain: owners=%d files=%d stats=%+v", delta.ReplacementOwners, beforeFiles, delta.AnalyzerPlugins)
	}
	found := false
	for _, f := range delta.Facts {
		if f.Props["plugin"] == "fixture" && f.File == "src/a.ts" {
			found = true
			if body, _ := f.Props["body"].(string); !strings.Contains(body, "99") {
				t.Fatalf("changed plugin owner kept stale body: %#v", f.Props)
			}
		}
	}
	if !found {
		t.Fatal("changed plugin owner missing from delta facts")
	}
	coldEng := admissionEngine(t, root, graphinput.Options{})
	coldEng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEng, root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	if err := cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, cold)
}

func TestPluginResolutionIndexIncompleteForcesWholeDomain(t *testing.T) {
	previous := map[string]analyzerplugin.PluginRecord{
		"fixture": {
			Units: map[string]analyzerplugin.UnitRecord{
				"file:src/a.ts": {
					Owners: map[string]analyzerplugin.OwnerResult{
						"src/a.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "fixture/state:idle"}}},
					},
				},
			},
		},
	}
	missing := map[string]*FileState{
		"src/a.ts": {Contrib: map[string][]facts.Fact{"typescript": {{Kind: facts.KindSymbol, Name: "a"}}}},
		"src/b.ts": {Contrib: map[string][]facts.Fact{"typescript": {{Kind: facts.KindSymbol, Name: "b"}}}},
	}
	if !pluginResolutionIndexIncomplete(missing, previous) {
		t.Fatal("missing plugin contrib indexes must prove incompleteness")
	}
	complete := map[string]*FileState{
		"src/a.ts": {Contrib: map[string][]facts.Fact{
			"typescript":     {{Kind: facts.KindSymbol, Name: "a"}},
			"plugin:fixture": {{Kind: facts.KindFSMState, Name: "fixture/state:idle"}},
		}},
	}
	if pluginResolutionIndexIncomplete(complete, previous) {
		t.Fatal("present plugin contrib indexes must prove completeness")
	}
	if pluginResolutionIndexIncomplete(map[string]*FileState{}, nil) {
		t.Fatal("empty previous plugin state is complete")
	}

	multi := map[string]analyzerplugin.PluginRecord{
		"fixture": {
			Units: map[string]analyzerplugin.UnitRecord{
				"file:src/a.ts": {
					Owners: map[string]analyzerplugin.OwnerResult{
						"src/a.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "fixture/state:a"}}},
						"src/b.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "fixture/state:b"}}},
					},
				},
			},
		},
	}
	partial := map[string]*FileState{
		"src/a.ts": {Contrib: map[string][]facts.Fact{
			"plugin:fixture": {{Kind: facts.KindFSMState, Name: "fixture/state:a"}},
		}},
		"src/b.ts": {Contrib: map[string][]facts.Fact{
			"typescript": {{Kind: facts.KindSymbol, Name: "b"}},
		}},
	}
	if !pluginResolutionIndexIncomplete(partial, multi) {
		t.Fatal("partial prior-owner plugin indexes must prove incompleteness")
	}
	full := map[string]*FileState{
		"src/a.ts": {Contrib: map[string][]facts.Fact{
			"plugin:fixture": {{Kind: facts.KindFSMState, Name: "fixture/state:a"}},
		}},
		"src/b.ts": {Contrib: map[string][]facts.Fact{
			"plugin:fixture": {{Kind: facts.KindFSMState, Name: "fixture/state:b"}},
		}},
	}
	if pluginResolutionIndexIncomplete(full, multi) {
		t.Fatal("complete prior-owner plugin indexes must prove completeness")
	}

	multiPlugin := map[string]analyzerplugin.PluginRecord{
		"alpha": {
			Units: map[string]analyzerplugin.UnitRecord{
				"file:src/a.ts": {
					Owners: map[string]analyzerplugin.OwnerResult{
						"src/a.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "alpha/state:idle"}}},
					},
				},
			},
		},
		"beta": {
			Units: map[string]analyzerplugin.UnitRecord{
				"file:src/a.ts": {
					Owners: map[string]analyzerplugin.OwnerResult{
						"src/a.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "beta/state:idle"}}},
					},
				},
			},
		},
	}
	wrongPluginOnly := map[string]*FileState{
		"src/a.ts": {Contrib: map[string][]facts.Fact{
			"plugin:alpha": {{Kind: facts.KindFSMState, Name: "alpha/state:idle"}},
		}},
	}
	if !pluginResolutionIndexIncomplete(wrongPluginOnly, multiPlugin) {
		t.Fatal("missing expected plugin:beta index must prove incompleteness even when another plugin index exists")
	}
	bothPlugins := map[string]*FileState{
		"src/a.ts": {Contrib: map[string][]facts.Fact{
			"plugin:alpha": {{Kind: facts.KindFSMState, Name: "alpha/state:idle"}},
			"plugin:beta":  {{Kind: facts.KindFSMState, Name: "beta/state:idle"}},
		}},
	}
	if pluginResolutionIndexIncomplete(bothPlugins, multiPlugin) {
		t.Fatal("exact per-plugin contribution indexes must prove completeness")
	}
}

func TestPluginFSMFactsCarryRepoSoTSAnchorsResolve(t *testing.T) {
	repoID := "/repo/product"
	pluginState := facts.Fact{
		Kind: facts.KindFSMState, Name: "fixture-machine/state:idle", File: "src/a.ts", Line: 1, Repo: "",
		Props: map[string]any{"plugin": "fixture"},
	}
	tsSymbol := facts.Fact{
		Kind: facts.KindSymbol, Name: "dispatch", File: "src/a.ts", Line: 2, Repo: repoID,
		Relations: []facts.Relation{{Kind: "fsm_dispatches", Target: "fixture-machine/state:idle"}},
	}
	assembled := mergeAnalyzerPluginAnchors([]facts.Fact{tsSymbol, pluginState}, nil)
	tagRepo(assembled, repoID)
	var gotFSM *facts.Fact
	for i := range assembled {
		if assembled[i].Kind == facts.KindFSMState && assembled[i].Name == "fixture-machine/state:idle" {
			gotFSM = &assembled[i]
			break
		}
	}
	if gotFSM == nil {
		t.Fatal("plugin FSM fact missing after merge")
	}
	if gotFSM.Repo != repoID {
		t.Fatalf("plugin FSM Repo = %q, want %q", gotFSM.Repo, repoID)
	}
	idx := buildIndex(assembled)
	tid, status := idx.resolveRelConstrained(repoID, facts.KindSymbol, "fsm_dispatches", "fixture-machine/state:idle", false, "")
	if status != graphstream.ResResolved {
		t.Fatalf("TS-anchored edge resolution = %s/%s", status, tid)
	}
	if tid != gotFSM.Identity() {
		t.Fatalf("resolved target %q, want plugin FSM identity %q", tid, gotFSM.Identity())
	}

	fallback := mergeAnalyzerPluginAnchors([]facts.Fact{tsSymbol}, []pluginAnchor{{
		Plugin: "fixture", Identity: "id",
		Value: analyzerplugin.Anchor{Symbol: "missing", Owner: "src/a.ts", Line: 3, Relations: []analyzerplugin.Relation{{Kind: "fsm_dispatches", Target: "fixture-machine/event:go"}}},
	}})
	tagRepo(fallback, repoID)
	foundFallback := false
	for _, f := range fallback {
		if f.Kind == facts.KindExtraction && strings.Contains(f.Name, "source-binding") {
			foundFallback = true
			if f.Repo != repoID {
				t.Fatalf("plugin fallback Repo = %q, want %q", f.Repo, repoID)
			}
		}
		if f.Kind == facts.KindSymbol && f.Name == "dispatch" && f.Repo != repoID {
			t.Fatalf("existing extracted fact Repo changed to %q", f.Repo)
		}
	}
	if !foundFallback {
		t.Fatal("expected plugin-created fallback extraction fact")
	}
}

func TestReplacePluginContributionsKeepsAdmittedUnhashedNonTSOwners(t *testing.T) {
	files := map[string]*FileState{
		"src/keep.ts": {Hash: "keep", Contrib: map[string][]facts.Fact{}, ContribHash: map[string]string{}},
	}
	contributions := map[string][]facts.Fact{
		"plugin:fixture": {
			{Kind: facts.KindFSMState, Name: "fixture/state:idle", File: "src/keep.ts"},
			{Kind: facts.KindFSMState, Name: "fixture/state:media", File: "assets/logo.png"},
			{Kind: facts.KindFSMState, Name: "fixture/state:gone", File: "src/gone.ts"},
		},
	}
	// assets/logo.png is admitted NameOnly media: present in inventory, absent
	// from the ordinary content-hash map.
	hashes := map[string]string{"src/keep.ts": "keep"}
	admitted := admittedOwnerSet([]string{"src/keep.ts", "assets/logo.png"}, hashes)
	replacePluginContributions(files, contributions, hashes, admitted)
	if got := files["src/keep.ts"].Contrib["plugin:fixture"]; len(got) != 1 {
		t.Fatalf("hashed owner contributions = %#v", got)
	}
	media := files["assets/logo.png"]
	if media == nil {
		t.Fatal("admitted unhashed non-TS owner was dropped")
	}
	if got := media.Contrib["plugin:fixture"]; len(got) != 1 || got[0].Name != "fixture/state:media" {
		t.Fatalf("unhashed admitted owner contributions = %#v", got)
	}
	if _, ok := files["src/gone.ts"]; ok {
		t.Fatal("non-admitted owner was resurrected")
	}
}

func TestAnalyzerPluginReplacementOwnersPreservesSeedsWhenIncomplete(t *testing.T) {
	s := &session{
		pluginDeltaOwners:     []string{"assets/logo.png", "src/a.ts"},
		pluginResOwners:       []string{"src/b.ts"},
		pluginScopeIncomplete: true,
		state: &State{Files: map[string]*FileState{
			"src/a.ts": {},
			"src/old.ts": {},
		}},
	}
	got := s.analyzerPluginReplacementOwners([]string{"src/a.ts", "assets/logo.png", "package-lock.json"})
	ids := map[string]bool{}
	for _, o := range got {
		ids[o.ID] = true
	}
	for _, want := range []string{"assets/logo.png", "src/a.ts", "src/b.ts", "src/old.ts"} {
		if !ids[want] {
			t.Fatalf("incomplete fallback dropped plugin/prior owner %s from %v", want, got)
		}
	}
	if ids["package-lock.json"] {
		t.Fatal("lockfile entered replacement owners")
	}
}

func TestAuthoritativeWholeDomainPlanKeepsPluginExtraOwners(t *testing.T) {
	prev := map[string]*FileState{"src/a.ts": {Hash: "a"}}
	// current mirrors session planning after graphSemanticNames(eng, inv.Files):
	// NameOnly media is already absent. Plugin seeds arrive only via extraOwners.
	plan, reason, err := authoritativeFilePlan(
		[]string{"src/a.ts"},
		[]string{"src/a.ts"},
		prev,
		map[string]string{"src/a.ts": "a"},
		true,
		[]string{"assets/logo.png", "package-lock.json"},
		membershipDelta{},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeWholeDomain {
		t.Fatalf("reason=%s", reason)
	}
	owners := map[string]bool{}
	for _, o := range plan.manifest() {
		owners[o.ID] = true
	}
	if !owners["src/a.ts"] || !owners["assets/logo.png"] {
		t.Fatalf("whole-domain plan omitted semantic or plugin NameOnly owner: %v", plan.manifest())
	}
	if owners["package-lock.json"] {
		t.Fatal("whole-domain plan admitted a lockfile extra owner")
	}
}

func TestIncompleteIndexWholeDomainBeginIncludesNameOnlyPluginOwner(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts": `export const a = 1;`,
	})
	registration := nameOnlyAwareAnalyzerPlugin(t, root)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "nameonly-incomplete-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"nameonly"}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	st := pluginState(t, stateDir)
	file := st.Files["src/a.ts"]
	if file == nil || file.Contrib["plugin:nameonly"] == nil {
		t.Fatalf("cold run missing plugin contrib index: %#v", file)
	}
	// Force incomplete prior-owner coverage while keeping plugin unit owners.
	delete(file.Contrib, "plugin:nameonly")
	delete(file.ContribHash, "plugin:nameonly")
	st.Files["src/a.ts"] = file
	if err := saveState(stateDir, st); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Minimal PNG header bytes; content is NameOnly so it stays unhashed.
	if err := os.WriteFile(filepath.Join(root, "assets/logo.png"), []byte("\x89PNG\r\n\x1a\nnameonly"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte(`export const a = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	incompleteFallback := false
	for _, fb := range delta.Fallbacks {
		if strings.Contains(fb.Reason, "plugin resolution name indexes are incomplete") ||
			(strings.Contains(fb.Reason, "analyzer plugin") && strings.Contains(fb.Reason, "incomplete")) {
			incompleteFallback = true
		}
	}
	if !incompleteFallback {
		// pluginScopeIncomplete may also trip the early wholeDomain term without
		// a dedicated fallback string when set before planning; still require the
		// NameOnly owner in Begin.
		t.Logf("delta fallbacks=%+v stats=%+v", delta.Fallbacks, delta.AnalyzerPlugins)
	}
	begins, _, ends, err := DecodeRun(deltaSink.CloneRecords())
	if err != nil || len(begins) != 1 || len(ends) != 1 {
		t.Fatalf("replacement envelopes: begins=%d ends=%d err=%v", len(begins), len(ends), err)
	}
	owners := map[string]bool{}
	for _, owner := range begins[0].OwnerScope {
		owners[owner.ID] = true
	}
	if !owners["assets/logo.png"] {
		t.Fatalf("Begin omitted newly admitted NameOnly plugin owner under incomplete-index fallback: %v", begins[0].OwnerScope)
	}
	if begins[0].OwnerScopeCount != len(begins[0].OwnerScope) || ends[0].OwnerScopeLen != begins[0].OwnerScopeCount || ends[0].OwnerScopeDigest != graphstream.DigestOwners(begins[0].OwnerScope) {
		t.Fatalf("owner scope changed after Begin: begin=%+v end=%+v", begins[0], ends[0])
	}
	foundMedia := false
	for _, f := range delta.Facts {
		if f.File == "assets/logo.png" && f.Props["plugin"] == "nameonly" {
			foundMedia = true
			break
		}
	}
	if !foundMedia {
		t.Fatal("NameOnly plugin owner contribution missing from delta facts")
	}
}

func nameOnlyAwareAnalyzerPlugin(t *testing.T, root string) analyzerplugin.Config {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for analyzer plugin integration tests")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "nameonly")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: nameonly
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files:
  - plugin.mjs
vocabularies:
  - enola.fsm@1
claims:
  machines:
    - nameonly-machine
owner_domain:
  - src/**
  - assets/**
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    const listed = await callback("list", { unit: "@plan", glob: "**/*.{ts,png}" });
    send({ id: message.id, op: "plan_result", units: (listed.paths || []).map((file) => ({ id: "file:" + file, kind: "file", params: { file } })) });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const file = unit.params.file;
      const source = await callback("read", { unit: unit.id, path: file });
      const body = source.text ?? "";
      const node = { kind: "fsm_state", name: "nameonly-machine/state:" + file, owner: file, line: 1, props: { body, plugin_owner: file } };
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [file]: { nodes: [node] } }, summary: { body_length: body.length } });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) {
      pending.delete(message.cb);
      resolve(message);
    }
    return;
  }
  handle(message).catch((err) => {
    send({ id: message.id, op: "error", error: String(err) });
  });
});
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/analyzers/nameonly"}
}

func fixedPlanAnalyzerPlugin(t *testing.T, root, name, sourceFile, ownerFile, planFile string) analyzerplugin.Config {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for analyzer plugin integration tests")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", name)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: %s
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files: [plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [%s]
owner_domain:
  - src/*
`, name, version, name)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`import { createInterface } from "node:readline";
const sourceFile = %q;
const ownerFile = %q;
const planFile = %q;
const machine = %q;
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    if (planFile) await callback("read", { unit: "@plan", path: planFile });
    send({ id: message.id, op: "plan_result", units: [{ id: "file:" + sourceFile, kind: "file", params: { file: sourceFile } }] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const source = await callback("read", { unit: unit.id, path: sourceFile });
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: {
        [ownerFile]: { nodes: [{ kind: "fsm_state", name: machine + "/state:idle", owner: ownerFile, line: 1, props: { body: source.text ?? "" } }] }
      }, summary: {} });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) { pending.delete(message.cb); resolve(message); }
    return;
  }
  void handle(message);
});
`, sourceFile, ownerFile, planFile, name)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: filepath.ToSlash(filepath.Join("tools", "analyzers", name))}
}

func TestNonAuthoritativePluginScopeIsFrozenBeforeBegin(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":    `export const a = 1;`,
		"src/b.ts":    `export const b = 1;`,
		"src/plan.ts": `export const plan = 1;`,
	})
	const plugin = "scopefixture"
	registration := fixedPlanAnalyzerPlugin(t, root, plugin, "src/a.ts", "src/b.ts", "src/plan.ts")
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := t.TempDir()
	opts := Options{StateDir: stateDir, AllowRepoPlugins: []string{plugin}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte(`export const a = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, deltaSink, opts); err != nil {
		t.Fatal(err)
	}
	begins, _, ends, err := DecodeRun(deltaSink.CloneRecords())
	if err != nil || len(begins) != 1 || len(ends) != 1 {
		t.Fatalf("replacement envelopes: begins=%d ends=%d err=%v", len(begins), len(ends), err)
	}
	owners := map[string]bool{}
	for _, owner := range begins[0].OwnerScope {
		owners[owner.ID] = true
	}
	if !owners["src/a.ts"] || !owners["src/b.ts"] {
		t.Fatalf("Begin omitted dirty source or settled plugin owner: %v", begins[0].OwnerScope)
	}
	if begins[0].OwnerScopeCount != len(begins[0].OwnerScope) || ends[0].OwnerScopeLen != begins[0].OwnerScopeCount || ends[0].OwnerScopeDigest != graphstream.DigestOwners(begins[0].OwnerScope) {
		t.Fatalf("owner scope changed after Begin: begin=%+v end=%+v", begins[0], ends[0])
	}
}

func TestPlanObservationRefreshPersistsWithoutGraphPublication(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":    `export const a = 1;`,
		"src/plan.ts": `export const plan = 1;`,
	})
	const plugin = "planrefresh"
	registration := fixedPlanAnalyzerPlugin(t, root, plugin, "src/a.ts", "src/a.ts", "src/plan.ts")
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := t.TempDir()
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{plugin}}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	state := pluginState(t, stateDir)
	record := state.AnalyzerPlugins[plugin]
	if record.PlanObservations.Reads["src/plan.ts"] == "" {
		t.Fatalf("fixture plan did not record its read observation: %#v", record.PlanObservations)
	}
	record.PlanObservations.Reads["src/plan.ts"] = "stale-observation"
	state.AnalyzerPlugins[plugin] = record
	if err := saveState(stateDir, state); err != nil {
		t.Fatal(err)
	}
	baseGeneration := state.Generation

	refreshSink := &graphstream.MemorySink{}
	refresh, err := Run(context.Background(), eng, root, refreshSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if refresh.AnalyzerPlugins.ProcessesStarted != 1 || refresh.AnalyzerPlugins.PlansRun != 1 || refresh.AnalyzerPlugins.UnitsExecuted != 0 {
		t.Fatalf("expected plan-only refresh with reused units, got %+v", refresh.AnalyzerPlugins)
	}
	if got := len(refreshSink.CloneRecords()); got != 0 {
		t.Fatalf("plan-only refresh published %d graph records", got)
	}
	refreshedState := pluginState(t, stateDir)
	planBytes, err := os.ReadFile(filepath.Join(root, "src/plan.ts"))
	if err != nil {
		t.Fatal(err)
	}
	wantSum := sha256.Sum256(planBytes)
	wantHash := hex.EncodeToString(wantSum[:])
	if got := refreshedState.AnalyzerPlugins[plugin].PlanObservations.Reads["src/plan.ts"]; got != wantHash {
		t.Fatalf("plan read observation was not persisted: got=%q want=%q", got, wantHash)
	}
	if refreshedState.Generation != baseGeneration || refresh.TargetGeneration != baseGeneration {
		t.Fatalf("plan bookkeeping advanced graph generation: state=%d result=%d base=%d", refreshedState.Generation, refresh.TargetGeneration, baseGeneration)
	}

	steadySink := &graphstream.MemorySink{}
	steady, err := Run(context.Background(), eng, root, steadySink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if steady.AnalyzerPlugins.ProcessesStarted != 0 || steady.AnalyzerPlugins.PlansRun != 0 || len(steadySink.CloneRecords()) != 0 {
		t.Fatalf("stable retry repeated plugin work or published graph records: stats=%+v records=%d", steady.AnalyzerPlugins, len(steadySink.CloneRecords()))
	}
}

func TestResidentExcludedPluginIdentityEditReloadsContributions(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	// Exclude the plugin directory from graph-input admission so identity edits
	// classify as Ignore unless explicitContentPath/watch coverage keep them.
	eng := admissionEngine(t, root, graphinput.Options{Exclude: []string{"tools/**"}})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "excluded-identity-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	sink := &graphstream.MemorySink{}
	res, err := OpenSession(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if _, err := res.reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	before := pluginState(t, stateDir).AnalyzerPlugins["fixture"]
	if before.Identity == "" {
		t.Fatal("missing initial plugin identity")
	}
	entry := filepath.Join(root, registration.Path, "plugin.mjs")
	if !res.explicitContentPath(entry) {
		t.Fatal("excluded plugin identity file was not classified as explicit content")
	}
	source := NewGraphFileChangeSource(eng, root, nil, 64)
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := source.CoverSessionInputs(res); err != nil {
		t.Fatal(err)
	}
	if !source.explicitTarget(entry) {
		t.Fatal("excluded plugin identity file was not covered by resident watch targets")
	}

	body, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, append(append([]byte(nil), body...), []byte("\n// excluded identity bump\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	batch := ChangeBatch{
		Epoch: res.epoch, From: res.watermark, Through: res.watermark + 1, Covered: true,
		Paths: []string{entry},
	}
	online, err := res.ApplyChanges(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if online == nil || online.AnalyzerPlugins.ProcessesStarted == 0 {
		t.Fatalf("excluded identity edit did not trigger plugin reload: %+v", online)
	}
	if online.FallbackReason != "repository analyzer plugin identity changed" {
		t.Fatalf("identity edit reason = %q, want plugin identity change", online.FallbackReason)
	}
	after := pluginState(t, stateDir).AnalyzerPlugins["fixture"]
	if after.Identity == "" || after.Identity == before.Identity {
		t.Fatalf("excluded identity edit reused stale plugin identity: before=%s after=%s", before.Identity, after.Identity)
	}
	unit, ok := after.Units["file:src/a.ts"]
	if !ok || len(unit.Owners) == 0 {
		t.Fatalf("reloaded plugin contributions missing from committed state: %#v", after.Units)
	}
}

func TestResidentManifestIdentityFileSwapCoversNewExcludedPath(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for analyzer plugin integration tests")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	pluginDir := filepath.Join(root, "tools", "analyzers", "swap")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pluginDir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pluginScript := `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
let callbackId = 0;
const pending = new Map();
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
const callback = (op, fields) => {
  const cb = ++callbackId;
  send({ cb, op, ...fields });
  return new Promise((resolve) => pending.set(cb, resolve));
};
async function handle(message) {
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    const listed = await callback("list", { unit: "@plan", glob: "src/*.ts" });
    send({ id: message.id, op: "plan_result", units: (listed.paths || []).map((file) => ({ id: "file:" + file, kind: "file", params: { file } })) });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const file = unit.params.file;
      const source = await callback("read", { unit: unit.id, path: file });
      const node = { kind: "fsm_state", name: "swap-machine/state:idle", owner: file, line: 1, props: { body: source.text ?? "" } };
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [file]: { nodes: [node] } }, summary: { body_length: (source.text ?? "").length } });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
}
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.cb !== undefined) {
    const resolve = pending.get(message.cb);
    if (resolve) {
      pending.delete(message.cb);
      resolve(message);
    }
    return;
  }
  handle(message).catch((err) => {
    send({ id: message.id, op: "error", error: String(err) });
  });
});
`
	write("plugin.mjs", pluginScript)
	write("a.txt", "identity-a\n")
	write("b.txt", "identity-b\n")
	manifestFor := func(identityRel string) string {
		return fmt.Sprintf(`api: enola.plugin/v1
name: swap
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files:
  - plugin.mjs
  - %s
vocabularies:
  - enola.fsm@1
claims:
  machines:
    - swap-machine
owner_domain:
  - src/**
`, version, identityRel)
	}
	write("enola-plugin.yaml", manifestFor("a.txt"))

	eng := admissionEngine(t, root, graphinput.Options{Exclude: []string{"tools/**"}})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{{Path: "tools/analyzers/swap"}}
	stateDir := filepath.Join(root, ".enola", "identity-swap-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"swap"}}
	sink := &graphstream.MemorySink{}
	res, err := OpenSession(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()

	source := NewGraphFileChangeSource(eng, root, nil, 64)
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	bootstrap := source.Drain()
	bootstrap.Reconcile = "test bootstrap"
	if _, err := res.ApplyChanges(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	if err := source.CoverSessionInputs(res); err != nil {
		t.Fatal(err)
	}

	pathA := filepath.Join(pluginDir, "a.txt")
	pathB := filepath.Join(pluginDir, "b.txt")
	manifestPath := filepath.Join(pluginDir, "enola-plugin.yaml")
	if !source.explicitTarget(pathA) || !res.pluginIdentityPath(pathA) {
		t.Fatal("initial identity file A was not covered/classified")
	}
	if source.explicitTarget(pathB) || res.pluginIdentityPath(pathB) {
		t.Fatal("undeclared identity file B was covered before manifest swap")
	}
	before := pluginState(t, stateDir).AnalyzerPlugins["swap"]
	if before.Identity == "" {
		t.Fatal("missing initial plugin identity")
	}

	if err := os.WriteFile(manifestPath, []byte(manifestFor("b.txt")), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestBatch := ChangeBatch{
		Epoch: res.epoch, From: res.watermark, Through: res.watermark + 1, Covered: true,
		Paths: []string{manifestPath},
	}
	manifestOnline, err := res.ApplyChanges(context.Background(), manifestBatch)
	if err != nil {
		t.Fatal(err)
	}
	if manifestOnline.AnalyzerPlugins.ProcessesStarted == 0 {
		t.Fatalf("manifest identity swap did not reload plugin: %+v", manifestOnline)
	}
	if err := source.CoverSessionInputs(res); err != nil {
		t.Fatal(err)
	}
	if !source.explicitTarget(pathB) || !res.pluginIdentityPath(pathB) {
		t.Fatal("new identity file B was not covered/classified after manifest swap")
	}
	if source.explicitTarget(pathA) || res.pluginIdentityPath(pathA) {
		t.Fatal("removed identity file A still covered/classified after manifest swap")
	}
	mid := pluginState(t, stateDir).AnalyzerPlugins["swap"]
	if mid.Identity == "" || mid.Identity == before.Identity {
		t.Fatalf("manifest swap reused stale identity: before=%s after=%s", before.Identity, mid.Identity)
	}

	if err := os.WriteFile(pathB, []byte("identity-b-edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	editBatch := ChangeBatch{
		Epoch: res.epoch, From: res.watermark, Through: res.watermark + 1, Covered: true,
		Paths: []string{pathB},
	}
	editOnline, err := res.ApplyChanges(context.Background(), editBatch)
	if err != nil {
		t.Fatal(err)
	}
	if editOnline.FallbackReason != "repository analyzer plugin identity changed" {
		t.Fatalf("edit of B reason = %q", editOnline.FallbackReason)
	}
	if editOnline.AnalyzerPlugins.ProcessesStarted == 0 || editOnline.AnalyzerPlugins.UnitsExecuted == 0 {
		t.Fatalf("edit of newly declared identity B did not reload/re-run plugin: %+v", editOnline.AnalyzerPlugins)
	}
	after := pluginState(t, stateDir).AnalyzerPlugins["swap"]
	if after.Identity == "" || after.Identity == mid.Identity {
		t.Fatalf("edit of B reused stale identity: mid=%s after=%s", mid.Identity, after.Identity)
	}
	unit, ok := after.Units["file:src/a.ts"]
	if !ok || len(unit.Owners) == 0 || unit.OutputDigest == "" {
		t.Fatalf("plugin contributions missing after B edit: %#v", after.Units)
	}
	if unit.OutputDigest == mid.Units["file:src/a.ts"].OutputDigest && after.Identity == mid.Identity {
		t.Fatal("stale unit output remained after identity B edit")
	}
}
