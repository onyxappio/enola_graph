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
		file = filepath.ToSlash(filepath.Clean(file))
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
		file = filepath.ToSlash(filepath.Clean(file))
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
		merged := false
		for i := range out {
			if out[i].Kind != facts.KindSymbol || out[i].Name != overlay.Name || filepath.ToSlash(out[i].File) != filepath.ToSlash(overlay.File) {
				continue
			}
			for _, rel := range overlay.Relations {
				seen := false
				for _, existing := range out[i].Relations {
					if existing.Kind == rel.Kind && existing.Target == rel.Target {
						seen = true
						break
					}
				}
				if !seen {
					out[i].Relations = append(out[i].Relations, rel)
				}
			}
			if out[i].Props == nil {
				out[i].Props = map[string]any{}
			}
			sites, _ := out[i].Props["fsm_evidence_sites"].([]map[string]any)
			props := make(map[string]any, len(overlay.Props))
			for key, value := range overlay.Props {
				props[key] = value
			}
			relations := make([]map[string]string, 0, len(overlay.Relations))
			for _, rel := range overlay.Relations {
				relations = append(relations, map[string]string{"kind": rel.Kind, "target": rel.Target})
			}
			sites = append(sites, map[string]any{
				"line": overlay.Line, "end_line": overlay.EndLine,
				"props": props, "relations": relations,
			})
			out[i].Props["fsm_evidence_sites"] = sites
			merged = true
			break
		}
		if !merged {
			if overlay.Props == nil {
				overlay.Props = map[string]any{}
			}
			if overlay.Props["symbol_kind"] == nil {
				overlay.Props["symbol_kind"] = facts.SymbolFunc
			}
			out = append(out, overlay)
		}
	}
	return out
}

func appendFSMReads(rec *FileRecord, paths []string, repoPath string, read func(string) []byte) {
	if rec == nil || len(paths) == 0 {
		return
	}
	set := map[string]bool{}
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
