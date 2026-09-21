package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIndependentUnsyncedAdmissionCannotPromote(t *testing.T) {
	if dir := os.Getenv("ENOLA_REVIEW_LOST_QUEUE_DIR"); dir != "" {
		old := newState("/repo", "c", "/repo", "v")
		old.Generation = 1
		old.LastComplete = true
		old.LastRunID = "old"
		if err := saveState(dir, old); err != nil {
			t.Fatal(err)
		}
		pending := newState("/repo", "c", "/repo", "v")
		pending.Generation = 2
		pending.LastComplete = true
		pending.LastRunID = "new"
		if err := writePendingState(dir, pending); err != nil {
			t.Fatal(err)
		}
		j, err := graphstream.OpenJournal(filepath.Join(dir, "journal"))
		if err != nil {
			t.Fatal(err)
		}
		entered := make(chan struct{})
		j.SetSyncHook(func() { close(entered); select {} })
		p := &graphstream.Publisher{Journal: j, Sink: &graphstream.MemorySink{}, Subject: "s"}
		p.EnableAsync(16, 1<<20)
		if err := p.Publish(context.Background(), "new:batch:1", []byte(`{"type":"batch","run_id":"new","seq":1}`)); err != nil {
			t.Fatal(err)
		}
		<-entered
		if err := p.Publish(context.Background(), "new:end_replace:2", []byte(`{"type":"end_replace","run_id":"new"}`)); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIndependentUnsyncedAdmissionCannotPromote$")
	cmd.Env = append(os.Environ(), "ENOLA_REVIEW_LOST_QUEUE_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash child: %v %s", err, out)
	}
	j, err := graphstream.OpenJournal(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if j.HasAckedEnd("new") {
		t.Fatal("lost volatile End invented durable ack")
	}
	st, err := recoverAcknowledgedPending(dir, j, Options{RepoID: "/repo", ContextID: "c"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.Generation != 1 {
		t.Fatalf("phantom completed generation: %+v", st)
	}
}
