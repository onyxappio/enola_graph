package graphsession

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/graphstream"
)

// Begin froze every file reachable backwards from a changed one, so editing the
// body of a widely imported module announced its whole importer closure - a real
// Product transition froze 1401 owners for 2 changed contributions, and adding
// one export froze 1629 and reparsed 1597. The closure is now a fixed point over
// surfaces someone actually observed: a hop is taken because a reparse published
// a different import/export surface, because a dependent cannot prove its own
// cross-file reads, or because a declared name entered or left the global index.
//
// Every case here judges the applied graph against a cold session, because a
// narrower manifest that is missing an owner is worse than a wide one. Each also
// pins the parse count, because shrinking one manifest while still reparsing the
// same files is not what this is for.

// bodyScopeRun runs a delta and returns the result with its Begin scope.
func bodyScopeRun(t *testing.T, eng *engine.Engine, root string, opts Options) (*Result, map[string]bool, []string) {
	t.Helper()
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	requireNoWholeDomainFallback(t, res, ids)
	return res, owners, ids
}

// bodyScopeStart runs the analysis that leaves state behind and returns the
// consumer holding it, so later deltas can be applied onto it and compared cold.
func bodyScopeStart(t *testing.T, eng *engine.Engine, root string, opts Options) *Consumer {
	t.Helper()
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	return cons
}

// bodyScopeDelta runs a delta onto an existing consumer and checks it cold.
func bodyScopeDelta(t *testing.T, eng *engine.Engine, root string, opts Options, cons *Consumer) (*Result, map[string]bool, []string) {
	t.Helper()
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	requireNoWholeDomainFallback(t, res, ids)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	return res, owners, ids
}

// bodyScopeDeltaCold runs a delta, applies it and compares the result against a
// cold session without reading the Begin manifest. A caller that does not
// announce authoritative files gets no frozen scope - the run is allowed to
// widen after Begin - so on that path the graph and the parse count are the
// whole contract.
func bodyScopeDeltaCold(t *testing.T, eng *engine.Engine, root string, opts Options, cons *Consumer) *Result {
	t.Helper()
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	// The oracle has to be the same caller: whether files are announced as
	// authoritative decides which global owners a session publishes at all, so
	// a cold run with the other setting differs for reasons that have nothing
	// to do with the delta under test.
	coldSink := &graphstream.MemorySink{}
	cold := opts
	cold.StateDir = t.TempDir()
	cold.ForceInitial = true
	if _, err := Run(context.Background(), eng, root, coldSink, cold); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
	return res
}

// fanOutRepo is a leaf every importer depends on, a barrel over it, and many
// files importing the barrel. It is the shape that made a one-line body edit
// freeze the repository.
func fanOutRepo(t *testing.T, importers int) (string, []string) {
	t.Helper()
	files := map[string]string{
		"core.ts":   "export function work(n: number): number {\n  return n + 1;\n}\n",
		"barrel.ts": "export { work } from './core';\n",
	}
	var names []string
	for i := range importers {
		rel := fmt.Sprintf("app/use%d.ts", i)
		files[rel] = fmt.Sprintf("import { work } from '../barrel';\nexport const r%d = work(%d);\n", i, i)
		names = append(names, rel)
	}
	return setupTSRepo(t, files), names
}

// The case the whole task is about: the body of a module every file depends on
// changes, its import/export surface does not, and nothing downstream can see a
// difference. Before the observed-surface closure this froze core.ts plus the
// barrel plus every importer; now the barrel and the importers stay out, and the
// applied graph still equals cold.
func TestBodyScopeColdLocalBodyEditStaysOnTheEditedFile(t *testing.T) {
	root, importers := fanOutRepo(t, 12)
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "core.ts", "export function work(n: number): number {\n  const bump = 2;\n  return n + bump;\n}\n")

	res, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "core.ts")
	forbidOwners(t, owners, ids, append([]string{"barrel.ts"}, importers...)...)
	if len(ids) != 1 {
		t.Fatalf("Begin froze %d owners for a body-only edit: %v", len(ids), ids)
	}
	if res.ParsedFiles != 1 {
		t.Fatalf("parsed=%d, want only the edited file", res.ParsedFiles)
	}
}

