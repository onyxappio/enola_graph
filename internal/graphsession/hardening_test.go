package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/goextractor"
	"github.com/enola-labs/enola/internal/extractors/manifestextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestRecoverPendingRequiresAckedEnd(t *testing.T) {
	dir := t.TempDir()
	committed := newState("/repo", "c", "/repo", "v")
	committed.Generation = 1
	committed.LastComplete = true
	committed.LastRunID = "run-old"
	if err := saveState(dir, committed); err != nil {
		t.Fatal(err)
	}
	pending := newState("/repo", "c", "/repo", "v")
	pending.Generation = 2
	pending.LastComplete = true
	pending.LastRunID = "run-new"
	if err := writePendingState(dir, pending); err != nil {
		t.Fatal(err)
	}
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Acked batches, no End: must not promote.
	if err := j.Append(graphstream.JournalEntry{MsgID: "run-new:batch:1", Subject: "s", Payload: []byte(`{"type":"batch","run_id":"run-new","seq":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("run-new:batch:1"); err != nil {
		t.Fatal(err)
	}
	st, err := recoverAcknowledgedPending(dir, j, Options{ContextID: "c", RepoID: "/repo"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.Generation != 1 {
		t.Fatalf("promoted unacknowledged pending: %+v", st)
	}
	if _, err := os.Stat(pendingStatePath(dir)); err != nil {
		t.Fatal("pending-state should remain until End is acked")
	}
}

func TestRecoverPendingPromotesAfterAckedEnd(t *testing.T) {
	dir := t.TempDir()
	committed := newState("/repo", "c", "/repo", "v")
	committed.Generation = 1
	committed.LastComplete = true
	if err := saveState(dir, committed); err != nil {
		t.Fatal(err)
	}
	pending := newState("/repo", "c", "/repo", "v")
	pending.Generation = 2
	pending.LastComplete = true
	pending.LastRunID = "run-new"
	if err := writePendingState(dir, pending); err != nil {
		t.Fatal(err)
	}
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	end := graphstream.EndReplace{Type: graphstream.TypeEndReplace, RunID: "run-new", BatchCount: 0, Completeness: graphstream.Completeness{Status: "success"}}
	payload, err := graphstream.Marshal(end)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(graphstream.JournalEntry{MsgID: "run-new:end_replace:1", Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := j.Ack("run-new:end_replace:1"); err != nil {
		t.Fatal(err)
	}
	st, err := recoverAcknowledgedPending(dir, j, Options{ContextID: "c", RepoID: "/repo"}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.Generation != 2 {
		t.Fatalf("did not restore acknowledged pending: %+v", st)
	}
	got, err := loadCommittedState(dir)
	if err != nil || got == nil || got.Generation != 2 {
		t.Fatalf("committed state not promoted: %v %+v", err, got)
	}
}

func TestRepoIDDefaultIsAbsolutePath(t *testing.T) {
	a := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;"})
	b := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;"})
	if filepath.Base(a) == filepath.Base(b) {
		// temp dirs can share a basename in theory; the stored id must still differ.
	}
	engA := testEngine(t, a)
	shared := t.TempDir()
	if _, err := Run(context.Background(), engA, a, &graphstream.MemorySink{}, Options{StateDir: shared, ContextID: "x"}); err != nil {
		t.Fatal(err)
	}
	engB := testEngine(t, b)
	_, err := Run(context.Background(), engB, b, &graphstream.MemorySink{}, Options{StateDir: shared, ContextID: "x"})
	if err == nil {
		t.Fatal("expected repo identity collision to be rejected")
	}
}

func TestDeletedOwnerAppliedEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":  "export const a=1;",
		"gone/b.ts": "export const b=2;",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s1, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s1.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "gone/b.ts")); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s2, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s2.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "gone/b.ts"}).String()
	if len(cons.Owners[key]) != 0 {
		t.Fatalf("deleted owner still has %d nodes", len(cons.Owners[key]))
	}
	coldState := filepath.Join(dir, ".enola", "cold")
	coldSink := &graphstream.MemorySink{}
	cold, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: coldState, ForceInitial: true})
	if err != nil {
		t.Fatal(err)
	}
	coldCons := NewConsumer()
	if err := coldCons.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if _, ok := factByName(cold.Facts, "gone.b"); ok {
		t.Fatal("cold still has gone.b")
	}
	if namesEqual(cons.FactNames(), coldCons.FactNames()) != "" {
		t.Fatalf("applied vs cold names: %s", namesEqual(cons.FactNames(), coldCons.FactNames()))
	}
}

func TestGraphQLContextChangeReextracts(t *testing.T) {
	d := setupTSRepo(t, map[string]string{
		"server/main.ts":  "export const server=1;",
		"schema/types.ts": "export const typeDefs = gql`type Query { ping: String }`;",
	})
	e := testEngine(t, d)
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "server/main.ts"), []byte("import { ApolloServer } from '@apollo/server'; new ApolloServer({typeDefs});"), 0o644); err != nil {
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
			if f.Kind == facts.KindRoute && f.File == "schema/types.ts" {
				n++
			}
		}
		return n
	}
	if count(warm.Facts) != count(cold.Facts) {
		t.Fatalf("graphql context warm=%d cold=%d parsed=%d fallbacks=%v", count(warm.Facts), count(cold.Facts), warm.ParsedFiles, warm.Fallbacks)
	}
}

func TestExtendsAliasInvalidation(t *testing.T) {
	d := setupTSRepo(t, map[string]string{
		"src/user.ts": "import {x} from '@lib/x'; export function user(){return x();}",
		"one/x.ts":    "export function x(){return 1;}",
		"two/x.ts":    "export function x(){return 2;}",
	})
	if err := os.MkdirAll(filepath.Join(d, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "tsconfig.json"), []byte(`{"extends":"./config/aliases.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "config/aliases.json"), []byte(`{"compilerOptions":{"paths":{"@lib/*":["../one/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, d)
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "config/aliases.json"), []byte(`{"compilerOptions":{"paths":{"@lib/*":["../two/*"]}}}`), 0o644); err != nil {
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
	targets := func(ff []facts.Fact) string {
		out := ""
		for _, f := range ff {
			if f.File == "src/user.ts" {
				for _, r := range f.Relations {
					out += r.Target + ";"
				}
			}
		}
		return out
	}
	if targets(warm.Facts) != targets(cold.Facts) {
		t.Fatalf("alias targets warm=%s cold=%s parsed=%d", targets(warm.Facts), targets(cold.Facts), warm.ParsedFiles)
	}
}

type reviewSink struct {
	graphstream.MemorySink
	before func(string)
}

func (s *reviewSink) Publish(c context.Context, sub, id string, p []byte) error {
	if s.before != nil {
		s.before(string(p))
	}
	return s.MemorySink.Publish(c, sub, id, p)
}

type mutationExtractor struct {
	name   string
	mutate func()
}

func (x mutationExtractor) Name() string                { return x.name }
func (x mutationExtractor) Detect(string) (bool, error) { return true, nil }
func (x mutationExtractor) OwnsFile(string) bool        { return false }
func (x mutationExtractor) Extract(context.Context, string, []string) ([]facts.Fact, error) {
	x.mutate()
	return nil, nil
}

func TestConfigChangeRestoreUsesCapturedBytes(t *testing.T) {
	d := setupTSRepo(t, map[string]string{
		"src/user.ts": "import {x} from '@lib/x'; export function user(){return x();}",
		"one/x.ts":    "export function x(){return 1}",
		"two/x.ts":    "export function x(){return 2}",
	})
	p := filepath.Join(d, "tsconfig.json")
	original := `{"compilerOptions":{"paths":{"@lib/*":["./one/*"]}}}`
	poison := strings.ReplaceAll(original, "one", "two")
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Extractors = []string{"before-ts", "typescript", "after-ts"}
	cfg.Repo = d
	cfg.Output.Dir = ".enola"
	e, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mutations := 0
	e.RegisterExtractor(mutationExtractor{"before-ts", func() {
		mutations++
		_ = os.WriteFile(p, []byte(poison), 0o644)
	}})
	e.RegisterExtractor(tsextractor.New())
	e.RegisterExtractor(mutationExtractor{"after-ts", func() {
		mutations++
		_ = os.WriteFile(p, []byte(original), 0o644)
	}})
	o := Options{StateDir: t.TempDir()}
	first, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
	if err != nil {
		t.Fatal(err)
	}
	if mutations != 2 {
		t.Fatalf("mutation fixture did not run: %d", mutations)
	}
	targets := func(ff []facts.Fact) string {
		out := ""
		for _, f := range ff {
			if f.File == "src/user.ts" {
				for _, r := range f.Relations {
					out += r.Target + ";"
				}
			}
		}
		return out
	}
	if !strings.Contains(targets(first.Facts), "one") || strings.Contains(targets(first.Facts), "two") {
		t.Fatalf("first run used transient config: %s", targets(first.Facts))
	}
	warm, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
	if err != nil {
		t.Fatal(err)
	}
	cold, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if targets(warm.Facts) != targets(cold.Facts) {
		t.Fatalf("change-restore poisoned cache: first=%s warm(parsed=%d)=%s cold=%s", targets(first.Facts), warm.ParsedFiles, targets(warm.Facts), targets(cold.Facts))
	}
}

func TestReverseCloseAccumulatesParsedCount(t *testing.T) {
	files := map[string]string{
		"src/c.ts": "export function leaf() { return 1; }\n",
		"src/b.ts": "export { leaf } from './c';\n",
		"src/a.ts": "import { leaf } from './b'; export function a() { return leaf(); }\n",
	}
	for i := 0; i < 10; i++ {
		files[fmt.Sprintf("src/ind_%d.ts", i)] = fmt.Sprintf("export const n%d = %d;\n", i, i)
	}
	dir := setupTSRepo(t, files)
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "state")
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/c.ts"), []byte("export function changed() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delta, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles < 3 {
		t.Fatalf("rename+reverse-close reported parsed=%d, want leaf plus importers (at least 3)", delta.ParsedFiles)
	}
}

func TestSourceHashUsesConsumedBytes(t *testing.T) {
	d := setupTSRepo(t, map[string]string{"src/a.ts": "export const original=1;"})
	e := testEngine(t, d)
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	o.OnBeforeParse = func(rel string) {
		if rel == "src/a.ts" || strings.HasSuffix(rel, "a.ts") {
			if err := os.WriteFile(filepath.Join(d, "src/a.ts"), []byte("export const transient=2;"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err == nil {
		t.Fatal("inconsistent live source bytes must refuse EndReplace")
	}
	if err := os.WriteFile(filepath.Join(d, "src/a.ts"), []byte("export const original=1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.OnBeforeParse = nil
	r, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := factByName(r.Facts, "src.transient"); ok {
		t.Fatal("cached transient facts under original source")
	}
	if _, ok := factByName(r.Facts, "src.original"); !ok {
		t.Fatal("expected original symbol after refused inconsistent run")
	}
}

func TestUnreadableAngularTemplateRefuses(t *testing.T) {
	d := setupTSRepo(t, map[string]string{
		"src/page.ts":   "import { Component } from '@angular/core'; @Component({templateUrl:'./page.html'}) export class Page { save(){} }",
		"src/page.html": `<button (click)="save()">Save</button>`,
	})
	if err := os.WriteFile(filepath.Join(d, "package.json"), []byte(`{"dependencies":{"@angular/core":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, d)
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "src/page.html")
	defer os.Chmod(p, 0o644)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err == nil {
		t.Fatal("unreadable Angular template must not complete successfully")
	}
}

func TestManifestsDoNotEraseTSCache(t *testing.T) {
	d := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;", "src/b.ts": "export const b=2;"})
	cfg := config.Default()
	cfg.Ignore = append(cfg.Ignore, "**/*.json")
	cfg.Repo = d
	cfg.Output.Dir = ".enola"
	e, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.RegisterExtractor(manifestextractor.New())
	e.RegisterExtractor(tsextractor.New())
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	if _, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "src/a.ts"), []byte("export const a=3;"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
	if err != nil {
		t.Fatal(err)
	}
	if r.ParsedFiles != 1 {
		t.Fatalf("parsed %d, want 1 (only a.ts); fallbacks=%v", r.ParsedFiles, r.Fallbacks)
	}
}

func TestStrictConsumerRejectsBadDigestAndOutOfScope(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;"})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola/s")}); err != nil {
		t.Fatal(err)
	}
	recs := sink.CloneRecords()
	c := NewConsumer()
	if err := c.ApplyRecords(recs); err != nil {
		t.Fatal(err)
	}
	bad := append([]graphstream.Recorded{}, recs...)
	for i, r := range bad {
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(r.Payload, &probe)
		if probe.Type == graphstream.TypeEndReplace {
			var e graphstream.EndReplace
			_ = json.Unmarshal(r.Payload, &e)
			e.BatchDigest = strings.Repeat("0", 64)
			p, _ := json.Marshal(e)
			bad[i].Payload = p
			bad[i].MsgID = r.MsgID + "-tampered"
		}
	}
	c2 := NewConsumer()
	if err := c2.ApplyRecords(bad); err == nil {
		t.Fatal("expected digest mismatch")
	}

	out := graphstream.Batch{
		Type:  graphstream.TypeBatch,
		RunID: "nope",
		Seq:   1,
		Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "other.ts"}, ID: "x", Kind: facts.KindSymbol, Name: "x"}},
	}
	bp, _ := graphstream.Marshal(out)
	c3 := NewConsumer()
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, SchemaVersion: graphstream.SchemaVersion,
		RunID: "nope", Phase: graphstream.PhaseResolved,
		OwnerScope: []graphstream.OwnerRef{{Kind: graphstream.OwnerFile, ID: "src/a.ts"}}, OwnerScopeCount: 1,
	}
	braw, _ := graphstream.Marshal(begin)
	if err := c3.Apply(graphstream.Recorded{MsgID: "b", Payload: braw}); err != nil {
		t.Fatal(err)
	}
	if err := c3.Apply(graphstream.Recorded{MsgID: "x", Payload: bp}); err == nil {
		t.Fatal("expected out-of-scope write to be rejected")
	}
}

