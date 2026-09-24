package graphsession

import (
	"fmt"
	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/extractors/manifestextractor"
	"github.com/enola-labs/enola/internal/extractors/pythonextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphinput"
	"testing"
)

func TestIndependentTSAdditionWithEmptyPythonDomain(t *testing.T) {
	files := map[string]string{
		"package.json":     `{"name":"root"}`,
		"tsconfig.json":    `{"compilerOptions":{"paths":{"@feature":["./first.ts"]}}}`,
		"first.ts":         "export const target=1;\n",
		"second.ts":        "export const target=2;\n",
		"consumer.ts":      "import { target } from '@feature';\nexport const use=target;\n",
		"requirements.txt": "# no active Python sources\n",
	}
	for i := 0; i < 8; i++ {
		files[fmt.Sprintf("unrelated/f%d.ts", i)] = fmt.Sprintf("export const value%d=%d;\n", i, i)
	}
	root := setupTSRepo(t, files)
	var attach func() *engine.Engine
	attach = func() *engine.Engine {
		cfg := config.Default()
		cfg.Repo = root
		cfg.Extractors = []string{"typescript", "python", "manifests"}
		eng, err := engine.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		policy, err := graphinput.Build(root, graphinput.Options{})
		if err != nil {
			t.Fatal(err)
		}
		scope := &inputscope.Scope{Root: root, Policy: policy}
		eng.RegisterExtractor(tsextractor.New())
		eng.RegisterExtractor(pythonextractor.NewGraph(scope))
		eng.RegisterExtractor(manifestextractor.NewGraph(scope))
		eng.ConfigureGraphInputs(scope, func() (*engine.Engine, error) { return attach(), nil })
		return eng
	}
	eng := attach()
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	writeFile(t, root, "new.ts", "export const fresh=7;\n")
	result, scope, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	target := independentDiscoveryAliasTarget(t, cons, "consumer.ts")
	found := false
	for _, n := range cons.Owners[ownerKey("first.ts")] {
		if n.ID == target {
			found = true
		}
	}
	if !found {
		t.Fatal("consumer lost original target")
	}
	if scope["scripts/tool.py"] || scope["unrelated/f0.ts"] {
		t.Fatalf("TS-only source addition replaced unrelated owners: scope=%v parsed=%d fallback=%v", ids, result.ParsedFiles, result.Fallbacks)
	}
}
