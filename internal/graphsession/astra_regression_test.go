package graphsession

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestEmptyConsumerRejectsNonZeroBase(t *testing.T) {
	c := NewConsumer()
	err := c.Apply(graphstream.Recorded{MsgID: "b", Payload: mustJSON(graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "delta", BaseGeneration: 41, TargetGeneration: 42, Phase: graphstream.PhaseResolved,
	})})
	if err == nil {
		t.Fatal("empty consumer must require base 0")
	}
}

func TestRetryRejectedEndCommitsWhenBatchesArrive(t *testing.T) {
	c := NewConsumer()
	b := graphstream.Recorded{MsgID: "b", Payload: mustJSON(graphstream.BeginReplace{Type: graphstream.TypeBeginReplace, RunID: "r", TargetGeneration: 1})}
	xraw, _ := graphstream.Marshal(graphstream.Batch{Type: graphstream.TypeBatch, RunID: "r", Seq: 1, Phase: graphstream.PhaseResolved})
	x := graphstream.Recorded{MsgID: "x", Payload: xraw}
	e := graphstream.Recorded{MsgID: "e", Payload: mustJSON(graphstream.EndReplace{Type: graphstream.TypeEndReplace, RunID: "r", BatchCount: 1, BatchDigest: graphstream.DigestBatches([][]byte{x.Payload}), Completeness: graphstream.Completeness{Status: "success"}})}
	if err := c.Apply(b); err != nil {
		t.Fatal(err)
	}
	if c.Apply(e) == nil {
		t.Fatal("expected incomplete error")
	}
	if err := c.Apply(x); err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(e); err != nil {
		t.Fatal(err)
	}
	if c.LastGeneration != 1 {
		t.Fatalf("generation %d, want 1 after retry", c.LastGeneration)
	}
}

func TestIdentityBoundBeforeReplay(t *testing.T) {
	a := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	b := setupTSRepo(t, map[string]string{"b.ts": "export const b=1"})
	state := t.TempDir()
	s := &graphstream.MemorySink{}
	s.FailAt(1, fmt.Errorf("offline"))
	if _, err := Run(context.Background(), testEngine(t, a), a, s, Options{StateDir: state, ContextID: "A", SinkID: "sink-A"}); err == nil {
		t.Fatal("must fail")
	}
	out := &graphstream.MemorySink{}
	_, err := Run(context.Background(), testEngine(t, b), b, out, Options{StateDir: state, ContextID: "B", SinkID: "sink-B"})
	if err == nil {
		t.Fatal("expected identity rejection before replay into a different repo/context/sink")
	}
}

func TestRouterMountChangeRepublishesRouteOwners(t *testing.T) {
	server := `import express from 'express'; import router from './api/orders.js'; const app=express();app.use('/v1',router);`
	d := setupTSRepo(t, map[string]string{
		"src/server.ts":     server,
		"src/api/orders.ts": `import express from 'express'; const router=express.Router();router.get('/orders',handler);export default router;`,
	})
	eng := testEngine(t, d)
	o := Options{StateDir: t.TempDir()}
	c := NewConsumer()
	s := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, d, s, o); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(s.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "src/server.ts"), []byte(strings.ReplaceAll(server, "v1", "v2")), 0o644); err != nil {
		t.Fatal(err)
	}
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, d, s, o); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(s.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	o.StateDir = t.TempDir()
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, d, coldSink, o); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	if err := cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, c, cold)
}