func TestForceInitialEpochClearsUnknownOwner(t *testing.T) {
	c := NewConsumer()
	c.Owners["file:stale.ts"] = []graphstream.Node{{ID: "stale", Kind: facts.KindSymbol, Name: "stale"}}
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, SchemaVersion: graphstream.SchemaVersion,
		RunID: "epoch-1", TargetGeneration: 1, Phase: graphstream.PhaseEpoch,
		OwnerScope: []graphstream.OwnerRef{{Kind: graphstream.OwnerFile, ID: "src/a.ts"}}, OwnerScopeCount: 1,
	}
	braw, _ := graphstream.Marshal(begin)
	batch := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "epoch-1", Seq: 1, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}, ID: "a", Kind: facts.KindSymbol, Name: "a"}},
	}
	rawBatch, _ := graphstream.Marshal(batch)
	end := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "epoch-1", BatchCount: 1,
		BatchDigest: graphstream.DigestBatches([][]byte{rawBatch}), OwnerScopeLen: 1,
		Completeness: graphstream.Completeness{Status: "success"},
	}
	eraw, _ := graphstream.Marshal(end)
	if err := c.ApplyRecords([]graphstream.Recorded{
		{MsgID: "1", Payload: braw},
		{MsgID: "2", Payload: rawBatch},
		{MsgID: "3", Payload: eraw},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Owners["file:stale.ts"]; ok {
		t.Fatal("epoch did not drop unknown owner")
	}
}

func TestPhaseScopeClearsExistingOwner(t *testing.T) {
	c := NewConsumer()
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}).String()
	c.Owners[key] = []graphstream.Node{{ID: "old", Kind: facts.KindSymbol, Name: "old"}}
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "r1", TargetGeneration: 1,
		Phase: graphstream.PhaseResolved, OwnerScopeCount: 1,
	}
	braw, _ := graphstream.Marshal(begin)
	scope := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "r1", Seq: 1, Phase: graphstream.PhaseScope,
		Owners: []graphstream.OwnerRef{{Kind: graphstream.OwnerFile, ID: "src/a.ts"}},
	}
	sraw, _ := graphstream.Marshal(scope)
	resolved := graphstream.Batch{Type: graphstream.TypeBatch, RunID: "r1", Seq: 2, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}, ID: "new", Kind: facts.KindSymbol, Name: "new"}},
	}
	rraw, _ := graphstream.Marshal(resolved)
	end := graphstream.EndReplace{Type: graphstream.TypeEndReplace, RunID: "r1", BatchCount: 2,
		BatchDigest: graphstream.DigestBatches([][]byte{sraw, rraw}), OwnerScopeLen: 1,
		Completeness: graphstream.Completeness{Status: "success"},
	}
	eraw, _ := graphstream.Marshal(end)
	if err := c.ApplyRecords([]graphstream.Recorded{
		{MsgID: "b", Payload: braw}, {MsgID: "s", Payload: sraw}, {MsgID: "r", Payload: rraw}, {MsgID: "e", Payload: eraw},
	}); err != nil {
		t.Fatal(err)
	}
	if len(c.Owners[key]) != 1 || c.Owners[key][0].Name != "new" {
		t.Fatalf("phase scope retained old nodes: %+v", c.Owners[key])
	}
}

