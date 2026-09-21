package graphsession

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestFrozenBeginPrecedesParsingAndNeverGrows(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export function a(){ return 1; }", "b.ts": "import {a} from './a'; export function b(){return a();}"})
	eng := testEngine(t, root)
	sink := &graphstream.MemorySink{}
	state := t.TempDir()
	cons := NewConsumer()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	opts.OnBeforeParse = func(string) {
		begins, _, _, err := DecodeRun(sink.CloneRecords())
		if err != nil || len(begins) != 1 {
			t.Errorf("expected full Begin before parse: %d %v", len(begins), err)
		}
	}
	first, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	check := func(sink *graphstream.MemorySink) {
		t.Helper()
		bs, batches, ends, err := DecodeRun(sink.CloneRecords())
		if err != nil || len(bs) != 1 || len(ends) != 1 {
			t.Fatalf("envelopes %d %d %v", len(bs), len(ends), err)
		}
		b, e := bs[0], ends[0]
		if b.SchemaVersion != graphstream.FrozenSchemaVersion || b.OwnerScopeCount != len(b.OwnerScope) || b.OwnerScopeDigest != e.OwnerScopeDigest || b.OwnerScopeCount != e.OwnerScopeLen {
			t.Fatal("manifest mismatch")
		}
		for _, batch := range batches {
			if batch.Phase != graphstream.PhaseResolved {
				t.Fatalf("unexpected phase %s", batch.Phase)
			}
		}
		if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
	}
	check(sink)
	opts.OnBeforeParse = nil
	if err := os.Rename(filepath.Join(root, "a.ts"), filepath.Join(root, "renamed.ts")); err != nil {
		t.Fatal(err)
	}
	sink = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	check(sink)
	bs, _, _, _ := DecodeRun(sink.CloneRecords())
	owners := map[string]bool{}
	for _, o := range bs[0].OwnerScope {
		owners[o.ID] = true
	}
	if !owners["a.ts"] || !owners["renamed.ts"] {
		t.Fatal("rename lost old/new identity")
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
	noop := &graphstream.MemorySink{}
	r, err := Run(context.Background(), eng, root, noop, opts)
	if err != nil || len(noop.CloneRecords()) != 0 || r.BaseGeneration != r.TargetGeneration || first.TargetGeneration != 1 {
		t.Fatalf("noop: %+v %v events=%d", r, err, len(noop.CloneRecords()))
	}
}

func TestFrozenOversizeBeginPublishesNothing(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	sink := &graphstream.MemorySink{}
	_, err := Run(context.Background(), testEngine(t, root), root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, MaxBeginBytes: 1})
	if err == nil || len(sink.CloneRecords()) != 0 {
		t.Fatalf("oversized Begin: %v records=%d", err, len(sink.CloneRecords()))
	}
	if !strings.Contains(err.Error(), "no scope chunks allowed") || !strings.Contains(err.Error(), "exceeds limit 1") {
		t.Fatalf("want fail-closed frozen Begin error, got %v", err)
	}
}

func TestCLIFrozenMaxBeginBytesPublishesNothing(t *testing.T) {
	bin := buildEnola(t)
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	state := t.TempDir()
	events := filepath.Join(t.TempDir(), "events.jsonl")
	cmd := exec.Command(bin, "graph", "analyze", "--authoritative-scope", "--max-begin-bytes", "1",
		"--context", "cli-test", "--state-dir", state, "--events", events, "--json", root)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("CLI accepted oversized frozen Begin:\n%s", out)
	}
	if !strings.Contains(string(out), "no scope chunks allowed") {
		t.Fatalf("CLI error missing fail-closed text:\n%s", out)
	}
	if st, statErr := os.Stat(events); statErr == nil && st.Size() != 0 {
		t.Fatalf("CLI published %d event bytes", st.Size())
	}
}

