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
	declaredIn, hasRegistrationSymbol := a.symbolRelation(m.file, spec.Registration, facts.RelFSMDeclaredIn)
	if hasRegistrationSymbol && declaredIn.TargetFile != "" {
		m.reads[declaredIn.TargetFile] = true
	} else if !hasRegistrationSymbol {
		m.partial = true
		m.coverage["unresolved_registration_symbol"] = 1
	}
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
		guardOverride, hasGuardOverride := rule.mapOverrides["guard"]
		if hasGuardOverride {
			props["guard_override_text"] = text(guardOverride, src)
			if condition := rule.mapOverrideConditions["guard"]; condition != "" {
				props["guard_status"] = "conditional_override"
				props["guard_override_condition_text"] = condition
				m.partial = true
				m.coverage["conditional_guard_overrides"] = 1
				if guard != nil {
					props["guard_presence_condition_text"] = "!(" + condition + ")"
					props["guard_text"] = text(guard, src)
					props["guard_line"] = nodeLine(guard)
					props["guard_end_line"] = int(guard.EndPosition().Row) + 1
					if kinds.Of(guard) == "identifier" {
						a.appendRuleTableSymbolRef(m, &rels, text(guard, src), facts.RelFSMGuardRef, "unresolved_guard_reference")
					} else {
						a.addRuleTableCallRelations(m, &rels, guard, src, kinds, facts.RelFSMGuardCalls, "unresolved_guard_calls")
					}
				}
			} else if text(guardOverride, src) == "undefined" {
				props["guard_status"] = "none"
			} else {
				props["guard_status"] = "unknown_override"
				m.partial = true
				m.coverage["unknown_guard_overrides"] = 1
			}
		} else if guard != nil {
			props["guard_text"] = text(guard, src)
			props["guard_line"] = nodeLine(guard)
			props["guard_end_line"] = int(guard.EndPosition().Row) + 1
			if kinds.Of(guard) == "identifier" {
				a.appendRuleTableSymbolRef(m, &rels, text(guard, src), facts.RelFSMGuardRef, "unresolved_guard_reference")
			} else {
				a.addRuleTableCallRelations(m, &rels, guard, src, kinds, facts.RelFSMGuardCalls, "unresolved_guard_calls")
			}
		}
		reducerOverride, hasReducerOverride := rule.mapOverrides["reduce"]
		if hasReducerOverride {
			props["reducer_override_text"] = text(reducerOverride, src)
			if condition := rule.mapOverrideConditions["reduce"]; condition != "" {
				props["reducer_status"] = "conditional_override"
				props["reducer_override_condition_text"] = condition
				if reducer != nil {
					props["reducer_presence_condition_text"] = "!(" + condition + ")"
					props["reducer_text"] = text(reducer, src)
					props["reducer_line"] = nodeLine(reducer)
					props["reducer_end_line"] = int(reducer.EndPosition().Row) + 1
					if kinds.Of(reducer) == "identifier" {
						a.appendRuleTableSymbolRef(m, &rels, text(reducer, src), facts.RelFSMReducerRef, "unresolved_reducer_reference")
					} else {
						a.addRuleTableCallRelations(m, &rels, reducer, src, kinds, facts.RelFSMActionCalls, "unresolved_reducer_calls")
					}
					commands, proven := commandTagsIn(reducer, spec.CommandType, src, kinds)
					if !proven {
						m.partial = true
						m.coverage["unresolved_reducer_command_flow"] = 1
					}
					for _, command := range commands {
						rels = append(rels, relation(facts.RelFSMEmits, machineMember(m.machine, "command", command)))
					}
				}
			} else {
				props["reducer_status"] = "unknown_override"
				m.partial = true
				m.coverage["unknown_reducer_overrides"] = 1
			}
		} else if reducer != nil {
			props["reducer_text"] = text(reducer, src)
			props["reducer_line"] = nodeLine(reducer)
			props["reducer_end_line"] = int(reducer.EndPosition().Row) + 1
			if kinds.Of(reducer) == "identifier" {
				a.appendRuleTableSymbolRef(m, &rels, text(reducer, src), facts.RelFSMReducerRef, "unresolved_reducer_reference")
			} else {
				a.addRuleTableCallRelations(m, &rels, reducer, src, kinds, facts.RelFSMActionCalls, "unresolved_reducer_calls")
			}
			commands, proven := commandTagsIn(reducer, spec.CommandType, src, kinds)
			if !proven {
				m.partial = true
				m.coverage["unresolved_reducer_command_flow"] = 1
			}
			for _, command := range commands {
				rels = append(rels, relation(facts.RelFSMEmits, machineMember(m.machine, "command", command)))
			}
		}
		if rule.conditional {
			props["availability"] = "conditional"
			props["condition_text"] = rule.condition
		} else {
			props["availability"] = "declared"
		}
		if hasGuardOverride {
			// The mapper override above determines whether the source guard may
			// be absent. Do not emit a stale guard reference from the base rule.
		} else if guard != nil {
			props["guard_status"] = "declared"
		} else {
			props["guard_status"] = "none"
		}
		site := rule.site
		if hasRegistrationSymbol {
			rels = append(rels, declaredIn)
		}
		fact := facts.Fact{Kind: facts.KindFSMTransition, Name: name, File: m.file,
			Line: nodeLine(site), EndLine: int(site.EndPosition().Row) + 1,
			Props: props, Relations: rels}
		if rule.node != site {
			fact.Props["rule_definition_line"] = nodeLine(rule.node)
		}
		m.facts = append(m.facts, fact)
		modeled++
	}

	for _, typ := range []string{spec.StateType, spec.EventType, spec.CommandType} {
		if typ != "" {
			if rel, ok := a.typeRelation(m.file, typ, facts.RelFSMTypedBy); ok {
				m.facts[0].Relations = append(m.facts[0].Relations, rel)
				if rel.TargetFile != "" {
					m.reads[rel.TargetFile] = true
				}
			} else {
				m.partial = true
				m.coverage["unresolved_typed_by"] = 1
			}
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
	node                  *sitter.Node
	site                  *sitter.Node
	bindings              map[string]string
	conditional           bool
	condition             string
	mapOverrides          map[string]*sitter.Node
	mapConditional        bool
	mapOverrideConditions map[string]string
}

type ruleMapEffect struct {
	overrides          map[string]*sitter.Node
	conditional        bool
	overrideConditions map[string]string
}

type returnedRuleSource struct {
	array     *sitter.Node
	variable  string
	transform ruleMapEffect
}

func staticRules(body, root *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]staticRule, int) {
	var rules []staticRule
	unsupported := 0
	var sources []returnedRuleSource
	var returnedNames = map[string]bool{}
	transforms := map[string]ruleMapEffect{}
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if kinds.Of(n) != "return_statement" {
			return
		}
		source, ok := returnedRuleArray(returnValue(n), src, kinds)
		if !ok {
			unsupported++
			return
		}
		sources = append(sources, source)
		if source.variable != "" {
			returnedNames[source.variable] = true
			transforms[source.variable] = source.transform
		}
	})
	if len(sources) == 0 {
		unsupported++
	}
	for _, source := range sources {
		found, skipped := staticRuleArray(source.array, root, src, kinds)
		for i := range found {
			found[i].mapOverrides = source.transform.overrides
			found[i].mapConditional = source.transform.conditional
			found[i].mapOverrideConditions = source.transform.overrideConditions
		}
		rules = append(rules, found...)
		unsupported += skipped
	}
	if len(returnedNames) > 0 {
		returned := map[string]bool{}
		for name := range returnedNames {
			returned[name] = true
		}
		walkFunctionScope(body, kinds, func(n *sitter.Node) {
			if kinds.Of(n) != "call_expression" || calleeName(n, src, kinds) != "push" {
				return
			}
			fn := n.ChildByFieldName("function")
			if fn == nil || kinds.Of(fn) != "member_expression" || !returned[text(fn.ChildByFieldName("object"), src)] {
				return
			}
			args := callArguments(n, kinds)
			if len(args) != 1 || kinds.Of(args[0]) != "object" {
				unsupported++
				return
			}
			condition, conditional := enclosingCondition(n, src, kinds)
			transform := transforms[text(fn.ChildByFieldName("object"), src)]
			rules = append(rules, staticRule{node: args[0], site: n, conditional: conditional, condition: condition,
				mapOverrides: transform.overrides, mapConditional: transform.conditional, mapOverrideConditions: transform.overrideConditions})
		})
	}
	return rules, unsupported
}

