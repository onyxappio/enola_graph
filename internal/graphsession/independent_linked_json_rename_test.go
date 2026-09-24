package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentUnclaimedRenameAcrossMarkdownReference(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":      "export const base=1;\n",
		"docs/readme.md":   "# Guide\n\n[Configuration](./linked.json)\n",
		"docs/unused.json": "{}\n",
	})
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	for _, step := range [][2]string{{"unused.json", "linked.json"}, {"linked.json", "renamed.json"}, {"renamed.json", "linked.json"}} {
		before := cons.Canonical()
		if err := os.Rename(filepath.Join(root, "docs", step[0]), filepath.Join(root, "docs", step[1])); err != nil {
			t.Fatal(err)
		}
		result, scope, _ := configScopeRun(t, eng, root, opts, cons)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
		if before == cons.Canonical() {
			t.Fatalf("rename %s to %s must alter linked Markdown graph", step[0], step[1])
		}
		if !scope["docs/readme.md"] {
			t.Fatalf("changed referencing page absent from scope: %v", scope)
		}
		sink := &graphstream.MemorySink{}
		quiet, err := Run(context.Background(), eng, root, sink, opts)
		if err != nil {
			t.Fatal(err)
		}
		if quiet.TargetGeneration != result.TargetGeneration || quiet.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 {
			t.Fatalf("followup must be quiet: generation %d->%d events=%d", result.TargetGeneration, quiet.TargetGeneration, len(sink.CloneRecords()))
		}
	}
}
