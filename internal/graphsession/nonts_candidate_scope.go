package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/pkg/plugin"
)

// capturedContextExtractor snapshots its complete input set - including reads
// the engine walk never sees - and returns the delta-context digest those exact
// bytes produce, so a caller can both extract from the snapshot and prove the
// snapshot is the input state this run already fingerprinted.
type capturedContextExtractor interface {
	CaptureContext(repoPath string) (map[string][]byte, string)
	ExtractCaptured(ctx context.Context, repoPath string, files []string, src map[string][]byte) ([]facts.Fact, error)
}

// capturedInputsExtractor extracts from a snapshot of its declared content
// inputs. It is only fenceable when those inputs are the whole of what it reads,
// which is what implementing DeltaInputs and not DeltaContext asserts.
type capturedInputsExtractor interface {
	CaptureInputs(repoPath string, files []string) (map[string][]byte, error)
	ExtractCaptured(ctx context.Context, repoPath string, files []string, src map[string][]byte) ([]facts.Fact, error)
}

// prepareNonTSCandidateScope previews one non-TypeScript extractor before Begin
// and returns the owners whose resolution its new contribution moves.
//
// Seeding an extractor's own files is not the same as bounding its effect. The
// graph resolver indexes Fact.Name globally and without a kind filter, so a
// manifest that starts declaring pkg:npm/left-pad newly resolves a reference in
// a source whose bytes, package and alias roots are all unchanged. That owner
// has no file dependency on the manifest and no TypeScript context delta, so
// nothing else in the plan reaches it; frozen Begin then fails closed on a
// resolution owner the manifest does not carry. Removing a dependency, and
// declaring one whose name collides with an existing candidate, are the same
// edge seen from the other two directions.
//
// The names cannot be guessed from the cache - only the extractor knows what it
// is about to emit - so this runs it, once, before Begin, and hands the result
// to the extraction site through s.preparedNonTS rather than letting it read the
// tree a second time. ownersForCandidateNameDelta then answers the question
// exactly, over prior contributions that include owners this run no longer
// walks, so a deleted or renamed manifest still retires the names it declared.
//
// The preview is only allowed to run from a capture, because its facts are the
// facts that get published. Reading the tree, extracting from the tree and
// hashing the tree afterwards would agree with itself across an edit that is
// restored before the second read, and would publish the transient bytes; both
// forms below instead compile from a snapshot and then prove what the snapshot
// is. An extractor whose reads reach outside the engine walk - which
// implementing plugin.DeltaContext declares - is fenced by re-deriving that
// context digest from the snapshot and comparing it against the value this run
// recorded, which covers the engine-pruned inputs it discovers for itself and
// the absent ones that contribute only a read error. An extractor whose content
// inputs are all it reads is fenced per file against this run's hashes.
//
// An extractor that can be fenced neither way declines. Planning from a live
// extraction would be planning from bytes nothing in this run vouches for, and
// re-reading at the extraction site does not recover the guarantee: the same
// edit-and-restore race is available to the second read. The boundary stays
// explicit instead, at the cost of the whole domain for that extractor's runs
// until it can extract from a capture.
//
// A false second result means the preview declined, and the caller must keep the
// conservative whole prior/current domain for this extractor: an unbounded
// candidate delta is exactly what a seed of its own files cannot cover. An error
// fails the run before Begin, with the completed state untouched.
func (s *session) prepareNonTSCandidateScope(ctx context.Context, ext plugin.Extractor, invFiles []string, prevFiles map[string]*FileState, hashes map[string]string, repoID string) ([]string, bool, error) {
	if s.state == nil || s.fast {
		return nil, false, nil
	}
	tPrev := time.Now()
	// The neutrality proof this run already made is that same capture and that
	// same extraction, so the candidate delta is derived from it rather than
	// read again. A run that skipped the proof - or an extractor it could not
	// reach - still previews here.
	pv := s.nonTSPreviews[ext.Name()]
	if pv == nil {
		src, extracted, reusable, err := s.captureAndExtract(ctx, ext, invFiles, hashes)
		if err != nil {
			return nil, false, err
		}
		if extracted == nil {
			return nil, false, nil
		}
		applyLocalIO(extracted)
		tagRepo(extracted, repoID)
		pv = &nonTSPreview{facts: extracted, reusable: reusable, owned: ownedFiles(ext, invFiles)}
		s.rememberNonTSPreview(ext.Name(), src, pv)
	}

	prior := append(cachedFactsFor(ext.Name(), priorContributingFiles(ext, invFiles, prevFiles), prevFiles),
		cloneTagged(syntheticFactsFor(s.state, ext.Name()), repoID)...)
	owners := ownersForCandidateNameDelta(prevFiles, prior, pv.facts)
	graphprofile.Since("non_ts_scope_preview", tPrev, fmt.Sprintf("%s facts=%d owners=%d", ext.Name(), len(pv.facts), len(owners)))
	return owners, true, nil
}

