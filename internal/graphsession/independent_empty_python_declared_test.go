package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

type independentDeclaredOwnerless struct{ names bool }

func (e independentDeclaredOwnerless) Name() string                { return "declared-ownerless" }
func (e independentDeclaredOwnerless) Detect(string) (bool, error) { return true, nil }
func (e independentDeclaredOwnerless) ContentInput(p string) bool  { return p == "input.txt" }
func (e independentDeclaredOwnerless) NameSetInput() bool          { return e.names }
func (e independentDeclaredOwnerless) Extract(_ context.Context, root string, files []string) ([]facts.Fact, error) {
	b, err := os.ReadFile(filepath.Join(root, "input.txt"))
	if err != nil {
		return nil, err
	}
	name := string(b)
	if e.names {
		paths := append([]string(nil), files...)
		sort.Strings(paths)
		name += strings.Join(paths, ",")
	}
	return []facts.Fact{{Kind: facts.KindSymbol, Name: name, File: "base.ts"}}, nil
}
func TestIndependentEmptyOwnershipStillChecksDeclaredInputs(t *testing.T) {
	for _, names := range []bool{false, true} {
		label := "content"
		if names {
			label = "names"
		}
		t.Run(label, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"base.ts": "export const base=1;\n", "input.txt": "before"})
			eng := multiEngine(t, root, independentDeclaredOwnerless{names: names})
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			cons := NewConsumer()
			initial := &graphstream.MemorySink{}
			first, err := Run(context.Background(), eng, root, initial, opts)
			if err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, initial)
			before := cons.Canonical()
			if names {
				writeFile(t, root, "new.ts", "export const added=1;\n")
			} else {
				writeRepoFile(t, root, "input.txt", "after")
			}
			changed := &graphstream.MemorySink{}
			result, err := Run(context.Background(), eng, root, changed, opts)
			if err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, changed)
			if result.TargetGeneration == first.TargetGeneration || cons.Canonical() == before {
				t.Fatal("declared input change did not update graph")
			}
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
			quiet := &graphstream.MemorySink{}
			n, err := Run(context.Background(), eng, root, quiet, opts)
			if err != nil {
				t.Fatal(err)
			}
			if n.TargetGeneration != result.TargetGeneration || n.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 {
				t.Fatalf("followup not quiet: %+v", n)
			}
		})
	}
}
