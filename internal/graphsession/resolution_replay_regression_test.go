package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/engine"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphinput"
)

// replayRepo is the two-module shape every case below starts from: one module
// declaring a name, one unchanged importer binding it, and one source that
// imports nothing so a widened closure is visible as an extra parse.
func replayRepo(t *testing.T, extra map[string]string) (string, *engine.Engine, *Consumer, Options, func(t *testing.T) (*Result, *Consumer)) {
	t.Helper()
	files := map[string]string{
		"src/a.ts":     "export function a() { return 1; }\n",
		"src/b.ts":     "import { a } from './a';\nexport function b() { return a(); }\n",
		"src/alone.ts": "export function alone() { return 0; }\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	dir := setupTSRepo(t, files)
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, dir, opts, cons)
	run := func(t *testing.T) (*Result, *Consumer) {
		t.Helper()
		res, _ := admissionRun(t, eng, dir, opts, cons)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
		return res, cons
	}
	return dir, eng, cons, opts, run
}

func requireParsed(t *testing.T, res *Result, want int, what string) {
	t.Helper()
	if res.ParsedFiles != want {
		t.Fatalf("%s parsed %d files, want %d: reasons=%+v", what, res.ParsedFiles, want, res.Invalidation)
	}
}

// Adding a name nobody imports moves the declared surface and nothing else. The
// importer's own bytes, specifiers and bindings are all unchanged, and the name
// it does bind still resolves to the same file, so the closure has no reason to
// reparse it.
func TestResolutionReplayUnusedExportAdditionSkipsImporter(t *testing.T) {
	dir, _, cons, _, run := replayRepo(t, nil)

	writeFile(t, dir, "src/a.ts", "export function a() { return 1; }\nexport function added() { return 2; }\n")
	res, _ := run(t)

	requireParsed(t, res, 1, "unused export addition")
	if got := res.Invalidation.ParsedByReason["resolution"]; got != 0 {
		t.Fatalf("unused export addition reparsed %d files for resolution", got)
	}
	// The skip has to leave the edge standing, not merely leave the file alone.
	if targets := resolvedTargetFiles(cons, "src/b.ts"); !targets["src/a.ts"] {
		t.Fatalf("importer lost its binding after the skip: %v", targets)
	}
}

// The same edit twice is a no-op: the second run has nothing to observe and must
// publish nothing at all.
func TestResolutionReplaySkipLeavesATrueNoop(t *testing.T) {
	dir, eng, cons, opts, _ := replayRepo(t, nil)

	writeFile(t, dir, "src/a.ts", "export function a() { return 1; }\nexport function added() { return 2; }\n")
	first, _ := admissionRun(t, eng, dir, opts, cons)
	requireParsed(t, first, 1, "unused export addition")

	second, sink := admissionRun(t, eng, dir, opts, cons)
	assertNoPublication(t, second, sink, first.TargetGeneration, "repeated identical content")
}

// Removing the name the importer binds is the case the name delta exists for:
// src.a leaves the declared surface, the importer's Referenced mentions it, and
// nameDependents takes it back.
func TestResolutionReplayExportRemovalStillReparsesImporter(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, nil)

	writeFile(t, dir, "src/a.ts", "export function other() { return 1; }\n")
	res, cons := run(t)

	if got := res.Invalidation.ParsedByReason["resolution"]; got == 0 {
		t.Fatalf("removing the bound name skipped the importer: %+v", res.Invalidation)
	}
	if targets := resolvedTargetFiles(cons, "src/b.ts"); targets["src/a.ts"] {
		t.Fatalf("importer kept a binding to a name that is gone: %v", targets)
	}
}

// A rename is a removal and an addition at once. The importer has to come back
// for the removal even though the addition alone would not have taken it.
func TestResolutionReplayRenameStillReparsesImporter(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, map[string]string{
		"src/b.ts": "import { a } from './a';\nexport function b() { return a(); }\n",
	})

	writeFile(t, dir, "src/a.ts", "export function renamed() { return 1; }\n")
	res, _ := run(t)

	if got := res.Invalidation.ParsedByReason["resolution"]; got == 0 {
		t.Fatalf("rename skipped the importer: %+v", res.Invalidation)
	}
}

