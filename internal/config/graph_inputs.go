package config

import "github.com/enola-labs/enola/internal/graphinput"

// GraphInputsConfig is graph-profile-only. Patterns use the same syntax as Ignore,
// not gitignore syntax. Exclude is additive; CacheExclusions replaces graph cache
// defaults when present (an empty list disables them). Semantic promotes media to
// content inputs but never overrides exclusions or gitignore.
type GraphInputsConfig struct {
	Exclude         []string  `yaml:"exclude"`
	CacheExclusions *[]string `yaml:"cache_exclusions"`
	Semantic        []string  `yaml:"semantic"`
}

// GraphInputOptions does not mutate cfg. Ignore keeps its existing replacement-list
// semantics; graph_inputs.exclude adds hard input exclusions. Test-reference consumers
// must choose their scope explicitly rather than reinterpret Excluded as extract-only.
func (cfg *Config) GraphInputOptions(stateDirs ...string) graphinput.Options {
	if cfg == nil {
		cfg = Default()
	}
	o := graphinput.Options{Exclude: append(append([]string{}, cfg.Ignore...), cfg.GraphInputs.Exclude...), Semantic: append([]string{}, cfg.GraphInputs.Semantic...), StateDirs: append([]string{}, stateDirs...)}
	if cfg.GraphInputs.CacheExclusions != nil {
		o.CacheExclusions = append([]string{}, (*cfg.GraphInputs.CacheExclusions)...)
	}
	if cfg.SourcePath != "" {
		o.ConfigPaths = []string{cfg.SourcePath}
	}
	// Keep actual output protected even for a programmatically constructed config.
	dir := cfg.Output.Dir
	if dir == "" {
		dir = ".enola"
	}
	o.StateDirs = append(o.StateDirs, dir)
	return o
}
