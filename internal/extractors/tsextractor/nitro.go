package tsextractor

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	nitroMethodSuffixes = map[string]string{
		"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH",
		"delete": "DELETE", "options": "OPTIONS", "head": "HEAD",
		"all": "*",
	}
	handlerResolvePath = regexp.MustCompile(`(?:^|[,{])\s*(?:handler|file)\s*:\s*(?:[A-Za-z_$][\w$]*\s*\.\s*resolve\s*\(\s*)?["'](\./[^"']+)["']`)
)

func extractNitroFacts(kinds *tsutil.KindTable, root *sitter.Node, src []byte, relFile string, isNuxt bool, aliases map[string]tsAlias, knownFiles map[string]bool) []facts.Fact {
	var out []facts.Fact
	if isNuxt {
		if route := detectNitroFileRoute(relFile); route != nil {
			out = append(out, *route)
		}
	}
	if bytes.Contains(src, []byte("@nuxt/kit")) {
		out = append(out, extractAddServerHandlerFacts(kinds, root, src, relFile, aliases, knownFiles)...)
	}
	return out
}

func detectNitroFileRoute(relFile string) *facts.Fact {
	ext := filepath.Ext(relFile)
	switch strings.ToLower(ext) {
	case ".ts", ".js", ".mjs", ".cjs", ".mts", ".cts":
	default:
		return nil
	}
	parts := strings.Split(filepath.ToSlash(relFile), "/")
	kind := ""
	idx := -1
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "server" {
			continue
		}
		if parts[i+1] == "middleware" {
			return nil
		}
		if parts[i+1] == "api" || parts[i+1] == "routes" {
			kind = parts[i+1]
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	remaining := parts[idx+2:]
	if len(remaining) == 0 {
		return nil
	}
	fileName := remaining[len(remaining)-1]
	baseName := strings.TrimSuffix(fileName, ext)
	method := "*"
	if dot := strings.LastIndexByte(baseName, '.'); dot >= 0 {
		if m, ok := nitroMethodSuffixes[strings.ToLower(baseName[dot+1:])]; ok {
			method = m
			baseName = baseName[:dot]
		}
	}
	routeParts := make([]string, 0, len(remaining))
	if kind == "api" {
		routeParts = append(routeParts, "api")
	}
	for j, rp := range remaining {
		if j == len(remaining)-1 {
			if baseName != "index" {
				routeParts = append(routeParts, baseName)
			}
			continue
		}
		routeParts = append(routeParts, rp)
	}
	routePath := "/" + strings.Join(routeParts, "/")
	if routePath == "/" {
		routePath = "/"
	}
	return &facts.Fact{
		Kind: facts.KindRoute,
		Name: routePath,
		File: relFile,
		Line: 1,
		Props: map[string]any{
			facts.PropRole: facts.RoleServer,
			"method":       method,
			"language":     "typescript",
			"framework":    "nitro",
			"router":       "server",
			"type":         kind,
		},
		Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: factpath.Dir(relFile)}},
	}
}

