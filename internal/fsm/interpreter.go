package fsm

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	stringLiteralRE = regexp.MustCompile(`['\"]([^'\"]+)['\"]`)
	stateMatchRE    = regexp.MustCompile(`(?:\.matches\s*\(\s*|\.value(?:\.([A-Za-z_$][\w$]*))?\s*===?\s*)['\"]([^'\"]+)['\"]`)
	stateObjectRE   = regexp.MustCompile(`\.matches\s*\(\s*\{\s*([A-Za-z_$][\w$]*)\s*:\s*['\"]([^'\"]+)['\"]`)
)

func (a *Analyzer) buildInterpreter(m *machineModel, root *sitter.Node, kinds *tsutil.KindTable, src []byte) {
	spec := m.spec
	if functionBody(root, spec.Dispatcher, src, kinds) == nil {
		m.partial = true
		m.facts = append(m.facts, coverageFact(m, map[string]any{"coverage_status": "unknown", "admission_status": "dispatcher_not_found"}))
		return
	}
	m.facts = append(m.facts, machineFact(m))
	stateFile, stateExport := a.resolveType(m.file, spec.StateType)
	eventFile, eventExport := a.resolveType(m.file, spec.EventType)
	if stateFile == "" {
		stateFile, stateExport = m.file, spec.StateType
	}
	if eventFile == "" {
		m.partial = true
		eventFile, eventExport = m.file, spec.EventType
	}
	m.reads[stateFile] = true
	m.reads[eventFile] = true
	stateTags := a.collectStateType(stateFile, stateExport, "", map[string]bool{})
	for _, st := range stateTags {
		if st.file != "" {
			m.reads[st.file] = true
		}
		m.addStateAtFile(st.file, st.tag, st.path, st.parent, st.line, st.endLine)
	}
	eventTags := a.collectTaggedType(eventFile, eventExport, "type", map[string]bool{})
	for _, declaration := range eventTags {
		if declaration.file != "" {
			m.reads[declaration.file] = true
		}
		m.addEventAt(declaration.tag, "machine_event", declaration.file, declaration.line, declaration.endLine)
	}
	if len(stateTags) == 0 || len(eventTags) == 0 {
		m.partial = true
	}
	if spec.EffectType != "" {
		effectFile, effectExport := a.resolveType(m.file, spec.EffectType)
		if effectFile == "" {
			effectFile, effectExport = m.file, spec.EffectType
		}
		m.reads[effectFile] = true
		for _, declaration := range a.collectTaggedType(effectFile, effectExport, "type", map[string]bool{}) {
			if declaration.file != "" {
				m.reads[declaration.file] = true
			}
			m.addCommandAt(declaration.tag, "effect_request", declaration.file, declaration.line, declaration.endLine)
		}
	}
	for _, typ := range []string{spec.StateType, spec.EventType, spec.EffectType} {
		if typ != "" && len(m.facts) > 0 {
			m.facts[0].Relations = append(m.facts[0].Relations, relation(facts.RelFSMTypedBy, typ))
		}
	}
	if spec.Start != "" {
		if body := functionBody(root, spec.Start, src, kinds); body != nil {
			if to := firstStateTarget(body, spec.Enter, src, kinds); to != "?" && to != "" {
				m.facts[0].Relations = append(m.facts[0].Relations, relation(facts.RelFSMInitial, machineMember(m.machine, "state", to)))
				m.facts[0].Props["initial_status"] = "resolved"
			} else {
				m.facts[0].Props["initial_status"] = "unknown"
				m.partial = true
			}
		}
	}

	handlers := reachableHandlers(root, spec, src, kinds)
	var transitions []facts.Fact
	branchCounts := map[string]int{}
	branchesSeen, unresolvedTarget, unresolvedTrigger, unclassified := 0, 0, 0, 0
	accountedReturnSites := map[uint]bool{}
	for _, handler := range handlers {
		fn := functionBody(root, handler.name, src, kinds)
		if fn == nil {
			continue
		}
		walkFunctionScope(fn, kinds, func(n *sitter.Node) {
			if kinds.Of(n) == "return_statement" && selectedReturnCallee(n, spec, src, kinds) != "" {
				accountedReturnSites[n.StartByte()] = true
			}
		})
		ret := []*sitter.Node{}
		walkFunctionScope(fn, kinds, func(n *sitter.Node) {
			if kinds.Of(n) != "return_statement" {
				return
			}
			call := transitionCall(n, spec, src, kinds)
			if call != nil {
				ret = append(ret, n)
			}
		})
		sort.Slice(ret, func(i, j int) bool { return ret[i].StartByte() < ret[j].StartByte() })
		for ordinal, r := range ret {
			branchesSeen++
			call := transitionCall(r, spec, src, kinds)
			path := appendPathConditions(handler.context, branchConditions(r, fn, handler.eventParam, handler.snapshotParam, spec.Reject, src, kinds))
			from := fromState(path, handler.snapshotParam)
			events := eventTriggers(path, handler.eventParam)
			to := transitionTarget(call, spec, from, src, kinds)
			if to == "?" {
				unresolvedTarget++
				unclassified++
			}
			if len(events) == 0 {
				unresolvedTrigger++
				unclassified++
			}
			if len(events) == 0 {
				events = []string{"?"}
			}
			toKey := to
			if toKey == "" {
				toKey = "?"
			}
			fromKey := from
			if fromKey == "" {
				fromKey = "*"
			}
			eventKey := strings.Join(sortedCopy(events), "|")
			structural := handler.name + "/" + fromKey + "/" + eventKey + "->" + toKey
			branchCounts[structural]++
			name := machineMember(m.machine, "transition", structural)
			if branchCounts[structural] > 1 {
				name += "#" + itoa(branchCounts[structural])
			}
			rels := []facts.Relation{}
			if from != "" && from != "*" {
				fromTarget := strings.TrimSuffix(from, ".*")
				rels = append(rels, relation(facts.RelFSMFrom, machineMember(m.machine, "state", fromTarget)))
			}
			if to != "?" && to != "" {
				rels = append(rels, relation(facts.RelFSMTo, machineMember(m.machine, "state", to)))
			}
			for _, event := range events {
				if event == "?" {
					continue
				}
				if handler.settlement {
					event = effectResultTag(event)
				}
				rels = append(rels, relation(facts.RelFSMOn, machineMember(m.machine, "event", event)))
			}
			props := map[string]any{
				"adapter":         AdapterReducerInterpreter,
				"handler":         handler.name,
				"branch_ordinal":  ordinal + 1,
				"from_status":     statusOf(from != "" && from != "*"),
				"to_status":       statusOf(to != "?" && to != ""),
				"trigger_status":  statusOf(len(events) > 0 && events[0] != "?"),
				"path_conditions": path,
				"identity_basis":  "handler/from/trigger-set/target",
			}
			guardConds := guardConditions(path, handler.eventParam, handler.snapshotParam)
			if len(guardConds) > 0 {
				props["guard_text"] = strings.Join(guardConds, " && ")
				props["guard_status"] = "declared"
				a.addInterpreterGuardRelations(m, &rels, guardConds, r, src, kinds)
			} else {
				props["guard_status"] = "none_detected"
			}
			props["action_line"] = nodeLine(r)
			props["action_end_line"] = int(r.EndPosition().Row) + 1
			addDirectCallRelations(&rels, call, src, kinds, facts.RelFSMActionCalls)
			for _, command := range effectCommands(call, m.commands, src, kinds) {
				rels = append(rels, relation(facts.RelFSMEmits, machineMember(m.machine, "command", command)))
			}
			rels = append(rels, relation(facts.RelFSMDeclaredIn, handler.name))
			transitions = append(transitions, facts.Fact{Kind: facts.KindFSMTransition, Name: name, File: m.file,
				Line: nodeLine(r), EndLine: int(r.EndPosition().Row) + 1, Props: props, Relations: rels})
		}
	}
	m.facts = append(m.facts, transitions...)

	// State entry effects are read from the configured entry function. The
	// source state predicate and effect request must occur in the same branch.
	entryFacts := a.entryFacts(m, root, src, kinds)
	m.facts = append(m.facts, entryFacts...)

	// A settlement's `result.type` arms describe command outcomes. Keep these
	// separate from external machine event tags.
	for _, settlement := range spec.Settlements {
		fn := namedFunction(root, settlement, src, kinds)
		params := parameterNames(fn, src, kinds)
		resultParam := "result"
		if len(params) > 1 {
			resultParam = params[1]
		} else if len(params) == 1 {
			resultParam = params[0]
		}
		for _, tag := range settlementTags(functionBody(root, settlement, src, kinds), resultParam, src, kinds) {
			resultEvent := effectResultTag(tag)
			m.addEvent(resultEvent, "effect_result", nil)
			if m.commands[tag].Kind != "" {
				addCommandRelation(m, tag, relation(facts.RelFSMOutcome, machineMember(m.machine, "event", resultEvent)))
			}
		}
	}

	enterSeen := 0
	eventConditionsSeen, eventConditionsCovered := 0, 0
	eventParametersByFunction := map[string]map[string]bool{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "function_declaration" && kinds.Of(n) != "arrow_function" && kinds.Of(n) != "function_expression" {
			return
		}
		fn := enclosingFunction(n, src, kinds)
		if fn == "" {
			return
		}
		for _, p := range namedChildren(n.ChildByFieldName("parameters")) {
			if a.typeNodeMatches(m, m.file, p.ChildByFieldName("type"), src, kinds) {
				if eventParametersByFunction[fn] == nil {
					eventParametersByFunction[fn] = map[string]bool{}
				}
				name := p.ChildByFieldName("name")
				if name == nil {
					name = p.ChildByFieldName("pattern")
				}
				if name != nil {
					eventParametersByFunction[fn][text(name, src)] = true
				}
			}
		}
	})
	reachableEventParameters := map[string]map[string]bool{}
	for _, h := range handlers {
		if h.eventParam == "" || h.settlement || !eventParametersByFunction[h.name][h.eventParam] {
			continue
		}
		if reachableEventParameters[h.name] == nil {
			reachableEventParameters[h.name] = map[string]bool{}
		}
		reachableEventParameters[h.name][h.eventParam] = true
	}
	countedEventConditions := map[uint]bool{}
	countedCoveredConditions := map[uint]bool{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) == "call_expression" && calleeName(n, src, kinds) == spec.Enter {
			enterSeen++
		}
		if kinds.Of(n) != "if_statement" {
			return
		}
		fn := enclosingFunction(n, src, kinds)
		condition := n.ChildByFieldName("condition")
		parameters := eventParametersByFunction[fn]
		if !conditionHasLiteralFieldComparison(condition, "type", src, kinds) {
			return
		}
		if !countedEventConditions[n.StartByte()] {
			countedEventConditions[n.StartByte()] = true
			eventConditionsSeen++
		}
		for param := range reachableEventParameters[fn] {
			if parameters[param] && conditionHasLiteralTypedDiscriminant(condition, "type", map[string]bool{param: true}, src, kinds) {
				if !countedCoveredConditions[n.StartByte()] {
					countedCoveredConditions[n.StartByte()] = true
					eventConditionsCovered++
				}
				break
			}
		}
	})
	classifiedSites := countCalls(functionBody(root, spec.Start, src, kinds), spec.Enter, src, kinds) + countCalls(functionBody(root, spec.Entry, src, kinds), spec.Enter, src, kinds)
	for _, h := range handlers {
		classifiedSites += countCalls(functionBody(root, h.name, src, kinds), spec.Enter, src, kinds)
	}
	for _, s := range spec.Settlements {
		classifiedSites += countCalls(functionBody(root, s, src, kinds), spec.Enter, src, kinds)
	}
	if classifiedSites > enterSeen {
		classifiedSites = enterSeen
	}
	unclassifiedSites := enterSeen - classifiedSites
	selectedReturnSites := map[uint]string{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) == "return_statement" {
			if callee := selectedReturnCallee(n, spec, src, kinds); callee != "" {
				selectedReturnSites[n.StartByte()] = callee
			}
		}
	})
	unsupportedReturnSites := len(selectedReturnSites) - len(accountedReturnSites)
	if unsupportedReturnSites < 0 {
		unsupportedReturnSites = 0
	}
	if unclassifiedSites > 0 || unclassified > 0 {
		m.partial = true
	}
	if unsupportedReturnSites > 0 {
		m.partial = true
	}
	m.coverage["branches_seen"] = branchesSeen
	m.coverage["branches_modeled"] = len(transitions)
	m.coverage["selected_return_sites_seen"] = len(selectedReturnSites)
	m.coverage["selected_return_sites_accounted"] = len(accountedReturnSites)
	m.coverage["selected_return_sites_outside_dispatch_closure"] = unsupportedReturnSites
	returnKinds := map[string]int{}
	for _, callee := range selectedReturnSites {
		kind := "other_transition"
		switch callee {
		case spec.Enter:
			kind = "enter_state"
		case spec.Reject:
			kind = "rejection"
		}
		returnKinds[kind]++
	}
	m.coverage["selected_return_site_kinds"] = returnKinds
	m.coverage["unresolved_target"] = unresolvedTarget
	m.coverage["unresolved_trigger"] = unresolvedTrigger
	m.coverage["unclassified_branch"] = unclassified
	m.coverage["enter_state_sites_seen"] = enterSeen
	m.coverage["enter_state_sites_accounted"] = classifiedSites
	m.coverage["enter_state_sites_unsupported"] = unclassifiedSites
	m.coverage["event_type_if_conditions_seen"] = eventConditionsSeen
	m.coverage["event_type_if_conditions_covered"] = eventConditionsCovered
	status := "complete"
	if m.partial {
		status = "partial"
	}
	m.coverage["coverage_status"] = status
	m.coverage["handler_coverage"] = "unknown"
	m.facts = append(m.facts, coverageFact(m, m.coverage))
}