// captureAndExtract snapshots an extractor's inputs, proves the snapshot is the
// state this run fingerprinted, and compiles the extractor's output from it. A
// nil fact slice with a nil error means this extractor cannot be previewed at
// all. A false third result means the output may be planned with but not
// published: it did not come from a fenced snapshot, so the extraction site must
// still read for itself.
func (s *session) captureAndExtract(ctx context.Context, ext plugin.Extractor, invFiles []string, hashes map[string]string) (map[string][]byte, []facts.Fact, bool, error) {
	if cc, ok := ext.(capturedContextExtractor); ok {
		if _, declared := ext.(plugin.DeltaContext); declared {
			recorded, known := hashes[deltaContextKey(ext)]
			if !known || recorded == "" {
				// Without the recorded digest there is nothing to compare the
				// snapshot against, so it cannot be shown to be this run's inputs.
				return nil, nil, false, nil
			}
			src, sum := cc.CaptureContext(s.abs)
			if sum != recorded {
				return nil, nil, false, fmt.Errorf("%w: %s inputs changed during the run; refusing to plan from bytes this run did not fingerprint", ErrInputsChanged, ext.Name())
			}
			out, err := cc.ExtractCaptured(ctx, s.abs, invFiles, src)
			if err != nil {
				return nil, nil, false, previewExtractError(err)
			}
			name, dc := ext.Name(), ext.(plugin.DeltaContext)
			s.previewFences = append(s.previewFences, func() error {
				// The digest, not the snapshot: recomputing it re-observes the
				// enumeration and every read error without retaining bytes.
				if sum := dc.DeltaContext(s.abs); sum != recorded {
					return fmt.Errorf("%w: %s context changed during the run; refusing successful EndReplace", ErrInputsChanged, name)
				}
				return nil
			})
			s.work.NonTSCaptures++
			return src, nonNilFacts(out), true, nil
		}
	}
	ci, hasCapture := ext.(capturedInputsExtractor)
	di, hasInputs := ext.(plugin.DeltaInputs)
	_, wider := ext.(plugin.DeltaContext)
	// The per-file fence covers an extractor's whole input set only when its
	// declared content inputs are all it reads, which is what implementing
	// DeltaInputs without DeltaContext asserts.
	if !hasCapture || !hasInputs || wider {
		return nil, nil, false, nil
	}
	for _, f := range invFiles {
		if !di.ContentInput(f) {
			continue
		}
		if _, known := lookupHash(hashes, filepath.ToSlash(f)); !known {
			// An input this run did not hash cannot be fenced at all.
			return nil, nil, false, nil
		}
	}
	src, err := ci.CaptureInputs(s.abs, invFiles)
	if err != nil {
		return nil, nil, false, fmt.Errorf("%w: %s: %v; refusing to plan from a partial capture", ErrInputsChanged, ext.Name(), err)
	}
	for rel, b := range src {
		h, known := lookupHash(hashes, rel)
		sum := sha256.Sum256(b)
		if !known || h != hex.EncodeToString(sum[:]) {
			return nil, nil, false, fmt.Errorf("%w: %s input %s changed during the run; refusing to plan from bytes this run did not hash", ErrInputsChanged, ext.Name(), rel)
		}
	}
	out, err := ci.ExtractCaptured(ctx, s.abs, invFiles, src)
	if err != nil {
		return nil, nil, false, previewExtractError(err)
	}
	s.work.NonTSCaptures++
	return src, nonNilFacts(out), true, nil
}

// previewExtractError keeps a fatal extraction fatal and turns any other failure
// into a decline: the extraction site logs and skips such an error, leaving the
// prior contribution in place, and planning from a partial output would announce
// a narrower candidate delta than that reality.
func previewExtractError(err error) error {
	var fatal *plugin.FatalError
	if asFatal(err, &fatal) {
		return err
	}
	return nil
}

// nonNilFacts keeps an empty but successful extraction distinguishable from a
// decline, since an extractor that now emits nothing is a real candidate delta.
func nonNilFacts(ff []facts.Fact) []facts.Fact {
	if ff == nil {
		return []facts.Fact{}
	}
	return ff
}

// takePreparedNonTS hands a pre-Begin extraction to the extraction site once.
func (s *session) takePreparedNonTS(name string) ([]facts.Fact, bool) {
	ff, ok := s.preparedNonTS[name]
	if ok {
		delete(s.preparedNonTS, name)
	}
	return ff, ok
}

// priorContributingFiles unions the files an extractor owns now with the files
// the stored state records it contributing to. A manifest that was deleted or
// renamed is in neither the walk nor the owned set, but the candidate names it
// used to declare still have to reach the delta as old->empty.
func priorContributingFiles(ext plugin.Extractor, invFiles []string, prevFiles map[string]*FileState) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range ownedFiles(ext, invFiles) {
		id := filepath.ToSlash(f)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for path, st := range prevFiles {
		if !extractorOwnsState(st, ext.Name()) {
			continue
		}
		id := filepath.ToSlash(path)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
