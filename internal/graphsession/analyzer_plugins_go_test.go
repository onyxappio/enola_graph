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
	"github.com/enola-labs/enola/internal/fsm"
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

func goFSMAnalyzerPluginFixture(t *testing.T, root string) analyzerplugin.Config {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Go FSM analyzer plugin fixture")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	pluginDir := filepath.Join(root, "tools", "product-fsm")
	entry := filepath.Join(pluginDir, "dist", "product-fsm")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", entry, "./internal/analyzerplugin/testdata/go_fsm_plugin")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Go FSM plugin fixture: %v\n%s", err, output)
	}
	manifest := "" +
		"api: enola.plugin/v2\n" +
		"name: product-fsm\n" +
		"runtime:\n  kind: go-executable\n  entry: dist/product-fsm\n" +
		"identity_files: [dist/product-fsm]\n" +
		"vocabularies: [enola.fsm@1]\n" +
		"claims:\n  machines: [product]\n" +
		"hooks: [analysis.plan@1, analysis.unit@1]\n" +
		"owner_domain: [src/**]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/product-fsm"}
}

func goNonFSMVocabularyAnalyzerPluginFixture(t *testing.T, root string) analyzerplugin.Config {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate non-FSM Go analyzer plugin fixture")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	pluginDir := filepath.Join(root, "tools", "generic-fsm-names")
	entry := filepath.Join(pluginDir, "dist", "generic-fsm-names")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", entry, "./internal/analyzerplugin/testdata/go_non_fsm_plugin")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build non-FSM Go plugin fixture: %v\n%s", err, output)
	}
	manifest := "" +
		"api: enola.plugin/v2\n" +
		"name: generic-fsm-names\n" +
		"runtime:\n  kind: go-executable\n  entry: dist/generic-fsm-names\n" +
		"identity_files: [dist/generic-fsm-names]\n" +
		"hooks: [analysis.plan@1, analysis.unit@1]\n" +
		"owner_domain: [src/**]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return analyzerplugin.Config{Path: "tools/generic-fsm-names"}
}

