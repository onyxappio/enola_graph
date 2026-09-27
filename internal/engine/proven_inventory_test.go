package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

func TestProvenInventoryPreservesWalkOrderAndPruning(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a/child.ts", "a-/sibling.ts", "a.ts", "pruned/hidden.ts", "pruned-more/visible.ts", "explicit/hidden.ts", "file.test.ts", "pnpm-lock.yaml", "node_modules/hidden.ts", "media.png"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	p, err := graphinput.Build(root, graphinput.Options{Exclude: []string{"explicit/**"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Ignore = []string{"pruned/**", "**/*.test.ts"}
	cfg.TestGlobs = []string{"**/*.test.ts"}
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.ConfigureGraphInputs(&inputscope.Scope{Root: root, Policy: p}, nil)
	want, err := e.Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	got, why, ok := e.ProvenInventory(root)
	if !ok || !reflect.DeepEqual(want, got) {
		t.Fatalf("proof=%v (%s)\nwant=%#v\ngot=%#v", ok, why, want, got)
	}
	if len(got.TestFiles) != 1 {
		t.Fatalf("lost reference-only test: %#v", got)
	}
	// Inventory prunes this directory; the policy proof must still detect names
	// appearing inside it. A failed proof must not leak its partial inventory.
	if err := os.WriteFile(filepath.Join(root, "pruned/new.ts"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	got, why, ok = e.ProvenInventory(root)
	if ok || why == "" || !reflect.DeepEqual(got, RepoInventory{}) {
		t.Fatalf("accepted changed pruned tree: %#v, %s, %v", got, why, ok)
	}
}

func TestProvenInventoryRejectsDifferentRoot(t *testing.T) {
	root := t.TempDir()
	p, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	e.ConfigureGraphInputs(&inputscope.Scope{Root: root, Policy: p}, nil)
	got, _, ok := e.ProvenInventory(t.TempDir())
	if ok || !reflect.DeepEqual(got, RepoInventory{}) {
		t.Fatalf("accepted wrong root: %#v", got)
	}
}
