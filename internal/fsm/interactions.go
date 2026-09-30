package fsm

import (
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (a *Analyzer) extractInteractions(m *machineModel, relFile string, src []byte, includeSourceReturns bool) ([]facts.Fact, map[string]bool) {
	relFile = slash(relFile)
	root, kinds, done := parse(relFile, src)
	if done == nil {
		return nil, nil
	}
	defer done()
	var out []facts.Fact
	reads := map[string]bool{}
	if containsPath(m.spec.DispatchFiles, relFile) {
		constructed, rr := a.extractEventConstructions(m, relFile, root, src, kinds)
		out = append(out, constructed...)
		for f := range rr {
			reads[f] = true
		}
	}
	if !a.collectingConstructorUses && includeSourceReturns {
		constructed, rr := a.extractSourceReturnConstructions(m, relFile, root, src, kinds)
		out = append(out, constructed...)
		for f := range rr {
			reads[f] = true
		}
	}
	for _, sink := range m.spec.DispatchSinks {
		if sink.MachineTag != nil {
			ff, rr := a.extractEffectSink(m, relFile, root, src, kinds, sink)
			out = append(out, ff...)
			for f := range rr {
				reads[f] = true
			}
		} else {
			ff, rr := a.extractObjectSink(m, relFile, root, src, kinds, sink)
			out = append(out, ff...)
			for f := range rr {
				reads[f] = true
			}
		}
	}
	if containsPath(m.spec.HandlerFiles, relFile) || (m.spec.EffectRunner != nil && slash(m.spec.EffectRunner.Module) == relFile) {
		out = append(out, a.extractCommandHandlers(m, relFile, root, src, kinds)...)
	}
	return bindNestedSourceDeclarations(relFile, root, src, kinds, out), reads
}

// bindNestedSourceDeclarations gives nested callable evidence a stable,
// source-derived symbol identity. The ordinary TS extractor intentionally
// indexes top-level declarations only; FSM interaction overlays inside a
// nested callable therefore carry this exact AST declaration to the merger.
func bindNestedSourceDeclarations(file string, root *sitter.Node, src []byte, kinds *tsutil.KindTable, overlays []facts.Fact) []facts.Fact {
	sourceFacts := []facts.Fact{}
	seen := map[string]bool{}
	for i := range overlays {
		overlay := &overlays[i]
		if overlay.Kind != facts.KindSymbol || overlay.File != file || len(overlay.Relations) == 0 || overlay.Props["fsm_evidence"] != "direct_source_binding" {
			continue
		}
		declaration := functionAtLine(root, overlay.Name, overlay.Line, src, kinds)
		if declaration == nil {
			continue
		}
		chain, ok := namedFunctionChain(declaration, src, kinds)
		if !ok || len(chain) < 2 {
			continue
		}
		identity := moduleName(file) + "." + strings.Join(chain, ".")
		if !uniqueFunctionChain(root, chain, src, kinds) {
			continue
		}
		props := make(map[string]any, len(overlay.Props)+2)
		for key, value := range overlay.Props {
			props[key] = value
		}
		props["fsm_source_identity"] = identity
		props["fsm_source_binding"] = "tree_sitter_nested_declaration"
		overlay.Props = props
		if seen[identity] {
			continue
		}
		seen[identity] = true
		sourceFacts = append(sourceFacts, facts.Fact{Kind: facts.KindSymbol, Name: identity, File: file,
			Line: int(declaration.StartPosition().Row) + 1, EndLine: int(declaration.EndPosition().Row) + 1,
			Props: map[string]any{"language": "typescript", "symbol_kind": facts.SymbolFunc, "fsm_source_binding": "tree_sitter_nested_declaration"}})
	}
	return append(sourceFacts, overlays...)
}

func functionAtLine(root *sitter.Node, name string, line int, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if root == nil || name == "" || line <= 0 {
		return nil
	}
	var selected *sitter.Node
	var selectedSize uint
	ambiguous := false
	walk(root, func(node *sitter.Node) {
		if !isFunctionBoundary(kinds.Of(node)) || functionSymbolName(node, src, kinds) != name {
			return
		}
		start, end := int(node.StartPosition().Row)+1, int(node.EndPosition().Row)+1
		if line < start || line > end {
			return
		}
		size := node.EndByte() - node.StartByte()
		if selected == nil || size < selectedSize {
			selected, selectedSize, ambiguous = node, size, false
		} else if size == selectedSize && !sameSyntaxNode(node, selected) {
			ambiguous = true
		}
	})
	if ambiguous {
		return nil
	}
	return selected
}

func functionSymbolName(node *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	if node == nil {
		return ""
	}
	switch kinds.Of(node) {
	case "function_declaration", "generator_function_declaration":
		return functionName(node, src)
	case "method_definition":
		return text(node.ChildByFieldName("name"), src)
	case "arrow_function", "function_expression":
		parent := node.Parent()
		value := parent.ChildByFieldName("value")
		if kinds.Of(parent) == "variable_declarator" && sameSyntaxNode(value, node) {
			return text(parent.ChildByFieldName("name"), src)
		}
		if kinds.Of(parent) == "pair" && sameSyntaxNode(value, node) {
			return text(parent.ChildByFieldName("key"), src)
		}
	}
	return ""
}

func sameSyntaxNode(left, right *sitter.Node) bool {
	return left != nil && right != nil && left.Id() == right.Id()
}

func namedFunctionChain(node *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]string, bool) {
	var reversed []string
	for parent := node; parent != nil; parent = parent.Parent() {
		if !isFunctionBoundary(kinds.Of(parent)) {
			continue
		}
		name := functionSymbolName(parent, src, kinds)
		if name == "" {
			return nil, false
		}
		reversed = append(reversed, name)
	}
	if len(reversed) == 0 {
		return nil, false
	}
	chain := make([]string, len(reversed))
	for i := range reversed {
		chain[len(reversed)-1-i] = reversed[i]
	}
	return chain, true
}

func uniqueFunctionChain(root *sitter.Node, expected []string, src []byte, kinds *tsutil.KindTable) bool {
	count := 0
	walk(root, func(node *sitter.Node) {
		if !isFunctionBoundary(kinds.Of(node)) {
			return
		}
		chain, ok := namedFunctionChain(node, src, kinds)
		if ok && strings.Join(chain, "\x00") == strings.Join(expected, "\x00") {
			count++
		}
	})
	return count == 1
}

