package graphsession

import "testing"

func TestIndependentRootAliasDoesNotHideNestedPackageRetarget(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"package.json":              `{"name":"root"}`,
		"tsconfig.json":             `{"compilerOptions":{"paths":{"@local/pkg":["./shadow.ts"]}}}`,
		"shadow.ts":                 "export const target=3;\n",
		"nested/tsconfig.json":      `{"compilerOptions":{"paths":{"@other/*":["./other/*"]}}}`,
		"nested/consumer.ts":        "import { target } from '@local/pkg';\nexport const use=target;\n",
		"packages/pkg/package.json": `{"name":"@local/pkg","exports":{".":"./first.ts"}}`,
		"packages/pkg/first.ts":     "export const target=1;\n",
		"packages/pkg/second.ts":    "export const target=2;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	check := func(want string) {
		t.Helper()
		configScopeRun(t, eng, root, opts, cons)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
		target := independentDiscoveryAliasTarget(t, cons, "nested/consumer.ts")
		for _, n := range cons.Owners[ownerKey(want)] {
			if n.ID == target {
				return
			}
		}
		t.Fatalf("target %s is not owned by %s", target, want)
	}
	check("packages/pkg/first.ts")
	writeFile(t, root, "packages/pkg/package.json", `{"name":"@local/pkg","exports":{".":"./second.ts"}}`)
	check("packages/pkg/second.ts")
}
