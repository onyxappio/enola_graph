package graphstream

import (
	"context"
	"testing"
)

func TestIndependentAsyncExistingUnackedFlush(t *testing.T) {
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.Append(JournalEntry{MsgID: "retry", Subject: "s", Payload: []byte("same")}); err != nil {
		t.Fatal(err)
	}
	sink := &MemorySink{}
	p := &Publisher{Journal: j, Sink: sink, Subject: "s"}
	p.EnableAsync(4, 1024)
	defer p.CloseAsync()
	if err := p.Publish(context.Background(), "retry", []byte("same")); err != nil {
		t.Fatal(err)
	}
	if err := p.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(j.Unacked()) != 0 || len(sink.CloneRecords()) != 1 {
		t.Fatalf("successful Flush left unacked=%d network=%d", len(j.Unacked()), len(sink.CloneRecords()))
	}
}
