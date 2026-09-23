package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/graphstream"
)

// These cases exercise membership planning through real Analyze/Delta runs and
// judge every one of them against a fresh cold session, because a delta that
// merely finishes without error can still have skipped an owner. Each case
// asserts the concrete Begin owner scope as well, so a planner that only
// happens to be right by way of a whole-domain fallback does not pass.

// beginScope returns the owner IDs the single replacement of a run froze at
// Begin, after checking that the scope did not grow before End.
func beginScope(t *testing.T, sink *graphstream.MemorySink) (map[string]bool, []string) {
	t.Helper()
	bs, _, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) != 1 || len(ends) != 1 {
		t.Fatalf("expected one Begin/End pair: begins=%d ends=%d err=%v", len(bs), len(ends), err)
	}
	if bs[0].OwnerScopeCount != len(bs[0].OwnerScope) {
		t.Fatalf("Begin manifest count %d does not match its inline scope %d", bs[0].OwnerScopeCount, len(bs[0].OwnerScope))
	}
	if bs[0].OwnerScopeDigest != ends[0].OwnerScopeDigest || bs[0].OwnerScopeCount != ends[0].OwnerScopeLen {
		t.Fatalf("owner scope grew after Begin: begin=%d/%s end=%d/%s",
			bs[0].OwnerScopeCount, bs[0].OwnerScopeDigest, ends[0].OwnerScopeLen, ends[0].OwnerScopeDigest)
	}
	owners := map[string]bool{}
	ids := make([]string, 0, len(bs[0].OwnerScope))
	for _, o := range bs[0].OwnerScope {
		owners[o.ID] = true
		ids = append(ids, o.ID)
	}
	sort.Strings(ids)
	return owners, ids
}

func requireOwners(t *testing.T, owners map[string]bool, ids []string, want ...string) {
	t.Helper()
	for _, id := range want {
		if !owners[id] {
			t.Fatalf("owner %q missing from Begin scope %v", id, ids)
		}
	}
}

func forbidOwners(t *testing.T, owners map[string]bool, ids []string, unwanted ...string) {
	t.Helper()
	for _, id := range unwanted {
		if owners[id] {
			t.Fatalf("unrelated owner %q widened Begin scope %v", id, ids)
		}
	}
}

// requireNoWholeDomainFallback keeps a case honest: covering every owner by
// falling back to the whole name-resolution domain is safe but says nothing
// about membership planning, which is what these cases are about.
func requireNoWholeDomainFallback(t *testing.T, res *Result, ids []string) {
	t.Helper()
	for _, fb := range res.Fallbacks {
		if fb.Scope == "all prior/current file owners" {
			t.Fatalf("delta fell back to the whole domain (%s); Begin scope was %v", fb.Reason, ids)
		}
	}
}

// coldConsumer is the oracle: a session with no prior state at all.
func coldConsumer(t *testing.T, eng *engine.Engine, root string) *Consumer {
	t.Helper()
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	if err := cold.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	return cold
}

// nodeFileByID finds which file declared the fact an edge resolved to.
func nodeFileByID(c *Consumer, id string) string {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.ID == id {
				return n.File
			}
		}
	}
	return ""
}

func resolvedCallTarget(t *testing.T, c *Consumer, owner string) string {
	t.Helper()
	for _, e := range c.Edges[owner] {
		if e.Kind == "calls" && e.Resolution == "resolved" {
			return e.TargetID
		}
	}
	t.Fatalf("no resolved call edge owned by %s: %+v", owner, c.Edges[owner])
	return ""
}

func ownsFile(c *Consumer, file string) bool {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.File == file {
				return true
			}
		}
	}
	return false
}

