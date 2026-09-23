package graphsession

import (
	"context"
	"fmt"
	"github.com/enola-labs/enola/internal/graphstream"
	"testing"
)

func TestIndependentResidentNuxtSourceRootSequence(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"one/target.ts":  "export const target = 1;\n",
		"two/target.ts":  "export const target = 2;\n",
		"consumer.ts":    "import { target } from '~/target';\nfunction helper(){ return 1; }\nexport function consumed(){ return target; }\n",
		"package.json":   `{"name":"app","dependencies":{"nuxt":"^3.0.0","vue":"^3.0.0"}}`,
		"nuxt.config.ts": `export default defineNuxtConfig({srcDir: "one"})`,
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
	originalTarget := independentDiscoveryAliasTarget(t, cons, "consumer.ts")
	for _, side := range []string{"two", "one", "two"} {
		t.Run("alias_"+side, func(t *testing.T) {
			before := independentDiscoveryAliasTarget(t, cons, "consumer.ts")
			writeRepoFile(t, root, "nuxt.config.ts", fmt.Sprintf(`export default defineNuxtConfig({srcDir: "%s"})`, side))
			q.Add("nuxt.config.ts")
			apply(t)
			after := independentDiscoveryAliasTarget(t, cons, "consumer.ts")
			if before == after {
				t.Fatal("alias retarget did not move importer")
			}
			if (side == "one") != (after == originalTarget) {
				t.Fatal("alias resolves to wrong retained context")
			}
			for _, body := range []string{"target + helper()", "target"} {
				beforeGraph := cons.Canonical()
				writeRepoFile(t, root, "consumer.ts", "import { target } from '~/target';\nfunction helper(){ return 1; }\nexport function consumed(){ return "+body+"; }\n")
				q.Add("consumer.ts")
				apply(t)
				if cons.Canonical() == beforeGraph {
					t.Fatal("body edit did not exercise a changed graph")
				}
				if independentDiscoveryAliasTarget(t, cons, "consumer.ts") != after {
					t.Fatal("content fast path reverted discovery context")
				}
				n := len(sink.CloneRecords())
				generation := r.state.Generation
				q.Add("consumer.ts")
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
