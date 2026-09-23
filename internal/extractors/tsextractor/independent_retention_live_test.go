package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIndependentRetentionRechecksLivePackageBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "package.json")
	if err := os.WriteFile(path, []byte(`{"name":"before","dependencies":{"vue":"^3"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	e := New()
	before := e.NewDiscovery(context.Background(), root, nil)
	if before.pkgNames["."] != "before" || !before.vue {
		t.Fatal("fixture did not observe live package")
	}
	if err := os.WriteFile(path, []byte(`{"name":"after"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cold := e.NewDiscovery(context.Background(), root, nil)
	if cold.pkgNames["."] == before.pkgNames["."] || cold.vue == before.vue {
		t.Fatal("fixture did not change cold discovery")
	}
	reused, _ := e.ReuseDiscovery(root, nil, before)
	if reused != nil {
		t.Fatalf("retained stale live package: name=%q vue=%v; cold name=%q vue=%v", reused.pkgNames["."], reused.vue, cold.pkgNames["."], cold.vue)
	}
}

func TestIndependentRetentionRechecksNewPackageDirectory(t *testing.T) {
	root := t.TempDir()
	captured := []byte(`{"name":"root","dependencies":{"typescript":"^5"}}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), captured, 0600); err != nil {
		t.Fatal(err)
	}
	e := New()
	sources := map[string][]byte{"package.json": captured}
	before := e.NewDiscovery(context.Background(), root, sources)
	if _, ok := before.pkgNames["packages/new"]; ok {
		t.Fatal("fixture already contains new package")
	}
	if err := os.MkdirAll(filepath.Join(root, "packages/new"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packages/new/package.json"), []byte(`{"name":"@fixture/new"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cold := e.NewDiscovery(context.Background(), root, sources)
	if cold.pkgNames["packages/new"] != "@fixture/new" {
		t.Fatal("fresh discovery did not observe new package")
	}
	reused, _ := e.ReuseDiscovery(root, sources, before)
	if reused != nil {
		t.Fatal("retained discovery misses newly added package directory")
	}
}

func TestIndependentRetentionRechecksExternalExtendsBytes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := []byte(`{"extends":"../shared.json"}`)
	shared := filepath.Join(parent, "shared.json")
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte(`{"compilerOptions":{"baseUrl":".","paths":{"@fixture/*":["one/*"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	e := New()
	sources := map[string][]byte{"tsconfig.json": cfg}
	before := e.NewDiscovery(context.Background(), root, sources)
	if err := os.WriteFile(shared, []byte(`{"compilerOptions":{"baseUrl":".","paths":{"@fixture/*":["two/*"]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cold := e.NewDiscovery(context.Background(), root, sources)
	if reflect.DeepEqual(before.aliasRoots, cold.aliasRoots) {
		t.Fatal("fixture did not alter cold aliases through external extends")
	}
	reused, _ := e.ReuseDiscovery(root, sources, before)
	if reused != nil {
		t.Fatal("retained stale alias roots after external extends byte change")
	}
}