// A changed export surface does reach importers, but only the ones that import
// it. The barrel re-exports the new name so it is reparsed; the files behind the
// barrel are reparsed only because the barrel's own Reexports then changed. A
// second module that imports the barrel without naming anything new must not be
// dragged in transitively beyond that.
func TestBodyScopeColdAddedExportStopsAtObservedSurfaceChanges(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"core.ts":  "export const a = 1;\n",
		"mid.ts":   "import { a } from './core';\nexport const m = a + 1;\n",
		"far.ts":   "import { m } from './mid';\nexport const f = m + 1;\n",
		"other.ts": "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "core.ts", "export const a = 1;\nexport const added = 2;\n")

	res, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "core.ts", "mid.ts")
	// mid.ts declares the same names as before, so nothing it exports moved and
	// far.ts sees no difference: the old rule reached it purely by reachability.
	forbidOwners(t, owners, ids, "far.ts", "other.ts")
	if res.ParsedFiles != 2 {
		t.Fatalf("parsed=%d, want core.ts and its direct importer: %v", res.ParsedFiles, ids)
	}
}

// Deleting an export moves the name out of the global index, and buildIndex
// resolves Fact.Name globally rather than along import edges: a file that calls
// the name without importing it has to be in Begin too.
func TestBodyScopeColdRemovedExportReachesGlobalNameConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"core.ts":   "export function gone(): number {\n  return 1;\n}\nexport const keep = 1;\n",
		"caller.ts": "export function callIt(): number {\n  return gone();\n}\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "core.ts", "export const keep = 1;\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "core.ts", "caller.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// The mirror of the case above, and the one the old planner could not see at
// all: declaredNameDependents reads the names a file declared BEFORE the edit, so
// an ADDED name had no cached evidence anywhere and the consumer reached Begin
// only when some import edge happened to cover it. Here none does.
func TestBodyScopeColdAddedGloballyAmbiguousNameReachesNameConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"first.ts":  "export function shared(): number {\n  return 1;\n}\n",
		"second.ts": "export const s = 1;\n",
		"caller.ts": "export function callIt(): number {\n  return shared();\n}\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	// second.ts now declares the same name first.ts does, so the call in
	// caller.ts becomes ambiguous. No import edge connects any of the three.
	writeFile(t, root, "second.ts", "export const s = 1;\nexport function shared(): number {\n  return 2;\n}\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "second.ts", "caller.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// An unchanged middle barrel whose leaf is retargeted. barrel.ts keeps its own
// bytes only in the first half of this case; here the retarget is the edit, so
// its Reexports move and the consumer must follow to the new leaf.
func TestBodyScopeColdRetargetedBarrelMovesConsumerToNewLeaf(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf1.ts":  "export function pick(): number {\n  return 1;\n}\n",
		"leaf2.ts":  "export function pick(): number {\n  return 2;\n}\n",
		"barrel.ts": "export { pick } from './leaf1';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "barrel.ts", "export { pick } from './leaf2';\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "barrel.ts", "use.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// A named import binds through the barrel to the leaf, and the binder records
// the leaf it actually read as a side read of the consumer. Extraction has always
// reparsed on a moved side-read hash, but it decided that after Begin; the frozen
// plan now replays the same comparison, so a leaf body edit behind an unchanged
// barrel still carries the consumer into Begin rather than failing closed.
func TestBodyScopeColdLeafBodyEditBehindBarrelCarriesSideReadConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "export function pick(): number {\n  return 1;\n}\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "leaf.ts", "export function pick(): number {\n  const v = 1;\n  return v;\n}\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "leaf.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// Namespace member access re-enters the import binder with a note, so ns.pick()
// records the leaf as a side read exactly as a named import does.
func TestBodyScopeColdNamespaceMemberAccessTracksLeaf(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "export function pick(): number {\n  return 1;\n}\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import * as ns from './barrel';\nexport const u = ns.pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "leaf.ts", "export function pick(): number {\n  return 3;\n}\nexport const extra = 1;\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "leaf.ts", "barrel.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// A default import binds by the local name with a path-derived fallback, and
// `export { default as X } from` resolves through fileSymbolName. Neither reads
// the leaf's body, so a leaf body edit must not drag the bridge's consumers in -
// but a leaf surface change must.
func TestBodyScopeColdDefaultImportBridge(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"impl.ts":   "export default function run(): number {\n  return 1;\n}\n",
		"bridge.ts": "export { default as Run } from './impl';\n",
		"use.ts":    "import Run from './impl';\nexport const u = Run();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "impl.ts", "export default function run(): number {\n  const v = 1;\n  return v;\n}\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "impl.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// Two files importing each other. Parsing reads source bytes and the fixed
// session filename context, never another file's record, so a file already
// parsed from its final captured source needs no second parse when its cycle
// partner publishes a changed surface a hop later.
func TestBodyScopeColdImportCycleParsesEachMemberOnce(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"a.ts":     "import { b } from './b';\nexport const a = b + 1;\n",
		"b.ts":     "import { a } from './a';\nexport const b = 1;\nexport const unused = a;\n",
		"other.ts": "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "a.ts", "import { b } from './b';\nexport const a = b + 1;\nexport const added = 2;\n")

	res, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "a.ts", "b.ts")
	forbidOwners(t, owners, ids, "other.ts")
	if res.ParsedFiles != 2 {
		t.Fatalf("parsed=%d, want each cycle member once: %v", res.ParsedFiles, ids)
	}
}

// A new source file is a membership change: the importer's specifier can rebind
// even though its own bytes did not move, so the narrowing must not apply to it.
func TestBodyScopeColdNewSourceStillRebindsImporter(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"foo/index.ts": "export function pick(): number {\n  return 1;\n}\n",
		"use.ts":       "import { pick } from './foo';\nexport const u = pick();\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "foo.ts", "export function pick(): number {\n  return 2;\n}\n")

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "foo.ts", "use.ts")
}

// Removing the leaf retires its identity. Its declared names leave the global
// index, so consumers that named them must be in Begin even with no edge left to
// follow.
func TestBodyScopeColdRemovedSourceRetiresNameForConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "export function pick(): number {\n  return 1;\n}\n",
		"caller.ts": "export function callIt(): number {\n  return pick();\n}\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	if err := os.Remove(filepath.Join(root, "leaf.ts")); err != nil {
		t.Fatal(err)
	}

	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "leaf.ts", "caller.ts")
	forbidOwners(t, owners, ids, "other.ts")
}

