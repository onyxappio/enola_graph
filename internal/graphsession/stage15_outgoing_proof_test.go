package graphsession

import (
	"bytes"
	"os"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
)

// stage15Record reads one committed TS record out of the state a run left.
func stage15Record(t *testing.T, stateDir, path string) *tsextractor.FileRecord {
	t.Helper()
	st, err := loadCommittedState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	fs := lookupState(st.Files, path)
	if fs == nil || fs.TS == nil {
		t.Fatalf("missing record %s", path)
	}
	return fs.TS
}

// TestStage15ImportPrivateChangeSpareseConsumers is the benefit case, and it is
// the one test here that is RED on unmodified main by design: main takes every
// importer of a file whose outgoing fields moved, so it reparses the consumers
// too. The dependency swaps which helper it imports; the name it imports, every
// name it declares, and every name it exports are untouched, and each consumer
// binds that export inside the dependency itself. Cold equality is still the
// authority - a skip that changed the graph would fail assertAppliedEqualsCold
// before the parse count was ever read.
func TestStage15ImportPrivateChangeSparesConsumers(t *testing.T) {
	for _, authoritative := range []bool{true, false} {
		name := "legacy"
		if authoritative {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"helpers/a.ts": "export function helper() {\n  return 1;\n}\n",
				"helpers/b.ts": "export function helper() {\n  return 2;\n}\n",
				"dep/dep.ts":   "import { helper } from '../helpers/a';\nexport function value() {\n  return helper();\n}\n",
				"use/u1.ts":    "import { value } from '../dep/dep';\nexport const r1 = value();\n",
				"use/u2.ts":    "import { value } from '../dep/dep';\nexport const r2 = value();\n",
				"use/u3.ts":    "import { value } from '../dep/dep';\nexport const r3 = value();\n",
				"use/u4.ts":    "import { value } from '../dep/dep';\nexport const r4 = value();\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative}
			cons := bodyScopeStart(t, eng, root, opts)

			before := stage15Record(t, opts.StateDir, "dep/dep.ts")
			if !before.ExportSurfaceRecorded || !before.ExportSurfaceContextFree {
				t.Fatalf("dependency that binds imports must still carry a context-free surface: recorded=%v contextFree=%v surface=%v",
					before.ExportSurfaceRecorded, before.ExportSurfaceContextFree, before.ExportSurface)
			}
			for _, consumer := range []string{"use/u1.ts", "use/u2.ts", "use/u3.ts", "use/u4.ts"} {
				for _, sr := range stage15Record(t, opts.StateDir, consumer).SideReads {
					if sr == "dep/dep.ts" {
						t.Fatalf("%s bound value locally, so it must record no side read of the dependency", consumer)
					}
				}
			}

			writeFile(t, root, "dep/dep.ts", "import { helper } from '../helpers/b';\nexport function value() {\n  return helper();\n}\n")
			res := bodyScopeDeltaCold(t, eng, root, opts, cons)
			after := stage15Record(t, opts.StateDir, "dep/dep.ts")

			// Logged before any predicate is asserted, so the counter stands on
			// its own: whatever the predicates then say, the receipt shows what
			// the session actually parsed for this edit.
			t.Logf("stage15 benefit counter: parsed_files=%d parsed_by_reason=%v consumers=4", res.ParsedFiles, res.Invalidation.ParsedByReason)

			if !surfaceChanged(before, after) {
				t.Fatal("expected an outgoing surface change")
			}
			if !outgoingOnlySurfaceShift(before, after) {
				t.Fatalf("expected the proof to hold: declared %v -> %v, reexports %v -> %v, surface %v -> %v (recorded %v/%v, contextFree %v/%v)",
					before.Declared, after.Declared, before.Reexports, after.Reexports,
					before.ExportSurface, after.ExportSurface,
					before.ExportSurfaceRecorded, after.ExportSurfaceRecorded,
					before.ExportSurfaceContextFree, after.ExportSurfaceContextFree)
			}
			if res.ParsedFiles != 1 {
				t.Fatalf("only the edited dependency should have been parsed, got %d", res.ParsedFiles)
			}
		})
	}
}