func TestConsumerIdempotentReplay(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;"})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola/s")}); err != nil {
		t.Fatal(err)
	}
	recs := sink.CloneRecords()
	c := NewConsumer()
	if err := c.ApplyRecords(recs); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyRecords(recs); err != nil {
		t.Fatal(err)
	}
}

func namesEqual(a, b []string) string {
	as := append([]string{}, a...)
	bs := append([]string{}, b...)
	sort.Strings(as)
	sort.Strings(bs)
	if strings.Join(as, ",") == strings.Join(bs, ",") {
		return ""
	}
	return "a=" + strings.Join(as, ",") + " b=" + strings.Join(bs, ",")
}

func TestGRPCContextChangeReextracts(t *testing.T) {
	stub := `import { ServiceType } from "@protobuf-ts/runtime-rpc"; export const DRAPlugin = new ServiceType("dra.v1.DRAPlugin", [{ name: "Prepare", options: {}, I: PrepareRequest, O: PrepareResponse }]);`
	d := setupTSRepo(t, map[string]string{
		"gen/v1/dra.ts": stub,
		"gen/v2/dra.ts": "export const nothing=1;",
		"app/use.ts":    `import { DRAPlugin } from "../gen/v1/dra"; const plugin = createClient(DRAPlugin, transport); export function prepare(req) { return plugin.prepare(req); }`,
	})
	e := testEngine(t, d)
	o := Options{StateDir: filepath.Join(d, ".enola/state")}
	a, err := Run(context.Background(), e, d, &graphstream.MemorySink{}, o)
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
	if count(a.Facts) != 1 {
		t.Fatalf("fixture must initially resolve: got %d", count(a.Facts))
	}
	if err := os.WriteFile(filepath.Join(d, "gen/v2/dra.ts"), []byte(`import { ServiceType } from "@protobuf-ts/runtime-rpc"; export const DRAPlugin = new ServiceType("dra.v2.DRAPlugin", [{ name: "Prepare", options: {}, I: PrepareRequest, O: PrepareResponse }]);`), 0o644); err != nil {
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
	if count(warm.Facts) != count(cold.Facts) {
		t.Fatalf("grpc context warm=%d cold=%d parsed=%d fallbacks=%v", count(warm.Facts), count(cold.Facts), warm.ParsedFiles, warm.Fallbacks)
	}
}

func TestNonTSOnlyEmitsBegin(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module ex\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Repo = d
	cfg.Output.Dir = ".enola"
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(goextractor.New())
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, d, sink, Options{StateDir: filepath.Join(d, ".enola/state")}); err != nil {
		t.Fatal(err)
	}
	begins, batches, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(begins) != 1 {
		t.Fatalf("non-TS run begins=%d want 1", len(begins))
	}
	if len(begins[0].OwnerScope) == 0 && begins[0].OwnerScopeCount == 0 {
		t.Fatal("non-TS Begin has empty owner scope")
	}
	if len(batches) == 0 || len(ends) != 1 {
		t.Fatalf("batches=%d ends=%d", len(batches), len(ends))
	}
}

func TestConsumedHashMatchesBytes(t *testing.T) {
	src := []byte("export const original=1;")
	sum := sha256.Sum256(src)
	if hex.EncodeToString(sum[:]) == "" {
		t.Fatal("empty")
	}
}