func TestGoFSMVocabularyPublishesHostFactsAndKeepsGenericContributionsNamespaced(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/machine.ts": "export function dispatch() {}\n"})
	registration := goFSMAnalyzerPluginFixture(t, root)
	eng := testEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	run, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: filepath.Join(root, ".enola", "fsm-plugin-state"), AuthoritativeFiles: true, AllowRepoPlugins: []string{"product-fsm"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.AnalyzerPlugins.ParsedFiles != 5 {
		t.Fatalf("plugin parsed-file census = %d, want sum 5", run.AnalyzerPlugins.ParsedFiles)
	}
	for name, kind := range map[string]string{
		"product":                       facts.KindFSMMachine,
		"product/state:idle":            facts.KindFSMState,
		"product/event:start":           facts.KindFSMEvent,
		"product/command:start":         facts.KindFSMCommand,
		"product/transition:idle-start": facts.KindFSMTransition,
	} {
		fact, found := factByName(run.Facts, name)
		if !found || fact.Kind != kind {
			t.Fatalf("FSM vocabulary kind %s/%s = %#v, found=%v", kind, name, fact, found)
		}
		if fact.Props["plugin_properties"] != nil {
			t.Fatalf("FSM vocabulary fact %s was nested: %#v", name, fact.Props)
		}
	}
	state, ok := factByName(run.Facts, "product/state:idle")
	if !ok || state.Kind != facts.KindFSMState {
		t.Fatalf("FSM state fact = %#v, found=%v", state, ok)
	}
	if state.Props["state"] != "idle" || state.Props["plugin_properties"] != nil {
		t.Fatalf("FSM state properties were not flat: %#v", state.Props)
	}
	if len(state.Relations) != 2 || state.Relations[0].Kind != facts.RelDeclares || state.Relations[0].Target != "product" || state.Relations[1].Kind != facts.RelFSMParent || state.Relations[1].TargetKind != facts.KindFSMState {
		t.Fatalf("FSM state relations were not host relations: %#v", state.Relations)
	}
	coverage, ok := factByName(run.Facts, "plugin:product-fsm:coverage")
	if !ok || coverage.Kind != facts.KindExtraction || coverage.Props["edge_coverage"] != "partial" || coverage.Props["plugin_properties"] != nil {
		t.Fatalf("coverage fact was not V1-compatible: %#v, found=%v", coverage, ok)
	}
	generic, ok := factByName(run.Facts, "product/diagnostic")
	if !ok || generic.Kind != "plugin:product-fsm:diagnostic" {
		t.Fatalf("generic fact lost its namespace: %#v, found=%v", generic, ok)
	}
	if generic.Props["plugin_properties"] == nil || generic.Relations[0].Kind != "plugin:product-fsm:mentions" {
		t.Fatalf("generic contribution lost V2 isolation: %#v", generic)
	}
	contribs := pluginState(t, filepath.Join(root, ".enola", "fsm-plugin-state")).Files["src/machine.ts"].Contrib["plugin:product-fsm"]
	for _, contrib := range contribs {
		if contrib.Kind == facts.KindSymbol && contrib.Name == "dispatch" {
			if contrib.Props["anchor_detail"] != "flat" || contrib.Props["plugin_properties"] != nil {
				t.Fatalf("persisted anchor properties were not flat: %#v", contrib.Props)
			}
			return
		}
	}
	t.Fatalf("persisted anchor symbol missing from plugin contribs: %#v", contribs)
}

func TestGoFSMVocabularyPublishesLegacyDeclaresEdges(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/machine.ts": "export function dispatch() {}\n"})
	eng := testEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{goFSMAnalyzerPluginFixture(t, root)}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, Options{
		StateDir: filepath.Join(root, ".enola", "fsm-declares-state"), AuthoritativeFiles: true, AllowRepoPlugins: []string{"product-fsm"},
	}); err != nil {
		t.Fatal(err)
	}
	consumer := applyGraph(t, sink)
	machine, ok := pluginGraphNode(consumer, facts.KindFSMMachine, "product")
	if !ok {
		t.Fatal("published FSM machine missing")
	}
	if !consumerHasRelation(consumer, "product", facts.RelDeclares, "src") {
		t.Fatal("published machine lost its un-namespaced declares relation")
	}
	for _, member := range []struct{ kind, name string }{
		{facts.KindFSMState, "product/state:idle"},
		{facts.KindFSMEvent, "product/event:start"},
		{facts.KindFSMCommand, "product/command:start"},
	} {
		node, ok := pluginGraphNode(consumer, member.kind, member.name)
		if !ok {
			t.Fatalf("published FSM member missing: %s/%s", member.kind, member.name)
		}
		assertPublishedDeclaresEdge(t, consumer, node, machine)
	}
}

func TestV2FSMVocabularyDeclaresResolvesLikeLegacy(t *testing.T) {
	module := facts.Fact{Kind: facts.KindModule, Name: "src", File: "src"}
	legacy := facts.Fact{Kind: facts.KindFSMMachine, Name: "product", File: "src/machine.ts", Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: "src"}}}
	pluginFacts := pluginOwnerResultFacts("product-fsm", analyzerplugin.GoAPIVersion, true, map[string]analyzerplugin.OwnerResult{
		"src/machine.ts": {Nodes: []analyzerplugin.Node{{
			Kind: facts.KindFSMMachine, Name: "product", Relations: []analyzerplugin.Relation{{Kind: facts.RelDeclares, Target: "src", TargetKind: facts.KindModule}},
		}}},
	})
	if len(pluginFacts) != 1 || len(pluginFacts[0].Relations) != 1 || pluginFacts[0].Relations[0].Kind != facts.RelDeclares {
		t.Fatalf("V2 FSM declares relation was namespaced or dropped: %#v", pluginFacts)
	}
	_, legacyEdges := encodeOwner(ownerOutput{Owner: ownerOf(legacy), Facts: []facts.Fact{legacy}}, buildIndex([]facts.Fact{legacy, module}), false)
	_, pluginEdges := encodeOwner(ownerOutput{Owner: ownerOf(pluginFacts[0]), Facts: pluginFacts}, buildIndex(append(pluginFacts, module)), false)
	if len(legacyEdges) != 1 || len(pluginEdges) != 1 {
		t.Fatalf("legacy/plugin declares edge count = %d/%d", len(legacyEdges), len(pluginEdges))
	}
	legacyEdge, pluginEdge := legacyEdges[0], pluginEdges[0]
	if pluginEdge.Kind != legacyEdge.Kind || pluginEdge.TargetName != legacyEdge.TargetName || pluginEdge.TargetID != legacyEdge.TargetID || pluginEdge.Resolution != legacyEdge.Resolution {
		t.Fatalf("V2 declares edge = %#v, want legacy %#v", pluginEdge, legacyEdge)
	}
}

