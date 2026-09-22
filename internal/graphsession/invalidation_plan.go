package graphsession

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

// authoritativeFilePlan derives the immutable replacement manifest before any
// new extraction starts. For ordinary deltas it uses the previous file-level
// dependency graph and reverse-closes the changed file set. A whole-domain
// fallback remains available for initial runs, config/policy changes, and
// incomplete dependency records.
const (
	frozenScopeReverseClose = "frozen scope: prior file-to-file dependency closure"
	frozenScopeWholeDomain  = "frozen scope: global name-resolution domain"
	frozenScopeMembership   = "add/delete/rename: prior file graph cannot prove a safe subset"
	frozenScopeNameDelta    = "frozen scope: reverse-close plus owners that reference added or removed fact names"
)

func authoritativeFilePlan(previous, current []string, prevFiles map[string]*FileState, hashes map[string]string, wholeDomain bool, extraOwners []string) (*fileInvalidationPlan, string, error) {
	previous = graphPublishedOwners(previous)
	current = graphSemanticNames(nil, current)
	domain := append(append([]string{}, previous...), current...)
	if wholeDomain {
		p, err := planFileInvalidation(domain, previous, current, nil, true, domain)
		return p, frozenScopeWholeDomain, err
	}

	known := make(map[string]bool, len(domain))
	for _, f := range domain {
		known[f] = true
	}
	currentSet := make(map[string]bool, len(current))
	for _, f := range current {
		currentSet[f] = true
	}
	previousSet := make(map[string]bool, len(previous))
	for _, f := range previous {
		previousSet[f] = true
	}
	changed := make(map[string]bool)
	membershipChanged := false
	for _, f := range current {
		st := lookupState(prevFiles, f)
		h, present := lookupHash(hashes, f)
		if st == nil || !present || st.Hash != h || st.Unreadable {
			ownedBefore := previousSet[f]
			source := tsextractor.IsSessionSource(f, false)
			if ownedBefore || source {
				changed[f] = true
			}
			if !ownedBefore && source {
				membershipChanged = true
			}
		}
	}
	for _, f := range previous {
		if !currentSet[f] {
			changed[f] = true
			membershipChanged = true
			continue
		}
		if _, present := lookupHash(hashes, f); !present {
			changed[f] = true
		}
	}
	// A new or renamed file can satisfy an import that was previously
	// unresolved. Include those importers before Begin so resolution changes
	// cannot escape the frozen manifest.
	if membershipChanged {
		// Add/delete/rename can introduce resolution edges that did not exist
		// in the prior file graph, including name collisions against already
		// resolved imports. Old reverse-edges cannot prove a safe subset.
		p, err := planFileInvalidation(domain, previous, current, nil, true, domain)
		return p, frozenScopeMembership, err
	}

	deps := make(map[string][]string)
	for path, st := range prevFiles {
		if st == nil {
			continue
		}
		from := filepath.ToSlash(path)
		if st.TS != nil {
			for _, dep := range st.TS.ResolvedFiles {
				dep = filepath.ToSlash(dep)
				if known[dep] {
					deps[from] = append(deps[from], dep)
				}
			}
		}
	}
	seed := make([]string, 0, len(changed)+len(extraOwners))
	for f := range changed {
		seed = append(seed, f)
	}
	for _, f := range extraOwners {
		f = filepath.ToSlash(f)
		if f != "" {
			seed = append(seed, f)
		}
	}
	// planFileInvalidation reverse-closes file dependencies. extraOwners must
	// already include every cached owner whose facts mention added/removed
	// names; buildIndex resolves Fact.Name globally, not along import edges.
	p, err := planFileInvalidation(seed, previous, current, deps, false, nil)
	reason := frozenScopeReverseClose
	if len(extraOwners) > 0 {
		reason = frozenScopeNameDelta
	}
	return p, reason, err
}

func factResolutionNames(ff []facts.Fact) map[string]bool {
	out := map[string]bool{}
	for _, f := range ff {
		if f.Name != "" {
			out[f.Name] = true
		}
		for _, r := range f.Relations {
			if r.Target != "" {
				out[r.Target] = true
			}
		}
	}
	return out
}

func cachedResolutionFacts(st *FileState) []facts.Fact {
	ff := fileFacts(st)
	if st == nil {
		return ff
	}
	for _, extra := range st.Contrib {
		ff = append(ff, extra...)
	}
	return ff
}

func ownersForNameDelta(prevFiles map[string]*FileState, dirty map[string]bool, newFacts map[string][]facts.Fact) []string {
	delta := map[string]bool{}
	for f, isDirty := range dirty {
		if !isDirty {
			continue
		}
		key := filepath.ToSlash(f)
		old := factResolutionNames(cachedResolutionFacts(lookupState(prevFiles, key)))
		neu := factResolutionNames(newFacts[key])
		if neu == nil {
			neu = factResolutionNames(newFacts[f])
		}
		for n := range old {
			if !neu[n] {
				delta[n] = true
			}
		}
		for n := range neu {
			if !old[n] {
				delta[n] = true
			}
		}
	}
	if len(delta) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var owners []string
	for path, st := range prevFiles {
		path = filepath.ToSlash(path)
		if seen[path] {
			continue
		}
		names := factResolutionNames(cachedResolutionFacts(st))
		for n := range names {
			if delta[n] {
				seen[path] = true
				owners = append(owners, path)
				break
			}
		}
	}
	return owners
}

