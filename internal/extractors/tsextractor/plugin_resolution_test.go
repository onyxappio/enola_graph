package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writePluginResolutionFiles(t *testing.T, root string, files map[string]string) []string {
	t.Helper()
	paths := make([]string, 0, len(files))
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, rel)
	}
	return paths
}

func TestPluginModuleResolutionUsesTypeScriptAliasesAndCandidateOrder(t *testing.T) {
	root := t.TempDir()
	files := writePluginResolutionFiles(t, root, map[string]string{
		"tsconfig.json":    `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`,
		"src/main.ts":      `import { value } from "@app/dep";`,
		"src/dep.ts":       `export const value = 1;`,
		"src/dep/index.ts": `export const value = 2;`,
	})
	discovery := New().NewDiscovery(context.Background(), root, nil)
	got := discovery.ResolvePluginModule("src/main.ts", "@app/dep", files)
	if got.External || got.File != "src/dep.ts" || got.ModuleDir != "src" {
		t.Fatalf("resolution = %+v", got)
	}
	if !reflect.DeepEqual(got.Candidates, []string{"src/dep", "src/dep.ts"}) {
		t.Fatalf("candidate prefix = %v", got.Candidates)
	}
}

func TestPluginExportResolutionFollowsNamedReexportsAndReportsReads(t *testing.T) {
	root := t.TempDir()
	files := writePluginResolutionFiles(t, root, map[string]string{
		"src/index.ts": `export { value } from "./impl";`,
		"src/impl.ts":  `export const value = 1;`,
	})
	discovery := New().NewDiscovery(context.Background(), root, nil)
	reads := map[string]bool{}
	target, file := discovery.ResolvePluginExport("src/index.ts", "value", files, func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil
		}
		return b
	}, func(rel string) { reads[rel] = true })
	if file != "src/impl.ts" || target != "src.value" {
		t.Fatalf("resolved export = (%q, %q)", target, file)
	}
	if !reads["src/index.ts"] || !reads["src/impl.ts"] {
		t.Fatalf("resolver reads = %v", reads)
	}
}
