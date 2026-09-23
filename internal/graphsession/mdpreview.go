package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphprofile"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/plugin"
)

const mdintentExtractor = "mdintent"

// preparedMD is the single mdintent extraction a run performs, made before the
// frozen Begin so the manifest can name the pages whose facts actually move.
// The extraction site consumes it instead of compiling every page again.
type preparedMD struct {
	facts []facts.Fact
	// owners is the replacement scope this output justifies: the owners whose
	// contribution differs from the stored one, and nothing else.
	owners []graphstream.OwnerRef
}

// prepareMDScope previews mdintent before Begin and reports the file owners it
// changes, so the frozen manifest carries those pages instead of every markdown
// file in the repository.
//
// Only mdintent is previewed. Declaring file ownership says which files an
// extractor's facts belong to; it does not say that the extractor reads only
// those files, nor that re-reading them yields the same facts, and a manifest
// frozen on a wrong answer fails the run. mdintent is previewable because this
// function supplies its bytes: it captures every content input once, checks the
// captured bytes against the hashes this run will record, and compiles from the
// capture. Without that fence the comparison could pass on bytes the state then
// attributes to a different revision.
//
// A markdown page cannot be judged from its cached relations alone. Links
// resolve against the walked file set, so adding the target of a previously
// dead link, or deleting the last child of a linked directory, changes what an
// otherwise untouched page emits. Compiling is the only way to see that, which
// is why the preview extracts rather than inspecting the cache.
//
// A false second result means the preview declined: this extractor keeps its
// conservative whole-extractor seed and reads from disk at the extraction site
// as before. The caller must consult that result rather than s.preparedMD,
// which a previewed mdintent leaves set for the whole run and which therefore
// says nothing about the extractor being seeded now.
//
// An error fails the run ahead of Begin, with the completed state untouched: a
// malformed declaration, and equally a page this run hashed that has since
// changed or become unreadable. Those are not reasons to fall back, because the
// ordinary extraction would read the moved tree and publish facts the state
// records under the hashes of a revision that is gone.
func (s *session) prepareMDScope(ctx context.Context, ext plugin.Extractor, invFiles []string, prevFiles map[string]*FileState, hashes map[string]string, repoID string, conservative bool) ([]string, bool, error) {
	if conservative || s.state == nil || s.fast {
		return nil, false, nil
	}
	md, ok := ext.(*mdintent.Extractor)
	if !ok {
		return nil, false, nil
	}
	// Only pages this run hashed can be fenced, and a page it did not hash is
	// not evidence of anything having moved: graph input policy decides what is
	// hashed, and the extractor's own content predicate is wider than that. So
	// an unhashed page declines the preview instead of failing the run, while
	// everything below - where a hash exists to compare against - fails closed.
	for _, rel := range invFiles {
		if !md.ContentInput(rel) {
			continue
		}
		if _, known := lookupHash(hashes, rel); !known {
			return nil, false, nil
		}
	}
	// The work this preview does is work the extraction site no longer does, so
	// it is timed here under its own phase rather than disappearing from the
	// profile. It is not counted as non_ts_extract: the extraction site skips
	// that timer for a prepared run, so the two never double-count a page.
	tPrev := time.Now()
	src, err := md.CaptureInputs(s.abs, invFiles)
	if err != nil {
		// Every content input was readable when this run hashed it, so one that
		// cannot be read now is the tree moving mid-transaction. The ordinary
		// extraction skips an unreadable page, which would publish its owner as
		// empty and record that as the page's contribution.
		return nil, false, fmt.Errorf("%w: %w; refusing to plan markdown from a partial capture", ErrInputsChanged, err)
	}
	for rel, b := range src {
		h, known := lookupHash(hashes, rel)
		sum := sha256.Sum256(b)
		if !known || h != hex.EncodeToString(sum[:]) {
			// The bytes the preview would plan from are not the bytes whose
			// hashes this run records - including an edit that lands back on
			// the original content, which a before/after re-read cannot see.
			// Publishing from them would attribute facts to a revision the
			// state does not claim.
			return nil, false, fmt.Errorf("%w: markdown input %s changed during the run; refusing to plan from bytes this run did not hash", ErrInputsChanged, rel)
		}
	}
	extracted, err := md.ExtractCaptured(ctx, s.abs, invFiles, src)
	if err != nil {
		var fatal *plugin.FatalError
		if asFatal(err, &fatal) {
			return nil, false, err
		}
		return nil, false, nil
	}
	// The extraction site's own post-processing, in its order, so the facts the
	// comparison sees are the facts that will be published.
	applyLocalIO(extracted)
	tagRepo(extracted, repoID)

	owned := ownedFiles(ext, invFiles)
	prior := append(cachedFactsFor(mdintentExtractor, owned, prevFiles),
		cloneTagged(syntheticFactsFor(s.state, mdintentExtractor), repoID)...)
	owners := changedExtractorOwners(mdintentExtractor, extracted, prior, prevFiles, s.state, repoID)
	graphprofile.Since("md_scope_preview", tPrev, fmt.Sprintf("pages=%d facts=%d owners=%d", len(src), len(extracted), len(owners)))
	// The capture joins the sources the rest of the run holds, which are read
	// back and compared before EndReplace. The hash fence above only proves the
	// bytes were current when the preview ran; a page edited after it - while
	// the TypeScript half of the same transaction is still parsing - has to fail
	// the run rather than leave a committed state describing bytes that are gone.
	s.mergeCaptured(src)
	s.preparedMD = &preparedMD{facts: extracted, owners: owners}

	var extra []string
	for _, o := range owners {
		if o.Kind == graphstream.OwnerFile {
			extra = append(extra, o.ID)
		}
	}
	// A page whose own facts are unchanged can still be why another owner must
	// be replanned: a renamed heading moves a candidate name that an untouched
	// page or source resolves against. ownersForNameDelta previews TypeScript
	// sources only - the session's dirty set is built from IsSessionSource - so
	// the markdown candidate delta is unioned here rather than left to the
	// fail-closed check at publication.
	extra = append(extra, ownersForCandidateNameDelta(prevFiles, prior, extracted)...)
	return extra, true, nil
}