func TestGRPCMethodChangeInvalidatesClients(t *testing.T) {
	stub := `import { ServiceType } from "@protobuf-ts/runtime-rpc"; export const DRAPlugin = new ServiceType("dra.v1.DRAPlugin", [{ name: "Prepare", options: {}, I: PrepareRequest, O: PrepareResponse }]);`
	d := setupTSRepo(t, map[string]string{
		"gen/v1/dra.ts": stub,
		"app/use.ts":    `import { DRAPlugin } from "../gen/v1/dra"; const plugin = createClient(DRAPlugin, transport); export function prepare(req) { return plugin.prepare(req); }`,
	})
	e := testEngine(t, d)
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "gen/v1/dra.ts"), []byte(`import { ServiceType } from "@protobuf-ts/runtime-rpc"; export const DRAPlugin = new ServiceType("dra.v1.DRAPlugin", [{ name: "Other", options: {}, I: PrepareRequest, O: PrepareResponse }]);`), 0o644); err != nil {
		t.Fatal(err)
	}
	warm, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
	if err != nil {
		t.Fatal(err)
	}
	o.StateDir = filepath.Join(d, ".enola/cold")
	o.ForceInitial = true
	cold, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
	if err != nil {
		t.Fatal(err)
	}
	count := func(ff []facts.Fact) int {
		n := 0
		for _, f := range ff {
			if f.Kind == facts.KindRoute && f.Props["framework"] == "grpc" {
				n++
			}
		}
		return n
	}
	if count(warm.Facts) != count(cold.Facts) {
		t.Fatalf("grpc method context warm=%d cold=%d parsed=%d fallbacks=%v", count(warm.Facts), count(cold.Facts), warm.ParsedFiles, warm.Fallbacks)
	}
}

func TestIncrementalScopeAllowsLocalBeforeManifest(t *testing.T) {
	c := NewConsumer()
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "epoch", TargetGeneration: 1,
		Phase: graphstream.PhaseEpoch, ScopeMode: graphstream.ScopeModeIncremental,
	}
	braw, _ := graphstream.Marshal(begin)
	owner := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}
	local := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "epoch", Seq: 1, Phase: graphstream.PhaseLocal,
		Nodes: []graphstream.Node{{Owner: owner, ID: "a", Kind: facts.KindSymbol, Name: "a"}},
	}
	lraw, _ := graphstream.Marshal(local)
	scope := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "epoch", Seq: 2, Phase: graphstream.PhaseScope, Owners: []graphstream.OwnerRef{owner}}
	sraw, _ := graphstream.Marshal(scope)
	resolved := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "epoch", Seq: 3, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: owner, ID: "a", Kind: facts.KindSymbol, Name: "a"}},
	}
	rraw, _ := graphstream.Marshal(resolved)
	end := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "epoch", BatchCount: 3,
		BatchDigest:   graphstream.DigestBatches([][]byte{lraw, sraw, rraw}),
		OwnerScopeLen: 1, OwnerScopeDigest: graphstream.DigestOwners([]graphstream.OwnerRef{owner}),
		Completeness: graphstream.Completeness{Status: "success"},
	}
	eraw, _ := graphstream.Marshal(end)
	if err := c.ApplyRecords([]graphstream.Recorded{
		{MsgID: "b", Payload: braw},
		{MsgID: "l", Payload: lraw},
		{MsgID: "s", Payload: sraw},
		{MsgID: "r", Payload: rraw},
		{MsgID: "e", Payload: eraw},
	}); err != nil {
		t.Fatal(err)
	}
	if c.LastGeneration != 1 {
		t.Fatalf("generation %d", c.LastGeneration)
	}
	out := graphstream.Batch{
		Type: graphstream.TypeBatch, RunID: "bad", Seq: 1, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "other.ts"}, ID: "x", Kind: facts.KindSymbol, Name: "x"}},
	}
	bp, _ := graphstream.Marshal(out)
	c2 := NewConsumer()
	begin2 := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "bad", TargetGeneration: 1,
		Phase: graphstream.PhaseEpoch, ScopeMode: graphstream.ScopeModeIncremental,
		OwnerScope: []graphstream.OwnerRef{owner}, OwnerScopeCount: 1,
	}
	b2, _ := graphstream.Marshal(begin2)
	if err := c2.Apply(graphstream.Recorded{MsgID: "b", Payload: b2}); err != nil {
		t.Fatal(err)
	}
	if err := c2.Apply(graphstream.Recorded{MsgID: "x", Payload: bp}); err != nil {
		t.Fatal(err)
	}
	endBad := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "bad", BatchCount: 1,
		BatchDigest: graphstream.DigestBatches([][]byte{bp}), OwnerScopeLen: 1,
		OwnerScopeDigest: graphstream.DigestOwners([]graphstream.OwnerRef{owner}),
		Completeness:     graphstream.Completeness{Status: "success"},
	}
	eb, _ := graphstream.Marshal(endBad)
	if err := c2.Apply(graphstream.Recorded{MsgID: "e", Payload: eb}); err == nil {
		t.Fatal("expected out-of-scope resolved write to fail at EndReplace")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
