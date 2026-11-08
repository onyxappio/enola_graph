package tsextractor

import (
	"path/filepath"
	"sync"

	"github.com/enola-labs/enola/internal/factpath"
)

// PluginResolver is a run-scoped cache for repository-plugin module answers.
// A Discovery and the inventory it receives are immutable for the duration of a
// graphsession run, so package-export aliases and per-directory module answers
// may be shared safely. Callers must construct a new resolver when either one
// changes; this deliberately prevents a stale answer from crossing a delta.
type PluginResolver struct {
	discovery      *Discovery
	known          map[string]bool
	packageAliases map[string]tsAlias
	mu             sync.Mutex
	aliases        map[string]map[string]tsAlias
	modules        map[pluginResolverKey]PluginModuleResolution
}

type pluginResolverKey struct {
	fromDir string
	spec    string
}

// NewPluginResolver retains the parsed Discovery state and materializes the
// inventory-dependent package aliases once. It is safe for concurrent plugin
// RPC handlers.
func (d *Discovery) NewPluginResolver(files []string) *PluginResolver {
	known := knownTSFiles(files)
	return &PluginResolver{
		discovery:      d,
		known:          known,
		packageAliases: d.packageAliasesFor(known),
		aliases:        map[string]map[string]tsAlias{},
		modules:        map[pluginResolverKey]PluginModuleResolution{},
	}
}

// ResolveModule returns the same answer as Discovery.ResolvePluginModule, but
// shares its known-file/alias setup and caches by the only source attribute
// that resolution observes: the requesting directory.
func (r *PluginResolver) ResolveModule(from, spec string) PluginModuleResolution {
	from = filepath.ToSlash(filepath.Clean(from))
	key := pluginResolverKey{fromDir: factpath.Dir(from), spec: spec}
	r.mu.Lock()
	defer r.mu.Unlock()
	if result, ok := r.modules[key]; ok {
		return clonePluginModuleResolution(result)
	}
	aliases, ok := r.aliases[key.fromDir]
	if !ok {
		aliases = mergePackageAliases(aliasesForDir(r.discovery.aliasRootsFor(), key.fromDir), r.packageAliases)
		r.aliases[key.fromDir] = aliases
	}
	resolved, _ := resolveImportPath(spec, key.fromDir, aliases)
	file, moduleDir, replaySpec, external := bindImportTarget(spec, key.fromDir, aliases, r.known)
	result := PluginModuleResolution{Resolved: resolved, File: file, ModuleDir: moduleDir, ReplaySpec: replaySpec, External: external}
	if !external {
		for _, candidate := range moduleResolutionCandidates(resolved) {
			result.Candidates = append(result.Candidates, candidate)
			if r.known[candidate] {
				break
			}
		}
	}
	r.modules[key] = result
	return clonePluginModuleResolution(result)
}

func clonePluginModuleResolution(in PluginModuleResolution) PluginModuleResolution {
	out := in
	out.Candidates = append([]string(nil), in.Candidates...)
	return out
}

// PluginModuleResolution is the host-owned answer to a repository plugin's
// module-resolution request. Candidates are ordered by resolver precedence and
// include the selected candidate, or every candidate when no file is present.
type PluginModuleResolution struct {
	Resolved   string   `json:"resolved"`
	File       string   `json:"file,omitempty"`
	ModuleDir  string   `json:"module_dir,omitempty"`
	ReplaySpec string   `json:"replay_spec,omitempty"`
	External   bool     `json:"external,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

// ResolvePluginModule applies the same aliases and extension/index precedence
// used by TypeScript import extraction. The returned candidate prefix is
// sufficient to detect a newly appearing higher-precedence target.
func (d *Discovery) ResolvePluginModule(from, spec string, files []string) PluginModuleResolution {
	known := knownTSFiles(files)
	aliases := mergePackageAliases(aliasesForDir(d.aliasRootsFor(), factpath.Dir(from)), d.packageAliasesFor(known))
	resolved, _ := resolveImportPath(spec, factpath.Dir(from), aliases)
	file, moduleDir, replaySpec, external := bindImportTarget(spec, factpath.Dir(from), aliases, known)
	result := PluginModuleResolution{Resolved: resolved, File: file, ModuleDir: moduleDir, ReplaySpec: replaySpec, External: external}
	if external {
		return result
	}
	for _, candidate := range moduleResolutionCandidates(resolved) {
		result.Candidates = append(result.Candidates, candidate)
		if known[candidate] {
			break
		}
	}
	return result
}

// ResolvePluginExport resolves an exported name through the same named-export
// index used by TypeScript import binding. readSource must serve captured bytes;
// onRead is called for each file whose bytes participate in the answer.
func (d *Discovery) ResolvePluginExport(file, exportName string, files []string, readSource func(string) []byte, onRead func(string)) (target, targetFile string) {
	known := knownTSFiles(files)
	aliases := mergePackageAliases(aliasesForDir(d.aliasRootsFor(), factpath.Dir(file)), d.packageAliasesFor(known))
	indexPath, moduleDir, found := file, "", false
	if known[filepath.ToSlash(file)] {
		indexPath, moduleDir, found = filepath.ToSlash(file), factpath.Dir(file), true
	} else if resolved, dir, ok := resolveModuleFile(filepath.ToSlash(file), known); ok {
		indexPath, moduleDir, found = resolved, dir, true
	}
	if !found {
		return "", ""
	}
	read := func(rel string) []byte {
		if onRead != nil {
			onRead(filepath.ToSlash(rel))
		}
		if readSource == nil {
			return nil
		}
		return readSource(filepath.ToSlash(rel))
	}
	return bindImportedSymbol(moduleDir, indexPath, exportName, filepath.ToSlash(file), true, read, aliases, known, newNamedExportCache(), nil)
}

func knownTSFiles(files []string) map[string]bool {
	known := make(map[string]bool, len(files))
	for _, file := range files {
		file = filepath.ToSlash(file)
		if isTypeScriptFile(file) {
			known[file] = true
		}
	}
	return known
}

func moduleResolutionCandidates(resolved string) []string {
	if candidates := tsExtensionSubstitutionCandidates(resolved); len(candidates) > 0 {
		return candidates
	}
	out := []string{filepath.ToSlash(resolved)}
	for _, ext := range tsModuleExts {
		out = append(out, filepath.ToSlash(resolved+ext))
	}
	for _, ext := range tsModuleExts {
		out = append(out, filepath.ToSlash(resolved+"/index"+ext))
	}
	for _, ext := range []string{".d.ts", ".d.mts", ".d.cts"} {
		out = append(out, filepath.ToSlash(resolved+ext))
	}
	for _, ext := range []string{".d.ts", ".d.mts", ".d.cts"} {
		out = append(out, filepath.ToSlash(resolved+"/index"+ext))
	}
	return out
}