type handlerRef struct {
	name, eventParam, snapshotParam string
	settlement                      bool
	context                         []pathCondition
}

func reachableHandlers(root *sitter.Node, spec Spec, src []byte, kinds *tsutil.KindTable) []handlerRef {
	var out []handlerRef
	seen := map[string]bool{}
	queue := []handlerRef{}
	if f := namedFunction(root, spec.Dispatcher, src, kinds); f != nil {
		event, snapshot := functionParameters(f, src, kinds)
		queue = append(queue, handlerRef{name: spec.Dispatcher, eventParam: event, snapshotParam: snapshot})
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		key := handlerContextKey(cur)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, cur)
		body := functionBody(root, cur.name, src, kinds)
		if body == nil {
			continue
		}
		walkFunctionScope(body, kinds, func(n *sitter.Node) {
			if kinds.Of(n) != "call_expression" {
				return
			}
			callee := calleeName(n, src, kinds)
			if callee == "" || callee == spec.Enter || callee == spec.Stay || callee == spec.Reject || callee == spec.Start || callee == spec.Entry {
				return
			}
			if functionBody(root, callee, src, kinds) == nil {
				return
			}
			args := callArguments(n, kinds)
			fn := namedFunction(root, callee, src, kinds)
			if fn == nil {
				return
			}
			params := parameterNames(fn, src, kinds)
			eventParam, snapshotParam := "", ""
			for i, arg := range args {
				if text(arg, src) == cur.eventParam && i < len(params) {
					eventParam = params[i]
				}
				if text(arg, src) == cur.snapshotParam && i < len(params) {
					snapshotParam = params[i]
				}
			}
			if eventParam != "" {
				callPath := branchConditions(n, body, cur.eventParam, cur.snapshotParam, spec.Reject, src, kinds)
				context := appendPathConditions(cur.context, callPath)
				child := handlerRef{name: callee, eventParam: eventParam, snapshotParam: snapshotParam, context: context}
				if !seen[handlerContextKey(child)] {
					queue = append(queue, child)
				}
			}
		})
	}
	for _, name := range spec.Settlements {
		if functionBody(root, name, src, kinds) == nil {
			continue
		}
		fn := namedFunction(root, name, src, kinds)
		params := parameterNames(fn, src, kinds)
		event, snapshot := "", ""
		if len(params) > 1 {
			snapshot = params[0]
			event = params[1]
		} else if len(params) > 0 {
			event = params[0]
		}
		out = append(out, handlerRef{name: name, eventParam: event, snapshotParam: snapshot, settlement: true})
	}
	return out
}

