package graphstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestDigestOwnersStable(t *testing.T) {
	a := []OwnerRef{{Kind: OwnerFile, ID: "b.ts"}, {Kind: OwnerFile, ID: "a.ts"}}
	b := []OwnerRef{{Kind: OwnerFile, ID: "a.ts"}, {Kind: OwnerFile, ID: "b.ts"}}
	if DigestOwners(a) != DigestOwners(b) {
		t.Fatal("owner digest must be order-independent")
	}
	if DigestOwners(a) == DigestOwners(a[:1]) {
		t.Fatal("distinct manifests must not collide")
	}
}

func TestMarshalBeginReplaceRoundTrip(t *testing.T) {
	b := BeginReplace{
		Type:             TypeBeginReplace,
		SchemaVersion:    SchemaVersion,
		RepoID:           "repo",
		ContextID:        "main",
		RunID:            "run-1",
		BaseGeneration:   0,
		TargetGeneration: 1,
		Phase:            PhaseResolved,
		OwnerScope:       []OwnerRef{{Kind: OwnerFile, ID: "src/a.ts"}},
	}
	raw, err := Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var got BeginReplace
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != SchemaVersion || got.RunID != "run-1" || got.OwnerScope[0].ID != "src/a.ts" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	again, err := Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatal("replay of the same BeginReplace must be byte-identical")
	}
}

func TestJournalReplayIdenticalPayload(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"type":"batch","run_id":"r","seq":1}`)
	if err := j.Append(JournalEntry{MsgID: "r:batch:1", Subject: "enola.graph.run", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	unacked := j.Unacked()
	if len(unacked) != 1 || !bytes.Equal(unacked[0].Payload, payload) {
		t.Fatalf("unacked = %+v", unacked)
	}

	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j, Subject: "enola.graph.run"}
	if err := p.ReplayUnacked(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sink.CloneRecords()
	if len(got) != 1 || got[0].MsgID != "r:batch:1" || !bytes.Equal(got[0].Payload, payload) {
		t.Fatalf("replayed %+v", got)
	}
	if n := len(j.Unacked()); n != 0 {
		t.Fatalf("unacked after replay = %d", n)
	}

	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(j2.Unacked()); n != 0 {
		t.Fatalf("reopened journal still unacked = %d", n)
	}
}

func TestPublisherRetryAfterSinkFailure(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("broker down")
	sink := &MemorySink{}
	sink.FailAt(1, boom)
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	payload := []byte(`{"type":"begin_replace"}`)
	if err := p.Publish(context.Background(), "id-1", payload); err == nil {
		t.Fatal("expected publish failure")
	}
	if n := len(j.Unacked()); n != 1 {
		t.Fatalf("unacked after fail = %d", n)
	}
	sink.FailAt(0, nil)
	if err := p.ReplayUnacked(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sink.CloneRecords()
	if len(got) != 1 || !bytes.Equal(got[0].Payload, payload) {
		t.Fatalf("retry payload %+v", got)
	}
}

func TestJournalRejectsConflictingRetry(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "x", Subject: "s", Payload: []byte("A")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "x", Subject: "s", Payload: []byte("B")}); err == nil {
		t.Fatal("expected conflict")
	}
	if err := j.Append(JournalEntry{MsgID: "x", Subject: "s", Payload: []byte("A")}); err != nil {
		t.Fatal(err)
	}
}

func TestJournalCopiesPayload(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	buf := []byte("A")
	if err := j.Append(JournalEntry{MsgID: "x", Subject: "s", Payload: buf}); err != nil {
		t.Fatal(err)
	}
	buf[0] = 'B'
	got := j.Unacked()
	if !bytes.Equal(got[0].Payload, []byte("A")) {
		t.Fatalf("payload mutated: %s", got[0].Payload)
	}
}

func TestJournalAckFirstEntryOnRetry(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	sink.FailAt(1, errors.New("fail"))
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	if err := p.Publish(context.Background(), "id-1", []byte("A")); err == nil {
		t.Fatal("expected fail")
	}
	sink.FailAt(0, nil)
	if err := p.Publish(context.Background(), "id-1", []byte("A")); err != nil {
		t.Fatal(err)
	}
	if n := len(j.Unacked()); n != 0 {
		t.Fatalf("unacked = %d", n)
	}
}

func TestOpenJournalMissingFile(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Entries()) != 0 {
		t.Fatal("expected empty journal")
	}
}
