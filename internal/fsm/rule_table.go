package fsm

import (
	"fmt"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (a *Analyzer) buildRuleTable(m *machineModel, root *sitter.Node, kinds *tsutil.KindTable, src []byte) {
	spec := m.spec
	regBody := functionBody(root, spec.Registration, src, kinds)
	if regBody == nil {
		m.partial = true
		m.coverage["coverage_status"] = "unknown"
		m.coverage["admission_status"] = "unresolved_registration"
		m.facts = append(m.facts, coverageFact(m, map[string]any{"coverage_status": "unknown", "admission_status": "unresolved_registration"}))
		return
	}
	var definitionCall *sitter.Node
	walk(regBody, func(n *sitter.Node) {
		if definitionCall != nil || kinds.Of(n) != "call_expression" {
			return
		}
		local := calleeName(n, src, kinds)
		resolved, ok := a.resolvedImport(m.file, local)
		if ok && a.matchesSymbolRef(resolved, spec.Factory) {
			definitionCall = n
		}
	})
	if definitionCall == nil {
		m.partial = true
		m.coverage["coverage_status"] = "unknown"
		m.coverage["admission_status"] = "factory_not_resolved"
		m.facts = append(m.facts, coverageFact(m, map[string]any{"coverage_status": "unknown", "admission_status": "factory_not_resolved"}))
		return
	}
	args := callArguments(definitionCall, kinds)
	if len(args) == 0 || kinds.Of(args[0]) != "object" {
		m.partial = true
		m.facts = append(m.facts, coverageFact(m, map[string]any{"coverage_status": "partial", "admission_status": "factory_resolved", "unresolved_definition_options": 1}))
		return
	}
	options := args[0]
	id, idLiteral := stringValue(objectPair(options, "id", src, kinds), src, kinds)
	if !idLiteral || id != spec.ID {
		m.partial = true
		m.facts = append(m.facts, coverageFact(m, map[string]any{"coverage_status": "unknown", "admission_status": "machine_id_mismatch", "configured_id": spec.ID, "source_id": id}))
		return
	}
	m.facts = append(m.facts, machineFact(m))
	stateTags := stringArray(objectPair(options, "stateTags", src, kinds), src, kinds)
	eventTags := stringArray(objectPair(options, "eventTags", src, kinds), src, kinds)
	rulesExpr := objectPair(options, "rules", src, kinds)
	if len(stateTags) == 0 || len(eventTags) == 0 {
		m.partial = true
	}
	for _, tag := range stateTags {
		m.addState(tag, tag, "", nodeForString(options, tag, src, kinds))
	}
	for _, tag := range eventTags {
		m.addEvent(tag, "machine", nodeForString(options, tag, src, kinds))
	}
	commands := []string{}
	walk(regBody, func(n *sitter.Node) {
		if kinds.Of(n) == "pair" && text(n.ChildByFieldName("key"), src) == "commandTags" {
			commands = stringArray(n.ChildByFieldName("value"), src, kinds)
		}
	})
	for _, tag := range unique(commands) {
		m.addCommand(tag, "machine", nil)
	}
	if len(commands) == 0 {
		m.partial = true
	}
	// The registration maps command tags to expected outcome events. This is
	// command lifecycle evidence; it does not prove that application code
	// executes the command.
	walk(regBody, func(n *sitter.Node) {
		if kinds.Of(n) != "pair" || text(n.ChildByFieldName("key"), src) != "commandHandlers" {
			return
		}
		for _, handler := range namedChildren(n.ChildByFieldName("value")) {
			command, commandOK := stringValue(objectPair(handler, "commandTag", src, kinds), src, kinds)
			outcome, outcomeOK := stringValue(objectPair(handler, "outcomeEventTag", src, kinds), src, kinds)
			if commandOK && outcomeOK && m.commands[command].Kind != "" && m.events[outcome].Kind != "" {
				addCommandRelation(m, command, relation(facts.RelFSMOutcome, machineMember(m.machine, "event", outcome)))
			} else {
				m.partial = true
				m.coverage["unresolved_command_outcomes"] = 1
			}
		}
	})

	var transitions []staticRule
	unsupportedFactoryCalls := 0
	if kinds.Of(rulesExpr) == "array" {
		transitions, unsupportedFactoryCalls = staticRuleArray(rulesExpr, root, src, kinds)
		if len(transitions) == 0 {
			m.partial = true
		}
		if unsupportedFactoryCalls > 0 {
			m.partial = true
			m.coverage["unsupported_rule_entries"] = unsupportedFactoryCalls
		}
	} else if kinds.Of(rulesExpr) == "call_expression" {
		builder := calleeName(rulesExpr, src, kinds)
		body := functionBody(root, builder, src, kinds)
		if body == nil {
			m.partial = true
			m.coverage["unresolved_rule_factory"] = builder
		} else {
			transitions, unsupportedFactoryCalls = staticRules(body, root, src, kinds)
			if len(transitions) == 0 || unsupportedFactoryCalls > 0 {
				m.partial = true
			}
			if unsupportedFactoryCalls > 0 {
				m.coverage["unsupported_rule_entries"] = unsupportedFactoryCalls
			}
			m.reads[m.file] = true
		}
	} else {
		m.partial = true
		m.coverage["unresolved_rule_expression"] = text(rulesExpr, src)
	}

	modeled, unresolved := 0, 0
	seen := map[string]int{}
	for ordinal, rule := range transitions {
		id, hasID := ruleString(rule, "id", src, kinds)
		from, fromOK := ruleString(rule, "from", src, kinds)
		to, toOK := ruleString(rule, "to", src, kinds)
		on := stringList(objectPair(rule.node, "on", src, kinds), src, kinds)
		if !hasID || id == "" {
			unresolved++
			m.partial = true
			id = fmt.Sprintf("unknown-rule-%d", ordinal+1)
		}
		seen[id]++
		transitionID := id
		if seen[id] > 1 {
			transitionID = fmt.Sprintf("%s#%d", id, seen[id])
			m.coverage["duplicate_rule_id"] = seen[id]
		}
		name := machineMember(m.machine, "transition", transitionID)
		props := map[string]any{
			"rule_id":        id,
			"branch_ordinal": ordinal + 1,
			"selection":      "exclusive",
		}
		var rels []facts.Relation
		if fromOK {
			rels = append(rels, relation(facts.RelFSMFrom, machineMember(m.machine, "state", from)))
			props["from_status"] = "resolved"
		} else {
			props["from_status"] = "unknown"
			unresolved++
			m.partial = true
		}
		if toOK {
			rels = append(rels, relation(facts.RelFSMTo, machineMember(m.machine, "state", to)))
			props["to_status"] = "resolved"
		} else {
			props["to_status"] = "unknown"
			unresolved++
			m.partial = true
		}
		if len(on) > 0 {
			for _, tag := range unique(on) {
				rels = append(rels, relation(facts.RelFSMOn, machineMember(m.machine, "event", tag)))
			}
			props["trigger_status"] = "resolved"
		} else {
			props["trigger_status"] = "unknown"
			unresolved++
			m.partial = true
		}
		guard := objectPair(rule.node, "guard", src, kinds)
		reducer := objectPair(rule.node, "reduce", src, kinds)
		if guard != nil {
			props["guard_text"] = text(guard, src)
			props["guard_line"] = nodeLine(guard)
			props["guard_end_line"] = int(guard.EndPosition().Row) + 1
			if kinds.Of(guard) == "identifier" {
				rels = append(rels, relation(facts.RelFSMGuardRef, text(guard, src)))
			} else {
				addDirectCallRelations(&rels, guard, src, kinds, facts.RelFSMGuardCalls)
			}
		}
		if reducer != nil {
			props["reducer_text"] = text(reducer, src)
			props["reducer_line"] = nodeLine(reducer)
			props["reducer_end_line"] = int(reducer.EndPosition().Row) + 1
			if kinds.Of(reducer) == "identifier" {
				rels = append(rels, relation(facts.RelFSMReducerRef, text(reducer, src)))
			} else {
				addDirectCallRelations(&rels, reducer, src, kinds, facts.RelFSMActionCalls)
			}
			for _, command := range commandTagsIn(reducer, spec.CommandType, src, kinds) {
				rels = append(rels, relation(facts.RelFSMEmits, machineMember(m.machine, "command", command)))
			}
		}
		if rule.conditional {
			props["availability"] = "conditional"
			props["condition_text"] = rule.condition
		} else {
			props["availability"] = "declared"
		}
		if strings.Contains(text(regBody, src), "unguardRuleIds") && guard != nil {
			props["guard_status"] = "conditional_override"
		} else if guard != nil {
			props["guard_status"] = "declared"
		} else {
			props["guard_status"] = "none"
		}
		site := rule.site
		fact := facts.Fact{Kind: facts.KindFSMTransition, Name: name, File: m.file,
			Line: nodeLine(site), EndLine: int(site.EndPosition().Row) + 1,
			Props: props, Relations: append(rels, relation(facts.RelFSMDeclaredIn, spec.Registration))}
		if rule.node != site {
			fact.Props["rule_definition_line"] = nodeLine(rule.node)
		}
		m.facts = append(m.facts, fact)
		modeled++
	}

	for _, typ := range []string{spec.StateType, spec.EventType, spec.CommandType} {
		if typ != "" {
			// The machine's type binding is direct and local to the configured
			// registration. Relations keep the type as a normal code-symbol target.
			m.facts[0].Relations = append(m.facts[0].Relations, relation(facts.RelFSMTypedBy, typ))
		}
	}
	initial := objectPair(options, "initialState", src, kinds)
	if initial != nil {
		if tag := initialStateTag(initial, spec.StateType, root, src, kinds); tag != "" {
			m.facts[0].Relations = append(m.facts[0].Relations, relation(facts.RelFSMInitial, machineMember(m.machine, "state", tag)))
			m.facts[0].Props["initial_status"] = "resolved"
		} else {
			m.facts[0].Props["initial_status"] = "unknown"
			m.partial = true
		}
	}
	if unresolved > 0 {
		m.coverage["unresolved_transition_fields"] = unresolved
	}
	m.coverage["branches_seen"] = len(transitions)
	m.coverage["branches_modeled"] = modeled
	m.coverage["unresolved_target"] = unresolved
	status := "complete"
	if m.partial {
		status = "partial"
	}
	m.coverage["coverage_status"] = status
	m.facts = append(m.facts, coverageFact(m, m.coverage))
}

func (m *machineModel) addState(tag, name, parent string, node *sitter.Node) {
	line, endLine := 0, 0
	if node != nil {
		line, endLine = nodeLine(node), int(node.EndPosition().Row)+1
	}
	m.addStateAt(tag, name, parent, line, endLine)
}

func (m *machineModel) addStateAt(tag, name, parent string, line, endLine int) {
	m.addStateAtFile(m.file, tag, name, parent, line, endLine)
}

func (m *machineModel) addStateAtFile(file, tag, name, parent string, line, endLine int) {
	if tag == "" || m.states[name].Kind != "" {
		return
	}
	props := map[string]any{"state_tag": tag, "state_path": name}
	var rels []facts.Relation
	if parent != "" {
		props["parent_state"] = parent
		rels = append(rels, relation(facts.RelFSMParent, machineMember(m.machine, "state", parent)))
	}
	if line > 0 {
		props["declaration_status"] = "literal"
	}
	if file == "" {
		file = m.file
	}
	f := facts.Fact{Kind: facts.KindFSMState, Name: machineMember(m.machine, "state", name), File: file, Props: props, Relations: append(rels, relation(facts.RelDeclares, m.machine))}
	if line > 0 {
		f.Line, f.EndLine = line, endLine
	}
	m.states[name] = f
	m.facts = append(m.facts, f)
}

func (m *machineModel) addEvent(tag, role string, node *sitter.Node) {
	line, endLine := 0, 0
	if node != nil {
		line, endLine = nodeLine(node), int(node.EndPosition().Row)+1
	}
	m.addEventAt(tag, role, m.file, line, endLine)
}

func (m *machineModel) addEventAt(tag, role, file string, line, endLine int) {
	if tag == "" || m.events[tag].Kind != "" {
		return
	}
	if file == "" {
		file = m.file
	}
	props := map[string]any{"event_tag": tag, "event_role": role}
	f := facts.Fact{Kind: facts.KindFSMEvent, Name: machineMember(m.machine, "event", tag), File: file, Props: props, Relations: []facts.Relation{relation(facts.RelDeclares, m.machine)}}
	if line > 0 {
		f.Line, f.EndLine = line, endLine
	}
	m.events[tag] = f
	m.facts = append(m.facts, f)
}

func (m *machineModel) addCommand(tag, role string, node *sitter.Node) {
	line, endLine := 0, 0
	if node != nil {
		line, endLine = nodeLine(node), int(node.EndPosition().Row)+1
	}
	m.addCommandAt(tag, role, m.file, line, endLine)
}

func (m *machineModel) addCommandAt(tag, role, file string, line, endLine int) {
	if tag == "" || m.commands[tag].Kind != "" {
		return
	}
	if file == "" {
		file = m.file
	}
	props := map[string]any{"command_tag": tag, "command_role": role}
	f := facts.Fact{Kind: facts.KindFSMCommand, Name: machineMember(m.machine, "command", tag), File: file, Props: props, Relations: []facts.Relation{relation(facts.RelDeclares, m.machine)}}
	if line > 0 {
		f.Line, f.EndLine = line, endLine
	}
	m.commands[tag] = f
	m.facts = append(m.facts, f)
}

func machineMember(machine, kind, value string) string { return machine + "/" + kind + ":" + value }

func machineFact(m *machineModel) facts.Fact {
	return facts.Fact{Kind: facts.KindFSMMachine, Name: m.machine, File: m.file, Props: map[string]any{"adapter": m.spec.Adapter, "admission_status": "configured_declaration_resolved"},
		Relations: []facts.Relation{relation(facts.RelDeclares, moduleName(m.file))}}
}

func coverageFact(m *machineModel, props map[string]any) facts.Fact {
	copyProps := map[string]any{"extractor": "typescript:fsm", "language": "typescript", "fsm_machine": m.machine}
	for k, v := range props {
		copyProps[k] = v
	}
	return facts.Fact{Kind: facts.KindExtraction, Name: "typescript:fsm:" + m.machine, File: m.file, Props: copyProps}
}

func moduleName(file string) string {
	d := filepathDir(file)
	if d == "." {
		return "."
	}
	return d
}

func filepathDir(file string) string {
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		return file[:i]
	}
	return "."
}

