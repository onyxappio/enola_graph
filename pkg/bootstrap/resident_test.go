package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestDefaultBuiltinResidentTSContentScope(t *testing.T) {
	root := t.TempDir()
	for p, b := range map[string]string{"package.json": `{"name":"app","dependencies":{"react":"18"}}`, "tsconfig.json": "{}", "README.md": "# Test\n\n[Source](a.ts)", "a.py": "def helper():\n    return 1\n", "main.tf": "resource \"null_resource\" \"example\" {}", "A.swift": "public struct A {}", "a.ts": "export function a(){return 1}", "b.ts": "import {a} from './a';export function b(){return a()}"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(b), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.Repo = root
	eng, err := NewEngineFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "default", Covered: true}); err != nil {
		t.Fatal(err)
	}
	for i, body := range []string{"export function a(){return fetch('/a')}", "export function renamed(){return 1}", "export function a(){return 1}"} {
		os.WriteFile(filepath.Join(root, "a.ts"), []byte(body), 0644)
		res, err := r.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "default", From: uint64(i), Through: uint64(i + 1), Covered: true, Paths: []string{"a.ts"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.Reconciled || res.Work.HashedFiles != 1 || res.Work.InventoryScans != 0 || len(res.Fallbacks) != 0 {
			t.Fatalf("default profile missed narrow path: %+v", res)
		}
		coldSink := &graphstream.MemorySink{}
		if _, err = graphsession.Run(context.Background(), eng.Analysis(), root, coldSink, graphsession.Options{StateDir: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		applied, cold := graphsession.NewConsumer(), graphsession.NewConsumer()
		if err = applied.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		if err = cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		if applied.Canonical() != cold.Canonical() {
			t.Fatalf("default resident graph differs from cold graph iteration=%d\napplied=%s\ncold=%s", i, applied.Canonical(), cold.Canonical())
		}
	}
}
