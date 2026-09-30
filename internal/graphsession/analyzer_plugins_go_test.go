package graphsession

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/analyzerplugin"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

func goAnalyzerPluginFixture(t *testing.T, root string) analyzerplugin.Config {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Go analyzer plugin fixture")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	pluginDir := filepath.Join(root, "tools", "task-graph")
	entry := filepath.Join(pluginDir, "dist", "task-graph")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", entry, "./internal/analyzerplugin/testdata/go_plugin")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Go plugin fixture: %v\n%s", err, output)
	}
	manifest := "" +
		"api: enola.plugin/v2\n" +
		"name: task-graph\n" +
		"runtime:\n  kind: go-executable\n  entry: dist/task-graph\n" +
		"identity_files: [dist/task-graph]\n" +
		"hooks: [analysis.plan@1, analysis.unit@1]\n" +
		"owner_domain: [docs/**]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/task-graph", Config: map[string]any{"series": "sample"}}
}

func TestGoNonFSMPluginColdDeltaAndNoopGraphContract(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"docs/T-1.md": "# Task one\n", "src/a.ts": "export const a = 1;\n"})
	registration := goAnalyzerPluginFixture(t, root)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "go-plugin-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}
	consumer := NewConsumer()

	coldSink := &graphstream.MemorySink{}
	coldRun, err := Run(context.Background(), eng, root, coldSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if coldRun.AnalyzerPlugins.ProcessesStarted != 1 {
		t.Fatalf("cold plugin process count = %d, want 1", coldRun.AnalyzerPlugins.ProcessesStarted)
	}
	coldGeneration := pluginState(t, stateDir).Generation
	applyRun(t, consumer, coldSink)
	canonical := consumer.Canonical()
	for _, want := range []string{"plugin:task-graph:task", "plugin:task-graph:depends_on", "plugin_properties", "task-graph"} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("cold graph omitted %q: %s", want, canonical)
		}
	}
	if !strings.Contains(canonical, "active") || !strings.Contains(canonical, "plugin:task-graph:related") {
		t.Fatalf("cold graph omitted plugin enrichment properties or relations: %s", canonical)
	}
	if !strings.Contains(canonical, "target_kind") || !strings.Contains(canonical, "plugin:task-graph:task") {
		t.Fatalf("cold graph lost typed relation target: %s", canonical)
	}

	noopSink := &graphstream.MemorySink{}
	noopRun, err := Run(context.Background(), eng, root, noopSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if noopRun.AnalyzerPlugins.ProcessesStarted != 0 || len(noopSink.CloneRecords()) != 0 {
		t.Fatalf("no-op run spawned or published plugin work: stats=%+v records=%d", noopRun.AnalyzerPlugins, len(noopSink.CloneRecords()))
	}
	if noopGeneration := pluginState(t, stateDir).Generation; noopGeneration != coldGeneration {
		t.Fatalf("no-op run advanced generation from %d to %d", coldGeneration, noopGeneration)
	}

	if err := os.WriteFile(filepath.Join(root, "docs", "T-1.md"), []byte("# Updated task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	deltaRun, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if deltaRun.AnalyzerPlugins.UnitsExecuted == 0 {
		t.Fatalf("changed owner did not rerun plugin analysis: %+v", deltaRun.AnalyzerPlugins)
	}
	applyRun(t, consumer, deltaSink)

	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	fullSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, fullSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}); err != nil {
		t.Fatal(err)
	}
	full := NewConsumer()
	applyRun(t, full, fullSink)
	assertAppliedEqualsCold(t, consumer, full)
}