// A side read proved inert must stay proved. The first delta skips the consumer
// because the leaf was reparsed and published the same surface; the consumer's
// cached SideReadHashes still hold the leaf's old bytes. If that proof is not
// written back, the second delta - which touches an unrelated file and does not
// reparse the leaf at all - sees a hash mismatch it can no longer explain and
// drags the consumer back in for a change already shown not to matter.
func TestBodyScopeColdProvenSideReadStaysProvenAcrossDeltas(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "export function pick(): number {\n  return 1;\n}\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "leaf.ts", "export function pick(): number {\n  const v = 1;\n  return v;\n}\n")
	first, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "leaf.ts")
	forbidOwners(t, owners, ids, "use.ts", "other.ts")
	if first.ParsedFiles != 1 {
		t.Fatalf("first delta parsed=%d, want only the edited leaf", first.ParsedFiles)
	}

	writeFile(t, root, "other.ts", "export const o = 2;\n")
	second, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "other.ts")
	forbidOwners(t, owners, ids, "leaf.ts", "barrel.ts", "use.ts")
	if second.ParsedFiles != 1 {
		t.Fatalf("second delta parsed=%d, want only the edited file; a stale side-read hash pulled the consumer back", second.ParsedFiles)
	}
}

// committedSideReads reads one file's recorded side-read hashes out of the last
// committed state, which is the copy a later delta invalidates against.
func committedSideReads(t *testing.T, stateDir, path string) map[string]string {
	t.Helper()
	st, err := loadCommittedState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if st == nil {
		t.Fatalf("no committed state in %s", stateDir)
	}
	fs := lookupState(st.Files, path)
	if fs == nil || fs.TS == nil {
		t.Fatalf("no committed record for %s", path)
	}
	return maps.Clone(fs.TS.SideReadHashes)
}

