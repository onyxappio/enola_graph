package graphsession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestEndStatisticsCoverInitialAndResidentDeltaWithoutNoopEvents(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		name := "legacy"
		if frozen {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			root, r, _, sink := residentFixture(t, map[string]string{"src/a.ts": "export function a(){return 1}"}, Options{AuthoritativeFiles: frozen, ChangedOwnersOnly: frozen})
			_, _, ends, err := DecodeRun(sink.CloneRecords())
			if err != nil || len(ends) != 1 {
				t.Fatalf("initial End: %v %v", ends, err)
			}
			if ends[0].Statistics == nil || ends[0].Statistics.TransactionDurationNS <= 0 {
				t.Fatal("initial transaction timing absent")
			}
			if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte("export function a(){return fetch('/a')}"), 0600); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			res, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: r.epoch, From: r.watermark, Through: r.watermark + 1, Covered: true, Paths: []string{"src/a.ts"}})
			elapsed := time.Since(started).Nanoseconds()
			if err != nil {
				t.Fatal(err)
			}
			_, _, ends, err = DecodeRun(sink.CloneRecords())
			if err != nil || len(ends) != 2 {
				t.Fatalf("delta End: %v %v", ends, err)
			}
			stats := ends[1].Statistics
			if stats == nil || stats.TransactionDurationNS <= 0 || stats.TransactionDurationNS > elapsed {
				t.Fatalf("delta timing not within call: %+v / %d", stats, elapsed)
			}
			if ends[1].Completeness.ParsedFiles != res.ParsedFiles {
				t.Fatal("existing parse counters changed")
			}
			coldSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), r.eng, root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: frozen, ChangedOwnersOnly: frozen}); err != nil {
				t.Fatal(err)
			}
			applied, cold := NewConsumer(), NewConsumer()
			applyRun(t, applied, sink)
			applyRun(t, cold, coldSink)
			assertAppliedEqualsCold(t, applied, cold)
			count := len(sink.CloneRecords())
			gen := res.TargetGeneration
			quiet, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: r.epoch, From: r.watermark, Through: r.watermark + 1, Covered: true, Paths: []string{"src/a.ts"}})
			if err != nil {
				t.Fatal(err)
			}
			if quiet.TargetGeneration != gen || quiet.ParsedFiles != 0 || len(sink.CloneRecords()) != count {
				t.Fatal("statistics made identical-write no-op publish or advance")
			}
		})
	}
}

func TestEndStatisticsSampleBeforeSinkDelivery(t *testing.T) {
	start := time.Now().Add(-time.Second)
	j, err := graphstream.OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var got graphstream.EndReplace
	var deliveryEntered int64
	sink := &notifySink{on: func(r graphstream.Recorded) {
		deliveryEntered = time.Since(start).Nanoseconds()
		if err := json.Unmarshal(r.Payload, &got); err != nil {
			t.Error(err)
		}
	}}
	s := &session{transactionStarted: start, pub: &graphstream.Publisher{Sink: sink, Journal: j}}
	if err := s.end(context.Background(), "timing", 0, nil, graphstream.Completeness{Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if got.Statistics == nil || got.Statistics.TransactionDurationNS < int64(time.Second) || got.Statistics.TransactionDurationNS > deliveryEntered {
		t.Fatalf("sample includes End delivery or misses start: %+v / %d", got.Statistics, deliveryEntered)
	}
}
