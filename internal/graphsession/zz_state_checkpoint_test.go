package graphsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// committedWithCheckpoint writes a committed state and returns it together
// with the checkpoint a resident holding it would carry.
func committedWithCheckpoint(t *testing.T, dir string, gen int64) (*State, stateCheckpoint) {
	t.Helper()
	st := newState("/repo", "c", "/repo", "v")
	st.Generation = gen
	st.LastComplete = true
	fp, err := saveStateFP(dir, st)
	if err != nil {
		t.Fatal(err)
	}
	if !fp.known() {
		t.Fatal("saveStateFP reported no fingerprint for bytes it just wrote")
	}
	return st, newStateCheckpoint(st, fp)
}

func recoverWithCheckpoint(t *testing.T, dir string, j *graphstream.Journal, ck stateCheckpoint) (*State, stateReadWork, error) {
	t.Helper()
	var w stateReadWork
	st, _, err := recoverAcknowledgedPendingFP(dir, j, Options{ContextID: "c", RepoID: "/repo"}, "/repo", ck, &w)
	return st, w, err
}

// The decode is the expensive half of a state load and the only half this
// change avoids. A resident that refused a transaction walks the recovery
// barrier holding the very state the committed file contains; reading the
// bytes back and finding them identical answers the same question the decode
// would have.
func TestRecoveryProvesCommittedBytesInsteadOfDecodingThem(t *testing.T) {
	dir := t.TempDir()
	held, ck := committedWithCheckpoint(t, dir, 1)
	got, w, err := recoverWithCheckpoint(t, dir, nil, ck)
	if err != nil {
		t.Fatal(err)
	}
	if got != held {
		t.Fatalf("recovery returned a different value than the one it was holding: %+v", got)
	}
	if w.proven != 1 || w.decodes != 0 {
		t.Fatalf("expected one proven committed load and no decode, got proven=%d decodes=%d", w.proven, w.decodes)
	}
	if w.fingerprints != 1 || w.fingerprintBytes == 0 {
		t.Fatalf("the proof left no record of what it cost: %+v", w)
	}
}

// The proof is over the bytes, not over the file's metadata. A state rewritten
// in place to the same length is the case size and modification time would
// both wave through.
func TestRecoveryDecodesWhenCommittedBytesChangedAtEqualLength(t *testing.T) {
	dir := t.TempDir()
	held, ck := committedWithCheckpoint(t, dir, 1)
	other := newState("/repo", "c", "/repo", "v")
	other.Generation = 2
	other.LastComplete = true
	b, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath(dir), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) != ck.fp.size {
		t.Fatalf("fixture no longer exercises equal-length replacement: %d vs %d", len(b), ck.fp.size)
	}
	got, w, err := recoverWithCheckpoint(t, dir, nil, ck)
	if err != nil {
		t.Fatal(err)
	}
	if got == held {
		t.Fatal("recovery served the checkpoint over bytes that had been replaced")
	}
	if got.Generation != 2 {
		t.Fatalf("recovery did not report the state on disk: generation %d", got.Generation)
	}
	if w.proven != 0 || w.decodes != 1 {
		t.Fatalf("expected one decode and no reuse, got proven=%d decodes=%d", w.proven, w.decodes)
	}
}

// Corruption that leaves the length alone must still surface as an error
// rather than be answered from memory.
func TestRecoveryRejectsCorruptCommittedStateAtEqualLength(t *testing.T) {
	dir := t.TempDir()
	_, ck := committedWithCheckpoint(t, dir, 1)
	junk := make([]byte, ck.fp.size)
	for i := range junk {
		junk[i] = '~'
	}
	if err := os.WriteFile(statePath(dir), junk, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := recoverWithCheckpoint(t, dir, nil, ck); err == nil {
		t.Fatal("recovery accepted a corrupt committed state")
	}
}

// A checkpoint whose file is gone describes nothing. Report the disk.
func TestRecoveryReportsMissingCommittedStateOverCheckpoint(t *testing.T) {
	dir := t.TempDir()
	_, ck := committedWithCheckpoint(t, dir, 1)
	if err := os.Remove(statePath(dir)); err != nil {
		t.Fatal(err)
	}
	got, w, err := recoverWithCheckpoint(t, dir, nil, ck)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("recovery invented a state for a file that is gone: %+v", got)
	}
	if w.proven != 0 {
		t.Fatalf("recovery counted a proof it could not have made: %+v", w)
	}
}

