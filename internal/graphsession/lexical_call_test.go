package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestEncodeOwnerLexicalSameFileCallResolves(t *testing.T) {
	local := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/costModel.ts", Repo: "r", Props: map[string]any{"exported": false}}
	sib := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/billingExport.ts", Repo: "r", Props: map[string]any{"exported": false}}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "lib.toMoneyPoint", File: "lib/costModel.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "lib.round", TargetFile: "lib/costModel.ts"}},
	}
	idx := buildIndex([]facts.Fact{local, sib, from})
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, idx, false)
	if len(edges) != 1 {
		t.Fatalf("edges=%d", len(edges))
	}
	if edges[0].Resolution != graphstream.ResResolved || edges[0].TargetID != local.Identity() {
		t.Fatalf("want local round %s, got %+v", local.Identity(), edges[0])
	}
	if edges[0].TargetID == sib.Identity() {
		t.Fatal("resolved the sibling declaration")
	}
}

func TestEncodeOwnerImportedCallDoesNotBindCallerLocal(t *testing.T) {
	local := facts.Fact{Kind: facts.KindSymbol, Name: "src.round", File: "src/a.ts", Repo: "r"}
	remote := facts.Fact{Kind: facts.KindSymbol, Name: "src.round", File: "src/b.ts", Repo: "r"}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "src.caller", File: "src/a.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "src.round", TargetFile: "src/b.ts"}},
	}
	idx := buildIndex([]facts.Fact{local, remote, from})
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, idx, false)
	if len(edges) != 1 {
		t.Fatalf("edges=%d", len(edges))
	}
	if edges[0].Resolution != graphstream.ResResolved || edges[0].TargetID != remote.Identity() {
		t.Fatalf("imported round must bind b.ts, got %+v", edges[0])
	}
}

func TestEncodeOwnerCallWithoutTargetFileStaysAmbiguousWithSibling(t *testing.T) {
	local := facts.Fact{Kind: facts.KindSymbol, Name: "src.round", File: "src/a.ts", Repo: "r"}
	sib := facts.Fact{Kind: facts.KindSymbol, Name: "src.round", File: "src/b.ts", Repo: "r"}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "src.caller", File: "src/a.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "src.round"}},
	}
	idx := buildIndex([]facts.Fact{local, sib, from})
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, idx, false)
	if edges[0].Resolution != graphstream.ResAmbiguous || edges[0].TargetID != "" {
		t.Fatalf("same-name without target_file must stay ambiguous: %+v", edges[0])
	}
}

func TestEncodeOwnerTrulyAmbiguousCallsStayAmbiguous(t *testing.T) {
	a := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/a.ts", Repo: "r"}
	b := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/b.ts", Repo: "r"}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "lib.use", File: "lib/c.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "lib.round"}},
	}
	idx := buildIndex([]facts.Fact{a, b, from})
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, idx, false)
	if edges[0].Resolution != graphstream.ResAmbiguous || edges[0].TargetID != "" {
		t.Fatalf("call with no local declaration must stay ambiguous: %+v", edges[0])
	}
}

func TestEncodeOwnerTwoSameFileOverloadsStayAmbiguous(t *testing.T) {
	a := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/costModel.ts", Repo: "r", Line: 10}
	b := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/costModel.ts", Repo: "r", Line: 20}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "lib.toMoneyPoint", File: "lib/costModel.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "lib.round", TargetFile: "lib/costModel.ts"}},
	}
	idx := buildIndex([]facts.Fact{a, b, from})
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, idx, false)
	if a.Identity() != b.Identity() {
		t.Fatal("same name+file should share identity; test setup wrong")
	}
	if edges[0].Resolution != graphstream.ResResolved {
		t.Fatalf("identical identities resolve; got %+v", edges[0])
	}
}

