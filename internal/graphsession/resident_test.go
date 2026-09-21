package graphsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/swiftextractor"
	"github.com/enola-labs/enola/internal/graphstream"
)

func residentFixture(t *testing.T, files map[string]string, opts Options) (string, *Resident, *ChangeQueue, *graphstream.MemorySink) {
	t.Helper()
	root := setupTSRepo(t, files)
	sink := &graphstream.MemorySink{}
	opts.StateDir = filepath.Join(root, ".enola", "resident")
	r, err := OpenSession(context.Background(), testEngine(t, root), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	q := NewChangeQueue("test", 8)
	q.Start(context.Background())
	if _, err = r.ApplyChanges(context.Background(), q.Drain()); err != nil {
		t.Fatal(err)
	}
	return root, r, q, sink
}
func residentApply(t *testing.T, r *Resident, q *ChangeQueue) *OnlineResult {
	t.Helper()
	res, err := r.ApplyChanges(context.Background(), q.Drain())
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func residentCold(t *testing.T, root string, r *Resident, sink *graphstream.MemorySink) {
	t.Helper()
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), r.eng, root, coldSink, Options{StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	applied, cold := NewConsumer(), NewConsumer()
	applyRun(t, applied, sink)
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, applied, cold)
}

func TestResidentCoveredIdleDoesNoWork(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{"src/a.ts": "export function a(){return fetch('/a')}"}, Options{})
	before, _ := json.Marshal(r.state)
	events := len(sink.CloneRecords())
	stateInfo, err := os.Stat(filepath.Join(root, ".enola", "resident", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		res := residentApply(t, r, q)
		if res.Work != (WorkCounters{}) || res.ParsedFiles != 0 || res.OwnersPublished != 0 || res.Facts != nil || res.TargetGeneration != 1 || res.Reconciled {
			t.Fatalf("idle: %+v", res)
		}
	}
	after, _ := json.Marshal(r.state)
	info, _ := os.Stat(filepath.Join(root, ".enola", "resident", "state.json"))
	if string(before) != string(after) || len(sink.CloneRecords()) != events || !stateInfo.ModTime().Equal(info.ModTime()) {
		t.Fatal("idle mutated state or published")
	}
	snap := r.Snapshot()
	for i := range snap {
		if snap[i].Props != nil {
			snap[i].Props["corrupt"] = true
		}
	}
	after, _ = json.Marshal(r.state)
	if string(before) != string(after) {
		t.Fatal("snapshot escaped committed ownership")
	}
}

func TestResidentContentEditsAndFallbacksEqualCold(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{"src/a.ts": "export function a(){return 1}", "src/b.ts": "import {a} from './a'; export function b(){return a()}", "src/c.ts": "export const c=1"}, Options{})
	for _, body := range []string{"export function a(){return 2}", "export function a(){return fetch('/a')}", "export function renamed(){return 3}", "export function a(){return 1}"} {
		p := filepath.Join(root, "src/a.ts")
		st, _ := os.Stat(p)
		independentWrite(t, root, "src/a.ts", body)
		os.Chtimes(p, st.ModTime(), st.ModTime())
		q.Add("src/a.ts")
		res := residentApply(t, r, q)
		if res.Reconciled || res.Work.InventoryScans != 0 || res.Work.DetectionScans != 0 || res.Work.ContextScans != 0 || res.Work.ConfigScans != 0 || res.Work.HashedFiles != 1 {
			t.Fatalf("not narrow: %+v", res)
		}
		residentCold(t, root, r, sink)
	}
	q.Add("src/a.ts")
	res := residentApply(t, r, q)
	if res.ParsedFiles != 0 || res.Work.HashedFiles != 1 || res.Work.Checkpoints != 0 {
		t.Fatalf("same bytes: %+v", res)
	}
	independentWrite(t, root, "src/new.ts", "export const newcomer=1")
	q.Add("src/new.ts")
	if res = residentApply(t, r, q); !res.Reconciled || res.FallbackReason == "" {
		t.Fatalf("new input not reconciled: %+v", res)
	}
	residentCold(t, root, r, sink)
	if err := os.Remove(filepath.Join(root, "src/c.ts")); err != nil {
		t.Fatal(err)
	}
	q.Add("src/c.ts")
	if !residentApply(t, r, q).Reconciled {
		t.Fatal("deletion must reconcile")
	}
	residentCold(t, root, r, sink)
	independentWrite(t, root, "tsconfig.json", `{"compilerOptions":{"baseUrl":"."}}`)
	q.Add("tsconfig.json")
	if !residentApply(t, r, q).Reconciled {
		t.Fatal("config must reconcile")
	}
	residentCold(t, root, r, sink)
}

