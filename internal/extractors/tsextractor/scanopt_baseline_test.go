package tsextractor

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/litfold"
)

// Frozen pre-fix helper implementations from the Product CPU profile source
// (/tmp/enola-product-profile-source, matching this tree before scan prefilters).
// Tests compare current helpers against these copies; they are not called from
// production code.

func baselineServerBindings(src []byte) map[string]serverBinding {
	out := map[string]serverBinding{}
	for _, re := range []*regexp.Regexp{appFactory, appFactoryRequire} {
		for _, m := range re.FindAllSubmatch(src, -1) {
			out[string(m[1])] = serverBinding{framework: frameworkOf[string(m[2])], mounted: true}
		}
	}
	for _, re := range []*regexp.Regexp{routerFactory, routerFactoryRequire} {
		for _, m := range re.FindAllSubmatch(src, -1) {
			name := string(m[1])
			if _, taken := out[name]; taken {
				continue
			}
			out[name] = serverBinding{framework: "express", isRouter: true}
		}
	}
	for _, m := range mountCall.FindAllSubmatch(src, -1) {
		prefix := firstNonEmpty(m[2], m[3], m[4])
		child := string(m[5])
		b, ok := out[child]
		if !ok || !b.isRouter || b.mounted {
			continue
		}
		if parent, ok := out[string(m[1])]; ok && parent.mounted {
			b.prefix = facts.JoinRoutePath(parent.prefix, prefix)
			b.mounted = true
			b.framework = parent.framework
			out[child] = b
		}
	}
	return out
}

func baselineCollectRouterFile(src []byte, relFile string, aliases map[string]tsAlias, knownFiles map[string]bool) *routerFile {
	bindings := baselineServerBindings(src)
	imports := collectRouterImports(src, relFile, aliases, knownFiles)
	if len(bindings) == 0 && len(imports) == 0 {
		return nil
	}

	f := &routerFile{
		relFile: relFile,
		roots:   map[string]bool{},
		routers: map[string]bool{},
		pending: map[string][]pendingRoute{},
		exports: map[string]string{},
		imports: imports,
	}

	for name, b := range bindings {
		switch {
		case !b.isRouter && b.mounted:
			f.roots[name] = true
		case b.isRouter && !b.mounted:
			f.routers[name] = true
		}
	}

	for _, m := range serverVerbCall.FindAllSubmatchIndex(src, -1) {
		recv := string(src[m[2]:m[3]])
		if !f.routers[recv] {
			continue
		}
		path, ok := cleanServerPath(firstNonEmptyGroup(src, m, 3, 4, 5))
		if !ok {
			continue
		}
		f.pending[recv] = append(f.pending[recv], pendingRoute{
			verb:      strings.ToUpper(nodeSlice(src, m, 2)),
			path:      path,
			line:      1 + bytes.Count(src[:m[0]], []byte("\n")),
			framework: bindings[recv].framework,
		})
	}

	for _, m := range mountCall.FindAllSubmatch(src, -1) {
		f.mounts = append(f.mounts, routerMountEdge{
			file:   relFile,
			parent: string(m[1]),
			prefix: firstNonEmpty(m[2], m[3], m[4]),
			child:  string(m[5]),
		})
	}
	for _, m := range mountCallFactory.FindAllSubmatch(src, -1) {
		f.mounts = append(f.mounts, routerMountEdge{
			file:      relFile,
			parent:    string(m[1]),
			prefix:    firstNonEmpty(m[2], m[3], m[4]),
			child:     string(m[5]),
			childCall: true,
		})
	}

	collectRouterExports(src, f)
	f.factories = collectRouterFactories(src, f.routers)

	if f.empty() {
		return nil
	}
	return f
}

