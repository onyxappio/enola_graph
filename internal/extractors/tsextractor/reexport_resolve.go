package tsextractor

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/factpath"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// namedExportCache holds one parsed export index per file for a session.
type namedExportCache struct {
	mu      sync.Mutex
	byFile  map[string]*namedExportEntry
	scans   *atomic.Int32
	derived *atomic.Int32
}

// namedExportEntry is one file's index, parsed once however many goroutines ask
// for it at once. The single flight is also what makes scans a count of files
// scanned rather than of races lost: under the previous check-then-act two
// workers could miss together, parse the same file twice, and both increment.
type namedExportEntry struct {
	once sync.Once
	idx  *namedExportIndex
	// ready publishes the filled index to readers that must not join the
	// single flight. peek is the only such reader: recording a proof may
	// never trigger the scan it is trying to avoid, and reading idx without
	// calling once.Do would race with the goroutine filling it.
	ready atomic.Pointer[namedExportIndex]
}

type namedExportIndex struct {
	local       map[string]bool
	named       map[string][][2]string // exported name → (module file, original name)
	unresolved  map[string]bool        // imported locals whose module origin is not indexed
	stars       []string
	defaultName string // proven default export symbol; empty if the file has none
	empty       bool
	// contextFree is what buildNamedExportIndex reported for this object, kept
	// on the object so a holder cannot lose it. An index reached through any
	// module specifier is false; see buildNamedExportIndex. Conservatively
	// false whenever it was not computed, so an unknown provenance proves
	// nothing.
	contextFree bool
}

func newNamedExportCache() *namedExportCache {
	return &namedExportCache{byFile: map[string]*namedExportEntry{}, scans: &atomic.Int32{}, derived: &atomic.Int32{}}
}

func (c *namedExportCache) summaryScans() int {
	if c == nil || c.scans == nil {
		return 0
	}
	return int(c.scans.Load())
}

// derivedIndexes counts entries filled from a tree the extractor had already
// parsed. Deliberately not folded into summaryScans: that number must stay a
// count of real parses, so a file whose index cost nothing is visible as
// nothing rather than as a scan that did not happen.
func (c *namedExportCache) derivedIndexes() int {
	if c == nil || c.derived == nil {
		return 0
	}
	return int(c.derived.Load())
}

// adopt fills file's entry from an index built off an already-parsed tree.
// It reports whether this call is the one that filled the entry; a later
// index() for the same file then returns this object without parsing.
//
// Only context-free indexes may be adopted. An index that names a module -
// anything reached through an `export ... from`, or an exported specifier that
// came from the import map - depends on the alias map and known-file set of
// whoever built it, and this session's cache is keyed by file alone. A
// context-free index has no such dependency: the same bytes yield it under any
// alias map, so sharing it cannot make a consumer see a different answer than
// the parse it replaced. buildNamedExportIndex decides that, not the caller.
func (c *namedExportCache) adopt(file string, idx *namedExportIndex) bool {
	if c == nil || idx == nil {
		return false
	}
	file = filepath.ToSlash(file)
	c.mu.Lock()
	entry, ok := c.byFile[file]
	if !ok {
		entry = &namedExportEntry{}
		c.byFile[file] = entry
	}
	c.mu.Unlock()
	filled := false
	entry.once.Do(func() {
		entry.idx = idx
		entry.ready.Store(idx)
		filled = true
		if c.derived != nil {
			c.derived.Add(1)
		}
	})
	return filled
}

func (c *namedExportCache) index(file string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool) *namedExportIndex {
	file = filepath.ToSlash(file)
	if c == nil {
		return parseNamedExportIndex(file, readSrc, aliases, knownFiles)
	}
	c.mu.Lock()
	entry, ok := c.byFile[file]
	if !ok {
		entry = &namedExportEntry{}
		c.byFile[file] = entry
	}
	c.mu.Unlock()
	entry.once.Do(func() {
		entry.idx = parseNamedExportIndex(file, readSrc, aliases, knownFiles)
		entry.ready.Store(entry.idx)
		if c.scans != nil {
			c.scans.Add(1)
		}
	})
	return entry.idx
}

// peek returns this file's index only if one is already in hand, and never
// causes one to be built. It exists so a record can note a proof the session
// already paid for - the index ts.go adopted off the tree it parsed anyway -
// without turning every recorded file into a summary scan. A miss is not an
// answer of "no index" but of "none yet", and every caller must treat it as
// unknown.
func (c *namedExportCache) peek(file string) *namedExportIndex {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	entry := c.byFile[filepath.ToSlash(file)]
	c.mu.Unlock()
	return entry.readyIndex()
}

