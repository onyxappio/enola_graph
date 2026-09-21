package graphstream

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Identical journaled workload for transport revisions: real fsync, bounded
// 32-message queue, eight workers, pre-encoded 25KB envelopes, no broker delay.
func BenchmarkJournalTransport(b *testing.B) {
	const count = 1000
	batch := Batch{Type: TypeBatch, RunID: "bench", Phase: PhaseResolved}
	for i := 0; i < 64; i++ {
		batch.Nodes = append(batch.Nodes, Node{Owner: OwnerRef{Kind: OwnerFile, ID: "apps/service/src/feature/handler.ts"}, ID: fmt.Sprint(i), Kind: "function", Name: "handler", Props: map[string]any{"exported": true, "signature": "(request: Request): Response"}})
		batch.Edges = append(batch.Edges, Edge{Owner: OwnerRef{Kind: OwnerFile, ID: "apps/service/src/feature/handler.ts"}, FromID: fmt.Sprint(i), Kind: "calls", TargetName: "service.handler", Resolution: ResResolved, TargetID: "target"})
	}
	payloads := make([][]byte, count)
	for i := range payloads {
		batch.Seq = i + 1
		payloads[i], _ = Marshal(batch)
	}
	var groups, singles, messages int
	var elapsed time.Duration
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		j, err := OpenJournal(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		previous := 0
		j.SetSyncHook(func() {
			j.mu.Lock()
			size := len(j.order) - previous
			previous = len(j.order)
			j.mu.Unlock()
			if size > 0 {
				groups++
				messages += size
				if size == 1 {
					singles++
				}
			}
		})
		p := &Publisher{Sink: discardTransportSink{}, Journal: j}
		p.EnableAsync(32, 8<<20)
		b.StartTimer()
		start := time.Now()
		for i, payload := range payloads {
			if err := p.Publish(context.Background(), fmt.Sprint(i), payload); err != nil {
				b.Fatal(err)
			}
		}
		if err := p.Flush(context.Background()); err != nil {
			b.Fatal(err)
		}
		elapsed += time.Since(start)
		b.StopTimer()
		p.CloseAsync()
		j.Close()
		b.StartTimer()
	}
	b.ReportMetric(float64(groups)/float64(b.N), "groups/run")
	b.ReportMetric(float64(singles)/float64(b.N), "singles/run")
	b.ReportMetric(float64(messages)/float64(groups), "messages/group")
	b.ReportMetric(elapsed.Seconds()/float64(b.N), "seconds/run")
}
