package graphsession

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
	"github.com/enola-labs/enola/pkg/plugin"
)

// factFileOwner is implemented by extractors that declare fact provenance
// without opting into plugin.FileOwner cache keys (mdintent).
type factFileOwner interface {
	OwnsFactFile(relFile string) bool
}

func ownedFiles(ext plugin.Extractor, files []string) []string {
	if ext == nil {
		return nil
	}
	switch o := ext.(type) {
	case plugin.FileOwner:
		var out []string
		for _, f := range files {
			if o.OwnsFile(f) {
				out = append(out, filepath.ToSlash(f))
			}
		}
		return out
	case factFileOwner:
		var out []string
		for _, f := range files {
			if o.OwnsFactFile(f) {
				out = append(out, filepath.ToSlash(f))
			}
		}
		return out
	default:
		return nil
	}
}

func extractorContrib(st *FileState, name string) []facts.Fact {
	if st == nil || name == "" {
		return nil
	}
	var out []facts.Fact
	if st.Extractor == name {
		if name == "typescript" && st.TS != nil {
			out = append(out, st.TS.Facts...)
		} else {
			out = append(out, st.Facts...)
		}
	}
	if st.Contrib != nil {
		out = append(out, st.Contrib[name]...)
	}
	return out
}

func extractorOwnsState(st *FileState, name string) bool {
	if st == nil || name == "" {
		return false
	}
	if st.Extractor == name {
		return true
	}
	if st.Contrib != nil {
		if _, ok := st.Contrib[name]; ok {
			return true
		}
	}
	if st.ContribHash != nil {
		if _, ok := st.ContribHash[name]; ok {
			return true
		}
	}
	return false
}

func extractorSeenHash(st *FileState, name string) string {
	if st == nil || name == "" {
		return ""
	}
	if st.ContribHash != nil {
		if h, ok := st.ContribHash[name]; ok {
			return h
		}
	}
	if st.Extractor == name {
		return st.Hash
	}
	if st.Contrib != nil {
		if _, ok := st.Contrib[name]; ok {
			return st.Hash
		}
	}
	return ""
}

func cloneFileState(st *FileState) *FileState {
	if st == nil {
		return nil
	}
	cp := *st
	if st.Contrib != nil {
		cp.Contrib = make(map[string][]facts.Fact, len(st.Contrib))
		for k, v := range st.Contrib {
			cp.Contrib[k] = v
		}
	}
	if st.ContribHash != nil {
		cp.ContribHash = make(map[string]string, len(st.ContribHash))
		for k, v := range st.ContribHash {
			cp.ContribHash[k] = v
		}
	}
	return &cp
}

func storeExtractorContribution(prev map[string]*FileState, path, extName, hash string, extracted []facts.Fact) {
	path = filepath.ToSlash(path)
	st := prev[path]
	if st == nil {
		prev[path] = &FileState{
			Hash:        hash,
			Extractor:   extName,
			Facts:       extracted,
			ContribHash: map[string]string{extName: hash},
		}
		return
	}
	st = cloneFileState(st)
	prev[path] = st
	if st.ContribHash == nil {
		st.ContribHash = map[string]string{}
	}
	st.ContribHash[extName] = hash
	if st.Extractor == extName && st.TS == nil {
		st.Hash = hash
		st.Facts = extracted
		return
	}
	if st.Extractor == "" && st.TS == nil {
		st.Extractor = extName
		st.Hash = hash
		st.Facts = extracted
		return
	}
	if st.Contrib == nil {
		st.Contrib = map[string][]facts.Fact{}
	}
	st.Contrib[extName] = extracted
}

func extractorInputDigest(ext plugin.Extractor, owned, invFiles, allNames []string, hashes map[string]string, fileSetHash, scanHash string) string {
	if d, ok := ext.(plugin.DeltaInputs); ok {
		seen := map[string]bool{}
		var content []string
		add := func(f string) {
			f = filepath.ToSlash(f)
			if f == "" || seen[f] || !d.ContentInput(f) {
				return
			}
			seen[f] = true
			content = append(content, f)
		}
		for _, f := range invFiles {
			add(f)
		}
		for _, f := range allNames {
			add(f)
		}
		for _, f := range owned {
			add(f)
		}
		cdig := inventoryDigest(content, hashes)
		if _, ok := ext.(plugin.DeltaContext); ok {
			cdig += ":" + hashes[deltaContextKey(ext)]
		}
		if d.NameSetInput() {
			return "v3names:" + inventoryDigest(invFiles, nil) + ":" + cdig
		}
		return "v3owned:" + cdig
	}
	_ = owned
	_ = fileSetHash
	return scanHash
}

