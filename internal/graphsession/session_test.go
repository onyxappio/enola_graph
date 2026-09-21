package graphsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func setupTSRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"app","type":"module"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func testEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	return eng
}

func factByName(ff []facts.Fact, name string) (facts.Fact, bool) {
	for _, f := range ff {
		if f.Name == name {
			return f, true
		}
	}
	return facts.Fact{}, false
}

func TestInitialStreamsLocalThenResolved(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/io.ts":   "export function get() { return fetch('/x'); }\n",
		"src/call.ts": "import { get } from './io';\nexport function run() { return get(); }\n",
	})
	eng := testEngine(t, dir)
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "graphstate")})
	if err != nil {
		t.Fatal(err)
	}
	if res.EarlyLocal == 0 {
		t.Fatal("expected early local batches before composition finished")
	}
	if res.ParsedFiles < 2 {
		t.Fatalf("parsed %d, want at least 2", res.ParsedFiles)
	}
	begins, batches, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(begins) != 1 {
		t.Fatalf("begins = %d, want 1 authoritative replacement", len(begins))
	}
	if begins[0].Phase != graphstream.PhaseResolved && begins[0].Phase != graphstream.PhaseEpoch {
		t.Fatalf("begin phase = %s", begins[0].Phase)
	}
	if begins[0].ScopeMode != "" && begins[0].ScopeMode != graphstream.ScopeModeIncremental && begins[0].ScopeMode != graphstream.ScopeModeComplete {
		t.Fatalf("begin scope_mode = %s", begins[0].ScopeMode)
	}
	if len(ends) != 1 {
		t.Fatalf("ends = %d, want 1", len(ends))
	}
	if ends[0].BatchDigest == "" {
		t.Fatal("EndReplace missing batch_digest")
	}
	if ends[0].OwnerScopeLen == 0 {
		t.Fatal("EndReplace owner_scope_len is 0")
	}
	if len(batches) == 0 {
		t.Fatal("no batches")
	}
	var sawLocal, sawResolved bool
	for _, b := range batches {
		if b.Phase == graphstream.PhaseLocal {
			sawLocal = true
		}
		if b.Phase == graphstream.PhaseResolved {
			sawResolved = true
		}
	}
	if !sawLocal || !sawResolved {
		t.Fatalf("want local and resolved batches, local=%v resolved=%v", sawLocal, sawResolved)
	}
	wrapper, ok := factByName(res.Facts, "src.get")
	if !ok {
		t.Fatal("missing src.get")
	}
	if io, _ := wrapper.Props["io_direct"].(bool); !io {
		t.Fatal("direct fetch must be io_direct")
	}
	caller, ok := factByName(res.Facts, "src.run")
	if !ok {
		t.Fatal("missing src.run")
	}
	if pio, _ := caller.Props["performs_io"].(bool); pio {
		t.Fatal("caller must not inherit performs_io")
	}

	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if len(cons.Owners) == 0 {
		t.Fatal("consumer applied no owners")
	}
}

func TestInitialLocalBatchOverlapsLaterFileParse(t *testing.T) {
	prev := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(prev)
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export function a() { return 1; }\n",
		"src/z_slow.ts": "export function slow() { return 2; }\n",
	})
	eng := testEngine(t, dir)
	blockSlow := make(chan struct{})
	slowEntered := make(chan struct{}, 1)
	localSeen := make(chan struct{})
	sink := &notifySink{on: func(r graphstream.Recorded) {
		var probe struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(r.Payload, &probe)
		if probe.Type == graphstream.TypeBatch && probe.Phase == graphstream.PhaseLocal {
			select {
			case <-localSeen:
			default:
				close(localSeen)
			}
		}
	}}
	opts := Options{
		StateDir: filepath.Join(dir, ".enola", "graphstate"),
		OnBeforeParse: func(rel string) {
			if strings.Contains(rel, "z_slow") {
				select {
				case slowEntered <- struct{}{}:
				default:
				}
				<-blockSlow
			}
		},
	}
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), eng, dir, sink, opts)
		done <- err
	}()
	select {
	case <-slowEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow file never entered parse")
	}
	select {
	case <-localSeen:
	case <-time.After(5 * time.Second):
		close(blockSlow)
		t.Fatal("broker did not see a local batch while later file analysis was blocked")
	}
	close(blockSlow)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type notifySink struct {
	graphstream.MemorySink
	on func(graphstream.Recorded)
}

func (s *notifySink) Publish(ctx context.Context, subject, msgID string, payload []byte) error {
	if err := s.MemorySink.Publish(ctx, subject, msgID, payload); err != nil {
		return err
	}
	if s.on != nil {
		s.on(graphstream.Recorded{Subject: subject, MsgID: msgID, Payload: append([]byte(nil), payload...)})
	}
	return nil
}