func appendPathConditions(base, added []pathCondition) []pathCondition {
	out := append([]pathCondition(nil), base...)
	seen := map[string]bool{}
	for _, condition := range out {
		seen[condition.Branch+"\x00"+condition.Text] = true
	}
	for _, condition := range added {
		key := condition.Branch + "\x00" + condition.Text
		if !seen[key] {
			out = append(out, condition)
			seen[key] = true
		}
	}
	return out
}

func handlerContextKey(h handlerRef) string {
	var b strings.Builder
	b.WriteString(h.name)
	b.WriteByte('\x00')
	b.WriteString(h.eventParam)
	b.WriteByte('\x00')
	b.WriteString(h.snapshotParam)
	b.WriteByte('\x00')
	for _, condition := range h.context {
		b.WriteString(condition.Branch)
		b.WriteByte(':')
		b.WriteString(condition.Text)
		b.WriteByte('\x00')
	}
	return b.String()
}

func functionParameters(fn *sitter.Node, src []byte, kinds *tsutil.KindTable) (event, snapshot string) {
	params := parameterNames(fn, src, kinds)
	if len(params) > 0 {
		snapshot = params[0]
	}
	if len(params) > 1 {
		event = params[1]
	}
	return
}

func parameterNames(fn *sitter.Node, src []byte, kinds *tsutil.KindTable) []string {
	params := fn.ChildByFieldName("parameters")
	var out []string
	if params == nil && kinds.Of(fn) == "arrow_function" {
		if parameter := fn.ChildByFieldName("parameter"); parameter != nil {
			name := parameter.ChildByFieldName("name")
			if name == nil {
				name = parameter.ChildByFieldName("pattern")
			}
			if name == nil {
				name = parameter
			}
			return []string{text(name, src)}
		}
	}
	for _, p := range namedChildren(params) {
		name := p.ChildByFieldName("name")
		if name == nil {
			name = p.ChildByFieldName("pattern")
		}
		if name != nil {
			out = append(out, text(name, src))
		}
	}
	return out
}

