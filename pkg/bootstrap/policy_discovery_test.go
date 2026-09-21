package bootstrap

import (
	"context"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
	"testing"
)

func TestPolicyDiscoveryAllowedFacts(t *testing.T) {
	for _, tc := range []struct{ name, ext, path, body string }{
		{"dot_manifest", "manifests", ".app/package.json", `{"dependencies":{"independent-probe":"^1.0.0"}}`},
		{"override_cache_manifest", "manifests", "node_modules/local/package.json", `{"dependencies":{"independent-probe":"^1.0.0"}}`},
		{"dot_markdown", "mdintent", ".app/README.md", "# Independent document\n"},
		{"deep_markdown", "mdintent", "a/b/c/d/e/f/README.md", "# Independent document\n"},
		{"override_cache_markdown", "mdintent", "node_modules/local/README.md", "# Independent document\n"},
		{"vendor_manifest", "manifests", "vendor/local/package.json", `{"dependencies":{"independent-probe":"^1.0.0"}}`},
		{"testdata_manifest", "manifests", "testdata/local/package.json", `{"dependencies":{"independent-probe":"^1.0.0"}}`},
		{"build_markdown", "mdintent", "build/README.md", "# Independent document\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			graphWrite(t, root, "mcp-arch.yaml", "extractors: ["+tc.ext+"]\nignore: []\ngraph_inputs:\n  cache_exclusions: []\n")
			graphWrite(t, root, tc.path, tc.body)
			eng, err := NewGraphEngine(GraphOptions{Repo: root})
			if err != nil {
				t.Fatal(err)
			}
			d := eng.Analysis().GraphScope().Policy.Classify(tc.path, false)
			if d.Kind != graphinput.Semantic {
				t.Fatalf("not allowed: %+v", d)
			}
			inv, err := eng.Analysis().Inventory(root)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range inv.Files {
				if p == tc.path {
					found = true
				}
			}
			if !found {
				t.Fatal("missing inventory")
			}
			res, err := graphsession.Run(context.Background(), eng.Analysis(), root, &graphstream.MemorySink{}, graphsession.Options{StateDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range res.Facts {
				if f.File == tc.path {
					return
				}
			}
			t.Fatalf("policy-allowed %s has no facts; detected=%v facts=%v", tc.path, eng.Analysis().DetectExtractors(root, inv.AllNames), res.Facts)
		})
	}
}
