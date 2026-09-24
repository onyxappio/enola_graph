package graphsession

import (
	"fmt"
	"testing"
)

func TestIndependentUnusedPackageAdditionIsBounded(t *testing.T) {
	files := map[string]string{"package.json": `{"name":"root"}`}
	for i := 0; i < 16; i++ {
		files[fmt.Sprintf("src/unchanged%d.ts", i)] = fmt.Sprintf("export const value%d = %d;\n", i, i)
	}
	root := setupTSRepo(t, files)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	writeFile(t, root, "packages/new/package.json", `{"name":"@local/new","main":"./src/index.ts","types":"./src/index.ts"}`)
	writeFile(t, root, "packages/new/tsconfig.json", `{"compilerOptions":{"strict":true},"include":["src/**/*.ts"]}`)
	writeFile(t, root, "packages/new/src/index.ts", "export const fresh = 1;\n")
	result, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if result.ParsedFiles > 1 {
		t.Fatalf("unused new package reparsed unrelated sources: parsed=%d want<=1 scope=%v fallbacks=%v", result.ParsedFiles, ids, result.Fallbacks)
	}
}
