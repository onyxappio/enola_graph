package tsextractor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/parallel"
)

// Diagnostic only: tracked TS/JS/SFC/SDL sources, not a graph-profile scope or
// end-to-end acceptance benchmark. Source loading is outside measured work.
func BenchmarkFrameworkSummaryProduct(b *testing.B) {
	root := os.Getenv("ENOLA_FRAMEWORK_BENCH_REPO")
	if root == "" {
		b.Skip("set ENOLA_FRAMEWORK_BENCH_REPO for a real repository")
	}
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		b.Fatal(err)
	}
	var files []string
	sources := map[string][]byte{}
	for _, rel := range strings.Split(string(raw), "\x00") {
		switch filepath.Ext(rel) {
		case ".ts", ".tsx", ".js", ".jsx", ".vue", ".svelte", ".graphql", ".gql":
		default:
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			b.Fatal(err)
		}
		files = append(files, rel)
		sources[rel] = src
	}
	if len(files) == 0 {
		b.Fatal("no sources")
	}
	b.Logf("diagnostic tracked source count: %d", len(files))
	b.Run("serial", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			results := make([]frameworkSummary, len(files))
			for i, rel := range files {
				results[i] = collectFrameworkSummary(rel, sources[rel])
			}
			frameworkSummarySink = results
		}
	})
	b.Run("bounded_parallel", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			frameworkSummarySink = parallel.MapFiles(context.Background(), files, func(rel string) frameworkSummary {
				return collectFrameworkSummary(rel, sources[rel])
			})
		}
	})
}

var frameworkSummarySink []frameworkSummary