// A repository always holds inputs that never emit a fact - tsconfig.json here,
// plus an explicitly empty JSON file. Those are absent from the prior
// contribution map, so a planner that reads "not previously an owner" as "newly
// added" sees the whole repository as new on every delta. Adding one unrelated
// helper must still scope to that helper, and must still equal cold.
func TestMembershipColdAddUnrelatedHelperWithEmptyJSONInput(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"a.ts":            "export const a = 1;\n",
		"independent.ts":  "export const i = 1;\n",
		"data/empty.json": "{}\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "helper.ts"), []byte("export const helper = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "helper.ts")
	forbidOwners(t, owners, ids, "a.ts", "independent.ts", "data/empty.json", "tsconfig.json", "package.json")
	requireNoWholeDomainFallback(t, delta, ids)
	if delta.ParsedFiles != 1 {
		t.Fatalf("parsed=%d, want only the added helper", delta.ParsedFiles)
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// Adding foo.ts next to foo/index.ts changes nothing about use.ts on disk, yet
// './foo' now resolves to the new file: the exact/extension/folder-index
// precedence in resolveModuleFile prefers it. The importer's resolved call edge
// must move to the fact declared by foo.ts, which only happens if the importer
// was in the frozen Begin scope and reparsed.
func TestMembershipColdFileModuleShadowsFolderIndexRebindsImporter(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"foo/index.ts": "export const value = 'index';\n",
		"use.ts":       "import { value } from './foo';\nexport const used = value;\n",
		"other.ts":     "export const other = 1;\n",
	})
	eng := testEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if before := nodeFileByID(cons, resolvedCallTarget(t, cons, "file:use.ts")); before != "foo/index.ts" {
		t.Fatalf("importer initially bound to %q, want foo/index.ts", before)
	}

	if err := os.WriteFile(filepath.Join(root, "foo.ts"), []byte("export const value = 'file';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "foo.ts", "use.ts")
	forbidOwners(t, owners, ids, "other.ts")
	requireNoWholeDomainFallback(t, delta, ids)

	cold := coldConsumer(t, eng, root)
	if after := nodeFileByID(cons, resolvedCallTarget(t, cons, "file:use.ts")); after != "foo.ts" {
		t.Fatalf("importer still bound to %q after the shadowing add, want foo.ts", after)
	}
	if want := nodeFileByID(cold, resolvedCallTarget(t, cold, "file:use.ts")); want != "foo.ts" {
		t.Fatalf("cold session binds the importer to %q, want foo.ts", want)
	}
	assertAppliedEqualsCold(t, cons, cold)
}

// Renaming src/leaf.ts retires that identity for every consumer, including ones
// that never imported it: docs/guide.md links the path by name through the
// markdown extractor. The retired old path has to reach the name delta as an
// old->empty contribution so the markdown owner lands inside Begin, and nothing
// owned by the old path may survive in the applied graph.
func TestMembershipColdRenamedSourceRetiresIdentityForGlobalNameConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/leaf.ts":   "export const leaf = 1;\n",
		"src/mid.ts":    "import { leaf } from './leaf';\nexport const mid = leaf;\n",
		"src/top.ts":    "import { mid } from './mid';\nexport const top = mid;\n",
		"docs/guide.md": "# Guide\n\nThe leaf lives in [leaf](../src/leaf.ts).\n",
		"other.ts":      "export const other = 1;\n",
	})
	eng := mdintentEngine(t, root, true)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if !ownsFile(cons, "src/leaf.ts") {
		t.Fatal("fixture produced no fact owned by src/leaf.ts")
	}

	if err := os.Rename(filepath.Join(root, "src/leaf.ts"), filepath.Join(root, "src/core.ts")); err != nil {
		t.Fatal(err)
	}
	mid := "import { leaf } from './core';\nexport const mid = leaf;\n"
	if err := os.WriteFile(filepath.Join(root, "src/mid.ts"), []byte(mid), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "src/leaf.ts", "src/core.ts", "src/mid.ts", "src/top.ts", "docs/guide.md")
	forbidOwners(t, owners, ids, "other.ts")
	requireNoWholeDomainFallback(t, delta, ids)
	if ownsFile(cons, "src/leaf.ts") {
		t.Fatal("facts owned by the renamed-away src/leaf.ts survived the delta")
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