func (a *Analyzer) extractObjectSink(m *machineModel, rel string, root *sitter.Node, src []byte, kinds *tsutil.KindTable, sink DispatchSink) ([]facts.Fact, map[string]bool) {
	reads := map[string]bool{}
	var out []facts.Fact
	actors := map[string]bool{}
	factoryLocal := map[string]bool{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" {
			return
		}
		local := calleeName(n, src, kinds)
		resolved, ok := a.resolvedImport(rel, local)
		if ok && a.matchesSymbolRef(resolved, sink.Factory) {
			factoryLocal[local] = true
			reads[targetModule(resolved)] = true
		}
	})
	if len(factoryLocal) == 0 {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "unresolved_import_or_call", "dispatch_unresolved_sink": 1})}, reads
	}
	provenFactory := false
	for _, local := range sortedKeys(factoryLocal) {
		if !a.factoryReturnsMethod(sink.Factory, sink.Method) {
			continue
		}
		provenFactory = true
		walk(root, func(n *sitter.Node) {
			if kinds.Of(n) != "variable_declarator" {
				return
			}
			value := n.ChildByFieldName("value")
			if value == nil {
				return
			}
			containsCallName(value, local, src, kinds, func(call *sitter.Node) { actors[text(n.ChildByFieldName("name"), src)] = true })
		})
	}
	if !provenFactory {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "factory_return_method_unproven", "dispatch_unresolved_sink": 1})}, reads
	}
	if len(actors) == 0 {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "local_factory_binding_unresolved", "dispatch_unresolved_sink": 1})}, reads
	}
	actors = compactNonempty(actors)
	sendAliases := map[string][]*sitter.Node{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "variable_declarator" {
			return
		}
		value := n.ChildByFieldName("value")
		if value == nil || kinds.Of(value) != "member_expression" {
			return
		}
		if actors[text(value.ChildByFieldName("object"), src)] && text(value.ChildByFieldName("property"), src) == sink.Method {
			name := text(n.ChildByFieldName("name"), src)
			sendAliases[name] = append(sendAliases[name], n)
		}
	})
	wrappers := map[string][]*sitter.Node{}
	walk(root, func(n *sitter.Node) {
		name := ""
		var value *sitter.Node
		if kinds.Of(n) == "variable_declarator" {
			name, value = text(n.ChildByFieldName("name"), src), n.ChildByFieldName("value")
		} else if kinds.Of(n) == "function_declaration" {
			name, value = functionName(n, src), n
		}
		if name == "" || value == nil || !functionLike(value, kinds) {
			return
		}
		eventParam := typedEventParameter(value, m, rel, src, kinds, a)
		forwards, forwardingReads := a.wrapperForwardsEvent(m, rel, root, value, sendAliases, sink.EventArgument, eventParam, src, kinds)
		for file := range forwardingReads {
			reads[file] = true
		}
		if eventParam == "" || !forwards {
			return
		}
		wrappers[name] = append(wrappers[name], n)
	})
	if len(sendAliases) == 0 {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "send_binding_unresolved", "dispatch_unresolved_sink": 1})}, reads
	}
	// Calls inside the adapter wrapper carry a generic parameter, not a
	// concrete construction site. Concrete call sites to that proven wrapper
	// are the dispatch evidence.
	var unknown int
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" {
			return
		}
		callee := calleeName(n, src, kinds)
		if provenLocalBindingAt(n.ChildByFieldName("function"), callee, wrappers, src, kinds) {
			if callInsideProvenWrapper(n, wrappers, src, kinds) {
				return
			}
			args := callArguments(n, kinds)
			constructed, constructionReads := a.eventArgumentConstructionFacts(m, rel, args, src, kinds)
			out = append(out, constructed...)
			for f := range constructionReads {
				reads[f] = true
			}
			tags, resolved, calleeFile, typeReads := a.eventArgumentTagsWithReads(m, rel, args, src, kinds)
			if calleeFile != "" {
				reads[calleeFile] = true
			}
			for _, file := range typeReads {
				reads[file] = true
			}
			if !resolved || len(tags) == 0 {
				unknown++
				out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMDispatchesUnknownEvent, m.machine, n, map[string]any{"dispatch_status": "unknown_event"}))
				return
			}
			for _, tag := range tags {
				if m.events[tag].Kind == "" {
					unknown++
					out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMDispatchesUnknownEvent, m.machine, n, map[string]any{"dispatch_status": "unrecognized_event_tag", "event_tag": tag}))
					continue
				}
				out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMDispatches, machineMember(m.machine, "event", tag), n, map[string]any{"dispatch_status": "resolved", "event_tag": tag}))
			}
			return
		}
		if configuredSendAliasAt(n, callee, sendAliases, src, kinds) && !callInsideProvenWrapper(n, wrappers, src, kinds) {
			// Direct actor.send call, outside the forwarding wrapper.
			args := callArguments(n, kinds)
			constructed, constructionReads := a.eventArgumentConstructionFacts(m, rel, args, src, kinds)
			out = append(out, constructed...)
			for f := range constructionReads {
				reads[f] = true
			}
			tags, resolved, calleeFile, typeReads := a.eventArgumentTagsWithReads(m, rel, args, src, kinds)
			if calleeFile != "" {
				reads[calleeFile] = true
			}
			for _, file := range typeReads {
				reads[file] = true
			}
			if !resolved || len(tags) == 0 {
				unknown++
				out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMDispatchesUnknownEvent, m.machine, n, map[string]any{"dispatch_status": "unknown_event"}))
				return
			}
			for _, tag := range tags {
				if m.events[tag].Kind == "" {
					unknown++
					continue
				}
				out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMDispatches, machineMember(m.machine, "event", tag), n, map[string]any{"dispatch_status": "resolved", "event_tag": tag}))
			}
		}
	})
	if unknown > 0 {
		out = append(out, dispatchCoverageFact(m, rel, map[string]any{"coverage_status": "partial", "dispatch_unresolved_event": unknown}))
	}
	return out, reads
}

func (a *Analyzer) factoryReturnsMethod(ref SymbolRef, method string) bool {
	file := slash(ref.Module)
	exportName := ref.Export
	if target, ok := a.resolveExport(file, ref.Export, map[string]bool{}); ok {
		file, exportName = targetModule(target), targetExport(target)
	}
	src := a.source(file)
	if len(src) == 0 {
		return false
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return false
	}
	defer done()
	body := functionBody(root, exportName, src, kinds)
	if body == nil {
		return false
	}
	if kinds.Of(body) != "statement_block" {
		return returnedObjectHasMethod(body, method, src, kinds)
	}
	returns, allCallable := 0, true
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if kinds.Of(n) == "return_statement" {
			returns++
			if !returnedObjectHasMethod(returnValue(n), method, src, kinds) {
				allCallable = false
			}
		}
	})
	return returns > 0 && allCallable && functionDefinitelyReturns(body, kinds)
}

func returnedObjectHasMethod(value *sitter.Node, method string, src []byte, kinds *tsutil.KindTable) bool {
	object := unwrapExpression(value, kinds)
	if object == nil || kinds.Of(object) != "object" {
		return false
	}
	for _, member := range namedChildren(object) {
		switch kinds.Of(member) {
		case "method_definition":
			if text(member.ChildByFieldName("name"), src) == method {
				return true
			}
		case "pair":
			if text(member.ChildByFieldName("key"), src) == method {
				value := unwrapExpression(member.ChildByFieldName("value"), kinds)
				return value != nil && (kinds.Of(value) == "arrow_function" || kinds.Of(value) == "function_expression")
			}
		}
	}
	return false
}

func (a *Analyzer) eventArgumentTagsWithReads(m *machineModel, from string, args []*sitter.Node, src []byte, kinds *tsutil.KindTable) ([]string, bool, string, []string) {
	if len(args) == 0 {
		return nil, false, "", nil
	}
	expr := unwrapExpression(args[0], kinds)
	if tag, ok := eventObjectTag(expr, src, kinds); ok {
		return []string{tag}, true, "", nil
	}
	if kinds.Of(expr) == "identifier" {
		name := text(expr, src)
		value := variableValueFromFunction(expr, name, src, kinds)
		if value != nil {
			return a.eventArgumentTagsWithReads(m, from, []*sitter.Node{unwrapExpression(value, kinds)}, src, kinds)
		}
	}
	if kinds.Of(expr) == "call_expression" {
		if tag, dep, ok := a.eventConstructorTag(m, from, expr, src, kinds); ok {
			return []string{tag}, true, dep, nil
		}
		name := calleeName(expr, src, kinds)
		resolved, ok := a.resolvedImport(from, name)
		if !ok {
			return nil, false, "", nil
		}
		calleeFile, exportName := targetModule(resolved), targetExport(resolved)
		if calleeFile == "" || exportName == "" {
			return nil, false, "", nil
		}
		source := a.source(calleeFile)
		if len(source) == 0 {
			return nil, false, calleeFile, nil
		}
		root, calleeKinds, done := parse(calleeFile, source)
		if done == nil {
			return nil, false, calleeFile, nil
		}
		defer done()
		body := functionBody(root, exportName, source, calleeKinds)
		if body == nil {
			return nil, false, calleeFile, nil
		}
		tags := []string{}
		unknown := false
		var returns []*sitter.Node
		var typeReads []string
		if calleeKinds.Of(body) == "statement_block" {
			walkFunctionScope(body, calleeKinds, func(n *sitter.Node) {
				if calleeKinds.Of(n) == "return_statement" {
					returns = append(returns, returnValue(n))
				}
			})
			complete := functionDefinitelyReturns(body, calleeKinds)
			if !complete {
				if fn := namedFunction(root, exportName, source, calleeKinds); fn != nil {
					var exhaustive bool
					exhaustive, typeReads = a.functionSwitchExhaustive(calleeFile, fn, body, source, calleeKinds)
					complete = exhaustive
				}
			}
			if !complete {
				unknown = true
			}
		} else {
			returns = append(returns, body)
		}
		if len(returns) == 0 {
			unknown = true
		}
		for _, expr := range returns {
			if tag, ok := eventObjectTag(unwrapExpression(expr, calleeKinds), source, calleeKinds); ok {
				tags = append(tags, tag)
			} else {
				unknown = true
			}
		}
		return unique(tags), len(tags) > 0 && !unknown, calleeFile, typeReads
	}
	return nil, false, "", nil
}

func (a *Analyzer) wrapperForwardsEvent(m *machineModel, file string, root, fn *sitter.Node, sends map[string][]*sitter.Node, eventArgument int, eventParam string, src []byte, kinds *tsutil.KindTable) (bool, map[string]bool) {
	reads := map[string]bool{}
	if fn == nil || eventArgument < 0 || eventParam == "" {
		return false, reads
	}
	body := fn.ChildByFieldName("body")
	forwarded := false
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if forwarded || kinds.Of(n) != "call_expression" || !configuredSendAliasAt(n, calleeName(n, src, kinds), sends, src, kinds) {
			return
		}
		args := callArguments(n, kinds)
		if eventArgument >= len(args) {
			return
		}
		forwarded = a.wrapperValuePreservesParameter(m, file, root, args[eventArgument], fn, eventParam, src, kinds, reads)
	})
	return forwarded, reads
}

