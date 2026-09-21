package mdintent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

func TestPolicyDiscoveryDetection(t *testing.T) {
	for _, tc := range []struct {
		path          string
		graph, legacy bool
	}{
		{"README.md", true, true},
		{"build/README.md", true, false},
		{".app/README.md", true, false},
		{"node_modules/local/README.md", true, false},
		{"a/b/c/d/e/f/README.md", true, false},
		{"_archive/README.md", false, false},
		{"_views/README.md", false, false},
		{"blocked/README.md", false, true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.path)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("# Document\n"), 0644); err != nil {
				t.Fatal(err)
			}
			policy, err := graphinput.Build(root, graphinput.Options{CacheExclusions: []string{}, Exclude: []string{"blocked/**"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []struct {
				name      string
				extractor *Extractor
				want      bool
			}{
				{"graph", NewGraph(&inputscope.Scope{Root: root, Policy: policy}), tc.graph},
				{"legacy", New(), tc.legacy},
			} {
				t.Run(mode.name, func(t *testing.T) {
					got, err := mode.extractor.Detect(root)
					if err != nil || got != mode.want {
						t.Fatalf("Detect = %v, %v; want %v", got, err, mode.want)
					}
					if mode.name == "graph" {
						facts, err := mode.extractor.Extract(context.Background(), root, []string{tc.path})
						if err != nil {
							t.Fatal(err)
						}
						if (len(facts) > 0) != mode.want {
							t.Fatalf("facts = %v; want facts: %v", facts, mode.want)
						}
					}
				})
			}
		})
	}
}
