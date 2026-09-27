package graphinput

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func repeatedIndexAncestors(raw []byte) (map[string]bool, map[string]bool) {
	files, dirs := map[string]bool{}, map[string]bool{}
	for _, name := range strings.Split(string(raw), "\x00") {
		if name == "" {
			continue
		}
		files[name] = true
		for dir := filepath.ToSlash(filepath.Dir(name)); dir != "."; dir = filepath.ToSlash(filepath.Dir(dir)) {
			dirs[dir] = true
		}
	}
	return files, dirs
}

// Opt-in diagnostic of index decoding on a real repository; Git IO is excluded.
// This is not a whole-CLI performance acceptance benchmark.
func BenchmarkIndexNamesProduct(b *testing.B) {
	root := os.Getenv("ENOLA_INDEX_BENCH_REPO")
	if root == "" {
		b.Skip("set ENOLA_INDEX_BENCH_REPO to a real repository")
	}
	raw, err := runGit(root, "", nil, "ls-files", "-z", "--cached", "--", ".")
	if err != nil {
		b.Fatal(err)
	}
	wantFiles, wantDirs := repeatedIndexAncestors(raw)
	gotFiles, gotDirs := parseIndexNames(raw)
	if !reflect.DeepEqual(wantFiles, gotFiles) || !reflect.DeepEqual(wantDirs, gotDirs) {
		b.Fatal("index membership differs")
	}
	b.Logf("tracked=%d directories=%d", len(wantFiles), len(wantDirs))
	for _, c := range []struct {
		name string
		fn   func([]byte) (map[string]bool, map[string]bool)
	}{
		{"repeated_ancestors", repeatedIndexAncestors}, {"unique_ancestors", parseIndexNames},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				f, d := c.fn(raw)
				if len(f) != len(wantFiles) || len(d) != len(wantDirs) {
					b.Fatal("membership changed")
				}
			}
		})
	}
}