// wrapperValuePreservesParameter follows only value-preserving forms at the
// configured send argument. Arbitrary nested references (for example
// send({type: "BOOT", metadata: event}) or send(discard(event))) do not prove
// that the caller's event reaches the machine.
func (a *Analyzer) wrapperValuePreservesParameter(m *machineModel, file string, root, value, wrapper *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable, reads map[string]bool) bool {
	value = unwrapExpression(value, kinds)
	if value == nil {
		return false
	}
	switch kinds.Of(value) {
	case "identifier":
		return text(value, src) == parameter && parameterReference(value, wrapper, parameter, src, kinds)
	case "object":
		return objectPreservesParameter(value, wrapper, parameter, src, kinds)
	case "conditional_expression":
		return a.wrapperValuePreservesParameter(m, file, root, value.ChildByFieldName("consequence"), wrapper, parameter, src, kinds, reads) &&
			a.wrapperValuePreservesParameter(m, file, root, value.ChildByFieldName("alternative"), wrapper, parameter, src, kinds, reads)
	case "call_expression":
		return a.forwardingHelperCall(m, file, root, value, wrapper, parameter, src, kinds, reads)
	default:
		return false
	}
}

func objectPreservesParameter(object, fn *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable) bool {
	spreadSeen := false
	for _, member := range namedChildren(object) {
		if kinds.Of(member) == "comment" {
			continue
		}
		if kinds.Of(member) == "spread_element" {
			argument := member.ChildByFieldName("argument")
			if argument == nil && member.NamedChildCount() > 0 {
				argument = member.NamedChild(0)
			}
			argument = unwrapExpression(argument, kinds)
			if spreadSeen {
				// A later spread can overwrite the event discriminator.
				return false
			}
			if kinds.Of(argument) == "identifier" && text(argument, src) == parameter && parameterReference(argument, fn, parameter, src, kinds) {
				spreadSeen = true
			}
			continue
		}
		if !spreadSeen {
			continue
		}
		key, known := staticObjectMemberKey(member, src, kinds)
		if !known || key == "type" {
			return false
		}
	}
	return spreadSeen
}

func staticObjectMemberKey(member *sitter.Node, src []byte, kinds *tsutil.KindTable) (string, bool) {
	if member == nil {
		return "", false
	}
	var key *sitter.Node
	switch kinds.Of(member) {
	case "pair":
		key = member.ChildByFieldName("key")
	case "method_definition":
		key = member.ChildByFieldName("name")
	case "shorthand_property_identifier", "shorthand_property_identifier_pattern":
		return text(member, src), true
	default:
		return "", false
	}
	if key == nil {
		return "", false
	}
	if kinds.Of(key) == "computed_property_name" {
		children := namedChildren(key)
		if len(children) != 1 {
			return "", false
		}
		if literal, ok := stringValue(children[0], src, kinds); ok {
			return literal, true
		}
		// An arbitrary computed key may overwrite the event discriminator.
		return "", false
	}
	if literal, ok := stringValue(key, src, kinds); ok {
		return literal, true
	}
	if kinds.Of(key) == "identifier" || kinds.Of(key) == "property_identifier" {
		return text(key, src), true
	}
	return "", false
}

func (a *Analyzer) forwardingHelperCall(m *machineModel, file string, root, call, wrapper *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable, reads map[string]bool) bool {
	callee := call.ChildByFieldName("function")
	if callee == nil || kinds.Of(callee) != "identifier" {
		return false
	}
	name := text(callee, src)
	args := callArguments(call, kinds)
	if name == "" || len(args) == 0 {
		return false
	}
	inputIndex := -1
	for i, arg := range args {
		arg = unwrapExpression(arg, kinds)
		if kinds.Of(arg) == "identifier" && text(arg, src) == parameter && parameterReference(arg, wrapper, parameter, src, kinds) {
			inputIndex = i
			break
		}
	}
	if inputIndex < 0 {
		return false
	}

	var helper *sitter.Node
	helperSrc := src
	helperKinds := kinds
	binding := localBindingAt(callee, name, src, kinds)
	if binding != nil {
		switch kinds.Of(binding) {
		case "function_declaration":
			helper = binding
		case "variable_declarator":
			value := binding.ChildByFieldName("value")
			if functionLike(value, kinds) {
				helper = value
			}
		}
	} else {
		imported, ok := a.importedLocal(file, name)
		if !ok {
			return false
		}
		module, exportName, hasExport := strings.Cut(imported, "#")
		if !hasExport || module == "" || exportName == "" {
			return false
		}
		reads[module] = true
		resolved, ok := a.resolveExport(module, exportName, map[string]bool{})
		if !ok {
			return false
		}
		module, exportName = targetModule(resolved), targetExport(resolved)
		if module == "" || exportName == "" {
			return false
		}
		reads[module] = true
		helperSrc = a.source(module)
		if len(helperSrc) == 0 {
			return false
		}
		helperRoot, parsedKinds, done := parse(module, helperSrc)
		if done == nil {
			return false
		}
		defer done()
		helper = topLevelFunctionNode(helperRoot, exportName, helperSrc, parsedKinds)
		helperKinds = parsedKinds
	}
	if helper == nil {
		return false
	}
	return functionReturnsParameter(helper, inputIndex, helperSrc, helperKinds)
}

func topLevelFunctionNode(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	for _, top := range namedChildren(root) {
		declarations := []*sitter.Node{top}
		if kinds.Of(top) == "export_statement" {
			declarations = namedChildren(top)
		}
		for _, declaration := range declarations {
			switch kinds.Of(declaration) {
			case "function_declaration":
				if functionName(declaration, src) == name {
					return declaration
				}
			case "lexical_declaration", "variable_declaration":
				for _, binding := range namedChildren(declaration) {
					if kinds.Of(binding) == "variable_declarator" && text(binding.ChildByFieldName("name"), src) == name {
						value := binding.ChildByFieldName("value")
						if functionLike(value, kinds) {
							return value
						}
					}
				}
			}
		}
	}
	return nil
}

func functionReturnsParameter(fn *sitter.Node, index int, src []byte, kinds *tsutil.KindTable) bool {
	params := fn.ChildByFieldName("parameters")
	if params == nil && kinds.Of(fn) == "arrow_function" {
		parameter := fn.ChildByFieldName("parameter")
		if index != 0 || parameter == nil {
			return false
		}
		params = parameter.Parent()
	}
	parameterNodes := namedChildren(params)
	if index < 0 || index >= len(parameterNodes) {
		return false
	}
	parameterNode := parameterNodes[index]
	nameNode := parameterNode.ChildByFieldName("name")
	if nameNode == nil {
		nameNode = parameterNode.ChildByFieldName("pattern")
	}
	if nameNode == nil || kinds.Of(nameNode) != "identifier" {
		return false
	}
	parameter := text(nameNode, src)
	body := fn.ChildByFieldName("body")
	if body == nil {
		return false
	}
	if kinds.Of(body) != "statement_block" {
		return valuePreservesFunctionParameter(body, fn, parameter, src, kinds)
	}
	if !functionDefinitelyReturns(body, kinds) {
		return false
	}
	var returns []*sitter.Node
	walkFunctionScope(body, kinds, func(node *sitter.Node) {
		if kinds.Of(node) == "return_statement" {
			returns = append(returns, returnValue(node))
		}
	})
	if len(returns) == 0 {
		return false
	}
	for _, value := range returns {
		if !valuePreservesFunctionParameter(value, fn, parameter, src, kinds) {
			return false
		}
	}
	return true
}

func valuePreservesFunctionParameter(value, fn *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable) bool {
	value = unwrapExpression(value, kinds)
	if value == nil {
		return false
	}
	switch kinds.Of(value) {
	case "identifier":
		return text(value, src) == parameter && parameterReference(value, fn, parameter, src, kinds)
	case "object":
		return objectPreservesParameter(value, fn, parameter, src, kinds)
	case "conditional_expression":
		return valuePreservesFunctionParameter(value.ChildByFieldName("consequence"), fn, parameter, src, kinds) &&
			valuePreservesFunctionParameter(value.ChildByFieldName("alternative"), fn, parameter, src, kinds)
	default:
		return false
	}
}

func parameterReference(reference, fn *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) bool {
	if reference == nil || fn == nil || name == "" {
		return false
	}
	for scope := reference.Parent(); scope != nil && !sameSyntaxNode(scope, fn); scope = scope.Parent() {
		if kinds.Of(scope) != "statement_block" {
			continue
		}
		for _, declaration := range namedChildren(scope) {
			switch kinds.Of(declaration) {
			case "lexical_declaration", "variable_declaration":
				for _, binding := range namedChildren(declaration) {
					if kinds.Of(binding) == "variable_declarator" && text(binding.ChildByFieldName("name"), src) == name {
						return false
					}
				}
			case "function_declaration":
				if functionName(declaration, src) == name {
					return false
				}
			}
		}
	}
	return true
}

