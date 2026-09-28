package fsm

import (
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type stateDecl struct {
	tag, path, parent string
	file              string
	line, endLine     int
}

type tagDecl struct {
	tag, file     string
	line, endLine int
}

func (a *Analyzer) resolveType(from, name string) (string, string) {
	if name == "" {
		return "", ""
	}
	if a.hasType(from, name) {
		return from, name
	}
	resolved, ok := a.resolvedImport(from, name)
	if !ok {
		return "", ""
	}
	module, export, _ := strings.Cut(resolved, "#")
	return module, export
}

func (a *Analyzer) hasType(file, name string) bool {
	src := a.source(file)
	if len(src) == 0 {
		return false
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return false
	}
	defer done()
	return typeDeclaration(root, name, src, kinds) != nil
}

func typeDeclaration(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	var found *sitter.Node
	walk(root, func(n *sitter.Node) {
		if found != nil {
			return
		}
		if kinds.Of(n) != "type_alias_declaration" && kinds.Of(n) != "interface_declaration" {
			return
		}
		if text(n.ChildByFieldName("name"), src) == name {
			found = n
		}
	})
	return found
}

func typeBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if v := n.ChildByFieldName("value"); v != nil {
		return v
	}
	return n.ChildByFieldName("body")
}

func typeAnnotationValue(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if t := n.ChildByFieldName("type"); t != nil {
		return t
	}
	for _, c := range namedChildren(n) {
		if c != nil {
			return c
		}
	}
	return nil
}

func (a *Analyzer) collectStateType(file, name, prefix string, seen map[string]bool) []stateDecl {
	key := slash(file) + "#" + name + "@" + prefix
	if seen[key] {
		return nil
	}
	seen[key] = true
	src := a.source(file)
	if len(src) == 0 {
		return nil
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return nil
	}
	defer done()
	decl := typeDeclaration(root, name, src, kinds)
	if decl == nil {
		return nil
	}
	var out []stateDecl
	var addType func(*sitter.Node, string, string)
	addType = func(n *sitter.Node, cur, parent string) {
		if n == nil {
			return
		}
		k := kinds.Of(n)
		switch k {
		case "string", "literal_type":
			var tag string
			if s, ok := stringValue(n, src, kinds); ok {
				tag = s
			} else {
				walk(n, func(c *sitter.Node) {
					if tag == "" && kinds.Of(c) == "string" {
						tag, _ = stringValue(c, src, kinds)
					}
				})
			}
			if tag != "" {
				path := tag
				if cur != "" {
					path = cur + "." + tag
				}
				out = append(out, stateDecl{tag: tag, path: path, parent: parent, file: file, line: nodeLine(n), endLine: int(n.EndPosition().Row) + 1})
			}
		case "type_identifier":
			alias := text(n, src)
			if typeDeclaration(root, alias, src, kinds) != nil {
				out = append(out, a.collectStateType(file, alias, cur, seen)...)
			} else if target, exportName := a.resolveType(file, alias); target != "" {
				out = append(out, a.collectStateType(target, exportName, cur, seen)...)
			}
		case "object_type":
			for _, p := range namedChildren(n) {
				if kinds.Of(p) != "property_signature" {
					continue
				}
				prop := text(p.ChildByFieldName("name"), src)
				if prop == "" {
					continue
				}
				path := prop
				if cur != "" {
					path = cur + "." + prop
				}
				// Parent regions are explicit nodes, so hierarchy can be followed
				// without inferring containment from a dotted display name.
				out = append(out, stateDecl{tag: prop, path: path, parent: cur, file: file, line: nodeLine(p), endLine: int(p.EndPosition().Row) + 1})
				addType(typeAnnotationValue(p.ChildByFieldName("type")), path, path)
			}
		case "union_type", "intersection_type", "parenthesized_type", "type_annotation":
			for _, c := range namedChildren(n) {
				addType(c, cur, parent)
			}
		case "generic_type":
			// Effect's Data.TaggedEnum<{ Queued: ..., Running: ... }> is a
			// tagged union whose top-level property names are the state tags.
			// Its property payloads are data, not nested states.
			if strings.Contains(text(n, src), "TaggedEnum<") {
				args := n.ChildByFieldName("type_arguments")
				var object *sitter.Node
				walk(args, func(c *sitter.Node) {
					if object == nil && kinds.Of(c) == "object_type" {
						object = c
					}
				})
				for _, p := range namedChildren(object) {
					if kinds.Of(p) != "property_signature" {
						continue
					}
					tag := text(p.ChildByFieldName("name"), src)
					if tag == "" {
						continue
					}
					path := tag
					if cur != "" {
						path = cur + "." + tag
					}
					out = append(out, stateDecl{tag: tag, path: path, parent: parent, file: file, line: nodeLine(p), endLine: int(p.EndPosition().Row) + 1})
				}
				return
			}
			for _, c := range namedChildren(n.ChildByFieldName("type_arguments")) {
				addType(c, cur, parent)
			}
		case "type_arguments":
			for _, c := range namedChildren(n) {
				addType(c, cur, parent)
			}
		default:
			if k == "type_alias_declaration" || k == "interface_declaration" {
				addType(typeBody(n), cur, parent)
			}
		}
	}
	addType(typeBody(decl), prefix, "")
	return out
}

