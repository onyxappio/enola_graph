package graphsession

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/analyzerplugin"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

func genericAnalyzerPlugin(t *testing.T, root, counter string) analyzerplugin.Config {
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
	pluginDir := filepath.Join(root, "tools", "analyzers", "fixture")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: fixture
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
    - fixture-machine
owner_domain:
  - src/*
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	counterJSON, err := json.Marshal(counter)
	if err != nil {
		t.Fatal(err)
	}
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
    appendFileSync(counter, JSON.stringify({ entry: process.argv[1], cwd: process.cwd() }) + "\n");
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    const listed = await callback("list", { unit: "@plan", glob: "src/*.ts" });
    send({ id: message.id, op: "plan_result", units: listed.paths.map((file) => ({ id: "file:" + file, kind: "file", params: { file } })) });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      const file = unit.params.file;
      const source = await callback("read", { unit: unit.id, path: file });
      const module = await callback("resolve_module", { unit: unit.id, from: file, spec: "./dep" });
      const exported = module.file ? await callback("resolve_export", { unit: unit.id, file: module.file, name: "value" }) : {};
      const node = { kind: "fsm_state", name: "fixture-machine/state:idle", owner: file, line: 1, props: { body: source.text ?? "" } };
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [file]: { nodes: [node] } }, summary: { body_length: (source.text ?? "").length, module_file: module.file ?? "", export_target: exported.target ?? "" } });
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
  void handle(message);
});
`, string(counterJSON))
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/analyzers/fixture", Config: map[string]any{"counter": counter}}
}

func summaryDependencyPlugin(t *testing.T, root, counter string, declareDependency bool) analyzerplugin.Config {
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
	pluginDir := filepath.Join(root, "tools", "analyzers", "summary-fixture")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: summary-fixture
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
    - summary-fixture
owner_domain:
  - src/*
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	counterJSON, err := json.Marshal(counter)
	if err != nil {
		t.Fatal(err)
	}
	consumes := `[]`
	if declareDependency {
		consumes = ` ["model:config"] `
	}
	script := fmt.Sprintf(`import { createInterface } from "node:readline";
import { appendFileSync } from "node:fs";
const counter = %s;
const consumes = %s;
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
    send({ id: message.id, op: "plan_result", units: [
      { id: "model:config", kind: "model" },
      { id: "file:src/a.ts", kind: "file", params: { file: "src/a.ts" }, consumes }
    ] });
  } else if (message.op === "run") {
    for (const unit of message.units) {
      appendFileSync(counter, unit.id + "\n");
      if (unit.id === "model:config") {
        const source = await callback("read", { unit: unit.id, path: "src/model.json" });
        send({ id: message.id, op: "unit_result", unit: unit.id, owners: {}, summary: { value: (source.text ?? "").trim() } });
      } else {
        const summary = await callback("summary", { unit: unit.id, target: "model:config" });
        const source = await callback("read", { unit: unit.id, path: unit.params.file });
        const node = { kind: "fsm_state", name: "summary-fixture/state:constant", owner: unit.params.file, line: 1, props: { value: summary.value.value, source: source.text ?? "" } };
        send({ id: message.id, op: "unit_result", unit: unit.id, owners: { [unit.params.file]: { nodes: [node] } }, summary: {} });
      }
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
  void handle(message);
});
`, string(counterJSON), consumes)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/analyzers/summary-fixture"}
}

func pluginState(t *testing.T, stateDir string) *State {
	t.Helper()
	state, err := loadCommittedState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func pluginCounterLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestPluginInputPathsRejectSymlinkEscapesBeforeIO(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "src")
	outside := t.TempDir()
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.ts"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(inside, "outside")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"src/outside/secret.ts", "src/outside/new.ts"} {
		if _, err := confinedRepositoryInputPath(root, rel); err == nil {
			t.Fatalf("repository path %q was allowed to escape through a symlink", rel)
		}
	}
}

func TestRepositoryAnalyzerPluginSummaryDependenciesUseCanonicalDigests(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":       `export const a = 1;`,
		"src/model.json": "alpha\n",
	})
	counter := filepath.Join(t.TempDir(), "unit-runs.log")
	registration := summaryDependencyPlugin(t, root, counter, true)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "summary-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"summary-fixture"}}
	cons := NewConsumer()
	run := func(engine *engine.Engine) *graphstream.MemorySink {
		t.Helper()
		sink := &graphstream.MemorySink{}
		if _, err := Run(context.Background(), engine, root, sink, opts); err != nil {
			t.Fatal(err)
		}
		if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		return sink
	}
	run(eng)
	if got := pluginCounterLines(t, counter); len(got) != 2 || got[0] != "model:config" || got[1] != "file:src/a.ts" {
		t.Fatalf("cold unit execution order = %v", got)
	}

	// The producer input changes, but its normalized summary remains equal;
	// the consumer must retain its valid cached output.
	if err := os.WriteFile(filepath.Join(root, "src/model.json"), []byte("alpha  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(eng)
	got := pluginCounterLines(t, counter)
	if len(got) != 3 || got[2] != "model:config" {
		t.Fatalf("equal-summary delta reran unexpected units: %v", got)
	}

	if err := os.WriteFile(filepath.Join(root, "src/model.json"), []byte("beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(eng)
	got = pluginCounterLines(t, counter)
	if len(got) != 5 || got[3] != "model:config" || got[4] != "file:src/a.ts" {
		t.Fatalf("changed summary did not rerun declared consumer: %v", got)
	}

	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldOpts := opts
	coldOpts.StateDir = t.TempDir()
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, coldSink, coldOpts); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)

	undeclaredRoot := setupTSRepo(t, map[string]string{
		"src/a.ts":       `export const a = 1;`,
		"src/model.json": "alpha\n",
	})
	undeclaredRegistration := summaryDependencyPlugin(t, undeclaredRoot, filepath.Join(t.TempDir(), "bad-runs.log"), false)
	undeclaredEngine := admissionEngine(t, undeclaredRoot, graphinput.Options{})
	undeclaredEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{undeclaredRegistration}
	undeclaredSink := &graphstream.MemorySink{}
	_, err := Run(context.Background(), undeclaredEngine, undeclaredRoot, undeclaredSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"summary-fixture"}})
	if err == nil || !strings.Contains(err.Error(), "undeclared summary") {
		t.Fatalf("undeclared summary read error = %v", err)
	}
	for _, rec := range undeclaredSink.CloneRecords() {
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(rec.Payload, &probe) != nil {
			continue
		}
		if probe.Type == graphstream.TypeEndReplace {
			t.Fatal("undeclared summary read completed EndReplace")
		}
	}
}

func TestRepositoryAnalyzerPluginHostResolverCacheAndColdEquality(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":         `import { value } from "./dep"; export const caller = value;`,
		"src/dep.tsx":      `export const value = 1;`,
		"src/impl.ts":      `export const value = 2;`,
		"src/dep/index.ts": `export const value = 3;`,
	})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "plugin-state")
	allow := []string{"fixture"}
	cons := NewConsumer()
	firstSink := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, root, firstSink, Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: allow})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, firstSink)
	state := pluginState(t, stateDir)
	record := state.AnalyzerPlugins["fixture"].Units["file:src/a.ts"]
	var summary map[string]any
	if err := json.Unmarshal(record.Summary, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["module_file"] != "src/dep.tsx" || summary["export_target"] != "src.value" {
		t.Fatalf("plugin host resolver summary = %#v unit=%+v observations=%+v", summary, record.Decl, record.Observations)
	}
	coldGeneration := state.Generation
	if _, ok := factByName(first.Facts, "fixture-machine/state:idle"); !ok {
		t.Fatal("plugin FSM contribution was not returned in graph facts")
	}
	starts := pluginCounterLines(t, counter)
	if len(starts) != 1 {
		t.Fatalf("plugin starts after cold run = %d", len(starts))
	}
	if first.AnalyzerPlugins.Plugins != 1 || first.AnalyzerPlugins.ProcessesStarted != 1 || first.AnalyzerPlugins.PlansRun != 1 || first.AnalyzerPlugins.UnitsPlanned == 0 || first.AnalyzerPlugins.UnitsExecuted != first.AnalyzerPlugins.UnitsPlanned || first.AnalyzerPlugins.UnitsReused != 0 || first.AnalyzerPlugins.OwnerFiles == 0 || first.ReplacementOwners == 0 {
		t.Fatalf("cold plugin work metrics = %+v replacement owners=%d", first.AnalyzerPlugins, first.ReplacementOwners)
	}
	var launch struct {
		Entry string `json:"entry"`
		CWD   string `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(starts[0]), &launch); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(launch.Entry, root) || strings.Contains(launch.CWD, root) || !strings.Contains(launch.CWD, "enola-plugin-") {
		t.Fatalf("plugin launch leaked repository path or missed temporary cwd: %+v", launch)
	}

	noopSink := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, root, noopSink, Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: allow})
	if err != nil {
		t.Fatal(err)
	}
	noopState := pluginState(t, stateDir)
	if noop.ParsedFiles != 0 || len(noopSink.CloneRecords()) != 0 || len(pluginCounterLines(t, counter)) != 1 || noopState.Generation != coldGeneration {
		t.Fatalf("plugin no-op parsed=%d records=%d starts=%d generation=%d want=%d", noop.ParsedFiles, len(noopSink.CloneRecords()), len(pluginCounterLines(t, counter)), noopState.Generation, coldGeneration)
	}
	if noop.AnalyzerPlugins.ProcessesStarted != 0 || noop.AnalyzerPlugins.PlansRun != 0 || noop.AnalyzerPlugins.UnitsExecuted != 0 || noop.AnalyzerPlugins.UnitsReused != noop.AnalyzerPlugins.UnitsPlanned || noop.ReplacementOwners != 0 {
		t.Fatalf("plugin no-op work metrics = %+v replacement owners=%d", noop.AnalyzerPlugins, noop.ReplacementOwners)
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lockSink := &graphstream.MemorySink{}
	lockOnly, err := Run(context.Background(), eng, root, lockSink, Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: allow})
	if err != nil {
		t.Fatal(err)
	}
	lockState := pluginState(t, stateDir)
	if lockOnly.ParsedFiles != 0 || len(lockSink.CloneRecords()) != 0 || len(pluginCounterLines(t, counter)) != 1 || lockState.Generation != coldGeneration {
		t.Fatalf("lock-only no-op parsed=%d records=%d starts=%d generation=%d want=%d", lockOnly.ParsedFiles, len(lockSink.CloneRecords()), len(pluginCounterLines(t, counter)), lockState.Generation, coldGeneration)
	}
	if lockOnly.AnalyzerPlugins.ProcessesStarted != 0 || lockOnly.AnalyzerPlugins.PlansRun != 0 || lockOnly.AnalyzerPlugins.UnitsExecuted != 0 || lockOnly.AnalyzerPlugins.UnitsReused != lockOnly.AnalyzerPlugins.UnitsPlanned || lockOnly.ReplacementOwners != 0 {
		t.Fatalf("lock-only plugin work metrics = %+v replacement owners=%d", lockOnly.AnalyzerPlugins, lockOnly.ReplacementOwners)
	}

	// Adding a higher-precedence candidate must invalidate the cached unit even
	// though its previous target and all plugin configuration are unchanged.
	if err := os.WriteFile(filepath.Join(root, "src/dep.ts"), []byte(`export { value } from "./impl";`), 0o644); err != nil {
		t.Fatal(err)
	}
	changedSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, changedSink, Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: allow}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, changedSink)
	state = pluginState(t, stateDir)
	record = state.AnalyzerPlugins["fixture"].Units["file:src/a.ts"]
	if err := json.Unmarshal(record.Summary, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["module_file"] != "src/dep.ts" || summary["export_target"] != "src.value" {
		t.Fatalf("resolver did not move to new preferred candidate: %#v", summary)
	}
	if len(pluginCounterLines(t, counter)) != 2 {
		t.Fatalf("preferred-candidate addition did not re-run one plugin process")
	}

	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, coldSink, Options{StateDir: t.TempDir(), ForceInitial: true, AuthoritativeFiles: true, AllowRepoPlugins: allow}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestRepositoryAnalyzerPluginResidentRunMetrics(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts": `export const a = 1;`,
		"src/b.ts": `export const b = 2;`,
	})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "resident-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	sink := &graphstream.MemorySink{}

	openStarted := time.Now()
	var openBefore, openAfter runtime.MemStats
	runtime.ReadMemStats(&openBefore)
	resident, err := OpenSession(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	runtime.ReadMemStats(&openAfter)
	t.Logf("T001_PLUGIN_RESIDENT %s", mustPluginMeasurementJSON(t, map[string]any{
		"scenario":                        "session_open",
		"wall_ms":                         float64(time.Since(openStarted).Nanoseconds()) / 1e6,
		"go_heap_total_alloc_delta_bytes": openAfter.TotalAlloc - openBefore.TotalAlloc,
	}))

	var committed *Result
	var applied *Consumer
	applied = NewConsumer()
	consumedRecords := 0
	measure := func(scenario string) *Result {
		t.Helper()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		started := time.Now()
		result, err := resident.reconcile(context.Background(), true)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		t.Logf("T001_PLUGIN_RESIDENT %s", mustPluginMeasurementJSON(t, map[string]any{
			"scenario":                        scenario,
			"wall_ms":                         float64(elapsed.Nanoseconds()) / 1e6,
			"go_heap_total_alloc_delta_bytes": after.TotalAlloc - before.TotalAlloc,
			"parsed_files":                    result.ParsedFiles,
			"replacement_owners":              result.ReplacementOwners,
			"owners_published":                result.OwnersPublished,
			"generation":                      []int64{result.BaseGeneration, result.TargetGeneration},
			"plugin_stats":                    result.AnalyzerPlugins,
		}))
		allRecords := sink.CloneRecords()
		if consumedRecords > len(allRecords) {
			t.Fatalf("resident sink record count went backwards: before=%d after=%d", consumedRecords, len(allRecords))
		}
		if err := applied.ApplyRecords(allRecords[consumedRecords:]); err != nil {
			t.Fatal(err)
		}
		consumedRecords = len(allRecords)
		return result
	}

	committed = measure("initial")
	if committed.AnalyzerPlugins.ProcessesStarted != 1 || committed.AnalyzerPlugins.PlansRun != 1 || committed.AnalyzerPlugins.UnitsExecuted != committed.AnalyzerPlugins.UnitsPlanned || committed.ParsedFiles == 0 {
		t.Fatalf("resident initial metrics = %+v parsed=%d", committed.AnalyzerPlugins, committed.ParsedFiles)
	}
	noChange := measure("no_change")
	if noChange.ParsedFiles != 0 || noChange.OwnersPublished != 0 || noChange.ReplacementOwners != 0 || noChange.TargetGeneration != committed.TargetGeneration || noChange.AnalyzerPlugins.ProcessesStarted != 0 || noChange.AnalyzerPlugins.UnitsExecuted != 0 || noChange.AnalyzerPlugins.UnitsReused != noChange.AnalyzerPlugins.UnitsPlanned {
		t.Fatalf("resident no-change metrics = %+v parsed=%d published=%d replacements=%d generation=%d want=%d", noChange.AnalyzerPlugins, noChange.ParsedFiles, noChange.OwnersPublished, noChange.ReplacementOwners, noChange.TargetGeneration, committed.TargetGeneration)
	}
	if got := pluginCounterLines(t, counter); len(got) != 1 {
		t.Fatalf("resident no-change plugin process starts = %d, want 1", len(got))
	}
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte(`export const a = 10;`), 0o644); err != nil {
		t.Fatal(err)
	}
	delta := measure("single_file_delta")
	if delta.ParsedFiles != 1 || delta.AnalyzerPlugins.ProcessesStarted != 1 || delta.AnalyzerPlugins.UnitsExecuted != 1 || delta.AnalyzerPlugins.UnitsReused != 1 {
		t.Fatalf("resident single-file delta metrics = %+v parsed=%d", delta.AnalyzerPlugins, delta.ParsedFiles)
	}
	if got := pluginCounterLines(t, counter); len(got) != 2 {
		t.Fatalf("resident process starts after delta = %d, want 2", len(got))
	}

	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, applied, cold)
}

func mustPluginMeasurementJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRepositoryAnalyzerPluginTrustFailsBeforePublishing(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	registration := genericAnalyzerPlugin(t, root, filepath.Join(t.TempDir(), "starts"))
	eng := configScopeEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	sink := &graphstream.MemorySink{}
	_, err := OpenSession(context.Background(), eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("untrusted configured plugin error = %v", err)
	}
	if len(sink.CloneRecords()) != 0 {
		t.Fatal("untrusted plugin published graph records")
	}
}

func TestRepositoryAnalyzerPluginAdmissionFailuresKeepCommittedGraph(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string, registration analyzerplugin.Config)
		allow  bool
	}{
		{
			name: "missing",
			mutate: func(t *testing.T, root string, registration analyzerplugin.Config) {
				t.Helper()
				if err := os.RemoveAll(filepath.Join(root, registration.Path)); err != nil {
					t.Fatal(err)
				}
			},
			allow: true,
		},
		{
			name: "malformed manifest",
			mutate: func(t *testing.T, root string, registration analyzerplugin.Config) {
				t.Helper()
				manifest := filepath.Join(root, registration.Path, "enola-plugin.yaml")
				if err := os.WriteFile(manifest, []byte("api: unsupported\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			allow: true,
		},
		{
			name: "untrusted",
			mutate: func(t *testing.T, _ string, _ analyzerplugin.Config) {
				t.Helper()
			},
		},
		{
			name: "process failure",
			mutate: func(t *testing.T, root string, registration analyzerplugin.Config) {
				t.Helper()
				entry := filepath.Join(root, registration.Path, "plugin.mjs")
				data, err := os.ReadFile(entry)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(entry, append([]byte("process.exit(23);\n"), data...), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			allow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
			counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
			registration := genericAnalyzerPlugin(t, root, counter)
			eng := admissionEngine(t, root, graphinput.Options{})
			eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
			stateDir := filepath.Join(root, ".enola", "plugin-state")
			opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
			committedSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, root, committedSink, opts); err != nil {
				t.Fatal(err)
			}
			committed := pluginState(t, stateDir)
			identity := committed.AnalyzerPlugins["fixture"].Identity

			tt.mutate(t, root, registration)
			if !tt.allow {
				opts.AllowRepoPlugins = nil
			}
			failedSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, root, failedSink, opts); err == nil {
				t.Fatal("invalid plugin was accepted")
			}
			if records := failedSink.CloneRecords(); len(records) != 0 {
				t.Fatalf("invalid plugin published %d records", len(records))
			}
			after := pluginState(t, stateDir)
			if after.Generation != committed.Generation || after.AnalyzerPlugins["fixture"].Identity != identity {
				t.Fatalf("failed admission changed committed state: before generation=%d after=%d before identity=%s after identity=%s", committed.Generation, after.Generation, identity, after.AnalyzerPlugins["fixture"].Identity)
			}
			if len(after.AnalyzerPlugins["fixture"].Units) == 0 {
				t.Fatal("failed admission removed previously committed plugin units")
			}
		})
	}
}

func TestRepositoryAnalyzerPluginRenameDeletionAndExplicitRemoval(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	cons := NewConsumer()
	var last *Result
	run := func(engine *engine.Engine, runOpts Options) {
		t.Helper()
		sink := &graphstream.MemorySink{}
		var err error
		last, err = Run(context.Background(), engine, root, sink, runOpts)
		if err != nil {
			t.Fatal(err)
		}
		if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
	}
	run(eng, opts)
	state := pluginState(t, stateDir)
	if _, ok := state.AnalyzerPlugins["fixture"].Units["file:src/a.ts"]; !ok {
		t.Fatal("initial plugin owner unit missing")
	}

	if err := os.Rename(filepath.Join(root, "src/a.ts"), filepath.Join(root, "src/renamed.ts")); err != nil {
		t.Fatal(err)
	}
	run(eng, opts)
	state = pluginState(t, stateDir)
	units := state.AnalyzerPlugins["fixture"].Units
	if _, ok := units["file:src/a.ts"]; ok {
		t.Fatal("renamed plugin unit retained its old owner")
	}
	if _, ok := units["file:src/renamed.ts"]; !ok {
		t.Fatal("renamed plugin owner unit was not planned")
	}
	if got := pluginCounterLines(t, counter); len(got) != 2 {
		t.Fatalf("rename did not use one changed-run plugin process: %v", got)
	}
	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldOpts := opts
	coldOpts.StateDir = t.TempDir()
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, coldSink, coldOpts); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)

	if err := os.Remove(filepath.Join(root, "src/renamed.ts")); err != nil {
		t.Fatal(err)
	}
	run(eng, opts)
	state = pluginState(t, stateDir)
	if len(state.AnalyzerPlugins["fixture"].Units) != 0 {
		t.Fatalf("deletion left unexpected plugin units: %#v", state.AnalyzerPlugins["fixture"].Units)
	}
	for _, fact := range last.Facts {
		if fact.File == "src/renamed.ts" && fact.Props["plugin"] == "fixture" {
			t.Fatal("deleted plugin owner retained a published contribution")
		}
	}
	coldEngine = admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	coldOpts.StateDir = t.TempDir()
	coldSink = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, coldSink, coldOpts); err != nil {
		t.Fatal(err)
	}
	cold = NewConsumer()
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, cons, cold)

	removedEngine := admissionEngine(t, root, graphinput.Options{})
	removedSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), removedEngine, root, removedSink, Options{StateDir: stateDir, AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	if len(pluginState(t, stateDir).AnalyzerPlugins) != 0 {
		t.Fatal("explicit configuration removal left plugin cache state")
	}
	if err := cons.ApplyRecords(removedSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	noPluginCold := coldConsumer(t, admissionEngine(t, root, graphinput.Options{}), root)
	assertAppliedEqualsCold(t, cons, noPluginCold)
}

func TestRepositoryAnalyzerPluginIdentityChangesInvalidateUnits(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	stateDir := filepath.Join(root, ".enola", "plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	run := func(config analyzerplugin.Config) *Result {
		t.Helper()
		eng := admissionEngine(t, root, graphinput.Options{})
		eng.Config().AnalyzerPlugins = []analyzerplugin.Config{config}
		res, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	run(registration)
	firstIdentity := pluginState(t, stateDir).AnalyzerPlugins["fixture"].Identity

	registration.Config = map[string]any{"counter": counter, "revision": 2}
	run(registration)
	configIdentity := pluginState(t, stateDir).AnalyzerPlugins["fixture"].Identity
	if configIdentity == firstIdentity || len(pluginCounterLines(t, counter)) != 2 {
		t.Fatal("plugin configuration change did not invalidate cached units")
	}

	entry := filepath.Join(root, "tools", "analyzers", "fixture", "plugin.mjs")
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, append(data, []byte("\n// identity change\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	run(registration)
	codeIdentity := pluginState(t, stateDir).AnalyzerPlugins["fixture"].Identity
	if codeIdentity == configIdentity || len(pluginCounterLines(t, counter)) != 3 {
		t.Fatal("plugin code change did not invalidate cached units")
	}
}

func TestRepositoryAnalyzerPluginFailureKeepsLastCommittedGraph(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}}
	firstSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, firstSink, opts); err != nil {
		t.Fatal(err)
	}
	committed := pluginState(t, stateDir)
	identity := committed.AnalyzerPlugins["fixture"].Identity
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte(`export const a = 2;`), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "tools", "analyzers", "fixture", "plugin.mjs")
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, append([]byte("process.exit(23);\n"), data...), 0o644); err != nil {
		t.Fatal(err)
	}
	failedSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, failedSink, opts); err == nil {
		t.Fatal("failed plugin run was accepted")
	}
	if records := failedSink.CloneRecords(); len(records) != 0 {
		t.Fatalf("failed plugin retracted or published %d graph records", len(records))
	}
	after := pluginState(t, stateDir)
	if after.Generation != committed.Generation || after.AnalyzerPlugins["fixture"].Identity != identity {
		t.Fatalf("failed plugin advanced committed cache: before=%d after=%d", committed.Generation, after.Generation)
	}
}

func TestRepositoryAnalyzerPluginOutOfInventoryOwnerFailsBeforeBegin(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": `export const a = 1;`})
	counter := filepath.Join(t.TempDir(), "plugin-starts.jsonl")
	registration := genericAnalyzerPlugin(t, root, counter)
	entry := filepath.Join(root, "tools", "analyzers", "fixture", "plugin.mjs")
	script, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	needle := `owners: { [file]: { nodes: [node] } }`
	replacement := `owners: { [file]: { nodes: [node] }, "src/ghost.ts": { nodes: [{ ...node, owner: "src/ghost.ts" }] } }`
	if !strings.Contains(string(script), needle) {
		t.Fatal("plugin fixture output shape changed")
	}
	if err := os.WriteFile(entry, []byte(strings.Replace(string(script), needle, replacement, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := t.TempDir()
	sink := &graphstream.MemorySink{}
	_, err = Run(context.Background(), eng, root, sink, Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"fixture"}})
	if err == nil || !strings.Contains(err.Error(), "outside the captured graph inventory") {
		t.Fatalf("out-of-inventory owner error = %v", err)
	}
	for _, rec := range sink.CloneRecords() {
		var probe struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(rec.Payload, &probe) != nil {
			continue
		}
		if probe.Type == graphstream.TypeEndReplace {
			t.Fatal("out-of-inventory output completed EndReplace")
		}
	}
	state, err := loadCommittedState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil && state.Generation != 0 {
		t.Fatalf("out-of-inventory output advanced generation to %d", state.Generation)
	}
}
