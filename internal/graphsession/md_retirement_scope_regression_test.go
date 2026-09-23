package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

// Retiring a published owner that was never a TypeScript source used to widen
// the frozen manifest to the whole prior/current domain: the cached import
// graph cannot enumerate a markdown page's consumers, so membershipScope
// refused every such deletion outright.
//
// It is not the import graph that answers this. The extractor that owns the
// page is previewed before Begin in the same run - from captured bytes, or
// from the conservative owned+retired seed - and both routes close over every
// cached owner naming a candidate the extractor withdrew, which is the index
// global Fact.Name resolution actually uses. These cases hold that proof to its
// terms: the pages whose facts really move are in Begin, unrelated sources are
// not, the applied graph equals a cold session, and a retirement no extractor
// proved this run still takes the broad fallback.

// requireWholeDomainFallback is the inverse of requireNoWholeDomainFallback: a
// retirement without a pre-Begin proof must fail closed, and a case that only
// checked cold equality would pass just as well if the fallback silently
// disappeared.
func requireWholeDomainFallback(t *testing.T, res *Result, reason string) {
	t.Helper()
	for _, fb := range res.Fallbacks {
		if fb.Scope == "all prior/current file owners" && fb.Reason == reason {
			return
		}
	}
	t.Fatalf("unproven retirement did not fall back to the whole domain with %q: %+v", reason, res.Fallbacks)
}

// resolvedTargetFiles reports which files the resolved edges owned by one file
// point into. Identities are opaque hashes, so a case that wants to say a link
// rebound from one page to another has to go back through the node that
// answered to it.
func resolvedTargetFiles(c *Consumer, owner string) map[string]bool {
	out := map[string]bool{}
	for key, edges := range c.Edges {
		if key != owner && key != "file:"+owner {
			continue
		}
		for _, e := range edges {
			if e.Resolution != "resolved" || e.TargetID == "" {
				continue
			}
			if f := nodeFileByID(c, e.TargetID); f != "" {
				out[f] = true
			}
		}
	}
	return out
}

