package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
)

func scopedFixture(t *testing.T) (string, *graphsession.Resident, *graphstream.MemorySink, func(...string) *graphsession.OnlineResult) {
	t.Helper()
	root := t.TempDir()
	graphGit(t, root, "init", "-q")
	for p, b := range map[string]string{
		"mcp-arch.yaml": "extractors: [typescript, manifests, mdintent, hcl, python, swift]\nignore: []\n",
		"package.json":  `{"name":"app","dependencies":{"react":"18"}}`, "tsconfig.json": "{}",
		"packages/clickhouse/package.json": `{"name":"clickhouse","devDependencies":{"helper":"1"}}`,
		"packages/database/package.json":   `{"name":"database","dependencies":{"helper":"1"}}`,
		"a.ts":                             "export function a(){return 1}", "b.ts": "import {a} from './a';export function b(){return a()}", "unrelated.ts": "export const separate=42",
		"packages/clickhouse/index.ts": "export const clickhouse=1", "packages/database/index.ts": "export const database=1",
		"README.md": "# Readme", "main.tf": "resource \"null_resource\" \"sample\" {}", "helper.py": "def helper():\n    return 1\n", "A.swift": "public struct A {}",
	} {
		graphWrite(t, root, p, b)
	}
	graphGit(t, root, "add", ".")
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	r, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, graphsession.Options{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	watermark := uint64(0)
	apply := func(paths ...string) *graphsession.OnlineResult {
		t.Helper()
		res, err := r.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "scoped", From: watermark, Through: watermark + 1, Covered: true, Paths: paths})
		watermark++
		if err != nil {
			t.Fatal(err)
		}
		sum := 0
		for _, n := range res.Invalidation.ParsedByReason {
			sum += n
		}
		if sum != res.ParsedFiles {
			t.Fatalf("parse reason counts %d != parsed %d: %+v", sum, res.ParsedFiles, res.Invalidation)
		}
		return res
	}
	apply()
	return root, r, sink, apply
}

func TestScopedPackageReadersStableContext(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	for _, step := range []struct {
		path, body             string
		graphChange, tsContext bool
	}{
		{"package.json", `{"name":"app","dependencies":{"react":"18"},"scripts":{"test":"echo changed"}}`, false, false},
		{"package.json", "{\n \"scripts\": {\"test\": \"echo changed\"},\n \"dependencies\": {\"react\":\"18\"}, \"name\":\"app\"\n}\n", false, false},
		{"packages/clickhouse/package.json", `{"name":"clickhouse","devDependencies":{"helper":"1","drizzle-orm":"catalog:"}}`, true, true},
		{"packages/database/package.json", `{"name":"database","dependencies":{"helper":"1","@onyx/contracts":"workspace:*"},"scripts":{"db:sync-payment-offers":"tsx src/paymentOfferSyncCli.ts"}}`, true, false},
	} {
		graphWrite(t, root, step.path, step.body)
		before := len(sink.CloneRecords())
		res := apply(step.path)
		if !step.tsContext && (res.ParsedFiles != 0 || len(res.Invalidation.ContextReasons) != 0) {
			t.Fatalf("stable readers reparsed TS: %+v", res)
		}
		if step.tsContext && res.ParsedFiles == 0 {
			t.Fatalf("owning-package ORM change must reparse: %+v", res)
		}
		for _, f := range res.Fallbacks {
			if f.Extractor == "manifests" {
				continue
			}
			if step.tsContext && f.Extractor == "typescript" {
				continue
			}
			t.Fatalf("unrelated fallback %+v", f)
		}
		if step.graphChange != (res.TargetGeneration > res.BaseGeneration) {
			t.Fatalf("manifest graph change=%v result=%+v", step.graphChange, res)
		}
		if !step.graphChange && len(sink.CloneRecords()) != before {
			t.Fatal("metadata-only package emitted events")
		}
		graphColdEqual(t, root, sink)
	}
}

