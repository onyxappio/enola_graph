package graphsession

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/internal/graphstream"
)

func statePath(dir string) string        { return filepath.Join(dir, "state.json") }
func pendingStatePath(dir string) string { return filepath.Join(dir, "pending-state.json") }
func identityPath(dir string) string     { return filepath.Join(dir, "identity.json") }

type boundIdentity struct {
	RepoID    string `json:"repo_id"`
	ContextID string `json:"context_id"`
	SinkID    string `json:"sink_id,omitempty"`
	Checkout  string `json:"checkout"`
}

// bindDispatchIdentity records repo/context/sink/checkout before any journal
// replay or append, and rejects a mismatched reuse of the state directory.
func bindDispatchIdentity(dir string, opts Options, abs string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := identityPath(dir)
	if b, err := os.ReadFile(path); err == nil {
		var id boundIdentity
		if err := json.Unmarshal(b, &id); err != nil {
			return fmt.Errorf("graphsession identity: %w", err)
		}
		st := &State{RepoID: id.RepoID, ContextID: id.ContextID, SinkID: id.SinkID, Checkout: id.Checkout}
		return identityOK(st, opts, abs)
	} else if !os.IsNotExist(err) {
		return err
	}
	if st, err := loadCommittedState(dir); err != nil {
		return err
	} else if st != nil {
		if err := identityOK(st, opts, abs); err != nil {
			return err
		}
	}
	if p, err := loadPendingState(dir); err != nil {
		return err
	} else if p != nil {
		if err := identityOK(p, opts, abs); err != nil {
			return err
		}
	}
	id := boundIdentity{RepoID: opts.RepoID, ContextID: opts.ContextID, SinkID: opts.SinkID, Checkout: abs}
	raw, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadCommittedState(dir string) (*State, error) {
	return readStateFile(statePath(dir))
}

func loadPendingState(dir string) (*State, error) {
	return readStateFile(pendingStatePath(dir))
}

func readStateFile(path string) (*State, error) {
	tRead := time.Now()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	graphprofile.Since("state_read_bytes", tRead, fmt.Sprintf("%s bytes=%d", filepath.Base(path), len(b)))
	tJSON := time.Now()
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("graphsession state %s: %w", filepath.Base(path), err)
	}
	graphprofile.Since("state_json_unmarshal", tJSON, fmt.Sprintf("files=%d", len(st.Files)))
	if st.Files == nil {
		st.Files = map[string]*FileState{}
	}
	if st.Synthetic == nil {
		st.Synthetic = map[string][]facts.Fact{}
	}
	if st.ExtractorInputHash == nil {
		st.ExtractorInputHash = map[string]string{}
	}
	if st.ExtractorSynthetic == nil {
		st.ExtractorSynthetic = map[string]map[string][]facts.Fact{}
	}
	return &st, nil
}

// recoverAcknowledgedPending promotes pending-state.json only when that
// generation's EndReplace is present and acknowledged in the journal.
// An empty unacked set is not enough: a crash after writePending and
// before End leaves prior messages acked with no End, and must not
// install the unacknowledged generation.
func recoverAcknowledgedPending(dir string, journal *graphstream.Journal, opts Options, abs string) (*State, error) {
	pending, err := loadPendingState(dir)
	if err != nil {
		return nil, err
	}
	if pending == nil || !pending.LastComplete || pending.LastRunID == "" {
		return loadCommittedState(dir)
	}
	if err := identityOK(pending, opts, abs); err != nil {
		// Pending belongs to a different repo/context/sink; leave it in place
		// and use the committed checkpoint.
		return loadCommittedState(dir)
	}
	if !journalHasAckedEnd(journal, pending.LastRunID) {
		return loadCommittedState(dir)
	}
	if err := promotePendingState(dir); err != nil {
		return nil, err
	}
	return pending, nil
}

// journalHasAckedEnd uses metadata decoded when the journal was appended or opened.
// Avoid copying and decoding every retained payload at the checkpoint barrier.
func journalHasAckedEnd(j *graphstream.Journal, runID string) bool {
	return j.HasAckedEnd(runID)
}

func writePendingState(dir string, st *State) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if st.Synthetic == nil {
		st.Synthetic = map[string][]facts.Fact{}
	}
	if st.ExtractorInputHash == nil {
		st.ExtractorInputHash = map[string]string{}
	}
	if st.ExtractorSynthetic == nil {
		st.ExtractorSynthetic = map[string]map[string][]facts.Fact{}
	}
	tJSON := time.Now()
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	graphprofile.Since("state_json_marshal", tJSON, fmt.Sprintf("bytes=%d files=%d", len(b), len(st.Files)))
	path := pendingStatePath(dir)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return fsyncDir(dir)
}

func promotePendingState(dir string) error {
	pending := pendingStatePath(dir)
	if _, err := os.Stat(pending); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Rename(pending, statePath(dir)); err != nil {
		return err
	}
	return fsyncDir(dir)
}

func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func saveState(dir string, st *State) error {
	if err := writePendingState(dir, st); err != nil {
		return err
	}
	return promotePendingState(dir)
}