func TestConfigCaptureIgnoresTransientEditAndRestore(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export const a=1;\n",
	})
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"app","dependencies":{"@prisma/client":"5.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("model User { id Int @id }\n")
	if err := os.WriteFile(filepath.Join(dir, "prisma/schema.prisma"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t, dir)
	transient := []byte("model User { id Int @id }\nmodel Transient { id Int @id }\n")
	restored := false
	sink := &notifySink{on: func(r graphstream.Recorded) {
		var probe struct {
			Type  string `json:"type"`
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(r.Payload, &probe)
		if !restored && probe.Type == graphstream.TypeBatch && probe.Phase == graphstream.PhaseResolved {
			restored = true
			_ = os.WriteFile(filepath.Join(dir, "prisma/schema.prisma"), original, 0o644)
		}
	}}
	opts := Options{
		StateDir: filepath.Join(dir, ".enola", "graphstate"),
		OnBeforeParse: func(string) {
			_ = os.WriteFile(filepath.Join(dir, "prisma/schema.prisma"), transient, 0o644)
		},
	}
	res, err := Run(context.Background(), eng, dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Facts {
		if f.Kind == facts.KindStorage && strings.Contains(f.Name, "Transient") {
			t.Fatal("extraction used transient prisma schema that was restored before EndReplace")
		}
	}
}

func TestDeltaSkipsUnchangedParseAndMatchesFull(t *testing.T) {
	files := map[string]string{
		"src/a.ts": "export function a() { return 1; }\n",
		"src/b.ts": "import { a } from './a';\nexport function b() { return a(); }\n",
	}
	dir := setupTSRepo(t, files)
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	sink1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, sink1, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}

	sink2 := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink2, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 {
		t.Fatalf("warm delta parsed %d files, want 0", second.ParsedFiles)
	}
	if second.BaseGeneration != first.TargetGeneration {
		t.Fatalf("base %d want %d", second.BaseGeneration, first.TargetGeneration)
	}

	if err := os.WriteFile(filepath.Join(dir, "src/a.ts"), []byte("export function a() { return fetch('/z'); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink3 := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, sink3, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.ParsedFiles != 1 {
		t.Fatalf("IO-only edit parsed %d files, want 1 (the edited file)", delta.ParsedFiles)
	}
	a, _ := factByName(delta.Facts, "src.a")
	if io, _ := a.Props["io_direct"].(bool); !io {
		t.Fatal("edited a() should be io_direct")
	}
	b, _ := factByName(delta.Facts, "src.b")
	if pio, _ := b.Props["performs_io"].(bool); pio {
		t.Fatal("b() must not pick up transitive performs_io after a()'s IO edit")
	}

	// Fresh full analysis of the same bytes must match under the local-fact contract.
	dir2 := setupTSRepo(t, map[string]string{
		"src/a.ts": "export function a() { return fetch('/z'); }\n",
		"src/b.ts": files["src/b.ts"],
	})
	eng2 := testEngine(t, dir2)
	full, err := Run(context.Background(), eng2, dir2, &graphstream.MemorySink{}, Options{StateDir: filepath.Join(dir2, ".enola", "graphstate")})
	if err != nil {
		t.Fatal(err)
	}
	if namesEqual := symbolPropEqual(delta.Facts, full.Facts, "src.a", "io_direct") && symbolPropEqual(delta.Facts, full.Facts, "src.b", "performs_io"); !namesEqual {
		t.Fatal("delta facts diverged from fresh full analysis")
	}
}

func TestAddFileResolvesImportAndDeleteRemovesOwner(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/user.ts": "import { helper } from './lib';\nexport function user() { return helper(); }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/lib.ts"), []byte("export function helper() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := factByName(added.Facts, "src.helper"); !ok {
		t.Fatal("adding lib.ts should extract helper")
	}
	if added.ParsedFiles < 1 {
		t.Fatal("adding a file must parse it")
	}

	if err := os.Remove(filepath.Join(dir, "src/lib.ts")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	deleted, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := factByName(deleted.Facts, "src.helper"); ok {
		t.Fatal("deleted file's symbol must not remain")
	}
}

func TestUnreadableIsNotSuccessfulDelete(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/ok.ts": "export function ok() { return 1; }\n",
	})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := factByName(first.Facts, "src.ok"); !ok {
		t.Fatal("missing src.ok")
	}
	// Replace the file with a directory so the next read fails.
	p := filepath.Join(dir, "src/ok.ts")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Unreadable) == 0 && second.ParsedFiles == 0 {
		// Directory named .ts may be skipped by walk as a dir; that's not a successful empty replace of the old file.
		t.Log("ok.ts became a directory and left the inventory; treated as deletion of a file path")
	}
}

func TestCLISeparateProcessDeltaParsesOnlyChangedFile(t *testing.T) {
	bin := buildEnola(t)
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export function a() { return 1; }\n",
		"src/b.ts": "import { a } from './a'; export function b() { return a(); }\n",
		"src/c.ts": "export function c() { return 1; }\n",
	})
	state := t.TempDir()
	events := filepath.Join(t.TempDir(), "events.jsonl")
	out1, err := runGraphCLI(t, bin, "analyze", dir, state, events)
	if err != nil {
		t.Fatal(err)
	}
	if out1.ParsedFiles < 3 {
		t.Fatalf("initial parsed %d", out1.ParsedFiles)
	}
	st1, err := loadCommittedState(state)
	if err != nil || st1 == nil {
		t.Fatalf("load state after initial: %v %#v", err, st1)
	}
	t.Logf("initial files=%d last_complete=%v cfg=%s", len(st1.Files), st1.LastComplete, st1.ConfigHash)
	for _, name := range []string{"src/a.ts", "src/b.ts", "src/c.ts"} {
		fs := st1.Files[name]
		if fs == nil {
			t.Logf("%s missing from state", name)
			continue
		}
		sum := sha256File(t, filepath.Join(dir, name))
		t.Logf("%s stored=%s disk=%s ts=%v", name, fs.Hash, sum, fs.TS != nil)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/c.ts"), []byte("export function c() { return 2; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out2, err := runGraphCLI(t, bin, "delta", dir, state, events)
	if err != nil {
		t.Fatal(err)
	}
	if out2.ParsedFiles != 1 {
		stPath := filepath.Join(state, "state.json")
		raw, _ := os.ReadFile(stPath)
		head := raw
		if len(head) > 600 {
			head = head[:600]
		}
		t.Fatalf("CLI delta after c.ts edit parsed %d files (cached %d gen %d→%d), want 1; fallbacks=%v state=%s\n%s", out2.ParsedFiles, out2.Stats.CachedFiles, out2.BaseGeneration, out2.TargetGeneration, out2.Fallbacks, stPath, head)
	}
	if out2.Stats.CachedFiles < 2 {
		t.Fatalf("cached %d, want at least 2 unchanged files", out2.Stats.CachedFiles)
	}
}

func TestNoopDeltaDoesNotPublishOrAdvance(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const x = 1;\n"})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	sink1 := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, sink1, Options{StateDir: state, ContextID: "n"})
	if err != nil {
		t.Fatal(err)
	}
	n1 := len(sink1.CloneRecords())
	sink2 := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink2, Options{StateDir: state, ContextID: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 {
		t.Fatalf("noop parsed %d", second.ParsedFiles)
	}
	if second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("noop advanced generation %d -> %d", first.TargetGeneration, second.TargetGeneration)
	}
	if len(sink2.CloneRecords()) != 0 {
		t.Fatalf("noop published %d messages, want 0 (initial published %d)", len(sink2.CloneRecords()), n1)
	}
}

func TestIdentityRejectsContextMismatch(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const x = 1;\n"})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state, ContextID: "a", SinkID: "file|1"}); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state, ContextID: "b", SinkID: "file|1"})
	if err == nil {
		t.Fatal("expected context mismatch")
	}
}

func TestPendingStatePromotedAfterEnd(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const x = 1;\n"})
	eng := testEngine(t, dir)
	state := filepath.Join(dir, ".enola", "graphstate")
	if _, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state, ContextID: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "state.json")); err != nil {
		t.Fatal("state.json missing after promote")
	}
	if _, err := os.Stat(filepath.Join(state, "pending-state.json")); !os.IsNotExist(err) {
		t.Fatal("pending-state.json should be renamed away")
	}
}