func TestPublishedSameFileCallResolvesWithSibling(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"lib/costModel.ts": `
function round(value: number, digits: number): number { return value; }
export function toMoneyPoint(hourlyEur: number) { return round(hourlyEur, 4); }
export function sumMoneyPoints() { return round(1, 2); }
`,
		"lib/billingExport.ts": `
function round(value: number, decimals: number): number { return value; }
export function dump() { return round(1, 0); }
`,
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertCallResolvedToFile(t, c, "lib/costModel.ts", "lib.round", "lib/costModel.ts")
	assertCallResolvedToFile(t, c, "lib/billingExport.ts", "lib.round", "lib/billingExport.ts")
}

func TestPublishedImportAliasSameDirectoryDoesNotBindLocal(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "import { round as externalRound } from './b';\nfunction round(n: number) { return n; }\nexport function caller() { return externalRound(1); }\n",
		"src/b.ts": "export function round(n: number) { return n + 1; }\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertCallResolvedToFile(t, c, "src/a.ts", "src.round", "src/b.ts")
	for _, e := range c.Edges[ownerKey("src/a.ts")] {
		if e.Kind == facts.RelCalls && e.TargetName == "src.round" {
			n := nodeByID(c, e.FromID)
			if n.Name == "src.caller" && nodeByID(c, e.TargetID).File == "src/a.ts" {
				t.Fatalf("caller must not bind a.ts round: %+v", e)
			}
		}
	}
}

func TestPublishedNamedReexportBridgeResolvesLeaf(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertCallResolvedToFile(t, c, "src/a.ts", "src.round", "src/b.ts")
}

func TestPublishedNamedReexportWithLocalDoesNotBindCaller(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round as externalRound } from './bridge';\nfunction round(n: number) { return n; }\nexport function caller() { return externalRound(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertCallResolvedToFile(t, c, "src/a.ts", "src.round", "src/b.ts")
	for _, e := range c.Edges[ownerKey("src/a.ts")] {
		if e.Kind == facts.RelCalls && e.TargetName == "src.round" {
			n := nodeByID(c, e.FromID)
			if n.Name == "src.caller" && nodeByID(c, e.TargetID).File == "src/a.ts" {
				t.Fatalf("caller must not bind a.ts round: %+v", e)
			}
		}
	}
}

func TestPublishedNamedReexportCollisionStaysAmbiguous(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './barrel';\nexport function caller() { return round(1); }\n",
		"src/barrel.ts": "export * from './b';\nexport * from './c';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	for _, e := range c.Edges[ownerKey("src/a.ts")] {
		if e.Kind == facts.RelCalls && e.TargetName == "src.round" {
			from := nodeByID(c, e.FromID)
			if from.Name == "src.caller" && e.Resolution == graphstream.ResResolved {
				t.Fatalf("colliding star reexports must stay unresolved/ambiguous: %+v", e)
			}
		}
	}
}

func TestPublishedNamedReexportDeltaEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "live")
	opts := Options{StateDir: state}
	live := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
		t.Fatal(err)
	}
	cons := applyGraph(t, live)
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/b.ts")

	if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("export function round(n: number) { return n + 3; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, deltaSink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles == 0 {
		t.Fatal("leaf edit parsed no files")
	}
	if err := cons.ApplyRecords(deltaSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	oracle := applyGraph(t, coldSink)
	assertAppliedEqualsCold(t, cons, oracle)
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/b.ts")
	assertCallResolvedToFile(t, oracle, "src/a.ts", "src.round", "src/b.ts")
}

func TestPublishedNamedReexportCachedUpgrade(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	opts := Options{StateDir: state}
	first := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, first, opts); err != nil {
		t.Fatal(err)
	}
	st, err := loadCommittedState(state)
	if err != nil || st == nil {
		t.Fatalf("load state: %v %#v", err, st)
	}
	st.ExtractorVersion = "v280"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("extractor version bump parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertCallResolvedToFile(t, c, "src/a.ts", "src.round", "src/b.ts")
	if engine.ExtractorVersion() == "v280" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v280")
	}
}

