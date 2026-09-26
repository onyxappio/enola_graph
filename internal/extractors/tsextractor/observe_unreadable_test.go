package tsextractor

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryWalkIgnoredReadErrorDoesNotProveEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	empty := filepath.Join(root, "empty")
	for _, dir := range []string{blocked, empty} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(blocked, "nuxt.config.ts")
	if err := os.WriteFile(config, []byte("export default {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0700) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("host privileges allow reading mode-000 directories")
	}
	probe := newOverlayProbe()
	ctx := withOverlayProbe(context.Background(), probe)
	sawReadError := false
	err := overlayWalkDir(ctx, root, nil, func(path string, entry fs.DirEntry, err error) error {
		if path == blocked && err != nil {
			sawReadError = true
		}
		return nil // discovery deliberately continues past inaccessible subtrees
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawReadError {
		t.Fatal("fixture did not exercise ignored directory read error")
	}
	dirs := probe.dirSnapshot()
	if _, recorded := dirs[absOverlayKey(blocked)]; recorded {
		t.Fatal("failed enumeration was recorded as a complete empty directory")
	}
	if children, recorded := dirs[absOverlayKey(empty)]; !recorded || len(children) != 0 {
		t.Fatalf("successfully enumerated empty directory lost: %v, %v", recorded, children)
	}
	if got := dirs[absOverlayKey(root)]["blocked"]; got != observedEntryDir {
		t.Fatalf("parent membership observation lost: %q", got)
	}
	if err := os.Chmod(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if !detectNuxtAt(ctx, blocked) {
		t.Fatal("direct probe must discover config after directory access is restored")
	}
}