// resolutionCandidatesChanged reports a same-name candidate identity change
// before Begin. A resolver can invalidate consumers even when the declared
// name itself is unchanged (for example a route/module candidate whose
// identity or kind changed). In that case a file-level reverse dependency
// closure is not a proven complete scope, so callers must use the global
// fallback rather than discover an out-of-scope owner after Begin.
func resolutionCandidatesChanged(prevFiles map[string]*FileState, dirty map[string]bool, newFacts map[string][]facts.Fact) bool {
	for file, isDirty := range dirty {
		if !isDirty {
			continue
		}
		oldByName := map[string][]string{}
		for _, f := range cachedResolutionFacts(lookupState(prevFiles, filepath.ToSlash(file))) {
			if f.Name != "" {
				oldByName[f.Name] = append(oldByName[f.Name], f.Identity())
			}
		}
		factsNow := newFacts[filepath.ToSlash(file)]
		if factsNow == nil {
			factsNow = newFacts[file]
		}
		newByName := map[string][]string{}
		for _, f := range factsNow {
			if f.Name != "" {
				newByName[f.Name] = append(newByName[f.Name], f.Identity())
			}
		}
		for name, oldIDs := range oldByName {
			if !slicesEqual(sortedStrings(oldIDs), sortedStrings(newByName[name])) {
				return true
			}
		}
		for name, newIDs := range newByName {
			if _, existed := oldByName[name]; !existed && len(newIDs) > 0 {
				return true
			}
		}
	}
	return false
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func relationBearingOwnerCount(prevFiles map[string]*FileState) (total, withRelations int) {
	for _, st := range prevFiles {
		total++
		for _, f := range cachedResolutionFacts(st) {
			if len(f.Relations) > 0 {
				withRelations++
				break
			}
		}
	}
	return total, withRelations
}

func incompleteDependencyRecords(prevFiles map[string]*FileState) bool {
	withSpecs, withResolved := 0, 0
	for _, st := range prevFiles {
		if st == nil || st.TS == nil {
			continue
		}
		if len(st.TS.ImportSpecs) > 0 {
			withSpecs++
		}
		if len(st.TS.ResolvedFiles) > 0 {
			withResolved++
		}
	}
	if withSpecs == 0 || withResolved > 0 {
		return false
	}
	for _, st := range prevFiles {
		if st == nil || st.TS == nil {
			continue
		}
		if st.TS.ImportComplete || len(st.TS.UnresolvedSpecs) > 0 || len(st.TS.ResolvedFiles) > 0 {
			return false
		}
	}
	return true
}

// fileInvalidationPlan is immutable after construction. Dependencies point from
// a source file to the files its analysis depends on. A fallback domain must be
// proven complete by its caller; nil means no narrower domain is established.
// The current name-based resolver requires the repository-wide fallback.
type fileInvalidationPlan struct {
	owners []graphstream.OwnerRef
	member map[string]bool
	digest string
}

func planFileInvalidation(changed, previous, current []string, dependencies map[string][]string, fallback bool, domain []string) (*fileInvalidationPlan, error) {
	p := &fileInvalidationPlan{member: make(map[string]bool)}
	queue := []string{}
	add := func(name string) error {
		if invalidOwnerPath(name) {
			return fmt.Errorf("invalidation plan: invalid repo-relative file owner %q", name)
		}
		if !p.member[name] {
			p.member[name] = true
			queue = append(queue, name)
		}
		return nil
	}
	for _, name := range changed {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	if fallback && len(changed) > 0 {
		if domain == nil {
			domain = append(append([]string{}, previous...), current...)
		}
		for _, name := range domain {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	reverse := map[string][]string{}
	for file, deps := range dependencies {
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], file)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, dependent := range reverse[queue[i]] {
			if err := add(dependent); err != nil {
				return nil, err
			}
		}
	}
	for file := range p.member {
		p.owners = append(p.owners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: file})
	}
	graphstream.SortOwners(p.owners)
	p.digest = graphstream.DigestOwners(p.owners)
	return p, nil
}

func (p *fileInvalidationPlan) manifest() []graphstream.OwnerRef {
	return append([]graphstream.OwnerRef(nil), p.owners...)
}

func (p *fileInvalidationPlan) check(owner graphstream.OwnerRef) error {
	if owner.Kind != graphstream.OwnerFile || !p.member[owner.ID] {
		return fmt.Errorf("invalidation plan: owner %s outside frozen scope", owner.String())
	}
	return nil
}

func invalidOwnerPath(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
		return true
	}
	if len(name) >= 2 && name[1] == ':' && isDriveLetter(name[0]) {
		return true
	}
	return path.Clean(name) != name
}

func isDriveLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