// TestStage15LocalToForwardedRefusesProof is the exact transition root proved
// unsafe, asserted at the predicate rather than at the graph: the new record
// forwards through an import, so it is not context-free and the proof refuses.
// Cold equality is asserted as well, so a regression shows up whichever layer
// breaks first.
func TestStage15LocalToForwardedRefusesProof(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"late/late.ts": "export function work() {\n  return 2;\n}\n",
		"mid/mid.ts":   "function work() {\n  return 1;\n}\nexport { work };\n",
		"use/use.ts":   "import { work } from '../mid/mid';\nexport const result = work();\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)
	before := stage15Record(t, opts.StateDir, "mid/mid.ts")

	writeFile(t, root, "mid/mid.ts", "import { work as imported } from '../late/late';\nfunction work() {\n  return 1;\n}\nexport { imported as work };\n")
	bodyScopeDeltaCold(t, eng, root, opts, cons)
	after := stage15Record(t, opts.StateDir, "mid/mid.ts")

	if !eqStrings(before.Declared, after.Declared) {
		t.Fatalf("fixture no longer holds Declared still: %v -> %v", before.Declared, after.Declared)
	}
	if len(before.Reexports) != 0 || len(after.Reexports) != 0 {
		t.Fatalf("fixture no longer holds Reexports empty: %v -> %v", before.Reexports, after.Reexports)
	}
	if !surfaceChanged(before, after) {
		t.Fatal("expected an outgoing surface change")
	}
	if outgoingOnlySurfaceShift(before, after) {
		t.Fatalf("a name that stopped resolving locally must refuse the proof: surface %v -> %v, contextFree %v -> %v",
			before.ExportSurface, after.ExportSurface, before.ExportSurfaceContextFree, after.ExportSurfaceContextFree)
	}
	if after.ExportSurfaceContextFree {
		t.Fatal("a bare export clause bound to an import is not context-free")
	}
}

// TestStage15ForwardingRetargetRefusesProof moves a forwarded name's target
// without touching a byte of the forwarder: the specifier it already names
// starts resolving. Reexports stays empty because a bare clause emits no
// dependency fact, so only the outgoing fields move - and the consumer must
// still rebind to the new leaf.
func TestStage15ForwardingRetargetRefusesProof(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"mid2/mid2.ts": "import { work } from '../late2/late2';\nexport { work };\n",
		"use3/u.ts":    "import { work } from '../mid2/mid2';\nexport const r = work();\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)
	before := stage15Record(t, opts.StateDir, "mid2/mid2.ts")

	writeFile(t, root, "late2/late2.ts", "export function work() {\n  return 3;\n}\n")
	bodyScopeDeltaCold(t, eng, root, opts, cons)
	after := stage15Record(t, opts.StateDir, "mid2/mid2.ts")

	if len(before.Reexports) != 0 || len(after.Reexports) != 0 {
		t.Fatalf("fixture no longer holds Reexports empty: %v -> %v", before.Reexports, after.Reexports)
	}
	if outgoingOnlySurfaceShift(before, after) {
		t.Fatalf("a forwarder must never carry the proof: contextFree %v -> %v, surface %v -> %v",
			before.ExportSurfaceContextFree, after.ExportSurfaceContextFree, before.ExportSurface, after.ExportSurface)
	}
}

// TestStage15AliasedForwardRefusesProof is the alias countercase. The exported
// name arrives through a tsconfig path alias, so the index that describes it
// depends on the alias map and is not context-free - which is decided before the
// import map is consulted, so an alias that stops resolving is refused too.
func TestStage15AliasedForwardRefusesProof(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"tsconfig.json": "{\n  \"compilerOptions\": {\n    \"baseUrl\": \".\",\n    \"paths\": { \"@lib/*\": [\"libs/*\"] }\n  }\n}\n",
		"libs/y.ts":     "export function work() {\n  return 1;\n}\n",
		"mid3/mid3.ts":  "import { work } from '@lib/y';\nexport { work };\n",
		"use4/u.ts":     "import { work } from '../mid3/mid3';\nexport const r = work();\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	bodyScopeStart(t, eng, root, opts)
	rec := stage15Record(t, opts.StateDir, "mid3/mid3.ts")
	if rec.ExportSurfaceContextFree {
		t.Fatalf("an aliased forward must not be recorded as context-free: surface %v", rec.ExportSurface)
	}
}