// The side-read write-back is a state change like any other: it belongs to the
// delta that proved it, so a run whose End never lands must leave the previous
// state exactly as it was. Otherwise an interrupted run would record a proof
// for a parse that was rolled back.
func TestBodyScopeColdProvenSideReadRefreshRollsBackWithFailedEnd(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "export function pick(): number {\n  return 1;\n}\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)
	before := committedSideReads(t, state, "use.ts")
	if len(before) == 0 {
		t.Fatal("fixture records no side read for use.ts")
	}

	writeFile(t, root, "leaf.ts", "export function pick(): number {\n  const v = 1;\n  return v;\n}\n")
	fail := &endFailingSink{err: errors.New("broker unavailable")}
	fail.armed.Store(true)
	if _, err := Run(context.Background(), eng, root, fail, opts); err == nil {
		t.Fatal("interrupted delta succeeded")
	}
	// The rollback under test is the one where the replacement was announced and
	// shipped and only the commit was lost: anything less does not exercise a
	// state that has to be thrown away. MemorySink.FailAt counts publishes, so
	// it can only name End by guessing the batch count.
	landed := fail.CloneRecords()
	published := map[string]int{}
	for _, r := range landed {
		published[envelopeKind(r.MsgID)]++
	}
	if published[graphstream.TypeBeginReplace] != 1 || published[graphstream.TypeBatch] == 0 || published[graphstream.TypeEndReplace] != 0 {
		t.Fatalf("fixture did not fail End: %v", published)
	}
	st, err := loadCommittedState(state)
	if err != nil || st == nil || st.Generation != 1 {
		t.Fatalf("interrupted run promoted generation: %v %+v", err, st)
	}
	if got := committedSideReads(t, state, "use.ts"); !maps.Equal(got, before) {
		t.Fatalf("interrupted run rewrote the committed side-read proof: %v -> %v", before, got)
	}

	// Completing the same delta does commit it, and the consumer then stays out
	// of an unrelated delta instead of being dragged back by old bytes. The
	// recovery finishes the publish that was interrupted rather than opening a
	// new one, so the consumer is given what the broker already holds first -
	// the Begin and batches that landed - and then the End that was lost.
	recover := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, recover, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(landed); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(recover.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if got := committedSideReads(t, state, "use.ts"); maps.Equal(got, before) {
		t.Fatalf("completed delta did not record the proven side read: still %v", got)
	}
	writeFile(t, root, "other.ts", "export const o = 2;\n")
	res, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "other.ts")
	forbidOwners(t, owners, ids, "leaf.ts", "barrel.ts", "use.ts")
	if res.ParsedFiles != 1 {
		t.Fatalf("parsed=%d after a committed proof, want only the edited file", res.ParsedFiles)
	}
}

// Angular binds across files through templates and decorators, which the record
// surfaces do not describe, so the session forces the whole domain for it before
// prepareFrozenTS is ever reached. This pins that coupling: a body-only edit in
// an injectable still carries the component that binds it, and the applied graph
// still equals cold. Angular is deliberately left unoptimized by this change.
func TestBodyScopeAngularCrossFileBindingIsNeverNarrowed(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/data.ts":   "import { Injectable } from '@angular/core';\n@Injectable() export class Data {\n  load(): number { return 1; }\n}\n",
		"src/page.ts":   "import { Component } from '@angular/core';\nimport { Data } from './data';\n@Component({templateUrl:'./page.html'}) export class Page {\n  constructor(private d: Data) {}\n  save() { return this.d.load(); }\n}\n",
		"src/page.html": `<button (click)="save()">Save</button>`,
		"src/other.ts":  "export const o = 1;\n",
	})
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"app","dependencies":{"@angular/core":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "src/data.ts", "import { Injectable } from '@angular/core';\n@Injectable() export class Data {\n  load(): number { const v = 2; return v; }\n}\n")

	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	if contextFallback(res) == "" {
		t.Fatalf("Angular delta narrowed the TypeScript session; Begin scope was %v", ids)
	}
	requireOwners(t, owners, ids, "src/data.ts", "src/page.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, testEngine(t, root), root))
}

