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
	"sort"
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

var serverRouteObjectCall = regexp.MustCompile(`([A-Za-z_$][\w$]*)\s*\.\s*route\s*\(\s*\{`)

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
	scopes := serverLexicalScopes(src)
	if len(bindings) == 0 && len(scopes) == 0 {
		return nil
	}
	dir := factpath.Dir(relFile)

	var out []facts.Fact
	seen := map[string]bool{}
	for _, m := range serverVerbCall.FindAllSubmatchIndex(src, -1) {
		recv := string(src[m[2]:m[3]])
		b, ok := serverReceiverAt(bindings, scopes, recv, m[0])
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
	out = append(out, extractServerRouteObjects(src, relFile, dir, bindings, scopes, seen)...)
	return out
}

func extractServerRouteObjects(src []byte, relFile, dir string, bindings map[string]serverBinding, scopes []fastifyParamScope, seen map[string]bool) []facts.Fact {
	if !bytes.Contains(src, []byte(".route")) {
		return nil
	}
	var out []facts.Fact
	for _, m := range serverRouteObjectCall.FindAllSubmatchIndex(src, -1) {
		recv := string(src[m[2]:m[3]])
		b, ok := serverReceiverAt(bindings, scopes, recv, m[0])
		if !ok || !b.mounted {
			continue
		}
		brace := m[1] - 1
		if brace < 0 || brace >= len(src) || src[brace] != '{' {
			continue
		}
		end, ok := matchObjectLiteral(src, brace)
		if !ok {
			continue
		}
		obj := src[brace : end+1]
		raw := objectLiteralStringField(obj, "url")
		if raw == "" {
			raw = objectLiteralStringField(obj, "path")
		}
		path, ok := cleanServerPath(raw)
		if !ok {
			continue
		}
		methods := objectLiteralMethods(obj)
		verb := joinRouteMethods(methods)
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

func matchObjectLiteral(src []byte, open int) (int, bool) {
	if open < 0 || open >= len(src) || src[open] != '{' {
		return 0, false
	}
	depth := 0
	var quote byte
	esc := false
	for i := open; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

func objectLiteralStringField(obj []byte, key string) string {
	re := regexp.MustCompile(`(?m)(?:^|[,{])\s*` + regexp.QuoteMeta(key) + `\s*:\s*(?:["']([^"']+)["']|` + "`" + `([^` + "`" + `]+)` + "`" + `)`)
	m := re.FindSubmatch(obj)
	if m == nil {
		return ""
	}
	return firstNonEmpty(m[1], m[2])
}

func objectLiteralMethods(obj []byte) []string {
	re := regexp.MustCompile(`(?m)(?:^|[,{])\s*method\s*:\s*`)
	loc := re.FindIndex(obj)
	if loc == nil {
		return nil
	}
	rest := obj[loc[1]:]
	rest = bytes.TrimSpace(rest)
	if len(rest) == 0 {
		return nil
	}
	if rest[0] == '[' {
		end := bytes.IndexByte(rest, ']')
		if end < 0 {
			return nil
		}
		var methods []string
		for _, m := range regexp.MustCompile(`["']([A-Za-z]+)["']`).FindAllSubmatch(rest[:end], -1) {
			methods = append(methods, strings.ToUpper(string(m[1])))
		}
		return methods
	}
	if rest[0] == '"' || rest[0] == '\'' || rest[0] == '`' {
		q := rest[0]
		j := 1
		for j < len(rest) && rest[j] != q {
			j++
		}
		if j < len(rest) {
			return []string{strings.ToUpper(string(rest[1:j]))}
		}
	}
	return nil
}

func joinRouteMethods(methods []string) string {
	if len(methods) == 0 {
		return "GET"
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range methods {
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	if len(out) == 1 {
		return out[0]
	}
	sort.Strings(out)
	return strings.Join(out, "|")
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

func isServerReceiverAt(bindings map[string]serverBinding, scopes []fastifyParamScope, name string, pos int) bool {
	if _, ok := fastifyScopeBinding(scopes, name, pos); ok {
		b, bound := serverReceiverAt(bindings, scopes, name, pos)
		return bound && b.mounted
	}
	return isServerReceiver(bindings, name)
}

func serverReceiverAt(bindings map[string]serverBinding, scopes []fastifyParamScope, name string, pos int) (serverBinding, bool) {
	if b, ok := fastifyScopeBinding(scopes, name, pos); ok {
		if !b.mounted {
			return serverBinding{}, false
		}
		return b, true
	}
	if b, ok := bindings[name]; ok {
		return b, true
	}
	return serverBinding{}, false
}

var (
	fastifyNamedImport = regexp.MustCompile(`(?m)import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+['"]fastify['"]`)
	fastifyTypedParam  = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*:\s*([A-Za-z_$][\w$]*)\b`)
)

var fastifyImportedTypes = map[string]bool{
	"FastifyInstance": true,
}

type fastifyParamScope struct {
	name       string
	start, end int
	binding    serverBinding
}

// typedFastifyParamScopes maps FastifyInstance parameters to the function body
// that owns them. FastifyPluginAsync/Callback name a plugin function, not an
// application object. The same identifier in a sibling function with another
// type is not a server receiver.
func typedFastifyParamScopes(src []byte) []fastifyParamScope {
	local := importedFastifyInstanceNames(src)
	if len(local) == 0 {
		return nil
	}
	mask := tsCommentStringMask(src)
	var out []fastifyParamScope
	for _, m := range fastifyTypedParam.FindAllSubmatchIndex(src, -1) {
		if mask[m[0]] {
			continue
		}
		ident, typ := string(src[m[2]:m[3]]), string(src[m[4]:m[5]])
		if !local[typ] {
			continue
		}
		bodyStart, bodyEnd, ok := functionBodyAroundParam(src, mask, m[0])
		if !ok {
			continue
		}
		out = append(out, fastifyParamScope{
			name: ident, start: bodyStart, end: bodyEnd,
			binding: serverBinding{framework: "fastify", mounted: true},
		})
	}
	return out
}

func importedFastifyInstanceNames(src []byte) map[string]bool {
	local := map[string]bool{}
	mask := tsCommentStringMask(src)
	for _, loc := range fastifyNamedImport.FindAllSubmatchIndex(src, -1) {
		if mask[loc[0]] {
			continue
		}
		inner := string(src[loc[2]:loc[3]])
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
	return local
}

func isFunctionParameter(src []byte, mask []bool, paramPos int) bool {
	i := paramPos
	for i > 0 && src[i] != '(' {
		if src[i] == ')' || src[i] == '{' || src[i] == '}' {
			return false
		}
		i--
	}
	if i < 0 || src[i] != '(' || mask[i] {
		return false
	}
	j := i - 1
	for j >= 0 && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
		j--
	}
	if j >= 0 && isJSIdentPart(src[j]) {
		end := j + 1
		for j >= 0 && isJSIdentPart(src[j]) {
			j--
		}
		name := string(src[j+1 : end])
		if name == "function" {
			return true
		}
		k := j
		for k >= 0 && (src[k] == ' ' || src[k] == '\t' || src[k] == '\n' || src[k] == '\r') {
			k--
		}
		if k >= 7 && string(src[k-7:k+1]) == "function" {
			return true
		}
	}
	depth := 0
	for k := i; k < len(src); k++ {
		if mask[k] {
			continue
		}
		switch src[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				p := k + 1
				for p < len(src) && (src[p] == ' ' || src[p] == '\t' || src[p] == '\n' || src[p] == '\r') {
					p++
				}
				if p+1 < len(src) && src[p] == '=' && src[p+1] == '>' {
					return true
				}
				return false
			}
		}
	}
	return false
}

func functionBodyAroundParam(src []byte, mask []bool, paramPos int) (start, end int, ok bool) {
	i := paramPos
	for i > 0 && src[i] != '(' {
		if src[i] == ')' || src[i] == '{' || src[i] == '}' {
			return 0, 0, false
		}
		i--
	}
	if i < 0 || src[i] != '(' || mask[i] {
		return 0, 0, false
	}
	depth := 0
	for j := i; j < len(src); j++ {
		if mask[j] {
			continue
		}
		switch src[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				k := j + 1
				for k < len(src) && (src[k] == ' ' || src[k] == '\t' || src[k] == '\n' || src[k] == '\r') {
					k++
				}
				for k < len(src) && src[k] != '{' && src[k] != ';' && src[k] != '\n' {
					if src[k] == '=' && k+1 < len(src) && src[k+1] == '>' {
						k += 2
						for k < len(src) && (src[k] == ' ' || src[k] == '\t' || src[k] == '\n' || src[k] == '\r') {
							k++
						}
						break
					}
					k++
				}
				for k < len(src) && (src[k] == ' ' || src[k] == '\t' || src[k] == '\n' || src[k] == '\r') {
					k++
				}
				if k >= len(src) || src[k] != '{' || mask[k] {
					return 0, 0, false
				}
				end := matchBrace(src, mask, k)
				if end < 0 {
					return 0, 0, false
				}
				return k, end, true
			}
		}
	}
	return 0, 0, false
}

func matchBrace(src []byte, mask []bool, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		if mask[i] {
			continue
		}
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func fastifyScopeBinding(scopes []fastifyParamScope, name string, pos int) (serverBinding, bool) {
	best := -1
	bestSpan := int(^uint(0) >> 1)
	for i, s := range scopes {
		if s.name != name || pos < s.start || pos > s.end {
			continue
		}
		span := s.end - s.start
		if span < bestSpan || (span == bestSpan && best >= 0 && s.binding.mounted && !scopes[best].binding.mounted) {
			bestSpan = span
			best = i
		}
	}
	if best < 0 {
		return serverBinding{}, false
	}
	return scopes[best].binding, true
}

func mergeFastifyScopes(typed, params []fastifyParamScope) []fastifyParamScope {
	if len(typed) == 0 {
		return params
	}
	if len(params) == 0 {
		return typed
	}
	out := make([]fastifyParamScope, 0, len(typed)+len(params))
	out = append(out, typed...)
	out = append(out, params...)
	return out
}

func serverLexicalScopes(src []byte) []fastifyParamScope {
	return mergeFastifyScopes(
		mergeFastifyScopes(typedFastifyParamScopes(src), collectParamNameScopes(src)),
		collectLocalBindingScopes(src),
	)
}

var localVarBinding = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*(?::[^=;]*)?=\s*`)

var localFactoryRHS = regexp.MustCompile(`^(?:new\s+)?(express|fastify|Fastify|Hono|Koa)\s*\(`)
var localRouterRHS = regexp.MustCompile(`^(?:new\s+)?(?:express\s*\.\s*Router|Router)\s*\(`)

func collectLocalBindingScopes(src []byte) []fastifyParamScope {
	mask := tsCommentStringMask(src)
	var out []fastifyParamScope
	for _, m := range localVarBinding.FindAllSubmatchIndex(src, -1) {
		if mask[m[0]] {
			continue
		}
		name := string(src[m[2]:m[3]])
		rhs := m[1]
		end := enclosingBlockEnd(src, mask, m[0])
		if end < rhs {
			continue
		}
		rest := src[rhs:]
		if localRouterRHS.Match(rest) {
			continue
		}
		b := serverBinding{}
		if fm := localFactoryRHS.FindSubmatch(rest); fm != nil {
			b = serverBinding{framework: frameworkOf[string(fm[1])], mounted: true}
		} else {
			i := 0
			for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' || rest[i] == '\r') {
				i++
			}
			if i >= len(rest) || rest[i] != '{' {
				continue
			}
		}
		out = append(out, fastifyParamScope{
			name:    name,
			start:   rhs,
			end:     end,
			binding: b,
		})
	}
	return out
}

func enclosingBlockEnd(src []byte, mask []bool, pos int) int {
	var opens []int
	for i := 0; i < pos && i < len(src); i++ {
		if mask[i] {
			continue
		}
		switch src[i] {
		case '{':
			opens = append(opens, i)
		case '}':
			if len(opens) > 0 {
				opens = opens[:len(opens)-1]
			}
		}
	}
	if len(opens) == 0 {
		return len(src)
	}
	end := matchBrace(src, mask, opens[len(opens)-1])
	if end < 0 {
		return len(src)
	}
	return end
}

func collectParamNameScopes(src []byte) []fastifyParamScope {
	mask := tsCommentStringMask(src)
	var out []fastifyParamScope
	i := 0
	for i < len(src) {
		if mask[i] {
			i++
			continue
		}
		if !isJSIdentStart(src[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(src) && isJSIdentPart(src[i]) {
			i++
		}
		j := start - 1
		for j >= 0 && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
			j--
		}
		if j < 0 || (src[j] != '(' && src[j] != ',') {
			continue
		}
		if !isFunctionParameter(src, mask, start) {
			continue
		}
		bodyStart, bodyEnd, ok := functionBodyAroundParam(src, mask, start)
		if !ok {
			continue
		}
		out = append(out, fastifyParamScope{
			name:  string(src[start:i]),
			start: bodyStart,
			end:   bodyEnd,
		})
	}
	return out
}

func isJSIdentStart(b byte) bool {
	return b == '_' || b == '$' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isJSIdentPart(b byte) bool {
	return isJSIdentStart(b) || (b >= '0' && b <= '9')
}

// tsCommentStringMask is true at bytes inside comments or string/template literals.
func tsCommentStringMask(src []byte) []bool {
	mask := make([]bool, len(src))
	i := 0
	for i < len(src) {
		switch src[i] {
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					mask[i] = true
					i++
				}
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				mask[i] = true
				mask[i+1] = true
				i += 2
				for i < len(src) {
					mask[i] = true
					if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
						mask[i+1] = true
						i += 2
						break
					}
					i++
				}
				continue
			}
		case '\'', '"', '`':
			q := src[i]
			mask[i] = true
			i++
			for i < len(src) {
				mask[i] = true
				if src[i] == '\\' && i+1 < len(src) {
					mask[i+1] = true
					i += 2
					continue
				}
				if src[i] == q {
					i++
					break
				}
				i++
			}
			continue
		}
		i++
	}
	return mask
}

// typedFastifyParamBindings is the union of scoped FastifyInstance parameters,
// kept for tests that inspect the file-level name set. It must not be used as
// a global receiver map.
func typedFastifyParamBindings(src []byte) map[string]serverBinding {
	out := map[string]serverBinding{}
	for _, s := range typedFastifyParamScopes(src) {
		out[s.name] = s.binding
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
