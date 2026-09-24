package graphsession

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// aliasBaseRepo is a workspace whose sources do not name any published package,
// so every alias in it is reachable by nobody until a test makes one reachable.
func aliasBaseRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{"package.json": `{"name":"root"}`}
	for i := 0; i < 4; i++ {
		files[fmt.Sprintf("src/unchanged%d.ts", i)] = fmt.Sprintf("export const value%d = %d;\n", i, i)
	}
	for k, v := range extra {
		files[k] = v
	}
	return setupTSRepo(t, files)
}

// TestAliasBoundUnreferencedPackageAdditionIsNarrow is the reported regression:
// publishing a package nothing imports must not reparse the workspace.
func TestAliasBoundUnreferencedPackageAdditionIsNarrow(t *testing.T) {
	root := aliasBaseRepo(t, nil)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeFile(t, root, "packages/new/package.json", `{"name":"@local/new","main":"./src/index.ts","types":"./src/index.ts"}`)
	writeFile(t, root, "packages/new/src/index.ts", "export const fresh = 1;\n")
	res, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if res.ParsedFiles > 1 {
		t.Fatalf("unreferenced package reparsed unrelated sources: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
	}
}

// TestAliasBoundReferencedPackageAdditionStillReparses is the true case the
// bound must not swallow: a source that already names the package resolves
// differently once the package exists, so it has to be reparsed.
func TestAliasBoundReferencedPackageAdditionStillReparses(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"src/consumer.ts": "import { fresh } from '@local/new';\nexport const used = fresh;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeFile(t, root, "packages/new/package.json", `{"name":"@local/new","main":"./src/index.ts","types":"./src/index.ts"}`)
	writeFile(t, root, "packages/new/src/index.ts", "export const fresh = 1;\n")
	res, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if !containsID(ids, "src/consumer.ts") {
		t.Fatalf("source naming the new package was not rescoped: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
	}
}

// TestAliasBoundPackageRemovalStillReparsesImporter is the other direction:
// once the package goes away the importer stops resolving, so it is dirty.
func TestAliasBoundPackageRemovalStillReparsesImporter(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"src/consumer.ts":           "import { fresh } from '@local/new';\nexport const used = fresh;\n",
		"packages/new/package.json": `{"name":"@local/new","main":"./src/index.ts","types":"./src/index.ts"}`,
		"packages/new/src/index.ts": "export const fresh = 1;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	aliasRemove(t, root, "packages/new/package.json")
	aliasRemove(t, root, "packages/new/src/index.ts")
	res, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if !containsID(ids, "src/consumer.ts") {
		t.Fatalf("importer was not rescoped when its package was removed: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func aliasRemove(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

// TestAliasBoundTSConfigPathsChangeReachesItsRoot is the tsconfig half of the
// bound. A `paths` entry belongs to one alias root, and the files that resolve
// against that root are the ones it can move - so the bound has to know which
// root a file resolves against, not merely that some alias moved somewhere.
func TestAliasBoundTSConfigPathsChangeReachesItsRoot(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"app/tsconfig.json":   `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["./lib/*"]}}}`,
		"app/lib/util.ts":     "export const util = 1;\n",
		"app/other/util.ts":   "export const util = 2;\n",
		"app/src/consumer.ts": "import { util } from '@app/util';\nexport const used = util;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeFile(t, root, "app/tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["./other/*"]}}}`)
	res, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if !containsID(ids, "app/src/consumer.ts") {
		t.Fatalf("a retargeted tsconfig path did not rescope the file resolving through it: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
	}
}