func TestGoPluginSummaryChangeInvalidatesDeclaredConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"docs/summary-source.md":   "source-v1\n",
		"docs/summary-consumer.md": "consumer\n",
	})
	registration := goAnalyzerPluginFixture(t, root)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "go-plugin-summary-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}
	sink := &graphstream.MemorySink{}
	cold, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cold.AnalyzerPlugins.UnitsExecuted != 2 {
		t.Fatalf("cold Go plugin units executed = %d, want producer and consumer", cold.AnalyzerPlugins.UnitsExecuted)
	}
	updated := NewConsumer()
	applyRun(t, updated, sink)

	if err := os.WriteFile(filepath.Join(root, "docs", "summary-source.md"), []byte("source-v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.AnalyzerPlugins.UnitsExecuted != 2 {
		t.Fatalf("changed summary executed %d Go plugin units, want producer and declared consumer", delta.AnalyzerPlugins.UnitsExecuted)
	}
	state := pluginState(t, stateDir)
	consumer := state.AnalyzerPlugins["task-graph"].Units["summary:consumer"]
	contribution := consumer.Owners["docs/summary-consumer.md"]
	if len(contribution.Nodes) != 1 || contribution.Nodes[0].Props["source_summary"] != "source-v2\n" {
		t.Fatalf("consumer contribution did not observe changed summary: %#v", contribution)
	}

	applyRun(t, updated, deltaSink)
	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	fullSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, fullSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}); err != nil {
		t.Fatal(err)
	}
	full := NewConsumer()
	applyRun(t, full, fullSink)
	assertAppliedEqualsCold(t, updated, full)
}

func TestGoPluginOwnerMoveKeepsStableFactIdentityAndColdEquality(t *testing.T) {
	const oldOwner = "docs/T-1.md"
	const newOwner = "docs/archive/T-1.md"
	root := setupTSRepo(t, map[string]string{oldOwner: "# Stable task\n"})
	registration := goAnalyzerPluginFixture(t, root)
	stateDir := filepath.Join(root, ".enola", "go-plugin-move-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}

	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	initialSink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), eng, root, initialSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	initialFact, ok := factByName(initial.Facts, "sample/T-1")
	if !ok || initialFact.Kind != "plugin:task-graph:task" || initialFact.File != oldOwner {
		t.Fatalf("initial plugin task fact = %#v, found=%v", initialFact, ok)
	}
	consumer := NewConsumer()
	applyRun(t, consumer, initialSink)

	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, newOwner)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, oldOwner), filepath.Join(root, newOwner)); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	movedFact, ok := factByName(delta.Facts, "sample/T-1")
	if !ok || movedFact.Kind != "plugin:task-graph:task" || movedFact.File != newOwner {
		t.Fatalf("moved plugin task fact = %#v, found=%v", movedFact, ok)
	}
	if movedFact.Identity() != initialFact.Identity() {
		t.Fatalf("plugin task identity changed on owner move: before=%s after=%s", initialFact.Identity(), movedFact.Identity())
	}
	if delta.AnalyzerPlugins.UnitsExecuted == 0 {
		t.Fatalf("owner move did not rerun plugin unit: %+v", delta.AnalyzerPlugins)
	}
	applyRun(t, consumer, deltaSink)

	state := pluginState(t, stateDir)
	oldOwnerPresent, newOwnerPresent := false, false
	for _, unit := range state.AnalyzerPlugins["task-graph"].Units {
		_, old := unit.Owners[oldOwner]
		_, moved := unit.Owners[newOwner]
		oldOwnerPresent = oldOwnerPresent || old
		newOwnerPresent = newOwnerPresent || moved
	}
	if oldOwnerPresent || !newOwnerPresent {
		t.Fatalf("moved owner records: old=%v new=%v", oldOwnerPresent, newOwnerPresent)
	}

	coldEngine := admissionEngine(t, root, graphinput.Options{})
	coldEngine.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	fullSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), coldEngine, root, fullSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}); err != nil {
		t.Fatal(err)
	}
	full := NewConsumer()
	applyRun(t, full, fullSink)
	assertAppliedEqualsCold(t, consumer, full)
}