func transitionCall(ret *sitter.Node, spec Spec, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	var found *sitter.Node
	walk(ret, func(n *sitter.Node) {
		if found != nil || kinds.Of(n) != "call_expression" {
			return
		}
		name := calleeName(n, src, kinds)
		if name == spec.Enter || (spec.Stay != "" && name == spec.Stay) {
			found = n
		}
	})
	return found
}

func selectedReturnCallee(ret *sitter.Node, spec Spec, src []byte, kinds *tsutil.KindTable) string {
	if ret == nil || kinds.Of(ret) != "return_statement" {
		return ""
	}
	expr := unwrapExpression(returnValue(ret), kinds)
	if expr == nil || kinds.Of(expr) != "call_expression" {
		return ""
	}
	name := calleeName(expr, src, kinds)
	if name == spec.Enter || name == spec.Reject || strings.HasPrefix(name, "transition") && len(name) > len("transition") && name[len("transition")] >= 'A' && name[len("transition")] <= 'Z' {
		return name
	}
	return ""
}

type pathCondition struct {
	Text        string   `json:"text"`
	Line        int      `json:"line"`
	Branch      string   `json:"branch"`
	triggerTags []string `json:"-"`
}

func branchConditions(ret, fn *sitter.Node, eventParam, snapshotParam, rejectName string, src []byte, kinds *tsutil.KindTable) []pathCondition {
	var reverse []pathCondition
	for p := ret.Parent(); p != nil && !sameSyntaxNode(p, fn); p = p.Parent() {
		if kinds.Of(p) == "if_statement" {
			cond := p.ChildByFieldName("condition")
			branch := "true"
			if alt := p.ChildByFieldName("alternative"); alt != nil && alt.StartByte() <= ret.StartByte() && alt.EndByte() >= ret.EndByte() {
				branch = "false"
			}
			truth := branch != "false"
			reverse = append(reverse, pathCondition{Text: text(cond, src), Line: nodeLine(cond), Branch: branch,
				triggerTags: eventDiscriminantTags(cond, eventParam, truth, src, kinds)})
		}
		if kinds.Of(p) == "switch_case" {
			value := p.ChildByFieldName("value")
			if value != nil && switchDiscriminant(p, eventParam, fn, src, kinds) {
				triggerTags := []string{}
				if tag, ok := stringValue(value, src, kinds); ok {
					triggerTags = append(triggerTags, tag)
				}
				reverse = append(reverse, pathCondition{Text: "case " + text(value, src), Line: nodeLine(value), Branch: "case", triggerTags: triggerTags})
			}
		}
	}
	for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}
	// Sequential reject guards constrain a later successful return even though
	// their `if` node is a preceding sibling, not an ancestor of the return.
	for p := ret.Parent(); p != nil && !sameSyntaxNode(p, fn); p = p.Parent() {
		if kinds.Of(p) != "statement_block" {
			continue
		}
		for _, sibling := range namedChildren(p) {
			if sibling.StartByte() >= ret.StartByte() {
				break
			}
			if kinds.Of(sibling) != "if_statement" {
				continue
			}
			if returnsOnlyReject(sibling.ChildByFieldName("consequence"), rejectName, src, kinds) {
				condition := sibling.ChildByFieldName("condition")
				cond := text(condition, src)
				reverse = append(reverse, pathCondition{Text: "!(" + cond + ")", Line: nodeLine(condition), Branch: "after_reject",
					triggerTags: eventDiscriminantTags(condition, eventParam, false, src, kinds)})
			}
		}
	}
	return reverse
}

