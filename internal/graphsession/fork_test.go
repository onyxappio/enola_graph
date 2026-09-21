package graphsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestForkSeedsBranchDeltaWithoutTouchingSource(t *testing.T) {
	files := map[string]string{
		"src/a.ts": "export function a() { return 1; }\n",
		"src/b.ts": "import { a } from './a'; export function b() { return a(); }\n",
	}
	for i := 0; i < 20; i++ {
		files[filepath.Join("src", "n"+itoa(i)+".ts")] = "export const n" + itoa(i) + " = " + itoa(i) + ";\n"
	}
	dir := setupTSRepo(t, files)
	eng := testEngine(t, dir)
	mainState := filepath.Join(dir, ".enola", "main")
	mainSink := &graphstream.MemorySink{}
	main, err := Run(context.Background(), eng, dir, mainSink, Options{StateDir: mainState, ContextID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if main.ParsedFiles < 22 {
		t.Fatalf("initial parsed %d", main.ParsedFiles)
	}
	srcBytes := readFile(t, filepath.Join(mainState, "state.json"))
	applied := NewConsumer()
	if err := applied.ApplyRecords(mainSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "src/a.ts"), []byte("export function a() { return fetch('/z'); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	branchState := filepath.Join(dir, ".enola", "feature")
	st, err := Fork(ForkOptions{
		SourceDir:        mainState,
		TargetDir:        branchState,
		ContextID:        "feature",
		Checkout:         dir,
		ExtractorVersion: engine.ExtractorVersion(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.ForkBaseRunID != main.RunID || st.ForkBaseGeneration != main.TargetGeneration || st.ForkBaseContextID != "main" {
		t.Fatalf("fork provenance %+v vs main run %s gen %d", st, main.RunID, main.TargetGeneration)
	}
	afterFork := readFile(t, filepath.Join(mainState, "state.json"))
	if !bytes.Equal(srcBytes, afterFork) {
		t.Fatal("fork modified source checkpoint")
	}

	branchSink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, branchSink, Options{StateDir: branchState, ContextID: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("branch delta parsed %d, want 1", delta.ParsedFiles)
	}
	afterDelta := readFile(t, filepath.Join(mainState, "state.json"))
	if !bytes.Equal(srcBytes, afterDelta) {
		t.Fatal("branch delta modified source checkpoint")
	}
	begins, _, _, err := DecodeRun(branchSink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(begins) != 1 || begins[0].ForkBaseRunID != main.RunID || begins[0].ForkBaseContextID != "main" {
		t.Fatalf("first branch begin missing fork base: %+v", begins)
	}

	branchCons := NewConsumer()
	if err := branchCons.SeedFrom(applied); err != nil {
		t.Fatal(err)
	}
	if branchCons.RepoID != applied.RepoID || branchCons.LastGeneration != applied.LastGeneration || branchCons.LastRunID != applied.LastRunID {
		t.Fatalf("seeded identity %+v vs source %+v", branchCons, applied)
	}
	if err := branchCons.ApplyRecords(branchSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: filepath.Join(dir, ".enola", "cold"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold := NewConsumer()
	if err := cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, branchCons, cold)

	if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("import { a } from './a'; export function b() { return a() + 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondSink := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, secondSink, Options{StateDir: branchState, ContextID: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 1 {
		t.Fatalf("second branch delta parsed %d, want 1", second.ParsedFiles)
	}
	afterSecond := readFile(t, filepath.Join(mainState, "state.json"))
	if !bytes.Equal(srcBytes, afterSecond) {
		t.Fatal("second branch delta modified source checkpoint")
	}
	secondBegins, _, _, err := DecodeRun(secondSink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(secondBegins) != 1 {
		t.Fatalf("second delta begins=%d", len(secondBegins))
	}
	if secondBegins[0].ForkBaseRunID != "" || secondBegins[0].ForkBaseContextID != "" {
		t.Fatalf("second branch begin must not restate fork base: %+v", secondBegins[0])
	}
	if secondBegins[0].ContextID != "feature" || secondBegins[0].BaseGeneration != delta.TargetGeneration {
		t.Fatalf("second branch identity %+v vs first delta gen %d", secondBegins[0], delta.TargetGeneration)
	}
	if err := branchCons.ApplyRecords(secondSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if applied.ContextID != "main" || applied.LastRunID != main.RunID {
		t.Fatal("branch apply mutated seeded source consumer")
	}
	cold2Sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, cold2Sink, Options{StateDir: filepath.Join(dir, ".enola", "cold2"), ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	cold2 := NewConsumer()
	if err := cold2.ApplyRecords(cold2Sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, branchCons, cold2)

	// Source continues independently.
	if err := os.WriteFile(filepath.Join(dir, "src/b.ts"), []byte("import { a } from './a'; export function b() { return 9; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: mainState, ContextID: "main"}); err != nil {
		t.Fatal(err)
	}
}

func TestForkRejectsOccupiedAndStaleJournal(t *testing.T) {
	for _, mode := range []string{"unrelated_file", "identity_and_unacked", "identity_and_pending", "payloads_only"} {
		t.Run(mode, func(t *testing.T) {
			d := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
			src := t.TempDir()
			if _, e := Run(context.Background(), testEngine(t, d), d, &graphstream.MemorySink{}, Options{StateDir: src, ContextID: "main"}); e != nil {
				t.Fatal(e)
			}
			dst := t.TempDir()
			switch mode {
			case "unrelated_file":
				if err := os.WriteFile(filepath.Join(dst, "precious.txt"), []byte("occupied"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "identity_and_unacked":
				if err := writeIdentityFile(dst, boundIdentity{RepoID: d, ContextID: "branch", Checkout: d}); err != nil {
					t.Fatal(err)
				}
				j, err := graphstream.OpenJournal(dst)
				if err != nil {
					t.Fatal(err)
				}
				if err := j.Append(graphstream.JournalEntry{MsgID: "stale", Subject: "other.repo", Payload: []byte(`{"type":"unexpected"}`)}); err != nil {
					t.Fatal(err)
				}
			case "identity_and_pending":
				if err := writeIdentityFile(dst, boundIdentity{RepoID: d, ContextID: "branch", Checkout: d}); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pendingStatePath(dst), []byte(`{}`), 0o644); err != nil {
					t.Fatal(err)
				}
			case "payloads_only":
				if err := os.WriteFile(filepath.Join(dst, "payloads.jsonl"), []byte("{}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			st, err := Fork(ForkOptions{SourceDir: src, TargetDir: dst, ContextID: "branch", Checkout: d})
			if err == nil {
				if mode == "identity_and_unacked" {
					sink := &graphstream.MemorySink{}
					if _, runErr := Run(context.Background(), testEngine(t, d), d, sink, Options{StateDir: dst, ContextID: "branch"}); runErr != nil {
						t.Log(runErr)
					}
					for _, r := range sink.CloneRecords() {
						if r.MsgID == "stale" {
							t.Logf("stale payload replayed to subject=%s", r.Subject)
						}
					}
				}
				t.Fatalf("fork accepted occupied target: %+v", st)
			}
		})
	}
}

func TestForkRejectsMatchingRetryWhenDirty(t *testing.T) {
	for _, mode := range []string{"completed_unrelated", "completed_unacked", "pending_unrelated", "pending_unacked"} {
		t.Run(mode, func(t *testing.T) {
			d := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
			srcDir := t.TempDir()
			if _, err := Run(context.Background(), testEngine(t, d), d, &graphstream.MemorySink{}, Options{StateDir: srcDir, ContextID: "main"}); err != nil {
				t.Fatal(err)
			}
			dst := t.TempDir()
			opts := ForkOptions{SourceDir: srcDir, TargetDir: dst, ContextID: "branch", Checkout: d}
			if strings.HasPrefix(mode, "completed") {
				if _, err := Fork(opts); err != nil {
					t.Fatal(err)
				}
			} else {
				src, err := loadCommittedState(srcDir)
				if err != nil || src == nil {
					t.Fatalf("source state: %v %+v", err, src)
				}
				pending, err := cloneState(src)
				if err != nil {
					t.Fatal(err)
				}
				pending.ContextID = "branch"
				pending.ForkBaseRepoID = src.RepoID
				pending.ForkBaseContextID = src.ContextID
				pending.ForkBaseGeneration = src.Generation
				pending.ForkBaseRunID = src.LastRunID
				pending.LastComplete = true
				if err := writeIdentityFile(dst, boundIdentity{RepoID: pending.RepoID, ContextID: "branch", Checkout: pending.Checkout}); err != nil {
					t.Fatal(err)
				}
				if err := writePendingState(dst, pending); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasSuffix(mode, "unrelated") {
				if err := os.WriteFile(filepath.Join(dst, "precious.txt"), []byte("occupied"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				j, err := graphstream.OpenJournal(dst)
				if err != nil {
					t.Fatal(err)
				}
				if err := j.Append(graphstream.JournalEntry{MsgID: "stale", Subject: "other.repo", Payload: []byte(`{"type":"unexpected"}`)}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Fork(opts); err == nil {
				t.Fatal("matching completed/pending fork must not bypass dirty occupancy")
			}
		})
	}
}

func TestForkMatchingIdentityAloneDoesNotBypassDirtyFiles(t *testing.T) {
	d := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	src := t.TempDir()
	if _, err := Run(context.Background(), testEngine(t, d), d, &graphstream.MemorySink{}, Options{StateDir: src, ContextID: "main"}); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := writeIdentityFile(dst, boundIdentity{RepoID: d, ContextID: "branch", Checkout: d}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "precious.txt"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Fork(ForkOptions{SourceDir: src, TargetDir: dst, ContextID: "branch", Checkout: d}); err == nil {
		t.Fatal("matching identity must not bypass arbitrary occupancy")
	}
}

func TestForkPromotesMatchingPendingWithoutJournal(t *testing.T) {
	d := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	srcDir := t.TempDir()
	main, err := Run(context.Background(), testEngine(t, d), d, &graphstream.MemorySink{}, Options{StateDir: srcDir, ContextID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	src, err := loadCommittedState(srcDir)
	if err != nil || src == nil {
		t.Fatalf("source state: %v %+v", err, src)
	}
	dst := t.TempDir()
	pending, err := cloneState(src)
	if err != nil {
		t.Fatal(err)
	}
	pending.ContextID = "branch"
	pending.ForkBaseRepoID = src.RepoID
	pending.ForkBaseContextID = src.ContextID
	pending.ForkBaseGeneration = src.Generation
	pending.ForkBaseRunID = src.LastRunID
	pending.LastComplete = true
	if err := writeIdentityFile(dst, boundIdentity{RepoID: pending.RepoID, ContextID: "branch", Checkout: pending.Checkout}); err != nil {
		t.Fatal(err)
	}
	if err := writePendingState(dst, pending); err != nil {
		t.Fatal(err)
	}
	st, err := Fork(ForkOptions{SourceDir: srcDir, TargetDir: dst, ContextID: "branch", Checkout: d})
	if err != nil {
		t.Fatal(err)
	}
	if st.ForkBaseRunID != main.RunID || st.ContextID != "branch" {
		t.Fatalf("promoted pending %+v", st)
	}
	if _, err := os.Stat(statePath(dst)); err != nil {
		t.Fatal(err)
	}
}

func TestSeedFromDeepClonesNestedPropsAndOtherState(t *testing.T) {
	src := NewConsumer()
	src.RepoID = "repo"
	src.ContextID = "main"
	src.LastGeneration = 1
	src.LastRunID = "r"
	src.Owners["a"] = []graphstream.Node{{
		ID: "a", Kind: "symbol", Name: "a",
		Props: map[string]any{
			"direct": "old",
			"nested": map[string]any{"v": "old", "inner": map[string]any{"k": "old"}},
			"list":   []any{"old", map[string]any{"i": "old"}},
		},
	}}
	src.Edges["a"] = []graphstream.Edge{{FromID: "a", Kind: "calls", TargetName: "b", TargetID: "b", Occurrence: 0}}
	before := src.Canonical()

	dst := NewConsumer()
	if err := dst.SeedFrom(src); err != nil {
		t.Fatal(err)
	}
	if dst.Canonical() != before {
		t.Fatalf("seeded Canonical diverged\n got=%s\nwant=%s", dst.Canonical(), before)
	}
	if dst.RepoID != "repo" || dst.ContextID != "main" || dst.LastGeneration != 1 || dst.LastRunID != "r" {
		t.Fatalf("seeded identity %+v", dst)
	}

	dst.Owners["a"][0].Props["direct"] = "new"
	dst.Owners["a"][0].Props["nested"].(map[string]any)["v"] = "new"
	dst.Owners["a"][0].Props["nested"].(map[string]any)["inner"].(map[string]any)["k"] = "new"
	dst.Owners["a"][0].Props["list"].([]any)[0] = "new"
	dst.Owners["a"][0].Props["list"].([]any)[1].(map[string]any)["i"] = "new"
	dst.Edges["a"][0].TargetID = "mutated"
	dst.Owners["extra"] = []graphstream.Node{{ID: "x"}}
	delete(dst.Owners, "a")
	dst.ContextID = "branch"
	dst.LastGeneration = 99
	dst.LastRunID = "other"

	got := src.Owners["a"][0].Props
	if got["direct"] != "old" {
		t.Fatalf("direct prop aliased: %+v", got)
	}
	if got["nested"].(map[string]any)["v"] != "old" || got["nested"].(map[string]any)["inner"].(map[string]any)["k"] != "old" {
		t.Fatalf("nested prop aliased: %+v", got)
	}
	if got["list"].([]any)[0] != "old" || got["list"].([]any)[1].(map[string]any)["i"] != "old" {
		t.Fatalf("list prop aliased: %+v", got)
	}
	if src.Edges["a"][0].TargetID != "b" {
		t.Fatal("edge slice aliased")
	}
	if _, ok := src.Owners["a"]; !ok {
		t.Fatal("deleting dest owner mutated source")
	}
	if _, ok := src.Owners["extra"]; ok {
		t.Fatal("adding dest owner mutated source")
	}
	if src.ContextID != "main" || src.LastGeneration != 1 || src.LastRunID != "r" {
		t.Fatalf("source identity mutated: %+v", src)
	}
	if src.Canonical() != before {
		t.Fatalf("source Canonical mutated after dest edits\n got=%s\nwant=%s", src.Canonical(), before)
	}
}

func TestSeedFromRaceIsolation(t *testing.T) {
	src := NewConsumer()
	src.RepoID = "repo"
	src.ContextID = "main"
	src.LastGeneration = 1
	src.LastRunID = "r"
	src.Owners["a"] = []graphstream.Node{{
		ID: "a",
		Props: map[string]any{
			"direct": "old",
			"nested": map[string]any{"v": "old"},
			"list":   []any{"old"},
		},
	}}
	src.Edges["a"] = []graphstream.Edge{{FromID: "a", Kind: "calls", TargetName: "b"}}
	before := src.Canonical()

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dst := NewConsumer()
			if err := dst.SeedFrom(src); err != nil {
				errCh <- err
				return
			}
			if dst.Canonical() != before {
				errCh <- errors.New("seeded Canonical diverged from source")
				return
			}
			dst.Owners["a"][0].Props["direct"] = "new"
			dst.Owners["a"][0].Props["nested"].(map[string]any)["v"] = "new"
			dst.Owners["a"][0].Props["list"].([]any)[0] = "new"
			dst.Edges["a"][0].TargetName = "mutated"
			dst.ContextID = "branch"
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if src.Canonical() != before {
		t.Fatalf("source mutated under concurrent SeedFrom: %s", src.Canonical())
	}
	if src.Owners["a"][0].Props["direct"] != "old" || src.Owners["a"][0].Props["nested"].(map[string]any)["v"] != "old" || src.Owners["a"][0].Props["list"].([]any)[0] != "old" {
		t.Fatalf("source properties aliased under race: %+v", src.Owners["a"][0].Props)
	}
}

func TestIncrementalEmptyScopeRequiresOwnerScopeLenPresence(t *testing.T) {
	c := NewConsumer()
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "empty", TargetGeneration: 1,
		Phase: graphstream.PhaseEpoch, ScopeMode: graphstream.ScopeModeIncremental,
	}
	braw, err := graphstream.Marshal(begin)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(graphstream.Recorded{MsgID: "b", Payload: braw}); err != nil {
		t.Fatal(err)
	}
	end := graphstream.EndReplace{
		Type: graphstream.TypeEndReplace, RunID: "empty",
		OwnerScopeDigest: graphstream.DigestOwners(nil),
		Completeness:     graphstream.Completeness{Status: "success"},
	}
	eraw, err := graphstream.Marshal(end)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(eraw, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "owner_scope_len")
	missing, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(graphstream.Recorded{MsgID: "end-missing", Payload: missing}); err == nil {
		t.Fatalf("missing mandatory owner_scope_len accepted for empty scope: generation=%d payload=%s", c.LastGeneration, missing)
	}
	if c.LastGeneration != 0 || len(c.open) == 0 {
		t.Fatalf("omitted count must not commit: gen=%d open=%d", c.LastGeneration, len(c.open))
	}
	if err := c.Apply(graphstream.Recorded{MsgID: "end-zero", Payload: eraw}); err != nil {
		t.Fatal(err)
	}
	if c.LastGeneration != 1 {
		t.Fatalf("explicit owner_scope_len=0 must commit empty incremental, gen=%d", c.LastGeneration)
	}
}

func TestForkRejectsSamePathNonemptyAndWrongBase(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;"})
	eng := testEngine(t, dir)
	mainState := t.TempDir()
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: mainState, ContextID: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Fork(ForkOptions{SourceDir: mainState, TargetDir: mainState, ContextID: "feature", Checkout: dir}); err == nil {
		t.Fatal("same path must be rejected")
	}
	occupied := t.TempDir()
	if err := os.WriteFile(filepath.Join(occupied, "state.json"), []byte(`{"schema":"nope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Fork(ForkOptions{SourceDir: mainState, TargetDir: occupied, ContextID: "feature", Checkout: dir}); err == nil {
		t.Fatal("nonempty target must be rejected")
	}

	c := NewConsumer()
	begin := graphstream.BeginReplace{
		Type: graphstream.TypeBeginReplace, RunID: "br", ContextID: "feature",
		BaseGeneration: 1, TargetGeneration: 2, Phase: graphstream.PhaseResolved,
		ForkBaseRepoID: "repo", ForkBaseContextID: "main", ForkBaseGeneration: 1, ForkBaseRunID: "run-x",
	}
	raw, _ := graphstream.Marshal(begin)
	if err := c.Apply(graphstream.Recorded{MsgID: "b", Payload: raw}); err == nil {
		t.Fatal("missing seed must reject fork begin")
	}

	mainCons := NewConsumer()
	if err := mainCons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	seeded := NewConsumer()
	if err := seeded.SeedFrom(mainCons); err != nil {
		t.Fatal(err)
	}
	wrong := begin
	wrong.RepoID = mainCons.RepoID
	wrong.ForkBaseRepoID = mainCons.RepoID
	wrong.ForkBaseGeneration = mainCons.LastGeneration
	wrong.ForkBaseRunID = "other-run"
	wraw, _ := graphstream.Marshal(wrong)
	if err := seeded.Apply(graphstream.Recorded{MsgID: "w", Payload: wraw}); err == nil {
		t.Fatal("wrong fork base run must be rejected")
	}
	wrongCtx := begin
	wrongCtx.RepoID = mainCons.RepoID
	wrongCtx.ForkBaseRepoID = mainCons.RepoID
	wrongCtx.ForkBaseGeneration = mainCons.LastGeneration
	wrongCtx.ForkBaseContextID = "other"
	wrongCtx.ForkBaseRunID = mainCons.LastRunID
	craw, _ := graphstream.Marshal(wrongCtx)
	seeded2 := NewConsumer()
	if err := seeded2.SeedFrom(mainCons); err != nil {
		t.Fatal(err)
	}
	if err := seeded2.Apply(graphstream.Recorded{MsgID: "c", Payload: craw}); err == nil {
		t.Fatal("same generation wrong context must be rejected")
	}
}

func TestForkIdempotentRetryAndWorktreeRejected(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;"})
	eng := testEngine(t, dir)
	mainState := t.TempDir()
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: mainState, ContextID: "main"}); err != nil {
		t.Fatal(err)
	}
	branch := t.TempDir()
	opts := ForkOptions{SourceDir: mainState, TargetDir: branch, ContextID: "feature", Checkout: dir, ExtractorVersion: engine.ExtractorVersion()}
	if _, err := Fork(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Fork(opts); err != nil {
		t.Fatal("completed fork must be retry-safe")
	}
	if err := os.Remove(filepath.Join(branch, "state.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Fork(opts); err != nil {
		t.Fatal("interrupted fork (identity without state) must complete")
	}
	other := t.TempDir()
	if _, err := Fork(ForkOptions{SourceDir: mainState, TargetDir: t.TempDir(), ContextID: "feature", Checkout: other, ExtractorVersion: engine.ExtractorVersion()}); err == nil {
		t.Fatal("independent worktree must be rejected")
	}
}

func TestCLIForkThenDeltaParsesOnlyChangedFile(t *testing.T) {
	bin := buildEnola(t)
	files := map[string]string{
		"src/a.ts": "export function a() { return 1; }\n",
		"src/b.ts": "import { a } from './a'; export function b() { return a(); }\n",
		"src/c.ts": "export function c() { return 1; }\n",
	}
	for i := 0; i < 8; i++ {
		files["src/n"+itoa(i)+".ts"] = "export const n" + itoa(i) + " = " + itoa(i) + ";\n"
	}
	dir := setupTSRepo(t, files)
	mainState := t.TempDir()
	eventsMain := filepath.Join(t.TempDir(), "main.jsonl")
	if _, err := runGraphCLIContext(t, bin, "analyze", dir, mainState, eventsMain, "main"); err != nil {
		t.Fatal(err)
	}
	srcHash := sha256File(t, filepath.Join(mainState, "state.json"))
	if err := os.WriteFile(filepath.Join(dir, "src/c.ts"), []byte("export function c() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	branchState := t.TempDir()
	cmd := exec.Command(bin, "graph", "fork", "--base-state-dir", mainState, "--state-dir", branchState, "--context", "feature", dir)
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fork: %v\n%s", err, out)
	}
	eventsBr := filepath.Join(t.TempDir(), "br.jsonl")
	delta, err := runGraphCLIContext(t, bin, "delta", dir, branchState, eventsBr, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("CLI branch delta parsed %d, want 1; fallbacks=%v", delta.ParsedFiles, delta.Fallbacks)
	}
	if sha256File(t, filepath.Join(mainState, "state.json")) != srcHash {
		t.Fatal("CLI fork/delta mutated source checkpoint")
	}
}

func runGraphCLIContext(t *testing.T, bin, mode, repo, state, events, contextID string) (*Result, error) {
	t.Helper()
	cmd := exec.Command(bin, "graph", mode, "--context", contextID, "--state-dir", state, "--events", events, "--json", repo)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	var results []Result
	payload := out
	if err := json.Unmarshal(payload, &results); err != nil {
		i := bytes.LastIndex(out, []byte("\n["))
		if i >= 0 {
			payload = out[i+1:]
		} else if i = bytes.Index(out, []byte("\n[")); i >= 0 {
			payload = out[i+1:]
		}
		if err := json.Unmarshal(payload, &results); err != nil || len(results) == 0 {
			return nil, err
		}
	}
	if len(results) == 0 {
		return nil, err
	}
	return &results[0], nil
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [12]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}