func TestGoPluginInvalidOwnerFailsBeforeReplacementAndPreservesCommit(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"docs/T-1.md": "valid task\n"})
	registration := goAnalyzerPluginFixture(t, root)
	eng := admissionEngine(t, root, graphinput.Options{})
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	stateDir := filepath.Join(root, ".enola", "go-plugin-fail-closed-state")
	opts := Options{StateDir: stateDir, AuthoritativeFiles: true, AllowRepoPlugins: []string{"task-graph"}}
	initialSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initialSink, opts); err != nil {
		t.Fatal(err)
	}
	committed := pluginState(t, stateDir)
	if committed.Generation == 0 {
		t.Fatal("initial Go plugin run did not commit a generation")
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "T-1.md"), []byte("INVALID_OWNER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failedSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, failedSink, opts); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("invalid owner run error = %v, want owner-domain/inventory failure", err)
	}
	for _, record := range failedSink.CloneRecords() {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(record.Payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Type == graphstream.TypeBeginReplace || envelope.Type == graphstream.TypeEndReplace {
			t.Fatalf("failed Go plugin run emitted replacement boundary %q", envelope.Type)
		}
	}
	after := pluginState(t, stateDir)
	if after.Generation != committed.Generation {
		t.Fatalf("failed Go plugin run advanced generation from %d to %d", committed.Generation, after.Generation)
	}
	if after.AnalyzerPlugins["task-graph"].Identity != committed.AnalyzerPlugins["task-graph"].Identity {
		t.Fatal("failed Go plugin run replaced committed plugin identity")
	}
	if !reflect.DeepEqual(after.AnalyzerPlugins["task-graph"].Units, committed.AnalyzerPlugins["task-graph"].Units) {
		t.Fatal("failed Go plugin run mutated committed plugin contributions")
	}
}

func TestMergeAnalyzerPluginEnrichmentTargetsStableHostFact(t *testing.T) {
	base := []facts.Fact{{
		Kind: facts.KindSymbol, Name: "Task", File: "docs/task.md",
		Props:     map[string]any{"visibility": "public"},
		Relations: []facts.Relation{{Kind: "declares", Target: "docs", TargetKind: facts.KindModule}},
	}}
	merged, err := mergeAnalyzerPluginEnrichments(base, []pluginEnrichment{{
		Plugin: "task-graph", API: analyzerplugin.GoAPIVersion, Identity: "identity",
		Value: analyzerplugin.Enrichment{
			Owner: "docs/task.md", Kind: facts.KindSymbol, Name: "Task",
			Props:     map[string]any{"task_status": "active"},
			Relations: []analyzerplugin.Relation{{Kind: "related", Target: "sample/T-2", TargetKind: "task"}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := base[0].Props["plugin_properties"]; got != nil {
		t.Fatalf("merge mutated base fact props: %#v", got)
	}
	props := merged[0].Props
	if props["visibility"] != "public" {
		t.Fatalf("merge removed host property: %#v", props)
	}
	pluginProps, ok := props["plugin_properties"].(map[string]any)
	if !ok {
		t.Fatalf("plugin properties missing: %#v", props)
	}
	taskGraph, ok := pluginProps["task-graph"].(map[string]any)
	if !ok || taskGraph["task_status"] != "active" {
		t.Fatalf("plugin properties not merged under plugin namespace: %#v", pluginProps)
	}
	wantRelation := facts.Relation{Kind: "plugin:task-graph:related", Target: "sample/T-2", TargetKind: "plugin:task-graph:task"}
	if len(merged[0].Relations) != 2 || merged[0].Relations[1] != wantRelation {
		t.Fatalf("enrichment relations = %#v, want appended %#v", merged[0].Relations, wantRelation)
	}
}

func TestMergeAnalyzerPluginEnrichmentFailsClosedForMissingOrAmbiguousTarget(t *testing.T) {
	enrichment := pluginEnrichment{Plugin: "task-graph", API: analyzerplugin.GoAPIVersion, Value: analyzerplugin.Enrichment{
		Owner: "docs/task.md", Kind: "task", Name: "sample/T-1",
	}}
	cases := []struct {
		name  string
		base  []facts.Fact
		match string
	}{
		{name: "missing", match: "missing"},
		{name: "ambiguous", base: []facts.Fact{
			{Kind: "plugin:task-graph:task", Name: "sample/T-1", File: "docs/task.md"},
			{Kind: "plugin:task-graph:task", Name: "sample/T-1", File: "docs/task.md"},
		}, match: "ambiguous"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mergeAnalyzerPluginEnrichments(tc.base, []pluginEnrichment{enrichment})
			if err == nil || !strings.Contains(err.Error(), tc.match) {
				t.Fatalf("merge error = %v, want %q", err, tc.match)
			}
		})
	}
}