func returnsOnlyReject(n *sitter.Node, reject string, src []byte, kinds *tsutil.KindTable) bool {
	if n == nil {
		return false
	}
	var returns, rejects int
	walk(n, func(c *sitter.Node) {
		if kinds.Of(c) != "return_statement" {
			return
		}
		returns++
		walk(c, func(call *sitter.Node) {
			if kinds.Of(call) == "call_expression" && calleeName(call, src, kinds) == reject {
				rejects++
			}
		})
	})
	return returns > 0 && returns == rejects
}

func eventTriggers(path []pathCondition, eventParam string) []string {
	if eventParam == "" {
		return nil
	}
	var out []string
	for _, p := range path {
		if p.triggerTags != nil {
			out = append(out, p.triggerTags...)
			continue
		}
		// Keep this helper useful to focused callers that construct pathCondition
		// values directly. Production paths carry AST-derived tags and do not
		// reparse condition text.
		out = append(out, eventTagsFromConditionText(p.Text, eventParam, p.Branch == "false" || p.Branch == "after_reject")...)
	}
	return unique(out)
}

func switchDiscriminant(node *sitter.Node, eventParam string, fn *sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	if node == nil || eventParam == "" {
		return false
	}
	for p := node.Parent(); p != nil && !sameSyntaxNode(p, fn); p = p.Parent() {
		if kinds.Of(p) == "switch_statement" {
			return isEventDiscriminant(p.ChildByFieldName("value"), eventParam, src, kinds)
		}
	}
	return false
}