// isContextFree is nil-safe so callers can chain it onto a peek that missed.
func (i *namedExportIndex) isContextFree() bool {
	return i != nil && i.contextFree
}

func (e *namedExportEntry) readyIndex() *namedExportIndex {
	if e == nil {
		return nil
	}
	return e.ready.Load()
}

func parseNamedExportIndex(file string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool) *namedExportIndex {
	idx := &namedExportIndex{local: map[string]bool{}, named: map[string][][2]string{}, unresolved: map[string]bool{}}
	if file == "" || readSrc == nil {
		idx.empty = true
		return idx
	}
	// Single-file-origin paths below return an index built elsewhere, which
	// carries its own provenance; the block-merging paths start from this
	// object, so it starts context-free and mergeNamedExportIndex narrows it.
	idx.contextFree = true
	src := readSrc(file)
	if isVueFile(file) {
		idx.defaultName = fileSymbolName(file)
		blocks := extractVueScriptBlocks(src)
		if len(blocks) == 0 {
			if len(src) == 0 {
				idx.empty = true
			}
			return idx
		}
		for _, b := range blocks {
			if len(b.Content) == 0 {
				continue
			}
			sub := parseNamedExportIndexBytes(file, b.Content, aliases, knownFiles, sourceSyntaxForEmbeddedScript(b.Lang))
			mergeNamedExportIndex(idx, sub)
		}
		return idx
	}
	if isSvelteFile(file) {
		idx.defaultName = fileSymbolName(file)
		blocks := extractSvelteScriptBlocks(src)
		if len(blocks) == 0 {
			if len(src) == 0 {
				idx.empty = true
			}
			return idx
		}
		for _, b := range blocks {
			if len(b.Content) == 0 {
				continue
			}
			sub := parseNamedExportIndexBytes(file, b.Content, aliases, knownFiles, sourceSyntaxForEmbeddedScript(b.Lang))
			mergeNamedExportIndex(idx, sub)
		}
		return idx
	}
	if isEmberTemplateTagFile(file) {
		src, _ = blankEmberTemplates(src)
	}
	if len(src) == 0 {
		idx.empty = true
		return idx
	}
	return parseNamedExportIndexBytes(file, src, aliases, knownFiles, sourceSyntaxForFile(file))
}

func mergeNamedExportIndex(dst, src *namedExportIndex) {
	// One block that consulted a specifier makes the merged answer context
	// dependent, whatever the others did - and so does one block nobody could
	// read. A missing block, or an empty one that carries no proof of its own
	// (a nil root, a parser that would not take the language), is unknown
	// rather than known-empty: it may name a module the merged index does not
	// show. Only a block proven empty - zero bytes, no specifier consulted -
	// leaves the proof standing. This narrowing happens before the early
	// return so an unknown block cannot pass through unaccounted.
	if src == nil || !src.contextFree {
		dst.contextFree = false
	}
	if src == nil || src.empty {
		return
	}
	for k, v := range src.local {
		dst.local[k] = v
	}
	for k, v := range src.named {
		dst.named[k] = append(dst.named[k], v...)
	}
	for k, v := range src.unresolved {
		dst.unresolved[k] = v
	}
	dst.stars = append(dst.stars, src.stars...)
	if dst.defaultName == "" {
		dst.defaultName = src.defaultName
	}
}

func parseNamedExportIndexBytes(file string, src []byte, aliases map[string]tsAlias, knownFiles map[string]bool, syntaxOverride ...parserSyntax) *namedExportIndex {
	syntax := sourceSyntaxForFile(file)
	if len(syntaxOverride) > 0 {
		syntax = syntaxOverride[0]
	}
	idx := &namedExportIndex{local: map[string]bool{}, named: map[string][][2]string{}, unresolved: map[string]bool{}}
	if len(src) == 0 {
		idx.empty = true
		idx.contextFree = true
		return idx
	}
	isTSX := strings.HasSuffix(file, ".tsx") || strings.HasSuffix(file, ".jsx")
	kinds := tsKindsFor(isTSX)
	lang := typescript.LanguageTypescript()
	if isTSX {
		lang = typescript.LanguageTSX()
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		idx.empty = true
		return idx
	}
	tree := parseSourceWithSyntax(parser, src, syntax)
	defer tree.Close()
	idx, _ = buildNamedExportIndex(file, src, kinds, tree.RootNode(), aliases, knownFiles)
	return idx
}