func (a *Analyzer) collectTaggedType(file, name, prop string, seen map[string]bool) []tagDecl {
	key := slash(file) + "#" + name + "." + prop
	if seen[key] {
		return nil
	}
	seen[key] = true
	src := a.source(file)
	if len(src) == 0 {
		return nil
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return nil
	}
	defer done()
	decl := typeDeclaration(root, name, src, kinds)
	if decl == nil {
		return nil
	}
	var out []tagDecl
	var scan func(*sitter.Node)
	scan = func(n *sitter.Node) {
		if n == nil {
			return
		}
		k := kinds.Of(n)
		if k == "generic_type" && strings.Contains(text(n, src), "TaggedEnum<") {
			args := n.ChildByFieldName("type_arguments")
			var object *sitter.Node
			walk(args, func(c *sitter.Node) {
				if object == nil && kinds.Of(c) == "object_type" {
					object = c
				}
			})
			for _, p := range namedChildren(object) {
				if kinds.Of(p) == "property_signature" {
					if tag := text(p.ChildByFieldName("name"), src); tag != "" {
						out = append(out, tagDecl{tag: tag, file: file, line: nodeLine(p), endLine: int(p.EndPosition().Row) + 1})
					}
				}
			}
			return
		}
		if k == "property_signature" && text(n.ChildByFieldName("name"), src) == prop {
			before := len(out)
			var collectStrings func(*sitter.Node)
			collectStrings = func(t *sitter.Node) {
				if t == nil {
					return
				}
				if kinds.Of(t) == "string" {
					if v, ok := stringValue(t, src, kinds); ok {
						out = append(out, tagDecl{tag: v, file: file, line: nodeLine(t), endLine: int(t.EndPosition().Row) + 1})
					}
					return
				}
				for _, c := range namedChildren(t) {
					collectStrings(c)
				}
			}
			typeNode := typeAnnotationValue(n.ChildByFieldName("type"))
			collectStrings(typeNode)
			if len(out) == before {
				// A tagged event can derive its discriminator from a local literal
				// tuple, e.g. `type Action = (typeof actionTypes)[number]`.
				// Follow only that statically literal form; variables or computed
				// arrays remain unresolved.
				scan(typeNode)
			}
			return
		}
		if arrayName := indexedLiteralArrayName(text(n, src)); arrayName != "" {
			value := variableValue(root, arrayName, src, kinds)
			if value == nil || kinds.Of(unwrapExpression(value, kinds)) != "array" {
				return
			}
			for _, item := range namedChildren(unwrapExpression(value, kinds)) {
				if tag, ok := stringValue(item, src, kinds); ok {
					out = append(out, tagDecl{tag: tag, file: file, line: nodeLine(item), endLine: int(item.EndPosition().Row) + 1})
				}
			}
			return
		}
		if k == "type_identifier" {
			alias := text(n, src)
			if typeDeclaration(root, alias, src, kinds) != nil {
				out = append(out, a.collectTaggedType(file, alias, prop, seen)...)
			} else if target, exported := a.resolveType(file, alias); target != "" {
				out = append(out, a.collectTaggedType(target, exported, prop, seen)...)
			}
			return
		}
		for _, c := range namedChildren(n) {
			scan(c)
		}
	}
	scan(typeBody(decl))
	seenTags := map[string]bool{}
	uniqueTags := make([]tagDecl, 0, len(out))
	for _, declaration := range out {
		if declaration.tag != "" && !seenTags[declaration.tag] {
			seenTags[declaration.tag] = true
			uniqueTags = append(uniqueTags, declaration)
		}
	}
	return uniqueTags
}