func eventDiscriminantTags(condition *sitter.Node, eventParam string, truth bool, src []byte, kinds *tsutil.KindTable) []string {
	if condition == nil || eventParam == "" {
		return []string{}
	}
	out := []string{}
	var collect func(*sitter.Node, bool)
	collect = func(node *sitter.Node, expected bool) {
		for node != nil && kinds.Of(node) == "parenthesized_expression" {
			var inner *sitter.Node
			for _, child := range namedChildren(node) {
				if kinds.Of(child) != "comment" {
					inner = child
					break
				}
			}
			if inner == nil {
				break
			}
			node = inner
		}
		node = unwrapExpression(node, kinds)
		if node == nil {
			return
		}
		if kinds.Of(node) == "unary_expression" && node.ChildCount() > 0 && node.Child(0).Kind() == "!" {
			collect(node.ChildByFieldName("argument"), !expected)
			return
		}
		if kinds.Of(node) != "binary_expression" {
			return
		}
		operator := binaryOperator(node)
		if operator == "&&" || operator == "||" {
			// A false compound branch can be explained by any operand. It
			// establishes no particular event tag, so keep it unknown.
			if expected {
				collect(node.ChildByFieldName("left"), true)
				collect(node.ChildByFieldName("right"), true)
			}
			return
		}
		if operator != "===" && operator != "==" && operator != "!==" && operator != "!=" {
			return
		}
		left, right := node.ChildByFieldName("left"), node.ChildByFieldName("right")
		var literal *sitter.Node
		if isEventDiscriminant(left, eventParam, src, kinds) {
			literal = right
		} else if isEventDiscriminant(right, eventParam, src, kinds) {
			literal = left
		} else {
			return
		}
		tag, ok := stringValue(literal, src, kinds)
		if !ok {
			return
		}
		equality := operator == "===" || operator == "=="
		if equality == expected {
			out = append(out, tag)
		}
	}
	collect(condition, truth)
	return unique(out)
}

func binaryOperator(node *sitter.Node) string {
	if node == nil || node.ChildCount() < 3 {
		return ""
	}
	return node.Child(1).Kind()
}

func isEventDiscriminant(node *sitter.Node, eventParam string, src []byte, kinds *tsutil.KindTable) bool {
	node = unwrapExpression(node, kinds)
	if node == nil || kinds.Of(node) != "member_expression" || text(node.ChildByFieldName("property"), src) != "type" {
		return false
	}
	object := unwrapExpression(node.ChildByFieldName("object"), kinds)
	return object != nil && kinds.Of(object) == "identifier" && text(object, src) == eventParam
}