// buildNamedExportIndex reads one module's export surface off an already-parsed
// root. It also reports whether the result is context-free, i.e. whether the
// same bytes would have produced the same index under any alias map and
// known-file set. Anything that consults a module specifier makes it false,
// including a specifier that failed to resolve: a failed resolution silently
// emits no entry, so an index that looks empty can still be one alias away from
// naming a module.
func buildNamedExportIndex(file string, src []byte, kinds *tsutil.KindTable, root *sitter.Node, aliases map[string]tsAlias, knownFiles map[string]bool) (*namedExportIndex, bool) {
	idx := &namedExportIndex{local: map[string]bool{}, named: map[string][][2]string{}, unresolved: map[string]bool{}}
	if root == nil {
		idx.empty = true
		return idx, false
	}
	// Empty bytes parse to a valid, childless root, so without this the index
	// would come back not-empty while a scan of the same file returns the empty
	// marker - and surface() reports that marker, so the two would disagree on
	// a recorded ExportSurface. A scan short-circuits on length before it ever
	// parses; match it exactly. The result is still context-free: no specifier
	// was consulted, so it is safe to adopt and is byte-for-byte what a scan
	// would have produced.
	if len(src) == 0 {
		idx.empty = true
		idx.contextFree = true
		return idx, true
	}
	contextFree := true
	fileDir := factpath.Dir(file)
	imported := map[string][2]string{}
	unresolved := map[string]bool{}
	localBind := map[string]bool{}
	collectModuleBindings(kinds, root, src, fileDir, aliases, knownFiles, imported, unresolved, localBind)
	idx.unresolved = unresolved

	for i := range root.ChildCount() {
		child := root.Child(i)
		if kindOf(kinds, child) != "export_statement" {
			continue
		}
		if hasChildKind(kinds, child, "default") {
			if idx.defaultName == "" {
				if decl := firstDeclChild(kinds, child); decl != nil {
					if names := declExportedNames(kinds, decl, src); len(names) > 0 {
						idx.defaultName = names[0]
					}
				}
				if idx.defaultName == "" {
					if name := defaultExportLocalName(kinds, child, src); name != "" {
						idx.defaultName = name
					} else {
						idx.defaultName = fileSymbolName(file)
					}
				}
			}
			continue
		}
		source := child.ChildByFieldName("source")
		if source != nil {
			contextFree = false
			importPath := strings.Trim(nodeText(source, src), `"'`)
			resolved, external := resolveImportPath(importPath, fileDir, aliases)
			if external {
				continue
			}
			mod, _, ok := resolveModuleFile(resolved, knownFiles)
			if !ok {
				continue
			}
			if clause := findChildByKind(kinds, child, "export_clause"); clause != nil {
				for j := range clause.ChildCount() {
					spec := clause.Child(j)
					if kindOf(kinds, spec) != "export_specifier" {
						continue
					}
					orig, exported, ok := exportSpecifierNames(kinds, spec, src)
					if !ok {
						continue
					}
					idx.named[exported] = append(idx.named[exported], [2]string{mod, orig})
				}
				continue
			}
			if findChildByKind(kinds, child, "namespace_export") != nil {
				continue
			}
			idx.stars = append(idx.stars, mod)
			continue
		}
		if decl := firstDeclChild(kinds, child); decl != nil {
			for _, n := range declExportedNames(kinds, decl, src) {
				idx.local[n] = true
			}
			continue
		}
		if clause := findChildByKind(kinds, child, "export_clause"); clause != nil {
			for j := range clause.ChildCount() {
				spec := clause.Child(j)
				if kindOf(kinds, spec) != "export_specifier" {
					continue
				}
				local, exported, ok := exportSpecifierNames(kinds, spec, src)
				if !ok {
					continue
				}
				if !localBind[local] {
					// Whether this name is a re-export or a local one is decided
					// by the import map, which resolution built; the outcome is
					// context-dependent either way, so record that before the
					// lookup rather than only on a hit.
					contextFree = false
					if unresolved[local] {
						continue
					}
					if bind, ok := imported[local]; ok {
						idx.named[exported] = append(idx.named[exported], bind)
						continue
					}
				}
				idx.local[exported] = true
			}
		}
	}
	idx.contextFree = contextFree
	return idx, contextFree
}