func TestFrozenEmptyInitialPublishesEpoch(t *testing.T) {
	root := t.TempDir()
	eng := testEngine(t, root)
	sink := &graphstream.MemorySink{}
	parsed := 0
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, OnBeforeParse: func(string) { parsed++ }}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != 0 {
		t.Fatalf("empty initial parsed %d files", parsed)
	}
	if res.TargetGeneration != 1 || len(sink.CloneRecords()) == 0 {
		t.Fatalf("empty initial gen=%d events=%d", res.TargetGeneration, len(sink.CloneRecords()))
	}
	bs, batches, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) != 1 || len(ends) != 1 {
		t.Fatalf("envelopes %d %d %v", len(bs), len(ends), err)
	}
	if bs[0].SchemaVersion != graphstream.FrozenSchemaVersion || bs[0].Phase != graphstream.PhaseEpoch || bs[0].ScopeMode != graphstream.ScopeModeComplete {
		t.Fatalf("empty begin %+v", bs[0])
	}
	if bs[0].OwnerScopeCount != len(bs[0].OwnerScope) || bs[0].OwnerScopeDigest != ends[0].OwnerScopeDigest || ends[0].OwnerScopeLen != bs[0].OwnerScopeCount || ends[0].BatchDigest == "" {
		t.Fatal("empty manifest mismatch")
	}
	for _, b := range batches {
		if b.Phase != graphstream.PhaseResolved {
			t.Fatalf("unexpected phase %s", b.Phase)
		}
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if cons.LastGeneration != 1 {
		t.Fatalf("empty consumer gen=%d", cons.LastGeneration)
	}
}

func TestFrozenLastFileEmptyReplacement(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, root, s1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s1.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if len(cons.FactNames()) == 0 {
		t.Fatal("initial missing owned facts")
	}
	if err := os.Remove(filepath.Join(root, "a.ts")); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, s2, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.TargetGeneration != first.TargetGeneration+1 || len(s2.CloneRecords()) == 0 {
		t.Fatalf("last-file deletion skipped empty replacement %+v events=%d", delta, len(s2.CloneRecords()))
	}
	bs, batches, ends, err := DecodeRun(s2.CloneRecords())
	if err != nil || len(bs) != 1 || len(ends) != 1 {
		t.Fatalf("last-file envelopes %d %d %v", len(bs), len(ends), err)
	}
	if bs[0].SchemaVersion != graphstream.FrozenSchemaVersion || bs[0].OwnerScopeDigest != ends[0].OwnerScopeDigest {
		t.Fatal("last-file manifest mismatch")
	}
	found := false
	for _, o := range bs[0].OwnerScope {
		if o.ID == "a.ts" {
			found = true
		}
	}
	if !found {
		t.Fatal("empty replacement omitted deleted owner")
	}
	for _, b := range batches {
		if b.Phase != graphstream.PhaseResolved {
			t.Fatalf("unexpected phase %s", b.Phase)
		}
		for _, n := range b.Nodes {
			if n.Owner.ID == "a.ts" {
				t.Fatal("deleted owner published nodes")
			}
		}
		for _, e := range b.Edges {
			if e.Owner.ID == "a.ts" {
				t.Fatal("deleted owner published edges")
			}
		}
	}
	if err := cons.ApplyRecords(s2.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "a.ts"}).String()
	if len(cons.Owners[key]) != 0 {
		t.Fatalf("last-file owner still has %d nodes", len(cons.Owners[key]))
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
	noop := &graphstream.MemorySink{}
	again, err := Run(context.Background(), eng, root, noop, opts)
	if err != nil || len(noop.CloneRecords()) != 0 || again.TargetGeneration != delta.TargetGeneration {
		t.Fatalf("empty completed last-file noop %+v events=%d", again, len(noop.CloneRecords()))
	}
}

func TestFrozenLastSourceDeletionEqualsCold(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"a.ts": "export const a=1",
		"b.ts": "export const b=2",
	})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, s1, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s1.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.ts")); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, s2, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.TargetGeneration == delta.BaseGeneration || len(s2.CloneRecords()) == 0 {
		t.Fatalf("deletion skipped replacement %+v events=%d", delta, len(s2.CloneRecords()))
	}
	bs, _, _, err := DecodeRun(s2.CloneRecords())
	if err != nil || len(bs) != 1 {
		t.Fatalf("deletion begin %v %v", bs, err)
	}
	owners := map[string]bool{}
	for _, o := range bs[0].OwnerScope {
		owners[o.ID] = true
	}
	if !owners["a.ts"] {
		t.Fatal("deletion lost previous owner")
	}
	if err := cons.ApplyRecords(s2.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "a.ts"}).String()
	if len(cons.Owners[key]) != 0 {
		t.Fatalf("deleted owner still has %d nodes", len(cons.Owners[key]))
	}
	if err := os.Remove(filepath.Join(root, "b.ts")); err != nil {
		t.Fatal(err)
	}
	s3 := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, s3, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s3.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
}

