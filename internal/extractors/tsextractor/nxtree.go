package tsextractor

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/factpath"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Proven Nx Tree bindings for HTTP-client suppression. Name-only "tree"
// identifiers, an @nx/devkit import with no typed binding, and sibling
// .write/.delete evidence are not sufficient. Unknown HTTP receivers named
// tree still emit client routes. A recognized identifier prefix is not proof
// of the complete annotation: unions, intersections, generics, local type
// shadows, and ambiguous schema sources stay conservative.

type nxTreeScope struct {
	name       string
	start, end int
	isTree     bool
}

var (
	nxDevkitNamedImport = regexp.MustCompile(`(?m)import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+['"]@nx/devkit['"]`)
	relativeNamedImport = regexp.MustCompile(`(?m)import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+['"](\.[^'"]+)['"]`)
	interfaceOpen       = regexp.MustCompile(`(?:export\s+)?(?:declare\s+)?interface\s+([A-Za-z_$][\w$]*)\s*\{`)
	typeObjectOpen      = regexp.MustCompile(`(?:export\s+)?(?:declare\s+)?type\s+([A-Za-z_$][\w$]*)\s*=\s*\{`)
	schemaField         = regexp.MustCompile(`(?m)^\s*([A-Za-z_$][\w$]*)\s*\??\s*:\s*([A-Za-z_$][\w$]*)\s*[;,\n]`)
)

func importedNxTreeNames(src []byte) map[string]bool {
	local := map[string]bool{}
	mask := tsCommentStringMask(src)
	for _, loc := range nxDevkitNamedImport.FindAllSubmatchIndex(src, -1) {
		if mask[loc[0]] {
			continue
		}
		collectNamedImportAliases(string(src[loc[2]:loc[3]]), "Tree", local)
	}
	return local
}

func collectNamedImportAliases(inner, want string, local map[string]bool) {
	for _, spec := range strings.Split(inner, ",") {
		spec = strings.TrimSpace(spec)
		spec = strings.TrimPrefix(spec, "type ")
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		name, alias := spec, ""
		if parts := strings.Split(spec, " as "); len(parts) == 2 {
			name = strings.TrimSpace(parts[0])
			alias = strings.TrimSpace(parts[1])
		}
		if name != want {
			continue
		}
		if alias != "" {
			local[alias] = true
		} else {
			local[name] = true
		}
	}
}

func collectNamedImportLocals(inner string) map[string]string {
	out := map[string]string{}
	for _, spec := range strings.Split(inner, ",") {
		spec = strings.TrimSpace(spec)
		spec = strings.TrimPrefix(spec, "type ")
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		name, alias := spec, spec
		if parts := strings.Split(spec, " as "); len(parts) == 2 {
			name = strings.TrimSpace(parts[0])
			alias = strings.TrimSpace(parts[1])
		}
		out[alias] = name
	}
	return out
}

// schemaNxTreeFields maps an imported local type name to field names whose
// declared type is the Nx Tree imported in that schema file. Inheritance,
// generics, and ambiguous/unknown schemas are skipped.
func schemaNxTreeFields(src []byte, relFile string, knownFiles map[string]bool, readSrc func(string) []byte, sideReads map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if readSrc == nil {
		return out
	}
	mask := tsCommentStringMask(src)
	fileDir := factpath.Dir(relFile)
	for _, loc := range relativeNamedImport.FindAllSubmatchIndex(src, -1) {
		if mask[loc[0]] {
			continue
		}
		inner := string(src[loc[2]:loc[3]])
		spec := string(src[loc[4]:loc[5]])
		locals := collectNamedImportLocals(inner)
		if len(locals) == 0 {
			continue
		}
		resolved, external := resolveImportPath(spec, fileDir, nil)
		if external {
			continue
		}
		leaf, _, ok := resolveModuleFile(resolved, knownFiles)
		if !ok || leaf == "" || filepath.ToSlash(leaf) == filepath.ToSlash(relFile) {
			continue
		}
		schemaSrc := readSrc(leaf)
		if len(schemaSrc) == 0 {
			continue
		}
		if sideReads != nil {
			f := filepath.ToSlash(leaf)
			if f != "" && f != filepath.ToSlash(relFile) {
				sideReads[f] = true
			}
		}
		schemaTrees := importedNxTreeNames(schemaSrc)
		if len(schemaTrees) == 0 {
			continue
		}
		fieldsByType := explicitNxTreeFields(schemaSrc, schemaTrees)
		for local, orig := range locals {
			if fields := fieldsByType[orig]; len(fields) > 0 {
				out[local] = fields
			}
		}
	}
	return out
}

