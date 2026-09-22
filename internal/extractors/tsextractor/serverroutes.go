// Server-side routes declared by CALL, rather than by decorator: Express, Fastify,
// Hono, Koa/Oak. The shape `<recv>.<verb>('/path', handler)` is common to all of
// them, so one pass covers the family.
//
// The whole difficulty here is that the shape is ALSO v141's client shape:
// `axios.get('/x')` and `router.get('/x')` are the same text, and the client pass
// already claims it. Disambiguation is therefore by RECEIVER BINDING, resolved
// within the file, and the default is deliberately "client" — an unknown receiver
// keeps exactly the v141 behaviour rather than being silently reclassified.
package tsextractor

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
)

// The four binding forms. ES-module and CommonJS are both first-class here: a Node
// server is as likely to be written `const app = require('express')()` as
// `import express from "express"; const app = express()`, and matching only the
// former found zero routes on the one real Express server in the corpus.

// appFactory binds an identifier to a whole application object — the ROOT of a route
// tree, so its routes are served at the path as written. ESM form.
var appFactory = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=;]*)?=\s*(?:new\s+)?(express|fastify|Fastify|Hono|Koa)\s*\(`)

// appFactoryRequire is the same binding written CommonJS-style:
// `const app = require('express')()`.
var appFactoryRequire = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=;]*)?=\s*require\(\s*['"](express|fastify|koa|hono)['"]\s*\)\s*\(`)

// routerFactory binds an identifier to a sub-router — a fragment MOUNTED somewhere,
// so its own paths are relative and mean nothing until the mount point is known.
var routerFactory = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=;]*)?=\s*(?:new\s+)?(?:express\s*\.\s*Router|Router)\s*\(`)

// routerFactoryRequire: `const router = require('express').Router()`.
var routerFactoryRequire = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=;]*)?=\s*require\(\s*['"]express['"]\s*\)\s*\.\s*Router\s*\(`)

// mountCall matches `app.use('/prefix', router)` — the statement that gives a
// sub-router its place in the tree.
var mountCall = regexp.MustCompile("([A-Za-z_$][\\w$]*)\\s*\\.\\s*use\\s*\\(\\s*(?:\"([^\"]*)\"|'([^']*)'|`([^`]*)`)\\s*,\\s*([A-Za-z_$][\\w$]*)")

// serverVerbCall matches a route registration on a named receiver, capturing the
// receiver so the binding table can rule on it.
var serverVerbCall = regexp.MustCompile("([A-Za-z_$][\\w$]*)\\s*\\.\\s*(get|post|put|patch|delete|all|options|head)\\s*(?:<[^()]*>)?\\s*\\(\\s*(?:\"([^\"]*)\"|'([^']*)'|`([^`]*)`)")

// frameworkOf normalises a factory token to the framework label emitted on facts.
var frameworkOf = map[string]string{
	"express": "express", "fastify": "fastify", "Fastify": "fastify",
	"Hono": "hono", "hono": "hono", "Koa": "koa", "koa": "koa",
}

// serverBinding is what an identifier in this file was bound to.
type serverBinding struct {
	framework string
	isRouter  bool   // a mounted fragment rather than an application root
	prefix    string // mount path, for a router mounted in THIS file
	mounted   bool   // whether a mount point is known at all
}

// Distinctive literals each factory regex requires. A file without them cannot
// produce a binding, so the NFA never runs. Tokens are case-sensitive and match
// the factory patterns exactly; extra hits only mean the regex still runs.
var (
	tokExpress    = []byte("express")
	tokFastify    = []byte("fastify")
	tokFastifyCap = []byte("Fastify")
	tokHono       = []byte("Hono")
	tokHonoLow    = []byte("hono")
	tokKoa        = []byte("Koa")
	tokKoaLow     = []byte("koa")
	tokRouter     = []byte("Router")
)

func possibleAppFactory(src []byte) bool {
	return containsAny(src, tokExpress, tokFastify, tokFastifyCap, tokHono, tokHonoLow, tokKoa, tokKoaLow)
}

func possibleRouterFactory(src []byte) bool {
	return bytes.Contains(src, tokRouter)
}

func possibleServerBinding(src []byte) bool {
	return possibleAppFactory(src) || possibleRouterFactory(src)
}

// serverBindings maps identifier -> binding for every app/router constructed in this
// file, with same-file mounts already resolved.
func serverBindings(src []byte) map[string]serverBinding {
	if !possibleServerBinding(src) {
		return nil
	}
	out := map[string]serverBinding{}
	if possibleAppFactory(src) {
		for _, re := range []*regexp.Regexp{appFactory, appFactoryRequire} {
			for _, m := range re.FindAllSubmatch(src, -1) {
				out[string(m[1])] = serverBinding{framework: frameworkOf[string(m[2])], mounted: true}
			}
		}
	}
	if possibleRouterFactory(src) {
		for _, re := range []*regexp.Regexp{routerFactory, routerFactoryRequire} {
			for _, m := range re.FindAllSubmatch(src, -1) {
				name := string(m[1])
				if _, taken := out[name]; taken {
					continue // an app binding of the same name wins; do not downgrade it
				}
				out[name] = serverBinding{framework: "express", isRouter: true}
			}
		}
	}
	for name, b := range typedFastifyParamBindings(src) {
		if _, taken := out[name]; taken {
			continue
		}
		out[name] = b
	}
	if len(out) == 0 {
		return nil
	}
	// Resolve mounts declared in this file: app.use('/webhooks', router).
	if hasDotKeyword(src, []byte("use")) {
		for _, m := range mountCall.FindAllSubmatch(src, -1) {
			prefix := firstNonEmpty(m[2], m[3], m[4])
			child := string(m[5])
			b, ok := out[child]
			if !ok || !b.isRouter || b.mounted {
				continue
			}
			// Only a parent whose OWN mount point is known can give one to a child.
			// Mounting onto an unmounted sub-router — `apiRouter.use('/orders', orders)`
			// in a file that does not itself mount apiRouter — yields the fragment
			// "/orders", which is exactly the wrong fact this pass exists to avoid: the
			// route really serves "/api/orders". The child stays unmounted here and
			// routermount.go composes it once both mounts are visible.
			if parent, ok := out[string(m[1])]; ok && parent.mounted {
				b.prefix = facts.JoinRoutePath(parent.prefix, prefix)
				b.mounted = true
				b.framework = parent.framework
				out[child] = b
			}
		}
	}
	return out
}