// The closure is a fixed point, so one delta can run several extract hops. The
// public counters are the only place that work is visible, and a hop that is
// not summed would report a cheap-looking delta that read the whole project
// again - the thing msg_ce5b1f465427 asked to be able to see. Three hops here:
// the leaf changes surface, the star re-export republishes it, and only then
// does the consumer's binding move.
func TestBodyScopeColdMultiHopCountsEveryHop(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"a.ts":   "export const one = 1;\n",
		"b.ts":   "export * from './a';\n",
		"c.ts":   "import { one } from './b';\nexport const c = one;\n",
		"far.ts": "export const f = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "a.ts", "export const one = 1;\nexport const two = 2;\n")

	res, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "a.ts", "b.ts", "c.ts")
	forbidOwners(t, owners, ids, "far.ts")
	if res.ParsedFiles != 3 {
		t.Fatalf("parsed=%d, want the leaf, the re-export and the consumer", res.ParsedFiles)
	}
	if res.Stats.FilesParsed != res.ParsedFiles {
		t.Fatalf("Stats.FilesParsed=%d disagrees with ParsedFiles=%d", res.Stats.FilesParsed, res.ParsedFiles)
	}
	// A single hop would read at most its own pending file; summing is what
	// makes the composition cost of a multi-hop delta visible at all.
	if res.Stats.FilesRead < res.ParsedFiles {
		t.Fatalf("FilesRead=%d under-counts %d parsed files; a hop is not summed", res.Stats.FilesRead, res.ParsedFiles)
	}
	// CachedFiles must mean "never read this delta". The last hop sees the two
	// earlier files as cached, and counting those would hide the hops again.
	if res.Stats.CachedFiles != 1 {
		t.Fatalf("CachedFiles=%d, want only far.ts; earlier hops are being counted as cached", res.Stats.CachedFiles)
	}
}

// The preview does the parsing on the session's behalf, so without the replay
// the delta reports no classified parse at all and the test hook never fires -
// exactly the blind spot msg_394a4ef078a3 and msg_ce5b1f465427 warned about.
// Every parse is reported once, by whichever site actually read the file.
func TestBodyScopeColdMultiHopReportsEachParseOnce(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"a.ts":   "export const one = 1;\n",
		"b.ts":   "export * from './a';\n",
		"c.ts":   "import { one } from './b';\nexport const c = one;\n",
		"far.ts": "export const f = 1;\n",
	})
	eng := testEngine(t, root)
	var seen []string
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, OnBeforeParse: func(rel string) {
		seen = append(seen, filepath.ToSlash(rel))
	}}
	cons := bodyScopeStart(t, eng, root, opts)

	seen = nil
	writeFile(t, root, "a.ts", "export const one = 1;\nexport const two = 2;\n")
	res, _, _ := bodyScopeDelta(t, eng, root, opts, cons)

	counts := map[string]int{}
	for _, rel := range seen {
		counts[rel]++
	}
	for _, want := range []string{"a.ts", "b.ts", "c.ts"} {
		if counts[want] != 1 {
			t.Fatalf("OnBeforeParse fired %d times for %s; saw %v", counts[want], want, seen)
		}
	}
	if len(seen) != res.ParsedFiles {
		t.Fatalf("OnBeforeParse fired %d times for %d parses: %v", len(seen), res.ParsedFiles, seen)
	}
	byReason := res.Invalidation.ParsedByReason
	if byReason["source content"] != 1 {
		t.Fatalf("edited file not classified as content dirt: %v", byReason)
	}
	if byReason["resolution"] != 2 {
		t.Fatalf("hop-discovered files not classified as resolution: %v", byReason)
	}
	if byReason["specialized extraction"] != 0 {
		t.Fatalf("parses fell through unclassified: %v", byReason)
	}
}

// A file the preview only discovers on a later hop is read inside the same
// transaction as the first one, and End must still refuse if it moved. The hook
// fires after the preview and before End, which is exactly the window the fence
// has to cover. Two guards stand behind this - the preview now captures each
// hop's sources, and the pre-End rehash re-reads every record - so the test
// pins the property, not one implementation of it.
func TestBodyScopeMultiHopSourceIsFencedBeforeEnd(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"a.ts":   "export const one = 1;\n",
		"b.ts":   "export * from './a';\n",
		"c.ts":   "import { one } from './b';\nexport const c = one;\n",
		"far.ts": "export const f = 1;\n",
	})
	eng := testEngine(t, root)
	state := t.TempDir()
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: state, AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}

	moved := false
	opts := Options{StateDir: state, AuthoritativeFiles: true, OnBeforeParse: func(string) {
		if moved {
			return
		}
		moved = true
		writeFile(t, root, "c.ts", "import { one } from './b';\nexport const c = one + 1;\n")
	}}
	writeFile(t, root, "a.ts", "export const one = 1;\nexport const two = 2;\n")
	_, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a hop-discovered source moved under the run and End accepted it: %v", err)
	}
}

