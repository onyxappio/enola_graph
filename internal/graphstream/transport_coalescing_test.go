package graphstream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommitCoalescingBypassesBarriers(t *testing.T) {
	for _, kind := range []jobKind{kindBegin, kindScope, kindEnd} {
		a := &asyncPub{maxItems: 32, maxBytes: 1024, pending: []asyncJob{{kind: kind}}}
		p := &Publisher{Journal: &Journal{}, async: a}
		// No stop context: entering the timer select instead of bypassing panics.
		a.mu.Lock()
		p.coalesceLocked()
		a.mu.Unlock()
	}
	a := &asyncPub{maxItems: 32, maxBytes: 1024, pending: []asyncJob{{kind: kindResolved}}, flushers: 1}
	p := &Publisher{Journal: &Journal{}, async: a}
	a.mu.Lock()
	p.coalesceLocked()
	a.mu.Unlock()
}

func TestCommitCoalescingSparseProducerProgress(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	synced := make(chan struct{}, 1)
	j.SetSyncHook(func() {
		select {
		case synced <- struct{}{}:
		default:
		}
	})
	p := &Publisher{Sink: discardTransportSink{}, Journal: j}
	p.EnableAsync(32, 8<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "one", []byte(`{"type":"batch","phase":"resolved"}`)); err != nil {
		t.Fatal(err)
	}
	// No Flush and no second message: the fixed deadline must release the group.
	select {
	case <-synced:
	case <-time.After(time.Second):
		t.Fatal("sparse group never committed")
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCommitCoalescingFlushReturnsDurabilityError(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	// Prevent creation of the commit index, after payload file fsync succeeds.
	if err := os.Mkdir(filepath.Join(dir, "commit.json.tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	p := &Publisher{Sink: sink, Journal: j}
	p.EnableAsync(32, 8<<20)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "one", []byte(`{"type":"batch","phase":"resolved"}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = p.Flush(ctx)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Flush must expose durability failure: %v", err)
	}
	if len(sink.CloneRecords()) != 0 {
		t.Fatal("published before successful durability fence")
	}
}
