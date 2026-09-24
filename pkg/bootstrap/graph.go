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
	"github.com/enola-labs/enola/internal/graphprofile"
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
	// One trace per construction. Its Marks partition this call from here on,
	// so the root and config path resolution below is inside the first span
	// rather than before it. The rebuild hook installed further down re-enters
	// this function, so a rebuild emits a second graphinput_build and a second
	// set of these Marks; they belong to the inner call and are already inside
	// the caller's rebuild_graph_inputs window rather than additional to it.
	gtr := graphprofile.StartNamed("graph_engine")
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
	gtr.Mark("config_load", filepath.Base(path))
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
	// Contains the graphinput_build trace's total, and is larger than it: the
	// state directory projection and GraphInputOptions preparation above happen
	// before Build is entered, and Build's deferred temp repository teardown
	// happens after its last Mark. Read the inner trace as a component of this
	// span with a residual, never as an equality.
	gtr.Mark("graphinput_build", fmt.Sprintf("configpaths=%d", len(policyOpts.ConfigPaths)))
	eng, err := engine.New(cfg)
	if err != nil {
		return nil, err
	}
	gtr.Mark("engine_new", "")
	scope := &inputscope.Scope{Root: root, Policy: policy}
	registerOSSPlugins(eng, cfg, scope)
	// Closes before ConfigureGraphInputs, so installing the rebuild hook is
	// attributed to config_recheck below rather than to this span.
	gtr.Mark("register_plugins", "")
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
	gtr.Mark("config_recheck", "")
	eng.SetPersistCache(false)
	return &Engine{eng: eng}, nil
}