// functionDefinitelyReturns is a deliberately small, conservative control-flow
// proof used for converter and returned-object coverage. Unsupported control
// flow stays unknown instead of turning a partial branch into a total result.
func functionDefinitelyReturns(node *sitter.Node, kinds *tsutil.KindTable) bool {
	if node == nil {
		return false
	}
	switch kinds.Of(node) {
	case "return_statement", "throw_statement":
		return true
	case "statement_block":
		children := namedChildren(node)
		return len(children) > 0 && functionDefinitelyReturns(children[len(children)-1], kinds)
	case "if_statement":
		consequence := node.ChildByFieldName("consequence")
		alternative := node.ChildByFieldName("alternative")
		return alternative != nil && functionDefinitelyReturns(consequence, kinds) && functionDefinitelyReturns(alternative, kinds)
	case "else_clause", "finally_clause":
		children := namedChildren(node)
		return len(children) > 0 && functionDefinitelyReturns(children[len(children)-1], kinds)
	case "switch_statement":
		return switchBranchesDefinitelyReturn(node, kinds)
	case "try_statement":
		body := node.ChildByFieldName("body")
		if finally := node.ChildByFieldName("finalizer"); finally != nil && functionDefinitelyReturns(finally, kinds) {
			return true
		}
		catch := node.ChildByFieldName("handler")
		return functionDefinitelyReturns(body, kinds) && (catch == nil || functionDefinitelyReturns(catch, kinds))
	case "catch_clause":
		children := namedChildren(node)
		return len(children) > 0 && functionDefinitelyReturns(children[len(children)-1], kinds)
	default:
		return false
	}
}

func switchBranchesDefinitelyReturn(node *sitter.Node, kinds *tsutil.KindTable) bool {
	body := node.ChildByFieldName("body")
	branches := namedChildren(body)
	if len(branches) == 0 {
		return false
	}
	hasDefault := false
	pendingCase := false
	for _, branch := range branches {
		switch kinds.Of(branch) {
		case "switch_case":
			pendingCase = true
			value := branch.ChildByFieldName("value")
			var statements []*sitter.Node
			for _, child := range namedChildren(branch) {
				if sameSyntaxNode(child, value) {
					continue
				}
				statements = append(statements, child)
			}
			if len(statements) == 0 {
				continue
			}
			if !functionDefinitelyReturns(statements[len(statements)-1], kinds) {
				return false
			}
			pendingCase = false
		case "switch_default":
			hasDefault = true
			statements := namedChildren(branch)
			if len(statements) == 0 || !functionDefinitelyReturns(statements[len(statements)-1], kinds) {
				return false
			}
			pendingCase = false
		default:
			return false
		}
	}
	return hasDefault && !pendingCase
}

func (a *Analyzer) functionSwitchExhaustive(file string, fn, body *sitter.Node, src []byte, kinds *tsutil.KindTable) (bool, []string) {
	var switches []*sitter.Node
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if kinds.Of(n) == "switch_statement" {
			switches = append(switches, n)
		}
	})
	for _, sw := range switches {
		discriminant := unwrapExpression(sw.ChildByFieldName("value"), kinds)
		if discriminant == nil || kinds.Of(discriminant) != "member_expression" || text(discriminant.ChildByFieldName("property"), src) != "type" {
			continue
		}
		object := unwrapExpression(discriminant.ChildByFieldName("object"), kinds)
		if object == nil || kinds.Of(object) != "identifier" {
			continue
		}
		parameter := text(object, src)
		typeName := functionParameterTypeName(fn, parameter, src, kinds)
		if typeName == "" {
			continue
		}
		typeFile, typeExport := a.resolveType(file, typeName)
		if typeFile == "" || typeExport == "" {
			continue
		}
		dependency := []string{}
		if typeFile != file {
			dependency = append(dependency, typeFile)
		}
		typeSrc := a.source(typeFile)
		if len(typeSrc) == 0 {
			return false, dependency
		}
		typeRoot, typeKinds, done := parse(typeFile, typeSrc)
		if done == nil {
			return false, dependency
		}
		tags, closed := stringDiscriminantUnion(typeDeclaration(typeRoot, typeExport, typeSrc, typeKinds), "type", typeSrc, typeKinds)
		done()
		if !closed || len(tags) == 0 || !functionBodyIsSoleSwitch(body, sw, kinds) {
			return false, dependency
		}
		covered, returns := switchCaseCoverage(sw, src, kinds)
		if !returns || !sameStringSet(tags, covered) {
			return false, dependency
		}
		return true, dependency
	}
	return false, nil
}

// A complete closed-union switch proves converter return completeness only
// when it is the function's entire executable body. A nested switch under an
// if/loop/try can leave another path that falls through the function.
func functionBodyIsSoleSwitch(body, candidate *sitter.Node, kinds *tsutil.KindTable) bool {
	if body == nil || candidate == nil || kinds.Of(body) != "statement_block" {
		return false
	}
	parent := candidate.Parent()
	if parent == nil || kinds.Of(parent) != "statement_block" || !sameSyntaxNode(parent, body) {
		return false
	}
	statements := []*sitter.Node{}
	for _, child := range namedChildren(body) {
		switch kinds.Of(child) {
		case "comment", "function_declaration":
			// Nested function declarations do not execute on this path.
		default:
			statements = append(statements, child)
		}
	}
	if kinds.Of(candidate) != "switch_statement" || len(statements) == 0 {
		return false
	}
	index := -1
	for i, statement := range statements {
		if sameSyntaxNode(statement, candidate) {
			index = i
			break
		}
	}
	if index != len(statements)-1 {
		return false
	}
	for _, statement := range statements[:index] {
		switch kinds.Of(statement) {
		case "lexical_declaration", "variable_declaration", "expression_statement":
			// These statements fall through to the discriminant switch.
		default:
			// In particular, a conditional or loop can bypass the switch.
			return false
		}
	}
	return true
}

func functionParameterTypeName(fn *sitter.Node, parameter string, src []byte, kinds *tsutil.KindTable) string {
	for _, arg := range namedChildren(fn.ChildByFieldName("parameters")) {
		name := arg.ChildByFieldName("name")
		if name == nil {
			name = arg.ChildByFieldName("pattern")
		}
		if text(name, src) != parameter {
			continue
		}
		typeNode := arg.ChildByFieldName("type")
		var names []string
		walk(typeNode, func(n *sitter.Node) {
			if kinds.Of(n) == "type_identifier" {
				names = append(names, text(n, src))
			}
		})
		if len(unique(names)) == 1 {
			return unique(names)[0]
		}
	}
	return ""
}

func stringDiscriminantUnion(declaration *sitter.Node, property string, src []byte, kinds *tsutil.KindTable) ([]string, bool) {
	if declaration == nil {
		return nil, false
	}
	root := declaration.ChildByFieldName("value")
	if root == nil {
		return nil, false
	}
	var members []*sitter.Node
	var flatten func(*sitter.Node)
	flatten = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if kinds.Of(n) == "union_type" {
			for _, child := range namedChildren(n) {
				flatten(child)
			}
			return
		}
		if kinds.Of(n) == "parenthesized_type" {
			for _, child := range namedChildren(n) {
				flatten(child)
			}
			return
		}
		members = append(members, n)
	}
	flatten(root)
	if len(members) == 0 {
		return nil, false
	}
	tags := []string{}
	for _, member := range members {
		if kinds.Of(member) != "object_type" {
			return nil, false
		}
		var fieldType *sitter.Node
		for _, field := range namedChildren(member) {
			if kinds.Of(field) == "property_signature" && text(field.ChildByFieldName("name"), src) == property {
				fieldType = field.ChildByFieldName("type")
				break
			}
		}
		values, ok := stringLiteralTypeValues(fieldType, src, kinds)
		if !ok || len(values) == 0 {
			return nil, false
		}
		tags = append(tags, values...)
	}
	return unique(tags), true
}

func stringLiteralTypeValues(node *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	if kinds.Of(node) == "type_annotation" {
		children := namedChildren(node)
		if len(children) == 1 {
			return stringLiteralTypeValues(children[0], src, kinds)
		}
	}
	if kinds.Of(node) == "union_type" {
		var out []string
		for _, child := range namedChildren(node) {
			values, ok := stringLiteralTypeValues(child, src, kinds)
			if !ok {
				return nil, false
			}
			out = append(out, values...)
		}
		return unique(out), len(out) > 0
	}
	if kinds.Of(node) != "literal_type" {
		return nil, false
	}
	for _, child := range namedChildren(node) {
		if value, ok := stringValue(child, src, kinds); ok {
			return []string{value}, true
		}
	}
	return nil, false
}