func matchesSymbolRef(resolved string, ref SymbolRef) bool {
	module, name, ok := strings.Cut(resolved, "#")
	if !ok || name != ref.Export {
		return false
	}
	return slash(module) == slash(ref.Module)
}

func (a *Analyzer) matchesSymbolRef(resolved string, ref SymbolRef) bool {
	if target, ok := a.resolveExport(ref.Module, ref.Export, map[string]bool{}); ok {
		return resolved == target
	}
	return matchesSymbolRef(resolved, ref)
}

func nodeForString(root *sitter.Node, value string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	var found *sitter.Node
	walk(root, func(n *sitter.Node) {
		if found != nil || kinds.Of(n) != "string" {
			return
		}
		if v, ok := stringValue(n, src, kinds); ok && v == value {
			found = n
		}
	})
	return found
}

func stringArray(n *sitter.Node, src []byte, kinds *tsutil.KindTable) []string {
	if n == nil || kinds.Of(n) != "array" {
		return nil
	}
	var out []string
	for _, c := range namedChildren(n) {
		if v, ok := stringValue(c, src, kinds); ok {
			out = append(out, v)
		}
	}
	return unique(out)
}

func stringList(n *sitter.Node, src []byte, kinds *tsutil.KindTable) []string {
	if value, ok := stringValue(n, src, kinds); ok {
		return []string{value}
	}
	return stringArray(n, src, kinds)
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

type staticRule struct {
	node        *sitter.Node
	site        *sitter.Node
	bindings    map[string]string
	conditional bool
	condition   string
}

func staticRules(body, root *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]staticRule, int) {
	// First resolve the statically initialized array. It is the common shape
	// used by the rule-table adapter and expands a helper only when the caller
	// passes a literal builder reference with no arguments.
	var arr *sitter.Node
	var varName string
	walk(body, func(n *sitter.Node) {
		if arr != nil || kinds.Of(n) != "variable_declarator" {
			return
		}
		value := n.ChildByFieldName("value")
		if kinds.Of(value) == "array" {
			arr, varName = value, text(n.ChildByFieldName("name"), src)
		}
	})
	var array *sitter.Node
	if arr != nil {
		array = arr
	} else {
		// scanRun's factory directly returns its rule array instead of assigning
		// it to a local first. Restrict the selection to a return owned by the
		// configured function body.
		for _, n := range namedChildren(body) {
			if kinds.Of(n) != "return_statement" {
				continue
			}
			value := returnValue(n)
			for value != nil && kinds.Of(value) == "parenthesized_expression" && value.NamedChildCount() == 1 {
				value = value.NamedChild(0)
			}
			if kinds.Of(value) == "array" {
				array = value
				break
			}
		}
	}
	var rules []staticRule
	unsupported := 0
	if array != nil {
		var unsupportedArray int
		rules, unsupportedArray = staticRuleArray(array, root, src, kinds)
		unsupported += unsupportedArray
	}
	if varName != "" {
		walkFunctionScope(body, kinds, func(n *sitter.Node) {
			if kinds.Of(n) != "call_expression" || calleeName(n, src, kinds) != "push" {
				return
			}
			fn := n.ChildByFieldName("function")
			if fn == nil || kinds.Of(fn) != "member_expression" || text(fn.ChildByFieldName("object"), src) != varName {
				return
			}
			args := callArguments(n, kinds)
			if len(args) != 1 || kinds.Of(args[0]) != "object" {
				unsupported++
				return
			}
			condition, conditional := enclosingCondition(n, src, kinds)
			rules = append(rules, staticRule{node: args[0], site: n, conditional: conditional, condition: condition})
		})
	}
	return rules, unsupported
}

