package graphstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func dirFingerprint(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func encodedLineOverhead(t *testing.T, id string, payload []byte) int64 {
	t.Helper()
	b, err := json.Marshal(payloadLine{MsgID: id, Subject: "s", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	return int64(len(b) + 1)
}

func beginPayload(runID string) []byte {
	b, err := Marshal(BeginReplace{
		Type: TypeBeginReplace, SchemaVersion: SchemaVersion,
		RepoID: "r", ContextID: "c", RunID: runID, TargetGeneration: 1,
		Phase: PhaseResolved, ScopeMode: ScopeModeComplete,
		OwnerScope: []OwnerRef{{Kind: OwnerFile, ID: "a.ts"}}, OwnerScopeCount: 1,
	})
	if err != nil {
		panic(err)
	}
	return b
}

func batchPayload(runID string, seq int, phase string, nodes []Node, owners []OwnerRef) []byte {
	b, err := Marshal(Batch{Type: TypeBatch, RunID: runID, Seq: seq, Phase: phase, Nodes: nodes, Owners: owners})
	if err != nil {
		panic(err)
	}
	return b
}

func TestJournalCloseReleasesHandles(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "a", Subject: "s", Payload: []byte("a")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	if n := len(j2.Unacked()); n != 1 {
		t.Fatalf("unacked %d", n)
	}
}

func TestJournalPhysicalBytesCountEncodedLines(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), 100)
	if err := j.Append(JournalEntry{MsgID: "id-1", Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	want := encodedLineOverhead(t, "id-1", payload)
	if got := j.PhysicalBytes(); got != want {
		t.Fatalf("physical %d want encoded line %d (not raw payload %d)", got, want, len(payload))
	}
	if got := j.PhysicalBytes(); got == int64(len(payload)) {
		t.Fatal("physical bound must not equal raw payload length")
	}
}

func TestJournalPhysicalCapRejectsUnackedEncodedOverflow(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("y"), 200)
	line := encodedLineOverhead(t, "id-0", payload)
	j.SetMaxBytes(line*2 + 512)
	if err := j.Append(JournalEntry{MsgID: "id-0", Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "id-1", Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "id-2", Subject: "s", Payload: payload}); err == nil {
		t.Fatal("expected encoded physical spool bound")
	}
	if got := j.PhysicalBytes(); got > line*2+512 {
		t.Fatalf("physical %d over cap", got)
	}
}

func TestJournalConflictingIDAfterLiveGCAndReopen(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	j.SetMaxBytes(8 << 10)
	first := []byte("AAAA")
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: first}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("same"); err != nil {
		t.Fatal(err)
	}
	filler := bytes.Repeat([]byte("f"), 512)
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("fill:%d", i)
		if err := j.Append(JournalEntry{MsgID: id, Subject: "s", Payload: filler}); err != nil {
			t.Fatal(err)
		}
		if err := j.Ack(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := j.Get("same"); ok {
		t.Fatal("expected live GC to drop byID entry for acked identity")
	}
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: []byte("BBBB")}); err == nil {
		t.Fatal("conflicting reuse after live GC must fail")
	}
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: first}); err != nil {
		t.Fatal(err)
	}
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := j2.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: []byte("BBBB")}); err == nil {
		t.Fatal("conflicting reuse after reopen must fail")
	}
	if err := j2.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: first}); err != nil {
		t.Fatal(err)
	}
}

func TestJournalTornTailIgnoredOnOpen(t *testing.T) {
	dir := t.TempDir()
	ok := payloadLine{MsgID: "keep", Subject: "s", Payload: []byte("ok")}
	row, err := json.Marshal(ok)
	if err != nil {
		t.Fatal(err)
	}
	raw := append(append([]byte{}, row...), '\n')
	raw = append(raw, []byte(`{"msg_id":"torn"`)...) // no newline, invalid JSON
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirFingerprint(t, dir)
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirFingerprint(t, dir) != before {
		t.Fatal("open must not truncate torn tail")
	}
	got := j.Unacked()
	if len(got) != 1 || got[0].MsgID != "keep" {
		t.Fatalf("unacked %+v", got)
	}
	if err := j.Append(JournalEntry{MsgID: "next", Subject: "s", Payload: []byte("n")}); err != nil {
		t.Fatal(err)
	}
	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range j2.Entries() {
		ids[e.MsgID] = true
	}
	if !ids["keep"] || !ids["next"] || ids["torn"] {
		t.Fatalf("after truncate+append ids=%v", ids)
	}
}

