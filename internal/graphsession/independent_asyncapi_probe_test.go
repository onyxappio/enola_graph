package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/asyncapiextractor"
	"testing"
)

func TestIndependentArbitraryJSONAsyncAPI(t *testing.T) {
	spec := `{"asyncapi":"2.6.0","info":{"title":"Orders","version":"1.0.0"},"channels":{"orders.created":{"publish":{"operationId":"emitOrder","message":{"payload":{"type":"object"}}}}}}`
	dir := setupTSRepo(t, map[string]string{"api.json": spec})
	ext := asyncapiextractor.New()
	eng := independentEngine(t, dir, nil, ext)
	detected, err := ext.Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !detected {
		t.Error("api.json AsyncAPI not detected by Detect")
	}
	inv, err := eng.Inventory(dir)
	if err != nil {
		t.Fatal(err)
	}
	detected, err = eng.DetectExtractor(ext, dir, inv.AllNames)
	if err != nil {
		t.Fatal(err)
	}
	if !detected {
		t.Error("api.json AsyncAPI not detected through engine FileListDetector")
	}
	direct, err := ext.Extract(context.Background(), dir, inv.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(direct) == 0 {
		t.Fatal("fixture does not extract AsyncAPI facts")
	}
	consumer := NewConsumer()
	r, n := independentRun(t, eng, dir, "state", consumer)
	nodes := 0
	for _, owned := range consumer.Owners {
		nodes += len(owned)
	}
	if len(r.Facts) != len(direct) || nodes == 0 {
		t.Errorf("cold stream loses independently extracted AsyncAPI facts: direct=%d streamed=%d nodes=%d events=%d", len(direct), len(r.Facts), nodes, n)
	}
	cold := NewConsumer()
	independentRun(t, eng, dir, "cold", cold)
	assertAppliedEqualsCold(t, consumer, cold)
}