// takePreparedMD hands the prepared extraction to the extraction site once.
func (s *session) takePreparedMD(name string) *preparedMD {
	if name != mdintentExtractor || s.preparedMD == nil {
		return nil
	}
	p := s.preparedMD
	s.preparedMD = nil
	return p
}

// changedExtractorOwners compares one extractor's fresh output against its
// stored contribution owner by owner, and returns only the owners whose facts
// differ. New owners, changed owners and owners that stopped emitting - a
// deleted page, or one that now yields nothing - all fall out of the same
// comparison, so a narrowed replacement still retires everything it must.
//
// Synthetic owners are compared like any other and returned alongside the file
// owners: a coverage counter that moved is a real replacement. They are not
// members of the frozen file manifest, so only the caller's file owners seed it.
func changedExtractorOwners(name string, extracted, prior []facts.Fact, prevFiles map[string]*FileState, st *State, repoID string) []graphstream.OwnerRef {
	refs := map[string]graphstream.OwnerRef{}
	group := func(ff []facts.Fact, into map[string][]facts.Fact) {
		for _, f := range ff {
			o := ownerOf(f)
			key := o.String()
			refs[key] = o
			into[key] = append(into[key], f)
		}
	}
	next := map[string][]facts.Fact{}
	group(extracted, next)
	old := map[string][]facts.Fact{}
	group(prior, old)
	// An owner the state records for this extractor but that carries facts on
	// neither side is named from the stored ownership rather than inferred.
	for _, o := range retireExtractorOwners(st, prevFiles, name) {
		if _, ok := refs[o.String()]; !ok {
			refs[o.String()] = o
		}
	}
	var out []graphstream.OwnerRef
	for key, o := range refs {
		if factsFingerprint(old[key]) == factsFingerprint(next[key]) {
			continue
		}
		out = append(out, o)
	}
	graphstream.SortOwners(out)
	return out
}
