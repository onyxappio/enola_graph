package graphsession

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/fsnotify/fsnotify"
)

func TestGitControlMetadataRequiresIdenticalCapturedBytes(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	index := filepath.Join(root, ".git", "index")
	if err := os.WriteFile(index, []byte("captured index"), 0644); err != nil {
		t.Fatal(err)
	}
	// A control need not be an index in this unit probe; use HEAD because Build
	// legitimately reads Git's index and should not parse invented index contents.
	os.Remove(index)
	control := filepath.Join(root, ".git", "HEAD")
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewFileChangeSource(root, nil, 16)
	s.policy.Store(policy)
	s.ChangeQueue.Start(context.Background())
	for i := 0; i < 4; i++ {
		s.handleEvent(fsnotify.Event{Name: control, Op: fsnotify.Chmod})
		b := s.Drain()
		if b.Reconcile != "" || len(b.Paths) > 0 {
			t.Fatalf("identical metadata triggered work %+v", b)
		}
	}
	original, _ := os.ReadFile(control)
	os.WriteFile(control, []byte("ref: refs/heads/changed\n"), 0644)
	s.handleEvent(fsnotify.Event{Name: control, Op: fsnotify.Chmod})
	if b := s.Drain(); b.Reconcile == "" {
		t.Fatal("changed bytes hidden by chmod")
	}
	os.WriteFile(control, original, 0644)
	for _, op := range []fsnotify.Op{fsnotify.Write, fsnotify.Rename, fsnotify.Remove, fsnotify.Create, fsnotify.Write | fsnotify.Chmod} {
		s.handleEvent(fsnotify.Event{Name: control, Op: op})
		if b := s.Drain(); b.Reconcile == "" {
			t.Fatalf("control operation %v ignored", op)
		}
	}
	os.Remove(control)
	s.handleEvent(fsnotify.Event{Name: control, Op: fsnotify.Chmod})
	if b := s.Drain(); b.Reconcile == "" {
		t.Fatal("missing control accepted as metadata-only")
	}
}
