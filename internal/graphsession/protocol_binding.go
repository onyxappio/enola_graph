package graphsession

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/enola-labs/enola/internal/graphstream"
)

// Bind before journal replay: a legacy interrupted run must never be delivered
// into a context whose consumer expects the frozen file-only protocol.
func bindProtocol(dir string, frozen bool) error {
	want := graphstream.SchemaVersion
	if frozen {
		want = graphstream.FrozenSchemaVersion
	}
	p := filepath.Join(dir, "protocol")
	if b, err := os.ReadFile(p); err == nil {
		if string(b) != want {
			return fmt.Errorf("graph protocol mismatch: state uses %s, requested %s; use a new state directory and consumer context", b, want)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if frozen {
		for _, name := range []string{"state.json", "pending-state.json", "journal.jsonl"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return fmt.Errorf("legacy graph state requires explicit consumer migration: use a new state directory/context for enola.graph.v2")
			} else if !os.IsNotExist(err) {
				return err
			}
		}
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(want); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return fsyncDir(dir)
}