// TestStage15DefaultConsumerStaysSideReadTaken pins the conservative half. A
// default import notes the dependency even when the default resolves there, so
// the consumer records the side read and every rule keeps taking it - the proof
// must not be read as permission to skip a default consumer.
func TestStage15DefaultConsumerStaysSideReadTaken(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"helpers/a.ts": "export function helper() {\n  return 1;\n}\n",
		"helpers/b.ts": "export function helper() {\n  return 2;\n}\n",
		"dep2/dep2.ts": "import { helper } from '../helpers/a';\nexport default function thing() {\n  return helper();\n}\n",
		"use2/u.ts":    "import thing from '../dep2/dep2';\nexport const r = thing();\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	found := false
	for _, sr := range stage15Record(t, opts.StateDir, "use2/u.ts").SideReads {
		if sr == "dep2/dep2.ts" {
			found = true
		}
	}
	if !found {
		t.Fatal("a default import must record the dependency as a side read")
	}

	writeFile(t, root, "dep2/dep2.ts", "import { helper } from '../helpers/b';\nexport default function thing() {\n  return helper();\n}\n")
	res := bodyScopeDeltaCold(t, eng, root, opts, cons)
	if res.ParsedFiles != 2 {
		t.Fatalf("the default consumer must be reparsed with its dependency, got %d parsed", res.ParsedFiles)
	}
}

// TestStage15UnprovenRecordsRefuse covers the migration and the two bits
// separately: a record written before the proof existed, and one whose surface
// was recorded but not proven context-free, each narrow nothing.
func TestStage15UnprovenRecordsRefuse(t *testing.T) {
	mk := func(recorded, contextFree bool, resolved []string) *tsextractor.FileRecord {
		return &tsextractor.FileRecord{
			ImportComplete:           true,
			ParseKind:                "ts",
			NuxtScope:                "-",
			Declared:                 []string{"dep.value"},
			ResolvedFiles:            resolved,
			ExportSurface:            []string{"local:value"},
			ExportSurfaceRecorded:    recorded,
			ExportSurfaceContextFree: contextFree,
		}
	}
	old := mk(true, true, []string{"helpers/a.ts"})
	neu := mk(true, true, []string{"helpers/b.ts"})
	if !outgoingOnlySurfaceShift(old, neu) {
		t.Fatal("two proven-equal context-free surfaces should hold")
	}
	if outgoingOnlySurfaceShift(mk(false, false, []string{"helpers/a.ts"}), neu) {
		t.Fatal("an unrecorded old surface must refuse")
	}
	if outgoingOnlySurfaceShift(old, mk(false, false, []string{"helpers/b.ts"})) {
		t.Fatal("an unrecorded new surface must refuse")
	}
	if outgoingOnlySurfaceShift(mk(true, false, []string{"helpers/a.ts"}), neu) {
		t.Fatal("a recorded but unproven old surface must refuse")
	}
	if outgoingOnlySurfaceShift(old, mk(true, false, []string{"helpers/b.ts"})) {
		t.Fatal("a recorded but unproven new surface must refuse")
	}
	moved := mk(true, true, []string{"helpers/b.ts"})
	moved.ExportSurface = []string{"local:value", "local:extra"}
	if outgoingOnlySurfaceShift(old, moved) {
		t.Fatal("a moved surface must refuse")
	}
	declared := mk(true, true, []string{"helpers/b.ts"})
	declared.Declared = []string{"dep.value", "dep.other"}
	if outgoingOnlySurfaceShift(old, declared) {
		t.Fatal("a moved declared surface belongs to the name delta and must refuse here")
	}
}

