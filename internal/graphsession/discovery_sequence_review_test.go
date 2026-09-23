package graphsession

import (
	"context"
	"fmt"
	"github.com/enola-labs/enola/internal/graphstream"
	"strings"
	"testing"
)

func TestIndependentResidentDiscoveryRetargetSequence(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/one/target.ts": "export const target = 1;\n",
		"src/two/target.ts": "export const target = 2;\n",
		"src/consumer.ts":   "import { target } from '@app/target';\nfunction helper(){ return 1; }\nexport function consumed(){ return target; }\n",
		"tsconfig.json":     `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/one/*"]}}}`,
	})
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(context.Background(), configScopeEngine(t, root), root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	q := NewChangeQueue("independent-discovery", 32)
	if err = q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	cons := NewConsumer()
	apply := func(t *testing.T) {
		t.Helper()
		if _, err := r.ApplyChanges(context.Background(), q.Drain()); err != nil {
			t.Fatal(err)
		}
		applyRun(t, cons, sink)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
	}
	apply(t)
	originalTarget := independentDiscoveryAliasTarget(t, cons, "src/consumer.ts")
	for _, side := range []string{"two", "one", "two"} {
		t.Run("alias_"+side, func(t *testing.T) {
			before := independentDiscoveryAliasTarget(t, cons, "src/consumer.ts")
			writeRepoFile(t, root, "tsconfig.json", fmt.Sprintf(`{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/%s/*"]}}}`, side))
			q.Add("tsconfig.json")
			apply(t)
			after := independentDiscoveryAliasTarget(t, cons, "src/consumer.ts")
			if before == after {
				t.Fatal("alias retarget did not move importer")
			}
			if (side == "one") != (after == originalTarget) {
				t.Fatal("alias resolves to wrong retained context")
			}
			for _, body := range []string{"target + helper()", "target"} {
				beforeGraph := cons.Canonical()
				writeRepoFile(t, root, "src/consumer.ts", "import { target } from '@app/target';\nfunction helper(){ return 1; }\nexport function consumed(){ return "+body+"; }\n")
				q.Add("src/consumer.ts")
				apply(t)
				if cons.Canonical() == beforeGraph {
					t.Fatal("body edit did not exercise a changed graph")
				}
				if independentDiscoveryAliasTarget(t, cons, "src/consumer.ts") != after {
					t.Fatal("content fast path reverted discovery context")
				}
				n := len(sink.CloneRecords())
				generation := r.state.Generation
				q.Add("src/consumer.ts")
				res, err := r.ApplyChanges(context.Background(), q.Drain())
				if err != nil {
					t.Fatal(err)
				}
				if res.ParsedFiles != 0 || r.state.Generation != generation || len(sink.CloneRecords()) != n {
					t.Fatal("identical-write event caused work/publication")
				}
			}
		})
	}
}

func independentDiscoveryAliasTarget(t *testing.T, cons *Consumer, owner string) string {
	t.Helper()
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: owner}).String()
	var targets []string
	for _, e := range cons.Edges[key] {
		if e.Resolution == "resolved" && e.TargetID != "" && strings.HasSuffix(e.TargetName, ".target") {
			targets = append(targets, e.TargetID)
		}
	}
	if len(targets) != 1 {
		t.Fatalf("expected one resolved target import, got %v", targets)
	}
	return targets[0]
}