// An owner that contributed nothing last time has no facts to diff against, so
// a closure keyed on "what moved" can miss it in both directions. Both are
// checked here: the empty module that starts declaring a name, and the module
// that stops. The name consumer has to follow in each case, and the emptied
// owner still needs an owner record of its own so its old name is withdrawn.
func TestBodyScopeColdZeroContributionOwnerGainsAndLosesNames(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"blank.ts": "export {};\n",
		"use.ts":   "export function u() { return blankName(); }\n",
		"far.ts":   "export const f = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := bodyScopeStart(t, eng, root, opts)

	writeFile(t, root, "blank.ts", "export function blankName() { return 1; }\n")
	_, owners, ids := bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "blank.ts", "use.ts")
	forbidOwners(t, owners, ids, "far.ts")

	writeFile(t, root, "blank.ts", "export {};\n")
	_, owners, ids = bodyScopeDelta(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "blank.ts", "use.ts")
	forbidOwners(t, owners, ids, "far.ts")
}

// endFailingSink accepts the Begin and every batch and rejects the End that
// would commit them, which is the interruption a rollback assertion is about.
type endFailingSink struct {
	graphstream.MemorySink
	err error
	// armed lets a resident session publish its first generations normally and
	// only then lose an End, which is the sequence a watch actually sees.
	armed atomic.Bool
}

func (s *endFailingSink) Publish(ctx context.Context, subject, msgID string, payload []byte) error {
	if s.armed.Load() && strings.Contains(msgID, graphstream.TypeEndReplace) {
		return s.err
	}
	return s.MemorySink.Publish(ctx, subject, msgID, payload)
}

// A file that imports nothing is the origin of every name it exports, so a
// consumer reading its bytes through a named import can only have taken a
// declared name from it. A file that does import something is a possible link
// in someone's re-export chain, and what arrives as which name there is a
// mapping no record field describes: Reexports is built from the dependency
// facts and names the module a barrel forwards to, not the bindings. These pin
// that the closure treats the two differently, on both the authoritative and
// the legacy caller path, with cold as the authority in every case.
func TestBodyScopeColdBarrelAliasRenameRebindsConsumers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after string
	}{
		{"rename", "export { work as renamed } from './core';\n"},
		{"swap", "export { work as other, other as work } from './core';\n"},
		{"star", "export * from './core';\n"},
	} {
		for _, authoritative := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/authoritative=%v", tc.name, authoritative), func(t *testing.T) {
				root := setupTSRepo(t, map[string]string{
					"core.ts":   "export function work(): number {\n  return 1;\n}\nexport function other(): number {\n  return 2;\n}\n",
					"barrel.ts": "export { work } from './core';\n",
					"use.ts":    "import { work } from './barrel';\nexport const u = work();\n",
					"alone.ts":  "export const a = 1;\n",
				})
				eng := testEngine(t, root)
				opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative}
				cons := bodyScopeStart(t, eng, root, opts)

				writeFile(t, root, "barrel.ts", tc.after)
				// The cold comparison inside both helpers is the real assertion:
				// the consumer has to be reparsed, or its call edge keeps pointing
				// at a binding that moved.
				var res *Result
				if authoritative {
					var owners map[string]bool
					var ids []string
					res, owners, ids = bodyScopeDelta(t, eng, root, opts, cons)
					requireOwners(t, owners, ids, "barrel.ts", "use.ts")
					forbidOwners(t, owners, ids, "alone.ts")
				} else {
					res = bodyScopeDeltaCold(t, eng, root, opts, cons)
				}
				if res.ParsedFiles < 2 {
					t.Fatalf("parsed=%d, want the barrel and its consumer", res.ParsedFiles)
				}
			})
		}
	}
}

// The contrast: the same consumer, the same barrel, and a body edit under the
// origin that moves no name at all. core.ts imports nothing, so the reparse of
// it proves what the consumer read there still reads the same, and the consumer
// stays out - on both caller paths.
func TestBodyScopeColdOriginBodyEditLeavesBarrelConsumerAlone(t *testing.T) {
	for _, authoritative := range []bool{true, false} {
		t.Run(fmt.Sprintf("authoritative=%v", authoritative), func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"core.ts":   "export function work(): number {\n  return 1;\n}\n",
				"barrel.ts": "export { work } from './core';\n",
				"use.ts":    "import { work } from './barrel';\nexport const u = work();\n",
				"alone.ts":  "export const a = 1;\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative}
			cons := bodyScopeStart(t, eng, root, opts)

			writeFile(t, root, "core.ts", "export function work(): number {\n  const v = 1;\n  return v;\n}\n")
			var res *Result
			if authoritative {
				var owners map[string]bool
				var ids []string
				res, owners, ids = bodyScopeDelta(t, eng, root, opts, cons)
				requireOwners(t, owners, ids, "core.ts")
				forbidOwners(t, owners, ids, "barrel.ts", "use.ts", "alone.ts")
			} else {
				res = bodyScopeDeltaCold(t, eng, root, opts, cons)
			}
			if res.ParsedFiles != 1 {
				t.Fatalf("parsed=%d, want only the edited origin", res.ParsedFiles)
			}
		})
	}
}

