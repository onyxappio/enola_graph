package graphsession

import (
	"path/filepath"
	"sort"
)

// contributingExtractors names every extractor whose output a cached state
// carries: the owning extractor plus the ones that wrote additional facts or
// input hashes for the same file.
func contributingExtractors(st *FileState) []string {
	if st == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	add(st.Extractor)
	for n := range st.Contrib {
		add(n)
	}
	for n := range st.ContribHash {
		add(n)
	}
	sort.Strings(out)
	return out
}

// provenRetiredOwners reports the prior owners whose retirement this run can
// plan without falling back to the whole domain.
//
// A retired owner that was never a TypeScript source is plannable only when
// every extractor that wrote its cached contributions ran its own pre-Begin
// preview this run. Both preview routes end in the same closure: the
// captured-bytes preview replays the extractor's entire output and names each
// page whose facts moved, the conservative seed claims every file the
// extractor owns plus every owner it retires, and each of them closes over the
// cached owners that mention a candidate name the extractor added or withdrew.
// That closure is the consumer index global Fact.Name resolution actually uses,
// which is the edge the import graph cannot see.
//
// An extractor that did not run this pass proves nothing about its own
// retirements - deleting the last markdown page stops mdintent being detected
// at all - so its retired owners keep the broad fallback. A path that also
// carries a TypeScript contribution is left unproven for the same reason: the
// TypeScript half is planned from import records, never from this loop, and a
// real source file is already excluded by the caller's IsSessionSource test.
//
// previewed is keyed by extractor name; prevFiles is the cached state the
// deletion is planned against.
func provenRetiredOwners(prevFiles map[string]*FileState, previewed map[string]bool) map[string]bool {
	if len(previewed) == 0 {
		return nil
	}
	out := map[string]bool{}
	for path, st := range prevFiles {
		names := contributingExtractors(st)
		if len(names) == 0 {
			continue
		}
		proven := true
		for _, n := range names {
			if !previewed[n] {
				proven = false
				break
			}
		}
		if proven {
			out[filepath.ToSlash(path)] = true
		}
	}
	return out
}
