package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
)

func graphWrite(t *testing.T, root, p, b string) {
	t.Helper()
	full := filepath.Join(root, p)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(b), 0644); err != nil {
		t.Fatal(err)
	}
}
func graphGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
}
func graphColdEqual(t *testing.T, root string, sink *graphstream.MemorySink) {
	t.Helper()
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err = graphsession.Run(context.Background(), eng.Analysis(), root, cold, graphsession.Options{StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	a, b := graphsession.NewConsumer(), graphsession.NewConsumer()
	if err = a.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if err = b.ApplyRecords(cold.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if a.Canonical() != b.Canonical() {
		t.Fatalf("resident differs from cold\nresident=%s\ncold=%s", a.Canonical(), b.Canonical())
	}
}

func TestGraphPolicyResidentScopeAndLocks(t *testing.T) {
	root := t.TempDir()
	graphGit(t, root, "init", "-q")
	for p, b := range map[string]string{"mcp-arch.yaml": "extractors: [typescript, manifests, mdintent, python, hcl, swift]\nignore: []\ngraph_inputs:\n  exclude: [private/**]\n", "a.ts": "export function a(){return 1}", "a.py": "def helper():\n    return 1\n", "main.tf": "resource \"null_resource\" \"example\" {}", "A.swift": "public struct A {}", "package.json": `{"name":"app","dependencies":{"react":"^18"}}`, "README.md": "[Image](pic.png)", "pic.png": "a", "hidden/.gitignore": "*\n!.gitignore\n", "hidden/a.ts": "export const hidden=1", "hidden/package.json": `{"name":"hidden"}`, "private/tsconfig.json": "invalid", "kept.ts": "export const kept=1"} {
		graphWrite(t, root, p, b)
	}
	graphGit(t, root, "add", "kept.ts")
	graphWrite(t, root, ".gitignore", "kept.ts\n")
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := eng.Analysis().Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range inv.AllNames {
		seen[p] = true
	}
	if seen["hidden/a.ts"] || seen["private/tsconfig.json"] || !seen["kept.ts"] {
		t.Fatalf("inventory=%v", inv.AllNames)
	}
	sink := &graphstream.MemorySink{}
	r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	watermark := uint64(0)
	apply := func(paths ...string) *graphsession.OnlineResult {
		t.Helper()
		b := graphsession.ChangeBatch{Epoch: "test", From: watermark, Through: watermark + 1, Covered: true, Paths: paths}
		watermark++
		res, err := r.ApplyChanges(context.Background(), b)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	apply()
	graphColdEqual(t, root, sink)
	for _, p := range []string{"package-lock.json", "nested/yarn.lock", "node_modules/x.ts", "pic.png", "hidden/a.ts", "hidden/package.json", "private/tsconfig.json"} {
		for _, action := range []string{"add", "edit", "delete"} {
			if action == "delete" {
				os.Remove(filepath.Join(root, p))
			} else {
				graphWrite(t, root, p, action)
			}
			if p == "pic.png" && action == "delete" {
				continue
			}
			n := len(sink.CloneRecords())
			res := apply(p)
			if res.TargetGeneration != res.BaseGeneration || !reflect.DeepEqual(res.Work, graphsession.WorkCounters{}) || len(sink.CloneRecords()) != n {
				t.Fatalf("ignored %s %s did work: %+v", p, action, res)
			}
		}
	}
	// Media membership needs explicit reconciliation; names affect Markdown edges.
	b := graphsession.ChangeBatch{Epoch: "test", From: watermark, Through: watermark + 1, Covered: true, Reconcile: "media removed"}
	watermark++
	if _, err = r.ApplyChanges(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "a.ts", "export function a(){return fetch('/api')}")
	res := apply("a.ts")
	if res.Reconciled || res.Work.HashedFiles != 1 {
		t.Fatalf("content fast path: %+v", res)
	}
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript, manifests, mdintent, python, hcl, swift]\nignore: []\ngraph_inputs:\n  exclude: [private/**, a.ts, kept.ts]\n")
	apply("mcp-arch.yaml")
	graphColdEqual(t, root, sink)
}

func TestGraphFactoryConfigOverrideAndUnsupported(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "a.ts", "export const a=1")
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\nignore: [a.ts]\n")
	other := filepath.Join(t.TempDir(), "explicit.yaml")
	os.WriteFile(other, []byte("extractors: [typescript]\nignore: []\n"), 0644)
	e, err := NewGraphEngine(GraphOptions{Repo: root, ConfigPath: other})
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := e.Analysis().Inventory(root)
	found := false
	for _, p := range inv.Files {
		found = found || p == "a.ts"
	}
	if !found {
		t.Fatal("override not used")
	}
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [go]\nignore: []\n")
	graphWrite(t, root, "go.mod", "module example.com/test\n\ngo 1.24\n")
	graphWrite(t, root, "main.go", "package main\nfunc main() {}\n")
	e, err = NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	_, err = graphsession.Run(context.Background(), e.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err == nil || len(sink.CloneRecords()) != 0 {
		t.Fatalf("unsupported active consumer: err=%v events=%d", err, len(sink.CloneRecords()))
	}
}

func TestGraphRealWatcherFiltersObservedLock(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "a.ts", "export const a=1")
	e, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	r, err := graphsession.OpenSession(context.Background(), e.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := graphsession.NewFileChangeSource(root, nil, 128)
	if err = s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		if _, err = r.ApplyChanges(context.Background(), s.Drain()); err != nil {
			t.Fatal(err)
		}
		if err = s.CoverSessionInputs(r); err != nil {
			t.Fatal(err)
		}
	}
	before := s.ObservedEvents()
	graphWrite(t, root, "package-lock.json", "{}")
	deadline := time.Now().Add(3 * time.Second)
	for s.ObservedEvents() == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.ObservedEvents() == before {
		t.Fatal("no native lock event observed")
	}
	res, err := r.ApplyChanges(context.Background(), s.Drain())
	if err != nil {
		t.Fatal(err)
	}
	if res.TargetGeneration != res.BaseGeneration || !reflect.DeepEqual(res.Work, graphsession.WorkCounters{}) {
		t.Fatalf("lock event did work %+v", res)
	}
}

func TestGraphPolicyStrictLockDeltaAndSemanticMedia(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "a.ts", "export const a=1")
	graphWrite(t, root, "package.json", `{"name":"app","dependencies":{"x":"^1"}}`)
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	opts := graphsession.Options{StateDir: t.TempDir()}
	if _, err = graphsession.Run(context.Background(), eng.Analysis(), root, sink, opts); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"packages":{"node_modules/x":{"version":"1.1"}}}`, `{"packages":{"node_modules/x":{"version":"1.2"}}}`, ""} {
		if body == "" {
			os.Remove(filepath.Join(root, "package-lock.json"))
		} else {
			graphWrite(t, root, "package-lock.json", body)
		}
		before := len(sink.CloneRecords())
		res, err := graphsession.Run(context.Background(), eng.Analysis(), root, sink, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.ParsedFiles != 0 || res.TargetGeneration != res.BaseGeneration || len(sink.CloneRecords()) != before {
			t.Fatalf("strict lock edit changed graph: %+v", res)
		}
		graphColdEqual(t, root, sink)
	}
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\nignore: []\ngraph_inputs:\n  semantic: ['**/*.png']\n")
	graphWrite(t, root, "pic.png", "first")
	eng, err = NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, &graphstream.MemorySink{}, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "media", Covered: true}); err != nil {
		t.Fatal(err)
	}
	graphWrite(t, root, "pic.png", "changed")
	res, err := r.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "media", Covered: true, Through: 1, Paths: []string{"pic.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reconciled {
		t.Fatal("explicit semantic media edit ignored")
	}
}

func TestGraphRealWatcherPolicyScopeChange(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "a.ts", "export const a=1")
	graphWrite(t, root, ".gitignore", "")
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	source := graphsession.NewGraphFileChangeSource(eng.Analysis(), root, nil, 128)
	if err = source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	settle := func() {
		t.Helper()
		for i := 0; i < 3; i++ {
			if _, err := r.ApplyChanges(context.Background(), source.Drain()); err != nil {
				t.Fatal(err)
			}
			if err = source.CoverSessionInputs(r); err != nil {
				t.Fatal(err)
			}
		}
	}
	settle()
	path := filepath.Join(root, ".gitignore")
	before := source.ObservedPath(path)
	graphWrite(t, root, ".gitignore", "a.ts\n")
	deadline := time.Now().Add(3 * time.Second)
	for source.ObservedPath(path) <= before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if source.ObservedPath(path) <= before {
		t.Fatal("no policy event observed")
	}
	settle()
	graphColdEqual(t, root, sink)
}

func TestGraphFactoryInitialConfigurationChange(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "a.ts", "export const a=1")
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\nignore: []\n")
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\nignore: [a.ts]\n")
	sink := &graphstream.MemorySink{}
	r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "initial", Covered: true}); err != nil {
		t.Fatal(err)
	}
	graphColdEqual(t, root, sink)
}

