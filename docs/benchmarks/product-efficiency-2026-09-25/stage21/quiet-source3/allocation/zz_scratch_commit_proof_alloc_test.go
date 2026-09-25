//go:build scratchprobe

// Scratch-only allocation probe for the whole commitProof boundary at exact
// Product size. It is not part of the frozen patch or its manifest, and it is
// not a latency measurement: the host is shared, iteration counts are small,
// and only bytes/op and allocs/op are reported as results.
//
// What is inside the measured boundary, matching commitProof's body
// (stateproof_commit.go:39):
//
//   - filesToHash(eng, inv, nil, detected)       the prev-nil selection
//   - filesToHash(eng, inv, st.Files, detected)  the with-prior selection
//   - buildStateProof(...)                       the proof payload
//   - writePendingProof(dir, p)                  the staged write
//   - promotePendingProof(dir)                   the rename into place
//
// What is deliberately outside it, in loadCommitFixture, which every benchmark
// calls once before b.ResetTimer:
//
//   - reading and JSON-decoding the 56MB Product state
//   - constructing the engine and its graph input policy
//   - eng.Inventory and eng.DetectExtractors
//   - eng.FileHashes over every selected target
//
// The engine, config overlay and Product checkout are the ones root's
// product-proof-profile3.py harness uses, so filesToHash selects the real
// semantic inventory rather than the state's owner keys alone. The checkout is
// an APFS clone in this worker's own scratch; root's copy is never written.
package graphsession

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/pkg/bootstrap"
)

const (
	allocProductPath = "/tmp/enola-stage21-proof/alloc/product"
	allocConfigPath  = "/tmp/enola-stage21-proof/alloc/product-config.yaml"
	allocStatePath   = "/tmp/enola-stage21-proof/alloc/product-state.json"
)

type commitFixture struct {
	eng      *engine.Engine
	abs      string
	st       *State
	fp       stateFingerprint
	inv      engine.RepoInventory
	detected map[string]bool
	hashes   map[string]string
	// selections are recomputed inside the measured boundary; these copies
	// exist only so the component benchmarks and the inventory report can see
	// the counts without paying for them again.
	nilPrior   []string
	withPrior  []string
	stateBytes int
}

func loadCommitFixture(tb testing.TB) *commitFixture {
	tb.Helper()
	if _, err := os.Stat(allocProductPath); err != nil {
		tb.Skipf("product checkout absent: %v", err)
	}
	b, err := os.ReadFile(allocStatePath)
	if err != nil {
		tb.Skipf("product state absent: %v", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		tb.Fatalf("decode product state: %v", err)
	}
	// StateDir is outside the checkout, as it is in root's harness, so the
	// output projection in NewGraphEngine drops it rather than excluding a
	// directory the real run did not exclude.
	bs, err := bootstrap.NewGraphEngine(bootstrap.GraphOptions{
		Repo:       allocProductPath,
		ConfigPath: allocConfigPath,
		StateDirs:  []string{"/tmp/enola-stage21-proof/alloc/probe-state"},
	})
	if err != nil {
		tb.Fatalf("graph engine: %v", err)
	}
	eng := bs.Analysis()
	inv, err := eng.Inventory(allocProductPath)
	if err != nil {
		tb.Fatalf("inventory: %v", err)
	}
	detected := eng.DetectExtractors(allocProductPath, inv.AllNames)
	f := &commitFixture{
		eng:        eng,
		abs:        allocProductPath,
		st:         &st,
		fp:         fingerprintStateBytes(b),
		inv:        inv,
		detected:   detected,
		stateBytes: len(b),
	}
	f.nilPrior = filesToHash(eng, inv, nil, detected)
	f.withPrior = filesToHash(eng, inv, st.Files, detected)
	f.hashes = eng.FileHashes(allocProductPath, f.withPrior)
	return f
}

// newProbeSession is the smallest session commitProof reads: the engine it
// selects with, the state directory it writes into, and the work counters it
// records into. No production code is changed to make this possible; every
// field it sets is one commitProof already reads.
func newProbeSession(f *commitFixture, dir string) *session {
	return &session{eng: f.eng, abs: f.abs, opts: Options{StateDir: dir}}
}

// TestCommitProofAllocBoundary reports the fixture the benchmarks measure, so
// the target counts are evidence rather than an assumption.
func TestCommitProofAllocBoundary(t *testing.T) {
	f := loadCommitFixture(t)
	t.Logf("state_bytes=%d owner_records=%d inventory_files=%d all_names=%d detected=%d",
		f.stateBytes, len(f.st.Files), len(f.inv.Files), len(f.inv.AllNames), len(f.detected))
	t.Logf("nil_prior_targets=%d with_prior_targets=%d hashes=%d",
		len(f.nilPrior), len(f.withPrior), len(f.hashes))
	extra := map[string]bool{}
	for _, p := range f.withPrior {
		if _, ok := f.st.Files[p]; !ok {
			extra[p] = true
		}
	}
	t.Logf("targets_absent_from_state=%d", len(extra))
	for p := range extra {
		t.Logf("  extra_target=%s", p)
	}
	dir := t.TempDir()
	s := newProbeSession(f, dir)
	s.commitProof(f.st, f.fp, f.inv, f.detected, f.hashes, proofForCommitted)
	if s.work.ProofRefusal != "" {
		t.Fatalf("probe fixture does not produce a proof: %s", s.work.ProofRefusal)
	}
	t.Logf("proof_writes=%d proof_write_bytes=%d", s.work.ProofWrites, s.work.ProofWriteBytes)
}

// BenchmarkCommitProofWhole is the number root asked for: the entire
// commitProof boundary including both selections, build, stage and promotion.
func BenchmarkCommitProofWhole(b *testing.B) {
	f := loadCommitFixture(b)
	dir := b.TempDir()
	s := newProbeSession(f, dir)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.commitProof(f.st, f.fp, f.inv, f.detected, f.hashes, proofForCommitted)
	}
	b.StopTimer()
	if s.work.ProofRefusal != "" {
		b.Fatalf("proof refused: %s", s.work.ProofRefusal)
	}
}