func switchCaseCoverage(node *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]string, bool) {
	var covered, pending []string
	for _, branch := range namedChildren(node.ChildByFieldName("body")) {
		if kinds.Of(branch) != "switch_case" {
			return nil, false
		}
		value := branch.ChildByFieldName("value")
		tag, ok := stringValue(value, src, kinds)
		if !ok {
			return nil, false
		}
		pending = append(pending, tag)
		var statements []*sitter.Node
		for _, child := range namedChildren(branch) {
			if sameSyntaxNode(child, value) {
				continue
			}
			statements = append(statements, child)
		}
		if len(statements) == 0 {
			continue
		}
		if !functionDefinitelyReturns(statements[len(statements)-1], kinds) {
			return nil, false
		}
		covered = append(covered, pending...)
		pending = nil
	}
	if len(pending) > 0 {
		return nil, false
	}
	return unique(covered), len(covered) > 0
}

func sameStringSet(a, b []string) bool {
	left, right := unique(a), unique(b)
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if !seen[value] {
			return false
		}
	}
	return true
}

func (a *Analyzer) eventArgumentConstructionFacts(m *machineModel, from string, args []*sitter.Node, src []byte, kinds *tsutil.KindTable) ([]facts.Fact, map[string]bool) {
	reads := map[string]bool{}
	if len(args) == 0 {
		return nil, reads
	}
	expr := unwrapExpression(args[0], kinds)
	if tag, ok := eventObjectTag(expr, src, kinds); ok {
		return []facts.Fact{interactionFact(from, enclosingFunction(expr, src, kinds), facts.RelFSMConstructsEvent,
			machineMember(m.machine, "event", tag), expr, map[string]any{"construction_status": "literal_object", "event_tag": tag})}, reads
	}
	if kinds.Of(expr) == "identifier" {
		if value := variableValueFromFunction(expr, text(expr, src), src, kinds); value != nil {
			return a.eventArgumentConstructionFacts(m, from, []*sitter.Node{value}, src, kinds)
		}
		return nil, reads
	}
	if kinds.Of(expr) != "call_expression" {
		return nil, reads
	}
	if _, dep, ok := a.eventConstructorTag(m, from, expr, src, kinds); ok {
		if dep != "" {
			reads[dep] = true
		}
		// The dispatch-file constructor pass owns this site already.
		return nil, reads
	}
	name := calleeName(expr, src, kinds)
	resolved, ok := a.resolvedImport(from, name)
	if !ok {
		return nil, reads
	}
	calleeFile, exportName := targetModule(resolved), targetExport(resolved)
	if calleeFile == "" || exportName == "" {
		return nil, reads
	}
	reads[calleeFile] = true
	if a.collectingConstructorUses {
		calleeSource := a.source(calleeFile)
		if len(calleeSource) > 0 {
			root, calleeKinds, done := parse(calleeFile, calleeSource)
			if done != nil {
				function, body := topLevelExportedFunction(root, exportName, calleeSource, calleeKinds)
				if function != nil && len(literalReturnEventTags(m, body, calleeSource, calleeKinds)) > 0 {
					key := constructorUseKey{machine: m.machine, file: calleeFile, export: exportName}
					if a.constructorUses[key] == nil {
						a.constructorUses[key] = map[string]bool{}
					}
					a.constructorUses[key][slash(from)] = true
				}
				done()
			}
		}
	}
	// Returned-event construction is emitted by the callee file's own
	// contribution. Emitting a callee-file symbol overlay from this caller would
	// give it the wrong owner and leave stale facts when the caller stops using
	// the converter.
	return nil, reads
}

func (a *Analyzer) extractSourceReturnConstructions(m *machineModel, file string, root *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]facts.Fact, map[string]bool) {
	reads := map[string]bool{}
	var out []facts.Fact
	admitted := map[string]bool{}
	for _, export := range a.constructorExports(m.machine, file) {
		admitted[export] = true
	}
	for _, export := range topLevelFunctionExports(root, src, kinds) {
		function, body := topLevelExportedFunction(root, export, src, kinds)
		if function == nil || body == nil {
			continue
		}
		typeName := functionReturnTypeName(function, src, kinds)
		if typeName == "" {
			continue
		}
		typedBy, ok := a.typeRelation(file, typeName, facts.RelFSMTypedBy)
		if ok && typedBy.TargetFile == m.eventFile && typedBy.Target == moduleName(m.eventFile)+"."+m.eventExport {
			admitted[export] = true
			if typedBy.TargetFile != file {
				reads[typedBy.TargetFile] = true
			}
		}
	}
	var orderedExports []string
	for export := range admitted {
		orderedExports = append(orderedExports, export)
	}
	sort.Strings(orderedExports)
	for _, export := range orderedExports {
		function, body := topLevelExportedFunction(root, export, src, kinds)
		if function == nil || body == nil {
			continue
		}
		if !topLevelExport(root, export, src, kinds) {
			continue
		}
		for _, caller := range a.constructorCallers(m.machine, file, export) {
			reads[caller] = true
		}
		for _, returned := range literalReturnEventSites(m, body, src, kinds) {
			tag := returned.tag
			out = append(out, interactionFact(file, export, facts.RelFSMConstructsEvent,
				machineMember(m.machine, "event", tag), returned.node,
				map[string]any{"construction_status": "literal_return", "event_tag": tag}))
		}
	}
	return out, reads
}

type eventReturnSite struct {
	tag  string
	node *sitter.Node
}

func literalReturnEventSites(m *machineModel, body *sitter.Node, src []byte, kinds *tsutil.KindTable) []eventReturnSite {
	var out []eventReturnSite
	appendReturn := func(returned *sitter.Node) {
		if returned == nil {
			return
		}
		tag, ok := eventObjectTag(unwrapExpression(returned, kinds), src, kinds)
		if !ok || m.events[tag].Kind == "" {
			return
		}
		out = append(out, eventReturnSite{tag: tag, node: returned})
	}
	if body == nil {
		return nil
	}
	if kinds.Of(body) != "statement_block" {
		appendReturn(body)
		return out
	}
	walkFunctionScope(body, kinds, func(node *sitter.Node) {
		if kinds.Of(node) == "return_statement" {
			appendReturn(returnValue(node))
		}
	})
	return out
}

func literalReturnEventTags(m *machineModel, body *sitter.Node, src []byte, kinds *tsutil.KindTable) []string {
	var tags []string
	for _, site := range literalReturnEventSites(m, body, src, kinds) {
		tags = append(tags, site.tag)
	}
	return unique(tags)
}

func topLevelFunctionExports(root *sitter.Node, src []byte, kinds *tsutil.KindTable) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, statement := range namedChildren(root) {
		if kinds.Of(statement) != "export_statement" {
			continue
		}
		for _, declaration := range namedChildren(statement) {
			switch kinds.Of(declaration) {
			case "function_declaration":
				add(functionName(declaration, src))
			case "lexical_declaration", "variable_declaration":
				for _, declarator := range namedChildren(declaration) {
					if kinds.Of(declarator) != "variable_declarator" || !functionLike(declarator.ChildByFieldName("value"), kinds) {
						continue
					}
					add(text(declarator.ChildByFieldName("name"), src))
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

func functionReturnTypeName(function *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	if function == nil {
		return ""
	}
	returnType := function.ChildByFieldName("return_type")
	if kinds.Of(returnType) == "type_annotation" {
		var typeNode *sitter.Node
		for _, child := range namedChildren(returnType) {
			if kinds.Of(child) == "comment" {
				continue
			}
			if typeNode != nil {
				return ""
			}
			typeNode = child
		}
		returnType = typeNode
	}
	if returnType == nil {
		return ""
	}
	switch kinds.Of(returnType) {
	case "type_identifier", "identifier":
		return text(returnType, src)
	default:
		return ""
	}
}

func topLevelExportedFunction(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) (*sitter.Node, *sitter.Node) {
	for _, statement := range namedChildren(root) {
		if kinds.Of(statement) != "export_statement" {
			continue
		}
		for _, declaration := range namedChildren(statement) {
			switch kinds.Of(declaration) {
			case "function_declaration":
				if functionName(declaration, src) == name {
					return declaration, declaration.ChildByFieldName("body")
				}
			case "lexical_declaration", "variable_declaration":
				for _, declarator := range namedChildren(declaration) {
					if kinds.Of(declarator) != "variable_declarator" || text(declarator.ChildByFieldName("name"), src) != name {
						continue
					}
					value := declarator.ChildByFieldName("value")
					if value == nil || !functionLike(value, kinds) {
						return nil, nil
					}
					return value, value.ChildByFieldName("body")
				}
			}
		}
	}
	return nil, nil
}

func (a *Analyzer) eventConstructorTag(m *machineModel, from string, call *sitter.Node, src []byte, kinds *tsutil.KindTable) (string, string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil || kinds.Of(fn) != "member_expression" {
		return "", "", false
	}
	object := fn.ChildByFieldName("object")
	if object == nil || kinds.Of(object) != "identifier" || m.eventExport == "" {
		return "", "", false
	}
	resolved, ok := a.resolvedImport(from, text(object, src))
	if !ok || targetModule(resolved) != m.eventFile || targetExport(resolved) != m.eventExport {
		return "", targetModule(resolved), false
	}
	tag := text(fn.ChildByFieldName("property"), src)
	return tag, targetModule(resolved), tag != ""
}

func (a *Analyzer) extractEventConstructions(m *machineModel, rel string, root *sitter.Node, src []byte, kinds *tsutil.KindTable) ([]facts.Fact, map[string]bool) {
	var out []facts.Fact
	reads := map[string]bool{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" {
			return
		}
		if tag, dep, ok := a.eventConstructorTag(m, rel, n, src, kinds); ok {
			if dep != "" && dep != rel {
				reads[dep] = true
			}
			out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMConstructsEvent,
				machineMember(m.machine, "event", tag), n, map[string]any{"construction_status": "resolved_constructor", "event_tag": tag}))
		}
	})
	return out, reads
}