// A barrel publishes names it never mentions. `export * from './a'` and
// `export { a } from './a'` are recorded as the same module-level pair, so
// Referenced cannot distinguish them and nameDependents would not take either.
// nameScopedDependent therefore refuses any re-exporting dependent.
func TestResolutionReplayStarBarrelStillReparsesOnExportAddition(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, map[string]string{
		"src/barrel.ts": "export * from './a';\n",
		"src/use.ts":    "import { a } from './barrel';\nexport function u() { return a(); }\n",
	})

	writeFile(t, dir, "src/a.ts", "export function a() { return 1; }\nexport function added() { return 2; }\n")
	res, _ := run(t)

	if got := res.Invalidation.ParsedByReason["resolution"]; got == 0 {
		t.Fatalf("a star barrel was skipped on an export addition: %+v", res.Invalidation)
	}
}

// A named re-export chain binds through bytes no specifier names. Renaming at
// the far end has to reach the consumer through the side-read machinery, which
// this narrowing must not disturb.
func TestResolutionReplayNamedReexportChainRebinds(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, map[string]string{
		"src/leaf.ts":   "export function leaf() { return 3; }\n",
		"src/barrel.ts": "export { leaf } from './leaf';\n",
		"src/use.ts":    "import { leaf } from './barrel';\nexport function u() { return leaf(); }\n",
	})

	writeFile(t, dir, "src/leaf.ts", "export function renamedLeaf() { return 3; }\n")
	res, cons := run(t)

	if got := res.Invalidation.ParsedByReason["resolution"]; got == 0 {
		t.Fatalf("the re-export chain did not rebind: %+v", res.Invalidation)
	}
	if targets := resolvedTargetFiles(cons, "src/use.ts"); targets["src/leaf.ts"] {
		t.Fatalf("consumer still binds the renamed leaf: %v", targets)
	}
}

// A dependency that gains an import of its own moved its bindings, not just its
// names, so the old rule stands for every importer.
func TestResolutionReplayMovedBindingStillTakesImporters(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, map[string]string{
		"src/dep.ts": "export function dep() { return 7; }\n",
	})

	writeFile(t, dir, "src/a.ts", "import { dep } from './dep';\nexport function a() { return dep(); }\n")
	res, _ := run(t)

	if got := res.Invalidation.ParsedByReason["resolution"]; got == 0 {
		t.Fatalf("a moved binding surface skipped its importers: %+v", res.Invalidation)
	}
}

// Import cycles are the shape a one-hop closure can loop on. The skip must not
// change that: the run terminates and equals cold.
func TestResolutionReplayCycleTerminatesAndEqualsCold(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, map[string]string{
		"src/a.ts": "import { b } from './b';\nexport function a() { return b(); }\n",
		"src/b.ts": "import { a } from './a';\nexport function b() { return 1; }\n",
	})

	writeFile(t, dir, "src/a.ts", "import { b } from './b';\nexport function a() { return b(); }\nexport function added() { return 2; }\n")
	res, _ := run(t)

	if res.ParsedFiles == 0 {
		t.Fatalf("the cycle edit parsed nothing: %+v", res.Invalidation)
	}
}

// nameOnlySurfaceShift is the whole of the narrowing, so it is pinned directly:
// every refusal below is a route through which a consumer could move without any
// name moving.
func TestNameOnlySurfaceShiftRefusesEverythingButNames(t *testing.T) {
	base := func() *tsextractor.FileRecord {
		return &tsextractor.FileRecord{
			ParseKind:      "ts",
			ImportComplete: true,
			ImportSpecs:    []string{"src/dep"},
			ResolvedFiles:  []string{"src/dep.ts"},
			Declared:       []string{"src.a"},
		}
	}
	if !nameOnlySurfaceShift(base(), func() *tsextractor.FileRecord {
		r := base()
		r.Declared = []string{"src.a", "src.added"}
		return r
	}()) {
		t.Fatal("a declared-name addition is exactly the shift this allows")
	}

	for name, mutate := range map[string]func(*tsextractor.FileRecord){
		"moved resolved files": func(r *tsextractor.FileRecord) { r.ResolvedFiles = []string{"src/other.ts"} },
		"moved import specs":   func(r *tsextractor.FileRecord) { r.ImportSpecs = []string{"src/other"} },
		"moved unresolved":     func(r *tsextractor.FileRecord) { r.UnresolvedSpecs = []string{"missing"} },
		"gained a reexport":    func(r *tsextractor.FileRecord) { r.Reexports = []string{"src -> src"} },
		"gained auto imports":  func(r *tsextractor.FileRecord) { r.AutoImportDirs = []string{"composables"} },
		"gained a router":      func(r *tsextractor.FileRecord) { r.Router = &tsextractor.RouterDTO{} },
		"became graphql":       func(r *tsextractor.FileRecord) { r.GraphQLServer = true },
		"gained sdl":           func(r *tsextractor.FileRecord) { r.GraphQLSDL = []string{"type Q { a: Int }"} },
		"lost import complete": func(r *tsextractor.FileRecord) { r.ImportComplete = false },
		"has a foreign kind":   func(r *tsextractor.FileRecord) { r.ParseKind = "" },
		"is an unknown kind":   func(r *tsextractor.FileRecord) { r.ParseKind = "python" },
	} {
		neu := base()
		neu.Declared = []string{"src.a", "src.added"}
		mutate(neu)
		if nameOnlySurfaceShift(base(), neu) {
			t.Fatalf("%s must keep the old rule", name)
		}
		old := base()
		mutate(old)
		if nameOnlySurfaceShift(old, base()) {
			t.Fatalf("%s must keep the old rule when it is the prior record", name)
		}
	}

	if nameOnlySurfaceShift(nil, base()) || nameOnlySurfaceShift(base(), nil) {
		t.Fatal("a missing record proves nothing")
	}
}

