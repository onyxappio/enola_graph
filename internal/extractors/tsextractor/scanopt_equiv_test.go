package tsextractor

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
)

func TestScanOpt_EdgeSyntaxMatchesBaseline(t *testing.T) {
	cases := []struct {
		name, file, src string
	}{
		{
			name: "express spaces and type annotation",
			file: "src/server.ts",
			src:  "const app: Express = express (\n);\napp . get ( \"/health\" , handler);\n",
		},
		{
			name: "dot-use with newlines",
			file: "src/server.ts",
			src: "const express = require('express');\nconst app = express();\nconst router = express.Router();\n" +
				"app.\n  use(\n    '/api',\n    router\n  );\nrouter.get('/items', h);\n",
		},
		{
			name: "hono and fastify factories",
			file: "src/server.ts",
			src:  "const app = new Hono();\napp.get('/ping', h);\nconst api = Fastify();\napi.post('/jobs', h);\n",
		},
		{
			name: "fetch ident and template",
			file: "src/client.ts",
			src:  "const url = `${HOST}/mcp`;\nfetch(url, { method: 'POST' });\nfetch('/api/x');\n",
		},
		{
			name: "url property with whitespace",
			file: "src/client.ts",
			src:  "request({ token, url\n:\n'/v2/messages.json' });\n",
		},
		{
			name: "lowercase verb with generic and spaces",
			file: "src/client.ts",
			src:  "await http.post<ApiResponse<string>> ( '/slots/reserve' , data);\n",
		},
		{
			name: "template-tail axios",
			file: "src/client.ts",
			src:  "await axios.get(`${baseUrl}/v1/charges`);\n",
		},
		{
			name: "openapi uppercase verbs",
			file: "src/client.ts",
			src:  "API.getApi().GET('/api/v3/items/{id}', { params });\n",
		},
		{
			name: "graphql sdl newline type",
			file: "src/schema.ts",
			src:  "const typeDefs = gql`\ntype\n  Query { ping: String }\n`;\n",
		},
		{
			name: "no signal large-looking ordinary",
			file: "src/util.ts",
			src:  "export function add(a: number, b: number) { return a + b; }\nconst schema = 1;\nprefetch(item);\nmap.get('k');\n",
		},
	}
	aliases := map[string]tsAlias{}
	known := map[string]bool{"src/server.ts": true, "src/client.ts": true, "src/schema.ts": true, "src/util.ts": true}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			assertHelperEquivalence(t, src, tc.file, aliases, known)
		})
	}
}

func TestScanOpt_LargeNegativeMatchesBaseline(t *testing.T) {
	src := largeNegativeSource()
	assertHelperEquivalence(t, src, "src/generated.ts", nil, map[string]bool{"src/generated.ts": true})
}

func TestScanOpt_LargeDecoyMatchesBaseline(t *testing.T) {
	src := largeDecoySource()
	assertHelperEquivalence(t, src, "src/decoy.ts", nil, map[string]bool{"src/decoy.ts": true})
}

func TestScanOpt_GraphQLSignalBytesVsString(t *testing.T) {
	neg := largeNegativeSource()
	if possibleGraphQLServerSignal(neg) || baselinePossibleGraphQLServerSignal(neg) {
		t.Fatal("negative source must not look like a GraphQL server")
	}
	pos := []byte("import { ApolloServer } from '@apollo/server';\nnew ApolloServer({ typeDefs });\n")
	if !possibleGraphQLServerSignal(pos) || !baselinePossibleGraphQLServerSignal(pos) {
		t.Fatal("ApolloServer constructor must remain a server signal")
	}
}

func TestScanOpt_SDLWithoutRootTypeEmitsNothing(t *testing.T) {
	src := []byte("const typeDefs = gql`type User { name: String }`;\nconst schema = `scalar Date`;\n")
	if got := extractGraphQLServerSDL(src, "src/schema.ts"); len(got) != 0 {
		t.Fatalf("non-root SDL emitted %v", got)
	}
	if got := baselineExtractGraphQLServerSDL(src, "src/schema.ts"); len(got) != 0 {
		t.Fatalf("baseline non-root SDL emitted %v", got)
	}
}

func TestScanOpt_BuildSegmentNoSplit(t *testing.T) {
	paths := []string{
		"src/index.ts",
		"dist/schema.graphql",
		"app/build/generated/ops.gql",
		"packages/foo/src/query.graphql",
	}
	for _, p := range paths {
		if hasGraphQLBuildSegment(p) != baselineHasGraphQLBuildSegment(p) {
			t.Errorf("%s: hasGraphQLBuildSegment mismatch", p)
		}
	}
}