func (a *Analyzer) extractEffectSink(m *machineModel, rel string, root *sitter.Node, src []byte, kinds *tsutil.KindTable, sink DispatchSink) ([]facts.Fact, map[string]bool) {
	reads := map[string]bool{}
	if sink.MachineTag == nil {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "machine_tag_unconfigured", "dispatch_unresolved_sink": 1})}, reads
	}
	tagFile, tagExport := slash(sink.MachineTag.Module), sink.MachineTag.Export
	tagTarget, ok := a.resolveExport(tagFile, tagExport, map[string]bool{})
	if !ok {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "machine_tag_unresolved", "dispatch_unresolved_sink": 1})}, reads
	}
	tagFile, tagExport = targetModule(tagTarget), targetExport(tagTarget)
	reads[tagFile] = true
	if !a.machineProvidedByTag(m, tagFile, tagExport) {
		return []facts.Fact{dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "machine_tag_unbound", "dispatch_unresolved_sink": 1})}, reads
	}
	var out []facts.Fact
	callsSeen, machineBound := 0, 0
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" {
			return
		}
		local := calleeName(n, src, kinds)
		resolved, imported := a.resolvedImport(rel, local)
		if !imported || !a.matchesSymbolRef(resolved, sink.Factory) {
			return
		}
		callsSeen++
		reads[targetModule(resolved)] = true
		args := callArguments(n, kinds)
		// The event may be nested in an inline request or one same-function
		// local object. No interprocedural guess is made here.
		machineIndex := sink.MachineArgument
		if machineIndex < 0 || machineIndex >= len(args) {
			return
		}
		machineArg := args[machineIndex]
		if !a.machineValueBound(m, rel, machineArg, tagFile, tagExport) {
			return
		}
		machineBound++
		tag, eventSource, eventOK, typeReads := findConfiguredEventInArgument(m, args, sink.EventArgument, sink.EventPath, rel, src, kinds, a)
		caller := enclosingFunction(n, src, kinds)
		if eventArg := eventArgumentAt(args, sink.EventArgument, sink.EventPath, src, kinds); eventArg != nil {
			constructed, constructionReads := a.eventArgumentConstructionFacts(m, rel, []*sitter.Node{eventArg}, src, kinds)
			out = append(out, constructed...)
			for f := range constructionReads {
				reads[f] = true
			}
		}
		if eventSource != "" {
			reads[eventSource] = true
		}
		for _, file := range typeReads {
			reads[file] = true
		}
		if eventOK && m.events[tag].Kind != "" {
			out = append(out, interactionFact(rel, caller, facts.RelFSMDispatches, machineMember(m.machine, "event", tag), n, map[string]any{"dispatch_status": "resolved", "event_tag": tag}))
		} else {
			out = append(out, interactionFact(rel, caller, facts.RelFSMDispatchesUnknownEvent, m.machine, n, map[string]any{"dispatch_status": "unknown_event"}))
		}
	})
	if callsSeen == 0 {
		out = append(out, dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "unresolved_import_or_call", "dispatch_unresolved_sink": 1}))
	} else if machineBound == 0 {
		out = append(out, dispatchCoverageFact(m, rel, map[string]any{"dispatch_sink_status": "machine_binding_unresolved", "dispatch_unresolved_sink": callsSeen}))
	}
	return out, reads
}

func (a *Analyzer) machineProvidedByTag(m *machineModel, tagFile, tagExport string) bool {
	src := a.source(tagFile)
	if len(src) == 0 {
		return false
	}
	root, kinds, done := parse(tagFile, src)
	if done == nil {
		return false
	}
	defer done()
	bound := false
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "call_expression" || calleeName(n, src, kinds) != "succeed" {
			return
		}
		args := callArguments(n, kinds)
		if len(args) < 2 || text(args[0], src) != tagExport || kinds.Of(args[1]) != "object" {
			return
		}
		machineExpr := objectPair(args[1], "machine", src, kinds)
		if machineExpr == nil {
			return
		}
		name := text(machineExpr, src)
		resolved, ok := a.resolvedImport(tagFile, name)
		if !ok {
			return
		}
		file, export := targetModule(resolved), targetExport(resolved)
		wrapped := a.source(file)
		if len(wrapped) == 0 {
			return
		}
		wrappedRoot, wk, closeWrapped := parse(file, wrapped)
		if closeWrapped == nil {
			return
		}
		defer closeWrapped()
		value := variableValue(wrappedRoot, export, wrapped, wk)
		if value == nil || wk.Of(value) != "call_expression" {
			return
		}
		callArgs := callArguments(value, wk)
		if len(callArgs) == 0 {
			return
		}
		machineName := text(callArgs[0], wrapped)
		machineTarget, found := a.resolvedImport(file, machineName)
		if !found {
			return
		}
		expected := m.spec.InstanceExport
		if expected == "" {
			expected = "providerJobMachine"
		}
		if targetExport(machineTarget) == expected && targetModule(machineTarget) == m.file {
			bound = true
		}
	})
	return bound
}

func (a *Analyzer) machineValueBound(m *machineModel, caller string, value *sitter.Node, tagFile, tagExport string) bool {
	if value == nil || m.spec.InstanceExport == "" {
		return false
	}
	if kindsName, ok := memberExpr(value); ok && kindsName == "machine" {
		return false
	}
	if text(value, a.source(caller)) == "" {
		return false
	}
	if object := value.ChildByFieldName("object"); object != nil && text(value.ChildByFieldName("property"), a.source(caller)) == "machine" {
		runtime := text(object, a.source(caller))
		return runtimeHasBoundTag(caller, runtime, tagFile, tagExport, a)
	}
	return false
}

func runtimeHasBoundTag(file, runtime, tagFile, tagExport string, a *Analyzer) bool {
	src := a.source(file)
	root, kinds, done := parse(file, src)
	if done == nil {
		return false
	}
	defer done()
	var tagLocal string
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) == "variable_declarator" && text(n.ChildByFieldName("name"), src) == runtime {
			v := n.ChildByFieldName("value")
			walk(v, func(c *sitter.Node) {
				if kinds.Of(c) == "identifier" {
					res, ok := a.resolvedImport(file, text(c, src))
					if ok && targetModule(res) == tagFile && targetExport(res) == tagExport {
						tagLocal = text(c, src)
					}
				}
			})
		}
	})
	return tagLocal != ""
}

