package graphsession

import (
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
)

// aliasScope answers, for one run, whether a moved alias declaration reaches a
// particular file.
//
// The alias projection is repository-wide in shape - mergePackageAliases hands
// every directory every package alias - but not in effect: a file's resolution
// only moves when a declaration it could actually bind through moves. Hashing
// the whole map into each file's context conflated the two, so publishing an
// unrelated package invalidated the repository. The declarations are projected
// per key instead, and the question is asked here, per file, against the record
// that file contributed before the change.
//
// Both sides of that question come from projections of configuration bytes, so a
// run whose configuration did not move has no changed keys at all and consults
// no record. That is what keeps an immediate second run silent, and what keeps
// the captured-input fence satisfied: nothing here moves when the cache behind
// it is rewritten mid-run.
type aliasScope struct {
	changes []tsextractor.AliasChange
	// merged is the alias map per alias root, built once per root that a file
	// in this run actually resolves against.
	merged map[string]map[string]tsextractor.AliasEntry
	after  map[string]string
	// structured is false against a state written before the per-key entries
	// existed. There is then nothing to diff per key, and the question is
	// answered by that state's own alias-inclusive per-file digests instead.
	structured bool
}

// aliasStateMode reports how the stored state's alias projection may be read.
// The marker is matched exactly, not merely for presence: a state written by a
// build that projects something else would otherwise be compared per key
// against entries that do not mean the same thing. An unknown marker, and a
// state claiming this one without the map that gives it meaning, are both
// refused rather than guessed at.
func (s *session) aliasStateMode() tsextractor.AliasStateMode {
	if s.state == nil {
		return tsextractor.AliasStateLegacy
	}
	return tsextractor.AliasStateModeFor(s.state.TSAliasMeta, len(s.state.TSFileContext), len(s.state.TSFileBase))
}

func (s *session) aliasScopeFor(input *runtimeInputs) *aliasScope {
	if s.aliasScopeCache != nil && s.aliasScopeInput == input {
		return s.aliasScopeCache
	}
	sc := &aliasScope{
		structured: s.aliasStateMode() == tsextractor.AliasStateStructured,
		merged:     map[string]map[string]tsextractor.AliasEntry{},
		after:      input.tsContext,
	}
	if sc.structured {
		sc.changes = tsextractor.AliasContextChanges(s.state.TSContext, input.tsContext)
	}
	s.aliasScopeCache = sc
	s.aliasScopeInput = input
	return sc
}

func (a *aliasScope) aliasesFor(root string, hasRoot bool) map[string]tsextractor.AliasEntry {
	cacheKey := "-"
	if hasRoot {
		cacheKey = "r:" + root
	}
	if m, ok := a.merged[cacheKey]; ok {
		return m
	}
	m := tsextractor.AliasesForRoot(a.after, root, hasRoot)
	a.merged[cacheKey] = m
	return m
}

// affects reports whether any alias declaration this run moved reaches the file,
// given the record it contributed under the previous declarations. A file that
// resolves against no alias root still sees package `exports` aliases, which are
// keyed under the empty root.
func (a *aliasScope) affects(fileContext string, rec *tsextractor.FileRecord) bool {
	if len(a.changes) == 0 {
		return false
	}
	root, hasRoot := tsextractor.FileAliasRoot(fileContext)
	var merged map[string]tsextractor.AliasEntry
	for _, ch := range a.changes {
		// A tsconfig declaration only reaches files whose nearest alias root is
		// the one that declared it; a package alias reaches every file.
		if !ch.Package && (!hasRoot || ch.Root != root) {
			continue
		}
		if merged == nil {
			merged = a.aliasesFor(root, hasRoot)
		}
		if tsextractor.AliasChangeAffects(ch, merged, rec) {
			return true
		}
	}
	return false
}

// tsFileContextMovedFor is the single per-file question every seed asks: did this
// file's own projection move, or did a declaration it resolves through move. The
// two are kept apart deliberately - the first is a digest comparison that needs
// no record, the second is a scoped answer that does.
func (s *session) tsFileContextMovedFor(f string, rec *tsextractor.FileRecord, input *runtimeInputs) bool {
	switch s.aliasStateMode() {
	case tsextractor.AliasStateLegacy:
		// Exactly the comparison the stored state was written under. The
		// alias set is inside both digests here, so an alias that moved still
		// dirties the file; what it cannot do is answer per key, which is why
		// the aggregate fallback stays in force for such a state.
		return s.state.TSFileContext[f] != input.tsFileContext[f]
	case tsextractor.AliasStateStructured:
		base := input.tsFileBase[f]
		if s.state.TSFileBase[f] != base {
			return true
		}
		return s.aliasScopeFor(input).affects(base, rec)
	default:
		// Nothing stored can be compared, so nothing stored is believed.
		return true
	}
}
