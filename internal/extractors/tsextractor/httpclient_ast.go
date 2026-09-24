package tsextractor

import (
	"path/filepath"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type lexicalFetchCall struct {
	raw      string
	identArg bool
	nameOff  int
	argEnd   int
}

type functionURLProp struct {
	raw string
	off int
	end int
}

type fetchEnv struct {
	parent *fetchEnv
	binds  map[string]bool
}

func (e *fetchEnv) lookup(name string) (proven bool, bound bool) {
	for e != nil {
		if v, ok := e.binds[name]; ok {
			return v, true
		}
		e = e.parent
	}
	return false, false
}

func (e *fetchEnv) set(name string, proven bool) {
	if e == nil {
		return
	}
	for env := e; env != nil; env = env.parent {
		if _, ok := env.binds[name]; ok {
			env.binds[name] = proven
			return
		}
	}
	if e.binds == nil {
		e.binds = map[string]bool{}
	}
	e.binds[name] = proven
}

func (e *fetchEnv) declare(name string, proven bool) {
	if e == nil {
		return
	}
	if e.binds == nil {
		e.binds = map[string]bool{}
	}
	e.binds[name] = proven
}

func childFetchEnv(parent *fetchEnv) *fetchEnv {
	return &fetchEnv{parent: parent, binds: map[string]bool{}}
}

func parseHTTPClientTree(src []byte, relFile string) (*sitter.Parser, *sitter.Tree, *tsutil.KindTable) {
	ext := strings.ToLower(filepath.Ext(relFile))
	isTSX := ext == ".tsx" || ext == ".jsx"
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
	return parser, tree, tsKindsFor(isTSX)
}

func lexicalFetchCalls(src []byte, relFile string) []lexicalFetchCall {
	calls, _ := lexicalFetchAnalysis(src, relFile)
	return calls
}

func lexicalFetchAnalysis(src []byte, relFile string) ([]lexicalFetchCall, map[int]bool) {
	parser, tree, kinds := parseHTTPClientTree(src, relFile)
	if parser == nil || tree == nil {
		return nil, nil
	}
	defer parser.Close()
	defer tree.Close()
	var out []lexicalFetchCall
	skip := map[int]bool{}
	root := tree.RootNode()
	env := childFetchEnv(nil)
	hoistFetchScope(kinds, root, src, env)
	walkFetchAST(kinds, root, src, env, &out, skip)
	return out, skip
}

func functionValuedURLProperties(src []byte, relFile string) []functionURLProp {
	parser, tree, kinds := parseHTTPClientTree(src, relFile)
	if parser == nil || tree == nil {
		return nil
	}
	defer parser.Close()
	defer tree.Close()
	var out []functionURLProp
	walkURLFns(kinds, tree.RootNode(), src, &out)
	return out
}

func walkFetchAST(kinds *tsutil.KindTable, n *sitter.Node, src []byte, env *fetchEnv, out *[]lexicalFetchCall, skip map[int]bool) {
	if n == nil {
		return
	}
	kind := kindOf(kinds, n)
	if tsTypeLikeKind(kind) {
		return
	}
	switch kind {
	case "function_declaration", "generator_function_declaration", "function_expression",
		"generator_function", "arrow_function", "method_definition":
		child := childFetchEnv(env)
		if params := n.ChildByFieldName("parameters"); params != nil {
			bindFetchParams(kinds, params, src, child)
		} else if params := findChildByKind(kinds, n, "formal_parameters"); params != nil {
			bindFetchParams(kinds, params, src, child)
		}
		if body := n.ChildByFieldName("body"); body != nil {
			hoistFetchScope(kinds, body, src, child)
			walkFetchAST(kinds, body, src, child, out, skip)
		} else {
			for i := range n.NamedChildCount() {
				c := n.NamedChild(i)
				if kindOf(kinds, c) == "formal_parameters" {
					continue
				}
				walkFetchAST(kinds, c, src, child, out, skip)
			}
		}
		return
	case "class_declaration", "abstract_class_declaration":
		if name := n.ChildByFieldName("name"); name != nil && kindOf(kinds, name) == "identifier" {
			env.declare(nodeText(name, src), false)
		}
		child := childFetchEnv(env)
		if body := n.ChildByFieldName("body"); body != nil {
			walkFetchAST(kinds, body, src, child, out, skip)
		}
		return
	case "statement_block", "for_statement", "for_in_statement", "for_of_statement", "catch_clause":
		child := childFetchEnv(env)
		hoistFetchScope(kinds, n, src, child)
		if kind == "catch_clause" {
			if param := n.ChildByFieldName("parameter"); param != nil {
				for _, name := range tsPatternBindingNames(kinds, param, src) {
					child.declare(name, false)
				}
			}
		}
		for i := range n.NamedChildCount() {
			walkFetchAST(kinds, n.NamedChild(i), src, child, out, skip)
		}
		return
	case "lexical_declaration", "variable_declaration":
		for i := range n.NamedChildCount() {
			d := n.NamedChild(i)
			if kindOf(kinds, d) != "variable_declarator" {
				walkFetchAST(kinds, d, src, env, out, skip)
				continue
			}
			nameN := d.ChildByFieldName("name")
			val := d.ChildByFieldName("value")
			if val != nil {
				walkFetchAST(kinds, val, src, env, out, skip)
			}
			if nameN != nil && kindOf(kinds, nameN) == "identifier" {
				env.declare(nodeText(nameN, src), exprIsProvenFetch(kinds, val, src, env))
			} else if nameN != nil {
				for _, name := range tsPatternBindingNames(kinds, nameN, src) {
					env.declare(name, false)
				}
			}
		}
		return
	case "assignment_expression":
		left := n.ChildByFieldName("left")
		right := n.ChildByFieldName("right")
		if right != nil {
			walkFetchAST(kinds, right, src, env, out, skip)
		}
		if left != nil && kindOf(kinds, left) == "identifier" {
			env.set(nodeText(left, src), exprIsProvenFetch(kinds, right, src, env))
		} else if left != nil {
			walkFetchAST(kinds, left, src, env, out, skip)
		}
		return
	case "call_expression":
		fn := n.ChildByFieldName("function")
		args := n.ChildByFieldName("arguments")
		if fn != nil && kindOf(kinds, fn) == "identifier" && args != nil {
			name := nodeText(fn, src)
			off := int(fn.StartByte())
			if identifierIsFetchCallee(env, name) {
				if call, ok := fetchCallFromArgs(kinds, args, src, off); ok {
					*out = append(*out, call)
				}
			} else if skip != nil && (name == "fetch" || name == "makeRequest") {
				skip[off] = true
			}
		}
	}
	for i := range n.NamedChildCount() {
		walkFetchAST(kinds, n.NamedChild(i), src, env, out, skip)
	}
}

func hoistFetchScope(kinds *tsutil.KindTable, n *sitter.Node, src []byte, env *fetchEnv) {
	if n == nil || env == nil {
		return
	}
	for i := range n.NamedChildCount() {
		stmt := n.NamedChild(i)
		hoistFetchStmt(kinds, stmt, src, env)
	}
}

func hoistFetchStmt(kinds *tsutil.KindTable, stmt *sitter.Node, src []byte, env *fetchEnv) {
	if stmt == nil {
		return
	}
	switch kindOf(kinds, stmt) {
	case "import_statement":
		bindFetchImport(kinds, stmt, src, env)
	case "function_declaration", "generator_function_declaration":
		if name := stmt.ChildByFieldName("name"); name != nil && kindOf(kinds, name) == "identifier" {
			env.declare(nodeText(name, src), false)
		}
	case "class_declaration", "abstract_class_declaration":
		if name := stmt.ChildByFieldName("name"); name != nil && kindOf(kinds, name) == "identifier" {
			env.declare(nodeText(name, src), false)
		}
	case "export_statement":
		for i := range stmt.NamedChildCount() {
			hoistFetchStmt(kinds, stmt.NamedChild(i), src, env)
		}
	}
}

func bindFetchImport(kinds *tsutil.KindTable, stmt *sitter.Node, src []byte, env *fetchEnv) {
	clause := findChildByKind(kinds, stmt, "import_clause")
	if clause == nil {
		return
	}
	for i := range clause.NamedChildCount() {
		child := clause.NamedChild(i)
		switch kindOf(kinds, child) {
		case "identifier":
			env.declare(nodeText(child, src), false)
		case "namespace_import":
			if id := findChildByKind(kinds, child, "identifier"); id != nil {
				env.declare(nodeText(id, src), false)
			}
		case "named_imports":
			for j := range child.NamedChildCount() {
				spec := child.NamedChild(j)
				if kindOf(kinds, spec) != "import_specifier" {
					continue
				}
				local := spec.ChildByFieldName("alias")
				if local == nil {
					local = spec.ChildByFieldName("name")
				}
				if local != nil && kindOf(kinds, local) == "identifier" {
					env.declare(nodeText(local, src), false)
				}
			}
		}
	}
}

func identifierIsFetchCallee(env *fetchEnv, name string) bool {
	if name == "makeRequest" {
		proven, bound := env.lookup(name)
		if !bound {
			return true
		}
		return proven
	}
	proven, bound := env.lookup(name)
	if bound {
		return proven
	}
	return name == "fetch"
}

func bindFetchParams(kinds *tsutil.KindTable, params *sitter.Node, src []byte, env *fetchEnv) {
	if params == nil {
		return
	}
	for i := range params.NamedChildCount() {
		p := params.NamedChild(i)
		pk := kindOf(kinds, p)
		if pk != "required_parameter" && pk != "optional_parameter" && pk != "rest_parameter" {
			continue
		}
		pat := p.ChildByFieldName("pattern")
		if pat == nil {
			pat = findChildByKind(kinds, p, "identifier")
		}
		val := p.ChildByFieldName("value")
		if pat != nil && kindOf(kinds, pat) == "identifier" {
			env.declare(nodeText(pat, src), exprIsProvenFetch(kinds, val, src, env))
			continue
		}
		if pat != nil {
			for _, name := range tsPatternBindingNames(kinds, pat, src) {
				env.declare(name, false)
			}
		}
	}
}

func exprIsProvenFetch(kinds *tsutil.KindTable, n *sitter.Node, src []byte, env *fetchEnv) bool {
	n = unwrapTSSyntaxExpr(kinds, n)
	if n == nil {
		return false
	}
	switch kindOf(kinds, n) {
	case "identifier":
		name := nodeText(n, src)
		proven, bound := env.lookup(name)
		if bound {
			return proven
		}
		return name == "fetch"
	case "member_expression":
		obj := unwrapTSSyntaxExpr(kinds, n.ChildByFieldName("object"))
		prop := n.ChildByFieldName("property")
		if obj == nil || prop == nil {
			return false
		}
		if kindOf(kinds, obj) != "identifier" || nodeText(obj, src) != "globalThis" {
			return false
		}
		if _, bound := env.lookup("globalThis"); bound {
			return false
		}
		propName := nodeText(prop, src)
		return propName == "fetch"
	case "binary_expression":
		op := n.ChildByFieldName("operator")
		if op == nil {
			return false
		}
		switch nodeText(op, src) {
		case "??", "||":
			return exprIsProvenFetch(kinds, n.ChildByFieldName("right"), src, env)
		}
	}
	return false
}

func fetchCallFromArgs(kinds *tsutil.KindTable, args *sitter.Node, src []byte, nameOff int) (lexicalFetchCall, bool) {
	var first *sitter.Node
	for i := range args.NamedChildCount() {
		c := args.NamedChild(i)
		if c == nil {
			continue
		}
		first = c
		break
	}
	if first == nil {
		return lexicalFetchCall{}, false
	}
	first = unwrapTSSyntaxExpr(kinds, first)
	end := int(first.EndByte())
	switch kindOf(kinds, first) {
	case "string", "template_string":
		raw := unquoteTSString(nodeText(first, src))
		return lexicalFetchCall{raw: raw, nameOff: nameOff, argEnd: end}, true
	case "identifier":
		return lexicalFetchCall{raw: nodeText(first, src), identArg: true, nameOff: nameOff, argEnd: end}, true
	}
	return lexicalFetchCall{}, false
}

func walkURLFns(kinds *tsutil.KindTable, n *sitter.Node, src []byte, out *[]functionURLProp) {
	if n == nil {
		return
	}
	kind := kindOf(kinds, n)
	if tsTypeLikeKind(kind) {
		return
	}
	if kind == "pair" {
		key := n.ChildByFieldName("key")
		val := n.ChildByFieldName("value")
		if key != nil && val != nil && strings.Trim(nodeText(key, src), `"'`) == "url" {
			if raw, ok := functionURLLiteral(kinds, val, src); ok {
				*out = append(*out, functionURLProp{raw: raw, off: int(n.StartByte()), end: int(n.EndByte())})
			}
		}
	}
	for i := range n.NamedChildCount() {
		walkURLFns(kinds, n.NamedChild(i), src, out)
	}
}

func functionURLLiteral(kinds *tsutil.KindTable, n *sitter.Node, src []byte) (string, bool) {
	n = unwrapTSSyntaxExpr(kinds, n)
	if n == nil {
		return "", false
	}
	switch kindOf(kinds, n) {
	case "arrow_function", "function_expression", "function_declaration":
		body := n.ChildByFieldName("body")
		if body == nil {
			return "", false
		}
		return returnedURLLiteral(kinds, body, src)
	}
	return "", false
}

func returnedURLLiteral(kinds *tsutil.KindTable, body *sitter.Node, src []byte) (string, bool) {
	body = unwrapTSSyntaxExpr(kinds, body)
	if body == nil {
		return "", false
	}
	if kindOf(kinds, body) != "statement_block" {
		return staticURLExpr(kinds, body, src)
	}
	var ret *sitter.Node
	count := 0
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		k := kindOf(kinds, n)
		if k == "arrow_function" || k == "function_expression" || k == "function_declaration" || k == "method_definition" {
			return
		}
		if k == "return_statement" {
			count++
			ret = n.NamedChild(0)
			return
		}
		for i := range n.NamedChildCount() {
			walk(n.NamedChild(i))
		}
	}
	walk(body)
	if count != 1 || ret == nil {
		return "", false
	}
	return staticURLExpr(kinds, ret, src)
}