func (a *Analyzer) extractCommandHandlers(m *machineModel, rel string, root *sitter.Node, src []byte, kinds *tsutil.KindTable) []facts.Fact {
	var out []facts.Fact
	discriminant := m.spec.CommandDiscriminant
	if discriminant == "" {
		discriminant = "_tag"
	}
	searchRoot := root
	runnerTyped := false
	typedParameters := map[string]map[string]bool{}
	if m.spec.EffectRunner != nil {
		resolved, ok := a.resolveExport(m.spec.EffectRunner.Module, m.spec.EffectRunner.Export, map[string]bool{})
		if !ok || targetModule(resolved) != rel || targetExport(resolved) != m.spec.EffectRunner.Export {
			// No source-bound runner: do not infer a command handler from tag spelling.
		} else {
			fn := functionBodyNodeForName(root, m.spec.EffectRunner.Export, src, kinds)
			if fn == nil {
			} else {
				searchRoot = fn.ChildByFieldName("body")
				for _, p := range namedChildren(fn.ChildByFieldName("parameters")) {
					if a.typeNodeMatchesCommand(m, rel, p.ChildByFieldName("type"), src, kinds) {
						runnerTyped = true
						addTypedParameter(typedParameters, m.spec.EffectRunner.Export, p, src)
					}
				}
			}
		}
	} else {
		walk(root, func(n *sitter.Node) {
			if kinds.Of(n) != "function_declaration" && kinds.Of(n) != "arrow_function" && kinds.Of(n) != "function_expression" {
				return
			}
			name := enclosingFunction(n, src, kinds)
			if name == "" {
				return
			}
			for _, p := range namedChildren(n.ChildByFieldName("parameters")) {
				if a.typeNodeMatchesCommand(m, rel, p.ChildByFieldName("type"), src, kinds) {
					addTypedParameter(typedParameters, name, p, src)
					runnerTyped = true
				}
			}
		})
	}
	handled := map[string]bool{}
	defaultCase := false
	visitHandlerEvidence := func(n *sitter.Node) {
		if kinds.Of(n) == "switch_case" {
			value := n.ChildByFieldName("value")
			if value == nil {
				defaultCase = true
				return
			}
			tag, ok := stringValue(value, src, kinds)
			if !ok || m.commands[tag].Kind == "" {
				return
			}
			parent := n.Parent()
			for parent != nil && kinds.Of(parent) != "switch_statement" {
				parent = parent.Parent()
			}
			if parent == nil {
				return
			}
			fn := enclosingFunction(n, src, kinds)
			switchValue := parent.ChildByFieldName("value")
			if switchValue == nil {
				// Tree-sitter's TypeScript grammar exposes the discriminant under
				// `condition` for switch_statement nodes.
				switchValue = parent.ChildByFieldName("condition")
			}
			if !memberFieldBoundToTypedParameter(switchValue, discriminant, typedParameters[fn], src, kinds) {
				return
			}
			out = append(out, interactionFact(rel, fn, facts.RelFSMHandlesCommand, machineMember(m.machine, "command", tag), n, map[string]any{"handler_binding": "type_bound_switch", "handler_status": "proven"}))
			handled[tag] = true
		}
		if kinds.Of(n) == "if_statement" {
			fn := enclosingFunction(n, src, kinds)
			if !conditionHasTypedDiscriminant(n.ChildByFieldName("condition"), discriminant, typedParameters[fn], src, kinds) {
				return
			}
			cond := text(n.ChildByFieldName("condition"), src)
			for tag := range m.commands {
				if strings.Contains(cond, "'"+tag+"'") || strings.Contains(cond, "\""+tag+"\"") {
					out = append(out, interactionFact(rel, fn, facts.RelFSMHandlesCommand, machineMember(m.machine, "command", tag), n, map[string]any{"handler_binding": "type_bound_tag_test", "handler_status": "proven"}))
					handled[tag] = true
				}
			}
		}
	}
	if m.spec.EffectRunner == nil {
		// A generic configured handler file may contain several independent
		// typed functions. Walk their bodies and bind each case to its own
		// typed parameter rather than treating the file root as one function.
		walk(root, visitHandlerEvidence)
	} else {
		walkFunctionScope(searchRoot, kinds, visitHandlerEvidence)
	}
	if m.spec.EffectRunner == nil {
		// A generic machine repository can narrow one command through an
		// explicit TypeScript type predicate, then perform the corresponding
		// command branch. This proves that tag only; it does not infer handlers
		// for sibling commands.
		walk(root, func(n *sitter.Node) {
			if kinds.Of(n) != "arrow_function" && kinds.Of(n) != "function_expression" && kinds.Of(n) != "function_declaration" {
				return
			}
			tag, site, ok := typedCommandPredicateTag(m, rel, n, src, kinds, a)
			if !ok {
				return
			}
			out = append(out, interactionFact(rel, enclosingFunction(n, src, kinds), facts.RelFSMHandlesCommand,
				machineMember(m.machine, "command", tag), site, map[string]any{"handler_binding": "typed_type_predicate", "handler_status": "proven"}))
			handled[tag] = true
		})
	}
	status := "partial"
	if runnerTyped && len(handled) == len(m.commands) && !defaultCase {
		status = "complete"
	}
	out = append(out, facts.Fact{Kind: facts.KindExtraction, Name: "typescript:fsm:" + m.machine + ":handlers:" + rel, File: rel,
		Props: map[string]any{"extractor": "typescript:fsm", "language": "typescript", "fsm_machine": m.machine,
			"handler_coverage": status, "handler_cases_seen": len(handled), "command_count": len(m.commands), "default_case": defaultCase}})
	return out
}

func typedCommandPredicateTag(m *machineModel, file string, fn *sitter.Node, src []byte, kinds *tsutil.KindTable, a *Analyzer) (string, *sitter.Node, bool) {
	returnType := fn.ChildByFieldName("return_type")
	if returnType == nil {
		return "", nil, false
	}
	var predicate *sitter.Node
	walk(returnType, func(n *sitter.Node) {
		if predicate == nil && kinds.Of(n) == "type_predicate" {
			predicate = n
		}
	})
	if predicate == nil {
		return "", nil, false
	}
	predicateType := predicate.ChildByFieldName("type")
	if !a.typeNodeMatchesCommand(m, file, predicateType, src, kinds) {
		return "", nil, false
	}
	parameter := text(predicate.ChildByFieldName("name"), src)
	if parameter == "" {
		return "", nil, false
	}
	var candidate string
	walk(predicateType, func(n *sitter.Node) {
		if candidate != "" || kinds.Of(n) != "string" {
			return
		}
		if tag, ok := stringValue(n, src, kinds); ok && m.commands[tag].Kind != "" {
			candidate = tag
		}
	})
	if candidate == "" {
		return "", nil, false
	}
	body := fn.ChildByFieldName("body")
	var site *sitter.Node
	check := func(n *sitter.Node) {
		if site != nil || kinds.Of(n) != "binary_expression" {
			return
		}
		operator := text(n.ChildByFieldName("operator"), src)
		left := text(n.ChildByFieldName("left"), src)
		right, literal := stringValue(n.ChildByFieldName("right"), src, kinds)
		if (operator == "===" || operator == "==") && left == parameter+".command._tag" && literal && right == candidate {
			site = n
		}
	}
	if kinds.Of(body) == "statement_block" {
		walkFunctionScope(body, kinds, func(n *sitter.Node) { check(n.ChildByFieldName("condition")) })
	} else {
		check(body)
	}
	if site == nil {
		return "", nil, false
	}
	return candidate, site, true
}

func addTypedParameter(byFunction map[string]map[string]bool, function string, parameter *sitter.Node, src []byte) {
	if parameter == nil || function == "" {
		return
	}
	name := parameter.ChildByFieldName("name")
	if name == nil {
		name = parameter.ChildByFieldName("pattern")
	}
	if name == nil {
		return
	}
	if byFunction[function] == nil {
		byFunction[function] = map[string]bool{}
	}
	byFunction[function][text(name, src)] = true
}

func memberFieldBoundToTypedParameter(n *sitter.Node, field string, parameters map[string]bool, src []byte, kinds *tsutil.KindTable) bool {
	n = unwrapExpression(n, kinds)
	if n == nil || kinds.Of(n) != "member_expression" || text(n.ChildByFieldName("property"), src) != field {
		return false
	}
	object := unwrapExpression(n.ChildByFieldName("object"), kinds)
	return object != nil && kinds.Of(object) == "identifier" && parameters[text(object, src)]
}

func conditionHasTypedDiscriminant(n *sitter.Node, field string, parameters map[string]bool, src []byte, kinds *tsutil.KindTable) bool {
	found := false
	walk(n, func(c *sitter.Node) {
		if !found && memberFieldBoundToTypedParameter(c, field, parameters, src, kinds) {
			found = true
		}
	})
	return found
}

func conditionHasLiteralTypedDiscriminant(n *sitter.Node, field string, parameters map[string]bool, src []byte, kinds *tsutil.KindTable) bool {
	found := false
	walk(n, func(c *sitter.Node) {
		if found || kinds.Of(c) != "binary_expression" {
			return
		}
		op := text(c.ChildByFieldName("operator"), src)
		if op != "===" && op != "==" {
			return
		}
		left, right := c.ChildByFieldName("left"), c.ChildByFieldName("right")
		_, rightLiteral := stringValue(right, src, kinds)
		_, leftLiteral := stringValue(left, src, kinds)
		if (rightLiteral && memberFieldBoundToTypedParameter(left, field, parameters, src, kinds)) ||
			(leftLiteral && memberFieldBoundToTypedParameter(right, field, parameters, src, kinds)) {
			found = true
		}
	})
	return found
}

