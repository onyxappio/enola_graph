package graphsession

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/analyzerplugin"
	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/fsm"
	"github.com/enola-labs/enola/internal/graphstream"
)

// TestLegacyAndV2FSMFixturePublishEquivalentMachineGraph is deliberately
// independent of mergeAnalyzerPluginAnchors.  It drives a tiny rule-table
// machine through the legacy state_machines path and the Go V2 fixture through
// separate sessions, then compares their published FSM subgraphs.  The nearby
// subtests document the nested-declaration owner selection contract used by
// the V2 anchor path: exact source line wins; ambiguous and out-of-span anchors
// are not attached.
func TestLegacyAndV2FSMFixturePublishEquivalentMachineGraph(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"impl.ts": "export function defineMachine(value: unknown) { return value; }\n",
		"src/machine.ts": `import { defineMachine } from '../impl';
type ProductState = 'idle';
type ProductEvent = { type: 'start' };
type ProductCommand = { type: 'start' };
export function createRegistration() {
  return { definition: defineMachine({
    id: 'product', stateTags: ['idle'], eventTags: ['start'],
    initialState: ProductState.idle(),
    rules: [{ id: 'idle-start', from: 'idle', on: 'start', to: 'idle', reduce: (s, e) => ({ next: s, commands: [ProductCommand.start()] }) }],
  }), commandTags: ['start'] };
}
`,
	})

	legacy := fsmFixtureEngine(t, root, []fsm.Spec{{
		ID: "product", Adapter: fsm.AdapterRuleTable, File: "src/machine.ts",
		Factory: fsm.SymbolRef{Module: "impl.ts", Export: "defineMachine"}, Registration: "createRegistration",
		StateType: "ProductState", EventType: "ProductEvent", CommandType: "ProductCommand",
	}}, nil)
	legacyGraph := publishedFSMFixtureGraph(t, legacy, root, filepath.Join(root, ".enola", "legacy-fixture"), Options{AuthoritativeFiles: true})

	v2 := fsmFixtureEngine(t, root, nil, []analyzerplugin.Config{goFSMAnalyzerPluginFixture(t, root)})
	v2Graph := publishedFSMFixtureGraph(t, v2, root, filepath.Join(root, ".enola", "v2-fixture"), Options{
		AuthoritativeFiles: true, AllowRepoPlugins: []string{"product-fsm"},
	})
	if got, want := strings.Join(v2Graph, "\n"), strings.Join(legacyGraph, "\n"); got != want {
		t.Fatalf("legacy and V2 published FSM graph differ\nV2-only / legacy-only:\n%s", publishedGraphDiff(v2Graph, legacyGraph))
	}

	t.Run("nested declaration owner selection", func(t *testing.T) {
		helper := facts.Fact{Kind: facts.KindSymbol, Name: "src.Container.dispatch", File: "src/machine.ts", Line: 10, EndLine: 30,
			Props: map[string]any{"language": "typescript", "symbol_kind": facts.SymbolFunc, "fsm_source_binding": "tree_sitter_nested_declaration"}}
		anchor := func(line int) facts.Fact {
			return facts.Fact{Kind: facts.KindSymbol, Name: "dispatch", File: helper.File, Line: line}
		}
		exact := helper
		exact.Line, exact.EndLine = 15, 25
		if got := pluginAnchorOwner([]facts.Fact{helper, exact}, anchor(15), helper.Name); got != 1 {
			t.Fatalf("exact-line preference owner=%d, want 1", got)
		}
		if got := pluginAnchorOwner([]facts.Fact{helper}, anchor(31), helper.Name); got != -1 {
			t.Fatalf("out-of-span owner=%d, want -1", got)
		}
		other := helper
		other.Line, other.EndLine = 40, 50
		if got := pluginAnchorOwner([]facts.Fact{helper, other}, anchor(0), helper.Name); got != -1 {
			t.Fatalf("ambiguous owner=%d, want -1", got)
		}
	})
	// These use real TypeScript extraction and the session/sink protocol (not
	// synthetic facts): a collision is rejected before Begin, and a helper's
	// retirement on delta publishes the same graph as a fresh cold analysis.
	t.Run("real TypeScript collision is rejected before Begin", TestGoFSMNestedHelperCollisionPublishesNoBegin)
	t.Run("helper retirement delta equals cold", TestGoFSMNestedHelperPublishedGraphRetiresColdEqualsDelta)
}

func fsmFixtureEngine(t *testing.T, root string, machines []fsm.Spec, plugins []analyzerplugin.Config) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo, cfg.Output.Dir = root, ".enola"
	cfg.StateMachines, cfg.AnalyzerPlugins = machines, plugins
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	return eng
}

func publishedFSMFixtureGraph(t *testing.T, eng *engine.Engine, root, state string, opts Options) []string {
	t.Helper()
	opts.StateDir = state
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	consumer := applyGraph(t, sink)
	var rows []string
	for owner, nodes := range consumer.Owners {
		byID := make(map[string]string, len(nodes))
		for _, node := range nodes {
			byID[node.ID] = node.Name
		}
		for _, node := range nodes {
			if !isFixtureFSMKind(node.Kind) {
				continue
			}
			rows = append(rows, "node "+owner+" "+node.Kind+" "+node.Name)
		}
		for _, edge := range consumer.Edges[owner] {
			from := byID[edge.FromID]
			if isFixtureFSMName(from) && isFixtureFSMName(edge.TargetName) && isFixtureFSMRelation(edge.Kind) {
				rows = append(rows, "edge "+owner+" "+from+" "+edge.Kind+" "+edge.TargetName)
			}
		}
	}
	sort.Strings(rows)
	return rows
}

func isFixtureFSMKind(kind string) bool {
	return kind == facts.KindFSMMachine || kind == facts.KindFSMState || kind == facts.KindFSMEvent || kind == facts.KindFSMCommand || kind == facts.KindFSMTransition
}

func isFixtureFSMName(name string) bool {
	return name == "product" || strings.HasPrefix(name, "product/")
}

func isFixtureFSMRelation(kind string) bool {
	return kind == facts.RelDeclares || strings.HasPrefix(kind, "fsm_")
}

func publishedGraphDiff(got, want []string) string {
	left, right := make(map[string]bool, len(got)), make(map[string]bool, len(want))
	for _, row := range got {
		left[row] = true
	}
	for _, row := range want {
		right[row] = true
	}
	var rows []string
	for row := range left {
		if !right[row] {
			rows = append(rows, "V2-only: "+row)
		}
	}
	for row := range right {
		if !left[row] {
			rows = append(rows, "legacy-only: "+row)
		}
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}
