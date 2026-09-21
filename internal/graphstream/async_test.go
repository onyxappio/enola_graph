package graphstream

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type slowSink struct {
	MemorySink
	delay time.Duration
	n     atomic.Int32
}

func (s *slowSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.delay):
	}
	s.n.Add(1)
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}

func TestAsyncPublishSeparatesFromSlowSink(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &slowSink{delay: 40 * time.Millisecond}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := p.Publish(context.Background(), MessageID("r", TypeBatch, i+1), []byte(`{"type":"batch"}`)); err != nil {
			t.Fatal(err)
		}
	}
	enqueued := time.Since(start)
	if enqueued > 80*time.Millisecond {
		t.Fatalf("enqueue blocked on sink RTT: %s", enqueued)
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.n.Load() != 5 {
		t.Fatalf("delivered %d", sink.n.Load())
	}
	if n := len(j.Unacked()); n != 0 {
		t.Fatalf("unacked %d", n)
	}
}

func TestAsyncFlushCancel(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &slowSink{delay: 200 * time.Millisecond}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "id-1", []byte("A")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Flush(ctx); err == nil {
		t.Fatal("expected flush cancellation")
	}
}

func TestAsyncRejectsPayloadOverMaxBytes(t *testing.T) {
	p := &Publisher{Sink: &MemorySink{}, Subject: "s"}
	p.EnableAsync(4, 64)
	defer p.CloseAsync()
	huge := make([]byte, 65)
	start := time.Now()
	err := p.Publish(context.Background(), "id-big", huge)
	if err == nil {
		t.Fatal("expected payload-over-maxBytes error")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("oversize publish deadlocked or blocked: %s", time.Since(start))
	}
}

func TestAsyncCopiesPayloadWithoutJournal(t *testing.T) {
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(4, 1<<20)
	defer p.CloseAsync()
	buf := []byte("ABC")
	if err := p.Publish(context.Background(), "id-1", buf); err != nil {
		t.Fatal(err)
	}
	buf[0] = 'Z'
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sink.CloneRecords()
	if len(got) != 1 || string(got[0].Payload) != "ABC" {
		t.Fatalf("queue payload mutated: %+v", got)
	}
}

