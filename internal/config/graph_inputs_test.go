package config

import "testing"

func TestGraphInputsYAMLReplacementAndIsolation(t *testing.T) {
	inherited, err := loadYAML(t, "graph_inputs:\n  exclude: [worker-reports/**]\n  semantic: ['**/*.PNG']\n")
	if err != nil {
		t.Fatal(err)
	}
	if inherited.GraphInputs.CacheExclusions != nil {
		t.Fatal("absent override must inherit graph defaults")
	}
	if !contains(inherited.Ignore, "**/node_modules/**") {
		t.Fatal("graph-only exclude replaced legacy ignore")
	}
	if contains(inherited.Ignore, "worker-reports/**") {
		t.Fatal("graph-only exclude leaked to legacy config")
	}
	if len(inherited.GraphInputs.Exclude) != 1 || len(inherited.GraphInputs.Semantic) != 1 {
		t.Fatal("graph fields not loaded")
	}
	empty, err := loadYAML(t, "ignore: []\ngraph_inputs:\n  cache_exclusions: []\n")
	if err != nil {
		t.Fatal(err)
	}
	if empty.GraphInputs.CacheExclusions == nil || len(*empty.GraphInputs.CacheExclusions) != 0 {
		t.Fatal("explicit empty override lost")
	}
	if contains(empty.Ignore, "**/node_modules/**") {
		t.Fatal("legacy replacement semantics changed")
	}
	custom, err := loadYAML(t, "graph_inputs:\n  cache_exclusions: [cache-here/**]\n")
	if err != nil {
		t.Fatal(err)
	}
	if custom.GraphInputs.CacheExclusions == nil || len(*custom.GraphInputs.CacheExclusions) != 1 || (*custom.GraphInputs.CacheExclusions)[0] != "cache-here/**" {
		t.Fatal("custom replacement not retained")
	}
}

func TestGraphInputOptionsCopiesAndOutput(t *testing.T) {
	cfg, err := loadYAML(t, "ignore: []\noutput:\n  dir: reports\ngraph_inputs:\n  exclude: [special/**]\n  semantic: ['**/*.PNG']\n  cache_exclusions: []\n")
	if err != nil {
		t.Fatal(err)
	}
	options := cfg.GraphInputOptions("state")
	if options.CacheExclusions == nil {
		t.Fatal("explicit empty cache override lost")
	}
	if len(options.ConfigPaths) != 1 || options.ConfigPaths[0] != cfg.SourcePath {
		t.Fatal("config dependency missing")
	}
	if !contains(options.StateDirs, "state") || !contains(options.StateDirs, "reports") {
		t.Fatal("state/output paths missing")
	}
	cfg.GraphInputs.Exclude[0] = "changed"
	cfg.GraphInputs.Semantic[0] = "changed"
	if !contains(options.Exclude, "special/**") || !contains(options.Semantic, "**/*.PNG") {
		t.Fatal("options alias config slices")
	}
	if Default().GraphInputOptions().CacheExclusions != nil {
		t.Fatal("default cache inheritance lost")
	}
}
