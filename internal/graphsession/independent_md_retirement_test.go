package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentRetiredMarkdownKeepsUnrelatedOwnersOut(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;\n", "docs/guide.md": "# Guide\n\n[Old](old.md)\n", "docs/old.md": "# Old\n\nProse.\n"})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !ownsFile(cons, "docs/old.md") {
		t.Fatal("missing initial owner")
	}
	if err := os.Remove(filepath.Join(root, "docs/old.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/old.md")
	if ownsFile(cons, "docs/old.md") {
		t.Fatal("retired contribution survived")
	}
	if owners["src/a.ts"] || owners["file:src/a.ts"] {
		t.Fatalf("unrelated source in deleted-page replacement: %v", ids)
	}
}

func TestIndependentRetiredMarkdownRetargetsUnchangedNameConsumer(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":     "export const a=1;\n",
		"docs/a.md":    "---\nenola_intent:\n  page:\n    type: decision\n    status: living\n    relations:\n      - {rel: depends-on, to: docs/b.md}\n---\n\n# A\n\nProse.\n",
		"docs/b.md":    "# B\n\nProse.\n",
		"docs/keep.md": "# Keep\n\nProse.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !ownsSection(cons, "docs/b.md") {
		t.Fatal("missing initial document candidate")
	}
	if err := os.Remove(filepath.Join(root, "docs/b.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	result, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/a.md", "docs/b.md")
	if ownsFile(cons, "docs/b.md") {
		t.Fatal("deleted candidate contribution survived")
	}
	if owners["src/a.ts"] || owners["file:src/a.ts"] {
		t.Fatalf("unrelated source in candidate retirement: %v", ids)
	}
	requireNoWholeDomainFallback(t, result, ids)
}