func staticRuleArray(array, root *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]staticRule, int) {
	var rules []staticRule
	unsupported := 0
	for _, n := range namedChildren(array) {
		switch kinds.Of(n) {
		case "object":
			rules = append(rules, staticRule{node: n, site: n})
		case "call_expression":
			if expanded, ok := expandRuleFactoryCall(n, root, src, kinds); ok {
				rules = append(rules, expanded)
			} else {
				unsupported++
			}
		default:
			// Spreads, identifiers, holes, and other expressions can add rule
			// entries that are not established by this static source model.
			unsupported++
		}
	}
	return rules, unsupported
}

func expandRuleFactoryCall(call, root *sitter.Node, src []byte, kinds *tsutil.KindTable) (staticRule, bool) {
	name := calleeName(call, src, kinds)
	body := functionBody(root, name, src, kinds)
	if body == nil {
		return staticRule{}, false
	}
	var template *sitter.Node
	walk(body, func(n *sitter.Node) {
		if template != nil || kinds.Of(n) != "object" {
			return
		}
		if hasRuleProperty(n, "id", src, kinds) && hasRuleProperty(n, "from", src, kinds) && hasRuleProperty(n, "to", src, kinds) {
			template = n
		}
	})
	if template == nil {
		return staticRule{}, false
	}
	fn := namedFunction(root, name, src, kinds)
	var params []string
	if fn != nil {
		params = parameterNames(fn, src, kinds)
	} else if value := variableValue(root, name, src, kinds); value != nil {
		params = parameterNames(value, src, kinds)
	}
	args := callArguments(call, kinds)
	if len(args) != len(params) {
		return staticRule{}, false
	}
	bindings := map[string]string{}
	for i, param := range params {
		value, ok := stringValue(args[i], src, kinds)
		if !ok {
			return staticRule{}, false
		}
		bindings[param] = value
	}
	// Only expand fields whose complete values are statically evaluated. The
	// helper call remains unknown if interpolation uses an unsupported form.
	for _, key := range []string{"id", "from", "on", "to"} {
		if key == "on" {
			if objectPair(template, key, src, kinds) == nil {
				return staticRule{}, false
			}
			continue
		}
		if _, ok := ruleString(staticRule{node: template, bindings: bindings}, key, src, kinds); !ok {
			return staticRule{}, false
		}
	}
	return staticRule{node: template, site: call, bindings: bindings}, true
}

