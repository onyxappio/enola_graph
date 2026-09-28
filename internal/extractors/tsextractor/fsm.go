package tsextractor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/fsm"
)

type fsmAnalyzerCache struct {
	baseKey     string
	analyzer    *fsm.Analyzer
	fileHashes  map[string]string
	aliasHashes map[string]string
	modelFiles  map[string]bool
}

func (e *TSExtractor) newFSMAnalyzer(ctx context.Context, repoPath string, knownFiles map[string]bool, read func(string) []byte, aliasesFor func(string) map[string]tsAlias) *fsm.Analyzer {
	if len(e.stateMachines) == 0 {
		return nil
	}
	baseKey := fsmAnalyzerBaseKey(e.stateMachines, knownFiles)
	e.fsmCacheMu.Lock()
	cached := e.fsmCache
	e.fsmCacheMu.Unlock()
	known := make([]string, 0, len(knownFiles))
	for f := range knownFiles {
		known = append(known, f)
	}
	readSource := func(rel string) ([]byte, error) {
		if b := read(filepath.ToSlash(rel)); b != nil {
			return b, nil
		}
		return nil, filepath.ErrBadPattern
	}
	resolveModule := func(fromFile, specifier string) (string, bool) {
		aliases := aliasesFor(fromFile)
		resolved, _, _, external := bindImportTarget(specifier, factpath.Dir(fromFile), aliases, knownFiles)
		if external || !knownFiles[filepath.ToSlash(resolved)] {
			return "", false
		}
		return filepath.ToSlash(resolved), true
	}
	if cached != nil && cached.baseKey == baseKey && cached.analyzer != nil {
		if !fsmAnalyzerModelChanged(cached, read, aliasesFor) {
			// Rebind even on a byte-identical cache hit so this run's resolver and
			// snapshot reader are used for any newly reached interaction inputs.
			return cached.analyzer.Rebind(readSource, resolveModule)
		}
	}
	return fsm.NewAnalyzer(e.stateMachines, known, readSource, resolveModule)
}

func (e *TSExtractor) rememberFSMAnalyzer(analyzer *fsm.Analyzer, knownFiles map[string]bool, read func(string) []byte, aliasesFor func(string) map[string]tsAlias) {
	if analyzer == nil || len(e.stateMachines) == 0 {
		return
	}
	cache := &fsmAnalyzerCache{
		baseKey:  fsmAnalyzerBaseKey(e.stateMachines, knownFiles),
		analyzer: analyzer, fileHashes: map[string]string{}, aliasHashes: map[string]string{}, modelFiles: map[string]bool{},
	}
	for _, file := range fsmAnalyzerInputFiles(e.stateMachines, analyzer) {
		cache.fileHashes[file] = fsmInputHash(read(file))
		if aliasesFor != nil {
			cache.aliasHashes[file] = fsmAliasHash(aliasesFor(file))
		}
	}
	for _, file := range fsmAnalyzerModelInputFiles(e.stateMachines, analyzer) {
		cache.modelFiles[file] = true
	}
	e.fsmCacheMu.Lock()
	e.fsmCache = cache
	e.fsmCacheMu.Unlock()
}

func fsmAnalyzerModelChanged(cache *fsmAnalyzerCache, read func(string) []byte, aliasesFor func(string) map[string]tsAlias) bool {
	for file, want := range cache.fileHashes {
		if cache.modelFiles[file] && fsmInputHash(read(file)) != want {
			return true
		}
	}
	if aliasesFor != nil {
		for file, want := range cache.aliasHashes {
			if cache.modelFiles[file] && fsmAliasHash(aliasesFor(file)) != want {
				return true
			}
		}
	}
	return false
}