func TestJournalCommittedCorruptionRejected(t *testing.T) {
	dir := t.TempDir()
	ok1, _ := json.Marshal(payloadLine{MsgID: "a", Subject: "s", Payload: []byte("a")})
	ok2, _ := json.Marshal(payloadLine{MsgID: "c", Subject: "s", Payload: []byte("c")})
	raw := bytes.Join([][]byte{append(ok1, '\n'), []byte("not-json\n"), append(ok2, '\n')}, nil)
	if err := os.WriteFile(filepath.Join(dir, "payloads.jsonl"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(dir); err == nil {
		t.Fatal("expected committed corruption error")
	}
}

func TestJournalReclaimLeavesNoTmpAndEndProof(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	j.SetMaxBytes(6 << 10)
	payload := bytes.Repeat([]byte("z"), 400)
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("b:%d", i)
		if err := j.Append(JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
			t.Fatal(err)
		}
		if err := j.Ack(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Append(JournalEntry{MsgID: "run:end_replace:1", Subject: "s", Payload: endPayload("run")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("run:end_replace:1"); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("leftover tmp %s", e.Name())
		}
	}
	if !j.HasAckedEnd("run") {
		t.Fatal("missing End proof")
	}
}

func TestAsyncGroupDurableOnDiskBeforeSink(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink := &diskProbeSink{dir: dir}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(16, 1<<20)
	defer p.CloseAsync()
	const n = 12
	for i := 0; i < n; i++ {
		if err := p.Publish(context.Background(), fmt.Sprintf("id-%d", i), []byte(`{"type":"batch","run_id":"r","seq":1}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.missing.Load() != 0 {
		t.Fatalf("%d sink publishes ran before their payload was a complete journal line", sink.missing.Load())
	}
}

type diskProbeSink struct {
	MemorySink
	dir     string
	missing atomic.Int32
}

func (s *diskProbeSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	b, err := os.ReadFile(filepath.Join(s.dir, "payloads.jsonl"))
	if err != nil || !journalFileContainsMsgID(b, msgID) {
		s.missing.Add(1)
	}
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}

func journalFileContainsMsgID(raw []byte, msgID string) bool {
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 || !json.Valid(line) {
			continue
		}
		var p payloadLine
		if json.Unmarshal(line, &p) != nil {
			continue
		}
		if p.MsgID == msgID {
			return true
		}
	}
	return false
}

func TestMemorySinkConcurrentPublish(t *testing.T) {
	s := &MemorySink{}
	var wg sync.WaitGroup
	const n = 64
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			_ = s.Publish(context.Background(), "s", fmt.Sprintf("id-%d", i), []byte("x"))
		}()
	}
	wg.Wait()
	got := s.CloneRecords()
	if len(got) != n {
		t.Fatalf("records %d", len(got))
	}
	got[0].Payload[0] = 'Z'
	again := s.CloneRecords()
	if again[0].Payload[0] != 'x' {
		t.Fatal("CloneRecords aliased live payloads")
	}
}

func TestAsyncDelayedBeginDoesNotPublishData(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan string, 8)
	sink := &holdSink{holdID: MessageID("run", TypeBeginReplace, 0), hold: hold, started: started}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	runID := "run"
	begin := beginPayload(runID)
	batch := batchPayload(runID, 1, PhaseLocal, []Node{{Owner: OwnerRef{Kind: OwnerFile, ID: "a.ts"}, ID: "n", Kind: "symbol", Name: "n"}}, nil)
	end := endPayload(runID)
	if err := p.Publish(context.Background(), MessageID(runID, TypeBeginReplace, 0), begin); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), MessageID(runID, TypeBatch, 1), batch); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), MessageID(runID, TypeEndReplace, 2), end); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-started:
		if id != MessageID(runID, TypeBeginReplace, 0) {
			t.Fatalf("first sink publish %s, want Begin", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Begin did not reach sink")
	}
	timer := time.NewTimer(80 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case id := <-started:
			t.Fatalf("published %s while Begin still held", id)
		case <-timer.C:
			goto released
		}
	}
released:
	close(hold)
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	recs := sink.CloneRecords()
	if err := strictApply(recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) < 2 || probeType(recs[0].Payload) != TypeBeginReplace {
		t.Fatalf("order %+v", recs)
	}
}

func TestAsyncDelayedEarlierBatchBlocksEnd(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan string, 16)
	batch1 := MessageID("run", TypeBatch, 1)
	sink := &holdSink{holdID: batch1, hold: hold, started: started}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	runID := "run"
	if err := p.Publish(context.Background(), MessageID(runID, TypeBeginReplace, 0), beginPayload(runID)); err != nil {
		t.Fatal(err)
	}
	b1 := batchPayload(runID, 1, PhaseResolved, []Node{{Owner: OwnerRef{Kind: OwnerFile, ID: "a.ts"}, ID: "n", Kind: "symbol", Name: "n"}}, nil)
	b2 := batchPayload(runID, 2, PhaseResolved, nil, nil)
	if err := p.Publish(context.Background(), batch1, b1); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), MessageID(runID, TypeBatch, 2), b2); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), MessageID(runID, TypeEndReplace, 3), endPayload(runID)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	sawHold := false
	for time.Now().Before(deadline) && !sawHold {
		select {
		case id := <-started:
			if id == MessageID(runID, TypeEndReplace, 3) {
				t.Fatal("End reached sink before earlier batch was released")
			}
			if id == batch1 {
				sawHold = true
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !sawHold {
		t.Fatal("batch 1 did not reach sink")
	}
	timer := time.NewTimer(80 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case id := <-started:
			if id == MessageID(runID, TypeEndReplace, 3) {
				t.Fatal("End published while earlier batch held")
			}
		case <-timer.C:
			goto released
		}
	}
released:
	close(hold)
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := strictApply(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncDelayedScopeBlocksResolved(t *testing.T) {
	hold := make(chan struct{})
	started := make(chan string, 16)
	scopeID := MessageID("run", TypeBatch, 1)
	sink := &holdSink{holdID: scopeID, hold: hold, started: started}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	runID := "run"
	if err := p.Publish(context.Background(), MessageID(runID, TypeBeginReplace, 0), beginPayload(runID)); err != nil {
		t.Fatal(err)
	}
	scope := batchPayload(runID, 1, PhaseScope, nil, []OwnerRef{{Kind: OwnerFile, ID: "a.ts"}})
	resolved := batchPayload(runID, 2, PhaseResolved, []Node{{Owner: OwnerRef{Kind: OwnerFile, ID: "a.ts"}, ID: "n", Kind: "symbol", Name: "n"}}, nil)
	if err := p.Publish(context.Background(), scopeID, scope); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), MessageID(runID, TypeBatch, 2), resolved); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	sawScope := false
	for time.Now().Before(deadline) && !sawScope {
		select {
		case id := <-started:
			if id == MessageID(runID, TypeBatch, 2) {
				t.Fatal("resolved batch published before scope ack")
			}
			if id == scopeID {
				sawScope = true
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !sawScope {
		t.Fatal("scope did not reach sink")
	}
	select {
	case id := <-started:
		if id == MessageID(runID, TypeBatch, 2) {
			t.Fatal("resolved batch published while scope held")
		}
	case <-time.After(80 * time.Millisecond):
	}
	close(hold)
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := strictApply(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
}

type holdSink struct {
	MemorySink
	holdID  string
	hold    chan struct{}
	started chan string
}

func (s *holdSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	select {
	case s.started <- msgID:
	default:
	}
	if msgID == s.holdID {
		select {
		case <-s.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}

func probeType(p []byte) string {
	var probe struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(p, &probe)
	return probe.Type
}

// strictApply mirrors Consumer.Apply control-event rejections: unknown run
// for batches/end, and missing sequences at End. It does not buffer End.
func strictApply(recs []Recorded) error {
	type open struct {
		seq map[int][]byte
	}
	runs := map[string]*open{}
	applied := map[string]bool{}
	for _, r := range recs {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(r.Payload, &probe); err != nil {
			return err
		}
		switch probe.Type {
		case TypeBeginReplace:
			var b BeginReplace
			if err := json.Unmarshal(r.Payload, &b); err != nil {
				return err
			}
			if applied[b.RunID] {
				continue
			}
			if _, ok := runs[b.RunID]; ok {
				return fmt.Errorf("duplicate begin for %s", b.RunID)
			}
			runs[b.RunID] = &open{seq: map[int][]byte{}}
		case TypeBatch:
			var b Batch
			if err := json.Unmarshal(r.Payload, &b); err != nil {
				return err
			}
			st := runs[b.RunID]
			if st == nil {
				if applied[b.RunID] {
					continue
				}
				return fmt.Errorf("batch seq %d for unknown run %s", b.Seq, b.RunID)
			}
			st.seq[b.Seq] = r.Payload
		case TypeEndReplace:
			var e EndReplace
			if err := json.Unmarshal(r.Payload, &e); err != nil {
				return err
			}
			if applied[e.RunID] {
				continue
			}
			st := runs[e.RunID]
			if st == nil {
				return fmt.Errorf("end for unknown run %s", e.RunID)
			}
			if e.BatchCount != 0 {
				for i := 1; i <= e.BatchCount; i++ {
					if _, ok := st.seq[i]; !ok {
						return fmt.Errorf("missing batch seq %d in %s", i, e.RunID)
					}
				}
			}
			applied[e.RunID] = true
			delete(runs, e.RunID)
		}
	}
	return nil
}
