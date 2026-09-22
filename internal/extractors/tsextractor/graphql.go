package tsextractor

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/gqlscan"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// GraphQL client operations — the client half of the GraphQL seam.
//
// A gql-tagged template literal or a standalone .graphql operation document
// names its operation kind and root fields literally; each root field becomes a
// client-role route fact (`Query.pageViews`), the joinable name the graphql
// cross-repo signal matches against a server's root-field facts. Type
// definition blocks (`type Query { … }`) are schema COPIES on the client side
// (codegen inputs) and deliberately emit nothing — the operations are the
// client truth.
//
// The operation grammar itself lives in internal/gqlscan, shared with the Ruby
// client scanner so both languages name the same document identically.

func isGraphQLDocFile(path string) bool {
	return strings.HasSuffix(path, ".graphql") || strings.HasSuffix(path, ".gql")
}

// hasGraphQLDocs probes the engine's existing file list for .graphql/.gql
// operation documents. A Swift or
// Kotlin repository carries its Apollo operation documents with no
// package.json anywhere (an iOS app being the measured case: 41 documents,
// zero TypeScript), so doc presence must activate the extractor on its own —
// the rest of the TypeScript machinery no-ops with no TS files to read, and
// schema COPIES stay inert because type-definition blocks emit nothing.
// The engine has already applied ignored-directory policy, so walking the
// repository again here only repeated work and could disagree with that list.
func hasGraphQLDocs(files []string) bool {
	for _, path := range files {
		if isGraphQLDocFile(strings.ToLower(path)) && !hasGraphQLBuildSegment(path) {
			return true
		}
	}
	return false
}

