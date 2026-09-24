package graphsession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// A run reads the tree before it announces anything: the planner preview
// compiles the dirty files and discovery reads the manifests beside them, all
// before the invalidation plan is frozen and a Begin is sent. A write landing in
// that window used to be caught only by the fence before a successful End, so
// the attempt announced a replacement and published facts derived from bytes
// that were already gone before refusing. These guard the earlier refusal, and
// they guard that it is a refusal and not a silence: the graph that follows must
// still be the graph a cold run produces.

// A run whose consumed bytes are untouched must not pay a refusal for the new
// question, and must not pay an unbounded number of reads for it either: the
// re-read covers what this run already read, never the repository.
func TestEarlySupersessionCheckIsBoundedAndSilent(t *testing.T) {
	files := map[string]string{"src/entry.ts": "export const e=0;\n"}
	for i := 0; i < 60; i++ {
		files[bulkSourceName(i)] = "export const x=0;\n"
	}
	root := setupTSRepo(t, files)
	eng := testEngine(t, root)
	ctx := context.Background()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	r, err := OpenSession(ctx, eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.ApplyChanges(ctx, ChangeBatch{Reconcile: "initial"}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "src/entry.ts", "export const e=1;\n")
	changed, err := r.ApplyChanges(ctx, ChangeBatch{Reconcile: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Result.OwnersPublished == 0 {
		t.Fatal("a real edit published nothing")
	}
	t.Logf("pre-Begin reads: delta=%d (repo=%d files); End-fence reads: %d", changed.Work.EarlyConsumedReads, len(files), changed.Work.VerifiedFiles)
	if n := changed.Work.EarlyConsumedReads; n == 0 || n > 12 {
		t.Fatalf("pre-Begin re-read %d file(s) for a one-file delta in a %d-file repository", n, len(files))
	}
}

func bulkSourceName(i int) string {
	return "src/f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".ts"
}

// The fence itself, asked directly: a session that has consumed bytes which are
// still on disk answers yes, and one whose bytes were overwritten answers with
// the retryable error - the same error the fence before End uses, because it is
// the same question asked earlier.
func TestConsumedInputsFenceAnswersDirectly(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.0"}`)
	body, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	// AuthoritativeFiles because the fence is asked only of the frozen contract;
	// the legacy streaming path keeps its own transient-configuration behaviour.
	s := &session{abs: root, opts: Options{AuthoritativeFiles: true},
		capturedSources: map[string][]byte{"package.json": append([]byte(nil), body...)}}
	if err := s.consumedInputsStillCurrent(); err != nil {
		t.Fatalf("an unmoved tree refused its own announcement: %v", err)
	}
	if s.work.EarlyConsumedReads != 1 {
		t.Fatalf("the fence read %d consumed source(s), want 1", s.work.EarlyConsumedReads)
	}
	if s.work.CapturedReads != 0 {
		t.Fatalf("the pre-Begin fence charged %d read(s) to the fence before End", s.work.CapturedReads)
	}
	writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.1"}`)
	err = s.consumedInputsStillCurrent()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("an overwritten input gave %v, want a retryable supersession", err)
	}
	if err := s.revalidateCapturedInputs("refusing successful EndReplace"); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("the fence before End no longer answers the same question: %v", err)
	}
}

// The fence is part of the frozen file-owner contract and only that. A legacy
// streaming run is allowed to read configuration that is overwritten and
// restored underneath it and still finish - see
// TestConfigChangeRestoreUsesCapturedBytes, whose fixture poisons once and
// restores once, so a retry there would find the poison and never converge. The
// exemption is written down here so that removing it fails a test rather than a
// suite.
func TestConsumedInputsFenceIsFrozenContractOnly(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{"name":"x"}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	captured := func() map[string][]byte {
		return map[string][]byte{"package.json": append([]byte(nil), body...)}
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"y"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := &session{abs: root, capturedSources: captured()}
	if err := legacy.consumedInputsStillCurrent(); err != nil {
		t.Fatalf("legacy streaming run refused a moved capture before Begin: %v", err)
	}
	if legacy.work.EarlyConsumedReads != 0 {
		t.Fatalf("legacy run paid %d early read(s)", legacy.work.EarlyConsumedReads)
	}
	frozen := &session{abs: root, opts: Options{AuthoritativeFiles: true}, capturedSources: captured()}
	if err := frozen.consumedInputsStillCurrent(); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("frozen run did not refuse a moved capture: %v", err)
	}
}