func TestFrozenLockOnlyDoesNotAdvanceGeneration(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	first, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	assertFrozenLockNoop := func(t *testing.T, action string) {
		t.Helper()
		sink := &graphstream.MemorySink{}
		res, err := Run(context.Background(), eng, root, sink, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.ParsedFiles != 0 || res.TargetGeneration != first.TargetGeneration || res.BaseGeneration != res.TargetGeneration || len(sink.CloneRecords()) != 0 {
			t.Fatalf("lock-only %s parsed=%d gen %d→%d events=%d", action, res.ParsedFiles, res.BaseGeneration, res.TargetGeneration, len(sink.CloneRecords()))
		}
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"packages":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	assertFrozenLockNoop(t, "add")
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"packages":{"node_modules/x":{"version":"1"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	assertFrozenLockNoop(t, "edit")
	if err := os.Rename(filepath.Join(root, "package-lock.json"), filepath.Join(root, "yarn.lock")); err != nil {
		t.Fatal(err)
	}
	assertFrozenLockNoop(t, "rename")
	if err := os.Remove(filepath.Join(root, "yarn.lock")); err != nil {
		t.Fatal(err)
	}
	assertFrozenLockNoop(t, "remove")
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"packages":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=2"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, edit, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.TargetGeneration != first.TargetGeneration+1 {
		t.Fatalf("source edit after lock-only gen %d→%d", first.TargetGeneration, res.TargetGeneration)
	}
	bs, _, _, err := DecodeRun(edit.CloneRecords())
	if err != nil || len(bs) != 1 {
		t.Fatalf("edit begin %v %v", bs, err)
	}
	for _, o := range bs[0].OwnerScope {
		if o.ID == "package-lock.json" || o.ID == "yarn.lock" {
			t.Fatalf("lockfile leaked into frozen scope: %v", bs[0].OwnerScope)
		}
	}
}

func TestFrozenForkPreservesProtocolAndRetry(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1", "b.ts": "export const b=2"})
	eng := testEngine(t, root)
	mainState := t.TempDir()
	mainSink := &graphstream.MemorySink{}
	main, err := Run(context.Background(), eng, root, mainSink, Options{StateDir: mainState, ContextID: "main", AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(mainState, "protocol"))
	if err != nil || string(got) != graphstream.FrozenSchemaVersion {
		t.Fatalf("source protocol %q %v", got, err)
	}
	applied := NewConsumer()
	if err := applied.ApplyRecords(mainSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=2"), 0o644); err != nil {
		t.Fatal(err)
	}
	branch := t.TempDir()
	opts := ForkOptions{SourceDir: mainState, TargetDir: branch, ContextID: "feature", Checkout: root, ExtractorVersion: engine.ExtractorVersion()}
	st, err := Fork(opts)
	if err != nil {
		t.Fatal(err)
	}
	if st.Protocol != graphstream.FrozenSchemaVersion {
		t.Fatalf("fork protocol %q", st.Protocol)
	}
	got, err = os.ReadFile(filepath.Join(branch, "protocol"))
	if err != nil || string(got) != graphstream.FrozenSchemaVersion {
		t.Fatalf("fork protocol file %q %v", got, err)
	}
	if _, err := Fork(opts); err != nil {
		t.Fatalf("completed frozen fork must be retry-safe: %v", err)
	}
	if err := os.Remove(filepath.Join(branch, "state.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Fork(opts); err != nil {
		t.Fatalf("interrupted frozen fork must complete: %v", err)
	}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: branch, ContextID: "feature"}); err == nil {
		t.Fatal("forked v2 state opened as v1")
	}
	branchSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, branchSink, Options{StateDir: branch, ContextID: "feature", AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("branch delta parsed %d", delta.ParsedFiles)
	}
	begins, _, _, err := DecodeRun(branchSink.CloneRecords())
	if err != nil || len(begins) != 1 {
		t.Fatalf("branch begin %v %v", begins, err)
	}
	if begins[0].SchemaVersion != graphstream.FrozenSchemaVersion || begins[0].ForkBaseRunID != main.RunID || begins[0].ForkBaseContextID != "main" {
		t.Fatalf("frozen fork begin %+v", begins[0])
	}
	branchCons := NewConsumer()
	if err := branchCons.SeedFrom(applied); err != nil {
		t.Fatal(err)
	}
	if err := branchCons.ApplyRecords(branchSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, branchCons, oracle)
}

func TestFrozenConsumerEmptyEpochAndDeletion(t *testing.T) {
	c := NewConsumer()
	emptyDigest := graphstream.DigestOwners(nil)
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, SchemaVersion: graphstream.FrozenSchemaVersion,
		RunID: "empty", TargetGeneration: 1, Phase: graphstream.PhaseEpoch,
		ScopeMode: graphstream.ScopeModeComplete, OwnerScopeCount: 0, OwnerScopeDigest: emptyDigest,
	}
	braw, _ := graphstream.Marshal(begin)
	end := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "empty", BatchCount: 0,
		BatchDigest: graphstream.DigestBatches(nil), OwnerScopeLen: 0, OwnerScopeDigest: emptyDigest,
		Completeness: graphstream.Completeness{Status: "success"},
	}
	eraw, _ := graphstream.Marshal(end)
	if err := c.ApplyRecords([]graphstream.Recorded{{MsgID: "b", Payload: braw}, {MsgID: "e", Payload: eraw}}); err != nil {
		t.Fatal(err)
	}
	if c.LastGeneration != 1 {
		t.Fatalf("empty epoch gen=%d", c.LastGeneration)
	}

	owner := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "gone.ts"}
	owners := []graphstream.OwnerRef{owner}
	digest := graphstream.DigestOwners(owners)
	filled := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, SchemaVersion: graphstream.FrozenSchemaVersion,
		RunID: "fill", BaseGeneration: 1, TargetGeneration: 2, Phase: graphstream.PhaseResolved,
		ScopeMode: graphstream.ScopeModeComplete, OwnerScope: owners, OwnerScopeCount: 1, OwnerScopeDigest: digest,
	}
	fraw, _ := graphstream.Marshal(filled)
	node := graphstream.Batch{
		Type: graphstream.TypeBatch, RunID: "fill", Seq: 1, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: owner, ID: "gone", Kind: facts.KindSymbol, Name: "gone"}},
	}
	nraw, _ := graphstream.Marshal(node)
	fillEnd := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "fill", BatchCount: 1,
		BatchDigest: graphstream.DigestBatches([][]byte{nraw}), OwnerScopeLen: 1, OwnerScopeDigest: digest,
		Completeness: graphstream.Completeness{Status: "success"},
	}
	feraw, _ := graphstream.Marshal(fillEnd)
	if err := c.ApplyRecords([]graphstream.Recorded{{MsgID: "fb", Payload: fraw}, {MsgID: "fn", Payload: nraw}, {MsgID: "fe", Payload: feraw}}); err != nil {
		t.Fatal(err)
	}
	cleared := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, SchemaVersion: graphstream.FrozenSchemaVersion,
		RunID: "del", BaseGeneration: 2, TargetGeneration: 3, Phase: graphstream.PhaseResolved,
		ScopeMode: graphstream.ScopeModeComplete, OwnerScope: owners, OwnerScopeCount: 1, OwnerScopeDigest: digest,
	}
	craw, _ := graphstream.Marshal(cleared)
	delEnd := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "del", BatchCount: 0,
		BatchDigest: graphstream.DigestBatches(nil), OwnerScopeLen: 1, OwnerScopeDigest: digest,
		Completeness: graphstream.Completeness{Status: "success"},
	}
	deraw, _ := graphstream.Marshal(delEnd)
	if err := c.ApplyRecords([]graphstream.Recorded{{MsgID: "db", Payload: craw}, {MsgID: "de", Payload: deraw}}); err != nil {
		t.Fatal(err)
	}
	if len(c.Owners[owner.String()]) != 0 {
		t.Fatalf("frozen deletion retained %d nodes", len(c.Owners[owner.String()]))
	}
}

