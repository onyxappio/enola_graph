package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGraphTargetRepositoryDiscoveryAndExplicitConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mcp-arch.yaml"), []byte("extractors: [typescript]\nignore: [hidden/**]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &Runner{}
	target := r.resolveGraphTarget(root, "", nil)
	if target.engine.Config().SourcePath != filepath.Join(root, "mcp-arch.yaml") {
		t.Fatal("repo config not discovered")
	}
	explicit := filepath.Join(t.TempDir(), "graph.yaml")
	if err := os.WriteFile(explicit, []byte("repo: "+root+"\nextractors: [typescript]\nignore: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{root, explicit}, {explicit, ""}} {
		target = r.resolveGraphTarget(pair[0], pair[1], nil)
		if target.engine.Config().SourcePath != explicit || target.repoPaths[0] != root {
			t.Fatalf("wrong explicit target %+v", target)
		}
	}
}