// extractServerRouteFacts emits a server-role route for every call-registered route
// whose receiver is a known application or a router with a known mount point.
//
// A router whose mount is NOT visible in this file emits NOTHING, on purpose. Its
// declared path is a fragment: `router.post('/login')` in a routes module mounted at
// '/webhooks' elsewhere serves '/webhooks/login', so emitting '/login' would be a wrong
// fact rather than a missing one — and a wrong path can false-match another repo's
// route, which is worse than silence. Cross-file mount resolution is a repo-wide pass
// (the shape of goextractor/routeprefix.go); it is deliberately not attempted here.
func extractServerRouteFacts(src []byte, relFile string) []facts.Fact {
	bindings := serverBindings(src)
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

// cleanServerPath accepts a declared route path, rejecting the ones that carry no
// architectural signal: a non-rooted literal, and a bare catch-all. `app.get('*')` is
// a SPA fallback rather than an endpoint, and indexing it would let it match any
// client path at all.
func cleanServerPath(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	if trimmed := strings.Trim(p, "/*"); trimmed == "" {
		return "", false
	}
	return p, true
}

// isServerReceiver reports whether an identifier in this file is bound to an
// application or router, so the CLIENT pass can leave its calls alone. Without this,
// `router.get('/x')` would be emitted twice — once as a server route here and once as
// an outbound client call by v141's lowerVerbCall.
func isServerReceiver(bindings map[string]serverBinding, name string) bool {
	_, ok := bindings[name]
	return ok
}

var (
	fastifyNamedImport = regexp.MustCompile(`(?m)import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+['"]fastify['"]`)
	fastifyTypedParam  = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*:\s*([A-Za-z_$][\w$]*)\b`)
)

var fastifyImportedTypes = map[string]bool{
	"FastifyInstance":       true,
	"FastifyPluginAsync":    true,
	"FastifyPluginCallback": true,
}

// typedFastifyParamBindings maps parameter identifiers whose type is a name
// imported from the `fastify` package (including `import type` and `as` aliases).
// It does not infer from the identifier `app`. A factory binding of the same
// name stays authoritative so an in-file Fastify() construction is not replaced.
func typedFastifyParamBindings(src []byte) map[string]serverBinding {
	local := map[string]bool{}
	for _, m := range fastifyNamedImport.FindAllSubmatch(src, -1) {
		for _, spec := range strings.Split(string(m[1]), ",") {
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
			if !fastifyImportedTypes[name] {
				continue
			}
			if alias != "" {
				local[alias] = true
			} else {
				local[name] = true
			}
		}
	}
	if len(local) == 0 {
		return nil
	}
	out := map[string]serverBinding{}
	for _, m := range fastifyTypedParam.FindAllSubmatch(src, -1) {
		ident, typ := string(m[1]), string(m[2])
		if !local[typ] {
			continue
		}
		out[ident] = serverBinding{framework: "fastify", mounted: true}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// identifierEndingAt returns the identifier immediately preceding pos, or "".
func identifierEndingAt(src []byte, pos int) string {
	end := pos
	i := pos
	for i > 0 && isIdentByte(src[i-1]) {
		i--
	}
	if i == end {
		return ""
	}
	return string(src[i:end])
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func containsAny(src []byte, tokens ...[]byte) bool {
	for _, tok := range tokens {
		if bytes.Contains(src, tok) {
			return true
		}
	}
	return false
}

// hasDotKeyword reports whether src contains `.` + optional whitespace + kw, the
// necessary shape of `<recv>.<kw>(...)` with the whitespace the mount/verb
// regexes allow between the dot and the name.
func hasDotKeyword(src, kw []byte) bool {
	if len(kw) == 0 {
		return false
	}
	for i := 0; i < len(src); {
		j := bytes.IndexByte(src[i:], '.')
		if j < 0 {
			return false
		}
		i += j + 1
		for i < len(src) && isASCIISpace(src[i]) {
			i++
		}
		if bytes.HasPrefix(src[i:], kw) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...[]byte) string {
	for _, v := range vals {
		if len(v) > 0 {
			return string(v)
		}
	}
	return ""
}

// nodeSlice returns capture group g of a FindAllSubmatchIndex match.
func nodeSlice(src []byte, m []int, g int) string {
	s, e := m[2*g], m[2*g+1]
	if s < 0 || e <= s {
		return ""
	}
	return string(src[s:e])
}
