package graphstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestNATSSinkPublishAckAndReplay(t *testing.T) {
	bin := natsServerBin()
	if bin == "" {
		t.Skip("nats-server not available")
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "js")
	port := freePort(t)
	cmd := exec.Command(bin, "--jetstream", "--store_dir", store, "--port", fmt.Sprintf("%d", port), "--addr", "127.0.0.1")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start nats-server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	url := fmt.Sprintf("nats://127.0.0.1:%d", port)
	var sink *NATSSink
	var err error
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Fatal("nats-server exited")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		sink, err = ConnectNATS(ctx, NATSOptions{URL: url, Stream: "ENOLA_GRAPH_TEST", Subject: "enola.graph.>"})
		cancel()
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sink.Close()

	begin := BeginReplace{
		Type:             TypeBeginReplace,
		SchemaVersion:    SchemaVersion,
		RepoID:           "r",
		ContextID:        "c",
		RunID:            "run-nats",
		TargetGeneration: 1,
		Phase:            PhaseResolved,
		OwnerScope:       []OwnerRef{{Kind: OwnerFile, ID: "a.ts"}},
	}
	payload, err := Marshal(begin)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	msgID := MessageID("run-nats", TypeBeginReplace, 0)
	subj := SubjectForRun("r", "c", "run-nats")
	if err := sink.Publish(ctx, subj, msgID, payload); err != nil {
		t.Fatal(err)
	}
	if err := sink.Publish(ctx, subj, msgID, payload); err != nil {
		t.Fatal(err)
	}

	cons, err := sink.JS().CreateOrUpdateConsumer(ctx, "ENOLA_GRAPH_TEST", jetstream.ConsumerConfig{
		Durable:       "test-reader",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subj,
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := cons.Next(jetstream.FetchMaxWait(3 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var decoded BeginReplace
	if err := json.Unmarshal(msg.Data(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RunID != "run-nats" {
		t.Fatalf("decoded %+v", decoded)
	}
	if err := msg.Ack(); err != nil {
		t.Fatal(err)
	}
}

func TestConcretePublishSubjectStaysInWildcard(t *testing.T) {
	got := ConcretePublishSubject("acceptance.custom.>", "r", "c", "run-1")
	if got == SubjectForRun("r", "c", "run-1") {
		t.Fatal("custom pattern collapsed onto default enola.graph subject")
	}
	if !(len(got) > len("acceptance.custom.") && got[:len("acceptance.custom.")] == "acceptance.custom.") {
		t.Fatalf("subject %q not under acceptance.custom.", got)
	}
}

func TestSubjectForRunIsCollisionFree(t *testing.T) {
	if SubjectForRun("a.b", "main", "r") == SubjectForRun("a_b", "main", "r") {
		t.Fatal("dot and underscore collapsed")
	}
	if SubjectForRun("feature/x", "c", "r") == SubjectForRun("feature_x", "c", "r") {
		t.Fatal("slash and underscore collapsed")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func natsServerBin() string {
	candidates := []string{
		os.Getenv("NATS_SERVER"),
		"/tmp/enola-toolchain/bin/nats-server",
		"nats-server",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if c == "nats-server" {
			if p, err := exec.LookPath(c); err == nil {
				return p
			}
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