func collectModuleBindings(kinds *tsutil.KindTable, root *sitter.Node, src []byte, fileDir string, aliases map[string]tsAlias, knownFiles map[string]bool, imported map[string][2]string, unresolved, localBind map[string]bool) {
	if root == nil {
		return
	}
	for i := range root.ChildCount() {
		child := root.Child(i)
		kind := kindOf(kinds, child)
		switch kind {
		case "import_statement":
			source := findChildByKind(kinds, child, "string")
			if source == nil {
				continue
			}
			importPath := strings.Trim(nodeText(source, src), `"'`)
			resolved, external := resolveImportPath(importPath, fileDir, aliases)
			mod := ""
			if !external {
				mod, _, _ = resolveModuleFile(resolved, knownFiles)
			}
			clause := findChildByKind(kinds, child, "import_clause")
			if clause == nil {
				continue
			}
			bindLocal := func(local, orig string) {
				if local == "" {
					return
				}
				if mod == "" {
					unresolved[local] = true
					return
				}
				imported[local] = [2]string{mod, orig}
			}
			var walkClause func(*sitter.Node)
			walkClause = func(n *sitter.Node) {
				if n == nil {
					return
				}
				switch kindOf(kinds, n) {
				case "import_specifier":
					nameNode := n.ChildByFieldName("name")
					if nameNode == nil {
						return
					}
					orig := nodeText(nameNode, src)
					local := orig
					if a := n.ChildByFieldName("alias"); a != nil {
						local = nodeText(a, src)
					}
					bindLocal(local, orig)
					return
				case "identifier":
					bindLocal(nodeText(n, src), "default")
					return
				case "namespace_import":
					if id := findChildByKind(kinds, n, "identifier"); id != nil {
						bindLocal(nodeText(id, src), "*")
					}
					return
				}
				for j := range n.NamedChildCount() {
					walkClause(n.NamedChild(j))
				}
			}
			walkClause(clause)
		case "export_statement":
			if decl := firstDeclChild(kinds, child); decl != nil {
				for _, n := range declExportedNames(kinds, decl, src) {
					localBind[n] = true
				}
			}
		case "function_declaration", "class_declaration", "abstract_class_declaration", "enum_declaration", "interface_declaration", "type_alias_declaration":
			if id := child.ChildByFieldName("name"); id != nil {
				if n := nodeText(id, src); n != "" {
					localBind[n] = true
				}
			}
		case "lexical_declaration", "variable_declaration":
			for _, n := range declExportedNames(kinds, child, src) {
				localBind[n] = true
			}
		}
	}
}

type followKind int

const (
	followNone followKind = iota
	followOne
	followMany
)

// followNamedExportFile walks proven `export { name } from` and `export * from`
// edges. followOne returns the unique declaring file; followMany is a collision;
// followNone is a missing or renamed-away export.
func followNamedExportFile(file, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, cache *namedExportCache, note func(string)) (string, string, followKind) {
	return followNamedExportFileSeen(file, exportName, readSrc, aliases, knownFiles, cache, note, map[string]bool{})
}

func followNamedExportFileSeen(file, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, cache *namedExportCache, note func(string), seen map[string]bool) (string, string, followKind) {
	file = filepath.ToSlash(file)
	if file == "" || exportName == "" || readSrc == nil {
		return "", "", followNone
	}
	key := file + "\x00" + exportName
	if seen[key] {
		return "", "", followNone
	}
	seen[key] = true
	if note != nil {
		note(file)
	}
	idx := cache.index(file, readSrc, aliases, knownFiles)
	if idx == nil || idx.empty {
		return "", "", followNone
	}
	if idx.local[exportName] {
		return file, exportName, followOne
	}
	if exportName == "default" && idx.defaultName != "" {
		return file, idx.defaultName, followOne
	}
	type owner struct {
		file, orig string
	}
	var owners []owner
	many := false
	add := func(o, orig string, k followKind) {
		if k == followMany {
			many = true
			return
		}
		if k != followOne || o == "" {
			return
		}
		for _, x := range owners {
			if x.file == o && x.orig == orig {
				return
			}
		}
		owners = append(owners, owner{file: o, orig: orig})
	}
	for _, n := range idx.named[exportName] {
		leaf, orig, k := followNamedExportFileSeen(n[0], n[1], readSrc, aliases, knownFiles, cache, note, seen)
		add(leaf, orig, k)
	}
	for _, s := range idx.stars {
		leaf, orig, k := followNamedExportFileSeen(s, exportName, readSrc, aliases, knownFiles, cache, note, seen)
		add(leaf, orig, k)
	}
	if many || len(owners) > 1 {
		return "", "", followMany
	}
	if len(owners) == 1 {
		return owners[0].file, owners[0].orig, followOne
	}
	return "", "", followNone
}