// envelopeKind reads the message type out of a publish id, which is the only
// thing a sink is given. DecodeRun cannot be asked here: the run it would be
// handed is the interrupted one, with no End to close it.
func envelopeKind(msgID string) string {
	parts := strings.Split(msgID, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2]
}

// TestBodyScopeDefaultOriginSwitchRebindsConsumers pins the case that broke the
// first version of the side-read proof. src/origin.ts declares round and ceil
// and calls both, so its Declared and Referenced sets do not move when the
// default export is pointed at the other one; it imports nothing, so it forwards
// no foreign binding either. Every field the proof compares holds, and yet the
// barrel re-exports `default` under the name pick and a.ts calls it, so the call
// has to land on ceil after the edit. The proof has to refuse this file, and the
// cold comparison is what says whether it did.
func TestBodyScopeDefaultOriginSwitchRebindsConsumers(t *testing.T) {
	for _, authoritative := range []bool{false, true} {
		name := "legacy"
		if authoritative {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			body := "export function round(n: number) { return n + 1; }\n" +
				"export function ceil(n: number) { return n + 2; }\n" +
				"round(1); ceil(1);\n"
			root := setupTSRepo(t, map[string]string{
				"src/a.ts":      "import { pick } from './barrel';\nexport function caller() { return pick(1); }\n",
				"src/barrel.ts": "export { default as pick } from './origin';\n",
				"src/origin.ts": body + "export default round;\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: filepath.Join(root, ".enola", "live"), AuthoritativeFiles: authoritative}

			sink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
				t.Fatal(err)
			}
			cons := applyGraph(t, sink)
			assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/origin.ts")

			writeFile(t, root, "src/origin.ts", body+"export default ceil;\n")
			delta := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, root, delta, opts); err != nil {
				t.Fatal(err)
			}
			if err := cons.ApplyRecords(delta.CloneRecords()); err != nil {
				t.Fatal(err)
			}

			cold := &graphstream.MemorySink{}
			coldOpts := opts
			coldOpts.StateDir = t.TempDir()
			coldOpts.ForceInitial = true
			if _, err := Run(context.Background(), eng, root, cold, coldOpts); err != nil {
				t.Fatal(err)
			}
			assertAppliedEqualsCold(t, cons, applyGraph(t, cold))
			assertCallResolvedToFile(t, cons, "src/a.ts", "src.ceil", "src/origin.ts")
		})
	}
}

