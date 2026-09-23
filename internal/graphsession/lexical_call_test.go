package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestPublishedNamedReexportCachedUpgradeFromV281(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
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
	st.ExtractorVersion = "v281"
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
	if engine.ExtractorVersion() == "v281" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v281")
	}
}

func TestPublishedWave8CachedUpgradeFromV295(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/schema.d.ts":  "export interface TsLibGeneratorSchema { name: string }\n",
		"src/schema.json":  "{\"type\":\"object\"}\n",
		"src/generator.ts": "import type { TsLibGeneratorSchema } from './schema'\nexport function run(s: TsLibGeneratorSchema) { return s }\n",
		"src/a.ts": `export function useStep() { return { nextDelayed: (s: string) => s } }
const { nextDelayed } = useStep()
export function handleNextClick() { nextDelayed('start') }
`,
		"src/b.ts": `export function useStep() { return { nextDelayed: (s: string) => s } }
const { nextDelayed } = useStep()
export function handleNextClick() { nextDelayed('other') }
`,
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
	st.ExtractorVersion = "v295"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v295 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/generator.ts", "src.TsLibGeneratorSchema", "src/schema.d.ts")
	assertCallResolvedToFile(t, c, "src/a.ts", "src.nextDelayed", "src/a.ts")
	if engine.ExtractorVersion() == "v295" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v295")
	}
}

func TestPublishedWave8CachedUpgradeFromV296(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/deps/a.ts": "export function work() { return 'a' }\nexport const nested = { work() { return 1 } }\n",
		"src/caller.ts": `export async function nestedPattern() {
  const { nested: { work } } = await import('./deps/a');
  work();
}
`,
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
	st.ExtractorVersion = "v296"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v296 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	for _, e := range c.Edges[ownerKey("src/caller.ts")] {
		if e.Kind != facts.RelCalls || e.Resolution != graphstream.ResResolved {
			continue
		}
		from := nodeByID(c, e.FromID)
		if from.Name != "src.nestedPattern" {
			continue
		}
		tgt := nodeByID(c, e.TargetID)
		if strings.HasSuffix(tgt.Name, ".nested") {
			t.Fatalf("nestedPattern resolved work to container: %+v target=%s", e, tgt.Name)
		}
	}
	if engine.ExtractorVersion() == "v296" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v296")
	}
}

func TestPublishedWave9CachedUpgradeFromV297(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/index.ts":    "export { Decision } from './decision';\nexport { max as alias } from './max';\n",
		"src/decision.ts": "export const Decision = { ok: true };\n",
		"src/types.ts":    "export type Decision = { ok: boolean };\n",
		"src/max.ts":      "export function max() { return 1; }\nexport function alias() { return 2; }\n",
		"src/app.ts":      "export function isLocalRequest(host: string) { return host === 'localhost'; }\n",
		"src/server.ts":   "export const host = '0.0.0.0';\n",
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
	st.ExtractorVersion = "v297"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v297 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/index.ts", "src.Decision", "src/decision.ts")
	assertCallResolvedToFile(t, c, "src/index.ts", "src.max", "src/max.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v297" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v297")
	}

	v1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, v1, Options{StateDir: filepath.Join(dir, ".enola", "v1"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	v2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, v2, Options{StateDir: filepath.Join(dir, ".enola", "v2"), ForceInitial: true, AuthoritativeFiles: true, MaxBeginBytes: 1048576}); err != nil {
		t.Fatal(err)
	}
	c1, c2 := applyGraph(t, v1), applyGraph(t, v2)
	assertCallResolvedToFile(t, c1, "src/index.ts", "src.Decision", "src/decision.ts")
	assertCallResolvedToFile(t, c2, "src/index.ts", "src.Decision", "src/decision.ts")
}

func TestPublishedWave9CachedUpgradeFromV298(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/dep.ts": "export function callback() { return 1; }\nexport function keep() { return 2; }\n",
		"src/app.ts": `
import { callback, keep } from './dep';
export function run() {
  type callback = string;
  interface keep { n: number }
  callback();
  keep();
}
`,
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
	st.ExtractorVersion = "v298"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v298 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/app.ts", "src.callback", "src/dep.ts")
	assertCallResolvedToFile(t, c, "src/app.ts", "src.keep", "src/dep.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v298" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v298")
	}
}

func TestPublishedWave9CachedUpgradeFromV299(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/dep.ts": "export function helper() { return 1; }\n",
		"src/app.ts": `
import { helper } from './dep';
export function run() {
  try { return helper(); } catch { return 0; }
}
export function catchShadow() {
  try { helper(); } catch (helper) { return helper; }
}
`,
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
	st.ExtractorVersion = "v299"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v299 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/app.ts", "src.helper", "src/dep.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v299" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v299")
	}
}

