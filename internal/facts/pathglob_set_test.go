package facts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobSetMatchesMatchGlob(t *testing.T) {
	patterns := []string{
		"**/vendor/**",
		"**/node_modules/**",
		"**/.git/**",
		"**/dist/**",
		"**/build/**",
		"**/tmp/**",
		"**/public/assets/**",
		"**/testdata/**",
		"**/*_test.go",
		"**/*.test.ts",
		"**/*.test.tsx",
		"**/*.spec.ts",
		"**/*.spec.tsx",
		"**/spec/**/*_spec.rb",
		"**/test/**/*_test.rb",
		"**/conftest.py",
		"**/test_*.py",
		"**/tests/**/*.py",
		"**/src/test/**/*.scala",
		"**/*.Tests/**/*.cs",
		"**/Tests/**/*.cs",
	}
	set := CompileGlobs(patterns)
	samples := []string{
		"src/a.ts",
		"src/a.test.ts",
		"packages/foo/node_modules/x/index.js",
		"vendor/pkg/lib.go",
		"dist/out.js",
		"build/a",
		"tmp/x",
		".git/HEAD",
		"public/assets/logo.png",
		"testdata/repos/x.ts",
		"pkg/foo_test.go",
		"app/jobs/cache_warmup_ab_test.rb",
		"spec/user_spec.rb",
		"test/user_test.rb",
		"tests/conftest.py",
		"test_app.py",
		"src/test/scala/A.scala",
		"test-magnolia/src/main/scala-3/zio/test/A.scala",
		"MyApp.Tests/Foo.cs",
		"MyApp/Foo.cs",
		"packages/crypto/src/password.ts",
		"packages/foo/package.json",
		"docs/n.md",
		"openapi.yaml",
		"a.png",
		"data.json",
	}
	for _, p := range samples {
		wantPat, want := MatchGlob(p, patterns)
		gotPat, got := set.Match(p)
		if got != want || gotPat != wantPat {
			t.Fatalf("%s: MatchGlob (%q,%v) GlobSet (%q,%v)", p, wantPat, want, gotPat, got)
		}
	}
}

func TestGlobSetProductIgnoreEquivalence(t *testing.T) {
	root := os.Getenv("ENOLA_PRODUCT_FIXTURE")
	if root == "" {
		t.Skip("ENOLA_PRODUCT_FIXTURE not set")
	}
	patterns := []string{
		"**/vendor/**",
		"**/node_modules/**",
		"**/.git/**",
		"**/dist/**",
		"**/build/**",
		"**/tmp/**",
		"**/public/assets/**",
		"**/testdata/**",
		"**/*_test.go",
		"**/*.test.ts",
		"**/*.spec.ts",
		"**/*.Tests/**/*.cs",
	}
	set := CompileGlobs(patterns)
	n := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		n++
		if n > 20000 {
			return filepath.SkipAll
		}
		wantPat, want := MatchGlob(rel, patterns)
		gotPat, got := set.Match(rel)
		if got != want || gotPat != wantPat {
			t.Fatalf("%s: MatchGlob (%q,%v) GlobSet (%q,%v)", rel, wantPat, want, gotPat, got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 100 {
		t.Fatalf("walked %d paths, want a real fixture", n)
	}
}