// returnedRuleArray follows only the array that reaches a function return. A
// map is accepted when every callback return either preserves its input rule
// or shallow-copies that rule without changing its structural identity fields.
func returnedRuleArray(value *sitter.Node, src []byte, kinds *tsutil.KindTable) (returnedRuleSource, bool) {
	value = unwrapExpression(value, kinds)
	if value == nil {
		return returnedRuleSource{}, false
	}
	if kinds.Of(value) == "array" {
		return returnedRuleSource{array: value}, true
	}
	if kinds.Of(value) == "identifier" {
		name := text(value, src)
		binding := localBindingAt(value, name, src, kinds)
		if binding == nil || kinds.Of(binding) != "variable_declarator" {
			return returnedRuleSource{}, false
		}
		array := unwrapExpression(binding.ChildByFieldName("value"), kinds)
		if kinds.Of(array) == "array" {
			return returnedRuleSource{array: array, variable: name}, true
		}
		return returnedRuleSource{}, false
	}
	if kinds.Of(value) != "call_expression" {
		return returnedRuleSource{}, false
	}
	fn := value.ChildByFieldName("function")
	if fn == nil || kinds.Of(fn) != "member_expression" || text(fn.ChildByFieldName("property"), src) != "map" {
		return returnedRuleSource{}, false
	}
	object := unwrapExpression(fn.ChildByFieldName("object"), kinds)
	if kinds.Of(object) != "identifier" {
		return returnedRuleSource{}, false
	}
	name := text(object, src)
	binding := localBindingAt(object, name, src, kinds)
	if binding == nil || kinds.Of(binding) != "variable_declarator" {
		return returnedRuleSource{}, false
	}
	array := unwrapExpression(binding.ChildByFieldName("value"), kinds)
	args := callArguments(value, kinds)
	if kinds.Of(array) != "array" || len(args) != 1 {
		return returnedRuleSource{}, false
	}
	transform, ok := ruleMapTransform(args[0], src, kinds)
	if !ok {
		return returnedRuleSource{}, false
	}
	return returnedRuleSource{array: array, variable: name, transform: transform}, true
}

