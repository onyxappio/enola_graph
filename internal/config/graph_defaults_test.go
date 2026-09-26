package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGraphDefaultsIncludeTestSource(t *testing.T) {
	legacy := Default()
	graph := GraphDefault()
	for _, pattern := range legacy.TestGlobs {
		if contains(graph.Ignore, pattern) {
			t.Errorf("graph still ignores %s", pattern)
		}
		if !contains(legacy.Ignore, pattern) {
			t.Errorf("legacy test policy changed for %s", pattern)
		}
	}
	for _, pattern := range []string{"**/node_modules/**", "**/testdata/**", "**/*.min.js"} {
		if !contains(graph.Ignore, pattern) {
			t.Errorf("unrelated exclusion lost: %s", pattern)
		}
	}
	if !reflect.DeepEqual(Default().Ignore, legacy.Ignore) {
		t.Fatal("graph defaults mutated legacy")
	}
}

func TestLoadGraphKeepsExplicitRepositoryExclusions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mcp-arch.yaml")
	for _, tc := range []struct {
		body    string
		ignored bool
	}{
		{"extractors: [typescript]\n", false},
		{"ignore: ['**/*.test.ts']\n", true},
		{"ignore: []\n", false},
	} {
		if err := os.WriteFile(p, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadGraph(p)
		if err != nil {
			t.Fatal(err)
		}
		if contains(cfg.Ignore, "**/*.test.ts") != tc.ignored {
			t.Fatalf("wrong ignore for %q", tc.body)
		}
	}
}
