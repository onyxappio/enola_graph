package graphstream

import (
	"context"
	"testing"
	"time"
)

func TestRootFreeQueueDoesNotWaitOnJournal(t *testing.T) {
	j, e := OpenJournal(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	p := &Publisher{Journal: j, Sink: &MemorySink{}, Subject: "s"}
	p.EnableAsync(64, 1<<20)
	j.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- p.Publish(context.Background(), "id", []byte(`{"x":1}`)) }()
	blocked := false
	select {
	case e := <-done:
		if e != nil {
			t.Error(e)
		}
	case <-time.After(200 * time.Millisecond):
		blocked = true
	}
	j.mu.Unlock()
	if blocked {
		<-done
	}
	if e := p.Flush(context.Background()); e != nil {
		t.Error(e)
	}
	p.CloseAsync()
	if blocked {
		t.Fatal("free-capacity Publish waited for journal mutex; producer still blocks on disk sync")
	}
}