func hasRuleProperty(object *sitter.Node, key string, src []byte, kinds *tsutil.KindTable) bool {
	if objectPair(object, key, src, kinds) != nil {
		return true
	}
	for _, child := range namedChildren(object) {
		if kinds.Of(child) == "shorthand_property_identifier" && text(child, src) == key {
			return true
		}
	}
	return false
}

func ruleString(rule staticRule, key string, src []byte, kinds *tsutil.KindTable) (string, bool) {
	n := objectPair(rule.node, key, src, kinds)
	if n == nil {
		// Per-rule helper objects commonly use shorthand properties such as
		// `{ id, from, on: 'Go', to }`. Resolve only names explicitly bound by
		// the literal helper call; unbound shorthand stays unknown.
		for _, child := range namedChildren(rule.node) {
			kind := kinds.Of(child)
			if kind != "shorthand_property_identifier" && kind != "shorthand_property_identifier_pattern" {
				continue
			}
			name := text(child, src)
			if name == key {
				value, ok := rule.bindings[name]
				return value, ok
			}
		}
		return "", false
	}
	if value, ok := stringValue(n, src, kinds); ok {
		return value, true
	}
	if kinds.Of(n) == "identifier" {
		value, ok := rule.bindings[text(n, src)]
		return value, ok
	}
	if kinds.Of(n) == "template_string" {
		value := strings.Trim(text(n, src), "`")
		for name, bound := range rule.bindings {
			value = strings.ReplaceAll(value, "${"+name+"}", bound)
			value = strings.ReplaceAll(value, "${"+name+".toLowerCase()}", strings.ToLower(bound))
		}
		if strings.Contains(value, "${") {
			return "", false
		}
		return value, true
	}
	return "", false
}

