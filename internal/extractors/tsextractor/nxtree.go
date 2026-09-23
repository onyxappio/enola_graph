package tsextractor

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/factpath"
)

// Proven Nx Tree bindings for HTTP-client suppression. Name-only "tree"
// identifiers, an @nx/devkit import with no typed binding, and sibling
// .write/.delete evidence are not sufficient. Unknown HTTP receivers named
// tree still emit client routes.

type nxTreeScope struct {
	name       string
	start, end int
	isTree     bool
}

var (
	nxDevkitNamedImport = regexp.MustCompile(`(?m)import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+['"]@nx/devkit['"]`)
	relativeNamedImport = regexp.MustCompile(`(?m)import\s+(?:type\s+)?\{([^}]+)\}\s+from\s+['"](\.[^'"]+)['"]`)
	typedIdentDecl      = regexp.MustCompile(`\b([A-Za-z_$][\w$]*)\s*:\s*([A-Za-z_$][\w$]*)\b`)
	objectDestructure   = regexp.MustCompile(`(?:const|let|var)\s*\{([^}]+)\}\s*=\s*([A-Za-z_$][\w$]*)`)
	plainIdentBinding   = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\b`)
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
			body := src[open+1 : end]
			for _, fm := range schemaField.FindAllSubmatch(body, -1) {
				field, typ := string(fm[1]), string(fm[2])
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

func nxTreeScopes(src []byte, relFile string, knownFiles map[string]bool, readSrc func(string) []byte, sideReads map[string]bool) []nxTreeScope {
	treeTypes := importedNxTreeNames(src)
	schemaFields := schemaNxTreeFields(src, relFile, knownFiles, readSrc, sideReads)
	if len(treeTypes) == 0 && len(schemaFields) == 0 {
		return nil
	}
	mask := tsCommentStringMask(src)
	var scopes []nxTreeScope

	add := func(name string, start, end int, isTree bool) {
		if name == "" || start < 0 || end <= start {
			return
		}
		scopes = append(scopes, nxTreeScope{name: name, start: start, end: end, isTree: isTree})
	}

	typedParams := map[string][]fastifyParamScope{} // param name -> typed schema/tree scopes
	for _, m := range typedIdentDecl.FindAllSubmatchIndex(src, -1) {
		if mask[m[0]] {
			continue
		}
		ident, typ := string(src[m[2]:m[3]]), string(src[m[4]:m[5]])
		isTree := treeTypes[typ]
		_, isSchema := schemaFields[typ]
		if !isTree && !isSchema {
			continue
		}
		if !isFunctionParameter(src, mask, m[0]) && !looksLikeTypedConst(src, mask, m[0]) {
			// Still allow function params via functionBodyAroundParam.
		}
		bodyStart, bodyEnd, ok := functionBodyAroundParam(src, mask, m[0])
		if !ok {
			// typed const: bind from the declaration to the enclosing block
			bodyStart, bodyEnd = enclosingBlock(src, mask, m[0])
			if bodyEnd <= bodyStart {
				continue
			}
		}
		if isTree {
			add(ident, bodyStart, bodyEnd, true)
		}
		typedParams[ident] = append(typedParams[ident], fastifyParamScope{
			name: ident, start: bodyStart, end: bodyEnd,
		})
		if fields, ok := schemaFields[typ]; ok {
			// Remember schema-typed params for destructure matching below.
			_ = fields
			typedParams[ident+"\x00"+typ] = append(typedParams[ident+"\x00"+typ], fastifyParamScope{
				name: ident, start: bodyStart, end: bodyEnd,
			})
		}
	}

	for _, m := range objectDestructure.FindAllSubmatchIndex(src, -1) {
		if mask[m[0]] {
			continue
		}
		fields := string(src[m[2]:m[3]])
		srcName := string(src[m[4]:m[5]])
		blockStart, blockEnd := enclosingBlock(src, mask, m[0])
		for _, entry := range strings.Split(fields, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			orig, local := entry, entry
			if parts := strings.Split(entry, ":"); len(parts) == 2 {
				orig = strings.TrimSpace(parts[0])
				local = strings.TrimSpace(parts[1])
			}
			orig = strings.TrimPrefix(orig, "...")
			if orig == "" || local == "" {
				continue
			}
			proven := false
			for typ, treeFields := range schemaFields {
				if !treeFields[orig] {
					continue
				}
				for _, sc := range typedParams[srcName+"\x00"+typ] {
					if m[0] >= sc.start && m[0] <= sc.end {
						proven = true
						break
					}
				}
				if proven {
					break
				}
			}
			add(local, m[0], blockEnd, proven)
			if !proven && blockEnd > blockStart {
				// shadowing of a proven tree name
				add(local, m[0], blockEnd, false)
			}
		}
	}

	for _, m := range plainIdentBinding.FindAllSubmatchIndex(src, -1) {
		if mask[m[0]] {
			continue
		}
		name := string(src[m[2]:m[3]])
		// Skip typed Tree consts already recorded.
		after := m[3]
		for after < len(src) && (src[after] == ' ' || src[after] == '\t') {
			after++
		}
		if after < len(src) && src[after] == ':' {
			continue
		}
		_, blockEnd := enclosingBlock(src, mask, m[0])
		add(name, m[0], blockEnd, false)
	}
	return scopes
}

func looksLikeTypedConst(src []byte, mask []bool, pos int) bool {
	i := pos
	for i > 0 && (src[i] == ' ' || src[i] == '\t') {
		i--
	}
	return i >= 0 && !mask[i]
}

func enclosingBlock(src []byte, mask []bool, pos int) (start, end int) {
	depth := 0
	start = 0
	for i := pos; i >= 0; i-- {
		if mask[i] {
			continue
		}
		switch src[i] {
		case '}':
			depth++
		case '{':
			if depth == 0 {
				end := matchBrace(src, mask, i)
				if end >= 0 {
					return i, end
				}
				return 0, len(src)
			}
			depth--
		}
	}
	return 0, len(src)
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
