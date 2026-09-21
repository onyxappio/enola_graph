package bootstrap

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

// GraphOptions selects a single graph checkout. ConfigPath overrides root discovery.
// StateDirs includes graph checkpoints and any graph output files inside the checkout.
type GraphOptions struct {
	Repo, ConfigPath string
	StateDirs        []string
}

// NewGraphEngine builds an immutable graph-only input profile. Legacy NewEngine
// intentionally retains its historical configuration and lockfile behavior.
func NewGraphEngine(opts GraphOptions) (*Engine, error) {
	opts.StateDirs = append([]string(nil), opts.StateDirs...)
	root := opts.Repo
	if root == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if st, err := os.Lstat(root); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("graph repository root is a symlink; use its resolved checkout path")
	}
	path := opts.ConfigPath
	explicit := path != ""
	if path == "" {
		path = filepath.Join(root, "mcp-arch.yaml")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	before, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	cfg := config.Default()
	if _, err = os.Stat(path); err == nil {
		cfg, err = config.Load(path)
		if err != nil {
			return nil, err
		}
	} else if explicit || !os.IsNotExist(err) {
		return nil, fmt.Errorf("graph config: %w", err)
	}
	cfg.Repo = root
	cfg.Repos = nil
	var outputs []string
	for _, p := range opts.StateDirs {
		if p != "" {
			if filepath.IsAbs(p) {
				rel, err := filepath.Rel(root, p)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					continue
				}
				p = rel
			}
			outputs = append(outputs, p)
		}
	}
	policyOpts := cfg.GraphInputOptions(outputs...)
	policyOpts.ConfigPaths = append(policyOpts.ConfigPaths, path)
	policy, err := graphinput.Build(root, policyOpts)
	if err != nil {
		return nil, err
	}
	eng, err := engine.New(cfg)
	if err != nil {
		return nil, err
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	registerOSSPlugins(eng, cfg, scope)
	eng.ConfigureGraphInputs(scope, func() (*engine.Engine, error) {
		fresh, err := NewGraphEngine(opts)
		if err != nil {
			return nil, err
		}
		return fresh.Analysis(), nil
	})
	after, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if !bytes.Equal(before, after) {
		return nil, fmt.Errorf("graph configuration changed during construction")
	}
	eng.SetPersistCache(false)
	return &Engine{eng: eng}, nil
}