func enclosingCondition(n *sitter.Node, src []byte, kinds *tsutil.KindTable) (string, bool) {
	var conditions []string
	conditional := false
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch kinds.Of(p) {
		case "if_statement":
			condition := text(p.ChildByFieldName("condition"), src)
			if alternative := p.ChildByFieldName("alternative"); alternative != nil && n.StartByte() >= alternative.StartByte() && n.EndByte() <= alternative.EndByte() {
				condition = "!(" + condition + ")"
			}
			conditions = append(conditions, condition)
			conditional = true
		case "switch_case", "for_statement", "for_in_statement", "while_statement", "do_statement":
			condition := text(p.ChildByFieldName("condition"), src)
			if condition == "" {
				condition = text(p, src)
			}
			conditions = append(conditions, condition)
			conditional = true
		}
	}
	return strings.Join(conditions, " && "), conditional
}

func addDirectCallRelations(rels *[]facts.Relation, node *sitter.Node, src []byte, kinds *tsutil.KindTable, relKind string) {
	seen := map[string]bool{}
	walk(node, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" {
			return
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || kinds.Of(fn) != "identifier" {
			return
		}
		name := text(fn, src)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		*rels = append(*rels, relation(relKind, name))
	})
}

func commandTagsIn(node *sitter.Node, commandType string, src []byte, kinds *tsutil.KindTable) []string {
	var out []string
	walk(node, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" {
			return
		}
		fn := n.ChildByFieldName("function")
		if fn == nil || kinds.Of(fn) != "member_expression" {
			return
		}
		obj := fn.ChildByFieldName("object")
		if obj == nil || text(obj, src) != commandType {
			return
		}
		out = append(out, text(fn.ChildByFieldName("property"), src))
	})
	return unique(out)
}

func initialStateTag(node *sitter.Node, stateType string, root *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	var tag string
	walk(node, func(n *sitter.Node) {
		if tag != "" {
			return
		}
		if kinds.Of(n) == "member_expression" && text(n.ChildByFieldName("object"), src) == stateType {
			tag = text(n.ChildByFieldName("property"), src)
		}
	})
	if tag != "" {
		return tag
	}
	// A one-callee return is safe only when its local body has one literal
	// tagged-constructor result; multiple/dynamic results stay unknown.
	var callee string
	walk(node, func(n *sitter.Node) {
		if callee == "" && kinds.Of(n) == "call_expression" {
			callee = calleeName(n, src, kinds)
		}
	})
	body := functionBody(root, callee, src, kinds)
	if body != nil {
		found := map[string]bool{}
		walk(body, func(n *sitter.Node) {
			if kinds.Of(n) == "member_expression" && text(n.ChildByFieldName("object"), src) == stateType {
				found[text(n.ChildByFieldName("property"), src)] = true
			}
		})
		if len(found) == 1 {
			for v := range found {
				return v
			}
		}
	}
	return ""
}
