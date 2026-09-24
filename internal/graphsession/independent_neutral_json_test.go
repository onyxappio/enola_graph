package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentNeutralJSONMembershipLifecycle(t *testing.T) {
	for _, action := range []string{"add", "edit", "rename", "delete"} {
		t.Run(action, func(t *testing.T) {
			files := map[string]string{"src/base.ts": "export const base=1;\n", "docs/readme.md": "# Guide\n\nPlain text.\n"}
			if action != "add" {
				files["docs/unused.json"] = "{}\n"
			}
			root := setupTSRepo(t, files)
			eng := multiEngine(t, root, mdintent.New())
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			ctx := context.Background()
			initial := &graphstream.MemorySink{}
			before, err := Run(ctx, eng, root, initial, opts)
			if err != nil {
				t.Fatal(err)
			}
			cons := NewConsumer()
			applyRun(t, cons, initial)
			switch action {
			case "add", "edit":
				writeRepoFile(t, root, "docs/unused.json", "{\"note\":\"unused\"}\n")
			case "rename":
				if err := os.Rename(filepath.Join(root, "docs/unused.json"), filepath.Join(root, "docs/renamed.json")); err != nil {
					t.Fatal(err)
				}
			case "delete":
				if err := os.Remove(filepath.Join(root, "docs/unused.json")); err != nil {
					t.Fatal(err)
				}
			}
			sink := &graphstream.MemorySink{}
			result, err := Run(ctx, eng, root, sink, opts)
			if err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, sink)
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
			if len(sink.CloneRecords()) != 0 || result.ParsedFiles != 0 || result.TargetGeneration != before.TargetGeneration {
				t.Errorf("neutral %s: events=%d parsed=%d gen=%d->%d fallback=%v", action, len(sink.CloneRecords()), result.ParsedFiles, before.TargetGeneration, result.TargetGeneration, result.Fallbacks)
			}
			quiet := &graphstream.MemorySink{}
			noop, err := Run(ctx, eng, root, quiet, opts)
			if err != nil {
				t.Fatal(err)
			}
			if noop.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 || noop.TargetGeneration != result.TargetGeneration {
				t.Fatalf("followup noop parsed=%d events=%d generation=%d->%d", noop.ParsedFiles, len(quiet.CloneRecords()), result.TargetGeneration, noop.TargetGeneration)
			}
		})
	}
}
