package graphsession

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/filelock"
	"github.com/enola-labs/enola/internal/graphstream"
)

// ForkOptions seed a NEW empty target state directory from a completed source
// checkpoint. Independent worktrees are not supported: Checkout must match the
// source checkout. The source directory is never written.
type ForkOptions struct {
	SourceDir        string
	TargetDir        string
	ContextID        string
	RepoID           string
	SinkID           string
	Checkout         string
	ExtractorVersion string
}

// Fork copies a completed source graphstate into an empty target directory
// under a new context. It does not publish events and does not copy a database.
func Fork(opts ForkOptions) (*State, error) {
	srcDir, err := absClean(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	dstDir, err := absClean(opts.TargetDir)
	if err != nil {
		return nil, err
	}
	if srcDir == dstDir {
		return nil, fmt.Errorf("graphsession fork: source and target state-dir are the same path")
	}
	if opts.ContextID == "" {
		return nil, fmt.Errorf("graphsession fork: target --context is required")
	}
	checkout := opts.Checkout
	if checkout != "" {
		checkout, err = absClean(checkout)
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	srcLock, dstLock, err := lockOrdered(srcDir, dstDir)
	if err != nil {
		return nil, err
	}
	defer srcLock.Release()
	defer dstLock.Release()

	src, err := loadForkSource(srcDir)
	if err != nil {
		return nil, err
	}
	if src.ContextID == opts.ContextID {
		return nil, fmt.Errorf("graphsession fork: target context %q must differ from source context %q", opts.ContextID, src.ContextID)
	}
	if opts.RepoID != "" && src.RepoID != "" && opts.RepoID != src.RepoID {
		return nil, fmt.Errorf("graphsession fork: repo %q does not match source %q", opts.RepoID, src.RepoID)
	}
	if checkout != "" && src.Checkout != "" && filepath.Clean(src.Checkout) != checkout {
		return nil, fmt.Errorf("graphsession fork: independent worktrees are not supported (source checkout %q, target %q)", src.Checkout, checkout)
	}
	wantVer := opts.ExtractorVersion
	if wantVer == "" {
		wantVer = engine.ExtractorVersion()
	}
	if src.ExtractorVersion != "" && src.ExtractorVersion != wantVer {
		return nil, fmt.Errorf("graphsession fork: source extractor version %q does not match %q", src.ExtractorVersion, wantVer)
	}
	if src.Schema != "" && src.Schema != stateSchema {
		return nil, fmt.Errorf("graphsession fork: source schema %q is not %s", src.Schema, stateSchema)
	}

	if existing, err := inspectForkTarget(dstDir, src, opts, checkout); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	dst, err := cloneState(src)
	if err != nil {
		return nil, err
	}
	dst.ContextID = opts.ContextID
	if opts.RepoID != "" {
		dst.RepoID = opts.RepoID
	}
	if checkout != "" {
		dst.Checkout = checkout
	}
	dst.SinkID = opts.SinkID
	dst.ForkBaseRepoID = src.RepoID
	dst.ForkBaseContextID = src.ContextID
	dst.ForkBaseGeneration = src.Generation
	dst.ForkBaseRunID = src.LastRunID
	dst.LastComplete = true

	id := boundIdentity{RepoID: dst.RepoID, ContextID: dst.ContextID, SinkID: dst.SinkID, Checkout: dst.Checkout}
	if err := writeIdentityFile(dstDir, id); err != nil {
		return nil, err
	}
	if err := saveState(dstDir, dst); err != nil {
		return nil, err
	}
	return dst, nil
}

func absClean(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("graphsession fork: path is empty")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func lockOrdered(a, b string) (srcLock, dstLock *filelock.Lock, err error) {
	first, second := a, b
	swapped := false
	if a > b {
		first, second = b, a
		swapped = true
	}
	l1, err := filelock.Acquire(filepath.Join(first, "session"))
	if err != nil {
		return nil, nil, fmt.Errorf("graphsession fork lock: %w", err)
	}
	l2, err := filelock.Acquire(filepath.Join(second, "session"))
	if err != nil {
		l1.Release()
		return nil, nil, fmt.Errorf("graphsession fork lock: %w", err)
	}
	if swapped {
		return l2, l1, nil
	}
	return l1, l2, nil
}

func loadForkSource(dir string) (*State, error) {
	if _, err := os.Stat(pendingStatePath(dir)); err == nil {
		return nil, fmt.Errorf("graphsession fork: source has unresolved pending-state; recover it with a normal graph run first")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	st, err := loadCommittedState(dir)
	if err != nil {
		return nil, err
	}
	if st == nil || !st.LastComplete || st.Generation == 0 || st.LastRunID == "" {
		return nil, fmt.Errorf("graphsession fork: source has no completed checkpoint")
	}
	j, err := graphstream.OpenJournal(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = j.Close() }()
	if n := len(j.Unacked()); n != 0 {
		return nil, fmt.Errorf("graphsession fork: source has %d unacknowledged journal messages", n)
	}
	return st, nil
}

func inspectForkTarget(dstDir string, src *State, opts ForkOptions, checkout string) (*State, error) {
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		return nil, err
	}
	hasIdentity := false
	hasState := false
	hasPending := false
	for _, e := range entries {
		if e.IsDir() {
			return nil, fmt.Errorf("graphsession fork: target state-dir is not empty")
		}
		switch e.Name() {
		case "session.lock", "identity.json.tmp":
		case "identity.json":
			hasIdentity = true
		case "state.json":
			hasState = true
		case "pending-state.json":
			hasPending = true
		default:
			return nil, fmt.Errorf("graphsession fork: target state-dir is not empty")
		}
	}
	if hasIdentity {
		if err := requireMatchingForkIdentity(dstDir, src, opts, checkout); err != nil {
			return nil, err
		}
	}
	if hasState && hasPending {
		return nil, fmt.Errorf("graphsession fork: target state-dir is not empty")
	}
	if hasState {
		st, err := loadCommittedState(dstDir)
		if err != nil {
			return nil, err
		}
		if !forkStateMatches(st, src, opts, checkout) {
			return nil, fmt.Errorf("graphsession fork: target state-dir is not empty")
		}
		return st, nil
	}
	if hasPending {
		pending, err := loadPendingState(dstDir)
		if err != nil {
			return nil, err
		}
		if !forkStateMatches(pending, src, opts, checkout) {
			return nil, fmt.Errorf("graphsession fork: target state-dir is not empty")
		}
		if err := promotePendingState(dstDir); err != nil {
			return nil, err
		}
		return pending, nil
	}
	return nil, nil
}

func requireMatchingForkIdentity(dir string, src *State, opts ForkOptions, checkout string) error {
	b, err := os.ReadFile(identityPath(dir))
	if err != nil {
		return fmt.Errorf("graphsession fork: target state-dir is not empty")
	}
	var id boundIdentity
	if err := json.Unmarshal(b, &id); err != nil {
		return fmt.Errorf("graphsession fork: target state-dir is not empty")
	}
	if !identityMatchesForkTarget(id, src, opts, checkout) {
		return fmt.Errorf("graphsession fork: target state-dir is not empty")
	}
	return nil
}

func forkStateMatches(st *State, src *State, opts ForkOptions, checkout string) bool {
	if st == nil || src == nil || !st.LastComplete {
		return false
	}
	if st.ContextID != opts.ContextID {
		return false
	}
	if st.ForkBaseRepoID != src.RepoID || st.ForkBaseContextID != src.ContextID || st.ForkBaseGeneration != src.Generation || st.ForkBaseRunID != src.LastRunID {
		return false
	}
	if opts.RepoID != "" && st.RepoID != "" && st.RepoID != opts.RepoID {
		return false
	}
	if checkout != "" && st.Checkout != "" && filepath.Clean(st.Checkout) != checkout {
		return false
	}
	if opts.SinkID != "" && st.SinkID != "" && st.SinkID != opts.SinkID {
		return false
	}
	return true
}

func identityMatchesForkTarget(id boundIdentity, src *State, opts ForkOptions, checkout string) bool {
	if id.ContextID != opts.ContextID {
		return false
	}
	wantRepo := ""
	if src != nil {
		wantRepo = src.RepoID
	}
	if opts.RepoID != "" {
		wantRepo = opts.RepoID
	}
	if id.RepoID != "" && wantRepo != "" && id.RepoID != wantRepo {
		return false
	}
	wantCheckout := ""
	if src != nil {
		wantCheckout = src.Checkout
	}
	if checkout != "" {
		wantCheckout = checkout
	}
	if id.Checkout != "" && wantCheckout != "" && filepath.Clean(id.Checkout) != filepath.Clean(wantCheckout) {
		return false
	}
	if opts.SinkID != "" && id.SinkID != "" && id.SinkID != opts.SinkID {
		return false
	}
	return true
}

func cloneState(src *State) (*State, error) {
	b, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst State
	if err := json.Unmarshal(b, &dst); err != nil {
		return nil, err
	}
	if dst.Files == nil {
		dst.Files = map[string]*FileState{}
	}
	if dst.Synthetic == nil {
		dst.Synthetic = map[string][]facts.Fact{}
	}
	return &dst, nil
}

func writeIdentityFile(dir string, id boundIdentity) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	path := identityPath(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