func explicitNxTreeFields(src []byte, treeTypes map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	mask := tsCommentStringMask(src)
	collect := func(re *regexp.Regexp) {
		for _, loc := range re.FindAllSubmatchIndex(src, -1) {
			if mask[loc[0]] {
				continue
			}
			name := string(src[loc[2]:loc[3]])
			open := loc[1] - 1
			if open < 0 || src[open] != '{' {
				continue
			}
			// Skip `interface X extends Y {` / generic forms: the name must
			// sit immediately before `{` aside from whitespace.
			before := strings.TrimSpace(string(src[loc[3]:open]))
			if before != "" {
				continue
			}
			end := matchBrace(src, mask, open)
			if end < 0 {
				continue
			}
			direct := directSchemaMembers(src[open+1:end], mask[open+1:end])
			for field, typ := range direct {
				if !treeTypes[typ] {
					continue
				}
				if out[name] == nil {
					out[name] = map[string]bool{}
				}
				out[name][field] = true
			}
		}
	}
	collect(interfaceOpen)
	collect(typeObjectOpen)
	return out
}

// directSchemaMembers returns identifier fields declared on the interface
// object itself. Nested object literals and comment/string bodies are ignored.
func directSchemaMembers(body []byte, mask []bool) map[string]string {
	out := map[string]string{}
	depth := 0
	i := 0
	for i < len(body) {
		if mask[i] {
			i++
			continue
		}
		switch body[i] {
		case '{':
			depth++
			i++
			continue
		case '}':
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if depth != 0 {
			i++
			continue
		}
		lineStart := i == 0
		if !lineStart {
			for j := i - 1; j >= 0; j-- {
				if mask[j] {
					continue
				}
				if body[j] == '\n' {
					lineStart = true
				}
				break
			}
		}
		if !lineStart {
			i++
			continue
		}
		fm := schemaField.FindSubmatchIndex(body[i:])
		if fm == nil || fm[0] != 0 {
			i++
			continue
		}
		field := string(body[i+fm[2] : i+fm[3]])
		typ := string(body[i+fm[4] : i+fm[5]])
		if !mask[i+fm[2]] {
			out[field] = typ
		}
		i += fm[1]
	}
	return out
}

type nxTypedBinding struct {
	name, typ  string
	start, end int
}

type nxTypeDecl struct {
	name       string
	start, end int
	nxImport   bool
}

func nxTreeScopes(src []byte, relFile string, knownFiles map[string]bool, readSrc func(string) []byte, sideReads map[string]bool) []nxTreeScope {
	treeTypes := importedNxTreeNames(src)
	schemaFields := schemaNxTreeFields(src, relFile, knownFiles, readSrc, sideReads)
	if len(treeTypes) == 0 && len(schemaFields) == 0 {
		return nil
	}
	kinds, root, closeParse := parseNxTSRoot(src, relFile)
	if closeParse != nil {
		defer closeParse()
	}
	if root == nil {
		return nil
	}

	var scopes []nxTreeScope
	add := func(name string, start, end int, isTree bool) {
		if name == "" || start < 0 || end <= start {
			return
		}
		scopes = append(scopes, nxTreeScope{name: name, start: start, end: end, isTree: isTree})
	}

	typeDecls := make([]nxTypeDecl, 0, len(treeTypes)+4)
	for name := range treeTypes {
		typeDecls = append(typeDecls, nxTypeDecl{name: name, start: 0, end: len(src), nxImport: true})
	}
	var collectTypes func(n *sitter.Node)
	collectTypes = func(n *sitter.Node) {
		if n == nil {
			return
		}
		kind := kindOf(kinds, n)
		if kind == "type_alias_declaration" || kind == "interface_declaration" {
			if nameN := n.ChildByFieldName("name"); nameN != nil {
				name := nodeText(nameN, src)
				_, blockEnd := nxEnclosingValueScope(kinds, n)
				typeDecls = append(typeDecls, nxTypeDecl{
					name: name, start: int(n.StartByte()), end: blockEnd, nxImport: false,
				})
			}
		}
		for i := range n.ChildCount() {
			collectTypes(n.Child(i))
		}
	}
	collectTypes(root)

	var typedBinds []nxTypedBinding
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		kind := kindOf(kinds, n)
		switch kind {
		case "required_parameter", "optional_parameter":
			pat := n.ChildByFieldName("pattern")
			if pat == nil {
				pat = n.ChildByFieldName("name")
			}
			if pat != nil && kindOf(kinds, pat) == "identifier" {
				ident := nodeText(pat, src)
				typ, okType := soleTypeIdentifier(kinds, nxTypeAnnotation(kinds, n), src)
				bodyStart, bodyEnd := nxParamBodyRange(kinds, n, src)
				isTree := false
				if okType {
					isTree = nxImportedTreeAt(typeDecls, treeTypes, typ, int(pat.StartByte()))
				}
				add(ident, bodyStart, bodyEnd, isTree)
				if !okType {
					typ = ""
				}
				typedBinds = append(typedBinds, nxTypedBinding{name: ident, typ: typ, start: bodyStart, end: bodyEnd})
			}
		case "variable_declarator":
			nameN := n.ChildByFieldName("name")
			if nameN == nil {
				break
			}
			nk := kindOf(kinds, nameN)
			declStart := int(n.StartByte())
			_, blockEnd := nxEnclosingValueScope(kinds, n)
			typ, okType := soleTypeIdentifier(kinds, nxTypeAnnotation(kinds, n), src)
			if nk == "identifier" {
				ident := nodeText(nameN, src)
				isTree := false
				if okType {
					isTree = nxImportedTreeAt(typeDecls, treeTypes, typ, declStart)
				}
				add(ident, declStart, blockEnd, isTree)
				if !okType {
					typ = ""
				}
				typedBinds = append(typedBinds, nxTypedBinding{name: ident, typ: typ, start: declStart, end: blockEnd})
			}
			if nk == "object_pattern" {
				srcName, srcOK := nxDestructureSourceIdent(kinds, n, src)
				nearest, ok := nearestTypedBinding(typedBinds, srcName, declStart)
				for _, b := range nxObjectPatternBindings(kinds, nameN, src) {
					proven := false
					if srcOK && ok {
						if treeFields := schemaFields[nearest.typ]; treeFields[b.orig] {
							proven = true
						}
					}
					add(b.local, declStart, blockEnd, proven)
				}
			}
		}
		if tsTypeLikeKind(kind) {
			return
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(root)
	return scopes
}

func parseNxTSRoot(src []byte, relFile string) (*tsutil.KindTable, *sitter.Node, func()) {
	if len(src) == 0 {
		return nil, nil, nil
	}
	isTSX := strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx")
	kinds := tsKindsFor(isTSX)
	lang := typescript.LanguageTypescript()
	if isTSX {
		lang = typescript.LanguageTSX()
	}
	parser := sitter.NewParser()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		parser.Close()
		return nil, nil, nil
	}
	tree := parseSourceForFile(parser, src, relFile)
	return kinds, tree.RootNode(), func() {
		tree.Close()
		parser.Close()
	}
}

func nxTypeAnnotation(kinds *tsutil.KindTable, n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if t := n.ChildByFieldName("type"); t != nil {
		return t
	}
	for i := range n.NamedChildCount() {
		ch := n.NamedChild(i)
		if kindOf(kinds, ch) == "type_annotation" {
			return ch
		}
	}
	return nil
}

func soleTypeIdentifier(kinds *tsutil.KindTable, n *sitter.Node, src []byte) (string, bool) {
	n = unwrapSoleType(kinds, n)
	if n == nil || kindOf(kinds, n) != "type_identifier" {
		return "", false
	}
	name := strings.TrimSpace(nodeText(n, src))
	if name == "" {
		return "", false
	}
	return name, true
}

func unwrapSoleType(kinds *tsutil.KindTable, n *sitter.Node) *sitter.Node {
	for n != nil {
		switch kindOf(kinds, n) {
		case "type_annotation", "parenthesized_type":
			if inner := n.ChildByFieldName("type"); inner != nil && inner != n {
				n = inner
				continue
			}
			var next *sitter.Node
			for i := range n.NamedChildCount() {
				ch := n.NamedChild(i)
				if ch == nil || !tsTypeLikeKind(kindOf(kinds, ch)) {
					continue
				}
				if next != nil {
					return n
				}
				next = ch
			}
			if next == nil || next == n {
				return n
			}
			n = next
		default:
			return n
		}
	}
	return n
}

func nxImportedTreeAt(decls []nxTypeDecl, imported map[string]bool, typ string, pos int) bool {
	if !imported[typ] {
		return false
	}
	d, ok := nearestTypeDecl(decls, typ, pos)
	if !ok {
		return false
	}
	return d.nxImport
}

func nearestTypeDecl(decls []nxTypeDecl, name string, pos int) (nxTypeDecl, bool) {
	best := -1
	span := int(^uint(0) >> 1)
	for i, d := range decls {
		if d.name != name || pos < d.start || pos > d.end {
			continue
		}
		w := d.end - d.start
		if w < span || (w == span && i > best) {
			span = w
			best = i
		}
	}
	if best < 0 {
		return nxTypeDecl{}, false
	}
	return decls[best], true
}

func nxParamBodyRange(kinds *tsutil.KindTable, param *sitter.Node, src []byte) (start, end int) {
	for p := param.Parent(); p != nil; p = p.Parent() {
		if !tsIsFunctionLike(kindOf(kinds, p)) {
			continue
		}
		if body := p.ChildByFieldName("body"); body != nil {
			return int(body.StartByte()), int(body.EndByte())
		}
		break
	}
	return int(param.StartByte()), len(src)
}

func nxEnclosingValueScope(kinds *tsutil.KindTable, n *sitter.Node) (start, end int) {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch kindOf(kinds, p) {
		case "statement_block", "program", "class_body":
			return int(p.StartByte()), int(p.EndByte())
		}
	}
	return 0, int(n.EndByte())
}