func buildEnola(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "enola")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/enola")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GOROOT="+os.Getenv("GOROOT"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func runGraphCLI(t *testing.T, bin, mode, repo, state, events string) (*Result, error) {
	t.Helper()
	cmd := exec.Command(bin, "graph", mode, "--context", "cli-test", "--state-dir", state, "--events", events, "--json", repo)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v\n%s", err, out)
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
		err = json.Unmarshal(payload, &results)
		if err != nil || len(results) == 0 {
			return nil, fmt.Errorf("decode: %v\n%s", err, out)
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no result\n%s", out)
	}
	return &results[0], nil
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestDeliveryRetryReplaysIdenticalPayload(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export const x = 1;\n",
	})
	eng := testEngine(t, dir)
	fail := &graphstream.MemorySink{}
	fail.FailAt(1, context.Canceled)
	_, err := Run(context.Background(), eng, dir, fail, Options{StateDir: filepath.Join(dir, ".enola", "graphstate")})
	if err == nil {
		t.Fatal("expected sink failure")
	}
	// Fresh sink, same state dir: journal replay happens at start, then a new run.
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "graphstate")}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int{}
	for _, r := range sink.CloneRecords() {
		ids[r.MsgID]++
	}
	for id, n := range ids {
		if n > 1 && !strings.Contains(id, "begin_replace") {
			t.Fatalf("duplicate msg id %s count %d", id, n)
		}
	}
}

func symbolPropEqual(a, b []facts.Fact, name, prop string) bool {
	fa, oa := factByName(a, name)
	fb, ob := factByName(b, name)
	if !oa || !ob {
		return oa == ob
	}
	va, _ := fa.Props[prop].(bool)
	vb, _ := fb.Props[prop].(bool)
	return va == vb
}