// BenchmarkCommitProofStaged is the same boundary for the pending disposition,
// which stages the proof and returns without promoting it.
func BenchmarkCommitProofStaged(b *testing.B) {
	f := loadCommitFixture(b)
	dir := b.TempDir()
	s := newProbeSession(f, dir)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.commitProof(f.st, f.fp, f.inv, f.detected, f.hashes, proofForPending)
	}
	b.StopTimer()
	if s.work.ProofRefusal != "" {
		b.Fatalf("proof refused: %s", s.work.ProofRefusal)
	}
}

func BenchmarkCommitProofSelectNilPrior(b *testing.B) {
	f := loadCommitFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := filesToHash(f.eng, f.inv, nil, f.detected); len(got) != len(f.nilPrior) {
			b.Fatalf("selection moved: %d want %d", len(got), len(f.nilPrior))
		}
	}
}

func BenchmarkCommitProofSelectWithPrior(b *testing.B) {
	f := loadCommitFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := filesToHash(f.eng, f.inv, f.st.Files, f.detected); len(got) != len(f.withPrior) {
			b.Fatalf("selection moved: %d want %d", len(got), len(f.withPrior))
		}
	}
}

func BenchmarkCommitProofBuild(b *testing.B) {
	f := loadCommitFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildStateProof(f.st, f.fp, f.nilPrior, f.withPrior, f.hashes, f.detected); err != nil {
			b.Fatalf("build: %v", err)
		}
	}
}

func BenchmarkCommitProofStage(b *testing.B) {
	f := loadCommitFixture(b)
	p, err := buildStateProof(f.st, f.fp, f.nilPrior, f.withPrior, f.hashes, f.detected)
	if err != nil {
		b.Fatalf("build: %v", err)
	}
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, reject := writePendingProof(dir, p); reject != "" {
			b.Fatalf("stage: %s", reject)
		}
	}
}

// BenchmarkCommitProofPromote stages outside the timer each iteration, because
// promotePendingProof consumes the staged file and has nothing to rename on a
// second call.
func BenchmarkCommitProofPromote(b *testing.B) {
	f := loadCommitFixture(b)
	p, err := buildStateProof(f.st, f.fp, f.nilPrior, f.withPrior, f.hashes, f.detected)
	if err != nil {
		b.Fatalf("build: %v", err)
	}
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if _, reject := writePendingProof(dir, p); reject != "" {
			b.Fatalf("stage: %s", reject)
		}
		b.StartTimer()
		if reject := promotePendingProof(dir); reject != "" {
			b.Fatalf("promote: %s", reject)
		}
	}
}