func baselineExtractHTTPClientFacts(src []byte, relFile string) []facts.Fact {
	dir := factpath.Dir(relFile)
	api := tsAPIHint(relFile)
	bases := fileBaseLiterals(src)

	folds := litfold.NewAssignments()
	declared := map[int]bool{}
	for _, m := range urlAssignDecl.FindAllSubmatchIndex(src, -1) {
		folds.Add(string(src[m[2]:m[3]]), firstNonEmptyGroup(src, m, 2, 3, 4))
		declared[m[2]] = true
	}
	for _, m := range bareAssign.FindAllSubmatchIndex(src, -1) {
		name := string(src[m[2]:m[3]])
		if declared[m[2]] || name == "const" || name == "let" || name == "var" ||
			name == "return" || name == "typeof" || name == "await" {
			continue
		}
		folds.Add(name, "")
	}

	var out []facts.Fact
	seen := map[string]bool{}
	add := func(rawPath, method, framework string, off int, derived string) {
		path, ok := cleanTSPath(rawPath, bases)
		if !ok {
			return
		}
		line := 1 + bytes.Count(src[:off], []byte("\n"))
		key := method + "\x00" + path + "\x00" + strconv.Itoa(line)
		if seen[key] {
			return
		}
		seen[key] = true
		props := map[string]any{
			facts.PropRole:   facts.RoleClient,
			"method":         method,
			"framework":      framework,
			"language":       "typescript",
			facts.PropSource: facts.RouteSourceTSHTTPClient,
			"api":            api,
		}
		if testDoublePath(relFile) {
			props["test_double"] = true
		}
		if derived != "" {
			props["derived"] = derived
		}
		if h := tsBaseHint(rawPath); h != "" {
			props["target_hint"] = h
		}
		out = append(out, facts.Fact{
			Kind:      facts.KindRoute,
			Name:      path,
			File:      relFile,
			Line:      line,
			Props:     props,
			Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: dir}},
		})
	}

	for _, m := range httpClientCall.FindAllSubmatchIndex(src, -1) {
		raw := firstNonEmptyGroup(src, m, 2, 3, 4)
		method := "GET"
		if opts := optionsObjectAfter(src, m[1]); opts != nil {
			if mm := httpClientMethod.FindSubmatch(opts); mm != nil {
				method = strings.ToUpper(string(mm[1]))
			}
		}
		add(raw, method, "fetch", m[2], "")
	}
	for _, m := range identArgCall.FindAllSubmatchIndex(src, -1) {
		raw, ok := folds.Resolve(string(src[m[4]:m[5]]))
		if !ok {
			continue
		}
		method := "GET"
		if opts := optionsObjectAfter(src, m[5]); opts != nil {
			if mm := httpClientMethod.FindSubmatch(opts); mm != nil {
				method = strings.ToUpper(string(mm[1]))
			}
		}
		add(raw, method, "fetch", m[2], "single-assignment")
	}
	for _, m := range verbNamedCall.FindAllSubmatchIndex(src, -1) {
		method := strings.ToUpper(string(src[m[2]:m[3]]))
		raw := firstNonEmptyGroup(src, m, 2, 3, 4)
		add(raw, method, "openapi-fetch", m[0], "")
	}
	serverRecv := baselineServerBindings(src)
	for _, m := range lowerVerbCall.FindAllSubmatchIndex(src, -1) {
		if isServerReceiver(serverRecv, identifierEndingAt(src, m[0])) {
			continue
		}
		method := strings.ToUpper(string(src[m[2]:m[3]]))
		raw := firstNonEmptyGroup(src, m, 2, 3, 4)
		add(raw, method, "axios", m[0], "")
	}
	for _, m := range lowerVerbTemplateCall.FindAllSubmatchIndex(src, -1) {
		if isServerReceiver(serverRecv, identifierEndingAt(src, m[0])) {
			continue
		}
		raw := string(src[m[4]:m[5]])
		if !litfold.TemplateTailPath(raw) {
			continue
		}
		method := strings.ToUpper(string(src[m[2]:m[3]]))
		add(raw, method, "axios", m[0], "template-tail")
	}
	for _, m := range urlProperty.FindAllSubmatchIndex(src, -1) {
		window := enclosingObject(src, m[0], m[1])
		if window == nil {
			continue
		}
		method := "GET"
		haveVerb := false
		if vm := requestVerbProperty.FindSubmatch(window); vm != nil {
			if v := mapClientVerb(string(vm[1])); v != "" {
				method = v
				haveVerb = true
			}
		}
		if !haveVerb && !requestPayloadKey.Match(window) {
			continue
		}
		raw := firstNonEmptyGroup(src, m, 1, 2, 3)
		add(raw, method, "request-options", m[0], "")
	}
	for _, m := range urlPropertyIdent.FindAllSubmatchIndex(src, -1) {
		raw, ok := folds.Resolve(string(src[m[2]:m[3]]))
		if !ok {
			continue
		}
		window := enclosingObject(src, m[0], m[1])
		if window == nil {
			continue
		}
		method := facts.MethodAny
		haveVerb := false
		if vm := requestVerbProperty.FindSubmatch(window); vm != nil {
			if v := mapClientVerb(string(vm[1])); v != "" {
				method = v
				haveVerb = true
			}
		}
		if !haveVerb && !requestPayloadKey.Match(window) {
			continue
		}
		add(raw, method, "request-options", m[0], "single-assignment")
	}
	return out
}

func baselinePossibleGraphQLServerSignal(src []byte) bool {
	text := string(src)
	for _, token := range []string{
		"ApolloServer", "GraphQLServer", "buildSchema", "makeExecutableSchema", "graphqlHTTP",
		"@apollo/server", "apollo-server", "graphql-yoga", "mercurius", "express-graphql",
		"graphql-http", "@graphql-tools/schema",
		"@nestjs/graphql", "type-graphql", "nexus", "@pothos/core",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func baselineExtractGraphQLServerSDL(src []byte, relFile string) []facts.Fact {
	raw := string(src)
	if !strings.Contains(raw, "`") ||
		(!strings.Contains(raw, "schema") && !strings.Contains(raw, "Schema") &&
			!strings.Contains(raw, "typeDefs") && !strings.Contains(raw, "TypeDefs") &&
			!strings.Contains(raw, "typeDefinitions") && !strings.Contains(raw, "TypeDefinitions") &&
			!strings.Contains(raw, "buildSchema")) {
		return nil
	}
	text := string(blankTSComments(src, relFile))
	var out []facts.Fact
	for _, m := range serverSDLOpen.FindAllStringIndex(text, -1) {
		open := m[1] - 1
		body, _ := templateBody(text, open)
		baseLine := 1 + strings.Count(text[:open], "\n")
		out = append(out, sdlRootFields(body, relFile, baseLine)...)
	}
	return out
}

func baselineHasGraphQLBuildSegment(path string) bool {
	for _, segment := range strings.Split(filepathToSlash(path), "/") {
		if tsSkipDirs[segment] {
			return true
		}
	}
	return false
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