func staticURLExpr(kinds *tsutil.KindTable, n *sitter.Node, src []byte) (string, bool) {
	n = unwrapTSSyntaxExpr(kinds, n)
	if n == nil {
		return "", false
	}
	switch kindOf(kinds, n) {
	case "string", "template_string":
		return unquoteTSString(nodeText(n, src)), true
	case "binary_expression":
		op := n.ChildByFieldName("operator")
		if op == nil || nodeText(op, src) != "+" {
			return "", false
		}
		left, okL := staticURLExpr(kinds, n.ChildByFieldName("left"), src)
		right, okR := staticURLExpr(kinds, n.ChildByFieldName("right"), src)
		if !okL || !okR {
			return "", false
		}
		return left + right, true
	case "call_expression":
		fn := n.ChildByFieldName("function")
		args := n.ChildByFieldName("arguments")
		if fn == nil || args == nil || kindOf(kinds, fn) != "member_expression" {
			return "", false
		}
		prop := fn.ChildByFieldName("property")
		if prop == nil || nodeText(prop, src) != "replace" {
			return "", false
		}
		obj, ok := staticURLExpr(kinds, fn.ChildByFieldName("object"), src)
		if !ok {
			return "", false
		}
		var a, b string
		var narg int
		for i := range args.NamedChildCount() {
			c := args.NamedChild(i)
			if c == nil {
				continue
			}
			lit, ok := staticURLExpr(kinds, c, src)
			if !ok {
				return "", false
			}
			narg++
			if narg == 1 {
				a = lit
			} else if narg == 2 {
				b = lit
			} else {
				return "", false
			}
		}
		if narg != 2 {
			return "", false
		}
		return strings.Replace(obj, a, b, 1), true
	}
	return "", false
}
