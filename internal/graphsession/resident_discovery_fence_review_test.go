package graphsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// Only configuration changes inside the hook. Source membership stays fixed,
// so source-inventory validation cannot mask a missing configuration fence.
func TestIndependentScopedResidentConfigMutationFence(t *testing.T) {
	for _, mode := range []string{"retarget-existing", "new-nested-config"} {
		t.Run(mode, func(t *testing.T) {
			var mutate func()
			root, r, q, sink := independentScopedFenceFixture(t, map[string]string{
				"package.json":    `{"name":"app"}`,
				"tsconfig.json":   `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["one/*"]}}}`,
				"one/target.ts":   "export const target = 1;\n",
				"two/target.ts":   "export const target = 2;\n",
				"src/consumer.ts": "import {target} from '@app/target';\nfunction helper(){return 1;}\nexport function use(){return target;}\n",
			}, Options{AuthoritativeFiles: true, OnBeforeParse: func(string) {
				if mutate != nil {
					f := mutate
					mutate = nil
					f()
				}
			}})
			defer q.Close()
			before, err := json.Marshal(r.state)
			if err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, ".enola", "resident", "state.json")
			diskBefore, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			recordCount := len(sink.CloneRecords())
			changedConfig := "tsconfig.json"
			if mode == "new-nested-config" {
				changedConfig = "src/tsconfig.json"
			}
			called := false
			mutate = func() {
				called = true
				config := `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["two/*"]}}}`
				if mode == "new-nested-config" {
					config = `{"compilerOptions":{"baseUrl":"..","paths":{"@app/*":["two/*"]}}}`
				}
				writeRepoFile(t, root, changedConfig, config)
			}
			writeRepoFile(t, root, "src/consumer.ts", "import {target} from '@app/target';\nfunction helper(){return 1;}\nexport function use(){return target + helper();}\n")
			q.Add("src/consumer.ts")
			_, err = r.ApplyChanges(context.Background(), q.Drain())
			if !called {
				t.Fatal("configuration mutation hook did not execute")
			}
			if !errors.Is(err, ErrInputsChanged) {
				if err == nil {
					observed := NewConsumer()
					applyRun(t, observed, sink)
					fresh := coldConsumer(t, configScopeEngine(t, root), root)
					t.Logf("successful run cold-equal=%v", observed.Canonical() == fresh.Canonical())
				}
				t.Fatalf("mid-run configuration mutation must refuse completion with ErrInputsChanged, got %v", err)
			}
			t.Logf("fence: %v", err)
			after, _ := json.Marshal(r.state)
			if !bytes.Equal(before, after) {
				t.Fatal("failed run changed resident state")
			}
			diskAfter, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(diskBefore, diskAfter) {
				t.Fatal("failed run changed completed disk state")
			}
			_, _, ends, err := DecodeRun(sink.CloneRecords()[recordCount:])
			if err != nil {
				t.Fatal(err)
			}
			for _, end := range ends {
				if end.Completeness.Status == "success" {
					t.Fatal("failed run published successful End")
				}
			}
			q.Add(changedConfig)
			if _, err = r.ApplyChanges(context.Background(), q.Drain()); err != nil {
				t.Fatal(err)
			}
			applied := NewConsumer()
			applyRun(t, applied, sink)
			coldSink := &graphstream.MemorySink{}
			if _, err = Run(context.Background(), configScopeEngine(t, root), root, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
				t.Fatal(err)
			}
			cold := NewConsumer()
			applyRun(t, cold, coldSink)
			assertAppliedEqualsCold(t, applied, cold)
			generation := r.state.Generation
			records := len(sink.CloneRecords())
			q.Add(changedConfig)
			res, err := r.ApplyChanges(context.Background(), q.Drain())
			if err != nil {
				t.Fatal(err)
			}
			if res.ParsedFiles != 0 || r.state.Generation != generation || len(sink.CloneRecords()) != records {
				t.Fatal("settled retry did not become silent")
			}
		})
	}
}

func independentScopedFenceFixture(t *testing.T, files map[string]string, opts Options) (string, *Resident, *ChangeQueue, *graphstream.MemorySink) {
	t.Helper()
	root := setupTSRepo(t, files)
	sink := &graphstream.MemorySink{}
	opts.StateDir = filepath.Join(root, ".enola", "resident")
	r, err := OpenSession(context.Background(), configScopeEngine(t, root), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	q := NewChangeQueue("scoped-review", 32)
	if err = q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyChanges(context.Background(), q.Drain()); err != nil {
		t.Fatal(err)
	}
	return root, r, q, sink
}