func eventTagsFromConditionText(condition, eventParam string, falseBranch bool) []string {
	wrapped := []byte("function __fsm(event){if (" + condition + ") { return; }}")
	root, kinds, done := parse("__fsm_trigger.ts", wrapped)
	if done == nil {
		return nil
	}
	defer done()
	var cond *sitter.Node
	walk(root, func(n *sitter.Node) {
		if cond == nil && kinds.Of(n) == "if_statement" {
			cond = n.ChildByFieldName("condition")
		}
	})
	return eventDiscriminantTags(cond, eventParam, !falseBranch, wrapped, kinds)
}

func fromState(path []pathCondition, snapshotParam string) string {
	if snapshotParam == "" {
		return ""
	}
	for _, p := range path {
		if p.Branch == "false" {
			continue
		}
		if !strings.Contains(p.Text, snapshotParam) {
			continue
		}
		if match := stateObjectRE.FindStringSubmatch(p.Text); len(match) == 3 {
			return match[1] + "." + match[2]
		}
		if match := stateMatchRE.FindStringSubmatch(p.Text); len(match) == 3 {
			if match[1] != "" {
				return match[1] + "." + match[2]
			}
			if strings.Contains(p.Text, ".matches") {
				return match[2] + ".*"
			}
			return match[2]
		}
		if strings.Contains(p.Text, snapshotParam+".value") {
			for _, m := range stringLiteralRE.FindAllStringSubmatch(p.Text, -1) {
				if len(m) == 2 {
					return m[1]
				}
			}
		}
	}
	return ""
}

func guardConditions(path []pathCondition, eventParam, snapshotParam string) []string {
	var out []string
	for _, p := range path {
		if p.Branch == "case" {
			continue
		}
		if selectorOnlyCondition(p.Text, eventParam, snapshotParam) {
			continue
		}
		if strings.HasPrefix(p.Text, "!(") && strings.HasSuffix(p.Text, ")") {
			inner := strings.TrimSuffix(strings.TrimPrefix(p.Text, "!("), ")")
			if strings.HasPrefix(strings.TrimSpace(inner), "!") {
				p.Text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(inner), "!"))
			}
		}
		out = append(out, p.Text)
	}
	return unique(out)
}

// Mixed predicates retain their complete expression: removing selector terms
// from an OR or negation would change the meaning of the remaining guard.
func selectorOnlyCondition(condition, eventParam, snapshotParam string) bool {
	source := []byte(condition)
	root, kinds, closeTree := parse("condition.ts", source)
	if root == nil {
		return false
	}
	defer closeTree()
	var literal func(*sitter.Node) bool
	literal = func(n *sitter.Node) bool {
		if n == nil {
			return false
		}
		switch kinds.Of(n) {
		case "string", "number", "true", "false", "null":
			return true
		case "object":
			for _, pair := range namedChildren(n) {
				if kinds.Of(pair) != "pair" || !literal(pair.ChildByFieldName("value")) {
					return false
				}
			}
			return true
		}
		return false
	}
	selector := func(n *sitter.Node) bool {
		if n == nil || kinds.Of(n) != "member_expression" {
			return false
		}
		object, property := n.ChildByFieldName("object"), n.ChildByFieldName("property")
		if kinds.Of(object) != "identifier" {
			return false
		}
		return (eventParam != "" && text(object, source) == eventParam && text(property, source) == "type") ||
			(snapshotParam != "" && text(object, source) == snapshotParam && text(property, source) == "value")
	}
	var only func(*sitter.Node) bool
	only = func(n *sitter.Node) bool {
		if n == nil {
			return false
		}
		switch kinds.Of(n) {
		case "program", "expression_statement", "parenthesized_expression":
			children := namedChildren(n)
			return len(children) == 1 && only(children[0])
		case "unary_expression":
			return text(n.ChildByFieldName("operator"), source) == "!" && only(n.ChildByFieldName("argument"))
		case "binary_expression":
			left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
			switch binaryOperator(n) {
			case "&&", "||":
				return only(left) && only(right)
			case "===", "!==", "==", "!=":
				return (selector(left) && literal(right)) || (literal(left) && selector(right))
			}
		case "call_expression":
			fn := n.ChildByFieldName("function")
			if fn == nil || kinds.Of(fn) != "member_expression" || snapshotParam == "" {
				return false
			}
			object := fn.ChildByFieldName("object")
			if kinds.Of(object) != "identifier" || text(object, source) != snapshotParam || text(fn.ChildByFieldName("property"), source) != "matches" {
				return false
			}
			args := callArguments(n, kinds)
			return len(args) == 1 && literal(args[0])
		}
		return false
	}
	return only(root)
}