func TestGraphExplicitDependencyNativeEvents(t *testing.T) {
	for _, kind := range []string{"external-tsconfig", "swift-media-include"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript, swift]\nignore: []\n")
			var dependency, changed string
			if kind == "external-tsconfig" {
				dependency = filepath.Join(t.TempDir(), "base.json")
				graphWrite(t, root, "a.ts", "export function target(){return 1}")
				graphWrite(t, root, "b.ts", "export function target(){return 2}")
				graphWrite(t, root, "main.ts", "import {target} from '@target';export function main(){return target()}")
				data, _ := json.Marshal(map[string]string{"extends": dependency})
				graphWrite(t, root, "tsconfig.json", string(data))
				initial := fmt.Sprintf(`{"compilerOptions":{"baseUrl":%q,"paths":{"@target":["a.ts"]}}}`, root)
				changed = fmt.Sprintf(`{"compilerOptions":{"baseUrl":%q,"paths":{"@target":["b.ts"]}}}`, root)
				if err := os.WriteFile(dependency, []byte(initial), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				graphWrite(t, root, "App/A.swift", "public struct A {}")
				graphWrite(t, root, "project.yml", "include: [targets.png]\n")
				graphWrite(t, root, "targets.png", "targets: {Before: {type: framework, sources: [App]}}\n")
				dependency = filepath.Join(root, "targets.png")
				changed = "targets: {After: {type: framework, sources: [App]}}\n"
			}
			eng, err := NewGraphEngine(GraphOptions{Repo: root})
			if err != nil {
				t.Fatal(err)
			}
			sink := &graphstream.MemorySink{}
			r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			source := graphsession.NewGraphFileChangeSource(eng.Analysis(), root, nil, 128)
			if err = source.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			for i := 0; i < 3; i++ {
				if _, err = r.ApplyChanges(context.Background(), source.Drain()); err != nil {
					t.Fatal(err)
				}
				if err = source.CoverSessionInputs(r); err != nil {
					t.Fatal(err)
				}
			}
			before := r.Snapshot()
			observed := source.ObservedPath(dependency)
			if err = os.WriteFile(dependency, []byte(changed), 0644); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for source.ObservedPath(dependency) <= observed && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if source.ObservedPath(dependency) <= observed {
				t.Fatal("no dependency event observed")
			}
			res, err := r.ApplyChanges(context.Background(), source.Drain())
			if err != nil {
				t.Fatal(err)
			}
			if !res.Reconciled || res.TargetGeneration == res.BaseGeneration {
				t.Fatalf("explicit content dependency ignored: %+v", res)
			}
			if reflect.DeepEqual(before, r.Snapshot()) {
				t.Fatal("declared dependency did not update snapshot")
			}
			graphColdEqual(t, root, sink)
		})
	}
}
