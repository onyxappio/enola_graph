package graphstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc64"
	"os"
	"path/filepath"
	"testing"
)

func TestJournalLegacyValidJSONWithoutNewlineRetained(t *testing.T) {
	dir := t.TempDir()
	row, err := json.Marshal(payloadLine{MsgID: "committed", Subject: "s", Payload: []byte("durable")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), row, 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirFingerprint(t, dir)
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if dirFingerprint(t, dir) != before {
		t.Fatal("open must not mutate a valid JSON tail that lacks a newline")
	}
	got := j.Unacked()
	if len(got) != 1 || got[0].MsgID != "committed" || !bytes.Equal(got[0].Payload, []byte("durable")) {
		t.Fatalf("retained %+v", got)
	}
}

func TestJournalCommitIndexRejectsTruncatedNewline(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "committed", Subject: "s", Payload: []byte("durable")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "payloads.jsonl")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("expected newline-framed payload, got %q", b)
	}
	if err := os.WriteFile(p, b[:len(b)-1], 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = OpenJournal(dir)
	if err == nil {
		t.Fatal("expected committed tail damage to be rejected")
	}
}

func TestJournalMalformedFinalRowWithNewlineRejected(t *testing.T) {
	dir := t.TempDir()
	ok, err := json.Marshal(payloadLine{MsgID: "keep", Subject: "s", Payload: []byte("ok")})
	if err != nil {
		t.Fatal(err)
	}
	raw := append(append([]byte{}, ok...), '\n')
	raw = append(raw, []byte("not-json\n")...)
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(dir); err == nil {
		t.Fatal("malformed final row with newline must be committed corruption")
	}
}

func TestJournalExactTombsBoundAndConflict(t *testing.T) {
	dir := t.TempDir()
	j, err := openJournal(dir, 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	first := []byte("AAAA")
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: first}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("same"); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), 100)
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("fill-%06d", i)
		if err := j.appendUnsynced(JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
			if _, ok := j.Get("same"); ok {
				t.Fatalf("identity bound before live GC of same: %v", err)
			}
			break
		}
		if err := j.ackUnsynced(id); err != nil {
			if _, ok := j.Get("same"); ok {
				t.Fatalf("identity bound before live GC of same: %v", err)
			}
			break
		}
	}
	if err := j.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, ok := j.Get("same"); ok {
		t.Fatal("expected live GC to drop byID for acked identity")
	}
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: []byte("BBBB")}); err == nil {
		t.Fatal("conflicting reuse after packed ident GC must fail")
	}
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: first}); err != nil {
		t.Fatal(err)
	}
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	if err := j2.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: []byte("BBBB")}); err == nil {
		t.Fatal("conflicting reuse after reopen must fail")
	}
	if err := j2.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: first}); err != nil {
		t.Fatal(err)
	}
}

func TestJournalCompactAckedFencesProtocolRun(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	run := "run-fence"
	if err := j.Append(JournalEntry{MsgID: MessageID(run, TypeBatch, 1), Subject: "s", Payload: []byte(`{"type":"batch","run_id":"run-fence","seq":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack(MessageID(run, TypeBatch, 1)); err != nil {
		t.Fatal(err)
	}
	end := endPayload(run)
	if err := j.Append(JournalEntry{MsgID: MessageID(run, TypeEndReplace, 2), Subject: "s", Payload: end}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack(MessageID(run, TypeEndReplace, 2)); err != nil {
		t.Fatal(err)
	}
	if err := j.CompactAcked(); err != nil {
		t.Fatal(err)
	}
	if _, ok := j.fenced[run]; !ok {
		t.Fatal("expected protocol run fence after CompactAcked")
	}
	if err := j.Append(JournalEntry{MsgID: MessageID(run, TypeBatch, 1), Subject: "s", Payload: []byte(`{"type":"batch","run_id":"run-fence","seq":1}`)}); err == nil {
		t.Fatal("retired-run append must be rejected, including identical payload")
	}
	if err := j.Append(JournalEntry{MsgID: MessageID(run, TypeBatch, 1), Subject: "s", Payload: []byte("different")}); err == nil {
		t.Fatal("retired-run append with a different payload must not no-op")
	}
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	if _, ok := j2.fenced[run]; !ok {
		t.Fatal("reopen lost run fence")
	}
	if err := j2.Append(JournalEntry{MsgID: MessageID(run, TypeBatch, 1), Subject: "s", Payload: []byte("different")}); err == nil {
		t.Fatal("reopen must still reject retired-run appends")
	}
}

func TestJournalReclaimCrashUsesNextManifest(t *testing.T) {
	dir := t.TempDir()
	oldRow, err := json.Marshal(payloadLine{MsgID: "old", Subject: "s", Payload: bytes.Repeat([]byte("o"), 200)})
	if err != nil {
		t.Fatal(err)
	}
	oldRaw := append(oldRow, '\n')
	newRow, err := json.Marshal(payloadLine{MsgID: "keep", Subject: "s", Payload: []byte("keep")})
	if err != nil {
		t.Fatal(err)
	}
	newRaw := append(newRow, '\n')
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), newRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	oldCommit := commitFile{
		Schema: commitSchema,
		Files: map[string]commitMeta{
			"payloads.jsonl": {Size: int64(len(oldRaw)), CRC64: crc64Of(oldRaw)},
			"acks.jsonl":     {},
		},
	}
	oldB, _ := json.Marshal(oldCommit)
	if err := os.WriteFile(filepath.Join(dir, "commit.json"), oldB, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(dir); err == nil {
		t.Fatal("stale commit size against compacted payloads must reject without commit.next")
	}
	next := commitFile{
		Schema: commitSchema,
		Files: map[string]commitMeta{
			"payloads.jsonl": {Size: int64(len(newRaw)), CRC64: crc64Of(newRaw)},
			"acks.jsonl":     {},
		},
	}
	nextB, _ := json.Marshal(next)
	if err := os.WriteFile(filepath.Join(dir, "commit.next.json"), nextB, 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirFingerprint(t, dir)
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if dirFingerprint(t, dir) != before {
		t.Fatal("open must not mutate while recovering commit.next")
	}
	got := j.Unacked()
	if len(got) != 1 || got[0].MsgID != "keep" {
		t.Fatalf("recovered entries %+v", got)
	}
}

func crc64Of(b []byte) uint64 {
	return crc64.Checksum(b, journalCRC)
}

func TestJournalOpenDoesNotWriteCommit(t *testing.T) {
	dir := t.TempDir()
	row, err := json.Marshal(payloadLine{MsgID: "a", Subject: "s", Payload: []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), append(row, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirFingerprint(t, dir)
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	j.Close()
	if dirFingerprint(t, dir) != before {
		t.Fatal("OpenJournal/Close of a read-only load must not create commit.json")
	}
	if _, err := os.Stat(filepath.Join(dir, "commit.json")); !os.IsNotExist(err) {
		t.Fatal("read-only open created commit.json")
	}
}
