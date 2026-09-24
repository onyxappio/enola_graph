package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/extractors/pythonextractor"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentMDSiblingModuleSequence(t *testing.T) {
	for _, lang := range []string{"ts", "py"} {
		t.Run(lang, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"src/base.ts": "export const base=1;\n", "docs/readme.md": "# Guide\n\nSome text.\n", "docs/empty.json": "{}\n"})
			eng := multiEngine(t, root, mdintent.New(), pythonextractor.New())
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			cons := NewConsumer()
			run := func() {
				t.Helper()
				sink := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
					t.Fatal(err)
				}
				applyRun(t, cons, sink)
				assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
			}
			run()
			body := "export function work(){return 1}\n"
			if lang == "py" {
				body = "def work():\n    return 1\n"
			}
			file := "docs/module." + lang
			writeRepoFile(t, root, file, body)
			run()
			if err := os.Rename(filepath.Join(root, file), filepath.Join(root, "docs/renamed."+lang)); err != nil {
				t.Fatal(err)
			}
			run()
			if err := os.Remove(filepath.Join(root, "docs/renamed."+lang)); err != nil {
				t.Fatal(err)
			}
			run()
		})
	}
}

func TestIndependentResidentMDSiblingModuleSequence(t *testing.T) {
	for _, lang := range []string{"ts", "py"} {
		t.Run(lang, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{"src/base.ts": "export const base=1;\n", "docs/readme.md": "# Guide\n\nSome text.\n", "docs/empty.json": "{}\n"})
			eng := multiEngine(t, root, mdintent.New(), pythonextractor.New())
			sink := &graphstream.MemorySink{}
			resident, err := OpenSession(context.Background(), eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
			if err != nil {
				t.Fatal(err)
			}
			defer resident.Close()
			q := NewChangeQueue("independent-md-siblings", 32)
			if err := q.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer q.Close()
			run := func(paths ...string) {
				t.Helper()
				for _, p := range paths {
					q.Add(p)
				}
				if _, err := resident.ApplyChanges(context.Background(), q.Drain()); err != nil {
					t.Fatal(err)
				}
				cons := NewConsumer()
				applyRun(t, cons, sink)
				assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
				lastBeginScope(t, sink)
			}
			run()
			file := "docs/module." + lang
			renamed := "docs/renamed." + lang
			body := "export function work(){return 1}\n"
			if lang == "py" {
				body = "def work():\n    return 1\n"
			}
			writeRepoFile(t, root, file, body)
			run(file)
			if err := os.Rename(filepath.Join(root, file), filepath.Join(root, renamed)); err != nil {
				t.Fatal(err)
			}
			run(file, renamed)
			if err := os.Remove(filepath.Join(root, renamed)); err != nil {
				t.Fatal(err)
			}
			run(renamed)
			generation, events := resident.state.Generation, len(sink.CloneRecords())
			res, err := resident.ApplyChanges(context.Background(), q.Drain())
			if err != nil {
				t.Fatal(err)
			}
			if res.ParsedFiles != 0 || resident.state.Generation != generation || len(sink.CloneRecords()) != events {
				t.Fatal("resident no-op did work")
			}
		})
	}
}