func bindNamedImportFile(indexPath, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, cache *namedExportCache, note func(string)) (string, string, followKind) {
	if indexPath == "" {
		return "", "", followNone
	}
	start := filepath.ToSlash(indexPath)
	chainNote := func(f string) {
		f = filepath.ToSlash(f)
		if f == "" || note == nil {
			return
		}
		// Named follow already has start as a direct import. Default identity is
		// a side-read of start's bytes even when the leaf is start itself
		// (`export default round` vs `export default ceil`).
		if f == start && exportName != "default" {
			return
		}
		note(f)
	}
	leaf, orig, kind := followNamedExportFile(indexPath, exportName, readSrc, aliases, knownFiles, cache, chainNote)
	if note != nil && (kind != followOne || filepath.ToSlash(leaf) != start) {
		note(start)
	}
	switch kind {
	case followOne:
		return leaf, orig, followOne
	case followMany:
		return "", "", followMany
	default:
		return indexPath, exportName, followNone
	}
}

func declExportedNames(kinds *tsutil.KindTable, decl *sitter.Node, src []byte) []string {
	var names []string
	switch kindOf(kinds, decl) {
	case "function_declaration", "generator_function_declaration", "class_declaration",
		"abstract_class_declaration", "interface_declaration", "type_alias_declaration",
		"enum_declaration":
		if name := decl.ChildByFieldName("name"); name != nil {
			names = append(names, nodeText(name, src))
		}
	case "lexical_declaration", "variable_declaration":
		for j := range decl.ChildCount() {
			d := decl.Child(j)
			if kindOf(kinds, d) != "variable_declarator" {
				continue
			}
			nameNode := d.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = findChildByKind(kinds, d, "identifier")
			}
			if nameNode == nil {
				continue
			}
			if kindOf(kinds, nameNode) == "identifier" {
				names = append(names, nodeText(nameNode, src))
				continue
			}
			names = append(names, bindingNamesFromPattern(kinds, nameNode, src)...)
		}
	}
	return names
}

func exportSpecifierNames(kinds *tsutil.KindTable, spec *sitter.Node, src []byte) (orig, exported string, ok bool) {
	nameNode := spec.ChildByFieldName("name")
	if nameNode != nil {
		orig = nodeText(nameNode, src)
	} else if hasChildKind(kinds, spec, "default") {
		orig = "default"
	}
	if orig == "" {
		return "", "", false
	}
	exported = orig
	if a := spec.ChildByFieldName("alias"); a != nil {
		exported = nodeText(a, src)
	}
	return orig, exported, true
}

func declExportsName(kinds *tsutil.KindTable, decl *sitter.Node, src []byte, exportName string) bool {
	for _, n := range declExportedNames(kinds, decl, src) {
		if n == exportName {
			return true
		}
	}
	return false
}

// surface encodes everything a consumer can observe about this file's exports
// through followNamedExportFile, as a deterministic sorted list.
//
// This is the binder's own view, not a summary of it: `local` decides whether an
// exported name resolves here at all, `named` and `stars` decide which other file
// it forwards to and under which original name, and `defaultName` answers the
// export name `default`. Nothing else in the index is consulted, so two states
// with equal surfaces are indistinguishable to every consumer that reads these
// bytes - and a state whose surface moved is one where at least one consumer can
// bind differently, whatever the rest of the file did.
func (idx *namedExportIndex) surface() []string {
	if idx == nil {
		return nil
	}
	if idx.empty {
		return []string{"empty"}
	}
	out := make([]string, 0, len(idx.local)+len(idx.named)+len(idx.stars)+1)
	for name := range idx.local {
		out = append(out, "local:"+name)
	}
	for exported, targets := range idx.named {
		for _, t := range targets {
			out = append(out, "named:"+exported+"="+t[0]+"#"+t[1])
		}
	}
	for _, mod := range idx.stars {
		out = append(out, "star:"+mod)
	}
	if idx.defaultName != "" {
		out = append(out, "default:"+idx.defaultName)
	}
	sort.Strings(out)
	return out
}