func conditionHasLiteralFieldComparison(n *sitter.Node, field string, src []byte, kinds *tsutil.KindTable) bool {
	found := false
	walk(n, func(c *sitter.Node) {
		if found || kinds.Of(c) != "binary_expression" {
			return
		}
		op := text(c.ChildByFieldName("operator"), src)
		if op != "===" && op != "==" {
			return
		}
		left, right := unwrapExpression(c.ChildByFieldName("left"), kinds), unwrapExpression(c.ChildByFieldName("right"), kinds)
		_, rightLiteral := stringValue(right, src, kinds)
		_, leftLiteral := stringValue(left, src, kinds)
		if (rightLiteral && left != nil && kinds.Of(left) == "member_expression" && text(left.ChildByFieldName("property"), src) == field) ||
			(leftLiteral && right != nil && kinds.Of(right) == "member_expression" && text(right.ChildByFieldName("property"), src) == field) {
			found = true
		}
	})
	return found
}

func (a *Analyzer) typeNodeMatchesCommand(m *machineModel, file string, typ *sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	matched := false
	walk(typ, func(n *sitter.Node) {
		if matched || kinds.Of(n) != "type_identifier" {
			return
		}
		resolvedFile, resolvedExport := a.resolveType(file, text(n, src))
		if resolvedFile == m.commandFile && resolvedExport == m.commandExport {
			matched = true
		}
	})
	return matched
}

func interactionFact(file, sourceSymbol, relKind, target string, site *sitter.Node, props map[string]any) facts.Fact {
	if sourceSymbol == "" {
		sourceSymbol = file
	}
	copyProps := map[string]any{"language": "typescript", "fsm_evidence": "direct_source_binding"}
	for k, v := range props {
		copyProps[k] = v
	}
	return facts.Fact{Kind: facts.KindSymbol, Name: sourceSymbol, File: file, Line: nodeLine(site), EndLine: int(site.EndPosition().Row) + 1,
		Props: copyProps, Relations: []facts.Relation{relation(relKind, target)}}
}

func dispatchCoverageFact(m *machineModel, file string, props map[string]any) facts.Fact {
	all := map[string]any{"extractor": "typescript:fsm", "language": "typescript", "fsm_machine": m.machine, "coverage_status": "partial"}
	for k, v := range props {
		all[k] = v
	}
	return facts.Fact{Kind: facts.KindExtraction, Name: "typescript:fsm:" + m.machine + ":dispatch:" + file, File: file, Props: all}
}

func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if slash(p) == slash(path) {
			return true
		}
	}
	return false
}

func targetModule(resolved string) string { module, _, _ := strings.Cut(resolved, "#"); return module }
func targetExport(resolved string) string {
	_, export, ok := strings.Cut(resolved, "#")
	if ok {
		return export
	}
	return ""
}

func compactNonempty(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		if v && k != "" {
			out[k] = true
		}
	}
	return out
}

func functionLike(n *sitter.Node, kinds *tsutil.KindTable) bool {
	k := kinds.Of(n)
	return k == "arrow_function" || k == "function_expression" || k == "function_declaration"
}

func containsCallName(n *sitter.Node, name string, src []byte, kinds *tsutil.KindTable, found func(*sitter.Node)) {
	walk(n, func(c *sitter.Node) {
		if kinds.Of(c) == "call_expression" && calleeName(c, src, kinds) == name {
			found(c)
		}
	})
}

func typedEventParameter(fn *sitter.Node, m *machineModel, file string, src []byte, kinds *tsutil.KindTable, a *Analyzer) string {
	params := fn.ChildByFieldName("parameters")
	for _, p := range namedChildren(params) {
		name := p.ChildByFieldName("name")
		if name == nil {
			name = p.ChildByFieldName("pattern")
		}
		ann := p.ChildByFieldName("type")
		if name != nil && ann != nil && a.typeNodeMatches(m, file, ann, src, kinds) {
			return text(name, src)
		}
	}
	return ""
}

func (a *Analyzer) typeNodeMatches(m *machineModel, file string, typ *sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	if typ == nil {
		return false
	}
	matched := false
	walk(typ, func(n *sitter.Node) {
		if matched || kinds.Of(n) != "type_identifier" {
			return
		}
		local := text(n, src)
		resolvedFile, resolvedExport := a.resolveType(file, local)
		if resolvedFile == m.eventFile && resolvedExport == m.eventExport {
			matched = true
		}
	})
	return matched
}

func eventObjectTag(n *sitter.Node, src []byte, kinds *tsutil.KindTable) (string, bool) {
	n = unwrapExpression(n, kinds)
	if n == nil || kinds.Of(n) != "object" {
		return "", false
	}
	return stringValue(objectPair(n, "type", src, kinds), src, kinds)
}

func unwrapExpression(n *sitter.Node, kinds *tsutil.KindTable) *sitter.Node {
	for n != nil {
		switch kinds.Of(n) {
		case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "type_assertion":
			var next *sitter.Node
			for _, c := range namedChildren(n) {
				if kinds.Of(c) != "type_annotation" && kinds.Of(c) != "type_arguments" {
					next = c
				}
			}
			if next == nil || next == n {
				return n
			}
			n = next
		default:
			return n
		}
	}
	return nil
}

func functionBodyNodeForName(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if fn := namedFunction(root, name, src, kinds); fn != nil {
		return fn
	}
	value := variableValue(root, name, src, kinds)
	if functionLike(value, kinds) {
		return value
	}
	return nil
}

func returnValue(n *sitter.Node) *sitter.Node {
	if n == nil || n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(0)
}

func variableValueFromFunction(expr *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	for p := expr.Parent(); p != nil; p = p.Parent() {
		if kinds.Of(p) != "statement_block" {
			continue
		}
		var found *sitter.Node
		walk(p, func(n *sitter.Node) {
			if found == nil && kinds.Of(n) == "variable_declarator" && text(n.ChildByFieldName("name"), src) == name {
				found = n.ChildByFieldName("value")
			}
		})
		return found
	}
	return nil
}

func enclosingFunction(n *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	for p := n; p != nil; p = p.Parent() {
		switch kinds.Of(p) {
		case "function_declaration":
			return functionName(p, src)
		case "variable_declarator":
			if functionLike(p.ChildByFieldName("value"), kinds) {
				return text(p.ChildByFieldName("name"), src)
			}
		case "method_definition":
			return text(p.ChildByFieldName("name"), src)
		}
	}
	return ""
}

func provenLocalBindingAt(reference *sitter.Node, name string, bindings map[string][]*sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	if reference == nil || name == "" {
		return false
	}
	resolved := localBindingAt(reference, name, src, kinds)
	if resolved == nil {
		return false
	}
	for _, binding := range bindings[name] {
		if sameSyntaxNode(resolved, binding) {
			return true
		}
	}
	return false
}

func configuredSendAliasAt(call *sitter.Node, name string, bindings map[string][]*sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	return call != nil && kinds.Of(call) == "call_expression" &&
		provenLocalBindingAt(call.ChildByFieldName("function"), name, bindings, src, kinds)
}

func callInsideProvenWrapper(call *sitter.Node, wrappers map[string][]*sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	for scope := call.Parent(); scope != nil; scope = scope.Parent() {
		if !isFunctionBoundary(kinds.Of(scope)) {
			continue
		}
		for name, candidates := range wrappers {
			if functionSymbolName(scope, src, kinds) != name {
				continue
			}
			for _, candidate := range candidates {
				if sameSyntaxNode(scope, candidate) {
					return true
				}
				if kinds.Of(candidate) == "variable_declarator" && sameSyntaxNode(scope, candidate.ChildByFieldName("value")) {
					return true
				}
			}
		}
		// A nested unproven function is a new lexical boundary. Do not let a
		// proven outer wrapper claim its sends or call sites.
		return false
	}
	return false
}

func memberExpr(n *sitter.Node) (string, bool) {
	if n == nil || n.Kind() != "member_expression" {
		return "", false
	}
	return "", true
}

func findConfiguredEventInArgument(m *machineModel, args []*sitter.Node, argIndex int, eventPath, file string, src []byte, kinds *tsutil.KindTable, a *Analyzer) (string, string, bool, []string) {
	arg := eventArgumentAt(args, argIndex, eventPath, src, kinds)
	arg = unwrapExpression(arg, kinds)
	if tag, ok := eventObjectTag(arg, src, kinds); ok {
		return tag, "", true, nil
	}
	tags, ok, dep, typeReads := a.eventArgumentTagsWithReads(m, file, []*sitter.Node{arg}, src, kinds)
	if ok && len(tags) == 1 {
		return tags[0], dep, true, typeReads
	}
	return "", dep, false, typeReads
}

func eventArgumentAt(args []*sitter.Node, argIndex int, eventPath string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if argIndex < 0 || argIndex >= len(args) {
		return nil
	}
	arg := args[argIndex]
	for _, part := range strings.Split(eventPath, ".") {
		if part == "" {
			continue
		}
		arg = unwrapExpression(arg, kinds)
		if kinds.Of(arg) == "identifier" {
			arg = variableValueFromFunction(arg, text(arg, src), src, kinds)
		}
		arg = objectPair(arg, part, src, kinds)
	}
	return arg
}
