package tsextractor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
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

func TestPluginResolverCacheMatchesDirectResolutionAndInvalidatesSnapshots(t *testing.T) {
	root := t.TempDir()
	files := writePluginResolutionFiles(t, root, map[string]string{
		"tsconfig.json":    `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`,
		"src/main.ts":      `export {};`,
		"src/dep/index.ts": `export const index = 1;`,
		"lib/dep.ts":       `export const library = 1;`,
	})
	discovery := New().NewDiscovery(context.Background(), root, nil)
	cached := discovery.NewPluginResolver(files)
	direct := discovery.ResolvePluginModule("src/main.ts", "@app/dep", files)
	if got := cached.ResolveModule("src/other.ts", "@app/dep"); !reflect.DeepEqual(got, direct) {
		t.Fatalf("cached directory answer = %#v, want direct %#v", got, direct)
	}

	// Concurrent callbacks share the same immutable run snapshot and must not
	// race or produce distinct answers.
	var wg sync.WaitGroup
	errs := make(chan string, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := cached.ResolveModule("src/again.ts", "@app/dep"); !reflect.DeepEqual(got, direct) {
				errs <- "concurrent cached answer changed"
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// A changed inventory builds a fresh resolver, so an exact file that now
	// beats index.ts cannot be served by the old snapshot's cache.
	if err := os.WriteFile(filepath.Join(root, "src", "dep.ts"), []byte("export const exact = 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	withExact := append(append([]string(nil), files...), "src/dep.ts")
	if got := discovery.NewPluginResolver(withExact).ResolveModule("src/main.ts", "@app/dep"); got.File != "src/dep.ts" {
		t.Fatalf("inventory-change resolver file = %q, want src/dep.ts", got.File)
	}

	// A changed tsconfig requires a new Discovery and must similarly discard
	// the earlier alias answer.
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["lib/*"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	afterConfig := New().NewDiscovery(context.Background(), root, nil)
	if got := afterConfig.NewPluginResolver(withExact).ResolveModule("src/main.ts", "@app/dep"); got.File != "lib/dep.ts" {
		t.Fatalf("tsconfig-change resolver file = %q, want lib/dep.ts", got.File)
	}
}

func BenchmarkPluginResolverCache(b *testing.B) {
	root := b.TempDir()
	files := make([]string, 0, 512)
	for i := 0; i < 256; i++ {
		rel := filepath.ToSlash(filepath.Join("packages", fmt.Sprintf("p%d", i), "src", fmt.Sprintf("value%d.ts", i)))
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("export const value = 1;"), 0o644); err != nil {
			b.Fatal(err)
		}
		files = append(files, rel)
	}
	from := "packages/p0/src/caller.ts"
	files = append(files, from)
	discovery := New().NewDiscovery(context.Background(), root, nil)
	b.Run("uncached", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = discovery.ResolvePluginModule(from, "./value0", files)
		}
	})
	b.Run("cached", func(b *testing.B) {
		resolver := discovery.NewPluginResolver(files)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = resolver.ResolveModule(from, "./value0")
		}
	})
}
