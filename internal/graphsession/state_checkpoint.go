package graphsession

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/enola-labs/enola/internal/graphprofile"
)

// A committed state file runs to tens of megabytes of JSON, and decoding it
// costs an order of magnitude more than reading it. A resident session whose
// transaction refused already held the state that file contains, so the
// recovery barrier re-reads and re-decodes bytes this process understood a
// moment earlier. Serving the decoded value from memory is only sound against
// a proof that the file still holds the bytes it came from, and a
// stateFingerprint is that proof.
//
// The fingerprint deliberately does not lean on size and modification time.
// Every committed write today lands by renaming a freshly created file over
// state.json, so nothing rewrites it in place - but a proof that holds only
// while that remains true is not a proof, and a same-length write inside one
// mtime tick would pass both fields unnoticed.
type stateFingerprint struct {
	size   int64
	digest [sha256.Size]byte
}

func (f stateFingerprint) known() bool { return f.size > 0 }

func fingerprintStateBytes(b []byte) stateFingerprint {
	return stateFingerprint{size: int64(len(b)), digest: sha256.Sum256(b)}
}

// stateReadWork is what the state loads behind one transaction cost: the files
// decoded, the decodes a fingerprint proved unnecessary, and the digest volume
// that proof took. A nil accumulator is accepted so the paths with nobody to
// report to - opening a session, the plain wrappers - stay unchanged.
type stateReadWork struct {
	decodes, proven, fingerprints int
	fingerprintBytes              int64
}

func (w *stateReadWork) decoded() {
	if w != nil {
		w.decodes++
	}
}

func (w *stateReadWork) reused() {
	if w != nil {
		w.proven++
	}
}

func (w *stateReadWork) digested(n int) {
	if w != nil {
		w.fingerprints++
		w.fingerprintBytes += int64(n)
	}
}

func (w stateReadWork) fold(c *WorkCounters) {
	c.StateDecodes += w.decodes
	c.StateDecodesProven += w.proven
	c.StateFingerprints += w.fingerprints
	c.StateFingerprintBytes += w.fingerprintBytes
}

// stateCheckpoint is a decoded committed State a resident still holds,
// together with the fingerprint of the bytes it was decoded from.
//
// The pairing is unique and permanent because a decoded State is never
// modified after it is adopted. A run copies the owner map shallowly and every
// mutation helper clones the record it is about to change and stores the clone
// back into its own copy - the invariant session.go states as "FileState
// records are immutable for the duration of a run" - so a run that shares
// record pointers with an earlier state, mutates its own view and then fails
// leaves the earlier state exactly as it was serialized. Synthetic facts are
// cloned on the same principle, and a published generation is assembled into a
// freshly allocated State rather than over the old one.
//
// Nothing here claims anything about what the failed run published. It does not
// need to: the barrier below still runs every recovery decision against the
// disk, and an acknowledged pending generation is always read and promoted. The
// only claim is that these bytes decode to this value, so reading the bytes and
// finding them unchanged is a complete substitute for decoding them again.
type stateCheckpoint struct {
	state *State
	fp    stateFingerprint
}

func newStateCheckpoint(st *State, fp stateFingerprint) stateCheckpoint {
	if st == nil || !fp.known() {
		return stateCheckpoint{}
	}
	return stateCheckpoint{state: st, fp: fp}
}

func (c stateCheckpoint) usable() bool { return c.state != nil && c.fp.known() }

// reuseCommittedState answers a recovery barrier's committed-state read from
// the checkpoint the resident holds, against one proof gathered here: the
// committed file still holds the exact bytes the checkpoint was decoded from.
//
// Every recovery decision above this point still runs against the disk on
// every call - unacked journal entries are replayed, pending-state.json is
// loaded and decoded, LastComplete and LastRunID are checked, identityOK runs,
// the acknowledged End is looked up, and the promotion itself renames. This
// function is reached only from the branches that had already decided to fall
// back to the committed checkpoint, so a promoted generation is never served
// from memory. Whether the failed transaction published, appended an End, or
// left a pending file behind changes which of those branches is taken; it
// cannot change what the committed bytes decode to.
//
// The read is not avoided, only the decode. Keeping a second copy of the state
// bytes to compare against would cost the memory the decode was trying to
// save, so the digest is taken over the bytes as they arrive and the bytes are
// released again.
func reuseCommittedState(dir string, ck stateCheckpoint, w *stateReadWork) (*State, stateFingerprint, error) {
	if !ck.usable() {
		return readStateFileFP(statePath(dir), w)
	}
	path := statePath(dir)
	tRead := time.Now()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// The checkpoint claimed a committed file that is gone. Report the
			// disk, not the memory.
			return nil, stateFingerprint{}, nil
		}
		return nil, stateFingerprint{}, err
	}
	graphprofile.Since("state_read_bytes", tRead, fmt.Sprintf("%s bytes=%d", filepath.Base(path), len(b)))
	tFP := time.Now()
	fp := fingerprintStateBytes(b)
	w.digested(len(b))
	graphprofile.Since("state_fingerprint", tFP, fmt.Sprintf("bytes=%d", len(b)))
	if fp == ck.fp {
		w.reused()
		graphprofile.Log("state_decode_reused", 0, fmt.Sprintf("files=%d bytes=%d", len(ck.state.Files), len(b)))
		return ck.state, fp, nil
	}
	st, err := decodeStateBytes(path, b)
	if err != nil {
		return nil, stateFingerprint{}, err
	}
	w.decoded()
	return st, fp, nil
}