// A fresh process holds no checkpoint and must validate the disk exactly as
// before.
func TestRecoveryWithoutCheckpointDecodes(t *testing.T) {
	dir := t.TempDir()
	committedWithCheckpoint(t, dir, 1)
	got, w, err := recoverWithCheckpoint(t, dir, nil, stateCheckpoint{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Generation != 1 {
		t.Fatalf("restart did not read the committed state: %+v", got)
	}
	if w.decodes != 1 || w.proven != 0 {
		t.Fatalf("restart skipped a decode it had no proof for: %+v", w)
	}
}

func appendEnd(t *testing.T, j *graphstream.Journal, runID string, ack bool) {
	t.Helper()
	payload, err := graphstream.Marshal(graphstream.EndReplace{
		Type:         graphstream.TypeEndReplace,
		RunID:        runID,
		Completeness: graphstream.Completeness{Status: "success"},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := runID + ":end_replace:1"
	if err := j.Append(graphstream.JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if ack {
		if err := j.Ack(id); err != nil {
			t.Fatal(err)
		}
	}
}

func writePendingGen(t *testing.T, dir string, gen int64, runID string) {
	t.Helper()
	pending := newState("/repo", "c", "/repo", "v")
	pending.Generation = gen
	pending.LastComplete = true
	pending.LastRunID = runID
	if err := writePendingState(dir, pending); err != nil {
		t.Fatal(err)
	}
}

// A generation this process never decoded is not one it can answer from
// memory. The promotion branch must read pending-state.json and install it,
// checkpoint or no checkpoint.
func TestRecoveryNeverServesCheckpointOverPromotedPending(t *testing.T) {
	dir := t.TempDir()
	held, ck := committedWithCheckpoint(t, dir, 1)
	writePendingGen(t, dir, 2, "run-new")
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendEnd(t, j, "run-new", true)
	got, w, err := recoverWithCheckpoint(t, dir, j, ck)
	if err != nil {
		t.Fatal(err)
	}
	if got == held || got.Generation != 2 {
		t.Fatalf("acknowledged pending was not promoted: %+v", got)
	}
	if w.proven != 0 {
		t.Fatalf("a promotion was answered from the checkpoint: %+v", w)
	}
	if _, err := os.Stat(pendingStatePath(dir)); !os.IsNotExist(err) {
		t.Fatal("pending-state survived its own promotion")
	}
}

// Failure after the pending write and before the End is appended: no
// promotion, and the committed fallback is the same value either way.
func TestRecoveryProvesCommittedStateWithPendingAndNoEnd(t *testing.T) {
	dir := t.TempDir()
	held, ck := committedWithCheckpoint(t, dir, 1)
	writePendingGen(t, dir, 2, "run-new")
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, w, err := recoverWithCheckpoint(t, dir, j, ck)
	if err != nil {
		t.Fatal(err)
	}
	if got != held {
		t.Fatalf("unacknowledged pending displaced the committed state: %+v", got)
	}
	if w.proven != 1 {
		t.Fatalf("committed fallback did not use the proof: %+v", w)
	}
	if _, err := os.Stat(pendingStatePath(dir)); err != nil {
		t.Fatal("pending-state must remain until its End is acked")
	}
}

// Failure after the End is appended but before it is acknowledged: still no
// promotion. The pending decode is unconditional, so it is counted.
func TestRecoveryProvesCommittedStateWithUnackedEnd(t *testing.T) {
	dir := t.TempDir()
	held, ck := committedWithCheckpoint(t, dir, 1)
	writePendingGen(t, dir, 2, "run-new")
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	appendEnd(t, j, "run-new", false)
	got, w, err := recoverWithCheckpoint(t, dir, j, ck)
	if err != nil {
		t.Fatal(err)
	}
	if got != held {
		t.Fatalf("an unacknowledged End promoted its generation: %+v", got)
	}
	if w.decodes != 1 {
		t.Fatalf("pending-state was not decoded at the barrier: %+v", w)
	}
	if w.proven != 1 {
		t.Fatalf("committed fallback did not use the proof: %+v", w)
	}
}

// A corrupt pending file is a fault, not an invitation to skip the barrier.
func TestRecoveryRejectsCorruptPendingWithCheckpointHeld(t *testing.T) {
	dir := t.TempDir()
	_, ck := committedWithCheckpoint(t, dir, 1)
	if err := os.WriteFile(pendingStatePath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := graphstream.OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := recoverWithCheckpoint(t, dir, j, ck); err == nil {
		t.Fatal("a corrupt pending state was walked past")
	}
}

// End to end: a refused transaction leaves the resident failed, and the retry
// that follows crosses the recovery barrier. The committed file has not moved,
// so the retry proves it rather than decoding it, and the graph it publishes
// still equals a cold build.
func TestRefusedTransactionRetryProvesStateAndStaysColdEqual(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"app/tsconfig.json":   `{"compilerOptions":{"baseUrl":"."}}`,
		"app/src/dep.ts":      "export function util(){return 1;}\n",
		"app/src/consumer.ts": "import {util} from './dep'; export function used(){return util();}\n",
		"app/src/trigger.ts":  "export const trigger=1;\n",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	ctx := context.Background()
	sink := &graphstream.MemorySink{}
	stateDir := t.TempDir()
	r, err := OpenSession(ctx, eng, root, sink, Options{StateDir: stateDir, AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	if !r.ck.usable() {
		t.Fatal("a committed run left no checkpoint to prove")
	}
	writeFile(t, root, "app/src/consumer.ts", "import {util} from './dep'; export function used(){return util();} export const added=1;\n")
	writeFile(t, root, "app/src/trigger.ts", "export const trigger=2;\n")
	r.mu.Lock()
	input, reason := r.contentInputs([]string{"app/src/consumer.ts", "app/src/trigger.ts"}, &WorkCounters{})
	r.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture: %s", reason)
	}
	writeFile(t, root, "app/src/trigger.ts", "export const trigger=3;\n")
	r.mu.Lock()
	_, _, err = r.transaction(ctx, input, true)
	r.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("expected refusal: %v", err)
	}
	r.mu.Lock()
	_, work, err := r.transaction(ctx, nil, false)
	r.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if work.StateDecodesProven != 1 || work.StateDecodes != 0 {
		t.Fatalf("the retry re-decoded a committed state it was already holding: proven=%d decodes=%d",
			work.StateDecodesProven, work.StateDecodes)
	}
	if work.StateFingerprints == 0 || work.StateFingerprintBytes == 0 {
		t.Fatalf("the proof reported no cost: %+v", work)
	}
	// The generation the retry published took the other pairing route: the
	// checkpoint is the bytes writePendingStateFP marshalled and
	// promotePendingState renamed over state.json. Re-serializing the state the
	// resident now holds must reproduce exactly those bytes, or a later recovery
	// would prove the wrong value.
	promoted, err := json.Marshal(r.state)
	if err != nil {
		t.Fatal(err)
	}
	if fingerprintStateBytes(promoted) != r.ck.fp {
		t.Fatal("the promoted checkpoint does not pair with the state the resident holds")
	}
	committed, err := os.ReadFile(statePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprintStateBytes(committed) != r.ck.fp {
		t.Fatal("state.json does not hold the bytes the promoted checkpoint claims")
	}

	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// The checkpoint is only sound if the State a resident adopted is still the
// value its bytes described once a later attempt has run over it and failed.
// The state is serialized with the same deterministic json.Marshal the
// committed write uses, so re-serializing the adopted value and digesting it is
// a direct check of that: the digest must equal both the checkpoint's and the
// one over the bytes sitting in state.json. A run that mutated a record in
// place instead of cloning it would move this digest.
func TestRefusedAttemptLeavesAdoptedStateSerializingToTheSameBytes(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"app/tsconfig.json":   `{"compilerOptions":{"baseUrl":"."}}`,
		"app/src/dep.ts":      "export function util(){return 1;}\n",
		"app/src/consumer.ts": "import {util} from './dep'; export function used(){return util();}\n",
		"app/src/trigger.ts":  "export const trigger=1;\n",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	ctx := context.Background()
	sink := &graphstream.MemorySink{}
	stateDir := t.TempDir()
	r, err := OpenSession(ctx, eng, root, sink, Options{StateDir: stateDir, AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}

	serialize := func(st *State) stateFingerprint {
		t.Helper()
		b, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		return fingerprintStateBytes(b)
	}
	adopted := r.state
	before := serialize(adopted)
	if before != r.ck.fp {
		t.Fatalf("the committed state does not re-serialize to the bytes it was written as: %d vs %d bytes",
			before.size, r.ck.fp.size)
	}
	onDisk, err := os.ReadFile(statePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprintStateBytes(onDisk) != before {
		t.Fatal("state.json does not hold the bytes the adopted state serializes to")
	}

	writeFile(t, root, "app/src/consumer.ts", "import {util} from './dep'; export function used(){return util();} export const added=1;\n")
	writeFile(t, root, "app/src/trigger.ts", "export const trigger=2;\n")
	r.mu.Lock()
	input, reason := r.contentInputs([]string{"app/src/consumer.ts", "app/src/trigger.ts"}, &WorkCounters{})
	r.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture: %s", reason)
	}
	writeFile(t, root, "app/src/trigger.ts", "export const trigger=3;\n")
	r.mu.Lock()
	_, _, err = r.transaction(ctx, input, true)
	r.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("expected refusal: %v", err)
	}

	if r.state != adopted {
		t.Fatal("a refused attempt replaced the resident's state")
	}
	if after := serialize(adopted); after != before {
		t.Fatalf("the failed attempt changed the adopted state in memory: %d/%x vs %d/%x",
			after.size, after.digest[:4], before.size, before.digest[:4])
	}
	if r.ck.fp != before {
		t.Fatal("a refused attempt moved the checkpoint")
	}
}

func benchState(files int) *State {
	st := newState("/repo", "c", "/repo", "v")
	st.Generation = 1
	st.LastComplete = true
	for i := 0; i < files; i++ {
		p := fmt.Sprintf("app/src/mod%04d/component%04d.ts", i%64, i)
		st.Files[p] = &FileState{
			Hash:      fmt.Sprintf("%064x", i),
			Extractor: "ts",
			Facts: []facts.Fact{
				{Kind: "module", Name: p, File: p, Line: 1},
				{Kind: "symbol", Name: fmt.Sprintf("sym%04d", i), File: p, Line: 3, Props: map[string]any{"exported": true}},
			},
			Imports:  []string{fmt.Sprintf("app/src/mod%04d/component%04d.ts", (i+1)%64, (i+1)%files)},
			Declared: []string{fmt.Sprintf("sym%04d", i)},
		}
	}
	return st
}

// What the proof costs against what it avoids, on the read path: a whole-file
// digest versus the json.Unmarshal it stands in for. Also reported for the
// write path, where the digest is added to a marshal that already happened.
func BenchmarkStateFingerprintVersusDecode(b *testing.B) {
	for _, files := range []int{500, 4000} {
		st := benchState(files)
		raw, err := json.Marshal(st)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("fingerprint/files=%d/bytes=%d", files, len(raw)), func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				if !fingerprintStateBytes(raw).known() {
					b.Fatal("no fingerprint")
				}
			}
		})
		b.Run(fmt.Sprintf("decode/files=%d/bytes=%d", files, len(raw)), func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				if _, err := decodeStateBytes("state.json", raw); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("marshal/files=%d/bytes=%d", files, len(raw)), func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				if _, err := json.Marshal(st); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