func ruleMapTransform(callback *sitter.Node, src []byte, kinds *tsutil.KindTable) (ruleMapEffect, bool) {
	callback = unwrapExpression(callback, kinds)
	if callback == nil || (kinds.Of(callback) != "arrow_function" && kinds.Of(callback) != "function_expression") {
		return ruleMapEffect{}, false
	}
	params := parameterNames(callback, src, kinds)
	if len(params) == 0 || params[0] == "" {
		return ruleMapEffect{}, false
	}
	body := callback.ChildByFieldName("body")
	if kinds.Of(body) != "statement_block" {
		return ruleMapTransformValue(body, callback, params[0], src, kinds)
	}
	if ruleCallbackMutatesInput(body, params[0], src, kinds) {
		return ruleMapEffect{}, false
	}
	var returns []*sitter.Node
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if kinds.Of(n) == "return_statement" {
			returns = append(returns, returnValue(n))
		}
	})
	if len(returns) == 0 || !functionDefinitelyReturns(body, kinds) {
		return ruleMapEffect{}, false
	}
	merged := ruleMapEffect{overrides: map[string]*sitter.Node{}}
	first := true
	for _, value := range returns {
		branch, ok := ruleMapTransformValue(value, callback, params[0], src, kinds)
		if !ok {
			return ruleMapEffect{}, false
		}
		if !first && !sameRuleOverrides(merged.overrides, branch.overrides, src) {
			merged.conditional = true
		}
		for key, override := range branch.overrides {
			merged.overrides[key] = override
		}
		first = false
	}
	return merged, true
}

