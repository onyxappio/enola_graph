package tsextractor

import (
	"path/filepath"
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
	mu     sync.Mutex
	byFile map[string]*namedExportIndex
	scans  *atomic.Int32
}

type namedExportIndex struct {
	local map[string]bool
	named map[string][][2]string // exported name → (module file, original name)
	stars []string
	empty bool
}

func newNamedExportCache() *namedExportCache {
	return &namedExportCache{byFile: map[string]*namedExportIndex{}, scans: &atomic.Int32{}}
}

func (c *namedExportCache) summaryScans() int {
	if c == nil || c.scans == nil {
		return 0
	}
	return int(c.scans.Load())
}

func (c *namedExportCache) index(file string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool) *namedExportIndex {
	file = filepath.ToSlash(file)
	if c == nil {
		return parseNamedExportIndex(file, readSrc, aliases, knownFiles)
	}
	c.mu.Lock()
	if idx, ok := c.byFile[file]; ok {
		c.mu.Unlock()
		return idx
	}
	c.mu.Unlock()
	idx := parseNamedExportIndex(file, readSrc, aliases, knownFiles)
	if c.scans != nil {
		c.scans.Add(1)
	}
	c.mu.Lock()
	if existing, ok := c.byFile[file]; ok {
		c.mu.Unlock()
		return existing
	}
	c.byFile[file] = idx
	c.mu.Unlock()
	return idx
}

func parseNamedExportIndex(file string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool) *namedExportIndex {
	idx := &namedExportIndex{local: map[string]bool{}, named: map[string][][2]string{}}
	if file == "" || readSrc == nil {
		idx.empty = true
		return idx
	}
	src := readSrc(file)
	if len(src) == 0 {
		idx.empty = true
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
	tree := parser.Parse(src, nil)
	defer tree.Close()
	root := tree.RootNode()
	fileDir := factpath.Dir(file)
	imported := map[string][2]string{}
	localBind := map[string]bool{}
	collectModuleBindings(kinds, root, src, fileDir, aliases, knownFiles, imported, localBind)

	for i := range root.ChildCount() {
		child := root.Child(i)
		if kindOf(kinds, child) != "export_statement" {
			continue
		}
		if hasChildKind(kinds, child, "default") {
			continue
		}
		source := child.ChildByFieldName("source")
		if source != nil {
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
					nameNode := spec.ChildByFieldName("name")
					if nameNode == nil {
						continue
					}
					orig := nodeText(nameNode, src)
					exported := orig
					if a := spec.ChildByFieldName("alias"); a != nil {
						exported = nodeText(a, src)
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
				nameNode := spec.ChildByFieldName("name")
				if nameNode == nil {
					continue
				}
				local := nodeText(nameNode, src)
				exported := local
				if a := spec.ChildByFieldName("alias"); a != nil {
					exported = nodeText(a, src)
				}
				if !localBind[local] {
					if bind, ok := imported[local]; ok {
						idx.named[exported] = append(idx.named[exported], bind)
						continue
					}
				}
				idx.local[exported] = true
			}
		}
	}
	return idx
}

func collectModuleBindings(kinds *tsutil.KindTable, root *sitter.Node, src []byte, fileDir string, aliases map[string]tsAlias, knownFiles map[string]bool, imported map[string][2]string, localBind map[string]bool) {
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
			if external {
				continue
			}
			mod, _, ok := resolveModuleFile(resolved, knownFiles)
			if !ok {
				continue
			}
			clause := findChildByKind(kinds, child, "import_clause")
			if clause == nil {
				continue
			}
			var walkClause func(*sitter.Node)
			walkClause = func(n *sitter.Node) {
				if n == nil {
					return
				}
				if kindOf(kinds, n) == "import_specifier" {
					nameNode := n.ChildByFieldName("name")
					if nameNode == nil {
						return
					}
					orig := nodeText(nameNode, src)
					local := orig
					if a := n.ChildByFieldName("alias"); a != nil {
						local = nodeText(a, src)
					}
					if local != "" {
						imported[local] = [2]string{mod, orig}
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
func followNamedExportFile(file, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, cache *namedExportCache, note func(string)) (string, followKind) {
	return followNamedExportFileSeen(file, exportName, readSrc, aliases, knownFiles, cache, note, map[string]bool{})
}

func followNamedExportFileSeen(file, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, cache *namedExportCache, note func(string), seen map[string]bool) (string, followKind) {
	file = filepath.ToSlash(file)
	if file == "" || exportName == "" || readSrc == nil {
		return "", followNone
	}
	key := file + "\x00" + exportName
	if seen[key] {
		return "", followNone
	}
	seen[key] = true
	if note != nil {
		note(file)
	}
	idx := cache.index(file, readSrc, aliases, knownFiles)
	if idx == nil || idx.empty {
		return "", followNone
	}
	if idx.local[exportName] {
		return file, followOne
	}
	var owners []string
	many := false
	add := func(o string, k followKind) {
		if k == followMany {
			many = true
			return
		}
		if k != followOne || o == "" {
			return
		}
		for _, x := range owners {
			if x == o {
				return
			}
		}
		owners = append(owners, o)
	}
	for _, n := range idx.named[exportName] {
		leaf, k := followNamedExportFileSeen(n[0], n[1], readSrc, aliases, knownFiles, cache, note, seen)
		add(leaf, k)
	}
	for _, s := range idx.stars {
		leaf, k := followNamedExportFileSeen(s, exportName, readSrc, aliases, knownFiles, cache, note, seen)
		add(leaf, k)
	}
	if many || len(owners) > 1 {
		return "", followMany
	}
	if len(owners) == 1 {
		return owners[0], followOne
	}
	return "", followNone
}

func bindNamedImportFile(indexPath, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, cache *namedExportCache, note func(string)) string {
	if indexPath == "" {
		return ""
	}
	start := filepath.ToSlash(indexPath)
	chainNote := func(f string) {
		f = filepath.ToSlash(f)
		if f == "" || f == start || note == nil {
			return
		}
		note(f)
	}
	leaf, kind := followNamedExportFile(indexPath, exportName, readSrc, aliases, knownFiles, cache, chainNote)
	if note != nil && (kind != followOne || filepath.ToSlash(leaf) != start) {
		note(start)
	}
	switch kind {
	case followOne:
		return leaf
	case followMany:
		return ""
	default:
		return indexPath
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
			if id := findChildByKind(kinds, d, "identifier"); id != nil {
				names = append(names, nodeText(id, src))
			}
		}
	}
	return names
}

func declExportsName(kinds *tsutil.KindTable, decl *sitter.Node, src []byte, exportName string) bool {
	for _, n := range declExportedNames(kinds, decl, src) {
		if n == exportName {
			return true
		}
	}
	return false
}