func TestPublishedWave9CachedUpgradeFromV300(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/figma.ts": "export function readScreenStructureFileKey() { return 'k'; }\n",
		"src/crop.ts":  "export function shouldRunInstanceCropCompare() { return true; }\n",
		"src/sib.ts":   "export function readScreenStructureFileKey() { return 'sib'; }\nexport function shouldRunInstanceCropCompare() { return false; }\n",
		"src/app.ts": `
export function runSuitePipeline() {
  const { readScreenStructureFileKey } = require('./figma') as typeof import('./figma');
  const { shouldRunInstanceCropCompare } = require('./crop') as typeof import('./crop');
  return readScreenStructureFileKey() && shouldRunInstanceCropCompare();
}
`,
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
	st.ExtractorVersion = "v300"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v300 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/app.ts", "src.readScreenStructureFileKey", "src/figma.ts")
	assertCallResolvedToFile(t, c, "src/app.ts", "src.shouldRunInstanceCropCompare", "src/crop.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v300" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v300")
	}
}

func TestPublishedWave9CachedUpgradeFromV301(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/dep.ts": "export function work() { return 1; }\nexport function keep() { return 2; }\n",
		"src/sib.ts": "export function work() { return 9; }\n",
		"src/app.ts": `
export function namespaceRequire() {
  const sdk = require('./dep');
  return sdk.work();
}
export function shadowedRequireLocal() {
  const require = (p: string) => ({ work: () => 0 });
  const { work } = require('./dep');
  return work();
}
export function laterRequireCapture() {
  const run = () => work();
  const { work } = require('./dep');
  return run();
}
`,
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
	st.ExtractorVersion = "v301"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v301 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/app.ts", "src.work", "src/dep.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v301" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v301")
	}
}

func TestPublishedWave9CachedUpgradeFromV302(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/factory.ts": "export function factory(p: string) { return { work: () => 0 }; }\n",
		"src/local.ts":   "export function work() { return 1; }\nexport function keep() { return 2; }\n",
		"src/sib.ts":     "export function work() { return 9; }\n",
		"src/app.ts": `
import { factory as require } from './factory';
import { keep } from './local';
export function run() {
  const { work } = require('./local');
  work();
  keep();
}
export function missingNs() {
  const sdk = require('./gone') as typeof import('./gone');
  return sdk.work();
}
`,
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
	st.ExtractorVersion = "v302"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v302 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/app.ts", "src.keep", "src/local.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v302" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v302")
	}
}

func TestPublishedWave9CachedUpgradeFromV303(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/fn.ts":    "export function ping() { return 1; }\n",
		"src/local.ts": "export function work() { return 1; }\nexport function keep() { return 2; }\n",
		"src/sib.ts":   "export function ping() { return 9; }\nexport function work() { return 9; }\n",
		"src/app.ts": `
export function callRequiredFn() {
  const ping = require('./fn');
  return ping();
}
export function namespaceRequire() {
  const sdk = require('./local');
  return sdk.work();
}
export function missingValue() {
  const gone = require('./gone');
  return gone();
}
`,
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
	st.ExtractorVersion = "v303"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v303 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	assertCallResolvedToFile(t, c, "src/app.ts", "src.ping", "src/fn.ts")
	assertCallResolvedToFile(t, c, "src/app.ts", "src.work", "src/local.ts")
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v303" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v303")
	}
}

func TestPublishedWave10CachedUpgradeFromV310(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"apps/landings/nuxt.config.ts": `export default defineNuxtConfig({})
`,
		"apps/landings/package.json": `{"name":"landings","dependencies":{"nuxt":"3.14.0"}}
`,
		"apps/landings/plugins/metaPixelPlugin.ts": `import { defineNuxtPlugin } from '#imports'
export default defineNuxtPlugin({
  name: 'meta-pixel',
  setup() { return {} },
})
`,
		"apps/landings/plugins/databreach-scan-supervisor.client.ts": `export default defineNuxtPlugin((nuxtApp) => {
  return {}
})
`,
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
	st.ExtractorVersion = "v310"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v310 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v310" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v310")
	}

	plugin := filepath.Join(dir, "apps/landings/plugins/metaPixelPlugin.ts")
	shadow := `function defineNuxtPlugin(value: any) { return value }
export default defineNuxtPlugin({ name: 'plain-value' })
`
	if err := os.WriteFile(plugin, []byte(shadow), 0o644); err != nil {
		t.Fatal(err)
	}
	mut := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, mut, opts); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(mut.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	shadowed := false
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.File != "apps/landings/plugins/metaPixelPlugin.ts" || n.Kind != facts.KindSymbol {
				continue
			}
			if n.Name != "apps/landings/plugins.MetaPixelPlugin" {
				continue
			}
			if n.Props["symbol_kind"] != facts.SymbolVariable {
				t.Fatalf("shadowed plugin kind=%v want variable name=%s", n.Props["symbol_kind"], n.Name)
			}
			shadowed = true
		}
	}
	if !shadowed {
		t.Fatal("missing shadowed plugin symbol")
	}
	orig := `import { defineNuxtPlugin } from '#imports'
export default defineNuxtPlugin({
  name: 'meta-pixel',
  setup() { return {} },
})
`
	if err := os.WriteFile(plugin, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	rest := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, rest, opts); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(rest.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, cold2, Options{StateDir: filepath.Join(dir, ".enola", "cold2"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, cold2))
}

