package graphstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func endPayload(runID string) []byte {
	end := EndReplace{Type: TypeEndReplace, RunID: runID, BatchCount: 1, Completeness: Completeness{Status: "success"}}
	b, err := Marshal(end)
	if err != nil {
		panic(err)
	}
	return b
}

func TestJournalTotalProgressExceedsCapWithBoundedSpool(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const capBytes = 32 << 10
	j.SetMaxBytes(capBytes)
	payload := bytes.Repeat([]byte("p"), 2048)
	const n = 80 // 160KiB total, 5x the cap
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("run:batch:%d", i)
		if err := j.Append(JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
			t.Fatalf("append %d: %v (retained %d)", i, err, j.RetainedBytes())
		}
		if err := j.Ack(id); err != nil {
			t.Fatalf("ack %d: %v", i, err)
		}
		if got := j.RetainedBytes(); got > capBytes {
			t.Fatalf("retained %d after %d acks; cap %d", got, i+1, capBytes)
		}
	}
	if err := j.Append(JournalEntry{MsgID: "run:end_replace:1", Subject: "s", Payload: endPayload("run")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("run:end_replace:1"); err != nil {
		t.Fatal(err)
	}
	if !j.HasAckedEnd("run") {
		t.Fatal("End proof missing after bounded progress")
	}
	if got := j.RetainedBytes(); got > capBytes {
		t.Fatalf("retained %d including End; cap %d", got, capBytes)
	}
}

func TestJournalUnackedBacklogStillBounded(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const capBytes = 16 << 10
	j.SetMaxBytes(capBytes)
	payload := bytes.Repeat([]byte("u"), 4096)
	var n int
	for {
		err := j.Append(JournalEntry{MsgID: fmt.Sprintf("x:%d", n), Subject: "s", Payload: payload})
		if err != nil {
			break
		}
		n++
		if n > 20 {
			t.Fatal("expected unacked spool bound")
		}
	}
	if n == 0 {
		t.Fatal("expected some unacked progress")
	}
	if got := j.RetainedBytes(); got > capBytes {
		t.Fatalf("unacked retained %d > cap %d", got, capBytes)
	}
	if len(j.Unacked()) != n {
		t.Fatalf("unacked %d want %d", len(j.Unacked()), n)
	}
}

func TestJournalReclaimKeepsAckedEndProof(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	j.SetMaxBytes(12 << 10)
	payload := bytes.Repeat([]byte("b"), 2048)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("run:batch:%d", i)
		if err := j.Append(JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if err := j.Ack(id); err != nil {
			t.Fatal(err)
		}
	}
	end := endPayload("run")
	if err := j.Append(JournalEntry{MsgID: "run:end_replace:1", Subject: "s", Payload: end}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("run:end_replace:1"); err != nil {
		t.Fatal(err)
	}
	if !j.HasAckedEnd("run") {
		t.Fatal("HasAckedEnd false before reopen")
	}
	found := false
	for _, e := range j.Entries() {
		if e.Acked && bytes.Equal(e.Payload, end) {
			found = true
		}
	}
	if !found {
		t.Fatal("Entries() dropped End proof during live reclaim")
	}

	j2, err := openJournal(dir, 12<<10)
	if err != nil {
		t.Fatal(err)
	}
	if !j2.HasAckedEnd("run") {
		t.Fatal("reopened journal lost End proof")
	}
	if n := len(j2.Unacked()); n != 0 {
		t.Fatalf("unacked after end ack: %d", n)
	}
	if err := j2.CompactAcked(); err != nil {
		t.Fatal(err)
	}
	if j2.HasAckedEnd("run") {
		t.Fatal("CompactAcked must drop End after checkpoint")
	}
}

func TestJournalPreEndCrashDoesNotInventEnd(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	j.SetMaxBytes(8 << 10)
	payload := bytes.Repeat([]byte("b"), 1024)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("run:batch:%d", i)
		if err := j.Append(JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if err := j.Ack(id); err != nil {
			t.Fatal(err)
		}
	}
	j2, err := openJournal(dir, 8<<10)
	if err != nil {
		t.Fatal(err)
	}
	if j2.HasAckedEnd("run") {
		t.Fatal("pre-End crash must not report acked End")
	}
	for _, e := range j2.Entries() {
		if isEndPayload(e.Payload) {
			t.Fatal("pre-End journal contains End payload")
		}
	}
}

func TestJournalLostAckReplaysIdenticalPayload(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := endPayload("run-lost")
	if err := j.Append(JournalEntry{MsgID: "run-lost:end_replace:1", Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	// Crash after durable journal, before local ack (lost broker-ack record).
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	unacked := j2.Unacked()
	if len(unacked) != 1 || !bytes.Equal(unacked[0].Payload, payload) {
		t.Fatalf("lost-ack unacked %+v", unacked)
	}
	if j2.HasAckedEnd("run-lost") {
		t.Fatal("unacked End must not count as proof")
	}
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j2, Subject: "s"}
	if err := p.ReplayUnacked(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sink.CloneRecords()
	if len(got) != 1 || !bytes.Equal(got[0].Payload, payload) {
		t.Fatalf("replay %+v", got)
	}
	if !j2.HasAckedEnd("run-lost") {
		t.Fatal("expected acked End after replay")
	}
}

func TestJournalOpenDoesNotMutateAckedWaste(t *testing.T) {
	dir := t.TempDir()
	payload := bytes.Repeat([]byte("w"), 1024)
	var payloadRows, ackRows [][]byte
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("old:batch:%d", i)
		row, err := json.Marshal(payloadLine{MsgID: id, Subject: "s", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		payloadRows = append(payloadRows, append(row, '\n'))
		ack, err := json.Marshal(ackLine{MsgID: id})
		if err != nil {
			t.Fatal(err)
		}
		ackRows = append(ackRows, append(ack, '\n'))
	}
	end := endPayload("old")
	endRow, err := json.Marshal(payloadLine{MsgID: "old:end_replace:1", Subject: "s", Payload: end})
	if err != nil {
		t.Fatal(err)
	}
	payloadRows = append(payloadRows, append(endRow, '\n'))
	endAck, err := json.Marshal(ackLine{MsgID: "old:end_replace:1"})
	if err != nil {
		t.Fatal(err)
	}
	ackRows = append(ackRows, append(endAck, '\n'))
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), bytes.Join(payloadRows, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acks.jsonl"), bytes.Join(ackRows, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirFingerprint(t, dir)
	j, err := openJournal(dir, 4<<10)
	if err != nil {
		t.Fatal(err)
	}
	after := dirFingerprint(t, dir)
	if before != after {
		t.Fatal("OpenJournal mutated source directory bytes")
	}
	if !j.HasAckedEnd("old") {
		t.Fatal("open dropped End proof")
	}
	if got := j.PhysicalBytes(); got <= 4<<10 {
		t.Fatalf("expected uncompacted physical spool on read-only open, got %d", got)
	}
	if n := len(j.Unacked()); n != 0 {
		t.Fatalf("unacked %d", n)
	}
	// Mutation path reclaims so the next append fits the cap.
	if err := j.Append(JournalEntry{MsgID: "new:batch:1", Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if got := j.PhysicalBytes(); got > 4<<10+encodedLineOverhead(t, "new:batch:1", payload) {
		t.Fatalf("after mutate reclaim physical %d", got)
	}
	if dirFingerprint(t, dir) == before {
		t.Fatal("append/reclaim should mutate journal files")
	}
}

func TestJournalConflictingRetryStillRejected(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "x", Subject: "s", Payload: []byte("A")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("x"); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "x", Subject: "s", Payload: []byte("B")}); err == nil {
		t.Fatal("acked identity must still reject conflicting payload")
	}
}

func TestAsyncGroupCommitAmortizesFsync(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(256, 4<<20)
	defer p.CloseAsync()
	const n = 400
	for i := 0; i < n; i++ {
		if err := p.Publish(context.Background(), fmt.Sprintf("id-%d", i), []byte(`{"type":"batch"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	syncs := j.SyncCount()
	if syncs == 0 {
		t.Fatal("expected at least one group fsync")
	}
	if syncs >= int64(n)/2 {
		t.Fatalf("group commit did not amortize fsyncs: %d syncs for %d messages", syncs, n)
	}
	if len(sink.CloneRecords()) != n {
		t.Fatalf("delivered %d want %d", len(sink.CloneRecords()), n)
	}
	if len(j.Unacked()) != 0 {
		t.Fatalf("unacked %d", len(j.Unacked()))
	}
}

func TestAsyncDoesNotPublishBeforeJournalSync(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &syncProbeSink{journal: j}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(32, 1<<20)
	defer p.CloseAsync()
	for i := 0; i < 20; i++ {
		if err := p.Publish(context.Background(), fmt.Sprintf("id-%d", i), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.firstSyncs.Load() < 1 {
		t.Fatalf("sink publish ran before journal group-commit (syncs=%d)", sink.firstSyncs.Load())
	}
}

type syncProbeSink struct {
	MemorySink
	journal    *Journal
	once       sync.Once
	firstSyncs atomic.Int64
}

func (s *syncProbeSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	s.once.Do(func() {
		s.firstSyncs.Store(s.journal.SyncCount())
	})
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}

func TestAsyncFailedBrokerLeavesUnackedForReplay(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	sink.FailAt(2, errors.New("broker down"))
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	_ = p.Publish(context.Background(), "id-1", []byte("A"))
	_ = p.Publish(context.Background(), "id-2", []byte("B"))
	_ = p.Publish(context.Background(), "id-3", []byte("C"))
	if err := p.Flush(context.Background()); err == nil {
		t.Fatal("expected broker failure")
	}
	p.CloseAsync()

	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	unacked := j2.Unacked()
	if len(unacked) == 0 {
		t.Fatal("expected unacked payloads after broker failure")
	}
	for _, e := range unacked {
		if e.MsgID == "id-2" && !bytes.Equal(e.Payload, []byte("B")) {
			t.Fatalf("payload mutated: %s", e.Payload)
		}
	}
	sink2 := &MemorySink{}
	p2 := &Publisher{Sink: sink2, Journal: j2, Subject: "s"}
	if err := p2.ReplayUnacked(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sink2.CloneRecords()
	if len(got) == 0 {
		t.Fatal("replay delivered nothing")
	}
	byID := map[string][]byte{}
	for _, r := range got {
		byID[r.MsgID] = r.Payload
	}
	if !bytes.Equal(byID["id-2"], []byte("B")) && containsID(unacked, "id-2") {
		t.Fatalf("id-2 replay mismatch: %q", byID["id-2"])
	}
}

func containsID(entries []JournalEntry, id string) bool {
	for _, e := range entries {
		if e.MsgID == id {
			return true
		}
	}
	return false
}

func TestAsyncPublisherExceedsJournalCapWithAcks(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const capBytes = 64 << 10
	j.SetMaxBytes(capBytes)
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(32, capBytes)
	defer p.CloseAsync()
	payload := bytes.Repeat([]byte("m"), 1024)
	const n = 200 // 200KiB > 64KiB cap
	for i := 0; i < n; i++ {
		if err := p.Publish(context.Background(), fmt.Sprintf("id-%d", i), payload); err != nil {
			t.Fatalf("publish %d: %v retained=%d", i, err, j.RetainedBytes())
		}
		if i%8 == 7 {
			if err := p.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(sink.CloneRecords()); got != n {
		t.Fatalf("delivered %d want %d", got, n)
	}
	if got := j.RetainedBytes(); got > capBytes {
		t.Fatalf("retained %d after acked progress", got)
	}
}

func TestAsyncFlushSeesAckedEnd(t *testing.T) {
	for i := 0; i < 50; i++ {
		j, err := OpenJournal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		p := &Publisher{Sink: &MemorySink{}, Journal: j, Subject: "s"}
		p.EnableAsync(64, 1<<20)
		runID := fmt.Sprintf("run-%d", i)
		if err := p.Publish(context.Background(), MessageID(runID, TypeEndReplace, 1), endPayload(runID)); err != nil {
			t.Fatal(err)
		}
		if err := p.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !j.HasAckedEnd(runID) {
			t.Fatalf("iter %d: Flush returned before End was acknowledged", i)
		}
		if n := len(j.Unacked()); n != 0 {
			t.Fatalf("iter %d: unacked %d after Flush", i, n)
		}
		p.CloseAsync()
	}
}

func TestAsyncFlushWaitsForPipelinedDelivery(t *testing.T) {
	var inFlight atomic.Int32
	var max atomic.Int32
	sink := &overlapSink{inFlight: &inFlight, max: &max, delay: 30 * time.Millisecond}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	start := time.Now()
	for i := 0; i < 4; i++ {
		if err := p.Publish(context.Background(), fmt.Sprintf("id-%d", i), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if max.Load() < 2 {
		t.Fatalf("expected pipelined in-flight >1, max=%d", max.Load())
	}
	if elapsed < 30*time.Millisecond {
		t.Fatalf("flush returned before sink work: %s", elapsed)
	}
	if sink.n.Load() != 4 {
		t.Fatalf("delivered %d", sink.n.Load())
	}
}

type overlapSink struct {
	MemorySink
	inFlight *atomic.Int32
	max      *atomic.Int32
	delay    time.Duration
	n        atomic.Int32
}

func (s *overlapSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	cur := s.inFlight.Add(1)
	for {
		old := s.max.Load()
		if cur <= old || s.max.CompareAndSwap(old, cur) {
			break
		}
	}
	defer s.inFlight.Add(-1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.delay):
	}
	s.n.Add(1)
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}