func TestFrozenInterruptedPublishReopen(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, root, s1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.TargetGeneration != 1 {
		t.Fatalf("initial gen=%d", first.TargetGeneration)
	}
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=2"), 0o644); err != nil {
		t.Fatal(err)
	}
	fail := &graphstream.MemorySink{}
	fail.FailAt(1, errors.New("broker unavailable"))
	if _, err := Run(context.Background(), eng, root, fail, opts); err == nil {
		t.Fatal("interrupted delta succeeded")
	}
	st, err := loadCommittedState(state)
	if err != nil || st == nil || st.Generation != 1 {
		t.Fatalf("interrupted run promoted generation: %v %+v", err, st)
	}
	if _, err := os.Stat(pendingStatePath(state)); err == nil {
		if rec, recErr := recoverAcknowledgedPending(state, mustOpenJournal(t, state), opts, root); recErr != nil {
			t.Fatal(recErr)
		} else if rec != nil && rec.Generation != 1 {
			t.Fatalf("pending promoted without acked End: %+v", rec)
		}
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, s2, opts)
	if err != nil {
		t.Fatal(err)
	}
	if delta.BaseGeneration != 1 || delta.TargetGeneration != 2 {
		t.Fatalf("reopen gen %d→%d", delta.BaseGeneration, delta.TargetGeneration)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(s1.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s2.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
}

func TestFrozenOutOfScopeFactAfterBegin(t *testing.T) {
	c := NewConsumer()
	owner := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "a.ts"}
	digest := graphstream.DigestOwners([]graphstream.OwnerRef{owner})
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, SchemaVersion: graphstream.FrozenSchemaVersion,
		RunID: "r", TargetGeneration: 1, Phase: graphstream.PhaseEpoch,
		ScopeMode: graphstream.ScopeModeComplete, OwnerScope: []graphstream.OwnerRef{owner},
		OwnerScopeCount: 1, OwnerScopeDigest: digest,
	}
	braw, _ := graphstream.Marshal(begin)
	if err := c.Apply(graphstream.Recorded{MsgID: "b", Payload: braw}); err != nil {
		t.Fatal(err)
	}
	injected := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "injected.ts"}
	batch := graphstream.Batch{
		Type: graphstream.TypeBatch, RunID: "r", Seq: 1, Phase: graphstream.PhaseResolved,
		Nodes: []graphstream.Node{{Owner: injected, ID: "x", Kind: facts.KindSymbol, Name: "x"}},
	}
	raw, _ := graphstream.Marshal(batch)
	if err := c.Apply(graphstream.Recorded{MsgID: "x", Payload: raw}); err == nil {
		t.Fatal("accepted out-of-scope fact after frozen Begin")
	}
	p, err := planFileInvalidation([]string{"a.ts"}, nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.check(injected); err == nil {
		t.Fatal("plan accepted owner outside frozen scope")
	}
}