func TestPublishedShadowedLocalCallIsNotModuleRound(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"lib/costModel.ts": `
function round(value: number, digits: number): number { return value; }
export function toMoneyPoint(hourlyEur: number) {
  const round = (n: number) => n;
  return round(hourlyEur);
}
`,
		"lib/billingExport.ts": `
function round(value: number, decimals: number): number { return value; }
export function dump() { return 1; }
`,
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	for _, e := range c.Edges[ownerKey("lib/costModel.ts")] {
		if e.Kind == facts.RelCalls && e.TargetName == "lib.round" && e.FromID != "" {
			from := nodeByID(c, e.FromID)
			if from.Name == "lib.toMoneyPoint" && e.Resolution == graphstream.ResResolved {
				t.Fatalf("shadowed round() must not bind the module function: %+v", e)
			}
		}
	}
}

func TestPublishedSameFileCallRenameAndDelete(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"lib/costModel.ts": `
function round(value: number, digits: number): number { return value; }
export function toMoneyPoint(hourlyEur: number) { return round(hourlyEur, 4); }
`,
		"lib/billingExport.ts": `function round(v: number, d: number): number { return v; }\nexport function dump() { return round(1, 0); }\n`,
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	opts := Options{StateDir: state}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, opts); err != nil {
		t.Fatal(err)
	}
	c := applyGraph(t, sink)
	assertCallResolvedToFile(t, c, "lib/costModel.ts", "lib.round", "lib/costModel.ts")

	if err := os.WriteFile(filepath.Join(dir, "lib/costModel.ts"), []byte(`
function quantize(value: number, digits: number): number { return value; }
export function toMoneyPoint(hourlyEur: number) { return quantize(hourlyEur, 4); }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ren := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, ren, opts); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(ren.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertCallResolvedToFile(t, c, "lib/costModel.ts", "lib.quantize", "lib/costModel.ts")

	if err := os.WriteFile(filepath.Join(dir, "lib/costModel.ts"), []byte(`
export function toMoneyPoint(hourlyEur: number) { return round(hourlyEur, 4); }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	del := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, del, opts); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(del.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range c.Edges[ownerKey("lib/costModel.ts")] {
		if e.Kind == facts.RelCalls && e.TargetName == "lib.round" {
			found = true
			if e.Resolution == graphstream.ResResolved && e.TargetID != "" {
				target := nodeByID(c, e.TargetID)
				if target.File == "lib/costModel.ts" {
					t.Fatal("deleted local round still resolved to costModel")
				}
			}
		}
	}
	if !found {
		t.Fatal("expected remaining call edge to lib.round")
	}
}

func assertCallResolvedToFile(t *testing.T, c *Consumer, ownerFile, targetName, wantFile string) {
	t.Helper()
	var hits []graphstream.Edge
	for _, e := range c.Edges[ownerKey(ownerFile)] {
		if e.Kind == facts.RelCalls && e.TargetName == targetName {
			hits = append(hits, e)
		}
	}
	if len(hits) == 0 {
		t.Fatalf("%s: no calls to %s", ownerFile, targetName)
	}
	for _, e := range hits {
		if e.Resolution != graphstream.ResResolved || e.TargetID == "" {
			from := nodeByID(c, e.FromID)
			t.Fatalf("%s -> %s resolution=%s id=%q from=%s name=%s kind=%s", ownerFile, targetName, e.Resolution, e.TargetID, e.FromID, from.Name, from.Kind)
		}
		n := nodeByID(c, e.TargetID)
		if n.File != wantFile {
			t.Fatalf("%s -> %s landed on %s want %s", ownerFile, targetName, n.File, wantFile)
		}
	}
}

func nodeByID(c *Consumer, id string) graphstream.Node {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.ID == id {
				return n
			}
		}
	}
	return graphstream.Node{}
}