func nxDestructureSourceIdent(kinds *tsutil.KindTable, decl *sitter.Node, src []byte) (string, bool) {
	val := decl.ChildByFieldName("value")
	if val == nil {
		return "", false
	}
	val = unwrapTSSyntaxExpr(kinds, val)
	if val == nil || kindOf(kinds, val) != "identifier" {
		return "", false
	}
	name := strings.TrimSpace(nodeText(val, src))
	return name, name != ""
}

type nxPatternBind struct {
	orig, local string
}

func nxObjectPatternBindings(kinds *tsutil.KindTable, pat *sitter.Node, src []byte) []nxPatternBind {
	var out []nxPatternBind
	if pat == nil {
		return out
	}
	for i := range pat.NamedChildCount() {
		ch := pat.NamedChild(i)
		switch kindOf(kinds, ch) {
		case "shorthand_property_identifier_pattern", "shorthand_property_identifier":
			name := strings.TrimSpace(nodeText(ch, src))
			if name != "" {
				out = append(out, nxPatternBind{orig: name, local: name})
			}
		case "pair_pattern", "object_assignment_pattern":
			key := ch.ChildByFieldName("key")
			if key == nil && ch.NamedChildCount() > 0 {
				key = ch.NamedChild(0)
			}
			val := ch.ChildByFieldName("value")
			if val == nil {
				val = ch.ChildByFieldName("pattern")
			}
			if val == nil && ch.NamedChildCount() > 1 {
				val = ch.NamedChild(1)
			}
			orig := strings.TrimSpace(nodeText(key, src))
			local := orig
			if val != nil && kindOf(kinds, val) == "identifier" {
				local = strings.TrimSpace(nodeText(val, src))
			}
			if orig != "" && local != "" {
				out = append(out, nxPatternBind{orig: orig, local: local})
			}
		}
	}
	return out
}

func nearestTypedBinding(binds []nxTypedBinding, name string, pos int) (nxTypedBinding, bool) {
	best := -1
	span := int(^uint(0) >> 1)
	for i, b := range binds {
		if b.name != name || pos < b.start || pos > b.end {
			continue
		}
		w := b.end - b.start
		if w < span || (w == span && i > best) {
			span = w
			best = i
		}
	}
	if best < 0 {
		return nxTypedBinding{}, false
	}
	return binds[best], true
}

func isNxTreeReceiverAt(scopes []nxTreeScope, name string, pos int) bool {
	if name == "" || len(scopes) == 0 {
		return false
	}
	best := -1
	isTree := false
	span := int(^uint(0) >> 1)
	for i, sc := range scopes {
		if sc.name != name || pos < sc.start || pos > sc.end {
			continue
		}
		w := sc.end - sc.start
		if w < span || (w == span && i > best) {
			span = w
			best = i
			isTree = sc.isTree
		}
	}
	return isTree
}
