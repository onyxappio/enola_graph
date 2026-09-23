package graphsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

type independentManifestEndFailure struct {
	graphstream.MemorySink
	fail atomic.Bool
}

func (s *independentManifestEndFailure) Publish(ctx context.Context, subject, id string, b []byte) error {
	var v struct{ Type string }
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.Type == graphstream.TypeEndReplace && s.fail.Load() {
		return errors.New("independent injected End failure")
	}
	return s.MemorySink.Publish(ctx, subject, id, b)
}

// TestIndependentManifestHashRefreshFailedEndRollback drives a manifest edit that
// genuinely moves the graph, so the run has to publish, and refuses its End. The
// refreshed manifest observation rides along with that publication, so the
// question the test asks is whether a refusal takes it back out again: neither
// the resident state nor the completed state on disk may carry an input the run
// never got to commit, and the retry has to reach the same graph a cold run does.
//
// Version-only and factless-added manifests used to be the edits here. They no
// longer attempt an End at all - proveNonTSNeutrality settles them before Begin -
// so keeping them would have asserted a publication the run is now correct to
// skip; TestIndependentNeutralManifestChangeNeverPublishes covers them instead.
func TestIndependentManifestHashRefreshFailedEndRollback(t *testing.T) {
	for _, mode := range []string{"dependency", "added-dependency"} {
		t.Run(mode, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"index.ts": "export function work(){return 1}", "package.json": `{"name":"app","version":"1.0.0","dependencies":{"some-lib":"^1.0.0"}}`})
			sink := &independentManifestEndFailure{}
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			r, err := OpenSession(context.Background(), configScopeEngine(t, root), root, sink, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err = r.reconcile(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(r.state)
			if err != nil {
				t.Fatal(err)
			}
			diskBefore, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "dependency" {
				writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.0","dependencies":{"some-lib":"^2.0.0"}}`)
			} else {
				if err := os.MkdirAll(filepath.Join(root, "side"), 0755); err != nil {
					t.Fatal(err)
				}
				writeRepoFile(t, root, "side/package.json", `{"name":"side","version":"1.0.0","dependencies":{"other-lib":"^1.0.0"}}`)
			}
			sink.fail.Store(true)
			if _, err = r.reconcile(context.Background(), false); err == nil {
				t.Fatal("expected End failure")
			}
			after, _ := json.Marshal(r.state)
			if !bytes.Equal(before, after) {
				t.Fatal("failed End mutated committed resident state")
			}
			diskAfter, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(diskBefore, diskAfter) {
				t.Fatal("failed End mutated completed disk state")
			}
			sink.fail.Store(false)
			if _, err = r.reconcile(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			coldSink := &graphstream.MemorySink{}
			if _, err = Run(context.Background(), configScopeEngine(t, root), root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
				t.Fatal(err)
			}
			applied, cold := NewConsumer(), NewConsumer()
			applyRun(t, applied, &sink.MemorySink)
			applyRun(t, cold, coldSink)
			assertAppliedEqualsCold(t, applied, cold)
			priorGeneration := r.state.Generation
			n := len(sink.CloneRecords())
			if _, err = r.reconcile(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if r.state.Generation != priorGeneration || len(sink.CloneRecords()) != n {
				t.Fatal("recovered manifest edit did not settle")
			}
		})
	}
}

// independentAnyPublishFailure refuses every record, so a run that publishes at
// all fails instead of being inspected afterwards. A neutral edit has to get
// past it without noticing it is there.
type independentAnyPublishFailure struct {
	graphstream.MemorySink
	armed atomic.Bool
}

func (s *independentAnyPublishFailure) Publish(ctx context.Context, subject, id string, b []byte) error {
	if s.armed.Load() {
		var v struct{ Type string }
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		return errors.New("independent injected failure: published " + v.Type + " for a graph-neutral manifest edit")
	}
	return s.MemorySink.Publish(ctx, subject, id, b)
}

// TestIndependentNeutralManifestChangeNeverPublishes is the other half of the
// rollback guard. These are the manifest edits whose output is the output the
// stored state already carries: a version bump, and a manifest added where there
// was none that contributes no facts. The run still has to read them - their
// bytes moved, so their input hashes moved - but it must decide before Begin
// that there is nothing to publish, record what it read, and settle. Arming the
// sink rather than counting records afterwards is deliberate: it distinguishes
// publishing nothing from publishing a replacement of the same thing.
func TestIndependentNeutralManifestChangeNeverPublishes(t *testing.T) {
	for _, mode := range []string{"version", "added-empty"} {
		t.Run(mode, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"index.ts": "export function work(){return 1}", "package.json": `{"name":"app","version":"1.0.0","dependencies":{"some-lib":"^1.0.0"}}`})
			sink := &independentAnyPublishFailure{}
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			r, err := OpenSession(context.Background(), configScopeEngine(t, root), root, sink, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err = r.reconcile(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			settled := r.state.Generation
			records := len(sink.CloneRecords())
			var priorState State
			priorDisk, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(priorDisk, &priorState); err != nil {
				t.Fatal(err)
			}
			if mode == "version" {
				writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.1","dependencies":{"some-lib":"^1.0.0"}}`)
			} else {
				if err := os.MkdirAll(filepath.Join(root, "side"), 0755); err != nil {
					t.Fatal(err)
				}
				writeRepoFile(t, root, "side/package.json", `{"name":"side","version":"1.0.0"}`)
			}
			sink.armed.Store(true)
			res, err := r.reconcile(context.Background(), false)
			if err != nil {
				t.Fatalf("graph-neutral manifest edit attempted a publication: %v", err)
			}
			if res.ParsedFiles != 0 {
				t.Fatalf("parsed=%d, want no reparse for a graph-neutral manifest edit", res.ParsedFiles)
			}
			if r.state.Generation != settled {
				t.Fatalf("graph-neutral manifest edit advanced the generation %d -> %d", settled, r.state.Generation)
			}
			// The proof is worth nothing if it is not written down: a run that
			// reads bytes the stored state does not describe and keeps quiet
			// about them makes the next run prove the same thing again.
			disk, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var committed State
			if err := json.Unmarshal(disk, &committed); err != nil {
				t.Fatal(err)
			}
			edited := "package.json"
			if mode == "added-empty" {
				edited = "side/package.json"
			}
			rec := lookupState(committed.Files, edited)
			if rec == nil || rec.Hash == "" {
				t.Fatalf("no-publication run did not record %s at all", edited)
			}
			if prior := lookupState(priorState.Files, edited); prior != nil && prior.Hash == rec.Hash {
				t.Fatalf("no-publication run left %s at the bytes it no longer has", edited)
			}
			// Settled means settled: with nothing further touched, the next run
			// has no need to raise, so the digest of the whole committed state
			// comes back unchanged.
			if _, err = r.reconcile(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			again, err := os.ReadFile(filepath.Join(opts.StateDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(disk, again) {
				t.Fatal("a true no-op after the neutral edit still rewrote the committed state")
			}
			sink.armed.Store(false)
			if n := len(sink.CloneRecords()); n != records {
				t.Fatalf("published %d record(s) over the %d from the initial run", n-records, records)
			}
			coldSink := &graphstream.MemorySink{}
			if _, err = Run(context.Background(), configScopeEngine(t, root), root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
				t.Fatal(err)
			}
			applied, cold := NewConsumer(), NewConsumer()
			applyRun(t, applied, &sink.MemorySink)
			applyRun(t, cold, coldSink)
			assertAppliedEqualsCold(t, applied, cold)
		})
	}
}
