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
	if err != nil {
		return p, "", err
	}
	if !planCoversDeclaredNameDependents(p, prevFiles, changed) {
		// Cached reverse-edges do not include name-resolution dependents that
		// post-parse discovery can still add. Begin must already contain them.
		p, err = planFileInvalidation(domain, previous, current, nil, true, domain)
		return p, frozenScopeWholeDomain, err
	}
	reason := frozenScopeReverseClose
	if len(extraOwners) > 0 {
		reason = frozenScopeNameDelta
	}
	return p, reason, err
}

func tsRecord(st *FileState) *tsextractor.FileRecord {
	if st == nil {
		return nil
	}
	return st.TS
}

// dependencyIndexProven is true when cached TS records can support reverse-close.
// Unresolved specs (external modules, CSS) do not unprove an otherwise complete
// import graph; incompleteDependencyRecords remains the completeness guard.
func dependencyIndexProven(prevFiles map[string]*FileState) bool {
	if incompleteDependencyRecords(prevFiles) {
		return false
	}
	for _, st := range prevFiles {
		if tsRecord(st) != nil {
			return true
		}
	}
	return false
}

func routerDTO(st *FileState) *tsextractor.RouterDTO {
	rec := tsRecord(st)
	if rec == nil {
		return nil
	}
	return rec.Router
}

func tsRecordsFromState(prevFiles map[string]*FileState) map[string]*tsextractor.FileRecord {
	out := map[string]*tsextractor.FileRecord{}
	for path, st := range prevFiles {
		if rec := tsRecord(st); rec != nil {
			out[filepath.ToSlash(path)] = rec
		}
	}
	return out
}

func overlayTSRecords(base, neu map[string]*tsextractor.FileRecord) map[string]*tsextractor.FileRecord {
	out := map[string]*tsextractor.FileRecord{}
	for k, v := range base {
		if v != nil {
			out[filepath.ToSlash(k)] = v
		}
	}
	for k, v := range neu {
		if v != nil {
			out[filepath.ToSlash(k)] = v
		}
	}
	return out
}

func recordFactsFromTS(recs map[string]*tsextractor.FileRecord) []facts.Fact {
	var out []facts.Fact
	for _, rec := range recs {
		if rec != nil {
			out = append(out, rec.Facts...)
		}
	}
	return out
}

func composedRouteFacts(recs map[string]*tsextractor.FileRecord) []facts.Fact {
	ff := append(recordFactsFromTS(recs), tsextractor.ComposedMountRoutes(recs)...)
	return tsextractor.ComposeEngineMounts(ff)
}