func ruleMapTransformValue(value, callback *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable) (ruleMapEffect, bool) {
	value = unwrapExpression(value, kinds)
	if value == nil {
		return ruleMapEffect{}, false
	}
	switch kinds.Of(value) {
	case "identifier":
		return ruleMapEffect{overrides: map[string]*sitter.Node{}}, text(value, src) == parameter && callbackParameterReference(value, callback, parameter, src, kinds)
	case "conditional_expression", "ternary_expression":
		left, leftOK := ruleMapTransformValue(value.ChildByFieldName("consequence"), callback, parameter, src, kinds)
		right, rightOK := ruleMapTransformValue(value.ChildByFieldName("alternative"), callback, parameter, src, kinds)
		if !leftOK || !rightOK {
			return ruleMapEffect{}, false
		}
		result := ruleMapEffect{overrides: map[string]*sitter.Node{}, overrideConditions: map[string]string{}}
		result.conditional = left.conditional || right.conditional
		if !sameRuleOverrides(left.overrides, right.overrides, src) {
			result.conditional = true
		}
		condition := strings.TrimSpace(text(value.ChildByFieldName("condition"), src))
		keys := map[string]bool{}
		for key := range left.overrides {
			keys[key] = true
		}
		for key := range right.overrides {
			keys[key] = true
		}
		for key := range keys {
			leftValue, leftHas := left.overrides[key]
			rightValue, rightHas := right.overrides[key]
			if leftHas && rightHas && text(leftValue, src) == text(rightValue, src) {
				result.overrides[key] = leftValue
				continue
			}
			result.conditional = true
			if leftHas {
				result.overrides[key] = leftValue
				if condition != "" && !rightHas {
					result.overrideConditions[key] = condition
				}
			} else if rightHas {
				result.overrides[key] = rightValue
				if condition != "" {
					result.overrideConditions[key] = "!(" + condition + ")"
				}
			} else {
				result.overrides[key] = nil
			}
		}
		for key, cond := range left.overrideConditions {
			if _, ok := result.overrideConditions[key]; !ok {
				result.overrideConditions[key] = cond
			}
		}
		for key, cond := range right.overrideConditions {
			if _, ok := result.overrideConditions[key]; !ok {
				result.overrideConditions[key] = cond
			}
		}
		return result, true
	case "object":
		preserves := false
		for _, member := range namedChildren(value) {
			if kinds.Of(member) == "spread_element" {
				spread := strings.TrimSpace(strings.TrimPrefix(text(member, src), "..."))
				if spread != parameter || !callbackParameterReference(member, callback, parameter, src, kinds) || preserves {
					return ruleMapEffect{}, false
				}
				preserves = true
			}
		}
		if !preserves {
			return ruleMapEffect{}, false
		}
		overrides := map[string]*sitter.Node{}
		for _, member := range namedChildren(value) {
			if kinds.Of(member) != "pair" {
				continue
			}
			key := text(member.ChildByFieldName("key"), src)
			if key == "" {
				return ruleMapEffect{}, false
			}
			for _, structural := range []string{"id", "from", "on", "to"} {
				if key == structural {
					return ruleMapEffect{}, false
				}
			}
			overrides[key] = member.ChildByFieldName("value")
		}
		return ruleMapEffect{overrides: overrides}, true
	default:
		return ruleMapEffect{}, false
	}
}

func callbackParameterReference(reference, callback *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable) bool {
	if reference == nil || callback == nil || parameter == "" {
		return false
	}
	for scope := reference.Parent(); scope != nil && scope != callback; scope = scope.Parent() {
		if kinds.Of(scope) != "statement_block" {
			continue
		}
		for _, declaration := range namedChildren(scope) {
			switch kinds.Of(declaration) {
			case "lexical_declaration", "variable_declaration":
				for _, binding := range namedChildren(declaration) {
					if kinds.Of(binding) == "variable_declarator" && text(binding.ChildByFieldName("name"), src) == parameter {
						return false
					}
				}
			case "function_declaration":
				if functionName(declaration, src) == parameter {
					return false
				}
			}
		}
	}
	return true
}

func sameRuleOverrides(a, b map[string]*sitter.Node, src []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		other, ok := b[key]
		if !ok || text(value, src) != text(other, src) {
			return false
		}
	}
	return true
}

