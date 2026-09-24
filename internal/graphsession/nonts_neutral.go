package graphsession

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphprofile"
)

// nonTSPreview is the single extraction a run performs for one non-TypeScript
// extractor, made before Begin from a fenced snapshot of that extractor's
// inputs. Both questions the run asks about that extractor are answered from
// it: whether its output moved at all, and - when it did - which owners its
// candidate names retarget. The extraction site publishes these same facts
// rather than reading the tree a second time.
type nonTSPreview struct {
	facts    []facts.Fact
	reusable bool
	owned    []string
	// neutral records that this output is the output the stored state already
	// carries, proven against inputs this run hashed for itself.
	neutral bool
	// input is the extractor input digest observed alongside a neutral proof,
	// so a run that publishes nothing can still record what it was proved
	// against.
	input string
}

// proveNonTSNeutrality answers, before anything is published, whether a
// non-TypeScript extractor this run needs actually produces a different graph.
//
// The need is raised from input hashes: a manifest whose own version field
// moved is a changed input, and every extractor owning a changed file has to
// run. Running it is not the same as changing the graph, and the run used to
// find that out too late to act on it - the whole-output fingerprint comparison
// lives in the extraction loop, which is reached only after the invalidation
// plan is frozen and a Begin announcing the manifest owner has been published.
// A version bump therefore published a replacement of itself, advanced a
// generation and woke every consumer, for a graph identical down to the byte.
//
// The comparison needs nothing the plan produces, so it is made first. The
// extraction is the one the extraction site would have performed anyway and is
// handed to it through s.preparedNonTS; what is new is that its fingerprint is
// available while the run can still decide not to publish at all. An extractor
// whose output is proven identical has its need discharged, and a run left with
// no other reason to publish falls through to the no-publication path, which
// records the observations this proof was made against.
//
// Only a fenced capture can carry the proof. Reading the tree, extracting from
// it and comparing afterwards would agree with itself across an edit restored
// between the two reads; captureAndExtract instead compiles from a snapshot and
// then proves the snapshot is the state this run fingerprinted. An extractor
// that cannot be captured, or that has no recorded digest to compare against,
// keeps its need and publishes exactly as before.
//
// A conservative run - forced, or with changed configuration behind it - is
// left alone. It replaces the whole domain regardless, and the extraction it
// would make here is not the one it publishes.
func (s *session) proveNonTSNeutrality(ctx context.Context, needByExt map[string]bool, detected map[string]bool, extractorDigest map[string]string, invFiles, allNames []string, hashes map[string]string, fileSetHash, scanHash, repoID string, conservative bool) error {
	// The discharges this proof feeds are all inside the frozen file-owner
	// contract - the scan digest, the raw configuration fingerprint and the
	// no-publication return that records what was observed - so the legacy
	// path is left exactly as it was rather than given an untested
	// short-circuit.
	if s.state == nil || s.fast || conservative || !s.opts.AuthoritativeFiles {
		return nil
	}
	for _, ext := range s.eng.Extractors() {
		name := ext.Name()
		if name == "typescript" || !needByExt[name] || !detected[name] {
			continue
		}
		t := time.Now()
		src, extracted, reusable, err := s.captureAndExtract(ctx, ext, invFiles, hashes)
		if err != nil {
			return err
		}
		if extracted == nil {
			continue
		}
		applyLocalIO(extracted)
		tagRepo(extracted, repoID)
		pv := &nonTSPreview{facts: extracted, reusable: reusable, owned: ownedFiles(ext, invFiles)}
		if reusable && extractorDigest[name] != "" && factsFingerprint(extracted) == extractorDigest[name] {
			pv.neutral = true
			pv.input = extractorInputDigest(ext, pv.owned, invFiles, allNames, hashes, fileSetHash, scanHash)
			needByExt[name] = false
		}
		s.rememberNonTSPreview(name, src, pv)
		graphprofile.Since("non_ts_neutral_proof", t, fmt.Sprintf("%s neutral=%v fenced=%v facts=%d", name, pv.neutral, reusable, len(extracted)))
	}
	return nil
}