func TestFrozenWatchSequentialGenerations(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &watchEndSink{ends: make(chan struct{}, 8)}
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, testEngine(t, root), root, sink, Options{
			StateDir: filepath.Join(root, ".enola", "watch"), WatchEvery: time.Millisecond, AuthoritativeFiles: true,
		})
	}()
	wait := func() {
		t.Helper()
		select {
		case <-sink.ends:
		case err := <-done:
			t.Fatalf("watch stopped: %v", err)
		case <-time.After(8 * time.Second):
			t.Fatal("watch did not publish")
		}
	}
	wait()
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte("export const a=2"), 0o644); err != nil {
		t.Fatal(err)
	}
	wait()
	if err := os.WriteFile(filepath.Join(root, "src/a.ts"), []byte("export const a=3"), 0o644); err != nil {
		t.Fatal(err)
	}
	wait()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("watch exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop")
	}
	bs, batches, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) != 3 || len(ends) != 3 {
		t.Fatalf("watch envelopes begins=%d ends=%d %v", len(bs), len(ends), err)
	}
	for i, b := range bs {
		if b.SchemaVersion != graphstream.FrozenSchemaVersion || b.ScopeMode != graphstream.ScopeModeComplete {
			t.Fatalf("watch begin %d %+v", i, b)
		}
		if b.BaseGeneration != int64(i) || b.TargetGeneration != int64(i+1) {
			t.Fatalf("watch generation %d: %d→%d", i, b.BaseGeneration, b.TargetGeneration)
		}
		if b.OwnerScopeDigest != ends[i].OwnerScopeDigest || b.OwnerScopeCount != ends[i].OwnerScopeLen {
			t.Fatalf("watch manifest %d", i)
		}
	}
	for _, batch := range batches {
		if batch.Phase != graphstream.PhaseResolved {
			t.Fatalf("watch phase %s", batch.Phase)
		}
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if cons.LastGeneration != 3 {
		t.Fatalf("watch consumer gen=%d", cons.LastGeneration)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), testEngine(t, root), root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
}

