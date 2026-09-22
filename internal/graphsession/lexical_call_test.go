package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestEncodeOwnerLexicalSameFileCallResolves(t *testing.T) {
	local := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/costModel.ts", Repo: "r", Props: map[string]any{"exported": false}}
	sib := facts.Fact{Kind: facts.KindSymbol, Name: "lib.round", File: "lib/billingExport.ts", Repo: "r", Props: map[string]any{"exported": false}}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "lib.toMoneyPoint", File: "lib/costModel.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "lib.round"}},
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
		Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "lib.round"}},
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
			t.Fatalf("%s -> %s resolution=%s id=%q", ownerFile, targetName, e.Resolution, e.TargetID)
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