func indexedLiteralArrayName(typeExpr string) string {
	if !strings.Contains(typeExpr, "[number]") {
		return ""
	}
	i := strings.Index(typeExpr, "typeof")
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(typeExpr[i+len("typeof"):])
	if rest == "" || !isIdentifierStart(rest[0]) {
		return ""
	}
	end := 1
	for end < len(rest) && isIdentifierPart(rest[end]) {
		end++
	}
	name := rest[:end]
	if strings.Contains(rest[end:], ")[number]") {
		return name
	}
	return ""
}

func isIdentifierStart(b byte) bool {
	return b == '_' || b == '$' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isIdentifierPart(b byte) bool { return isIdentifierStart(b) || b >= '0' && b <= '9' }

func (a *Analyzer) entryFacts(m *machineModel, root *sitter.Node, src []byte, kinds *tsutil.KindTable) []facts.Fact {
	if m.spec.Entry == "" {
		return nil
	}
	body := functionBody(root, m.spec.Entry, src, kinds)
	if body == nil {
		m.partial = true
		m.coverage["entry_status"] = "unknown"
		return nil
	}
	var entries []facts.Relation
	unresolved := 0
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" || calleeName(n, src, kinds) != "entryResult" {
			return
		}
		path := branchConditions(n, body, "", "value", m.spec.Reject, src, kinds)
		state := entryStateFromPath(path)
		if state == "" {
			unresolved++
			return
		}
		args := callArguments(n, kinds)
		if len(args) < 2 {
			unresolved++
			return
		}
		for _, command := range effectCommands(args[1], m.commands, src, kinds) {
			entries = append(entries, relation(facts.RelFSMEntryEmits, machineMember(m.machine, "command", command)))
			addStateRelation(m, state, relation(facts.RelFSMEntryEmits, machineMember(m.machine, "command", command)))
		}
	})
	if unresolved > 0 {
		m.coverage["unresolved_entry_effects"] = unresolved
		m.partial = true
	}
	if len(entries) > 0 {
		m.coverage["entry_status"] = "partial"
	} else if unresolved == 0 {
		m.coverage["entry_status"] = "none_detected"
	}
	return nil
}

func addStateRelation(m *machineModel, path string, rel facts.Relation) {
	f, ok := m.states[path]
	if !ok {
		return
	}
	f.Relations = append(f.Relations, rel)
	m.states[path] = f
	for i := range m.facts {
		if m.facts[i].Kind == facts.KindFSMState && m.facts[i].Name == f.Name {
			m.facts[i] = f
			return
		}
	}
}

func addCommandRelation(m *machineModel, tag string, rel facts.Relation) {
	f, ok := m.commands[tag]
	if !ok {
		return
	}
	for _, existing := range f.Relations {
		if existing.Kind == rel.Kind && existing.Target == rel.Target && existing.TargetFile == rel.TargetFile {
			return
		}
	}
	f.Relations = append(f.Relations, rel)
	m.commands[tag] = f
	for i := range m.facts {
		if m.facts[i].Kind == facts.KindFSMCommand && m.facts[i].Name == f.Name {
			m.facts[i] = f
			return
		}
	}
}

func entryStateFromPath(path []pathCondition) string {
	for _, c := range path {
		text := c.Text
		if strings.Contains(text, ".matches") {
			continue
		}
		for _, key := range []string{"forgotPassword", "getStarted", "firstSession", "authenticating", "commandCenter"} {
			needle := "value." + key + " ==="
			if strings.Contains(text, needle) {
				matches := stringLiteralRE.FindAllStringSubmatch(text, -1)
				if len(matches) > 0 {
					return key + "." + matches[len(matches)-1][1]
				}
			}
		}
	}
	return ""
}

func firstStateTarget(body *sitter.Node, enter string, src []byte, kinds *tsutil.KindTable) string {
	var target string
	walk(body, func(n *sitter.Node) {
		if target != "" || kinds.Of(n) != "call_expression" || calleeName(n, src, kinds) != enter {
			return
		}
		args := callArguments(n, kinds)
		if len(args) > 0 {
			target = stateValue(args[0], src, kinds)
		}
	})
	return target
}