func pluginGraphNode(c *Consumer, kind, name string) (graphstream.Node, bool) {
	for _, nodes := range c.Owners {
		for _, node := range nodes {
			if node.Kind == kind && node.Name == name {
				return node, true
			}
		}
	}
	return graphstream.Node{}, false
}

func assertPublishedDeclaresEdge(t *testing.T, c *Consumer, from, want graphstream.Node) {
	t.Helper()
	for owner, nodes := range c.Owners {
		for _, node := range nodes {
			if node.ID != from.ID {
				continue
			}
			for _, edge := range c.Edges[owner] {
				if edge.FromID == from.ID && edge.Kind == facts.RelDeclares && edge.TargetName == want.Name && edge.TargetID == want.ID && edge.Resolution == graphstream.ResResolved {
					return
				}
			}
		}
	}
	t.Fatalf("missing resolved legacy declares edge %s -> %s", from.Name, want.Name)
}

func TestGoFSMVocabularyRejectsInvalidFSMNodeWithV1Validation(t *testing.T) {
	p := analyzerplugin.Loaded{Manifest: analyzerplugin.Manifest{API: analyzerplugin.GoAPIVersion, Name: "product-fsm", Vocabularies: []string{analyzerplugin.FSMVocabularyV1}, Claims: analyzerplugin.Claims{Machines: []string{"product"}}, OwnerDomain: []string{"src/**"}}}
	u := analyzerplugin.UnitDecl{ID: "model:product", Kind: "model"}
	bad := analyzerplugin.UnitResult{Unit: u.ID, Owners: map[string]analyzerplugin.OwnerResult{"src/machine.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "other/state:idle"}}}}}
	if err := analyzerplugin.ValidateResult(p, u, bad); err == nil || !strings.Contains(err.Error(), "claimed machine") {
		t.Fatalf("V2 FSM vocabulary accepted invalid V1 FSM identity: %v", err)
	}
}

func TestAnalyzerPluginParsedFileCensusSurvivesSummaryJSON(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/machine.ts": "export function dispatch() {}\n"})
	registration := goFSMAnalyzerPluginFixture(t, root)
	eng := testEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	opts := Options{StateDir: filepath.Join(root, ".enola", "summary-state"), AuthoritativeFiles: true, AllowRepoPlugins: []string{"product-fsm"}}
	result, err := RunSummary(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalyzerPlugins.ParsedFiles != 5 || result.Facts != nil {
		t.Fatalf("summary result = %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatal(err)
	}
	stats, ok := summary["AnalyzerPlugins"].(map[string]any)
	if !ok || stats["ParsedFiles"] != float64(5) {
		t.Fatalf("summary JSON omitted plugin parsed files: %s", encoded)
	}
	noop, err := RunSummary(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if noop.AnalyzerPlugins.ParsedFiles != 0 || noop.AnalyzerPlugins.UnitsReused == 0 {
		t.Fatalf("summary no-op did not reuse units without census: %+v", noop.AnalyzerPlugins)
	}
}

func TestGoV2WithoutFSMVocabularyNamespacesFSMNamedKinds(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/machine.ts": "export function dispatch() {}\n"})
	registration := goNonFSMVocabularyAnalyzerPluginFixture(t, root)
	eng := testEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	run, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: filepath.Join(root, ".enola", "generic-fsm-names-state"), AuthoritativeFiles: true, AllowRepoPlugins: []string{"generic-fsm-names"}})
	if err != nil {
		t.Fatalf("generic V2 fixture was rejected instead of validated generically: %v", err)
	}
	for name, wantKind := range map[string]string{
		"product/state:generic": "plugin:generic-fsm-names:fsm_state",
		"product/coverage":      "plugin:generic-fsm-names:extraction",
	} {
		fact, found := factByName(run.Facts, name)
		if !found || fact.Kind != wantKind {
			t.Fatalf("generic V2 fact %q = %#v, found=%v; want kind %q", name, fact, found, wantKind)
		}
		props, _ := fact.Props["plugin_properties"].(map[string]any)
		owned, _ := props["generic-fsm-names"].(map[string]any)
		if len(owned) == 0 {
			t.Fatalf("generic V2 fact %q lost nested plugin properties: %#v", name, fact.Props)
		}
	}
}

func TestGoFSMVocabularyV2IdentityRejectsSameHostFactAcrossOwners(t *testing.T) {
	s := &session{opts: Options{analyzerPlugins: []analyzerplugin.Loaded{{Manifest: analyzerplugin.Manifest{API: analyzerplugin.GoAPIVersion, Name: "product-fsm", Vocabularies: []string{analyzerplugin.FSMVocabularyV1}}}}}}
	records := map[string]analyzerplugin.PluginRecord{"product-fsm": {Units: map[string]analyzerplugin.UnitRecord{"fixture": {Owners: map[string]analyzerplugin.OwnerResult{
		"src/a.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "product/state:idle"}}},
		"src/b.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: "product/state:idle"}}},
	}}}}}
	err := s.collectPluginContributions(records, map[string]bool{"src/a.ts": true, "src/b.ts": true})
	if err == nil || !strings.Contains(err.Error(), "stable fact identity fsm_state/product/state:idle") || !strings.Contains(err.Error(), "multiple owners") {
		t.Fatalf("same V2 FSM identity across owners was not rejected as documented: %v", err)
	}
}

func TestPluginOwnerResultFactsKeepsFSMVocabularyResolutionCandidatesUnnamespaced(t *testing.T) {
	owners := func(name string) map[string]analyzerplugin.OwnerResult {
		return map[string]analyzerplugin.OwnerResult{
			"src/machine.ts": {Nodes: []analyzerplugin.Node{{Kind: facts.KindFSMState, Name: name}}},
		}
	}
	previous := pluginOwnerResultFacts("product-fsm", analyzerplugin.GoAPIVersion, true, owners("product/state:idle"))
	next := pluginOwnerResultFacts("product-fsm", analyzerplugin.GoAPIVersion, true, owners("product/state:ready"))
	for _, fact := range append(previous, next...) {
		if fact.Kind != facts.KindFSMState {
			t.Fatalf("FSM vocabulary resolution candidate was namespaced: %#v", fact)
		}
	}
	changed := changedResolutionCandidateNames(previous, next)
	for _, name := range []string{"product/state:idle", "product/state:ready"} {
		if !changed[name] {
			t.Fatalf("FSM vocabulary resolution delta omitted unnamespaced candidate %q: %#v", name, changed)
		}
	}
}

func TestOpenSessionRejectsPluginClaimCollisionWithLegacyStateMachine(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/machine.ts": "export function dispatch() {}\n"})
	registration := goFSMAnalyzerPluginFixture(t, root)
	eng := testEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	eng.Config().StateMachines = []fsm.Spec{{ID: "product"}}
	_, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: filepath.Join(root, ".enola", "collision-state"), AuthoritativeFiles: true, AllowRepoPlugins: []string{"product-fsm"}})
	if err == nil || !strings.Contains(err.Error(), "configuration conflict") || !strings.Contains(err.Error(), "legacy state_machines") {
		t.Fatalf("OpenSession accepted legacy/plugin machine collision or returned an unclear error: %v", err)
	}
}

func TestResidentReloadRejectsPluginClaimCollisionAddedAfterOpen(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/machine.ts": "export function dispatch() {}\n"})
	registration := goFSMAnalyzerPluginFixture(t, root)
	eng := testEngine(t, root)
	eng.Config().AnalyzerPlugins = []analyzerplugin.Config{registration}
	r, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: filepath.Join(root, ".enola", "resident-collision-state"), AuthoritativeFiles: true, AllowRepoPlugins: []string{"product-fsm"}})
	if err != nil {
		t.Fatalf("open without legacy collision: %v", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	eng.Config().StateMachines = []fsm.Spec{{ID: "product"}}
	if _, err := r.reconcile(context.Background(), false); err == nil || !strings.Contains(err.Error(), "configuration conflict") {
		t.Fatalf("resident reload accepted newly colliding legacy state machine: %v", err)
	}
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
