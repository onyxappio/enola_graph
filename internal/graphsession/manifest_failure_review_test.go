package graphsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
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
func TestIndependentManifestHashRefreshFailedEndRollback(t *testing.T) {
	for _, mode := range []string{"version", "added-empty"} {
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
			if mode == "version" {
				writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.1","dependencies":{"some-lib":"^1.0.0"}}`)
			} else {
				if err := os.MkdirAll(filepath.Join(root, "side"), 0755); err != nil {
					t.Fatal(err)
				}
				writeRepoFile(t, root, "side/package.json", `{"name":"side","version":"1.0.0"}`)
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
