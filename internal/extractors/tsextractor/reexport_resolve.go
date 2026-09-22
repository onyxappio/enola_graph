package tsextractor

import (
	"path/filepath"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/factpath"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// followNamedExportFile returns the known source file that locally declares
// exportName, walking proven `export { name } from` and `export * from` edges.
// Multiple distinct owners yield "" so callers leave RelCalls unconstrained.
func followNamedExportFile(file, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool) string {
	return followNamedExportFileSeen(file, exportName, readSrc, aliases, knownFiles, map[string]bool{})
}

func followNamedExportFileSeen(file, exportName string, readSrc func(string) []byte, aliases map[string]tsAlias, knownFiles map[string]bool, seen map[string]bool) string {
	file = filepath.ToSlash(file)
	if file == "" || exportName == "" || readSrc == nil {
		return ""
	}
	key := file + "\x00" + exportName
	if seen[key] {
		return ""
	}
	seen[key] = true
	src := readSrc(file)
	if len(src) == 0 {
		return ""
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
		return ""
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	root := tree.RootNode()
	fileDir := factpath.Dir(file)

	local := false
	renamed := false
	var named [][2]string
	var stars []string
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
			idx, _, ok := resolveModuleFile(resolved, knownFiles)
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
					if exported == exportName {
						if orig != exportName {
							renamed = true
						} else {
							named = append(named, [2]string{idx, orig})
						}
					}
				}
				continue
			}
			if findChildByKind(kinds, child, "namespace_export") != nil {
				continue
			}
			stars = append(stars, idx)
			continue
		}
		if decl := firstDeclChild(kinds, child); decl != nil {
			if declExportsName(kinds, decl, src, exportName) {
				local = true
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
				exported := nodeText(nameNode, src)
				if a := spec.ChildByFieldName("alias"); a != nil {
					exported = nodeText(a, src)
				}
				if exported == exportName {
					local = true
				}
			}
		}
	}
	if local {
		return file
	}
	if renamed {
		return ""
	}
	var owners []string
	add := func(o string) {
		if o == "" {
			return
		}
		for _, x := range owners {
			if x == o {
				return
			}
		}
		owners = append(owners, o)
	}
	for _, n := range named {
		add(followNamedExportFileSeen(n[0], n[1], readSrc, aliases, knownFiles, seen))
	}
	for _, s := range stars {
		add(followNamedExportFileSeen(s, exportName, readSrc, aliases, knownFiles, seen))
	}
	if len(owners) == 1 {
		return owners[0]
	}
	return ""
}

func declExportsName(kinds *tsutil.KindTable, decl *sitter.Node, src []byte, exportName string) bool {
	switch kindOf(kinds, decl) {
	case "function_declaration", "generator_function_declaration", "class_declaration",
		"abstract_class_declaration", "interface_declaration", "type_alias_declaration",
		"enum_declaration":
		if name := decl.ChildByFieldName("name"); name != nil {
			return nodeText(name, src) == exportName
		}
	case "lexical_declaration", "variable_declaration":
		for j := range decl.ChildCount() {
			d := decl.Child(j)
			if kindOf(kinds, d) != "variable_declarator" {
				continue
			}
			if id := findChildByKind(kinds, d, "identifier"); id != nil && nodeText(id, src) == exportName {
				return true
			}
		}
	}
	return false
}
