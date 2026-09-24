package graphsession

import (
	"sort"

	"github.com/enola-labs/enola/internal/engine"
)

// claimedScanNames narrows the semantic name set to the names some active
// extractor claims, and reports whether every active extractor could answer.
//
// The scan digest covers every semantic name and its content, but a name no
// extractor owns contributes only its name: nothing reads the file, so nothing
// records a hash for it. That asymmetry is already visible in behaviour - an
// edit to such a file is silent, while its arrival or departure is not - and it
// is also why the neutrality proof cannot reach it. That proof restores the
// files a proven-neutral extractor owns, and an unclaimed file has no owner to
// prove neutral, so its name can never be put back or taken away.
//
// Narrowing to the claimed names asks the smaller question that can be
// answered: did any name an extractor claims move? It deliberately uses plain
// ownership rather than the module-candidate narrowing, because the question
// here is which files are read at all, not which could carry a module.
//
// Ownership is not the whole of what an extractor reads. An extractor may
// declare content it does not own - DeltaInputs.ContentInput is a wider question
// than OwnsFile - and such a file's bytes really do matter to it. Nothing here
// claims otherwise: what makes a name safe to set aside is that the extractor
// which reads it has already answered for it, by its declared input digest not
// moving, which is what the caller's !nonTSNeed term reports, and by TypeScript's
// resolution context not moving, which is the tsFileContextMoved term. This
// digest only supplies the remaining piece, that nothing anybody owns moved.
//
// One active extractor that does not declare its ownership leaves the claim set
// unknown rather than empty, and an unknown claim set proves nothing: a file
// this digest would call unclaimed might be an input that extractor reads, and
// an opaque extractor has no declared digest to have answered with. Such an
// engine reports bounded false and discharges nothing.
func claimedScanNames(eng *engine.Engine, detected map[string]bool, semantic []string) ([]string, bool) {
	if eng == nil {
		return nil, false
	}
	claimed := make(map[string]bool, len(semantic))
	bounded := true
	for _, ext := range eng.Extractors() {
		if !detected[ext.Name()] {
			continue
		}
		if !declaresFileOwnership(ext) {
			bounded = false
			continue
		}
		for _, f := range ownedFiles(ext, semantic) {
			claimed[f] = true
		}
	}
	if !bounded {
		return nil, false
	}
	out := make([]string, 0, len(claimed))
	for f := range claimed {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, true
}

// scanMembershipNeutral reports whether the stored claimed-name digest comes
// back once every proven-neutral extractor's files are restored to the bytes the
// stored state recorded for them.
//
// It is the second half of the same argument scanChangeNeutral makes, over the
// smaller name set: if the claimed digest returns while the full scan digest did
// not, the whole remaining difference is in names nobody owns. The two discharges
// share one restoration so that a manifest's own version field moving and an
// unclaimed name arriving reconcile together rather than each defeating the
// other.
//
// An empty neutral set is not a reason to refuse. With nothing to put back the
// restoration is the identity and this compares the stored claim digest against
// the current one, which is exactly the question in a repository whose only
// extractor is TypeScript - the case that has no preview to offer and is
// otherwise the one left paying for an unclaimed name.
//
// A name-set-sensitive extractor is the reader for which an unclaimed name is
// still an input, and it raises its own need for exactly that reason; so does
// TypeScript when its resolution context moves. This is reached only once both
// have been discharged, by the caller's !nonTSNeed and tsFileContextMoved
// guards, which stay authoritative: they, not ownership, are what make setting a
// name aside safe.
func scanMembershipNeutral(st *State, claimed []string, bounded bool, hashes map[string]string, prevFiles map[string]*FileState, neutral map[string]*nonTSPreview) bool {
	if st == nil || !bounded || st.ScanClaimedMeta != claimedScanVersion || st.ScanClaimedHash == "" {
		return false
	}
	restored, appeared := restoreNeutralHashes(hashes, prevFiles, neutral)
	return st.ScanClaimedHash == inventoryDigest(withoutAppeared(claimed, appeared), restored)
}