func TestScopedSemanticChangesRemainConservative(t *testing.T) {
	for _, scenario := range []struct{ name, path, body string }{
		{"root-framework", "package.json", `{"name":"app","dependencies":{"react":"18","next":"15"}}`},
		{"root-orm", "package.json", `{"name":"app","dependencies":{"react":"18","drizzle-orm":"1"}}`},
		{"nested-angular", "packages/database/package.json", `{"name":"database","dependencies":{"@angular/core":"20"}}`},
		{"nested-ember", "packages/database/package.json", `{"name":"database","dependencies":{"ember-source":"6"}}`},
		{"package-name", "packages/database/package.json", `{"name":"renamed","dependencies":{"helper":"1"}}`},
		{"aliases", "tsconfig.json", `{"compilerOptions":{"paths":{"@db":["./packages/database/index.ts"]}}}`},
		{"malformed-package", "package.json", `{"name":`},
		{"malformed-tsconfig", "tsconfig.json", `{"compilerOptions":`},
		{"unsupported-package-extends", "tsconfig.json", `{"extends":"expo/tsconfig.base"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root, _, sink, apply := scopedFixture(t)
			graphWrite(t, root, scenario.path, scenario.body)
			res := apply(scenario.path)
			if res.ParsedFiles == 0 || (len(res.Invalidation.ContextReasons) == 0 && res.Invalidation.ContextAffectedSources == 0) {
				t.Fatalf("semantic reader change reused context: %+v", res)
			}
			graphColdEqual(t, root, sink)
		})
	}
}

func TestScopedTrackedMembershipAndResolution(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	graphWrite(t, root, "added.ts", "export const fresh=1")
	graphGit(t, root, "add", "added.ts")
	res := apply("added.ts")
	// Staging a source Git does not ignore moves the index but no admission
	// decision, so this is a membership addition and not a policy change: the
	// one new file parses without the policy being reconciled around it.
	if res.ParsedFiles != 1 || res.Invalidation.PolicyReconciled || len(res.Invalidation.ContextReasons) != 0 {
		t.Fatalf("tracked source forced global parse: %+v", res)
	}
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "barrel.ts", "export {a} from './a'")
	graphWrite(t, root, "b.ts", "import {a} from './barrel';export function b(){return a()}")
	apply("barrel.ts", "b.ts")
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "a.ts", "export function renamed(){return 2}")
	graphWrite(t, root, "barrel.ts", "export {renamed} from './a'")
	apply("a.ts", "barrel.ts")
	graphColdEqual(t, root, sink)
	if err := os.Rename(filepath.Join(root, "a.ts"), filepath.Join(root, "renamed.ts")); err != nil {
		t.Fatal(err)
	}
	graphWrite(t, root, "barrel.ts", "export {renamed} from './renamed'")
	apply("a.ts", "renamed.ts", "barrel.ts")
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript, manifests, mdintent, hcl, python, swift]\nignore: []\ngraph_inputs:\n  exclude: [unrelated.ts]\n")
	res = apply("mcp-arch.yaml")
	if len(res.Invalidation.ContextReasons) != 0 {
		t.Fatalf("source exclusion changed parser context %+v", res)
	}
	graphColdEqual(t, root, sink)
}

func TestScopedRawPackageCaptureStillFenced(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "package.json", `{"name":"app","dependencies":{"react":"18"}}`)
	graphWrite(t, root, "a.ts", "export const a=1")
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	opts := graphsession.Options{StateDir: t.TempDir()}
	if _, err = graphsession.Run(context.Background(), eng.Analysis(), root, sink, opts); err != nil {
		t.Fatal(err)
	}
	graphWrite(t, root, "package.json", `{"name":"app","dependencies":{"react":"18"},"scripts":{"test":"first"}}`)
	graphWrite(t, root, "a.ts", "export const a=2")
	opts.OnBeforeParse = func(string) {
		graphWrite(t, root, "package.json", `{"name":"app","dependencies":{"react":"18"},"scripts":{"test":"late"}}`)
	}
	_, err = graphsession.Run(context.Background(), eng.Analysis(), root, sink, opts)
	if !errors.Is(err, graphsession.ErrInputsChanged) {
		t.Fatalf("raw package edit not fenced: %v", err)
	}
	opts.OnBeforeParse = nil
	if _, err = graphsession.Run(context.Background(), eng.Analysis(), root, sink, opts); err != nil {
		t.Fatal(err)
	}
	graphColdEqual(t, root, sink)
}

func TestScopedDiscoveryCacheMigration(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [manifests, mdintent]\nignore: []\n")
	graphWrite(t, root, ".app/package.json", `{"name":"hidden","dependencies":{"newly-seen":"^1"}}`)
	graphWrite(t, root, "build/README.md", "# Previously omitted")
	graph, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := NewEngineFromConfig(graph.Config())
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	opts := graphsession.Options{StateDir: t.TempDir()}
	before, err := graphsession.Run(context.Background(), legacy.Analysis(), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Facts) != 0 {
		t.Fatal("legacy discovery fixture unexpectedly emitted facts")
	}
	statePath := filepath.Join(opts.StateDir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["extractor_version"] = "v273"
	data, _ = json.Marshal(state)
	if err = os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	after, err := graphsession.Run(context.Background(), graph.Analysis(), root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Facts) == 0 || after.TargetGeneration <= after.BaseGeneration {
		t.Fatalf("old checkpoint not rebuilt %+v", after)
	}
	graphColdEqual(t, root, sink)
}

func TestScopedNativeGitMetadataSettles(t *testing.T) {
	root, r, sink, _ := scopedFixture(t)
	eng, err := NewGraphEngine(GraphOptions{Repo: root})
	if err != nil {
		t.Fatal(err)
	}
	source := graphsession.NewGraphFileChangeSource(eng.Analysis(), root, nil, 128)
	if err = source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for i := 0; i < 4; i++ {
		if _, err = r.ApplyChanges(context.Background(), source.Drain()); err != nil {
			t.Fatal(err)
		}
		if err = source.CoverSessionInputs(r); err != nil {
			t.Fatal(err)
		}
	}
	index := filepath.Join(root, ".git", "index")
	st, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		observed := source.ObservedPath(index)
		if err = os.Chmod(index, st.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for source.ObservedPath(index) <= observed && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if source.ObservedPath(index) <= observed {
			t.Fatal("native metadata delivery not observed")
		}
		res, err := r.ApplyChanges(context.Background(), source.Drain())
		if err != nil {
			t.Fatal(err)
		}
		if res.Reconciled || res.Work.PolicyBuilds != 0 || res.TargetGeneration != res.BaseGeneration {
			t.Fatalf("Git metadata self-reconciliation %+v", res)
		}
	}
	// No writers remain: the byte check must not create a self-sustaining
	// backend notification/hash loop even when the graph queue is empty.
	time.Sleep(50 * time.Millisecond)
	quiet := source.ObservedEvents()
	time.Sleep(200 * time.Millisecond)
	if got := source.ObservedEvents(); got != quiet {
		t.Fatalf("native observer kept generating idle events: %d -> %d", quiet, got)
	}
	graphWrite(t, root, ".gitignore", "unrelated.ts\n")
	graphGit(t, root, "rm", "--cached", "unrelated.ts")
	deadline := time.Now().Add(3 * time.Second)
	for {
		batch := source.Drain()
		if batch.Reconcile != "" {
			if _, err = r.ApplyChanges(context.Background(), batch); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("true index change not observed")
		}
		time.Sleep(time.Millisecond)
	}
	graphColdEqual(t, root, sink)
}

func TestScopedNearestPackageAndAliases(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	graphWrite(t, root, "packages/database/child/package.json", `{"name":"child","main":"nested.ts"}`)
	graphWrite(t, root, "packages/database/child/nested.ts", "export const child=1")
	apply("packages/database/child/package.json", "packages/database/child/nested.ts")
	graphWrite(t, root, "packages/database/package.json", `{"name":"new-database","dependencies":{"helper":"1"}}`)
	res := apply("packages/database/package.json")
	if res.ParsedFiles != 1 || res.Invalidation.ContextAffectedSources != 1 {
		t.Fatalf("nearest-package boundary crossed: %+v", res)
	}
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "packages/database/tsconfig.json", `{"compilerOptions":{"paths":{"@dep":["../clickhouse/index.ts"]}}}`)
	graphWrite(t, root, "packages/database/index.ts", "import {clickhouse} from '@dep';export function database(){return clickhouse}")
	apply("packages/database/tsconfig.json", "packages/database/index.ts")
	graphWrite(t, root, "packages/database/tsconfig.json", `{"compilerOptions":{"paths":{"@dep":["../../a.ts"]}}}`)
	res = apply("packages/database/tsconfig.json")
	if res.ParsedFiles != 2 || res.Invalidation.ContextAffectedSources != 2 || len(res.Invalidation.ContextReasons) != 0 {
		t.Fatalf("nested alias escaped effective descendant scope: %+v", res)
	}
	graphColdEqual(t, root, sink)
	graphWrite(t, root, "packages/tsconfig.base.json", `{"compilerOptions":{"paths":{"@dep":["./clickhouse/index.ts"]}}}`)
	graphWrite(t, root, "packages/database/tsconfig.json", `{"extends":"../tsconfig.base.json"}`)
	apply("packages/tsconfig.base.json", "packages/database/tsconfig.json")
	graphWrite(t, root, "packages/tsconfig.base.json", `{"compilerOptions":{"paths":{"@dep":["../a.ts"]}}}`)
	res = apply("packages/tsconfig.base.json")
	if res.Invalidation.ContextAffectedSources != 3 || len(res.Invalidation.ContextReasons) != 0 {
		t.Fatalf("inherited alias scope wrong: %+v", res)
	}
	graphColdEqual(t, root, sink)
}

func TestScopedConfiguredClientsAndInvalidConfigRecovery(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript, manifests, mdintent, hcl, python, swift]\nignore: []\nclients:\n  - name: sdk-http\n    language: typescript\n    receiver_types: [IHttpRequestService]\n    methods:\n      - name: sendRequest\n        service_arg: 0\n        path_arg: 1\n        options_arg: 2\n")
	res := apply("mcp-arch.yaml")
	if res.ParsedFiles != 5 || len(res.Invalidation.ContextReasons) == 0 {
		t.Fatalf("configured clients reused parse context: %+v", res)
	}
	graphColdEqual(t, root, sink)
	for _, body := range []string{`{"compilerOptions":`, `{}`, `{"compilerOptions":{"paths":[]}}`, `{}`} {
		graphWrite(t, root, "tsconfig.json", body)
		res = apply("tsconfig.json")
		if res.ParsedFiles != 5 {
			t.Fatalf("config validity transition not invalidated %+v", res)
		}
		graphColdEqual(t, root, sink)
	}
}

func TestScopedHarmlessValidContextMembership(t *testing.T) {
	root, _, sink, apply := scopedFixture(t)
	for _, path := range []string{"packages/database/empty/package.json", "packages/database/tsconfig.json", "tsconfig.base.json"} {
		graphWrite(t, root, path, "{}")
		res := apply(path)
		if res.ParsedFiles != 0 || len(res.Invalidation.ContextReasons) != 0 {
			t.Fatalf("valid empty membership forced parsing: %s %+v", path, res)
		}
		graphColdEqual(t, root, sink)
		if err := os.Remove(filepath.Join(root, path)); err != nil {
			t.Fatal(err)
		}
		res = apply(path)
		if res.ParsedFiles != 0 || len(res.Invalidation.ContextReasons) != 0 {
			t.Fatalf("valid empty removal forced parsing: %s %+v", path, res)
		}
		graphColdEqual(t, root, sink)
	}
	// A new named package affects only its actual descendant attribution boundary.
	graphWrite(t, root, "packages/database/new/source.ts", "export const source=1")
	apply("packages/database/new/source.ts")
	graphWrite(t, root, "packages/database/new/package.json", `{"name":"new-child"}`)
	res := apply("packages/database/new/package.json")
	if res.ParsedFiles != 1 || res.Invalidation.ContextAffectedSources != 1 || len(res.Invalidation.ContextReasons) != 0 {
		t.Fatalf("known package boundary became global %+v", res)
	}
	graphColdEqual(t, root, sink)
}