func extractAddServerHandlerFacts(kinds *tsutil.KindTable, root *sitter.Node, src []byte, relFile string, aliases map[string]tsAlias, knownFiles map[string]bool) []facts.Fact {
	if !bytes.Contains(src, []byte("addServerHandler")) {
		return nil
	}
	locals := nuxtKitAddServerHandlerLocals(kinds, root, src)
	if len(locals) == 0 {
		return nil
	}
	type decl struct {
		path, handler, method string
		line                  int
	}
	var decls []decl
	addObj := func(open int) {
		end, ok := matchObjectLiteral(src, open)
		if !ok {
			return
		}
		obj := src[open : end+1]
		raw := objectLiteralStringField(obj, "route")
		path, ok := cleanServerPath(raw)
		if !ok {
			return
		}
		handler := ""
		if hm := handlerResolvePath.FindSubmatch(obj); hm != nil {
			spec := string(hm[1])
			resolved, ext := resolveImportPath(spec, factpath.Dir(relFile), aliases)
			if !ext {
				if file, _, found := resolveModuleFile(resolved, knownFiles); found {
					handler = file
				} else if resolved != "" {
					handler = filepath.ToSlash(resolved)
				}
			}
		}
		method := "*"
		if methods := objectLiteralMethods(obj); len(methods) > 0 {
			method = joinRouteMethods(methods)
		}
		decls = append(decls, decl{
			path:    path,
			handler: handler,
			line:    1 + bytes.Count(src[:open], []byte("\n")),
			method:  method,
		})
	}
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if kindOf(kinds, n) == "call_expression" {
			fn := n.ChildByFieldName("function")
			if fn != nil && kindOf(kinds, fn) == "identifier" {
				name := nodeText(fn, src)
				if locals[name] && !graphqlImportedNameShadowed(kinds, fn, src, name) {
					args := n.ChildByFieldName("arguments")
					if args != nil {
						for i := range args.NamedChildCount() {
							arg := args.NamedChild(i)
							if arg != nil && kindOf(kinds, arg) == "object" {
								addObj(int(arg.StartByte()))
								break
							}
						}
					}
				}
			}
		}
		for i := range n.NamedChildCount() {
			walk(n.NamedChild(i))
		}
	}
	walk(root)
	if len(decls) == 0 {
		return nil
	}
	byPath := map[string][]decl{}
	var order []string
	for _, d := range decls {
		if _, ok := byPath[d.path]; !ok {
			order = append(order, d.path)
		}
		byPath[d.path] = append(byPath[d.path], d)
	}
	dir := factpath.Dir(relFile)
	var out []facts.Fact
	for _, path := range order {
		group := byPath[path]
		handlers := map[string]bool{}
		methods := map[string]bool{}
		line := group[0].line
		for _, d := range group {
			if d.handler != "" {
				handlers[d.handler] = true
			}
			methods[d.method] = true
			if d.line < line {
				line = d.line
			}
		}
		method := "*"
		if len(methods) == 1 {
			for m := range methods {
				method = m
			}
		}
		f := facts.Fact{
			Kind: facts.KindRoute,
			Name: path,
			File: relFile,
			Line: line,
			Props: map[string]any{
				facts.PropRole: facts.RoleServer,
				"method":       method,
				"language":     "typescript",
				"framework":    "nitro",
			},
			Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: dir}},
		}
		if len(handlers) == 1 {
			for h := range handlers {
				f.Props["handler"] = h
			}
		}
		out = append(out, f)
	}
	return out
}

func nuxtKitAddServerHandlerLocals(kinds *tsutil.KindTable, root *sitter.Node, src []byte) map[string]bool {
	out := map[string]bool{}
	if kinds == nil || root == nil {
		return out
	}
	for i := range root.ChildCount() {
		stmt := root.Child(i)
		if kindOf(kinds, stmt) != "import_statement" {
			continue
		}
		source := findChildByKind(kinds, stmt, "string")
		if source == nil {
			continue
		}
		pkg := strings.Trim(nodeText(source, src), `"'`)
		if pkg != "@nuxt/kit" {
			continue
		}
		var walkClause func(*sitter.Node)
		walkClause = func(n *sitter.Node) {
			if n == nil {
				return
			}
			switch kindOf(kinds, n) {
			case "import_specifier":
				imported := n.ChildByFieldName("name")
				local := n.ChildByFieldName("alias")
				if local == nil {
					local = imported
				}
				if imported != nil && nodeText(imported, src) == "addServerHandler" && local != nil {
					if name := nodeText(local, src); name != "" {
						out[name] = true
					}
				}
			case "namespace_import":
				// kit.addServerHandler is not a bare identifier call.
			default:
				for j := range n.NamedChildCount() {
					walkClause(n.NamedChild(j))
				}
			}
		}
		walkClause(stmt)
	}
	return out
}