func filesToHash(eng *engine.Engine, inv engine.RepoInventory, prev map[string]*FileState, detected map[string]bool) []string {
	if eng == nil {
		return append(append([]string{}, inv.Files...), append([]string{}, inv.AllNames...)...)
	}
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		f = filepath.ToSlash(f)
		if scope := eng.GraphScope(); scope != nil && scope.Policy.Classify(f, false).Kind != graphinput.Semantic {
			return
		}
		if f == "" || seen[f] {
			return
		}
		seen[f] = true
		out = append(out, f)
	}
	opaque := false
	for _, ext := range eng.Extractors() {
		if eng.Config() != nil && !eng.Config().IsExtractorEnabled(ext.Name()) {
			continue
		}
		if detected != nil && !detected[ext.Name()] {
			continue
		}
		if _, ok := ext.(plugin.DeltaInputs); !ok {
			opaque = true
		}
	}
	if opaque {
		for _, f := range inv.Files {
			add(f)
		}
		for _, f := range inv.TestFiles {
			add(f)
		}
		for _, f := range inv.AllNames {
			add(f)
		}
		return out
	}
	for p := range prev {
		add(p)
	}
	lists := [][]string{inv.Files, inv.TestFiles, inv.AllNames}
	for _, ext := range eng.Extractors() {
		if eng.Config() != nil && !eng.Config().IsExtractorEnabled(ext.Name()) {
			continue
		}
		if detected != nil && !detected[ext.Name()] {
			continue
		}
		if d, ok := ext.(plugin.DeltaInputs); ok {
			for _, list := range lists {
				for _, f := range list {
					if d.ContentInput(f) {
						add(f)
					}
				}
			}
		}
		if kd, ok := ext.(plugin.KeyDependent); ok {
			for _, f := range inv.Files {
				if kd.AffectsKey(f) {
					add(f)
				}
			}
		}
	}
	return out
}

// graphSemanticNames keeps graph-profile analysis names: lockfiles and other
// non-semantic inventory entries are omitted even when they remain in AllNames
// for extractor detection.
func graphSemanticNames(eng *engine.Engine, names []string) []string {
	return filterGraphNames(eng, names, true)
}

// graphPublishedOwners keeps previously published file owners for a replacement
// manifest. Current policy must not drop them: exclusion is expressed as an
// empty replacement of the old identity.
func graphPublishedOwners(names []string) []string {
	return filterGraphNames(nil, names, false)
}