// Whether a name is exported at all, and whether an `export { ... }` clause still
// lists it, decide what a consumer binds through a barrel - and neither is on the
// record anywhere except the export surface. Declared holds every declaration
// whether exported or not, Referenced holds the calls, and both sit still while
// `export function round` becomes `function round` or `export { round, ceil }`
// becomes `export { ceil }`. These four cases are the coordinator's independent
// probes: each ran green on unmodified main and red on the first surface proof,
// in both caller modes, which is what makes them regressions rather than tests
// that happen to pass.
func TestBodyScopeExportMembershipMovesRebindConsumers(t *testing.T) {
	const keywordBody = "export function round(n: number) { return n + 1; }\n" +
		"export function ceil(n: number) { return n + 2; }\n" +
		"round(1); ceil(1);\n"
	const clauseBody = "function round(n: number) { return n + 1; }\n" +
		"function ceil(n: number) { return n + 2; }\n" +
		"round(1); ceil(1);\n" +
		"export { round, ceil };\n"
	demoted := strings.Replace(keywordBody, "export function round", "function round", 1)
	narrowed := strings.Replace(clauseBody, "export { round, ceil }", "export { ceil }", 1)

	for _, tc := range []struct {
		name          string
		before, after string
		// boundBefore and boundAfter say whether src/a.ts resolves the call at
		// that point. An unexported origin name leaves it unresolved, so the
		// direction of the edit decides which end can be asserted directly;
		// cold equality is asserted either way and is the real contract.
		boundBefore, boundAfter bool
	}{
		{"export keyword removed", keywordBody, demoted, true, false},
		{"export keyword added", demoted, keywordBody, false, true},
		{"clause member removed", clauseBody, narrowed, true, false},
		{"clause member added", narrowed, clauseBody, false, true},
	} {
		for _, authoritative := range []bool{false, true} {
			mode := "legacy"
			if authoritative {
				mode = "frozen"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				root := setupTSRepo(t, map[string]string{
					"src/a.ts":      "import { pick } from './barrel';\nexport function caller() { return pick(1); }\n",
					"src/barrel.ts": "export { round as pick } from './origin';\n",
					"src/origin.ts": tc.before,
				})
				eng := testEngine(t, root)
				opts := Options{StateDir: filepath.Join(root, ".enola", "live"), AuthoritativeFiles: authoritative}
				cons := bodyScopeStart(t, eng, root, opts)
				if tc.boundBefore {
					assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/origin.ts")
				}

				writeFile(t, root, "src/origin.ts", tc.after)
				bodyScopeDeltaCold(t, eng, root, opts, cons)
				if tc.boundAfter {
					assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/origin.ts")
				}
			})
		}
	}
}

// residentSideReads reads the hashes the session holds in memory, which is the
// copy a watch answers from between generations.
func residentSideReads(t *testing.T, r *Resident, path string) map[string]string {
	t.Helper()
	if r.state == nil {
		t.Fatal("resident holds no state")
	}
	fs := lookupState(r.state.Files, path)
	if fs == nil || fs.TS == nil {
		t.Fatalf("no resident record for %s", path)
	}
	return maps.Clone(fs.TS.SideReadHashes)
}

// The same rollback, but in the session that stays open. A watch keeps its
// committed state in memory across generations, so a transaction that loses its
// End must leave that copy alone as well as the file on disk - otherwise the
// next delta compares against a proof for a parse nobody committed, and the
// following unrelated edit inherits it. The failure is armed only after the
// session has published normally, so this is a lost End mid-watch rather than a
// session that never worked.
func TestBodyScopeResidentProvenSideReadRollsBackWithFailedEnd(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"leaf.ts":   "export function pick(): number {\n  return 1;\n}\n",
		"barrel.ts": "export { pick } from './leaf';\n",
		"use.ts":    "import { pick } from './barrel';\nexport const u = pick();\n",
		"other.ts":  "export const o = 1;\n",
	})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}

	sink := &endFailingSink{err: errors.New("broker unavailable")}
	r, err := OpenSession(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "initial"}); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	before := residentSideReads(t, r, "use.ts")
	if len(before) == 0 {
		t.Fatal("fixture records no side read for use.ts")
	}

	sink.armed.Store(true)
	writeFile(t, root, "leaf.ts", "export function pick(): number {\n  const v = 1;\n  return v;\n}\n")
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "interrupted"}); err == nil {
		t.Fatal("interrupted generation succeeded")
	}
	if got := residentSideReads(t, r, "use.ts"); !maps.Equal(got, before) {
		t.Fatalf("interrupted generation rewrote the resident side-read proof: %v -> %v", before, got)
	}
	if got := committedSideReads(t, state, "use.ts"); !maps.Equal(got, before) {
		t.Fatalf("interrupted generation rewrote the committed side-read proof: %v -> %v", before, got)
	}

	// Recovery replays and reconciles in the same session, and only then is the
	// proof allowed to move.
	sink.armed.Store(false)
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "recovery"}); err != nil {
		t.Fatal(err)
	}
	if got := residentSideReads(t, r, "use.ts"); maps.Equal(got, before) {
		t.Fatalf("recovered generation did not record the proven side read: still %v", got)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	// An unrelated edit afterwards must stand on the recovered state, not on the
	// one the lost End would have written.
	writeFile(t, root, "other.ts", "export const o = 2;\n")
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "unrelated"}); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	coldSink := &graphstream.MemorySink{}
	cold := opts
	cold.StateDir = t.TempDir()
	cold.ForceInitial = true
	if _, err := Run(context.Background(), eng, root, coldSink, cold); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
}