func ruleCallbackMutatesInput(body *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable) bool {
	mutates := false
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if mutates {
			return
		}
		switch kinds.Of(n) {
		case "assignment_expression", "augmented_assignment_expression":
			if expressionRootIdentifier(n.ChildByFieldName("left"), src, kinds) == parameter {
				mutates = true
			}
		case "update_expression":
			if expressionRootIdentifier(n.ChildByFieldName("argument"), src, kinds) == parameter {
				mutates = true
			}
		case "call_expression":
			for _, arg := range callArguments(n, kinds) {
				if kinds.Of(unwrapExpression(arg, kinds)) == "identifier" && text(unwrapExpression(arg, kinds), src) == parameter {
					mutates = true
				}
			}
		}
	})
	return mutates
}

func expressionRootIdentifier(node *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	node = unwrapExpression(node, kinds)
	for node != nil {
		switch kinds.Of(node) {
		case "identifier":
			return text(node, src)
		case "member_expression", "subscript_expression":
			node = node.ChildByFieldName("object")
		default:
			return ""
		}
	}
	return ""
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

func (a *Analyzer) appendRuleTableSymbolRef(m *machineModel, rels *[]facts.Relation, local, relationKind, coverageKey string) bool {
	rel, ok := a.symbolRelation(m.file, local, relationKind)
	if !ok {
		m.partial = true
		m.coverage[coverageKey] = 1
		return false
	}
	*rels = append(*rels, rel)
	if rel.TargetFile != "" && rel.TargetFile != m.file {
		m.reads[rel.TargetFile] = true
	}
	return true
}

func (a *Analyzer) addRuleTableCallRelations(m *machineModel, rels *[]facts.Relation, node *sitter.Node, src []byte, kinds *tsutil.KindTable, relationKind, coverageKey string) {
	seen := map[string]bool{}
	unresolved := 0
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
		if !a.appendRuleTableSymbolRef(m, rels, name, relationKind, coverageKey) {
			unresolved++
		}
	})
	if unresolved > 0 {
		m.partial = true
		m.coverage[coverageKey] = unresolved
	}
}

