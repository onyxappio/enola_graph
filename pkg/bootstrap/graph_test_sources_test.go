package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestGraphTestSourcesInitialDeltaAndExclusion(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\n")
	graphWrite(t, root, "source.ts", "export function answer(){ return 42; }")
	graphWrite(t, root, "source.test.ts", "import {answer} from './source'; export function testAnswer(){ return answer(); }")
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	s, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	var watermark uint64
	apply := func(paths ...string) *graphsession.OnlineResult {
		t.Helper()
		watermark++
		r, err := s.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "test-source", From: watermark - 1, Through: watermark, Covered: true, Paths: paths})
		if err != nil {
			t.Fatal(err)
		}
		coldEngine, err := NewGraphEngine(GraphOptions{Repo: root})
		if err != nil {
			t.Fatal(err)
		}
		coldSink := &graphstream.MemorySink{}
		if _, err := graphsession.Run(context.Background(), coldEngine.Analysis(), root, coldSink, graphsession.Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
			t.Fatal(err)
		}
		actual, cold := graphsession.NewConsumer(), graphsession.NewConsumer()
		if err := actual.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		if err := cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		if actual.Canonical() != cold.Canonical() {
			t.Fatal("authoritative delta differs from authoritative cold graph")
		}
		return r
	}
	canonical := func() string {
		t.Helper()
		c := graphsession.NewConsumer()
		if err := c.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
		return c.Canonical()
	}
	apply()
	first := canonical()
	if !strings.Contains(first, "testAnswer") || !strings.Contains(first, "source.test.ts") {
		t.Fatal("test function absent from graph")
	}
	var owners []struct {
		Owner string `json:"owner"`
		Edges []struct {
			Kind       string `json:"kind"`
			Target     string `json:"target_name"`
			Resolution string `json:"resolution"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(first), &owners); err != nil {
		t.Fatal(err)
	}
	resolvedCall := false
	for _, owner := range owners {
		if owner.Owner == "file:source.test.ts" {
			for _, edge := range owner.Edges {
				if edge.Kind == "calls" && strings.HasSuffix(edge.Target, ".answer") && edge.Resolution == "resolved" {
					resolvedCall = true
				}
			}
		}
	}
	if !resolvedCall {
		t.Fatal("test-to-source call missing from graph")
	}
	graphWrite(t, root, "source.test.ts", "import {answer} from './source'; export function testChanged(){ return answer()+1; }")
	if r := apply("source.test.ts"); r.ParsedFiles == 0 {
		t.Fatal("test edit was not parsed")
	}
	if got := canonical(); !strings.Contains(got, "testChanged") || strings.Contains(got, "testAnswer") {
		t.Fatal("test edit left stale graph")
	}
	n := len(sink.CloneRecords())
	r := apply("source.test.ts")
	if r.ParsedFiles != 0 || r.BaseGeneration != r.TargetGeneration || len(sink.CloneRecords()) != n {
		t.Fatal("unchanged test published work")
	}
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\ngraph_inputs:\n  exclude: ['**/*.test.ts']\n")
	apply("mcp-arch.yaml")
	if strings.Contains(canonical(), "testChanged") {
		t.Fatal("explicit exclusion did not retire test contribution")
	}
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\n")
	apply("mcp-arch.yaml")
	if !strings.Contains(canonical(), "testChanged") {
		t.Fatal("removing exclusion did not restore test contribution")
	}
	if err := os.Remove(filepath.Join(root, "source.test.ts")); err != nil {
		t.Fatal(err)
	}
	apply("source.test.ts")
	if strings.Contains(canonical(), "testChanged") {
		t.Fatal("deleted test contribution remains")
	}
}

func TestGraphDefaultInventoryIncludesTestLanguages(t *testing.T) {
	root := t.TempDir()
	names := []string{"x_test.go", "x.test.ts", "x.spec.tsx", "tests/test_x.py", "spec/x_spec.rb", "tests/X.cs", "src/test/scala/X.scala", "test/x_test.dart"}
	for _, name := range names {
		graphWrite(t, root, name, "")
	}
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := eng.Analysis().Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range inv.Files {
		seen[name] = true
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("test source omitted: %s", name)
		}
	}
}

func TestGraphDefaultTestSourcesRespectSupportedExtractors(t *testing.T) {
	for _, tc := range []struct{ name, extractor, path, body, want string }{
		{"go", "go", "answer_test.go", "package app\nimport \"testing\"\nfunc TestAnswer(t *testing.T) { t.Log(42) }\n", "TestAnswer"},
		{"python", "python", "test_answer.py", "def test_answer():\n    assert 42 == 42\n", "test_answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			graphWrite(t, root, "mcp-arch.yaml", "extractors: ["+tc.extractor+"]\n")
			if tc.name == "go" {
				graphWrite(t, root, "go.mod", "module example.test/app\ngo 1.24\n")
			}
			graphWrite(t, root, tc.path, tc.body)
			eng, err := NewGraphEngine(GraphOptions{Repo: root})
			if err != nil {
				t.Fatal(err)
			}
			sink := &graphstream.MemorySink{}
			_, runErr := graphsession.Run(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
			if tc.name == "go" {
				// Go already fails closed in the graph profile because its side
				// inputs have not been audited. Including tests must not bypass it.
				if runErr == nil || !strings.Contains(runErr.Error(), "active extractor go has unaudited side inputs") {
					t.Fatalf("unsupported extractor gate changed: %v", runErr)
				}
				return
			}
			if runErr != nil {
				t.Fatal(runErr)
			}
			c := graphsession.NewConsumer()
			if err := c.ApplyRecords(sink.CloneRecords()); err != nil {
				t.Fatal(err)
			}
			if got := c.Canonical(); !strings.Contains(got, tc.want) || !strings.Contains(got, tc.path) {
				t.Fatalf("test source missing: %s", got)
			}
		})
	}
}

func TestGraphTestScopeMigratesExistingCheckpoint(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\n")
	graphWrite(t, root, "x.ts", "export const x=1;")
	graphWrite(t, root, "x.test.ts", "import {x} from './x'; export function testX(){return x;}")
	// Construct the old graph defaults with the same unchanged repository config.
	cfg, err := config.Load(filepath.Join(root, "mcp-arch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repo = root
	old, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := graphinput.Build(root, cfg.GraphInputOptions())
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	registerOSSPlugins(old, cfg, scope)
	old.ConfigureGraphInputs(scope, nil)
	sink := &graphstream.MemorySink{}
	opts := graphsession.Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	if _, err := graphsession.Run(context.Background(), old, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	before := graphsession.NewConsumer()
	if err := before.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before.Canonical(), "testX") {
		t.Fatal("old fixture already includes tests")
	}
	current, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graphsession.Run(context.Background(), current.Analysis(), root, sink, opts); err != nil {
		t.Fatal(err)
	}
	after := graphsession.NewConsumer()
	if err := after.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after.Canonical(), "testX") {
		t.Fatal("upgraded checkpoint missed test source")
	}
	coldSink := &graphstream.MemorySink{}
	if _, err := graphsession.Run(context.Background(), current.Analysis(), root, coldSink, graphsession.Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
		t.Fatal(err)
	}
	cold := graphsession.NewConsumer()
	if err := cold.ApplyRecords(coldSink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	if after.Canonical() != cold.Canonical() {
		t.Fatal("upgraded checkpoint differs from test-inclusive cold graph")
	}
	n := len(sink.CloneRecords())
	res, err := graphsession.Run(context.Background(), current.Analysis(), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles != 0 || len(sink.CloneRecords()) != n {
		t.Fatal("post-migration no-op did work")
	}
}
