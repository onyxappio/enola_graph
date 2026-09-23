package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestIndependentResidentPackageExportsMembershipSequence(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"packages/lib/package.json":      `{"name":"local-lib","exports":{".":{"types":"./preferred/entry.ts","import":"./fallback/entry.ts"}}}`,
		"packages/lib/fallback/entry.ts": "export const target = 1;\n",
		"consumer.ts":                    "import { target } from 'local-lib';\nfunction helper(){return 1}\nexport function consume(){return target}\n",
	})
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(context.Background(), configScopeEngine(t, root), root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	q := NewChangeQueue("independent-package-membership", 32)
	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	consumer := NewConsumer()
	apply := func(want string) {
		t.Helper()
		if _, err := r.ApplyChanges(context.Background(), q.Drain()); err != nil {
			t.Fatal(err)
		}
		applyRun(t, consumer, sink)
		assertAppliedEqualsCold(t, consumer, coldConsumer(t, configScopeEngine(t, root), root))
		target := independentDiscoveryAliasTarget(t, consumer, "consumer.ts")
		found := false
		for _, node := range consumer.Owners[ownerKey(want)] {
			if node.ID == target {
				found = true
			}
		}
		if !found {
			t.Fatalf("import target %s is not owned by %s", target, want)
		}
	}
	apply("packages/lib/fallback/entry.ts")
	// The manifest never changes. Alias selection must still use the current
	// file set when a higher-priority export target appears or disappears.
	for _, present := range []bool{true, false, true} {
		want := "packages/lib/fallback/entry.ts"
		if present {
			if err := os.MkdirAll(filepath.Join(root, "packages/lib/preferred"), 0755); err != nil {
				t.Fatal(err)
			}
			writeRepoFile(t, root, "packages/lib/preferred/entry.ts", "export const target = 2;\n")
			want = "packages/lib/preferred/entry.ts"
		} else if err := os.Remove(filepath.Join(root, "packages/lib/preferred/entry.ts")); err != nil {
			t.Fatal(err)
		}
		q.Add("packages/lib/preferred/entry.ts")
		apply(want)
		for _, body := range []string{"target + helper()", "target"} {
			before := consumer.Canonical()
			writeRepoFile(t, root, "consumer.ts", "import { target } from 'local-lib';\nfunction helper(){return 1}\nexport function consume(){return "+body+"}\n")
			q.Add("consumer.ts")
			apply(want)
			if consumer.Canonical() == before {
				t.Fatal("body edit did not change the graph")
			}
			generation, events := r.state.Generation, len(sink.CloneRecords())
			q.Add("consumer.ts")
			res, err := r.ApplyChanges(context.Background(), q.Drain())
			if err != nil {
				t.Fatal(err)
			}
			if res.ParsedFiles != 0 || r.state.Generation != generation || len(sink.CloneRecords()) != events {
				t.Fatal("duplicate content event did work or published")
			}
		}
	}
}