func TestScanOpt_ProductHelpersMatchBaseline(t *testing.T) {
	root := os.Getenv("ENOLA_PRODUCT_FIXTURE")
	if root == "" {
		t.Skip("ENOLA_PRODUCT_FIXTURE unset; 6787-file Product helper equivalence already recorded")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Fatalf("ENOLA_PRODUCT_FIXTURE=%q is not a directory: %v", root, err)
	}
	exts := map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true}
	var files []string
	known := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !exts[filepath.Ext(path)] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		files = append(files, rel)
		known[rel] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("product fixture contained no TS/JS files")
	}

	var newRouters, oldRouters []*routerFile
	compared := 0
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		compared++
		if !bindingsEqual(serverBindings(src), baselineServerBindings(src)) {
			t.Fatalf("%s: serverBindings mismatch", rel)
		}
		gotRoutes := extractServerRouteFacts(src, rel)
		wantRoutes := baselineExtractServerRouteFacts(src, rel)
		if !factsEqual(gotRoutes, wantRoutes) {
			t.Fatalf("%s: extractServerRouteFacts mismatch\n got %v\nwant %v", rel, scanoptFactSummary(gotRoutes), scanoptFactSummary(wantRoutes))
		}
		gotHTTP := extractHTTPClientFacts(src, rel)
		wantHTTP := baselineExtractHTTPClientFacts(src, rel)
		if !factsEqual(gotHTTP, wantHTTP) {
			t.Fatalf("%s: extractHTTPClientFacts mismatch\n got %v\nwant %v", rel, scanoptFactSummary(gotHTTP), scanoptFactSummary(wantHTTP))
		}
		if possibleGraphQLServerSignal(src) != baselinePossibleGraphQLServerSignal(src) {
			t.Fatalf("%s: possibleGraphQLServerSignal mismatch", rel)
		}
		if hasGraphQLSDLRootType(src) {
			gotSDL := extractGraphQLServerSDL(src, rel)
			wantSDL := baselineExtractGraphQLServerSDL(src, rel)
			if !factsEqual(gotSDL, wantSDL) {
				t.Fatalf("%s: extractGraphQLServerSDL mismatch\n got %v\nwant %v", rel, scanoptFactSummary(gotSDL), scanoptFactSummary(wantSDL))
			}
		}
		gotR := collectRouterFile(src, rel, nil, known)
		wantR := baselineCollectRouterFile(src, rel, nil, known)
		if !routerContribEqual(gotR, wantR) {
			t.Fatalf("%s: collectRouterFile composition fields mismatch", rel)
		}
		if gotR != nil {
			newRouters = append(newRouters, gotR)
		}
		if wantR != nil {
			oldRouters = append(oldRouters, wantR)
		}
	}
	gotMounted := composeRouterMounts(newRouters)
	wantMounted := composeRouterMounts(oldRouters)
	if !factsEqual(gotMounted, wantMounted) {
		t.Fatalf("composeRouterMounts mismatch: got %d want %d", len(gotMounted), len(wantMounted))
	}
	t.Logf("compared %d product TS/JS files", compared)
}

func baselineExtractServerRouteFacts(src []byte, relFile string) []facts.Fact {
	bindings := baselineServerBindings(src)
	if len(bindings) == 0 {
		return nil
	}
	dir := factpath.Dir(relFile)
	var out []facts.Fact
	seen := map[string]bool{}
	for _, m := range serverVerbCall.FindAllSubmatchIndex(src, -1) {
		b, ok := bindings[string(src[m[2]:m[3]])]
		if !ok || !b.mounted {
			continue
		}
		raw := firstNonEmptyGroup(src, m, 3, 4, 5)
		path, ok := cleanServerPath(raw)
		if !ok {
			continue
		}
		verb := strings.ToUpper(nodeSlice(src, m, 2))
		full := facts.JoinRoutePath(b.prefix, path)
		line := 1 + bytes.Count(src[:m[0]], []byte("\n"))
		key := verb + "\x00" + full + "\x00" + strconv.Itoa(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, facts.Fact{
			Kind: facts.KindRoute,
			Name: full,
			File: relFile,
			Line: line,
			Props: map[string]any{
				facts.PropRole: facts.RoleServer,
				"method":       verb,
				"framework":    b.framework,
				"language":     "typescript",
			},
			Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: dir}},
		})
	}
	return out
}

func assertHelperEquivalence(t *testing.T, src []byte, relFile string, aliases map[string]tsAlias, known map[string]bool) {
	t.Helper()
	if !bindingsEqual(serverBindings(src), baselineServerBindings(src)) {
		t.Fatalf("serverBindings mismatch for %s: got %#v want %#v", relFile, serverBindings(src), baselineServerBindings(src))
	}
	if !factsEqual(extractServerRouteFacts(src, relFile), baselineExtractServerRouteFacts(src, relFile)) {
		t.Fatalf("extractServerRouteFacts mismatch for %s", relFile)
	}
	if !factsEqual(extractHTTPClientFacts(src, relFile), baselineExtractHTTPClientFacts(src, relFile)) {
		t.Fatalf("extractHTTPClientFacts mismatch for %s\n got %v\nwant %v", relFile, scanoptFactSummary(extractHTTPClientFacts(src, relFile)), scanoptFactSummary(baselineExtractHTTPClientFacts(src, relFile)))
	}
	if possibleGraphQLServerSignal(src) != baselinePossibleGraphQLServerSignal(src) {
		t.Fatalf("possibleGraphQLServerSignal mismatch for %s", relFile)
	}
	if !factsEqual(extractGraphQLServerSDL(src, relFile), baselineExtractGraphQLServerSDL(src, relFile)) {
		t.Fatalf("extractGraphQLServerSDL mismatch for %s", relFile)
	}
	if !routerContribEqual(collectRouterFile(src, relFile, aliases, known), baselineCollectRouterFile(src, relFile, aliases, known)) {
		t.Fatalf("collectRouterFile mismatch for %s", relFile)
	}
}