func filterGraphNames(eng *engine.Engine, names []string, applyPolicy bool) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, f := range names {
		f = filepath.ToSlash(f)
		if f == "" || seen[f] || graphinput.IsLockfile(f) {
			continue
		}
		if applyPolicy && eng != nil {
			if scope := eng.GraphScope(); scope != nil && scope.Policy != nil && scope.Policy.Classify(f, false).Kind != graphinput.Semantic {
				continue
			}
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// scanHashEquivalent reports whether stored ScanHash matches the current
// semantic digest or a pre-version AllNames digest of the same tree. A version
// stamp or lockfile-policy migration must not by itself advance generation.
func scanHashEquivalent(st *State, allNames, semantic []string, hashes map[string]string, semanticDigest string) bool {
	if st == nil {
		return false
	}
	if st.ScanHash == semanticDigest {
		return true
	}
	if st.ScanHashVersion == authoritativeScanHashVersion {
		return false
	}
	legacy := inventoryDigest(allNames, hashes)
	return st.ScanHash == legacy
}

func inventoryDigest(files []string, hashes map[string]string) string {
	seen := map[string]bool{}
	sorted := make([]string, 0, len(files))
	for _, f := range files {
		f = filepath.ToSlash(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		sorted = append(sorted, f)
	}
	sort.Strings(sorted)
	h := sha256.New()
	for _, f := range sorted {
		h.Write([]byte(f))
		h.Write([]byte{0})
		hf, _ := lookupHash(hashes, f)
		h.Write([]byte(hf))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func factsFingerprint(ff []facts.Fact) string {
	rows := make([][]byte, len(ff))
	for i, f := range ff {
		row, err := json.Marshal(f)
		if err != nil {
			// Unmarshalable props must not look like a valid, unchanged row.
			row = append([]byte{0, 'm', 'a', 'r', 's', 'h', 'a', 'l', '-'}, []byte(err.Error())...)
		}
		rows[i] = row
	}
	sort.Slice(rows, func(i, j int) bool { return bytes.Compare(rows[i], rows[j]) < 0 })
	sum := sha256.New()
	var nbuf [8]byte
	binary.BigEndian.PutUint64(nbuf[:], uint64(len(rows)))
	sum.Write(nbuf[:])
	for _, row := range rows {
		binary.BigEndian.PutUint64(nbuf[:], uint64(len(row)))
		sum.Write(nbuf[:])
		sum.Write(row)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func nonTSExtractorNeed(owned []string, prevFiles map[string]*FileState, hashes map[string]string, extName, prevScan, scanHash string, forceAll, configChanged bool) bool {
	if forceAll || configChanged {
		return true
	}
	if len(owned) == 0 {
		if prevScan == "" || prevScan != scanHash {
			return true
		}
		for path, prev := range prevFiles {
			if !extractorOwnsState(prev, extName) {
				continue
			}
			if _, still := lookupHash(hashes, path); !still {
				return true
			}
		}
		return false
	}
	for _, f := range owned {
		h, ok := lookupHash(hashes, f)
		prev := lookupState(prevFiles, f)
		if !ok || prev == nil || extractorSeenHash(prev, extName) != h {
			return true
		}
	}
	for path, prev := range prevFiles {
		if !extractorOwnsState(prev, extName) {
			continue
		}
		h, still := lookupHash(hashes, path)
		if !still {
			return true
		}
		if extractorSeenHash(prev, extName) != h {
			return true
		}
	}
	return false
}

func mergeNonTSContrib(dst, src *FileState) {
	if dst == nil || src == nil {
		return
	}
	if src.Contrib != nil {
		if dst.Contrib == nil {
			dst.Contrib = map[string][]facts.Fact{}
		}
		for k, v := range src.Contrib {
			if _, ok := dst.Contrib[k]; !ok {
				dst.Contrib[k] = v
			}
		}
	}
	if src.ContribHash != nil {
		if dst.ContribHash == nil {
			dst.ContribHash = map[string]string{}
		}
		for k, v := range src.ContribHash {
			if _, ok := dst.ContribHash[k]; !ok {
				dst.ContribHash[k] = v
			}
		}
	}
	if src.Extractor == "" || src.Extractor == "typescript" || src.TS != nil {
		return
	}
	if dst.Contrib == nil {
		dst.Contrib = map[string][]facts.Fact{}
	}
	if _, ok := dst.Contrib[src.Extractor]; !ok {
		dst.Contrib[src.Extractor] = src.Facts
	}
	if dst.ContribHash == nil {
		dst.ContribHash = map[string]string{}
	}
	if _, ok := dst.ContribHash[src.Extractor]; !ok {
		h := src.Hash
		if src.ContribHash != nil {
			if ch, ok := src.ContribHash[src.Extractor]; ok {
				h = ch
			}
		}
		if h != "" {
			dst.ContribHash[src.Extractor] = h
		}
	}
}

func cachedFactsFor(name string, owned []string, prev map[string]*FileState) []facts.Fact {
	var out []facts.Fact
	seen := map[string]bool{}
	for _, f := range owned {
		f = filepath.ToSlash(f)
		seen[f] = true
		out = append(out, extractorContrib(prev[f], name)...)
	}
	for path, st := range prev {
		if seen[filepath.ToSlash(path)] {
			continue
		}
		out = append(out, extractorContrib(st, name)...)
	}
	return out
}

func fileFacts(st *FileState) []facts.Fact {
	if st == nil {
		return nil
	}
	if st.TS != nil {
		return st.TS.Facts
	}
	return st.Facts
}

func canonicalizeRouteFact(f facts.Fact) facts.Fact {
	f.Repo = ""
	f.File = canonicalFactFile(f.File)
	return f
}

func routeDigestByFile(ff []facts.Fact) map[string]string {
	grouped := map[string][]facts.Fact{}
	for _, f := range ff {
		if f.Kind != facts.KindRoute {
			continue
		}
		f = canonicalizeRouteFact(f)
		grouped[f.File] = append(grouped[f.File], f)
	}
	out := map[string]string{}
	for file, rows := range grouped {
		out[file] = factsFingerprint(rows)
	}
	return out
}

func addChangedRouteFiles(scope map[string]bool, old, neu []facts.Fact) {
	a := routeDigestByFile(old)
	b := routeDigestByFile(neu)
	for file, dig := range b {
		if a[file] != dig {
			scope[file] = true
		}
	}
	for file := range a {
		if _, ok := b[file]; !ok {
			scope[file] = true
		}
	}
}

func anyDirty(dirty map[string]bool) bool {
	for _, d := range dirty {
		if d {
			return true
		}
	}
	return false
}

func dirtyCount(dirty map[string]bool) int {
	n := 0
	for _, d := range dirty {
		if d {
			n++
		}
	}
	return n
}

func lookupHash(hashes map[string]string, path string) (string, bool) {
	if h, ok := hashes[path]; ok {
		return h, true
	}
	slash := filepath.ToSlash(path)
	h, ok := hashes[slash]
	return h, ok
}

func lookupState(files map[string]*FileState, path string) *FileState {
	if files == nil {
		return nil
	}
	if st := files[path]; st != nil {
		return st
	}
	return files[filepath.ToSlash(path)]
}

func ownerOf(f facts.Fact) graphstream.OwnerRef {
	if f.Kind == facts.KindModule && (f.File == f.Name || f.File == "" || f.File == f.Name+"/") {
		return graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: "module:" + f.Name}
	}
	if f.Kind == facts.KindExtraction {
		return graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: "aggregate:typescript"}
	}
	file := filepath.ToSlash(f.File)
	if file == "" {
		return graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: "unfiled:" + f.Kind + ":" + f.Name}
	}
	// Directory-shaped provenance that is not a source file.
	if !strings.Contains(filepath.Base(file), ".") && f.Kind != facts.KindFileRef && f.Kind != facts.KindTestRef {
		if f.Kind == facts.KindModule {
			return graphstream.OwnerRef{Kind: graphstream.OwnerSynthetic, ID: "module:" + f.Name}
		}
	}
	return graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: file}
}

func applyLocalIO(ff []facts.Fact) {
	for i := range ff {
		f := &ff[i]
		if f.Kind != facts.KindSymbol {
			continue
		}
		direct, _ := f.Props["io_direct"].(bool)
		if direct {
			if f.Props == nil {
				f.Props = map[string]any{}
			}
			f.Props["performs_io"] = true
			continue
		}
		if f.Props != nil {
			delete(f.Props, "performs_io")
		}
	}
}

type idIndex struct {
	byName map[string][]facts.Fact
}

func buildIndex(ff []facts.Fact) *idIndex {
	idx := &idIndex{byName: map[string][]facts.Fact{}}
	for _, f := range ff {
		idx.byName[f.Name] = append(idx.byName[f.Name], f)
	}
	return idx
}

func (idx *idIndex) resolve(fromRepo, target string) (id, status string) {
	cands := idx.byName[target]
	if len(cands) == 0 {
		return "", graphstream.ResUnresolved
	}
	pick := -1
	for i, f := range cands {
		if fromRepo != "" && f.Repo != fromRepo {
			continue
		}
		if pick == -1 {
			pick = i
			continue
		}
		if cands[pick].Identity() != f.Identity() {
			return "", graphstream.ResAmbiguous
		}
	}
	if pick == -1 {
		id0 := cands[0].Identity()
		for _, f := range cands[1:] {
			if f.Identity() != id0 {
				return "", graphstream.ResAmbiguous
			}
		}
		return id0, graphstream.ResResolved
	}
	return cands[pick].Identity(), graphstream.ResResolved
}

type ownerOutput struct {
	Owner graphstream.OwnerRef
	Facts []facts.Fact
}

func groupOwners(ff []facts.Fact) []ownerOutput {
	order := make([]graphstream.OwnerRef, 0)
	grouped := map[string]*ownerOutput{}
	for _, f := range ff {
		o := ownerOf(f)
		key := o.String()
		g, ok := grouped[key]
		if !ok {
			g = &ownerOutput{Owner: o}
			grouped[key] = g
			order = append(order, o)
		}
		g.Facts = append(g.Facts, f)
	}
	out := make([]ownerOutput, 0, len(order))
	for _, o := range order {
		out = append(out, *grouped[o.String()])
	}
	return out
}

func encodeOwner(o ownerOutput, idx *idIndex, pending bool) (nodes []graphstream.Node, edges []graphstream.Edge) {
	occ := map[string]int{}
	for _, f := range o.Facts {
		id := f.Identity()
		n := occ[id]
		occ[id] = n + 1
		nodes = append(nodes, graphstream.Node{
			Owner:      o.Owner,
			ID:         id,
			Kind:       f.Kind,
			Name:       f.Name,
			File:       f.File,
			Line:       f.Line,
			EndLine:    f.EndLine,
			Column:     f.Column,
			EndColumn:  f.EndColumn,
			Repo:       f.Repo,
			Props:      f.Props,
			Occurrence: n,
		})
		for i, r := range f.Relations {
			e := graphstream.Edge{
				Owner:      o.Owner,
				FromID:     id,
				Kind:       r.Kind,
				TargetName: r.Target,
				Occurrence: i,
			}
			if pending {
				e.Resolution = graphstream.ResPending
			} else {
				tid, st := idx.resolve(f.Repo, r.Target)
				e.Resolution = st
				e.TargetID = tid
			}
			edges = append(edges, e)
		}
	}
	return nodes, edges
}

func deltaContextKey(ext plugin.Extractor) string { return "\x00delta-context:" + ext.Name() }

func captureDeltaContexts(eng *engine.Engine, root string, detected map[string]bool) map[string]string {
	out := map[string]string{}
	for _, ext := range eng.Extractors() {
		if !detected[ext.Name()] {
			continue
		}
		if d, ok := ext.(plugin.DeltaContext); ok {
			out[deltaContextKey(ext)] = d.DeltaContext(root)
		}
	}
	return out
}

// Candidate changes can affect references owned by another extractor even when
// that extractor's local facts did not change (for example a markdown file link
// gaining a TS file_ref target). Refresh those owners without re-extraction.
//
// The resolver domain for a name is the sorted Fact.Identity() multiset in
// idIndex.byName. changedResolutionOwners skips relation targets whose old and
// new fingerprints match, so timing/cache assembly cannot widen a stable owner
// such as app.ts. Additions, removals, and collisions change the fingerprint
// and remain fail-closed. Ordinary TypeScript edits do not fall back to the
// whole TS domain unless this check reports a real candidate change.
func changedResolutionOwners(groups []ownerOutput, old, next *idIndex, ignoredFiles map[string]bool) []graphstream.OwnerRef {
	changedNames := changedCandidateNames(old, next, ignoredFiles)
	if len(changedNames) == 0 {
		return nil
	}
	var owners []graphstream.OwnerRef
	for _, g := range groups {
		if ownerReferencesChangedCandidates(g, changedNames) {
			owners = append(owners, g.Owner)
		}
	}
	return owners
}

func ownerReferencesChangedCandidates(g ownerOutput, changedNames map[string]bool) bool {
	if g.Owner.Kind != graphstream.OwnerFile {
		return false
	}
	for _, f := range g.Facts {
		for _, rel := range f.Relations {
			if rel.Target != "" && changedNames[rel.Target] {
				return true
			}
		}
	}
	return false
}

func changedCandidateNames(old, next *idIndex, ignored ...map[string]bool) map[string]bool {
	ignoredFiles := map[string]bool{}
	if len(ignored) > 0 && ignored[0] != nil {
		ignoredFiles = ignored[0]
	}
	changed := map[string]bool{}
	all := map[string]bool{}
	if old != nil {
		for name := range old.byName {
			all[name] = true
		}
	}
	if next != nil {
		for name := range next.byName {
			all[name] = true
		}
	}
	for name := range all {
		if !slicesEqual(candidateFingerprint(old, name, ignoredFiles), candidateFingerprint(next, name, ignoredFiles)) {
			changed[name] = true
		}
	}
	return changed
}

// candidateFingerprint is the canonical resolver-domain key for one name: the
// sorted Fact.Identity() multiset from idx.byName. Line, column, and props are
// outside Identity, so a body-only edit does not change the fingerprint.
func candidateFingerprint(idx *idIndex, name string, ignoredFiles map[string]bool) []string {
	if idx == nil {
		return nil
	}
	if ignoredFiles == nil {
		ignoredFiles = map[string]bool{}
	}
	rows := make([]string, 0, len(idx.byName[name]))
	for _, f := range idx.byName[name] {
		if ignoredFiles[canonicalFactFile(f.File)] {
			continue
		}
		rows = append(rows, candidateIdentity(f))
	}
	sort.Strings(rows)
	return rows
}

func canonicalFactFile(file string) string {
	return filepath.ToSlash(strings.ReplaceAll(file, "\\", "/"))
}

// candidateIdentity is Fact.Identity after slash-normalizing File so cache and
// assembled indexes do not diverge on path separators. Repo/kind/name/file are
// the resolver domain; line, column, and props are not.
func candidateIdentity(f facts.Fact) string {
	f.File = canonicalFactFile(f.File)
	return f.Identity()
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stateResolutionIndex(st *State, repo string) *idIndex {
	if st == nil {
		return fileResolutionIndex(nil, repo)
	}
	return fileResolutionIndex(st.Files, repo)
}

// fileResolutionIndex builds the name-resolution domain from cached file
// contributions (FileRecord facts plus contrib). Session-level composition
// extras (router mounts, coverage aggregates, directory modules) live on
// ExtractSession.Facts and must not be mixed into this index: a CLI delta
// has no prior composed index, so comparing cache against published
// composition looks like a global candidate change.
func fileResolutionIndex(files map[string]*FileState, repo string) *idIndex {
	return buildIndex(fileContributionFacts(files, repo))
}

func fileContributionFacts(files map[string]*FileState, repo string) []facts.Fact {
	var ff []facts.Fact
	for _, rec := range files {
		ff = append(ff, fileFacts(rec)...)
		if rec != nil {
			for _, contrib := range rec.Contrib {
				ff = append(ff, contrib...)
			}
		}
	}
	return publishedResolutionFacts(ff, repo)
}

func publishedResolutionFacts(ff []facts.Fact, repo string) []facts.Fact {
	out := make([]facts.Fact, 0, len(ff))
	for _, f := range ff {
		if ownerOf(f).Kind == graphstream.OwnerSynthetic {
			continue
		}
		out = append(out, canonResolutionFact(f, repo))
	}
	return out
}

func canonResolutionFact(f facts.Fact, repo string) facts.Fact {
	if f.Repo == "" {
		f.Repo = repo
	}
	f.File = filepath.ToSlash(f.File)
	return f
}

func changedContentOwners(prev, next map[string]*FileState) map[string]bool {
	out := map[string]bool{}
	for path, st := range next {
		slash := filepath.ToSlash(path)
		old := lookupState(prev, path)
		if old == nil || st == nil || old.Hash != st.Hash {
			out[slash] = true
		}
	}
	for path := range prev {
		if lookupState(next, path) == nil {
			out[filepath.ToSlash(path)] = true
		}
	}
	return out
}

// overlayStablePublished copies assembled/composed facts for hash-stable owners
// onto the cached index so old and next share the current publish semantics.
// Facts owned by content-changed files stay on the next side only (fail-closed
// for new names and collisions).
func overlayStablePublished(base, published []facts.Fact, unstable map[string]bool) []facts.Fact {
	have := make(map[string]bool, len(base))
	for _, f := range base {
		have[candidateIdentity(f)] = true
	}
	out := append([]facts.Fact(nil), base...)
	for _, f := range published {
		o := ownerOf(f)
		if o.Kind == graphstream.OwnerFile && unstable[o.ID] {
			continue
		}
		k := candidateIdentity(f)
		if have[k] {
			continue
		}
		out = append(out, f)
		have[k] = true
	}
	return out
}

func resolutionIndexes(prev, nextFiles map[string]*FileState, assembled []facts.Fact, repo string) (oldIdx, nextIdx *idIndex) {
	published := publishedResolutionFacts(assembled, repo)
	oldFacts := overlayStablePublished(fileContributionFacts(prev, repo), published, changedContentOwners(prev, nextFiles))
	return buildIndex(oldFacts), buildIndex(published)
}