func TestResidentOverflowRestartAndContinuity(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{"src/a.ts": "export const a=1"}, Options{})
	independentWrite(t, root, "src/a.ts", "export const a=2")
	for i := 0; i < 12; i++ {
		q.Add(strings.Repeat("x", i+1))
	}
	res := residentApply(t, r, q)
	if !res.Reconciled || !strings.Contains(res.FallbackReason, "overflow") {
		t.Fatalf("overflow: %+v", res)
	}
	residentCold(t, root, r, sink)
	if residentApply(t, r, q).Reconciled {
		t.Fatal("deterministic queue coverage should survive path overflow")
	}
	res, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: "other", Covered: true})
	if err != nil || !res.Reconciled {
		t.Fatalf("epoch gap: %+v %v", res, err)
	}
	r.Close()
	independentWrite(t, root, "src/a.ts", "export const a=3")
	fresh, err := OpenSession(context.Background(), r.eng, root, &graphstream.MemorySink{}, r.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	res, err = fresh.ApplyChanges(context.Background(), ChangeBatch{Epoch: "other", Covered: true})
	if err != nil || !res.Reconciled || res.ParsedFiles != 1 {
		t.Fatalf("restart gap: %+v %v", res, err)
	}
}

type residentEndSink struct {
	graphstream.MemorySink
	mu    sync.Mutex
	onEnd func()
}