// TestStage15ConsumerGatesRefuseFrameworkBinding pins the dependent-side
// refusals: a name can arrive without an import statement, and the export index
// says nothing about those.
func TestStage15ConsumerGatesRefuseFrameworkBinding(t *testing.T) {
	base := func() *tsextractor.FileRecord {
		return &tsextractor.FileRecord{ImportComplete: true, ParseKind: "ts", NuxtScope: "-"}
	}
	if !locallyBoundConsumer(base()) {
		t.Fatal("a plain ts consumer with recorded reads should qualify")
	}
	if locallyBoundConsumer(nil) {
		t.Fatal("an absent record proves nothing")
	}
	vue := base()
	vue.ParseKind = "vue"
	if locallyBoundConsumer(vue) {
		t.Fatal("a template binds components by convention and must refuse")
	}
	nuxt := base()
	nuxt.NuxtScope = "."
	if locallyBoundConsumer(nuxt) {
		t.Fatal("a Nuxt scope must refuse")
	}
	auto := base()
	auto.AutoImportDirs = []string{"composables"}
	if locallyBoundConsumer(auto) {
		t.Fatal("auto-import dirs must refuse")
	}
	aliases := base()
	aliases.NuxtAliases = []string{"a=>b"}
	if locallyBoundConsumer(aliases) {
		t.Fatal("nuxt aliases must refuse")
	}
	incomplete := base()
	incomplete.ImportComplete = false
	if locallyBoundConsumer(incomplete) {
		t.Fatal("a record whose reads are not recorded must refuse")
	}
}

// TestStage15PersistedRecordWithoutProofFallsBack exercises the codec, not a
// constructed struct: it takes the state a real run committed, strips the new
// key from the committed JSON exactly as a binary that predates it would have
// written the file, and runs the delta against that. The dependency and its
// consumers are byte-identical to the benefit case, so the only difference is
// the missing proof - and a missing proof must read as unproven and take every
// importer, never as a default that lets an old record through a new rule.
func TestStage15PersistedRecordWithoutProofFallsBack(t *testing.T) {
	for _, authoritative := range []bool{true, false} {
		name := "legacy"
		if authoritative {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"helpers/a.ts": "export function helper() {\n  return 1;\n}\n",
				"helpers/b.ts": "export function helper() {\n  return 2;\n}\n",
				"dep/dep.ts":   "import { helper } from '../helpers/a';\nexport function value() {\n  return helper();\n}\n",
				"use/u1.ts":    "import { value } from '../dep/dep';\nexport const r1 = value();\n",
				"use/u2.ts":    "import { value } from '../dep/dep';\nexport const r2 = value();\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative}
			cons := bodyScopeStart(t, eng, root, opts)

			path := statePath(opts.StateDir)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(`"export_surface_context_free"`)) {
				t.Fatal("committed state carries no proof key, so stripping it would prove nothing")
			}
			// The field is omitempty and only ever true, so an old file simply
			// has no such key. Drop it the way its absence would read.
			aged := bytes.ReplaceAll(raw, []byte(`"export_surface_context_free":true,`), nil)
			aged = bytes.ReplaceAll(aged, []byte(`,"export_surface_context_free":true`), nil)
			if bytes.Contains(aged, []byte(`"export_surface_context_free"`)) {
				t.Fatal("proof key survived the downgrade")
			}
			if err := os.WriteFile(path, aged, 0o644); err != nil {
				t.Fatal(err)
			}
			reloaded := stage15Record(t, opts.StateDir, "dep/dep.ts")
			if !reloaded.ExportSurfaceRecorded {
				t.Fatal("downgrade removed more than the proof key")
			}
			if reloaded.ExportSurfaceContextFree {
				t.Fatal("record decoded from an old file must read as unproven")
			}

			writeFile(t, root, "dep/dep.ts", "import { helper } from '../helpers/b';\nexport function value() {\n  return helper();\n}\n")
			res := bodyScopeDeltaCold(t, eng, root, opts, cons)
			t.Logf("stage15 migration counter: parsed_files=%d parsed_by_reason=%v consumers=2", res.ParsedFiles, res.Invalidation.ParsedByReason)
			if res.ParsedFiles < 3 {
				t.Fatalf("an unproven old record must take its importers, parsed %d of 3", res.ParsedFiles)
			}
		})
	}
}
