package tsextractor

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/facts"
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
	for name, b := range typedFastifyParamBindings(src) {
		if _, taken := out[name]; taken {
			continue
		}
		out[name] = b
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

// baselineExtractHTTPClientFacts is the ungated reference extractor: every
// HTTP-client pass runs even when possibleHTTPClientSignal is false. It does
// not call the gated production entrypoint. Equality with extractHTTPClientFacts
// proves the prefilter is a necessary condition (no dropped matches) rather
// than comparing the gated implementation with itself.
func baselineExtractHTTPClientFacts(src []byte, relFile string) []facts.Fact {
	return extractHTTPClientFactsUngated(src, relFile)
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