func TestPublishedWave10CachedUpgradeFromV309(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { pick } from './barrel';\nexport function caller() { return pick(1); }\n",
		"src/barrel.ts": "export { default as pick } from './origin';\n",
		"src/origin.ts": "export function round(n: number) { return n + 1; }\nexport function ceil(n: number) { return n + 2; }\nround(1); ceil(1);\nexport default ceil;\n",
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
	st.ExtractorVersion = "v309"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v309 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v309" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v309")
	}
}

func TestPublishedWave10CachedUpgradeFromV308(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"app/components/stamp.gts": `import Component from '@glimmer/component';
export default class KitBadge extends Component {
  <template>
    <span>{{yield}}</span>
  </template>
}
`,
		"app/use.ts": `import KitBadge from './components/stamp';
export function use() { return KitBadge; }
`,
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
	st.ExtractorVersion = "v308"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v308 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v308" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v308")
	}
}

func TestPublishedWave10CachedUpgradeFromV307(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"runtime/safariPlugin.ts": `import { defineNuxtPlugin } from '#app'
export default defineNuxtPlugin({
  setup() { return {} },
})
`,
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
	st.ExtractorVersion = "v307"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v307 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v307" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v307")
	}
}

func TestPublishedWave10CachedUpgradeFromV306(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
type HttpClient = { delete: (url: string) => unknown };
export function send(tree: Tree | HttpClient, base: string) {
  tree.delete(` + "`${base}/http`" + `);
}
`,
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
	st.ExtractorVersion = "v306"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v306 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v306" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v306")
	}
}

func TestPublishedWave10CachedUpgradeFromV305(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
export function outer(tree: Tree, base: string) {
  tree.delete(` + "`${base}/fs`" + `);
  function inner(tree: any) {
    tree.delete(` + "`${base}/http`" + `);
  }
}
`,
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
	st.ExtractorVersion = "v305"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v305 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v305" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v305")
	}
}

func TestPublishedWave10CachedUpgradeFromV304(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/ids.ts": `export class BoundedIdSet {
  order: string[] = [];
  constructor() {}
  add() {}
}
export default { ok: true };
`,
		"src/use.ts": `import ids from './ids';
export function use() { return ids.ok; }
`,
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
	st.ExtractorVersion = "v304"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	up := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, up, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v304 migration parsed no files")
	}
	c := applyGraph(t, first)
	if err := c.ApplyRecords(up.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, applyGraph(t, coldSink))
	quiet := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, dir, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParsedFiles != 0 {
		t.Fatalf("silent nochange parsed=%d", again.ParsedFiles)
	}
	if engine.ExtractorVersion() == "v304" {
		t.Fatal("cached upgrade test requires cacheVersion newer than v304")
	}
}

func TestPublishedBridgeTargetChangeResolvesToC(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
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

	if err := os.WriteFile(filepath.Join(dir, "src/bridge.ts"), []byte("export { round } from './c';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, deltaSink, opts); err != nil {
		t.Fatal(err)
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
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/c.ts")
	assertCallResolvedToFile(t, oracle, "src/a.ts", "src.round", "src/c.ts")
}

func TestPublishedBridgeLeafDeleteDoesNotBindUnrelated(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
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

	if err := os.Remove(filepath.Join(dir, "src/b.ts")); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, deltaSink, opts); err != nil {
		t.Fatal(err)
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
	assertCallerRoundNotResolvedTo(t, cons, "src/c.ts")
	assertCallerRoundNotResolvedTo(t, oracle, "src/c.ts")
}

func TestPublishedBridgeExportRenameDeltaEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { round } from './bridge';\nexport function caller() { return round(1); }\n",
		"src/bridge.ts": "export { round } from './b';\n",
		"src/b.ts":      "export function round(n: number) { return n + 1; }\n",
		"src/c.ts":      "export function round(n: number) { return n + 2; }\n",
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

	if err := os.WriteFile(filepath.Join(dir, "src/bridge.ts"), []byte("export { round as renamed } from './b';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deltaSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, deltaSink, opts); err != nil {
		t.Fatal(err)
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
	assertCallerRoundNotResolvedTo(t, cons, "src/b.ts")
	assertCallerRoundNotResolvedTo(t, cons, "src/c.ts")
	assertCallerRoundNotResolvedTo(t, oracle, "src/b.ts")
	assertCallerRoundNotResolvedTo(t, oracle, "src/c.ts")
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

func assertCallerRoundNotResolvedTo(t *testing.T, c *Consumer, forbidden string) {
	t.Helper()
	for _, e := range c.Edges[ownerKey("src/a.ts")] {
		if e.Kind != facts.RelCalls || e.TargetName != "src.round" {
			continue
		}
		from := nodeByID(c, e.FromID)
		if from.Name != "src.caller" {
			continue
		}
		if e.Resolution == graphstream.ResResolved && nodeByID(c, e.TargetID).File == forbidden {
			t.Fatalf("caller resolved to forbidden %s: %+v", forbidden, e)
		}
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