func commandTagsIn(node *sitter.Node, commandType string, src []byte, kinds *tsutil.KindTable) ([]string, bool) {
	body, ok := localCallableBodyAt(node, src, kinds)
	if !ok || body == nil {
		return nil, false
	}
	var returned []*sitter.Node
	if kinds.Of(body) == "statement_block" {
		walkFunctionScope(body, kinds, func(n *sitter.Node) {
			if kinds.Of(n) == "return_statement" {
				returned = append(returned, returnValue(n))
			}
		})
	} else {
		returned = append(returned, body)
	}
	if len(returned) == 0 {
		return nil, false
	}
	commandExprs := []*sitter.Node{}
	proven := true
	var collectCommands func(*sitter.Node, map[string]bool)
	collectCommands = func(value *sitter.Node, seen map[string]bool) {
		value = unwrapExpression(value, kinds)
		if value == nil {
			proven = false
			return
		}
		switch kinds.Of(value) {
		case "object":
			var found bool
			for _, pair := range namedChildren(value) {
				if kinds.Of(pair) != "pair" || text(pair.ChildByFieldName("key"), src) != "commands" {
					continue
				}
				found = true
				commandExprs = append(commandExprs, pair.ChildByFieldName("value"))
			}
			if !found {
				proven = false
			}
		case "identifier":
			name := text(value, src)
			if seen[name] {
				proven = false
				return
			}
			binding := localBindingAt(value, name, src, kinds)
			if binding == nil || kinds.Of(binding) != "variable_declarator" {
				proven = false
				return
			}
			next := make(map[string]bool, len(seen)+1)
			for k, v := range seen {
				next[k] = v
			}
			next[name] = true
			collectCommands(binding.ChildByFieldName("value"), next)
		case "conditional_expression":
			collectCommands(value.ChildByFieldName("consequence"), seen)
			collectCommands(value.ChildByFieldName("alternative"), seen)
		default:
			proven = false
		}
	}
	for _, value := range returned {
		collectCommands(value, map[string]bool{})
	}
	if len(commandExprs) == 0 {
		return nil, false
	}
	var out []string
	for _, expr := range commandExprs {
		walkFunctionScope(expr, kinds, func(n *sitter.Node) {
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
	}
	return unique(out), proven
}

func localBindingAt(reference *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if reference == nil || name == "" {
		return nil
	}
	for scope := reference.Parent(); scope != nil; scope = scope.Parent() {
		kind := kinds.Of(scope)
		switch kind {
		case "function_declaration", "generator_function_declaration", "function_expression", "arrow_function", "method_definition":
			if functionName(scope, src) == name || text(scope.ChildByFieldName("name"), src) == name {
				return scope
			}
			if parameter := parameterBindingAt(scope, name, src, kinds); parameter != nil {
				return parameter
			}
		case "catch_clause":
			if parameter := scope.ChildByFieldName("parameter"); bindingPatternHasName(parameter, name, src, kinds) {
				return parameter
			}
		}
		if kind != "statement_block" && kind != "program" {
			continue
		}
		for _, declaration := range namedChildren(scope) {
			declarations := []*sitter.Node{declaration}
			if kinds.Of(declaration) == "export_statement" {
				declarations = namedChildren(declaration)
			}
			for _, declaration := range declarations {
				switch kinds.Of(declaration) {
				case "function_declaration":
					if functionName(declaration, src) == name {
						return declaration
					}
				case "class_declaration":
					if text(declaration.ChildByFieldName("name"), src) == name {
						return declaration
					}
				case "lexical_declaration", "variable_declaration":
					for _, binding := range namedChildren(declaration) {
						if kinds.Of(binding) == "variable_declarator" && bindingPatternHasName(binding.ChildByFieldName("name"), name, src, kinds) {
							return binding
						}
					}
				}
			}
		}
	}
	return nil
}

func parameterBindingAt(function *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if function == nil {
		return nil
	}
	parameters := function.ChildByFieldName("parameters")
	if parameters == nil {
		parameters = function.ChildByFieldName("parameter")
	}
	if kinds.Of(parameters) == "identifier" && text(parameters, src) == name {
		return parameters
	}
	for _, parameter := range namedChildren(parameters) {
		pattern := parameter.ChildByFieldName("name")
		if pattern == nil {
			pattern = parameter.ChildByFieldName("pattern")
		}
		if pattern == nil && kinds.Of(parameter) == "identifier" {
			pattern = parameter
		}
		if bindingPatternHasName(pattern, name, src, kinds) {
			return pattern
		}
	}
	return nil
}

func bindingPatternHasName(pattern *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) bool {
	if pattern == nil || name == "" {
		return false
	}
	switch kinds.Of(pattern) {
	case "identifier", "shorthand_property_identifier_pattern":
		return text(pattern, src) == name
	case "pair_pattern":
		value := pattern.ChildByFieldName("value")
		if value == nil {
			value = pattern.ChildByFieldName("key")
		}
		return bindingPatternHasName(value, name, src, kinds)
	case "assignment_pattern", "required_parameter", "optional_parameter", "rest_pattern":
		for _, field := range []string{"left", "pattern", "name"} {
			if child := pattern.ChildByFieldName(field); child != nil && bindingPatternHasName(child, name, src, kinds) {
				return true
			}
		}
		return false
	case "object_pattern", "array_pattern":
		for _, child := range namedChildren(pattern) {
			if bindingPatternHasName(child, name, src, kinds) {
				return true
			}
		}
	}
	return false
}

func localCallableBodyAt(reference *sitter.Node, src []byte, kinds *tsutil.KindTable) (*sitter.Node, bool) {
	seen := map[string]bool{}
	value := unwrapExpression(reference, kinds)
	for value != nil && kinds.Of(value) == "identifier" {
		name := text(value, src)
		if seen[name] {
			return nil, false
		}
		seen[name] = true
		binding := localBindingAt(value, name, src, kinds)
		if binding == nil {
			return nil, false
		}
		if kinds.Of(binding) == "function_declaration" {
			return binding.ChildByFieldName("body"), true
		}
		value = unwrapExpression(binding.ChildByFieldName("value"), kinds)
	}
	if value != nil && (kinds.Of(value) == "arrow_function" || kinds.Of(value) == "function_expression") {
		return value.ChildByFieldName("body"), true
	}
	return nil, false
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