func bindingsEqual(a, b map[string]serverBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func factsEqual(a, b []facts.Fact) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func routerContribEqual(a, b *routerFile) bool {
	a = dropUnusedRouterFile(a)
	b = dropUnusedRouterFile(b)
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.relFile != b.relFile {
		return false
	}
	if !reflect.DeepEqual(a.roots, b.roots) || !reflect.DeepEqual(a.routers, b.routers) {
		return false
	}
	if !reflect.DeepEqual(a.pending, b.pending) || !reflect.DeepEqual(a.mounts, b.mounts) {
		return false
	}
	if !reflect.DeepEqual(nilToEmptyMap(a.factories), nilToEmptyMap(b.factories)) {
		return false
	}
	if len(a.routers) > 0 || len(a.factories) > 0 || len(b.routers) > 0 || len(b.factories) > 0 {
		if !reflect.DeepEqual(nilToEmptyMap(a.exports), nilToEmptyMap(b.exports)) {
			return false
		}
	}
	if len(a.mounts) > 0 || len(b.mounts) > 0 {
		if !reflect.DeepEqual(a.imports, b.imports) {
			return false
		}
	}
	return true
}

func dropUnusedRouterFile(f *routerFile) *routerFile {
	if f == nil {
		return nil
	}
	if len(f.pending) == 0 && len(f.mounts) == 0 && len(f.factories) == 0 && len(f.routers) == 0 {
		return nil
	}
	return f
}

func nilToEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func scanoptFactSummary(ff []facts.Fact) []string {
	out := make([]string, len(ff))
	for i, f := range ff {
		out[i] = f.Kind + " " + f.Name + " L" + strconv.Itoa(f.Line)
	}
	return out
}

func largeNegativeSource() []byte {
	var b strings.Builder
	b.Grow(1 << 20)
	for i := 0; i < 12000; i++ {
		b.WriteString("export function helper")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("(x: number) {\n  return x + 1;\n}\n")
	}
	return []byte(b.String())
}

func largeDecoySource() []byte {
	var b strings.Builder
	b.Grow(1 << 20)
	for i := 0; i < 8000; i++ {
		b.WriteString("const v")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" = map.get('key');\nprefetch(item);\nconst schema = 1;\nheaders.get('content-type');\n")
	}
	return []byte(b.String())
}

func BenchmarkServerBindings_LargeNegative(b *testing.B) {
	src := largeNegativeSource()
	b.Run("current", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if serverBindings(src) != nil {
				b.Fatal()
			}
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if len(baselineServerBindings(src)) != 0 {
				b.Fatal()
			}
		}
	})
}

func BenchmarkExtractHTTPClientFacts_LargeNegative(b *testing.B) {
	src := largeNegativeSource()
	b.Run("current", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if extractHTTPClientFacts(src, "src/generated.ts") != nil {
				b.Fatal()
			}
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if baselineExtractHTTPClientFacts(src, "src/generated.ts") != nil {
				b.Fatal()
			}
		}
	})
}

func BenchmarkExtractHTTPClientFacts_LargeDecoy(b *testing.B) {
	src := largeDecoySource()
	b.Run("current", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = extractHTTPClientFacts(src, "src/decoy.ts")
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = baselineExtractHTTPClientFacts(src, "src/decoy.ts")
		}
	})
}

func BenchmarkCollectRouterFile_LargeNegative(b *testing.B) {
	src := largeNegativeSource()
	known := map[string]bool{"src/generated.ts": true}
	b.Run("current", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if collectRouterFile(src, "src/generated.ts", nil, known) != nil {
				b.Fatal()
			}
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if baselineCollectRouterFile(src, "src/generated.ts", nil, known) != nil {
				b.Fatal()
			}
		}
	})
}

func BenchmarkPossibleGraphQLServerSignal_LargeNegative(b *testing.B) {
	src := largeNegativeSource()
	b.Run("current", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if possibleGraphQLServerSignal(src) {
				b.Fatal()
			}
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if baselinePossibleGraphQLServerSignal(src) {
				b.Fatal()
			}
		}
	})
}

func BenchmarkExtractGraphQLServerSDL_LargeNegative(b *testing.B) {
	src := largeNegativeSource()
	b.Run("current", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if extractGraphQLServerSDL(src, "src/generated.ts") != nil {
				b.Fatal()
			}
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if baselineExtractGraphQLServerSDL(src, "src/generated.ts") != nil {
				b.Fatal()
			}
		}
	})
}
