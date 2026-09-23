package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

// Both sides must use identical protocol semantics: v1 synthetic modules are
// intentionally absent from the authoritative file-owner profile.
func TestIndependentDeletionColdSameProtocol(t *testing.T) {
	for _, authoritative := range []bool{false, true} {
		name := "legacy"
		if authoritative {
			name = "authoritative"
		}
		t.Run(name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"src/a.ts": "export const a=1;\n", "src/b.ts": "export const b=2;\n"})
			eng := testEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative}
			applied := NewConsumer()
			run := func() *Result {
				t.Helper()
				sink := &graphstream.MemorySink{}
				res, err := Run(context.Background(), eng, root, sink, opts)
				if err != nil {
					t.Fatal(err)
				}
				if err = applied.ApplyRecords(sink.CloneRecords()); err != nil {
					t.Fatal(err)
				}
				return res
			}
			run()
			for _, path := range []string{"src/b.ts", "src/a.ts"} {
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				res := run()
				sink := &graphstream.MemorySink{}
				_, err := Run(context.Background(), testEngine(t, root), root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative})
				if err != nil {
					t.Fatal(err)
				}
				cold := NewConsumer()
				if err = cold.ApplyRecords(sink.CloneRecords()); err != nil {
					t.Fatal(err)
				}
				assertAppliedEqualsCold(t, applied, cold)
				noop := &graphstream.MemorySink{}
				nr, err := Run(context.Background(), eng, root, noop, opts)
				if err != nil {
					t.Fatal(err)
				}
				assertNoPublication(t, nr, noop, res.TargetGeneration, "same-mode deletion followed by no-op")
			}
		})
	}
}