func fsmAnalyzerBaseKey(specs []fsm.Spec, knownFiles map[string]bool) string {
	var b strings.Builder
	b.WriteString(fsm.Fingerprint(specs))
	b.WriteByte(0)
	paths := make([]string, 0, len(knownFiles))
	for path := range knownFiles {
		paths = append(paths, filepath.ToSlash(path))
	}
	sort.Strings(paths)
	for _, path := range paths {
		b.WriteString(path)
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func fsmAnalyzerInputFiles(specs []fsm.Spec, analyzer *fsm.Analyzer) []string {
	set := map[string]bool{}
	add := func(file string) {
		file = factpath.Clean(file)
		if file != "" && file != "." {
			set[file] = true
		}
	}
	for _, spec := range specs {
		add(spec.File)
		add(spec.Factory.Module)
		if spec.EffectRunner != nil {
			add(spec.EffectRunner.Module)
		}
		for _, file := range spec.HandlerFiles {
			add(file)
		}
		for _, file := range spec.DispatchFiles {
			add(file)
		}
		for _, sink := range spec.DispatchSinks {
			add(sink.Factory.Module)
			if sink.MachineTag != nil {
				add(sink.MachineTag.Module)
			}
		}
	}
	if analyzer != nil {
		for _, file := range analyzer.SourceFiles() {
			add(file)
		}
		// Rebound analyzers keep immutable model dependencies separately from
		// interaction reads, so they remain hash-checked across later sessions.
		for _, file := range analyzer.ModelSourceFiles() {
			add(file)
		}
	}
	out := make([]string, 0, len(set))
	for file := range set {
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func fsmAnalyzerModelInputFiles(specs []fsm.Spec, analyzer *fsm.Analyzer) []string {
	set := map[string]bool{}
	add := func(file string) {
		file = factpath.Clean(file)
		if file != "" && file != "." {
			set[file] = true
		}
	}
	for _, spec := range specs {
		add(spec.File)
		add(spec.Factory.Module)
	}
	if analyzer != nil {
		for _, file := range analyzer.ModelSourceFiles() {
			add(file)
		}
	}
	out := make([]string, 0, len(set))
	for file := range set {
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func fsmInputHash(src []byte) string {
	if src == nil {
		return "missing"
	}
	sum := sha256.Sum256(src)
	return "present:" + hex.EncodeToString(sum[:])
}

func fsmAliasHash(aliases map[string]tsAlias) string {
	keys := make([]string, 0, len(aliases))
	for key := range aliases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		alias := aliases[key]
		b.WriteString(key)
		b.WriteByte(0)
		b.WriteString(alias.replacement)
		b.WriteByte(0)
		b.WriteString(alias.suffix)
		if alias.exact {
			b.WriteByte(1)
		} else {
			b.WriteByte(0)
		}
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func mergeFSMFacts(base, added []facts.Fact) []facts.Fact {
	if len(added) == 0 {
		return base
	}
	out := append([]facts.Fact(nil), base...)
	for _, overlay := range added {
		if overlay.Kind != facts.KindSymbol || len(overlay.Relations) == 0 {
			out = append(out, overlay)
			continue
		}
		owner := fsmOverlayOwner(out, overlay)
		if owner < 0 {
			out = append(out, fsmUnboundOverlayEvidence(overlay))
			continue
		}
		for _, rel := range overlay.Relations {
			seen := false
			for _, existing := range out[owner].Relations {
				if existing.Kind == rel.Kind && existing.Target == rel.Target && existing.TargetFile == rel.TargetFile {
					seen = true
					break
				}
			}
			if !seen {
				out[owner].Relations = append(out[owner].Relations, rel)
			}
		}
		if out[owner].Props == nil {
			out[owner].Props = map[string]any{}
		}
		sites, _ := out[owner].Props["fsm_evidence_sites"].([]map[string]any)
		props := make(map[string]any, len(overlay.Props))
		for key, value := range overlay.Props {
			props[key] = value
		}
		relations := make([]map[string]string, 0, len(overlay.Relations))
		for _, rel := range overlay.Relations {
			relations = append(relations, map[string]string{"kind": rel.Kind, "target": rel.Target, "target_file": rel.TargetFile})
		}
		sites = append(sites, map[string]any{
			"line": overlay.Line, "end_line": overlay.EndLine,
			"props": props, "relations": relations,
		})
		out[owner].Props["fsm_evidence_sites"] = sites
	}
	return out
}

// fsmOverlayOwner selects only an existing source declaration. The interaction
// overlay's line is the binding site; it may lie inside a multiline declaration,
// so span containment is stronger than choosing the first same-name symbol.
func fsmOverlayOwner(base []facts.Fact, overlay facts.Fact) int {
	var candidates, exact []int
	canonical := factpath.Dir(filepath.ToSlash(overlay.File)) + "." + overlay.Name
	identity, _ := overlay.Props["fsm_source_identity"].(string)
	for i := range base {
		candidate := base[i]
		if candidate.Kind != facts.KindSymbol || filepath.ToSlash(candidate.File) != filepath.ToSlash(overlay.File) {
			continue
		}
		if identity != "" {
			if candidate.Name != identity || candidate.Props["fsm_source_binding"] != "tree_sitter_nested_declaration" {
				continue
			}
		} else if candidate.Name != overlay.Name && candidate.Name != canonical {
			continue
		}
		if overlay.Line > 0 && candidate.Line > 0 && candidate.EndLine > 0 &&
			(overlay.Line < candidate.Line || overlay.Line > candidate.EndLine) {
			continue
		}
		candidates = append(candidates, i)
		if overlay.Line > 0 && candidate.Line == overlay.Line {
			exact = append(exact, i)
		}
	}
	if len(exact) == 1 {
		return exact[0]
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return -1
}

func fsmUnboundOverlayEvidence(overlay facts.Fact) facts.Fact {
	props := make(map[string]any, len(overlay.Props)+3)
	for key, value := range overlay.Props {
		props[key] = value
	}
	props["coverage_status"] = "partial"
	props["extractor"] = "typescript:fsm"
	props["unresolved_source_binding"] = overlay.Name
	props["unresolved_source_bindings"] = 1
	relations := make([]map[string]string, 0, len(overlay.Relations))
	for _, rel := range overlay.Relations {
		relations = append(relations, map[string]string{"kind": rel.Kind, "target": rel.Target, "target_file": rel.TargetFile})
	}
	props["unbound_fsm_relations"] = relations
	return facts.Fact{Kind: facts.KindExtraction, Name: "typescript:fsm:source-binding:" + filepath.ToSlash(overlay.File) + ":" + overlay.Name,
		File: overlay.File, Line: overlay.Line, EndLine: overlay.EndLine, Props: props}
}

func appendFSMReads(rec *FileRecord, paths []string, repoPath string, read func(string) []byte) {
	if rec == nil {
		return
	}
	rec.FSMReads = nil
	if len(paths) == 0 {
		return
	}
	set := map[string]bool{}
	fsmSet := map[string]bool{}
	for _, f := range rec.SideReads {
		set[filepath.ToSlash(f)] = true
	}
	if rec.SideReadHashes == nil {
		rec.SideReadHashes = map[string]string{}
	}
	for _, f := range paths {
		f = filepath.ToSlash(f)
		if f == rec.File {
			continue
		}
		fsmSet[f] = true
		set[f] = true
		b := read(f)
		if b == nil {
			continue
		}
		sum := sha256.Sum256(b)
		rec.SideReadHashes[f] = hex.EncodeToString(sum[:])
	}
	rec.SideReads = rec.SideReads[:0]
	for f := range set {
		rec.SideReads = append(rec.SideReads, f)
	}
	sortStrings(rec.SideReads)
	for f := range fsmSet {
		rec.FSMReads = append(rec.FSMReads, f)
	}
	sortStrings(rec.FSMReads)
}

func sortStrings(s []string) { sort.Strings(s) }

func fsmReadFile(ctx context.Context, repoPath, rel string, input *inputscope.Scope, overlay map[string][]byte) []byte {
	if b, ok := overlay[filepath.ToSlash(rel)]; ok {
		return b
	}
	b, err := input.ReadFile(filepath.Join(repoPath, rel))
	if err != nil {
		return nil
	}
	return b
}

func (e *TSExtractor) fsmAliases(aliasesFor func(string) map[string]tsAlias) func(string) map[string]tsAlias {
	return func(from string) map[string]tsAlias {
		return aliasesFor(strings.TrimPrefix(filepath.ToSlash(from), "./"))
	}
}
