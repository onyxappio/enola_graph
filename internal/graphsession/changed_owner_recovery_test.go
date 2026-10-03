package graphsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

type failChangedOwnerEnd struct {
	graphstream.MemorySink
	failed graphstream.Recorded
}

func (s *failChangedOwnerEnd) Publish(ctx context.Context, subject, id string, payload []byte) error {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return err
	}
	if envelope.Type == graphstream.TypeEndReplace {
		s.failed = graphstream.Recorded{Subject: subject, MsgID: id, Payload: append([]byte(nil), payload...)}
		return errors.New("injected End delivery failure")
	}
	return s.MemorySink.Publish(ctx, subject, id, payload)
}

func TestChangedOwnerScopeEndFailureKeepsConfirmedDigests(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export function a(){return 1}"})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ChangedOwnersOnly: true}
	cons := NewConsumer()
	admissionRun(t, eng, root, opts, cons)
	path := filepath.Join(opts.StateDir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "src/a.ts", "export function a(){return 1}; export function added(){return 2}")
	failing := &failChangedOwnerEnd{}
	_, err = Run(context.Background(), eng, root, failing, opts)
	if err == nil || failing.failed.MsgID == "" {
		t.Fatalf("End failure not exercised: %v", err)
	}
	var failedEnd graphstream.EndReplace
	if err := json.Unmarshal(failing.failed.Payload, &failedEnd); err != nil {
		t.Fatal(err)
	}
	if failedEnd.Statistics == nil || failedEnd.Statistics.TransactionDurationNS <= 0 {
		t.Fatal("failed delivery lost original End statistics")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unacknowledged run promoted checkpoint/digests")
	}
	if _, err := os.Stat(filepath.Join(opts.StateDir, "pending-state.json")); err != nil {
		t.Fatalf("pending checkpoint absent: %v", err)
	}
	if err := cons.ApplyRecords(failing.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	result, sink := admissionRun(t, eng, root, opts, cons)
	replayed := false
	for _, record := range sink.CloneRecords() {
		if record.MsgID == failing.failed.MsgID {
			replayed = true
			if !bytes.Equal(record.Payload, failing.failed.Payload) {
				t.Fatal("replayed End bytes changed")
			}
		}
	}
	if !replayed {
		t.Fatal("failed End was not replayed")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	unchanged, quiet := admissionRun(t, eng, root, opts, cons)
	assertNoPublication(t, unchanged, quiet, result.TargetGeneration, "recovered no-op")
}
