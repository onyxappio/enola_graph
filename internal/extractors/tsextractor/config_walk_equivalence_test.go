package tsextractor

import (
	"context"
	"fmt"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphprofile"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// Frozen traversal from 674cf84; preserve its prune and config-membership rules.
func stage22ConfigInputsOracle(ctx context.Context, repoPath string, inputScopes ...*inputscope.Scope) []string {
	inputScope := inputscope.First(inputScopes)
	tCfg := time.Now()
	names := []string{
		"tsconfig.json", "tsconfig.base.json", "jsconfig.json",
		"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"deno.json", "deno.jsonc", "svelte.config.js", "svelte.config.ts", "svelte.config.mjs",
		"angular.json", "nx.json", "schema.prisma", filepath.Join("prisma", "schema.prisma"),
		"nuxt.config.ts", "nuxt.config.js", "nuxt.config.mjs", "next.config.js", "next.config.mjs", "next.config.ts",
	}
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = filepath.ToSlash(rel)
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}
	for _, n := range names {
		p := filepath.Join(repoPath, n)
		if _, err := overlayStat(ctx, p, inputScope); err == nil {
			add(n)
			if n == "tsconfig.json" || n == "tsconfig.base.json" || n == "jsconfig.json" {
				followTSConfigExtends(repoPath, p, add, inputScope)
			}
		} else {
			// Keep a stable slot so a later appearance still changes the fingerprint.
			add(n)
		}
	}
	// Framework detectors also inspect the selected TS root. Retain missing
	// candidates so config additions enter the resident reconciliation path.
	if tsRoot, found := findTSRoot(ctx, repoPath, inputScope); found {
		rel, err := filepath.Rel(repoPath, tsRoot)
		rel = factpath.Slash(rel)
		if err == nil {
			for _, family := range []string{"svelte", "nuxt", "next"} {
				for _, ext := range []string{"js", "ts", "mjs"} {
					add(factpath.Join(rel, family+".config."+ext))
				}
			}
		}
	}
	// Match the actual alias reader's roots, including inherited tsconfig paths.
	// The repository-root fallback is already represented by names above.
	for _, root := range collectTSAliasRoots(ctx, repoPath, inputScope) {
		for _, name := range []string{"svelte.config.js", "svelte.config.ts", "svelte.config.mjs"} {
			add(factpath.Join(root.dir, name))
		}
	}
	_ = overlayWalkDir(ctx, repoPath, inputScope, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || tsSkipDirs[name] {
				if path != repoPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch d.Name() {
		case "tsconfig.json", "tsconfig.base.json", "jsconfig.json", "package.json":
			rel, relErr := filepath.Rel(repoPath, path)
			if relErr != nil {
				return nil
			}
			rel = factpath.Slash(rel)
			add(rel)
			if d.Name() != "package.json" {
				followTSConfigExtends(repoPath, path, add, inputScope)
			}
		}
		return nil
	})
	sort.Strings(out)
	graphprofile.Since("ts_config_inputs", tCfg, fmt.Sprintf("n=%d", len(out)))
	return out
}

func TestConfigDiscoverySingleWalkMatchesStage22(t *testing.T) {
	root := discoveryWalkFixture(t)
	for path, body := range map[string]string{
		"tsconfig.json":                 `{"compilerOptions":{"paths":{"@root/*":["src/*"]}}}`,
		"packages/ui/tsconfig.json":     `{"extends":"../../base.json"}`,
		"base.json":                     `{"compilerOptions":{"paths":{"@ui/*":["ui/*"]}}}`,
		"testdata/config/tsconfig.json": `{"extends":"../../missing.json"}`,
		".hidden/tsconfig.json":         `{}`,
		"packages/api/jsconfig.json":    `{}`,
		"packages/broken/tsconfig.json": `broken`,
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"ordinary", "hidden-root", "symlink-root", "config-named-symlink-root"} {
		t.Run(name, func(t *testing.T) {
			path := root
			if name == "hidden-root" {
				parent := t.TempDir()
				path = filepath.Join(parent, ".repo")
				if err := os.Rename(root, path); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Rename(path, root); err != nil {
						t.Errorf("cleanup: %v", err)
					}
				}()
			}
			if name == "symlink-root" || name == "config-named-symlink-root" {
				base := "repo"
				if name == "config-named-symlink-root" {
					base = "tsconfig.json"
				}
				path = filepath.Join(t.TempDir(), base)
				if err := os.Symlink(root, path); err != nil {
					t.Fatal(err)
				}
			}
			want := stage22ConfigInputsOracle(context.Background(), path)
			got := ConfigInputPaths(path)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("config membership changed:\nwant %v\ngot %v", want, got)
			}
		})
	}
}

func TestConfigDiscoverySingleWalkPolicy(t *testing.T) {
	root := discoveryWalkFixture(t)
	for _, excludes := range [][]string{nil, {"packages/ui"}, {"packages/ui/**"}, {"testdata/**"}, {"**/package.json"}} {
		policy, err := graphinput.Build(root, graphinput.Options{Exclude: excludes})
		if err != nil {
			t.Fatal(err)
		}
		if len(excludes) == 1 && excludes[0] == "packages/ui" && policy.Classify(filepath.Join(root, "packages/ui"), true).Kind != graphinput.Excluded {
			t.Fatal("fixture did not exclude directory itself")
		}
		scope := &inputscope.Scope{Root: root, Policy: policy}
		want := stage22ConfigInputsOracle(context.Background(), root, scope)
		got := ConfigInputPaths(root, scope)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("excludes %v: want %v got %v", excludes, want, got)
		}
	}
}
