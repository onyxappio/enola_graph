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
	frozenScopeMembershipRe = "add/delete/rename: reverse-close plus owners whose cached import resolution changes"
)

// authoritativeFilePlan consumes the membershipDelta the caller already used to
// seed the pre-Begin preview, rather than deriving one of its own afterwards:
// the importers a membership change rebinds are reparses whose names and route
// mounts can move, so they have to be previewed before the name and composed
// route deltas run, not discovered once those have finished.
func authoritativeFilePlan(previous, current []string, prevFiles map[string]*FileState, hashes map[string]string, wholeDomain bool, extraOwners []string, membership membershipDelta, proof *frozenPreview) (*fileInvalidationPlan, string, error) {
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
	for _, f := range current {
		st := lookupState(prevFiles, f)
		h, present := lookupHash(hashes, f)
		if st == nil || !present || st.Hash != h || st.Unreadable {
			ownedBefore := previousSet[f]
			source := tsextractor.IsSessionSource(f, false)
			if ownedBefore || source {
				changed[f] = true
			}
			// A file that is neither a prior owner nor a session source is not
			// claimed here. The prior owner map records contributions, not the
			// prior input inventory, so it cannot tell a genuinely new
			// JSON/media file from a pre-existing one that never contributed.
			// Those owners are planned from real input hashes by the
			// extractor-need path in session.go instead.
		}
	}
	for _, f := range previous {
		if !currentSet[f] {
			changed[f] = true
			continue
		}
		if _, present := lookupHash(hashes, f); !present {
			changed[f] = true
		}
	}
	// A file the pre-Begin preview reparsed carries no reverse edges here. The
	// preview already took exactly the dependents its observed surface proved
	// are affected and merged them into the seed, so closing over the same file
	// again would re-derive the old reachability answer and undo it. Dropping
	// the edge rather than the seed also stops a closure that arrives at such a
	// file from some other owner, which is sound for the same reason. Without a
	// proof - no preview ran, or the session keeps the broad rule - every seed
	// reverse-closes exactly as before.
	settled := map[string]bool{}
	if proof != nil && !proof.broad {
		settled = proof.closed
		// The preview reparses more than the byte-changed files: it adds the
		// dependents its observed surfaces proved are affected. Their own bytes did
		// not move, so nothing above claimed them, yet extraction will replace
		// their contributions and the plan has to carry them. Reverse-closing them
		// is a no-op - the edges into a settled file are dropped below - so they
		// cost only their own membership.
		for f := range settled {
			if known[f] {
				changed[f] = true
			}
		}
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
				if known[dep] && !settled[dep] {
					deps[from] = append(deps[from], dep)
				}
			}
			for _, dep := range st.TS.SideReads {
				dep = filepath.ToSlash(dep)
				if known[dep] && !settled[dep] {
					deps[from] = append(deps[from], dep)
				}
			}
		}
	}
	// A new, renamed or deleted file changes module resolution for importers
	// whose own bytes did not change: a previously unresolved specifier can
	// become satisfiable, and an already resolved one can rebind to a different
	// file when the added path wins the extension/index precedence. Those
	// importers must be in Begin, but the cached import surface enumerates
	// them, so the whole domain is not required.
	if membership.changed {
		if !membership.proven {
			// Consumers the TypeScript import graph cannot see, or importers
			// whose cached surface cannot be replayed. Old reverse-edges cannot
			// prove a safe subset.
			p, err := planFileInvalidation(domain, previous, current, nil, true, domain)
			return p, frozenScopeMembership, err
		}
		// Merged into changed, not just the seed, so the declared-name
		// post-check below also covers the names these owners republish.
		for _, f := range membership.rebound {
			changed[f] = true
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
	// Cached reverse-edges do not include name-resolution dependents that
	// post-parse discovery can still add. Begin must already contain them - but
	// they are enumerable from the same cached records, so seed them and replan
	// instead of discarding the plan for the whole domain. The enlarged plan is
	// a superset, so the second check can only fail if the records stopped
	// naming them; the global fallback stays for that case.
	nameDeps := declaredNameDependents(p, prevFiles, changed, proof)
	if len(nameDeps) > 0 {
		extraOwners = append(extraOwners, nameDeps...)
		p, err = planFileInvalidation(append(seed, nameDeps...), previous, current, deps, false, nil)
		if err != nil {
			return p, "", err
		}
		if len(declaredNameDependents(p, prevFiles, changed, proof)) > 0 {
			p, err = planFileInvalidation(domain, previous, current, nil, true, domain)
			return p, frozenScopeWholeDomain, err
		}
	}
	reason := frozenScopeReverseClose
	if len(extraOwners) > 0 {
		reason = frozenScopeNameDelta
	}
	if membership.changed {
		reason = frozenScopeMembershipRe
	}
	return p, reason, err
}

// directoryModuleSiblings lists markdown owners that share a directory with a
// file that entered or left the published set. Every extractor that emits a
// directory-shaped module names it after the directory it sits in - mdintent at
// document.go:143, tsextractor at ts.go:512, pythonextractor at python.go:196,
// hclextractor at hcl.go:138, swiftextractor at swift.go:277 - so a sibling in
// any of those languages appearing or leaving can move that module's identity
// while the markdown pages themselves are untouched, and their declares edges
// have to be republished.
//
// claimed is the set of current files an active extractor owns and bounded says
// every active extractor could answer; both come from extractorClaimedFiles.
func directoryModuleSiblings(previous, current []string, prevFiles map[string]*FileState, claimed map[string]bool, bounded bool) []string {
	prevSet := map[string]bool{}
	for _, f := range previous {
		prevSet[filepath.ToSlash(f)] = true
	}
	currSet := map[string]bool{}
	for _, f := range current {
		currSet[filepath.ToSlash(f)] = true
	}
	dirs := map[string]bool{}
	markDir := func(f string) {
		dirs[filepath.ToSlash(filepath.Dir(f))] = true
	}
	// A prior owner absent from the current semantic set really did leave it:
	// prevSet is the published owner set, so this direction is exact.
	for f := range prevSet {
		if !currSet[f] {
			markDir(f)
		}
	}
	// The other direction is not symmetric, for the reason membershipScopeWithProof
	// records below: prevSet is a contribution map, not the prior input inventory,
	// so a file that existed but never emitted a fact is absent from it and "not
	// previously an owner" reads as "new" on nearly every delta. current is the
	// semantic name set, which admits files no extractor claims at all; those
	// produce no facts, so they carry no directory module and can move none.
	// Marking their directories seeds every markdown page under them into the
	// frozen manifest, and on this path the owning extractor does not rerun, so
	// each one is republished from cache byte for byte unchanged.
	//
	// A file an extractor does claim is a real module candidate whatever its
	// language, so it still marks: this is not a TypeScript-only test, and a new
	// python, swift or hcl sibling reaches the same directory module the markdown
	// page declares. When an extractor cannot declare its owner domain, claimed
	// is not a bound and nothing is narrowed here.
	for f := range currSet {
		if prevSet[f] {
			continue
		}
		if bounded && !claimed[f] {
			continue
		}
		markDir(f)
	}
	if len(dirs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		f = filepath.ToSlash(f)
		if f == "" || seen[f] || !strings.HasSuffix(strings.ToLower(f), ".md") {
			return
		}
		if !dirs[filepath.ToSlash(filepath.Dir(f))] {
			return
		}
		seen[f] = true
		out = append(out, f)
	}
	for f := range prevSet {
		add(f)
	}
	for f := range currSet {
		add(f)
	}
	for path := range prevFiles {
		add(path)
	}
	sort.Strings(out)
	return out
}

// membershipDelta is the pre-Begin answer to what an add, delete or rename
// moves besides the files themselves. It is computed once, before the planning
// extract, because the importers it names are reparses like any other: their
// declared names and composed route mounts can move, and those consumers have
// to be inside Begin too.
type membershipDelta struct {
	// changed is true when a file identity entered or left either the published
	// owner set or the TypeScript resolution universe.
	changed bool
	// proven is false when no safe subset can be derived and the caller must
	// take the wider fallback; reason then carries the published explanation.
	proven bool
	reason string
	// retired lists prior owners absent from the current set. They are never
	// parsed, but they must reach the name and route deltas as old->empty
	// contributions so global consumers of a deleted candidate - including
	// non-TypeScript owners such as a markdown link - land inside Begin.
	retired []string
	// rebound lists cached importers whose module resolution the new file set
	// moves. It applies invalidateTS's own rule (recordRebound) to the same
	// record map and the same two known sets, so the frozen scope stays a
	// superset of the reparse set extraction will compute: no dirty file can
	// land outside Begin.
	rebound []string
}

// membershipScope derives that answer from the cached import surface.
//
// The two resolution universes are deliberately the ones the extractor itself
// uses: priorKnownFiles rebuilds the TypeScript sources the cached records were
// parsed against, and sessionFiles is the inventory ExtractSession receives, not
// the policy-filtered semantic name list - a name the graph policy drops is
// still a resolution target for the next parse.
func membershipScope(previous, current, sessionFiles []string, prevFiles map[string]*FileState) membershipDelta {
	return membershipScopeWithProof(previous, current, sessionFiles, prevFiles, nil)
}

// membershipScopeWithProof is membershipScope with the retirements this run
// already accounted for before Begin. provenRetired comes from
// provenRetiredOwners, keyed by the extractors the caller actually previewed:
// a retired owner in it has had every cached consumer of the names it withdrew
// enumerated already, so it no longer forces the wider fallback. Every other
// non-TypeScript retirement still does.
func membershipScopeWithProof(previous, current, sessionFiles []string, prevFiles map[string]*FileState, provenRetired map[string]bool) membershipDelta {
	md := membershipDelta{proven: true}
	previousSet := make(map[string]bool, len(previous))
	for _, f := range graphPublishedOwners(previous) {
		previousSet[filepath.ToSlash(f)] = true
	}
	currentSet := make(map[string]bool, len(current))
	for _, f := range graphSemanticNames(nil, current) {
		currentSet[f] = true
	}
	// An addition is not detected here. previousSet is a contribution map, not
	// the prior input inventory: a JSON or media file that existed but never
	// emitted a fact is absent from it, so "not previously an owner" would read
	// as "new" on nearly every delta. TypeScript additions are detected below
	// against the resolution universe, which the cached records do record
	// faithfully; other additions are planned by the owning extractor.
	for f := range previousSet {
		if currentSet[f] {
			continue
		}
		md.changed = true
		md.retired = append(md.retired, f)
		if !tsextractor.IsSessionSource(f, false) && !provenRetired[f] {
			// A published owner that left the tree without ever being a
			// TypeScript source and without a pre-Begin proof: its consumers
			// are outside the cached import graph and the prior owner map
			// cannot enumerate them.
			md.proven = false
			md.reason = frozenScopeMembership
		}
	}
	sort.Strings(md.retired)

	prevRecs := tsRecordsFromState(prevFiles)
	priorKnown := priorKnownFiles(prevRecs)
	known := sessionKnownFiles(sessionFiles)
	// A TypeScript source entering or leaving the resolution universe is what
	// invalidateTS reports as filenameChanged, even when the published owner set
	// did not move. If such a file is neither a current nor a prior owner it is
	// not a plannable identity at all: extraction will reparse it, the frozen
	// manifest cannot name it, and only the wider fallback stays safe.
	resolutionChanged := false
	for f := range known {
		if !priorKnown[f] {
			resolutionChanged = true
			if !currentSet[f] && !previousSet[f] {
				md.proven = false
				md.reason = frozenScopeMembership
			}
		}
	}
	for f := range priorKnown {
		if !known[f] {
			resolutionChanged = true
			if !currentSet[f] && !previousSet[f] {
				md.proven = false
				md.reason = frozenScopeMembership
			}
		}
	}
	if resolutionChanged {
		md.changed = true
	}
	if !md.changed || !md.proven {
		return md
	}
	if !resolutionChanged {
		// An owner retired without the TypeScript resolution universe moving,
		// so no cached specifier can rebind. The retired identity still needs
		// the name and route deltas, which the caller seeds from md.retired.
		return md
	}
	if !dependencyIndexProven(prevFiles) {
		md.proven = false
		md.reason = frozenScopeMembership
		return md
	}
	for file, rec := range prevRecs {
		if len(rec.ImportSpecs) == 0 {
			if len(rec.ResolvedFiles) > 0 || len(rec.UnresolvedSpecs) > 0 {
				// Resolution outcomes without the specifiers that produced them,
				// so this importer's resolution cannot be replayed at all. The
				// unresolved half matters as much as the resolved one: recordRebound
				// replays UnresolvedSpecs, so reading a truncated record as an
				// importer with no imports would let extraction dirty a file the
				// frozen scope never named.
				return membershipDelta{changed: true, proven: false, reason: frozenScopeMembership, retired: md.retired}
			}
			continue
		}
		if !rec.ImportComplete && len(rec.UnresolvedSpecs) == 0 && len(rec.ResolvedFiles) == 0 {
			// Specs stored without any evidence that resolution ran, so the
			// replay below would read absence as "never resolved".
			return membershipDelta{changed: true, proven: false, reason: frozenScopeMembership, retired: md.retired}
		}
		if recordRebound(rec, priorKnown, known) {
			md.rebound = append(md.rebound, file)
		}
	}
	sort.Strings(md.rebound)
	return md
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

// overlayTSRecords projects the next record set: cached records, replaced by the
// previewed ones, minus the identities that left the tree. Without the removal
// step a deleted router file keeps its cached mounts in the "after" graph, so a
// delete would look like no route change at all.
func overlayTSRecords(base, neu map[string]*tsextractor.FileRecord, retired map[string]bool) map[string]*tsextractor.FileRecord {
	out := map[string]*tsextractor.FileRecord{}
	for k, v := range base {
		if v != nil {
			out[filepath.ToSlash(k)] = v
		}
	}
	for k := range retired {
		delete(out, filepath.ToSlash(k))
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

func routeCandidateFacts(ff []facts.Fact) []facts.Fact {
	out := make([]facts.Fact, 0, len(ff))
	for _, f := range ff {
		if f.Kind == facts.KindRoute {
			out = append(out, f)
		}
	}
	return out
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
func dirtyRouterMountChildren(prevFiles map[string]*FileState, dirty map[string]bool, newRecs map[string]*tsextractor.FileRecord, retired map[string]bool) []string {
	prevRecs := tsRecordsFromState(prevFiles)
	nextRecs := overlayTSRecords(prevRecs, newRecs, retired)
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
func composedRouteOwnerDelta(prevFiles map[string]*FileState, dirty map[string]bool, newRecs map[string]*tsextractor.FileRecord, retired map[string]bool) []string {
	prevRecs := tsRecordsFromState(prevFiles)
	nextRecs := overlayTSRecords(prevRecs, newRecs, retired)
	scope := map[string]bool{}
	oldComposed := composedRouteFacts(prevRecs)
	newComposed := composedRouteFacts(nextRecs)
	addChangedRouteFiles(scope, oldComposed, newComposed)
	for _, id := range ownersForCandidateNameDelta(prevFiles, routeCandidateFacts(oldComposed), routeCandidateFacts(newComposed)) {
		scope[id] = true
	}
	for _, id := range dirtyRouterMountChildren(prevFiles, dirty, newRecs, retired) {
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

// declaredNameDependents lists cached owners outside the plan that reference a
// name declared by a dirty file. buildIndex resolves Fact.Name globally rather
// than along import edges, so such an owner can gain or lose an edge without any
// file dependency connecting it to the change; it has to be inside Begin for the
// frozen replacement to be able to rewrite it. Returning them lets the caller
// widen the plan by exactly this set instead of falling back to the whole domain.
// declaredNameDependents lists cached owners outside the plan whose Referenced
// surface mentions a name a dirty file declares. The cached record shows only the
// names a file declared BEFORE the edit, so an added export has no cached
// evidence at all; when a preview ran, its observed name delta supplies those.
func declaredNameDependents(p *fileInvalidationPlan, prevFiles map[string]*FileState, dirty map[string]bool, proof *frozenPreview) []string {
	if p == nil {
		return nil
	}
	declared := map[string]bool{}
	proven := map[string]bool{}
	if proof != nil && !proof.broad {
		proven = proof.recorded
		// The preview reparsed these files, so the names that actually entered or
		// left the global index are known exactly. Every other name a dirty file
		// declares is still declared by it and still resolves to the same
		// candidate, and a consumer that only mentions one of those sees nothing
		// move. Without the proof the cached record shows the old surface alone,
		// so the whole surface has to count.
		for n := range proof.declaredAdded {
			if n != "" {
				declared[n] = true
			}
		}
		for n := range proof.declaredRemoved {
			if n != "" {
				declared[n] = true
			}
		}
	}
	for f, d := range dirty {
		if !d || proven[f] || proven[filepath.ToSlash(f)] {
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
		return nil
	}
	var out []string
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
				out = append(out, id)
				break
			}
		}
	}
	sort.Strings(out)
	return out
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
	return ownersForChangedCandidateNames(prevFiles, delta)
}

func ownersForCandidateNameDelta(prevFiles map[string]*FileState, old, neu []facts.Fact) []string {
	return ownersForChangedCandidateNames(prevFiles, changedResolutionCandidateNames(old, neu))
}

func ownersForChangedCandidateNames(prevFiles map[string]*FileState, delta map[string]bool) []string {
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