func (s *residentEndSink) Publish(ctx context.Context, subject, id string, b []byte) error {
	var p struct{ Type string }
	json.Unmarshal(b, &p)
	if p.Type == graphstream.TypeEndReplace {
		s.mu.Lock()
		f := s.onEnd
		s.onEnd = nil
		s.mu.Unlock()
		if f != nil {
			f()
		}
	}
	return s.MemorySink.Publish(ctx, subject, id, b)
}
func TestResidentLateEventsDuringAcknowledgmentSurvive(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1", "b.ts": "export const b=1"})
	q := NewChangeQueue("late", 8)
	q.Start(context.Background())
	sink := &residentEndSink{}
	r, err := OpenSession(context.Background(), testEngine(t, root), root, sink, Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Events during baseline and after captured-byte validation must remain queued.
	sink.onEnd = func() { os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=2"), 0644); q.Add("a.ts") }
	residentApply(t, r, q)
	sink.mu.Lock()
	sink.onEnd = func() {
		os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=3"), 0644)
		q.Add("a.ts")
		os.WriteFile(filepath.Join(root, "b.ts"), []byte("export const b=3"), 0644)
		q.Add("b.ts")
	}
	sink.mu.Unlock()
	first := residentApply(t, r, q)
	second := residentApply(t, r, q)
	if first.ParsedFiles != 1 || second.ParsedFiles != 2 || second.TargetGeneration != 3 {
		t.Fatalf("lost late events: %+v %+v", first, second)
	}
	residentCold(t, root, r, &sink.MemorySink)
}

func TestResidentFailurePreservesCommittedCachesAndRecovers(t *testing.T) {
	var mutate func()
	root, r, q, sink := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{OnBeforeParse: func(string) {
		if mutate != nil {
			mutate()
		}
	}})
	before, _ := json.Marshal(r.state)
	independentWrite(t, root, "a.ts", "export const a=2")
	q.Add("a.ts")
	mutate = func() { os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=3"), 0644); q.Add("a.ts") }
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err == nil {
		t.Fatal("mid-parse captured-byte mutation succeeded")
	}
	after, _ := json.Marshal(r.state)
	if string(before) != string(after) {
		t.Fatal("failed transaction mutated committed cache")
	}
	mutate = nil
	res := residentApply(t, r, q)
	if !res.Reconciled {
		t.Fatal("failure must replay/reconcile")
	}
	residentCold(t, root, r, sink)
	// A failed broker delivery must likewise be replayed before any idle shortcut.
	independentWrite(t, root, "a.ts", "export const a=4")
	q.Add("a.ts")
	sink.FailAt(len(sink.CloneRecords())+1, errors.New("broker unavailable"))
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err == nil {
		t.Fatal("expected broker failure")
	}
	sink.FailAt(0, nil)
	if !residentApply(t, r, q).Reconciled {
		t.Fatal("broker failure was skipped")
	}
	residentCold(t, root, r, sink)
}

func TestResidentConfigReloadFailsClosed(t *testing.T) {
	root, r, q, _ := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
	independentWrite(t, root, "mcp-arch.yaml", "ignore: [a.ts]\n")
	q.Add("mcp-arch.yaml")
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err == nil || !strings.Contains(err.Error(), "ReloadEngine") {
		t.Fatalf("expected config reload requirement: %v", err)
	}
	// The source batch was drained; a failed configuration transaction must remain dirty.
	r.opts.ReloadEngine = func(context.Context) (*engine.Engine, error) {
		eng := testEngine(t, root)
		eng.Config().Ignore = append(eng.Config().Ignore, "a.ts")
		return eng, nil
	}
	q.Lost("retry config reload")
	if !residentApply(t, r, q).Reconciled {
		t.Fatal("reload skipped")
	}
}

func TestFileChangeSourceRealWritesAtomicSaveAndNewDirectory(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.ts")
	os.WriteFile(p, []byte("old"), 0644)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewFileChangeSource(root, nil, 32)
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Drain()
	wait := func() {
		t.Helper()
		select {
		case <-s.Ready():
		case <-time.After(3 * time.Second):
			t.Fatal("real filesystem write was not observed")
		}
		b := s.Drain()
		if !b.Covered {
			t.Fatalf("lost coverage: %+v", b)
		}
	}
	st, _ := os.Stat(p)
	os.WriteFile(p, []byte("new"), 0644)
	os.Chtimes(p, st.ModTime(), st.ModTime())
	wait()
	tmp := filepath.Join(root, "tmp")
	os.WriteFile(tmp, []byte("end"), 0644)
	os.Rename(tmp, p)
	wait()
	dir := filepath.Join(root, "nested")
	os.Mkdir(dir, 0755)
	os.WriteFile(filepath.Join(dir, "b.ts"), []byte("a"), 0644)
	wait()
	// Synchronize with recursive registration before draining creation events.
	s.registration.Lock()
	s.registration.Unlock()
	time.Sleep(30 * time.Millisecond)
	s.Drain()
	os.WriteFile(filepath.Join(dir, "b.ts"), []byte("b"), 0644)
	wait()
	s.markUncertain("simulated kernel loss")
	if s.Drain().Covered || s.Drain().Covered {
		t.Fatal("coverage loss was cleared by drain")
	}
}

func TestChangeQueueBoundsAndLateCapture(t *testing.T) {
	q := NewChangeQueue("fake", 2)
	q.Start(context.Background())
	q.Add("a")
	first := q.Drain()
	q.Add("a")
	q.Add("b")
	second := q.Drain()
	if first.Through != second.From || !reflect.DeepEqual(second.Paths, []string{"a", "b"}) {
		t.Fatalf("late capture %+v %+v", first, second)
	}
	q.Add("a")
	q.Add("b")
	q.Add("c")
	q.Add("d")
	b := q.Drain()
	if b.Reconcile == "" || len(b.Paths) != 0 || b.Through != 7 {
		t.Fatalf("overflow %+v", b)
	}
}

func TestResidentSwiftContextIncludesAreNeverSkipped(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1", "Sources/A.swift": "public struct A {}", "project.yml": "include:\n  - inputs.data\n", "inputs.data": "targets:\n  A:\n    type: framework\n    sources: [Sources]\n"})
	eng := testEngine(t, root)
	eng.RegisterExtractor(swiftextractor.New())
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(context.Background(), eng, root, sink, Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	q := NewChangeQueue("swift", 10)
	q.Start(context.Background())
	residentApply(t, r, q)
	independentWrite(t, root, "a.ts", "export const a=2")
	q.Add("a.ts")
	res := residentApply(t, r, q)
	if res.Reconciled || res.Work.BoundedContextChecks != 1 {
		t.Fatalf("unrelated TS edit did not retain Swift: %+v", res)
	}
	residentCold(t, root, r, sink)
	independentWrite(t, root, "inputs.data", "targets:\n  B:\n    type: framework\n    sources: [Sources]\n")
	q.Add("inputs.data")
	if !residentApply(t, r, q).Reconciled {
		t.Fatal("Swift include event was skipped")
	}
	residentCold(t, root, r, sink)
}

type watchEndSink struct {
	graphstream.MemorySink
	ends chan struct{}
}

func (s *watchEndSink) Publish(ctx context.Context, subject, id string, b []byte) error {
	if err := s.MemorySink.Publish(ctx, subject, id, b); err != nil {
		return err
	}
	var p struct{ Type string }
	json.Unmarshal(b, &p)
	if p.Type == graphstream.TypeEndReplace {
		select {
		case s.ends <- struct{}{}:
		default:
		}
	}
	return nil
}
func TestWatchUsesResidentSessionForRealFileEdits(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &watchEndSink{ends: make(chan struct{}, 8)}
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, testEngine(t, root), root, sink, Options{StateDir: filepath.Join(root, ".enola", "watch"), WatchEvery: time.Millisecond})
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
	independentWrite(t, root, "src/a.ts", "export const a=2")
	wait()
	tmp := filepath.Join(root, "src/atomic.tmp")
	os.WriteFile(tmp, []byte("export const a=3"), 0644)
	os.Rename(tmp, filepath.Join(root, "src/a.ts"))
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
}

func TestResidentUnauditedInactiveDetectorReconciles(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	eng := testEngine(t, root)
	eng.Config().Extractors = append(eng.Config().Extractors, "custom")
	eng.RegisterExtractor(stubExtractor{name: "custom", detect: false})
	r, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	q := NewChangeQueue("custom", 4)
	q.Start(context.Background())
	residentApply(t, r, q)
	independentWrite(t, root, "a.ts", "export const a=2")
	q.Add("a.ts")
	res := residentApply(t, r, q)
	if !res.Reconciled || !strings.Contains(res.FallbackReason, "custom") {
		t.Fatalf("custom detector was assumed independent: %+v", res)
	}
}

func TestFileChangeSourceExternalExtendedConfig(t *testing.T) {
	root, r, _, _ := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
	external := filepath.Join(t.TempDir(), "base.json")
	os.WriteFile(external, []byte("{}"), 0644)
	b, _ := json.Marshal(map[string]string{"extends": external})
	os.WriteFile(filepath.Join(root, "tsconfig.json"), b, 0644)
	source := NewFileChangeSource(root, []string{r.opts.StateDir}, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := source.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := r.ApplyChanges(ctx, source.Drain()); err != nil {
		t.Fatal(err)
	}
	if err := source.CoverSessionInputs(r); err != nil {
		t.Fatal(err)
	}
	batch := source.Drain()
	if batch.Reconcile == "" {
		t.Fatal("external registration gap not reconciled")
	}
	if _, err := r.ApplyChanges(ctx, batch); err != nil {
		t.Fatal(err)
	}
	source.Drain()
	os.WriteFile(external, []byte(`{"compilerOptions":{"strict":true}}`), 0644)
	select {
	case <-source.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("external config event missing")
	}
	batch = source.Drain()
	res, err := r.ApplyChanges(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reconciled {
		t.Fatal("external config did not trigger reconciliation")
	}
}

func TestExternalCoverageIgnoresSiblingLogsAndDetectsAtomicConfig(t *testing.T) {
	root, r, _, _ := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
	externalDir := t.TempDir()
	external := filepath.Join(externalDir, "base.json")
	os.WriteFile(external, []byte("{}"), 0644)
	b, _ := json.Marshal(map[string]string{"extends": external})
	os.WriteFile(filepath.Join(root, "tsconfig.json"), b, 0644)
	source := NewFileChangeSource(root, []string{r.opts.StateDir}, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := source.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := r.ApplyChanges(ctx, source.Drain()); err != nil {
		t.Fatal(err)
	}
	if err := source.CoverSessionInputs(r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyChanges(ctx, source.Drain()); err != nil {
		t.Fatal(err)
	}
	if err := source.CoverSessionInputs(r); err != nil {
		t.Fatal(err)
	}
	source.Drain()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(externalDir, "run.log"), []byte(strings.Repeat("log", i+1)), 0644)
	}
	os.Mkdir(filepath.Join(externalDir, "state"), 0755)
	os.WriteFile(filepath.Join(externalDir, "state", "checkpoint"), []byte("data"), 0644)
	select {
	case <-source.Ready():
		t.Fatalf("external sibling triggered analysis: %+v", source.Drain())
	case <-time.After(150 * time.Millisecond):
	}
	tmp := filepath.Join(externalDir, "config.tmp")
	os.WriteFile(tmp, []byte(`{"compilerOptions":{"strict":true}}`), 0644)
	os.Rename(tmp, external)
	select {
	case <-source.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("atomic external config replacement missing")
	}
	res, err := r.ApplyChanges(ctx, source.Drain())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reconciled {
		t.Fatal("external config replacement was ignored")
	}
}

func TestWatchRetriesCapturedInputChange(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a=1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &watchEndSink{ends: make(chan struct{}, 4)}
	done := make(chan error, 1)
	var once sync.Once
	eng := testEngine(t, root)
	go func() {
		done <- Watch(ctx, eng, root, sink, Options{StateDir: t.TempDir(), WatchEvery: time.Millisecond, OnBeforeParse: func(string) {
			once.Do(func() { os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=2"), 0644) })
		}})
	}()
	select {
	case <-sink.ends:
	case err := <-done:
		t.Fatalf("watch did not retry: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatal("retry did not complete")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not close")
	}
}