// nameScopedDependent is the dependent-side half of the same rule.
func TestNameScopedDependentRefusesForwardingRecords(t *testing.T) {
	ok := &tsextractor.FileRecord{ParseKind: "ts", ImportComplete: true, Referenced: []string{"src.a"}}
	if !nameScopedDependent(ok) {
		t.Fatal("an ordinary importer is name scoped")
	}
	barrel := *ok
	barrel.Reexports = []string{"src -> src"}
	if nameScopedDependent(&barrel) {
		t.Fatal("a record that forwards names cannot be name scoped")
	}
	if nameScopedDependent(nil) {
		t.Fatal("a missing record proves nothing")
	}
}

// rebindableConsumer is the third predicate: not "does this file have to be
// taken" but "does being taken mean reading it again". Everything it refuses
// keeps the old behaviour of a reparse.
func TestRebindableConsumerRefusesEverythingNotLocal(t *testing.T) {
	base := func() *tsextractor.FileRecord {
		return &tsextractor.FileRecord{
			ParseKind:      "ts",
			ImportComplete: true,
			NuxtScope:      "-",
			ImportSpecs:    []string{"./a"},
			ResolvedFiles:  []string{"src/a.ts"},
			SideReads:      []string{"src/origin.ts"},
			Referenced:     []string{"src.future"},
		}
	}
	moved := map[string]bool{"src/provider.ts": true}
	if !rebindableConsumer(base(), moved) {
		t.Fatal("a plain importer that holds no edge into anything that moved rebinds")
	}

	for name, mutate := range map[string]func(*tsextractor.FileRecord){
		"imports a file that moved":    func(r *tsextractor.FileRecord) { r.ResolvedFiles = []string{"src/provider.ts"} },
		"side reads a file that moved": func(r *tsextractor.FileRecord) { r.SideReads = []string{"src/provider.ts"} },
		"has an unresolved specifier":  func(r *tsextractor.FileRecord) { r.UnresolvedSpecs = []string{"./maybe"} },
		"forwards names":               func(r *tsextractor.FileRecord) { r.Reexports = []string{"src -> src"} },
		"is inside a nuxt app":         func(r *tsextractor.FileRecord) { r.NuxtScope = "apps/web" },
		"is the root nuxt app":         func(r *tsextractor.FileRecord) { r.NuxtScope = "." },
		"has no recorded nuxt scope":   func(r *tsextractor.FileRecord) { r.NuxtScope = "" },
		"declares auto import dirs":    func(r *tsextractor.FileRecord) { r.AutoImportDirs = []string{"composables"} },
		"is a router":                  func(r *tsextractor.FileRecord) { r.Router = &tsextractor.RouterDTO{} },
		"is a template kind":           func(r *tsextractor.FileRecord) { r.ParseKind = "vue" },
		"is a foreign record":          func(r *tsextractor.FileRecord) { r.ParseKind = "" },
		"lost import complete":         func(r *tsextractor.FileRecord) { r.ImportComplete = false },
		"serves graphql":               func(r *tsextractor.FileRecord) { r.GraphQLServer = true },
	} {
		rec := base()
		mutate(rec)
		if rebindableConsumer(rec, moved) {
			t.Fatalf("%s must still be reparsed", name)
		}
	}

	if rebindableConsumer(nil, moved) {
		t.Fatal("a missing record proves nothing")
	}
}