func TestAsyncCloseCancelsInFlight(t *testing.T) {
	sink := &slowSink{delay: 5 * time.Second}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(4, 1<<20)
	if err := p.Publish(context.Background(), "id-1", []byte("A")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	p.CloseAsync()
	if time.Since(start) > time.Second {
		t.Fatalf("CloseAsync hung: %s", time.Since(start))
	}
}

func TestAsyncQueuePlusOneInFlightBound(t *testing.T) {
	started := make(chan struct{}, 1)
	released := make(chan struct{})
	sink := &gateSink{started: started, release: released}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(1, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "id-1", []byte("A")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first publish did not reach sink")
	}
	// Queue bound is 1 plus the in-flight job, so a second enqueue still fits.
	if err := p.Publish(context.Background(), "id-2", []byte("B")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Publish(ctx, "id-3", []byte("C")); err == nil {
		t.Fatal("third publish should block: queue full and one in-flight")
	}
	close(released)
}

type gateSink struct {
	MemorySink
	started chan struct{}
	release chan struct{}
}

func (s *gateSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}

func TestAsyncPipelinesMultipleInFlight(t *testing.T) {
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	sink := &countingGateSink{started: started, release: release}
	p := &Publisher{Sink: sink, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	for i := 0; i < 4; i++ {
		if err := p.Publish(context.Background(), MessageID("r", TypeBatch, i+1), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(2 * time.Second)
	n := 0
	for n < 2 {
		select {
		case <-started:
			n++
		case <-deadline:
			t.Fatal("expected pipelined in-flight publishes; single FIFO still blocking")
		}
	}
	close(release)
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type countingGateSink struct {
	MemorySink
	started chan struct{}
	release chan struct{}
}

func (s *countingGateSink) Publish(ctx context.Context, subject, msgID string, p []byte) error {
	select {
	case s.started <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.MemorySink.Publish(ctx, subject, msgID, p)
}

func TestAsyncSinkErrorFailsFlush(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	sink.FailAt(1, context.Canceled)
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(4, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "id-1", []byte("A")); err != nil {
		t.Fatal(err)
	}
	if err := p.Flush(context.Background()); err == nil {
		t.Fatal("expected sink error on flush")
	}
}

func TestAsyncEnqueueIndependentOfSlowSync(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	stall := make(chan struct{})
	j.SetSyncHook(func() {
		select {
		case started <- struct{}{}:
		default:
		}
		<-stall
	})
	p := &Publisher{Sink: &MemorySink{}, Journal: j, Subject: "s"}
	p.EnableAsync(32, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "id-0", []byte("a")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("commit worker never reached fsync")
	}
	deadline := time.Now().Add(time.Second)
	for {
		p.async.mu.Lock()
		n := p.async.committing
		p.async.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first group never detached into committing")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	for i := 1; i < 16; i++ {
		if err := p.Publish(context.Background(), fmt.Sprintf("id-%d", i), []byte("a")); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed > 80*time.Millisecond {
		t.Fatalf("available-capacity enqueue blocked on fsync: %s", elapsed)
	}
	p.async.mu.Lock()
	pending := len(p.async.pending)
	committing := p.async.committing
	p.async.mu.Unlock()
	if pending < 8 {
		t.Fatalf("staggered enqueue did not form a real pending group: pending=%d committing=%d", pending, committing)
	}
	close(stall)
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(j.Unacked()); n != 0 {
		t.Fatalf("unacked %d after Flush", n)
	}
}

func TestAsyncQueuedConflictRejected(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stall := make(chan struct{})
	started := make(chan struct{}, 1)
	j.SetSyncHook(func() {
		select {
		case started <- struct{}{}:
		default:
		}
		<-stall
	})
	p := &Publisher{Sink: &MemorySink{}, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer func() {
		select {
		case <-stall:
		default:
			close(stall)
		}
		p.CloseAsync()
	}()
	if err := p.Publish(context.Background(), "same", []byte("AAAA")); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := p.Publish(context.Background(), "same", []byte("AAAA")); err != nil {
		t.Fatalf("identical queued retry: %v", err)
	}
	if err := p.Publish(context.Background(), "same", []byte("BBBB")); err == nil {
		t.Fatal("conflicting queued payload must fail")
	}
	close(stall)
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncHistoricalConflictFailsAtFlush(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: []byte("AAAA")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("same"); err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "same", []byte("BBBB")); err != nil {
		t.Fatalf("volatile admission of a historical conflict must not fail Publish: %v", err)
	}
	if err := p.Flush(context.Background()); err == nil {
		t.Fatal("historical conflict must fail Flush before network")
	}
	if n := len(sink.CloneRecords()); n != 0 {
		t.Fatalf("conflicting payload reached sink: %d", n)
	}
}

func TestAsyncIdenticalAckedRetrySkipsSink(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: "same", Subject: "s", Payload: []byte("AAAA")}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("same"); err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "same", []byte("AAAA")); err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(context.Background(), "next", []byte("BBBB")); err != nil {
		t.Fatal(err)
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := sink.CloneRecords()
	if len(got) != 1 || got[0].MsgID != "next" {
		t.Fatalf("acked identical retry should skip the sink: %+v", got)
	}
}

func TestAsyncRetiredRunFailsAtFlush(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := "run-fence"
	if err := j.Append(JournalEntry{MsgID: MessageID(run, TypeBatch, 1), Subject: "s", Payload: []byte(`{"type":"batch"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack(MessageID(run, TypeBatch, 1)); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(JournalEntry{MsgID: MessageID(run, TypeEndReplace, 2), Subject: "s", Payload: endPayload(run)}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack(MessageID(run, TypeEndReplace, 2)); err != nil {
		t.Fatal(err)
	}
	if err := j.CompactAcked(); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j, err = OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j, Subject: "s"}
	p.EnableAsync(8, 1<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), MessageID(run, TypeBatch, 1), []byte("different")); err != nil {
		t.Fatalf("retired-run admission must not fail Publish: %v", err)
	}
	if err := p.Flush(context.Background()); err == nil {
		t.Fatal("retired-run conflict must fail Flush")
	}
	if n := len(sink.CloneRecords()); n != 0 {
		t.Fatalf("retired-run payload reached sink: %d", n)
	}
}