func mountFingerprint(dto *tsextractor.RouterDTO) string {
	if dto == nil || len(dto.Mounts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(dto.Mounts))
	for _, m := range dto.Mounts {
		parts = append(parts, strings.Join([]string{m.Parent, m.Prefix, m.Child, m.File}, "|"))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func resolveMountChildFile(dto *tsextractor.RouterDTO, m tsextractor.MountDTO) string {
	child := filepath.ToSlash(m.Child)
	if child == "" {
		return ""
	}
	if strings.Contains(child, "/") || strings.HasSuffix(child, ".ts") || strings.HasSuffix(child, ".js") || strings.HasSuffix(child, ".mts") || strings.HasSuffix(child, ".cts") {
		return child
	}
	if dto != nil && dto.Imports != nil {
		if ref, ok := dto.Imports[m.Child]; ok && ref.File != "" {
			return filepath.ToSlash(ref.File)
		}
	}
	return ""
}

func mountOwnerFilesDeep(recs map[string]*tsextractor.FileRecord, dto *tsextractor.RouterDTO) []string {
	if dto == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var walk func(*tsextractor.RouterDTO)
	walk = func(d *tsextractor.RouterDTO) {
		if d == nil {
			return
		}
		for _, m := range d.Mounts {
			child := resolveMountChildFile(d, m)
			if child == "" || seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			if recs != nil {
				if rec := recs[child]; rec != nil {
					walk(rec.Router)
				}
			}
		}
	}
	walk(dto)
	return out
}

// dirtyRouterMountChildren returns child route owners whose mount prefix or
// parent changed on a dirty file, including nested mount descendants. They
// must be in Begin before extract because composed KindRoute facts are owned
// by the child files.
func dirtyRouterMountChildren(prevFiles map[string]*FileState, dirty map[string]bool, newRecs map[string]*tsextractor.FileRecord) []string {
	prevRecs := tsRecordsFromState(prevFiles)
	nextRecs := overlayTSRecords(prevRecs, newRecs)
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = filepath.ToSlash(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for f, d := range dirty {
		if !d {
			continue
		}
		key := filepath.ToSlash(f)
		oldDTO := routerDTO(lookupState(prevFiles, f))
		var newDTO *tsextractor.RouterDTO
		if rec := nextRecs[key]; rec != nil {
			newDTO = rec.Router
		}
		if mountFingerprint(oldDTO) == mountFingerprint(newDTO) {
			continue
		}
		for _, id := range mountOwnerFilesDeep(prevRecs, oldDTO) {
			add(id)
		}
		for _, id := range mountOwnerFilesDeep(nextRecs, newDTO) {
			add(id)
		}
	}
	return out
}

// composedRouteOwnerDelta returns owners whose composed or per-file KindRoute
// domain changed after overlaying dirty preview records onto the cached graph.
// Previous raw FileRecord.Facts omit composition-only routes, so both sides
// reconstruct composeRouterMounts from Router DTOs.
func composedRouteOwnerDelta(prevFiles map[string]*FileState, dirty map[string]bool, newRecs map[string]*tsextractor.FileRecord) []string {
	prevRecs := tsRecordsFromState(prevFiles)
	nextRecs := overlayTSRecords(prevRecs, newRecs)
	scope := map[string]bool{}
	addChangedRouteFiles(scope, composedRouteFacts(prevRecs), composedRouteFacts(nextRecs))
	for _, id := range dirtyRouterMountChildren(prevFiles, dirty, newRecs) {
		scope[id] = true
	}
	if len(scope) == 0 {
		return nil
	}
	out := make([]string, 0, len(scope))
	for id := range scope {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func planCoversDeclaredNameDependents(p *fileInvalidationPlan, prevFiles map[string]*FileState, dirty map[string]bool) bool {
	if p == nil {
		return false
	}
	declared := map[string]bool{}
	for f, d := range dirty {
		if !d {
			continue
		}
		rec := tsRecord(lookupState(prevFiles, f))
		if rec == nil {
			continue
		}
		for _, n := range rec.Declared {
			if n != "" {
				declared[n] = true
			}
		}
	}
	if len(declared) == 0 {
		return true
	}
	for path, st := range prevFiles {
		rec := tsRecord(st)
		if rec == nil {
			continue
		}
		id := filepath.ToSlash(path)
		if p.member[id] {
			continue
		}
		for _, n := range rec.Referenced {
			if declared[n] {
				return false
			}
		}
	}
	return true
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

func comparableCandidateID(f facts.Fact) string {
	f.Repo = ""
	f.File = canonicalFactFile(f.File)
	return f.Identity()
}

func candidateIDSetByName(ff []facts.Fact) map[string][]string {
	sets := map[string]map[string]bool{}
	for _, f := range ff {
		if f.Name == "" {
			continue
		}
		if sets[f.Name] == nil {
			sets[f.Name] = map[string]bool{}
		}
		sets[f.Name][comparableCandidateID(f)] = true
	}
	out := map[string][]string{}
	for name, ids := range sets {
		for id := range ids {
			out[name] = append(out[name], id)
		}
		sort.Strings(out[name])
	}
	return out
}

func changedResolutionCandidateNames(old, neu []facts.Fact) map[string]bool {
	a := candidateIDSetByName(old)
	b := candidateIDSetByName(neu)
	changed := map[string]bool{}
	for name, ids := range a {
		if !slicesEqual(ids, b[name]) {
			changed[name] = true
		}
	}
	for name, ids := range b {
		if !slicesEqual(ids, a[name]) {
			changed[name] = true
		}
	}
	return changed
}

func ownerMentionsNames(st *FileState) map[string]bool {
	names := factResolutionNames(cachedResolutionFacts(st))
	if rec := tsRecord(st); rec != nil {
		for _, n := range rec.Declared {
			if n != "" {
				names[n] = true
			}
		}
		for _, n := range rec.Referenced {
			if n != "" {
				names[n] = true
			}
		}
	}
	return names
}

func ownersForNameDelta(prevFiles map[string]*FileState, dirty map[string]bool, newFacts map[string][]facts.Fact) []string {
	delta := map[string]bool{}
	for f, isDirty := range dirty {
		if !isDirty {
			continue
		}
		key := filepath.ToSlash(f)
		old := cachedResolutionFacts(lookupState(prevFiles, key))
		neu := newFacts[key]
		if neu == nil {
			neu = newFacts[f]
		}
		for n := range changedResolutionCandidateNames(old, neu) {
			delta[n] = true
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
		names := ownerMentionsNames(st)
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