// The page that linked to the deleted one is the consumer the import graph
// cannot see. It must be inside Begin - its link no longer resolves - while a
// TypeScript source that never mentioned either page stays out.
func TestMDRetirementDeletedLinkTargetIncludesReferrerNotUnrelatedSources(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe old page is [old](old.md).\n",
		"docs/old.md":   "# Old\n\nProse.\n",
		"notes/keep.md": "# Keep\n\nNothing links out.\n",
	})
	eng, md := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !resolvedTargetFiles(cons, "docs/guide.md")["docs/old.md"] {
		t.Fatal("fixture produced no resolved link to retire")
	}

	if err := os.Remove(filepath.Join(root, "docs/old.md")); err != nil {
		t.Fatal(err)
	}
	compiled := md.ExtractCalls()
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	// A delta that changes markdown output already compiles the pages twice on
	// main: once for the neutrality proof that tries to discharge the need, and
	// once for the planning preview. Planning the retirement instead of falling
	// back must not add a third read of the tree.
	if got := md.ExtractCalls() - compiled; got > 2 {
		t.Fatalf("mdintent compiled %d times in one delta, want no more than the two main already performs", got)
	}

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/old.md", "docs/guide.md")
	// Pages sharing the deleted page's directory carry the same directory
	// module identity, so docs/keep.md would be in Begin on its own account.
	// The unrelated page is in another directory, where nothing moved.
	forbidOwners(t, owners, ids, "src/a.ts", "file:src/a.ts", "notes/keep.md")
	requireNoWholeDomainFallback(t, delta, ids)
	if ownsFile(cons, "docs/old.md") {
		t.Fatal("the deleted page kept its contribution")
	}
	if got := resolvedTargetFiles(cons, "docs/guide.md"); got["docs/old.md"] {
		t.Fatalf("the referrer still resolves a link to the deleted page: %v", got)
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A relative link is resolved against the referring page's directory first and
// the repository root second, so deleting the nearer target does not take the
// link away - it rebinds it to the other file. The referrer's own bytes never
// change, and no import edge connects it to either target.
func TestMDRetirementRebindsLinkToRepositoryRootTarget(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe old page is [old](old.md).\n",
		"docs/old.md":   "# Docs old\n\nProse.\n",
		"old.md":        "# Root old\n\nProse.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if before := resolvedTargetFiles(cons, "docs/guide.md"); !before["docs/old.md"] || before["old.md"] {
		t.Fatalf("fixture did not bind the link to the nearer page: %v", before)
	}

	if err := os.Remove(filepath.Join(root, "docs/old.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/old.md", "docs/guide.md")
	forbidOwners(t, owners, ids, "src/a.ts", "file:src/a.ts")
	requireNoWholeDomainFallback(t, delta, ids)
	after := resolvedTargetFiles(cons, "docs/guide.md")
	if !after["old.md"] || after["docs/old.md"] {
		t.Fatalf("the link did not rebind to the repository-root page: %v", after)
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A rename is a retirement and an addition in the same run. The old identity
// has to be retired from the manifest, the new one published, and the page that
// still links to the old name has to be reparsed to lose its resolution.
func TestMDRetirementRenamedPageRetiresTheOldIdentity(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe old page is [old](old.md).\n",
		"docs/old.md":   "# Old\n\n## Detail\n\nProse.\n",
		"notes/keep.md": "# Keep\n\nNothing links out.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !ownsSection(cons, "docs/old.md#detail") {
		t.Fatal("fixture produced no section to carry across the rename")
	}

	if err := os.Rename(filepath.Join(root, "docs/old.md"), filepath.Join(root, "docs/new.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/old.md", "docs/new.md", "docs/guide.md")
	forbidOwners(t, owners, ids, "src/a.ts", "file:src/a.ts", "notes/keep.md")
	requireNoWholeDomainFallback(t, delta, ids)
	if ownsSection(cons, "docs/old.md#detail") {
		t.Fatal("the renamed page kept its old section identity")
	}
	if !ownsSection(cons, "docs/new.md#detail") {
		t.Fatal("the renamed page did not publish its new identity")
	}
	if got := resolvedTargetFiles(cons, "docs/guide.md"); got["docs/old.md"] || got["docs/new.md"] {
		t.Fatalf("the referrer still resolves a link to the renamed page: %v", got)
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// The proof is per extractor and it is all-or-nothing. Deleting the last page
// takes mdintent out of detection entirely, so nothing previews the retirement
// and nothing enumerates its consumers: the run must keep the broad fallback
// rather than narrow on a proof it never obtained.
func TestMDRetirementWithoutAnyExtractorProofKeepsTheBroadFallback(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nProse.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !ownsFile(cons, "docs/guide.md") {
		t.Fatal("fixture produced no markdown owner to retire")
	}

	if err := os.Remove(filepath.Join(root, "docs/guide.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/guide.md")
	requireWholeDomainFallback(t, delta, frozenScopeMembership)
	if ownsFile(cons, "docs/guide.md") {
		t.Fatal("the deleted page kept its contribution")
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// The proven retirement must leave the session in a state that settles. A
// second run over the same tree has nothing to do, and a retirement that
// refreshed its observations badly would show up here as a republication.
func TestMDRetirementLeavesATrueNoop(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe old page is [old](old.md).\n",
		"docs/old.md":   "# Old\n\nProse.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	if err := os.Remove(filepath.Join(root, "docs/old.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	owners, ids := beginScope(t, sink)
	requireNoWholeDomainFallback(t, delta, ids)
	forbidOwners(t, owners, ids, "src/a.ts", "file:src/a.ts")

	noop := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, root, noop, opts)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPublication(t, second, noop, delta.TargetGeneration, "the run after a proven retirement")

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// The helper itself, away from a session: a path is proven only when every
// extractor that wrote its cached contributions was previewed. One previewed
// extractor does not speak for the other's consumers.
func TestProvenRetiredOwnersRequiresEveryContributingExtractor(t *testing.T) {
	prevFiles := map[string]*FileState{
		"docs/only.md":   {Extractor: "mdintent"},
		"docs/shared.md": {Extractor: "mdintent", Contrib: map[string][]facts.Fact{"other": nil}},
		"docs/hashed.md": {Extractor: "mdintent", ContribHash: map[string]string{"other": "h"}},
		"src/a.ts":       {Extractor: "typescript"},
		"docs/none.md":   {},
	}
	proven := provenRetiredOwners(prevFiles, map[string]bool{"mdintent": true})
	if !proven["docs/only.md"] {
		t.Fatal("a page owned only by the previewed extractor was not proven")
	}
	for _, path := range []string{"docs/shared.md", "docs/hashed.md", "src/a.ts", "docs/none.md"} {
		if proven[path] {
			t.Fatalf("%s was proven without every contributing extractor being previewed", path)
		}
	}
	if got := provenRetiredOwners(prevFiles, nil); len(got) != 0 {
		t.Fatalf("a run that previewed nothing proved %v", got)
	}
}