// rememberNonTSPreview keeps one preview for the rest of the run. A fenced
// capture joins the sources read back before the run commits, so an input
// edited after the fence fails the run rather than leaving a committed state
// describing bytes that are gone. Only a preview the extraction site still has
// to publish is handed to it; a neutral one publishes nothing, and leaving it
// staged would have the site adopt facts it is not going to store.
func (s *session) rememberNonTSPreview(name string, src map[string][]byte, pv *nonTSPreview) {
	if s.nonTSPreviews == nil {
		s.nonTSPreviews = map[string]*nonTSPreview{}
	}
	s.nonTSPreviews[name] = pv
	if !pv.reusable {
		return
	}
	s.mergeCaptured(src)
	if pv.neutral {
		return
	}
	if s.preparedNonTS == nil {
		s.preparedNonTS = map[string][]facts.Fact{}
	}
	s.preparedNonTS[name] = pv.facts
}

// neutralNonTSPreviews returns the extractors this run proved produce the
// output the stored state already carries.
func (s *session) neutralNonTSPreviews() map[string]*nonTSPreview {
	var out map[string]*nonTSPreview
	for name, pv := range s.nonTSPreviews {
		if pv == nil || !pv.neutral {
			continue
		}
		if out == nil {
			out = map[string]*nonTSPreview{}
		}
		out[name] = pv
	}
	return out
}

// scanChangeNeutral reports whether the stored scan digest comes back once every
// file belonging to a proven-neutral extractor is put back at the bytes the
// stored state recorded for it.
//
// The digest covers every semantic name and its content, so a manifest's own
// version field moves it exactly as a source edit would, and comparing digests
// can only say that something moved. Substituting the recorded bytes answers
// which: if the stored digest returns, the whole difference was in files whose
// extractor has already proved its output unchanged. A source edit, a name that
// appeared or vanished, or a file no prior state describes leaves the two apart
// and the run publishes.
func scanChangeNeutral(st *State, semantic []string, hashes map[string]string, prevFiles map[string]*FileState, neutral map[string]*nonTSPreview) bool {
	if st == nil || st.ScanHash == "" || len(neutral) == 0 {
		return false
	}
	restored, appeared := restoreNeutralHashes(hashes, prevFiles, neutral)
	return st.ScanHash == inventoryDigest(withoutAppeared(semantic, appeared), restored)
}

// restoreNeutralHashes puts the content hashes a proven-neutral extractor's
// files had when the stored state was written back over the current ones, and
// names the files it could not put back because the stored state does not
// describe them.
func restoreNeutralHashes(hashes map[string]string, prevFiles map[string]*FileState, neutral map[string]*nonTSPreview) (map[string]string, map[string]bool) {
	restored := make(map[string]string, len(hashes))
	for k, v := range hashes {
		restored[k] = v
	}
	appeared := map[string]bool{}
	for _, pv := range neutral {
		for _, f := range pv.owned {
			prev := lookupState(prevFiles, f)
			if prev == nil || prev.Hash == "" {
				// Nothing is recorded for this file because it was not in the
				// tree when the stored state was written. Putting the tree back
				// means taking it out again, not giving it a content hash it
				// never had. A file that vanished is not restored here at all:
				// its name would have to be put back, and the current inventory
				// no longer says which extractor owned it, so a deletion leaves
				// the digests apart and the run publishes.
				appeared[filepath.ToSlash(f)] = true
				continue
			}
			restored[filepath.ToSlash(f)] = prev.Hash
		}
	}
	return restored, appeared
}

// withoutAppeared drops the names restoreNeutralHashes could not restore, so the
// comparison is against the name set the stored digest was taken over.
func withoutAppeared(names []string, appeared map[string]bool) []string {
	if len(appeared) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, f := range names {
		if appeared[filepath.ToSlash(f)] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// tsFileContextMoved reports whether a configuration edit moved some source's
// own semantic context - the package it belongs to, the alias roots its
// specifiers resolve against - without moving its bytes.
//
// Neither digest above can see this. The scan digest hashes names and content,
// and no byte moved; the session context is the repository-wide projection, and
// a package rename leaves it alone while changing the nearest package of every
// file under it. The plan block seeds exactly these files as dirty and reparses
// them, so a run deciding it has nothing to do has to ask the same question
// before it skips the plan entirely.
func (s *session) tsFileContextMoved(current []string, prevFiles map[string]*FileState, input *runtimeInputs) bool {
	if s.eng.GraphScope() == nil || s.state == nil {
		return false
	}
	for _, f := range current {
		st := lookupState(prevFiles, f)
		if st == nil || st.TS == nil {
			continue
		}
		if s.tsFileContextMovedFor(f, st.TS, input) {
			return true
		}
	}
	return false
}
