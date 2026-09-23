package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// deletionShape is one tree plus one mutation that removes sources from it.
type deletionShape struct {
	name   string
	files  map[string]string
	mutate func(t *testing.T, root string)
}

// removeAll is os.RemoveAll with the path spelled out: every deletion in these
// cases names one exact relative path, so a shape can never widen by accident.
func removeAll(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
}

// Deletion has to converge for every shape of what is left behind, not only for
// the sibling-remains case the watch race happens to produce: a directory that
// still owns files, one that is now empty, one nested inside another that is
// also emptied, and a directory that moved rather than vanished. Convergence is
// checked against a cold run under the same owner contract, and then again
// after a run with nothing to do, because a no-op that republished or retired
// anything would leave the second run disagreeing with cold.
func TestDeletionShapesConvergeToCold(t *testing.T) {
	shapes := []deletionShape{
		{
			name: "one child of a module that keeps a sibling",
			files: map[string]string{
				"src/a.ts":      "export const a=1;\n",
				"src/doomed.ts": "export const doomed=1;\n",
			},
			mutate: func(t *testing.T, root string) { removeAll(t, root, "src/doomed.ts") },
		},
		{
			name: "the last child of a module",
			files: map[string]string{
				"src/only.ts":  "export const only=1;\n",
				"keep/kept.ts": "export const kept=1;\n",
			},
			mutate: func(t *testing.T, root string) { removeAll(t, root, "src/only.ts") },
		},
		{
			name: "the last child of a nested module",
			files: map[string]string{
				"src/a.ts":             "export const a=1;\n",
				"src/deep/nested/b.ts": "export const b=1;\n",
			},
			mutate: func(t *testing.T, root string) { removeAll(t, root, "src/deep/nested/b.ts") },
		},
		{
			name: "a whole nested module directory",
			files: map[string]string{
				"src/a.ts":             "export const a=1;\n",
				"src/deep/nested/b.ts": "export const b=1;\n",
				"src/deep/nested/c.ts": "export const c=1;\n",
			},
			mutate: func(t *testing.T, root string) { removeAll(t, root, "src/deep") },
		},
		{
			name: "a module directory renamed",
			files: map[string]string{
				"src/a.ts":     "export const a=1;\n",
				"src/old/x.ts": "export const x=1;\n",
			},
			mutate: func(t *testing.T, root string) {
				if err := os.Rename(filepath.Join(root, "src/old"), filepath.Join(root, "src/new")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "a module file renamed within its directory",
			files: map[string]string{
				"src/a.ts":   "export const a=1;\n",
				"src/old.ts": "export const x=1;\n",
			},
			mutate: func(t *testing.T, root string) {
				if err := os.Rename(filepath.Join(root, "src/old.ts"), filepath.Join(root, "src/new.ts")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, authoritative := range []bool{false, true} {
		contract := "legacy"
		if authoritative {
			contract = "authoritative"
		}
		for _, shape := range shapes {
			t.Run(contract+"/"+shape.name, func(t *testing.T) {
				root := setupTSRepo(t, shape.files)
				eng := testEngine(t, root)
				stateDir := t.TempDir()
				opts := func() Options {
					return Options{StateDir: stateDir, AuthoritativeFiles: authoritative}
				}
				base := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, root, base, opts()); err != nil {
					t.Fatal(err)
				}
				applied := NewConsumer()
				applyRun(t, applied, base)

				shape.mutate(t, root)
				after := &graphstream.MemorySink{}
				res, err := Run(context.Background(), eng, root, after, opts())
				if err != nil {
					t.Fatal(err)
				}
				if res.TargetGeneration <= res.BaseGeneration {
					t.Fatalf("the deletion run did not advance the generation: base=%d target=%d", res.BaseGeneration, res.TargetGeneration)
				}
				applyRun(t, applied, after)
				assertAppliedEqualsCold(t, applied, coldConsumerUnderContract(t, root, authoritative))

				// A run with nothing left to do must publish nothing that moves
				// the stream away from cold.
				noop := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, root, noop, opts()); err != nil {
					t.Fatal(err)
				}
				applyRun(t, applied, noop)
				assertAppliedEqualsCold(t, applied, coldConsumerUnderContract(t, root, authoritative))
			})
		}
	}
}