func transitionTarget(call *sitter.Node, spec Spec, from string, src []byte, kinds *tsutil.KindTable) string {
	if calleeName(call, src, kinds) == spec.Stay {
		if from != "" && !strings.HasSuffix(from, ".*") {
			return from
		}
		return "?"
	}
	args := callArguments(call, kinds)
	if len(args) == 0 {
		return "?"
	}
	return stateValue(args[0], src, kinds)
}

func stateValue(n *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	if s, ok := stringValue(n, src, kinds); ok {
		return s
	}
	if kinds.Of(n) == "object" {
		parts := []string{}
		for _, p := range namedChildren(n) {
			if kinds.Of(p) != "pair" {
				continue
			}
			key, val := p.ChildByFieldName("key"), p.ChildByFieldName("value")
			k := text(key, src)
			v, ok := stringValue(val, src, kinds)
			if !ok {
				return "?"
			}
			parts = append(parts, k+"."+v)
		}
		if len(parts) == 1 {
			return parts[0]
		}
	}
	return "?"
}

func statusOf(ok bool) string {
	if ok {
		return "resolved"
	}
	return "unknown"
}

func (a *Analyzer) addInterpreterGuardRelations(m *machineModel, rels *[]facts.Relation, conds []string, site *sitter.Node, original []byte, originalKinds *tsutil.KindTable) {
	seen := map[string]bool{}
	for _, cond := range conds {
		source := []byte(cond)
		root, kinds, closeTree := parse(m.file, source)
		if root == nil {
			continue
		}
		walk(root, func(n *sitter.Node) {
			if kinds.Of(n) != "call_expression" {
				return
			}
			fn := n.ChildByFieldName("function")
			if fn == nil || kinds.Of(fn) != "identifier" {
				return
			}
			name := text(fn, source)
			if seen[name] {
				return
			}
			seen[name] = true
			if binding := localBindingAt(site, name, original, originalKinds); binding != nil {
				parent := binding.Parent()
				for parent != nil && (originalKinds.Of(parent) == "lexical_declaration" || originalKinds.Of(parent) == "variable_declaration" || originalKinds.Of(parent) == "export_statement") {
					parent = parent.Parent()
				}
				if parent == nil || originalKinds.Of(parent) != "program" {
					m.partial = true
					m.coverage["unresolved_guard_calls"] = 1
					return
				}
			}
			a.appendRuleTableSymbolRef(m, rels, name, facts.RelFSMGuardCalls, "unresolved_guard_calls")
		})
		closeTree()
	}
}

func effectCommands(node *sitter.Node, commands map[string]facts.Fact, src []byte, kinds *tsutil.KindTable) []string {
	var out []string
	walkFunctionScope(node, kinds, func(n *sitter.Node) {
		if kinds.Of(n) != "object" {
			return
		}
		if tag, ok := stringValue(objectPair(n, "type", src, kinds), src, kinds); ok && commands[tag].Kind != "" {
			out = append(out, tag)
		}
	})
	return unique(out)
}

func settlementTags(node *sitter.Node, resultParam string, src []byte, kinds *tsutil.KindTable) []string {
	var out []string
	walkFunctionScope(node, kinds, func(n *sitter.Node) {
		if kinds.Of(n) != "switch_case" {
			return
		}
		for p := n.Parent(); p != nil; p = p.Parent() {
			if kinds.Of(p) != "switch_statement" {
				continue
			}
			if !strings.Contains(text(p.ChildByFieldName("value"), src), resultParam+".type") {
				return
			}
			if v := n.ChildByFieldName("value"); v != nil {
				if tag, ok := stringValue(v, src, kinds); ok {
					out = append(out, tag)
				}
			}
			return
		}
	})
	return unique(out)
}

func effectResultTag(tag string) string { return "$effect-result:" + tag }

func countCalls(node *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) int {
	count := 0
	walkFunctionScope(node, kinds, func(n *sitter.Node) {
		if kinds.Of(n) == "call_expression" && calleeName(n, src, kinds) == name {
			count++
		}
	})
	return count
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