func TestFrozenPolicyIdentityTriggersPlan(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	eng := testEngine(t, root)
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	first, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	st, err := loadCommittedState(state)
	if err != nil || st == nil {
		t.Fatalf("state: %v %+v", err, st)
	}
	st.PolicyIdentity = "stale-policy"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !delta.Invalidation.PolicyReconciled {
		t.Fatal("policy identity mismatch was not reconciled")
	}
	if delta.TargetGeneration != first.TargetGeneration+1 || len(sink.CloneRecords()) == 0 {
		t.Fatalf("PolicyIdentity change skipped frozen plan gen %d→%d events=%d", first.TargetGeneration, delta.TargetGeneration, len(sink.CloneRecords()))
	}
	bs, _, _, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) != 1 || bs[0].SchemaVersion != graphstream.FrozenSchemaVersion {
		t.Fatalf("policy begin %v %v", bs, err)
	}
}

func TestFrozenPolicyGitignoreClearsObsoleteOwner(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1", "gone.ts": "export const gone=1"})
	state := t.TempDir()
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	cons := NewConsumer()
	s1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), frozenPolicyEngine(t, root), root, s1, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(s1.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("gone.ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), frozenPolicyEngine(t, root), root, s2, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !delta.Invalidation.PolicyReconciled {
		t.Fatal("gitignore policy change was not reconciled")
	}
	if delta.TargetGeneration != first.TargetGeneration+1 || len(s2.CloneRecords()) == 0 {
		t.Fatalf("gitignore exclusion skipped frozen plan %+v events=%d", delta, len(s2.CloneRecords()))
	}
	bs, batches, _, err := DecodeRun(s2.CloneRecords())
	if err != nil || len(bs) != 1 {
		t.Fatalf("gitignore begin %v %v", bs, err)
	}
	found := false
	for _, o := range bs[0].OwnerScope {
		if o.ID == "gone.ts" {
			found = true
		}
	}
	if !found {
		t.Fatal("obsolete owner dropped from frozen plan")
	}
	for _, b := range batches {
		for _, n := range b.Nodes {
			if n.Owner.ID == "gone.ts" {
				t.Fatal("excluded owner published nodes")
			}
		}
	}
	if err := cons.ApplyRecords(s2.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "gone.ts"}).String()
	if len(cons.Owners[key]) != 0 {
		t.Fatalf("obsolete owner still has %d nodes", len(cons.Owners[key]))
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), frozenPolicyEngine(t, root), root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	oracle := NewConsumer()
	if err := oracle.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, oracle)
}

func frozenPolicyEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	var attach func(*engine.Engine) *engine.Engine
	attach = func(eng *engine.Engine) *engine.Engine {
		p, err := graphinput.Build(dir, graphinput.Options{})
		if err != nil {
			t.Fatal(err)
		}
		eng.ConfigureGraphInputs(&inputscope.Scope{Root: dir, Policy: p}, func() (*engine.Engine, error) {
			return attach(testEngine(t, dir)), nil
		})
		return eng
	}
	return attach(testEngine(t, dir))
}

func mustOpenJournal(t *testing.T, dir string) *graphstream.Journal {
	t.Helper()
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}