func hasGraphQLBuildSegment(path string) bool {
	p := filepath.ToSlash(path)
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			if tsSkipDirs[p[start:i]] {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// extractGraphQLClientOps scans text (a gql template body or a .graphql file)
// for operations and returns one client route per root field.
func extractGraphQLClientOps(text, relFile, source string) []facts.Fact {
	var out []facts.Fact
	seen := map[string]bool{}
	heads := gqlscan.OperationHeads(text)
	for _, m := range heads {
		kind := text[m[2]:m[3]]
		kindName := strings.ToUpper(kind[:1]) + kind[1:]
		for _, field := range gqlscan.RootFields(text[m[1]:]) {
			full := kindName + "." + field
			if seen[full] {
				continue
			}
			seen[full] = true
			out = append(out, facts.Fact{
				Kind: facts.KindRoute,
				Name: full,
				File: relFile,
				Line: 1 + strings.Count(text[:m[0]], "\n"),
				Props: map[string]any{
					"language":          "typescript",
					"framework":         "graphql",
					facts.PropRole:      facts.RoleClient,
					facts.PropRouteType: facts.RouteTypeGraphQL,
					facts.PropSource:    source,
				},
			})
		}
	}
	// GraphQL's query shorthand omits the `query` keyword and operation name.
	// It is unambiguous inside an operation-bearing tag/string/document.
	if len(heads) == 0 {
		start := 0
		for start < len(text) && strings.ContainsRune(" \t\r\n,", rune(text[start])) {
			start++
		}
		if start < len(text) && text[start] == '{' {
			for _, field := range gqlscan.RootFields(text[start+1:]) {
				full := "Query." + field
				if !seen[full] {
					seen[full] = true
					out = append(out, facts.Fact{Kind: facts.KindRoute, Name: full, File: relFile, Line: 1 + strings.Count(text[:start], "\n"), Props: map[string]any{
						"language": "typescript", "framework": "graphql", facts.PropRole: facts.RoleClient,
						facts.PropRouteType: facts.RouteTypeGraphQL, facts.PropSource: source,
					}})
				}
			}
		}
	}
	return out
}

// GraphQL server SDL — the server half of the seam for schema-first Node
// GraphQL servers (Apollo, Yoga, Mercurius, express-graphql, graphql-http, and
// schemas assembled with GraphQL Tools or GraphQL.js).
//
// A schema-first server names its SDL as a plain string or a gql-tagged
// template (`type Query { … }`), which is a different
// grammar from an operation document: a field carries a return type after `:`
// or an argument list after `(`, where an operation's root field carries
// neither. Root fields on Query/Mutation/Subscription (including `extend type`,
// how a modular schema splits its root fields across files) become server-role
// route facts, joinable against the same client route facts gql tags and
// .graphql documents already produce. Resolver-to-field binding is a separate,
// later capability — this reads only the schema surface, the way the
// graphql-ruby field DSL does on the Ruby side.

// graphqlServerSignal is deliberately a set of high-confidence constructor,
// registration, and package-import signals. Package strings cover APIs whose
// local import may be aliased; call shapes cover GraphQL.js and GraphQL Tools.
// Apollo stops at the constructor name because TypeScript may insert a generic
// argument list (`new ApolloServer<MyContext>(...)`).
var graphqlServerPackages = map[string]bool{
	"@apollo/server": true, "apollo-server": true, "apollo-server-express": true,
	"apollo-server-fastify": true, "apollo-server-koa": true,
	"graphql-yoga": true, "mercurius": true, "express-graphql": true,
	"graphql-http": true, "@graphql-tools/schema": true,
	"@nestjs/graphql": true, "type-graphql": true, "nexus": true,
	"@pothos/core": true,
}

var graphqlRootByDeclaration = map[string]string{
	"queryType": "Query", "mutationType": "Mutation", "subscriptionType": "Subscription",
}

var graphqlRootByFieldCall = map[string]string{
	"queryField": "Query", "mutationField": "Mutation", "subscriptionField": "Subscription",
}

var graphqlRootNames = map[string]string{
	"Query": "Query", "Mutation": "Mutation", "Subscription": "Subscription",
}

type graphqlImportBindings struct {
	packages   map[string]bool
	named      map[string]map[string]string // package -> local -> exported
	namespaces map[string]map[string]bool   // package -> local namespace
	defaults   map[string]map[string]bool   // package -> local default binding
}

func collectGraphQLImportBindings(kinds *tsutil.KindTable, root *sitter.Node, src []byte) graphqlImportBindings {
	b := graphqlImportBindings{packages: map[string]bool{}, named: map[string]map[string]string{}, namespaces: map[string]map[string]bool{}, defaults: map[string]map[string]bool{}}
	for i := range root.ChildCount() {
		stmt := root.Child(i)
		if kindOf(kinds, stmt) != "import_statement" {
			continue
		}
		source := findChildByKind(kinds, stmt, "string")
		if source == nil {
			continue
		}
		pkg := graphqlPackageBase(strings.Trim(nodeText(source, src), `"'`))
		if !graphqlServerPackages[pkg] && pkg != "graphql-request" {
			continue
		}
		b.packages[pkg] = true
		if b.named[pkg] == nil {
			b.named[pkg] = map[string]string{}
		}
		if b.namespaces[pkg] == nil {
			b.namespaces[pkg] = map[string]bool{}
		}
		if b.defaults[pkg] == nil {
			b.defaults[pkg] = map[string]bool{}
		}
		clause := findChildByKind(kinds, stmt, "import_clause")
		if clause == nil {
			continue
		}
		for j := range clause.ChildCount() {
			child := clause.Child(j)
			switch kindOf(kinds, child) {
			case "identifier":
				b.defaults[pkg][nodeText(child, src)] = true
			case "namespace_import":
				if id := findChildByKind(kinds, child, "identifier"); id != nil {
					b.namespaces[pkg][nodeText(id, src)] = true
				}
			case "named_imports":
				for k := range child.ChildCount() {
					spec := child.Child(k)
					if kindOf(kinds, spec) != "import_specifier" {
						continue
					}
					name := spec.ChildByFieldName("name")
					if name == nil {
						continue
					}
					exported, local := nodeText(name, src), nodeText(name, src)
					if alias := spec.ChildByFieldName("alias"); alias != nil {
						local = nodeText(alias, src)
					}
					b.named[pkg][local] = exported
				}
			}
		}
	}
	return b
}

func graphqlPackageBase(path string) string {
	parts := strings.Split(path, "/")
	if strings.HasPrefix(path, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return path
}

func (b graphqlImportBindings) hasPackage(pkg string) bool { return b.packages[pkg] }

func (b graphqlImportBindings) exportedName(local string, packages ...string) string {
	for _, pkg := range packages {
		if exported := b.named[pkg][local]; exported != "" {
			return exported
		}
	}
	return ""
}

func (b graphqlImportBindings) callExport(kinds *tsutil.KindTable, fn *sitter.Node, src []byte, pkg string) string {
	if kindOf(kinds, fn) == "identifier" {
		local := nodeText(fn, src)
		if graphqlImportedNameShadowed(kinds, fn, src, local) {
			return ""
		}
		return b.named[pkg][local]
	}
	if kindOf(kinds, fn) == "member_expression" {
		obj, prop := fn.ChildByFieldName("object"), fn.ChildByFieldName("property")
		if obj == nil || prop == nil {
			return ""
		}
		ns := nodeText(obj, src)
		if graphqlImportedNameShadowed(kinds, obj, src, ns) {
			return ""
		}
		if b.namespaces[pkg][ns] {
			return nodeText(prop, src)
		}
	}
	return ""
}

// graphqlImportedNameShadowed reports a non-import lexical binding of name that
// is in scope at ident (parameter, function name, catch binding, or a prior
// local/block declaration). Module import bindings are the Yoga/Tools names
// themselves and do not count as shadows.
func graphqlImportedNameShadowed(kinds *tsutil.KindTable, ident *sitter.Node, src []byte, name string) bool {
	if ident == nil || name == "" {
		return false
	}
	at := ident.StartByte()
	for p := ident.Parent(); p != nil; p = p.Parent() {
		kind := kindOf(kinds, p)
		if tsIsFunctionLike(kind) && tsFunctionBindsName(kinds, p, src, name) {
			return true
		}
		if kind == "catch_clause" && tsPatternBindsName(kinds, p, src, name) {
			return true
		}
		if kind == "statement_block" || kind == "program" || kind == "class_body" {
			if tsScopeBindsNameBefore(kinds, p, src, name, at) {
				return true
			}
		}
	}
	return false
}

func tsFunctionBindsName(kinds *tsutil.KindTable, fn *sitter.Node, src []byte, name string) bool {
	if n := fn.ChildByFieldName("name"); n != nil && nodeText(n, src) == name {
		return true
	}
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		params = fn.ChildByFieldName("parameter")
	}
	return tsPatternBindsName(kinds, params, src, name)
}

func tsScopeBindsNameBefore(kinds *tsutil.KindTable, scope *sitter.Node, src []byte, name string, at uint) bool {
	for i := range scope.ChildCount() {
		stmt := unwrapTSExport(kinds, scope.Child(i))
		if stmt == nil {
			continue
		}
		kind := kindOf(kinds, stmt)
		if kind == "import_statement" {
			continue
		}
		if kind == "function_declaration" || kind == "generator_function_declaration" || kind == "class_declaration" {
			if n := stmt.ChildByFieldName("name"); n != nil && nodeText(n, src) == name {
				return true
			}
			continue
		}
		if stmt.EndByte() > at {
			continue
		}
		if kind == "lexical_declaration" || kind == "variable_declaration" {
			if tsPatternBindsName(kinds, stmt, src, name) {
				return true
			}
		}
	}
	return false
}

func unwrapTSExport(kinds *tsutil.KindTable, n *sitter.Node) *sitter.Node {
	if n != nil && kindOf(kinds, n) == "export_statement" {
		if d := n.ChildByFieldName("declaration"); d != nil {
			return d
		}
		for i := range n.ChildCount() {
			ch := n.Child(i)
			switch kindOf(kinds, ch) {
			case "function_declaration", "generator_function_declaration", "class_declaration", "lexical_declaration", "variable_declaration":
				return ch
			}
		}
	}
	return n
}

func tsPatternBindsName(kinds *tsutil.KindTable, n *sitter.Node, src []byte, name string) bool {
	if n == nil {
		return false
	}
	switch kindOf(kinds, n) {
	case "identifier", "shorthand_property_identifier_pattern":
		// Shorthand `{ createSchema }` is a leaf whose text is the bound name.
		return nodeText(n, src) == name
	case "pair_pattern":
		// `{ createSchema: unrelated }` binds `unrelated` only. The property key
		// is not a lexical binding even when it matches the imported name.
		if v := n.ChildByFieldName("value"); v != nil {
			return tsPatternBindsName(kinds, v, src, name)
		}
		if p := n.ChildByFieldName("pattern"); p != nil {
			return tsPatternBindsName(kinds, p, src, name)
		}
		return false
	case "assignment_pattern", "object_assignment_pattern":
		// Defaults are expressions, not declarations: `{ createSchema = fn }`
		// binds `createSchema`; identifiers inside the default do not.
		if p := n.ChildByFieldName("left"); p != nil {
			return tsPatternBindsName(kinds, p, src, name)
		}
		if p := n.ChildByFieldName("name"); p != nil {
			return tsPatternBindsName(kinds, p, src, name)
		}
		if n.ChildCount() > 0 {
			return tsPatternBindsName(kinds, n.Child(0), src, name)
		}
		return false
	case "variable_declarator", "required_parameter", "optional_parameter", "rest_parameter", "rest_pattern":
		if p := n.ChildByFieldName("name"); p != nil {
			return tsPatternBindsName(kinds, p, src, name)
		}
		if p := n.ChildByFieldName("pattern"); p != nil {
			return tsPatternBindsName(kinds, p, src, name)
		}
		for i := range n.ChildCount() {
			ch := n.Child(i)
			k := kindOf(kinds, ch)
			if k == "identifier" || k == "shorthand_property_identifier_pattern" || k == "object_pattern" || k == "array_pattern" || k == "rest_pattern" || k == "assignment_pattern" || k == "object_assignment_pattern" {
				if tsPatternBindsName(kinds, ch, src, name) {
					return true
				}
			}
		}
		return false
	case "object_pattern", "array_pattern", "formal_parameters", "lexical_declaration", "variable_declaration", "catch_clause":
		for i := range n.ChildCount() {
			if tsPatternBindsName(kinds, n.Child(i), src, name) {
				return true
			}
		}
	}
	return false
}

func graphqlCallTypeDefFields(kinds *tsutil.KindTable, cfg *sitter.Node, src []byte, root *sitter.Node) (map[string]bool, bool) {
	td := objectPropValue(kinds, cfg, src, "typeDefs")
	if td == nil {
		td = objectPropValue(kinds, cfg, src, "typeDefinitions")
	}
	if td == nil {
		return nil, false
	}
	out := map[string]bool{}
	if !collectTypeDefFields(kinds, td, src, root, out, 0) || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func collectTypeDefFields(kinds *tsutil.KindTable, n *sitter.Node, src []byte, root *sitter.Node, out map[string]bool, depth int) bool {
	if n == nil || depth > 8 {
		return false
	}
	switch kindOf(kinds, n) {
	case "template_string", "string":
		for _, f := range sdlRootFields(unquoteTSString(nodeText(n, src)), "", 1) {
			out[f.Name] = true
		}
		return true
	case "call_expression":
		args := n.ChildByFieldName("arguments")
		if args == nil {
			return false
		}
		ok := false
		for i := range args.ChildCount() {
			if collectTypeDefFields(kinds, args.Child(i), src, root, out, depth+1) {
				ok = true
			}
		}
		return ok
	case "array":
		ok := false
		for i := range n.ChildCount() {
			if collectTypeDefFields(kinds, n.Child(i), src, root, out, depth+1) {
				ok = true
			}
		}
		return ok
	case "identifier":
		if root == nil {
			return false
		}
		name := nodeText(n, src)
		if init := tsModuleBindingInit(kinds, root, src, name); init != nil {
			return collectTypeDefFields(kinds, init, src, root, out, depth+1)
		}
		return false
	}
	return false
}

func unquoteTSString(s string) string {
	if len(s) >= 2 {
		switch s[0] {
		case '`', '"', '\'':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}

func tsModuleBindingInit(kinds *tsutil.KindTable, root *sitter.Node, src []byte, name string) *sitter.Node {
	for i := range root.ChildCount() {
		stmt := unwrapTSExport(kinds, root.Child(i))
		if stmt == nil {
			continue
		}
		if kindOf(kinds, stmt) != "lexical_declaration" && kindOf(kinds, stmt) != "variable_declaration" {
			continue
		}
		for j := range stmt.ChildCount() {
			d := stmt.Child(j)
			if kindOf(kinds, d) != "variable_declarator" {
				continue
			}
			nm := d.ChildByFieldName("name")
			if nm != nil && kindOf(kinds, nm) == "identifier" && nodeText(nm, src) == name {
				return d.ChildByFieldName("value")
			}
		}
	}
	return nil
}

func collectPothosBuilders(kinds *tsutil.KindTable, root *sitter.Node, src []byte, bindings graphqlImportBindings) map[string]bool {
	out := map[string]bool{}
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if kindOf(kinds, n) == "variable_declarator" {
			name, value := n.ChildByFieldName("name"), n.ChildByFieldName("value")
			if name != nil && value != nil && kindOf(kinds, value) == "new_expression" {
				ctor := value.ChildByFieldName("constructor")
				if ctor != nil && bindings.defaults["@pothos/core"][nodeText(ctor, src)] {
					out[nodeText(name, src)] = true
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

func importedReceiverCall(kinds *tsutil.KindTable, fn *sitter.Node, src []byte, receivers map[string]bool) string {
	if kindOf(kinds, fn) != "member_expression" {
		return ""
	}
	obj, prop := fn.ChildByFieldName("object"), fn.ChildByFieldName("property")
	if obj == nil || prop == nil || !receivers[nodeText(obj, src)] {
		return ""
	}
	return nodeText(prop, src)
}

type graphqlServerContext struct {
	enabled      bool
	sdlDocuments map[string]bool
}

// detectGraphQLServerUsage establishes repository context through syntax, not
// text: examples in comments and strings must not turn a library into a server.
// It also records standalone SDL documents with direct provenance (an import by
// a server-construction file, or Hasura's metadata convention).
func detectGraphQLServerUsage(files []string, sources map[string][]byte) graphqlServerContext {
	ctx := graphqlServerContext{sdlDocuments: map[string]bool{}}
	for _, relFile := range files {
		src := sources[relFile]
		contrib := collectGraphQLContribution(relFile, src)
		if contrib.Server {
			ctx.enabled = true
		}
		for _, imported := range contrib.SDL {
			ctx.sdlDocuments[imported] = true
		}
	}
	return ctx
}

// graphqlFileContribution is one file's GraphQL-server context summary. Cached
// unchanged files reuse this instead of re-running graphQLServerASTSignals.
type graphqlFileContribution struct {
	Server bool
	SDL    []string
}

func collectGraphQLContribution(relFile string, src []byte) graphqlFileContribution {
	var out graphqlFileContribution
	if facts.IsTestPath(relFile) {
		return out
	}
	if isGraphQLDocFile(relFile) {
		if isHasuraSDLPath(relFile) {
			out.Server = true
			out.SDL = []string{filepath.ToSlash(relFile)}
		}
		return out
	}
	if len(src) == 0 || !possibleGraphQLServerSignal(src) {
		return out
	}
	server, imports := graphQLServerASTSignals(src, relFile)
	if server {
		out.Server = true
		out.SDL = imports
	}
	return out
}

var graphqlServerSignalTokens = [][]byte{
	[]byte("ApolloServer"), []byte("GraphQLServer"), []byte("buildSchema"), []byte("makeExecutableSchema"), []byte("graphqlHTTP"),
	[]byte("@apollo/server"), []byte("apollo-server"), []byte("graphql-yoga"), []byte("mercurius"), []byte("express-graphql"),
	[]byte("graphql-http"), []byte("@graphql-tools/schema"),
	[]byte("@nestjs/graphql"), []byte("type-graphql"), []byte("nexus"), []byte("@pothos/core"),
}

func possibleGraphQLServerSignal(src []byte) bool {
	for _, token := range graphqlServerSignalTokens {
		if bytes.Contains(src, token) {
			return true
		}
	}
	return false
}

// extractGraphQLCodeFirst reads root fields from the high-confidence code-first
// APIs used by NestJS/TypeGraphQL, Nexus, and Pothos. Package provenance and
// syntax are both required: a method called queryField, or an unrelated @Query
// decorator, must not become a GraphQL route merely because its spelling fits.
func extractGraphQLCodeFirst(src []byte, relFile string) []facts.Fact {
	lang := typescript.LanguageTypescript()
	if strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx") {
		lang = typescript.LanguageTSX()
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	kinds := tsKindsFor(strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx"))
	root := tree.RootNode()
	return extractGraphQLCodeFirstAST(src, relFile, kinds, root, collectGraphQLImportBindings(kinds, root, src))
}

func extractGraphQLCodeFirstAST(src []byte, relFile string, kinds *tsutil.KindTable, rootNode *sitter.Node, bindings graphqlImportBindings) []facts.Fact {
	decoratorEnabled := bindings.hasPackage("@nestjs/graphql") || bindings.hasPackage("type-graphql")
	nexusEnabled := bindings.hasPackage("nexus")
	pothosEnabled := bindings.hasPackage("@pothos/core")
	if !decoratorEnabled && !nexusEnabled && !pothosEnabled {
		return nil
	}
	pothosBuilders := collectPothosBuilders(kinds, rootNode, src, bindings)

	seen := map[string]bool{}
	var out []facts.Fact
	add := func(kind, name string, line int, source string) {
		if name == "" {
			return
		}
		full := kind + "." + name
		if seen[full] {
			return
		}
		seen[full] = true
		out = append(out, facts.Fact{Kind: facts.KindRoute, Name: full, File: relFile, Line: line, Props: map[string]any{
			"language": "typescript", "framework": "graphql-code-first",
			facts.PropRole: facts.RoleServer, facts.PropRouteType: facts.RouteTypeGraphQL,
			facts.PropSource: source,
		}})
	}

	var walk func(*sitter.Node)
	emitDecoratedMember := func(member *sitter.Node, decorators []*sitter.Node) {
		if !decoratorEnabled {
			return
		}
		method := findChildByKind(kinds, member, "property_identifier")
		if method == nil {
			method = findChildByKind(kinds, member, "identifier")
		}
		if method == nil {
			return
		}
		for _, d := range decorators {
			name, args := decoratorNameArgs(kinds, d, src)
			name = bindings.exportedName(name, "@nestjs/graphql", "type-graphql")
			root := graphqlRootNames[name]
			if root == "" {
				continue
			}
			field := nodeText(method, src)
			if explicit, overridden := graphqlDecoratorFieldName(kinds, args, src); overridden {
				// A computed override is the runtime GraphQL name, but cannot be
				// resolved statically. Falling back to the method name would invent
				// a route that does not exist.
				if explicit == "" {
					continue
				}
				field = explicit
			}
			add(root, field, int(d.StartPosition().Row)+1, facts.RouteSourceGraphQLDecorator)
		}
	}
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch kindOf(kinds, n) {
		case "class_body":
			var pending []*sitter.Node
			for i := range n.ChildCount() {
				member := n.Child(i)
				if kindOf(kinds, member) == "decorator" {
					pending = append(pending, member)
					continue
				}
				if !member.IsNamed() || kindOf(kinds, member) == "comment" {
					continue
				}
				if kindOf(kinds, member) == "method_definition" || kindOf(kinds, member) == "public_field_definition" {
					emitDecoratedMember(member, pending)
				}
				pending = nil
			}
		case "call_expression":
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn != nil && args != nil {
				field := firstStringArg(kinds, args, src)
				nexusCall := bindings.callExport(kinds, fn, src, "nexus")
				pothosCall := importedReceiverCall(kinds, fn, src, pothosBuilders)
				switch {
				case pothosEnabled && graphqlRootByFieldCall[pothosCall] != "":
					add(graphqlRootByFieldCall[pothosCall], field, int(n.StartPosition().Row)+1, facts.RouteSourceGraphQLPothos)
				case nexusEnabled && graphqlRootByFieldCall[nexusCall] != "":
					root := graphqlRootByFieldCall[nexusCall]
					add(root, field, int(n.StartPosition().Row)+1, facts.RouteSourceGraphQLNexus)
					for _, f := range nexusCallbackFields(kinds, args, src) {
						add(root, f.name, f.line, facts.RouteSourceGraphQLNexus)
					}
				case nexusEnabled && (nexusCall == "queryType" || nexusCall == "mutationType" || nexusCall == "subscriptionType" || nexusCall == "extendType"):
					root := graphqlRootByDeclaration[nexusCall]
					obj := findChildByKind(kinds, args, "object")
					if nexusCall == "extendType" && obj != nil {
						root = objectStringProp(kinds, obj, src, "type")
						if root != "Query" && root != "Mutation" && root != "Subscription" {
							root = ""
						}
					}
					if root != "" {
						for _, f := range nexusDefinitionFields(kinds, obj, src) {
							add(root, f.name, f.line, facts.RouteSourceGraphQLNexus)
						}
					}
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(rootNode)
	return out
}

// graphqlDecoratorFieldName returns the static override and whether an override
// was supplied. An empty name with overridden=true denotes a computed name.
func graphqlDecoratorFieldName(kinds *tsutil.KindTable, args *sitter.Node, src []byte) (string, bool) {
	if args == nil {
		return "", false
	}
	// NestJS's schema-first overload accepts @Query("field").
	if name := firstStringArg(kinds, args, src); name != "" {
		return name, true
	}
	// Code-first NestJS and TypeGraphQL put an override in the options object,
	// commonly after a return-type arrow: @Query(() => T, { name: "field" }).
	for i := range args.ChildCount() {
		arg := args.Child(i)
		if kindOf(kinds, arg) == "template_string" {
			text := nodeText(arg, src)
			if strings.Contains(text, "${") {
				return "", true
			}
			return strings.Trim(text, "`"), true
		}
		if kindOf(kinds, arg) != "object" {
			continue
		}
		for j := range arg.ChildCount() {
			pair := arg.Child(j)
			if kindOf(kinds, pair) != "pair" {
				continue
			}
			key := pair.ChildByFieldName("key")
			if key == nil || strings.Trim(nodeText(key, src), `"'`) != "name" {
				continue
			}
			value := pair.ChildByFieldName("value")
			if value == nil {
				return "", true
			}
			switch kindOf(kinds, value) {
			case "string":
				return strings.Trim(nodeText(value, src), `"'`), true
			case "template_string":
				text := nodeText(value, src)
				if !strings.Contains(text, "${") {
					return strings.Trim(text, "`"), true
				}
			}
			return "", true
		}
	}
	return "", false
}

type graphqlNamedLine struct {
	name string
	line int
}

var nexusNonFieldMethods = map[string]bool{"implements": true, "modify": true}

func nexusDefinitionFields(kinds *tsutil.KindTable, obj *sitter.Node, src []byte) []graphqlNamedLine {
	if obj == nil {
		return nil
	}
	for i := range obj.ChildCount() {
		member := obj.Child(i)
		switch kindOf(kinds, member) {
		case "method_definition":
			name := member.ChildByFieldName("name")
			if name != nil && nodeText(name, src) == "definition" {
				return nexusFieldsInFunction(kinds, member, src)
			}
		case "pair":
			key := member.ChildByFieldName("key")
			if key == nil || strings.Trim(nodeText(key, src), `"'`) != "definition" {
				continue
			}
			value := member.ChildByFieldName("value")
			if value != nil {
				return nexusFieldsInFunction(kinds, value, src)
			}
		}
	}
	return nil
}

func nexusCallbackFields(kinds *tsutil.KindTable, args *sitter.Node, src []byte) []graphqlNamedLine {
	if args == nil {
		return nil
	}
	for i := range args.ChildCount() {
		arg := args.Child(i)
		if kindOf(kinds, arg) == "arrow_function" || kindOf(kinds, arg) == "function_expression" {
			return nexusFieldsInFunction(kinds, arg, src)
		}
	}
	return nil
}

func nexusFieldsInFunction(kinds *tsutil.KindTable, fn *sitter.Node, src []byte) []graphqlNamedLine {
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		params = fn.ChildByFieldName("parameter")
	}
	builder := firstDescendantOfKind(kinds, params, "identifier")
	if builder == nil {
		return nil
	}
	prefix := nodeText(builder, src) + "."
	body := fn.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	var out []graphqlNamedLine
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		// A nested closure is a different lexical scope. In particular, its `t`
		// may shadow the Nexus definition builder and must not declare root fields.
		if n != body && tsIsFunctionLike(kindOf(kinds, n)) {
			return
		}
		if kindOf(kinds, n) == "call_expression" {
			callee := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			calleeText, method := nodeText(callee, src), ""
			if callee != nil && kindOf(kinds, callee) == "member_expression" {
				if prop := callee.ChildByFieldName("property"); prop != nil {
					method = nodeText(prop, src)
				}
			}
			if strings.HasPrefix(calleeText, prefix) && method != "" && !nexusNonFieldMethods[method] {
				if name := firstStringArg(kinds, args, src); name != "" {
					out = append(out, graphqlNamedLine{name: name, line: int(n.StartPosition().Row) + 1})
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(body)
	return out
}

func firstDescendantOfKind(kinds *tsutil.KindTable, node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}
	if kindOf(kinds, node) == kind {
		return node
	}
	for i := range node.ChildCount() {
		if found := firstDescendantOfKind(kinds, node.Child(i), kind); found != nil {
			return found
		}
	}
	return nil
}

func isHasuraSDLPath(path string) bool {
	p := "/" + strings.ToLower(filepath.ToSlash(path)) + "/"
	return strings.Contains(p, "/hasura/metadata/")
}

func graphQLServerASTSignals(src []byte, relFile string) (bool, []string) {
	lang := typescript.LanguageTypescript()
	if strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx") {
		lang = typescript.LanguageTSX()
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		return false, nil
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	kinds := tsKindsFor(strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx"))
	root := tree.RootNode()
	server := false
	var gqlImports []string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch kindOf(kinds, n) {
		case "import_statement":
			if source := findChildByKind(kinds, n, "string"); source != nil {
				path := strings.Trim(nodeText(source, src), `"'`)
				base := graphqlPackageBase(path)
				if graphqlServerPackages[base] {
					server = true
				}
				// TODO: Resolve tsconfig path aliases before accepting non-relative
				// .graphql/.gql imports, then add positive and ambiguous-alias tests.
				if isGraphQLDocFile(path) && strings.HasPrefix(path, ".") {
					gqlImports = append(gqlImports, factpath.Clean(factpath.Join(factpath.Dir(relFile), path)))
				}
			}
		case "new_expression":
			if ctor := n.ChildByFieldName("constructor"); ctor != nil {
				name := nodeText(ctor, src)
				if i := strings.IndexByte(name, '<'); i >= 0 {
					name = name[:i]
				}
				if name == "ApolloServer" || name == "GraphQLServer" {
					server = true
				}
			}
		case "call_expression":
			if fn := n.ChildByFieldName("function"); fn != nil {
				name := nodeText(fn, src)
				if name == "buildSchema" || name == "makeExecutableSchema" || name == "graphqlHTTP" || name == "createSchema" {
					server = true
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(root)
	if !server {
		return false, nil
	}
	return true, gqlImports
}

// serverSDLOpen matches an SDL binding/object property or direct buildSchema
// call through its opening backtick. Bindings may carry a TypeScript annotation
// (`const schema: string = ...`) and modular schemas commonly use suffixed names
// (`gqlSchema`, `userTypeDefs`). Object properties stay restricted to the
// conventional schema/typeDefs keys so an arbitrary fooSchema property is not
// promoted merely because the repository also runs a GraphQL server.
var serverSDLOpen = regexp.MustCompile("(?:(?:\\b(?:typeDefs|typeDefinitions|schema|[A-Za-z_$][A-Za-z0-9_$]*(?:TypeDefs|TypeDefinitions|Schema))\\s*(?::\\s*[^=\\n]+)?\\s*=)|(?:\\b(?:typeDefs|schema)\\s*:)|(?:\\bbuildSchema\\s*\\())\\s*(?:(?:gql|graphql)\\s*)?`")

// sdlTypeBlock matches a Query/Mutation/Subscription root type declaration
// through its opening brace.
var sdlTypeBlock = regexp.MustCompile(`(?m)^\s*(?:extend\s+)?type\s+(Query|Mutation|Subscription)\b[^{]*\{`)

var (
	tokBacktick           = []byte("`")
	tokSchemaLow          = []byte("schema")
	tokSchemaCap          = []byte("Schema")
	tokTypeDefs           = []byte("typeDefs")
	tokTypeDefsCap        = []byte("TypeDefs")
	tokTypeDefinitions    = []byte("typeDefinitions")
	tokTypeDefinitionsCap = []byte("TypeDefinitions")
	tokBuildSchema        = []byte("buildSchema")
	tokGQLTag             = []byte("gql`")
	tokGraphQLTag         = []byte("graphql`")
	tokGraphQLRequest     = []byte("graphql-request")
	tokFetchParen         = []byte("fetch(")
	tokQueryColon         = []byte("query:")
	tokType               = []byte("type")
	tokQueryRoot          = []byte("Query")
	tokMutationRoot       = []byte("Mutation")
	tokSubscriptionRoot   = []byte("Subscription")
)

func possibleGraphQLServerSDL(src []byte) bool {
	if !bytes.Contains(src, tokBacktick) {
		return false
	}
	if !containsAny(src, tokSchemaLow, tokSchemaCap, tokTypeDefs, tokTypeDefsCap, tokTypeDefinitions, tokTypeDefinitionsCap, tokBuildSchema) {
		return false
	}
	return hasGraphQLSDLRootType(src)
}

// hasGraphQLSDLRootType is a necessary condition for sdlTypeBlock: `type` +
// whitespace + Query|Mutation|Subscription. `extend type Query` contains that
// sequence after `extend`.
func hasGraphQLSDLRootType(src []byte) bool {
	for i := 0; i < len(src); {
		j := bytes.Index(src[i:], tokType)
		if j < 0 {
			return false
		}
		at := i + j
		if at > 0 && isIdentByte(src[at-1]) {
			i = at + 4
			continue
		}
		end := at + 4
		if end < len(src) && isIdentByte(src[end]) {
			i = at + 1
			continue
		}
		k := end
		for k < len(src) && isASCIISpace(src[k]) {
			k++
		}
		if hasGraphQLRootNameAt(src, k) {
			return true
		}
		i = at + 4
	}
	return false
}

func hasGraphQLRootNameAt(src []byte, k int) bool {
	for _, name := range [][]byte{tokQueryRoot, tokMutationRoot, tokSubscriptionRoot} {
		if bytes.HasPrefix(src[k:], name) {
			end := k + len(name)
			if end == len(src) || !isIdentByte(src[end]) {
				return true
			}
		}
	}
	return false
}

// extractGraphQLServerSDL returns one server route per root field in candidate
// SDL templates. The caller owns the repository-level server gate.
func extractGraphQLServerSDL(src []byte, relFile string) []facts.Fact {
	if !possibleGraphQLServerSDL(src) {
		return nil
	}
	text := string(blankTSComments(src, relFile))
	var out []facts.Fact
	for _, m := range serverSDLOpen.FindAllStringIndex(text, -1) {
		open := m[1] - 1 // the backtick itself
		body, _ := templateBody(text, open)
		baseLine := 1 + strings.Count(text[:open], "\n")
		out = append(out, sdlRootFields(body, relFile, baseLine)...)
	}
	return out
}

func blankTSComments(src []byte, relFile string) []byte {
	lang := typescript.LanguageTypescript()
	if strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx") {
		lang = typescript.LanguageTSX()
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		return src
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	out := append([]byte(nil), src...)
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "comment" {
			for i := int(n.StartByte()); i < int(n.EndByte()) && i < len(out); i++ {
				if out[i] != '\n' && out[i] != '\r' {
					out[i] = ' '
				}
			}
			return
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return out
}

// extractGraphQLServerSDLDocument handles schema-first servers that load SDL
// from standalone .graphql/.gql files (Hasura metadata and loader-based Node
// servers are common examples). Repository context is established by the
// caller; this function only parses the document surface.
func extractGraphQLServerSDLDocument(src []byte, relFile string) []facts.Fact {
	return sdlRootFields(string(src), relFile, 1)
}

// sdlRootFields returns one route fact per root field declared inside every
// Query/Mutation/Subscription block of an SDL document, with line numbers
// relative to baseLine (the document's own start line in its enclosing file).
func sdlRootFields(body, relFile string, baseLine int) []facts.Fact {
	var out []facts.Fact
	structural := blankSDLNonStructuralText(body)
	for _, tm := range sdlTypeBlock.FindAllStringSubmatchIndex(structural, -1) {
		kind := body[tm[2]:tm[3]] // Query, Mutation, or Subscription
		blockStart := tm[1]       // just after the opening brace
		blockEnd := matchingBrace(body, blockStart)
		blockLine := baseLine + strings.Count(body[:blockStart], "\n")
		for _, field := range sdlFieldNames(body[blockStart:blockEnd]) {
			out = append(out, facts.Fact{
				Kind: facts.KindRoute,
				Name: kind + "." + field.name,
				File: relFile,
				Line: blockLine + field.lineOffset,
				Props: map[string]any{
					"language":          "typescript",
					"framework":         "graphql-sdl",
					facts.PropRole:      facts.RoleServer,
					facts.PropRouteType: facts.RouteTypeGraphQL,
					facts.PropSource:    facts.RouteSourceGraphQLSDL,
				},
			})
		}
	}
	return out
}

// blankSDLNonStructuralText removes comments, quoted strings, and block-string
// descriptions while preserving byte offsets and newlines. Root declarations
// shown inside a `"""..."""` description are prose, not schema structure.
func blankSDLNonStructuralText(text string) string {
	out := []byte(text)
	for i := 0; i < len(out); {
		start := i
		switch out[i] {
		case '#':
			i = skipSDLComment(text, i)
		case '"':
			i = skipSDLString(text, i)
		default:
			i++
			continue
		}
		for j := start; j < i; j++ {
			if out[j] != '\n' && out[j] != '\r' {
				out[j] = ' '
			}
		}
	}
	return string(out)
}

// matchingBrace returns the index into text of the closing brace matching the
// opening one just before start (start is the position right after it), or
// len(text) if the block is unterminated. Comments and string/block-string
// values are skipped so braces in descriptions or default values do not alter
// the structural depth.
func matchingBrace(text string, start int) int {
	depth := 1
	i := start
	for i < len(text) && depth > 0 {
		switch text[i] {
		case '#':
			i = skipSDLComment(text, i)
			continue
		case '"':
			i = skipSDLString(text, i)
			continue
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	if depth != 0 {
		return len(text)
	}
	return i - 1
}

type sdlField struct {
	name       string
	lineOffset int
}

// sdlFieldNames reads direct field declarations in a root type body. It tracks
// argument and nested-value depth so multiline arguments cannot masquerade as
// fields, and skips GraphQL comments and string/block-string descriptions.
func sdlFieldNames(block string) []sdlField {
	var fields []sdlField
	parenDepth, braceDepth, line := 0, 0, 0
	for i := 0; i < len(block); {
		switch {
		case block[i] == '\n':
			line++
			i++
		case block[i] == '#':
			i = skipSDLComment(block, i)
		case block[i] == '"':
			next := skipSDLString(block, i)
			line += strings.Count(block[i:next], "\n")
			i = next
		case block[i] == '(':
			parenDepth++
			i++
		case block[i] == ')':
			if parenDepth > 0 {
				parenDepth--
			}
			i++
		case block[i] == '{':
			braceDepth++
			i++
		case block[i] == '}':
			if braceDepth > 0 {
				braceDepth--
			}
			i++
		case parenDepth == 0 && braceDepth == 0 && isSDLIdentStart(block[i]):
			start := i
			i++
			for i < len(block) && isSDLIdentChar(block[i]) {
				i++
			}
			next := skipSDLTrivia(block, i)
			if !precededBySDLDirective(block, start) && next < len(block) && (block[next] == '(' || block[next] == ':') {
				fields = append(fields, sdlField{name: block[start:i], lineOffset: line})
			}
		default:
			i++
		}
	}
	return fields
}

func precededBySDLDirective(text string, i int) bool {
	for i > 0 {
		i--
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n' || text[i] == ',' {
			continue
		}
		return text[i] == '@'
	}
	return false
}

func skipSDLComment(text string, i int) int {
	for i < len(text) && text[i] != '\n' {
		i++
	}
	return i
}

func skipSDLString(text string, i int) int {
	if strings.HasPrefix(text[i:], `"""`) {
		if end := strings.Index(text[i+3:], `"""`); end >= 0 {
			return i + 3 + end + 3
		}
		return len(text)
	}
	for i++; i < len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if i < len(text) && text[i] == '"' {
			return i + 1
		}
	}
	return len(text)
}

func skipSDLTrivia(text string, i int) int {
	for i < len(text) {
		if text[i] == ',' || text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n' {
			i++
			continue
		}
		if text[i] == '#' {
			i = skipSDLComment(text, i)
			continue
		}
		break
	}
	return i
}

func isSDLIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isSDLIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// bindGraphQLSchemaResolvers attaches RelHandledBy from SDL/code-first root
// fields to explicit createSchema/makeExecutableSchema resolver functions.
func bindGraphQLSchemaResolvers(kinds *tsutil.KindTable, root *sitter.Node, src []byte, relFile string, ff []facts.Fact, importMap map[string]string) []facts.Fact {
	bindings := collectGraphQLImportBindings(kinds, root, src)
	if !bindings.hasPackage("graphql-yoga") && !bindings.hasPackage("@graphql-tools/schema") {
		return nil
	}
	dir := factpath.Dir(relFile)
	routes := map[string]int{}
	for i, f := range ff {
		if f.Kind == facts.KindRoute && (f.Props[facts.PropRouteType] == facts.RouteTypeGraphQL || f.Props["type"] == "graphql") {
			routes[f.Name] = i
		}
	}
	var extra []facts.Fact
	var calls []*sitter.Node
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if kindOf(kinds, n) == "call_expression" {
			fn := n.ChildByFieldName("function")
			if fn != nil {
				exported := bindings.callExport(kinds, fn, src, "graphql-yoga")
				if exported == "" {
					exported = bindings.callExport(kinds, fn, src, "@graphql-tools/schema")
				}
				if exported == "createSchema" || exported == "makeExecutableSchema" {
					calls = append(calls, n)
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(root)
	for _, call := range calls {
		extra = append(extra, bindSchemaResolverObject(kinds, call, src, relFile, dir, ff, routes, importMap, root, len(calls) > 1)...)
	}
	return extra
}

func bindSchemaResolverObject(kinds *tsutil.KindTable, call *sitter.Node, src []byte, relFile, dir string, ff []facts.Fact, routes map[string]int, importMap map[string]string, root *sitter.Node, multiCall bool) []facts.Fact {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var cfg *sitter.Node
	for i := range args.ChildCount() {
		if kindOf(kinds, args.Child(i)) == "object" {
			cfg = args.Child(i)
			break
		}
	}
	resolvers := objectPropValue(kinds, cfg, src, "resolvers")
	if resolvers == nil || kindOf(kinds, resolvers) != "object" {
		return nil
	}
	owned, haveOwned := graphqlCallTypeDefFields(kinds, cfg, src, root)
	if !haveOwned && multiCall {
		return nil
	}
	var extra []facts.Fact
	for i := range resolvers.ChildCount() {
		pair := resolvers.Child(i)
		if kindOf(kinds, pair) != "pair" {
			continue
		}
		key := pair.ChildByFieldName("key")
		val := pair.ChildByFieldName("value")
		if key == nil || val == nil {
			continue
		}
		rootName := strings.Trim(nodeText(key, src), `"'`)
		if rootName != "Query" && rootName != "Mutation" && rootName != "Subscription" {
			continue
		}
		if kindOf(kinds, val) != "object" {
			continue
		}
		for j := range val.ChildCount() {
			field := val.Child(j)
			if kindOf(kinds, field) != "pair" {
				continue
			}
			fk := field.ChildByFieldName("key")
			fv := field.ChildByFieldName("value")
			if fk == nil || fv == nil {
				continue
			}
			fieldName := strings.Trim(nodeText(fk, src), `"'`)
			routeName := rootName + "." + fieldName
			if haveOwned && !owned[routeName] {
				continue
			}
			ri, ok := routes[routeName]
			if !ok {
				continue
			}
			handler, sym := graphqlResolverHandler(kinds, fv, src, relFile, dir, routeName, int(fv.StartPosition().Row)+1, importMap)
			if handler == "" {
				continue
			}
			if !ff[ri].HasRelation(facts.RelHandledBy, handler) {
				ff[ri].Relations = append(ff[ri].Relations, facts.Relation{Kind: facts.RelHandledBy, Target: handler})
			}
			if ff[ri].Props == nil {
				ff[ri].Props = map[string]any{}
			}
			ff[ri].Props["handler"] = handler
			if sym != nil {
				extra = append(extra, *sym)
			}
		}
	}
	return extra
}

func graphqlResolverHandler(kinds *tsutil.KindTable, value *sitter.Node, src []byte, relFile, dir, routeName string, line int, importMap map[string]string) (string, *facts.Fact) {
	switch kindOf(kinds, value) {
	case "identifier":
		name := nodeText(value, src)
		if canon := importMap[name]; canon != "" {
			return canon, nil
		}
		return dir + "." + name, nil
	case "arrow_function", "function_expression", "function_declaration":
		name := dir + "." + routeName
		return name, &facts.Fact{
			Kind: facts.KindSymbol,
			Name: name,
			File: relFile,
			Line: line,
			Props: map[string]any{
				"symbol_kind": facts.SymbolFunc,
				"exported":    false,
				"language":    "typescript",
			},
			Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: dir}},
		}
	default:
		return "", nil
	}
}

// gqlTagOpen matches the opening of a gql`…` / graphql`…` tagged template.
//
// The leading class is load-bearing: a tag sits where an expression starts, so
// the character before it is punctuation or whitespace, never part of a path.
// `uri: ${this.config.railsURL}/graphql` ends in the word and a backtick and is
// not a tag at all — the backtick closes the surrounding template — and a
// pattern keyed only on the word matched it. Where the body ENDS is not a regex
// question at all; see templateBody.
var gqlTagOpen = regexp.MustCompile("(?:^|[\\s=(,{\\[:;>?])(?:gql|graphql)`")

// templateBody returns the span of a template literal whose opening backtick is
// at start, handling the one thing a regex cannot: `${…}` interpolations that
// contain their own template literals, and therefore their own backticks.
//
// The old pattern was `([^`]*)`, which ends the body at the first inner
// backtick. One frontend's analytics reports interpolate a whole selection —
// `${inOverview ? `sessionPageviewQuery(…) {…}` : `…`}` — so the captured body
// was the operation head and half an interpolation, with no closing brace in
// it. Two consequences, and the quiet one is worse. The visible one: the head
// regex had no `${…}` to skip, so it stopped at the interpolation's brace and
// read the JavaScript variable as the operation's first field, which is where
// Query.inOverview and Query.onlyVisits came from. The quiet one: every real
// root field after the truncation point was never seen at all.
func templateBody(text string, start int) (body string, end int) {
	i := start + 1
	for i < len(text) {
		switch {
		case text[i] == '\\':
			i += 2
		case text[i] == '`':
			return text[start+1 : i], i + 1
		case text[i] == '$' && i+1 < len(text) && text[i+1] == '{':
			i = skipInterpolation(text, i+2)
		default:
			i++
		}
	}
	// Unterminated: return nothing. The tempting alternative — hand back the
	// rest of the file, since an operation at the top of it still names its
	// root fields — is how `uri: ${config.railsURL}/graphql` came to be read
	// as a tag opening. The backtick after that word CLOSES an ordinary
	// template, so the scan ran to end of file and read an Apollo options
	// object as a document, emitting Query.variables and Query.network. A
	// missing edge beats a wrong one.
	return "", len(text)
}

// skipInterpolation walks from just past a `${` to just past its matching `}`,
// following nested braces and any template literals inside — which may
// themselves interpolate.
func skipInterpolation(text string, i int) int {
	braces := 1
	for i < len(text) && braces > 0 {
		switch text[i] {
		case '\\':
			i++
		case '{':
			braces++
		case '}':
			braces--
		case '`':
			_, next := templateBody(text, i)
			i = next
			continue
		}
		i++
	}
	return i
}

// extractGraphQLTagFacts pulls client operations out of a source file's tagged
// templates.
func extractGraphQLTagFacts(src []byte, relFile string) []facts.Fact {
	if !bytes.Contains(src, tokGQLTag) && !bytes.Contains(src, tokGraphQLTag) {
		return nil
	}
	text := string(src)
	var out []facts.Fact
	for at := 0; at < len(text); {
		m := gqlTagOpen.FindStringIndex(text[at:])
		if m == nil {
			break
		}
		open := at + m[1] - 1
		body, end := templateBody(text, open)
		for _, f := range extractGraphQLClientOps(body, relFile, facts.RouteSourceGraphQLTag) {
			f.Line += strings.Count(text[:open+1], "\n")
			out = append(out, f)
		}
		at = end
	}
	return out
}

// extractGraphQLTagFactsAST finds template literals through syntax, so examples
// in comments and ordinary strings cannot masquerade as gql/graphql tags.
// TODO: Once Relay synthesized operations are modeled, cover @refetchable
// fragments and their generated query/root-field semantics with tests.
func extractGraphQLTagFactsAST(src []byte, relFile string, kinds *tsutil.KindTable, isTSX bool, root *sitter.Node) []facts.Fact {
	var out []facts.Fact
	// Tagged templates are matched by tree-sitter rather than found by walking every
	// node in the file; see tsutil.QueryNodes.
	for _, n := range tsutil.QueryNodes(tsGrammarKey(isTSX), tsGrammarLanguage(isTSX), "(template_string) @t", root) {
		{
			parent := n.Parent()
			if parent != nil && kindOf(kinds, parent) == "call_expression" {
				fn := parent.ChildByFieldName("function")
				if fn != nil {
					tag := nodeText(fn, src)
					if tag == "gql" || tag == "graphql" {
						body := nodeText(n, src)
						if len(body) >= 2 {
							body = body[1 : len(body)-1]
							for _, f := range extractGraphQLClientOps(body, relFile, facts.RouteSourceGraphQLTag) {
								f.Line += int(n.StartPosition().Row)
								out = append(out, f)
							}
						}
					}
				}
			}
		}
	}
	return out
}

// extractGraphQLClientCallFacts covers clients that accept a document without
// a gql tag: graphql-request/urql request calls and plain fetch bodies with a
// `query` property. Only static string/template nodes containing an explicit
// operation are read. The package or fetch-body evidence is mandatory, keeping
// GraphQL examples in arbitrary strings inert.
func extractGraphQLClientCallFacts(src []byte, relFile string) []facts.Fact {
	possibleGraphQLRequest := bytes.Contains(src, tokGraphQLRequest)
	fetchBody := hasGraphQLFetchBodyCandidateBytes(src)
	if !possibleGraphQLRequest && !fetchBody {
		return nil
	}

	lang := typescript.LanguageTypescript()
	if strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx") {
		lang = typescript.LanguageTSX()
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		return nil
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	kinds := tsKindsFor(strings.HasSuffix(relFile, ".tsx") || strings.HasSuffix(relFile, ".jsx"))
	root := tree.RootNode()
	return extractGraphQLClientCallFactsAST(src, relFile, kinds, root, fetchBody, collectGraphQLImportBindings(kinds, root, src))
}

func hasGraphQLFetchBodyCandidate(text string) bool {
	return strings.Contains(text, "fetch(") && strings.Contains(text, "query:")
}

func hasGraphQLFetchBodyCandidateBytes(src []byte) bool {
	return bytes.Contains(src, tokFetchParen) && bytes.Contains(src, tokQueryColon)
}

func extractGraphQLClientCallFactsAST(src []byte, relFile string, kinds *tsutil.KindTable, rootNode *sitter.Node, fetchBody bool, bindings graphqlImportBindings) []facts.Fact {
	text := string(src)
	knownClient := bindings.hasPackage("graphql-request")
	if !knownClient && !fetchBody {
		return nil
	}
	seen := map[string]bool{}
	var out []facts.Fact
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		kind := kindOf(kinds, n)
		if kind == "string" || kind == "template_string" {
			eligible := knownClient
			if !eligible && fetchBody {
				if parent := n.Parent(); parent != nil && kindOf(kinds, parent) == "pair" {
					pair := strings.TrimSpace(nodeText(parent, src))
					eligible = strings.HasPrefix(pair, "query:") || strings.HasPrefix(pair, `"query":`) || strings.HasPrefix(pair, `'query':`)
				}
			}
			if !eligible {
				for i := range n.ChildCount() {
					walk(n.Child(i))
				}
				return
			}
			start := int(n.StartByte())
			prefix := text[max(0, start-12):start]
			// gql`...` and graphql`...` are handled by extractGraphQLTagFacts.
			if !strings.HasSuffix(strings.TrimSpace(prefix), "gql") && !strings.HasSuffix(strings.TrimSpace(prefix), "graphql") {
				body := nodeText(n, src)
				if len(body) >= 2 {
					body = body[1 : len(body)-1]
					for _, f := range extractGraphQLClientOps(body, relFile, facts.RouteSourceGraphQLClientCall) {
						if seen[f.Name] {
							continue
						}
						seen[f.Name] = true
						f.Line += int(n.StartPosition().Row)
						out = append(out, f)
					}
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(rootNode)
	return out
}
