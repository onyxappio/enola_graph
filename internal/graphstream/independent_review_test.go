package graphstream

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reviewDisk(t *testing.T, dir string) int64 {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	for _, e := range es {
		st, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		n += st.Size()
	}
	return n
}
func TestReviewPhysicalTombBound(t *testing.T) {
	d := t.TempDir()
	j, err := openJournal(d, 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var boundErr error
	nDone := 0
	for i := 0; i < 4000; i++ {
		id := fmt.Sprintf("run-batch-%06d", i)
		if err := j.appendUnsynced(JournalEntry{MsgID: id, Subject: "s", Payload: bytes.Repeat([]byte("x"), 100)}); err != nil {
			boundErr = err
			break
		}
		if err := j.ackUnsynced(id); err != nil {
			boundErr = err
			break
		}
		nDone++
	}
	if err := j.Sync(); err != nil && boundErr == nil {
		t.Fatal(err)
	}
	n := reviewDisk(t, d)
	t.Logf("maxBytes=%d reportedPhysical=%d actualDisk=%d tombEntries=%d done=%d boundErr=%v", j.maxBytes, j.PhysicalBytes(), n, len(j.tomb), nDone, boundErr)
	if n > 2*j.maxBytes {
		t.Fatalf("total physical spool exceeds even 2x configured bound")
	}
	if boundErr == nil {
		t.Fatal("expected exact identity history to hit the tiny cap instead of truncating hashes")
	}
	if nDone == 0 {
		t.Fatal("expected some progress before identity bound")
	}
	if err := j.Append(JournalEntry{MsgID: "run-batch-000000", Subject: "s", Payload: bytes.Repeat([]byte("y"), 100)}); err == nil {
		t.Fatal("conflicting replay of a persisted identity must still fail")
	}
}
func TestReviewCommittedTailLoss(t *testing.T) {
	d := t.TempDir()
	j, err := OpenJournal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "committed", Subject: "s", Payload: []byte("durable")}); err != nil {
		t.Fatal(err)
	}
	j.Close()
	p := filepath.Join(d, "payloads.jsonl")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, b[:len(b)-1], 0644); err != nil {
		t.Fatal(err)
	}
	r, err := OpenJournal(d)
	if err != nil {
		t.Logf("correctly rejected committed damage: %v", err)
		return
	}
	defer r.Close()
	if len(r.Unacked()) != 1 {
		t.Fatalf("durably appended unacked message silently dropped after committed trailing newline corruption: got %d", len(r.Unacked()))
	}
}
func TestReviewDetachedGroupFlushAndBound(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.Append(JournalEntry{MsgID: "one", Payload: []byte("one")}); err != nil {
		t.Fatal(err)
	}
	p := &Publisher{Journal: j, Sink: &MemorySink{}}
	p.EnableAsync(1, 3)
	defer p.CloseAsync()
	j.mu.Lock()
	if err := p.enqueue(context.Background(), "one", []byte("one")); err != nil {
		j.mu.Unlock()
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		p.async.mu.Lock()
		n := p.async.committing
		q := p.async.queueLen()
		b := p.async.queueBytes()
		p.async.mu.Unlock()
		if n == 1 {
			if q != 1 || b != 3 {
				j.mu.Unlock()
				t.Fatalf("detached bounds q=%d b=%d", q, b)
			}
			break
		}
		if time.Now().After(deadline) {
			j.mu.Unlock()
			t.Fatal("commit never detached")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err = p.Flush(ctx)
	cancel()
	if err == nil {
		j.mu.Unlock()
		t.Fatal("Flush escaped blocked durability group")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	err = p.enqueue(ctx, "two", []byte("two"))
	cancel()
	j.mu.Unlock()
	if err == nil {
		t.Fatal("queue escaped committing group bound")
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}
