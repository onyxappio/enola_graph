package graphstream

import (
	"context"
	"fmt"
	"testing"
)

type discardTransportSink struct{}

func (discardTransportSink) Publish(context.Context, string, string, []byte) error { return nil }
func (discardTransportSink) Flush(context.Context) error                           { return nil }
func (discardTransportSink) Close() error                                          { return nil }

// Isolate admission, encoding classification and acknowledgment scheduling from
// broker and journal latency; retain the production queue bounds.
func BenchmarkAsyncTransport(b *testing.B) {
	for _, count := range []int{1000, 4000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			payloads := make([][]byte, count)
			ids := make([]string, count)
			for i := range payloads {
				ids[i] = MessageID("bench", TypeBatch, i+1)
				payloads[i], _ = Marshal(Batch{Type: TypeBatch, RunID: "bench", Seq: i + 1, Phase: PhaseResolved, Nodes: []Node{{Owner: OwnerRef{Kind: OwnerFile, ID: "src/a.ts"}, ID: "a", Kind: "function", Name: "a"}}})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p := &Publisher{Sink: discardTransportSink{}}
				p.EnableAsync(32, 8<<20)
				for n, payload := range payloads {
					if err := p.Publish(context.Background(), ids[n], payload); err != nil {
						b.Fatal(err)
					}
				}
				if err := p.Flush(context.Background()); err != nil {
					b.Fatal(err)
				}
				p.CloseAsync()
			}
		})
	}
}

func BenchmarkTransportMetadata(b *testing.B) {
	batch := Batch{Type: TypeBatch, RunID: "bench", Seq: 1, Phase: PhaseResolved}
	for i := 0; i < 64; i++ {
		batch.Nodes = append(batch.Nodes, Node{Owner: OwnerRef{Kind: OwnerFile, ID: "apps/service/src/feature/handler.ts"}, ID: fmt.Sprint(i), Kind: "function", Name: "handler", Props: map[string]any{"exported": true, "signature": "(request: Request): Response"}})
		batch.Edges = append(batch.Edges, Edge{Owner: OwnerRef{Kind: OwnerFile, ID: "apps/service/src/feature/handler.ts"}, FromID: fmt.Sprint(i), Kind: "calls", TargetName: "service.handler", Resolution: ResResolved, TargetID: "target"})
	}
	payload, err := Marshal(batch)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inspectPayload(payload)
	}
}
