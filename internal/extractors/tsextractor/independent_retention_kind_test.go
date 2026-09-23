package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentRetentionRechecksEntryKind(t *testing.T) {
	root := t.TempDir()
	captured := []byte(`{"name":"root","dependencies":{"typescript":"^5"}}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), captured, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "packages")
	if err := os.WriteFile(path, []byte("placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	e := New()
	sources := map[string][]byte{"package.json": captured}
	before := e.NewDiscovery(context.Background(), root, sources)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "new"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "new/package.json"), []byte(`{"name":"@fixture/new"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cold := e.NewDiscovery(context.Background(), root, sources)
	if cold.pkgNames["packages/new"] != "@fixture/new" {
		t.Fatal("cold fixture did not discover new package")
	}
	reused, _ := e.ReuseDiscovery(root, sources, before)
	if reused != nil {
		t.Fatal("retained discovery accepted file-to-directory transition with unchanged parent name set")
	}
}