// The end-to-end half of the same refusal: a consumer that references the new
// global name AND imports the file that declared it has an import edge into
// something that moved, so its facts can carry that file's provenance and it is
// read again rather than replayed.
func TestResolutionReplayGlobalNameImporterOfChangedFileStillReparses(t *testing.T) {
	dir, _, _, _, run := replayRepo(t, map[string]string{
		"src/global.ts": "import { a } from './a';\nexport function g() { return a() + future(); }\n",
	})
	writeFile(t, dir, "src/a.ts", "export function a() { return 1; }\nexport function future() { return 2; }\n")
	res, _ := run(t)
	requireParsed(t, res, 2, "a global reference whose declaring file is also imported")
}

// The Nuxt refusal against real records. Both repositories get the same edit in
// the same directory: a second exported name appears next to one a sibling never
// imported. Outside a Nuxt application the sibling records scope "-" and
// replays; inside one it records the app and is refused, and the run parses the
// whole app anyway through the existing Nuxt fallback - which is the point, the
// predicate must not be the thing that lets a bare call escape it.
func TestResolutionReplayNuxtBareCallConsumerStillReparses(t *testing.T) {
	sources := map[string]string{
		"composables/known.ts": "export function useKnown() { return 1; }\n",
		"composables/page.ts":  "export function page() { return useFuture(); }\n",
	}
	grown := "export function useKnown() { return 1; }\nexport function useFuture() { return 2; }\n"

	edit := func(t *testing.T, files map[string]string) (*Result, *tsextractor.FileRecord) {
		t.Helper()
		dir := setupTSRepo(t, files)
		eng := admissionEngine(t, dir, graphinput.Options{})
		opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
		cons := NewConsumer()
		admissionRun(t, eng, dir, opts, cons)
		writeFile(t, dir, "composables/known.ts", grown)
		res, _ := admissionRun(t, eng, dir, opts, cons)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
		st, err := readStateFile(statePath(opts.StateDir))
		if err != nil {
			t.Fatal(err)
		}
		f := st.Files["composables/page.ts"]
		if f == nil || f.TS == nil {
			t.Fatalf("no record for the bare-call consumer: %v", f)
		}
		return res, f.TS
	}

	moved := map[string]bool{"composables/known.ts": true}

	plain := map[string]string{}
	for k, v := range sources {
		plain[k] = v
	}
	res, rec := edit(t, plain)
	requireParsed(t, res, 1, "a bare call outside any Nuxt application")
	if rec.NuxtScope != "-" {
		t.Fatalf("a file outside any Nuxt application records scope %q, want \"-\"", rec.NuxtScope)
	}
	if !rebindableConsumer(rec, moved) {
		t.Fatal("the recorded non-Nuxt consumer is the case this replays")
	}

	nuxt := map[string]string{
		"package.json":   `{"name":"app","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0"}}`,
		"nuxt.config.ts": `export default defineNuxtConfig({})`,
	}
	for k, v := range sources {
		nuxt[k] = v
	}
	res, rec = edit(t, nuxt)
	if rec.NuxtScope == "-" {
		t.Fatal("a file inside a Nuxt application must record its app, not \"-\"")
	}
	if rebindableConsumer(rec, moved) {
		t.Fatalf("the recorded Nuxt consumer (scope %q) must be read again", rec.NuxtScope)
	}
	requireParsed(t, res, 3, "a bare call inside a Nuxt application")
}

// A candidate qualifies against the dirt as it stands when the name delta takes
// it, and a later hop can move that ground: here the barrel the consumer
// imports is parsed on the second hop, so by the third the consumer holds an
// edge into something that moved and is read again like any other dependent.
// The same consumer with no import at all keeps its replay, which is what makes
// the extra parse attributable to the edge rather than to the name.
func TestResolutionReplayLaterHopDirtPromotesCandidateToParse(t *testing.T) {
	run := func(t *testing.T, consumer string) *Result {
		t.Helper()
		dir := setupTSRepo(t, map[string]string{
			"src/prov.ts":   "export function known() { return 1; }\n",
			"src/barrel.ts": "export * from './prov';\n",
			"src/cons.ts":   consumer,
		})
		eng := admissionEngine(t, dir, graphinput.Options{})
		opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
		cons := NewConsumer()
		admissionRun(t, eng, dir, opts, cons)
		writeFile(t, dir, "src/prov.ts", "export function known() { return 1; }\nexport function future() { return 2; }\n")
		res, _ := admissionRun(t, eng, dir, opts, cons)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
		return res
	}

	requireParsed(t, run(t, "import './barrel';\nexport function c() { return future(); }\n"),
		3, "a candidate whose imported barrel is parsed on a later hop")
	requireParsed(t, run(t, "export function c() { return future(); }\n"),
		2, "the same consumer holding no import edge")
}
