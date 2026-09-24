package fsm

import (
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (a *Analyzer) extractInteractions(m *machineModel, relFile string, src []byte) ([]facts.Fact, map[string]bool) {
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
	return out, reads
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
	sendAliases := map[string]bool{}
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) != "variable_declarator" {
			return
		}
		value := n.ChildByFieldName("value")
		if value == nil || kinds.Of(value) != "member_expression" {
			return
		}
		if actors[text(value.ChildByFieldName("object"), src)] && text(value.ChildByFieldName("property"), src) == sink.Method {
			sendAliases[text(n.ChildByFieldName("name"), src)] = true
		}
	})
	wrappers := map[string]bool{}
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
		if !containsCallToAny(value.ChildByFieldName("body"), sendAliases, src, kinds) {
			return
		}
		eventParam := typedEventParameter(value, m, rel, src, kinds, a)
		if eventParam == "" {
			return
		}
		wrappers[name] = true
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
		if wrappers[callee] {
			if enclosingFunction(n, src, kinds) == callee {
				return
			}
			args := callArguments(n, kinds)
			constructed, constructionReads := a.eventArgumentConstructionFacts(m, rel, args, src, kinds)
			out = append(out, constructed...)
			for f := range constructionReads {
				reads[f] = true
			}
			tags, resolved, calleeFile := a.eventArgumentTags(m, rel, args, src, kinds)
			if calleeFile != "" {
				reads[calleeFile] = true
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
		if sendAliases[callee] && enclosingFunction(n, src, kinds) != wrapperForSend(n, wrappers, sendAliases, src, kinds) {
			// Direct actor.send call, outside the forwarding wrapper.
			args := callArguments(n, kinds)
			constructed, constructionReads := a.eventArgumentConstructionFacts(m, rel, args, src, kinds)
			out = append(out, constructed...)
			for f := range constructionReads {
				reads[f] = true
			}
			tags, resolved, calleeFile := a.eventArgumentTags(m, rel, args, src, kinds)
			if calleeFile != "" {
				reads[calleeFile] = true
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
	found := false
	walkFunctionScope(body, kinds, func(n *sitter.Node) {
		if kinds.Of(n) == "return_statement" && returnedObjectHasMethod(returnValue(n), method, src, kinds) {
			found = true
		}
	})
	return found
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
				return true
			}
		}
	}
	return false
}

func (a *Analyzer) eventArgumentTags(m *machineModel, from string, args []*sitter.Node, src []byte, kinds *tsutil.KindTable) ([]string, bool, string) {
	if len(args) == 0 {
		return nil, false, ""
	}
	expr := unwrapExpression(args[0], kinds)
	if tag, ok := eventObjectTag(expr, src, kinds); ok {
		return []string{tag}, true, ""
	}
	if kinds.Of(expr) == "identifier" {
		name := text(expr, src)
		value := variableValueFromFunction(expr, name, src, kinds)
		if value != nil {
			return a.eventArgumentTags(m, from, []*sitter.Node{unwrapExpression(value, kinds)}, src, kinds)
		}
	}
	if kinds.Of(expr) == "call_expression" {
		if tag, dep, ok := a.eventConstructorTag(m, from, expr, src, kinds); ok {
			return []string{tag}, true, dep
		}
		name := calleeName(expr, src, kinds)
		resolved, ok := a.resolvedImport(from, name)
		if !ok {
			return nil, false, ""
		}
		calleeFile, exportName := targetModule(resolved), targetExport(resolved)
		if calleeFile == "" || exportName == "" {
			return nil, false, ""
		}
		source := a.source(calleeFile)
		if len(source) == 0 {
			return nil, false, calleeFile
		}
		root, calleeKinds, done := parse(calleeFile, source)
		if done == nil {
			return nil, false, calleeFile
		}
		defer done()
		body := functionBody(root, exportName, source, calleeKinds)
		if body == nil {
			return nil, false, calleeFile
		}
		tags := []string{}
		unknown := false
		returns := 0
		walkFunctionScope(body, calleeKinds, func(n *sitter.Node) {
			if calleeKinds.Of(n) != "return_statement" {
				return
			}
			returns++
			expr := returnValue(n)
			if tag, ok := eventObjectTag(unwrapExpression(expr, calleeKinds), source, calleeKinds); ok {
				tags = append(tags, tag)
			} else if expr != nil {
				unknown = true
			}
		})
		if returns == 0 {
			unknown = true
		}
		return unique(tags), len(tags) > 0 && !unknown, calleeFile
	}
	return nil, false, ""
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
	calleeSource := a.source(calleeFile)
	if len(calleeSource) == 0 {
		return nil, reads
	}
	root, calleeKinds, done := parse(calleeFile, calleeSource)
	if done == nil {
		return nil, reads
	}
	defer done()
	body := functionBody(root, exportName, calleeSource, calleeKinds)
	if body == nil {
		return nil, reads
	}
	var out []facts.Fact
	walkFunctionScope(body, calleeKinds, func(n *sitter.Node) {
		if calleeKinds.Of(n) != "return_statement" {
			return
		}
		returned := returnValue(n)
		if tag, literal := eventObjectTag(unwrapExpression(returned, calleeKinds), calleeSource, calleeKinds); literal {
			out = append(out, interactionFact(calleeFile, exportName, facts.RelFSMConstructsEvent,
				machineMember(m.machine, "event", tag), returned, map[string]any{"construction_status": "literal_return", "event_tag": tag}))
		}
	})
	return out, reads
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
		tag, eventSource, eventOK := findConfiguredEventInArgument(m, args, sink.EventArgument, sink.EventPath, rel, src, kinds, a)
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

func configuredTypeImported(m *machineModel, file string, src []byte, kinds *tsutil.KindTable, a *Analyzer) bool {
	typeName := m.spec.CommandType
	if typeName == "" {
		typeName = m.spec.EffectType
	}
	if typeName == "" {
		return false
	}
	resolvedFile, resolvedExport := a.resolveType(file, typeName)
	return resolvedFile == m.commandFile && resolvedExport == m.commandExport
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

func memberFieldIs(n *sitter.Node, field string, src []byte, kinds *tsutil.KindTable) bool {
	if n == nil || kinds.Of(n) != "member_expression" {
		return false
	}
	return text(n.ChildByFieldName("property"), src) == field
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

func containsCallToAny(n *sitter.Node, names map[string]bool, src []byte, kinds *tsutil.KindTable) bool {
	found := false
	walk(n, func(c *sitter.Node) {
		if kinds.Of(c) == "call_expression" && names[calleeName(c, src, kinds)] {
			found = true
		}
	})
	return found
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

func firstArg(args []*sitter.Node, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if len(args) > 0 {
		return args[0]
	}
	return nil
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

func wrapperForSend(n *sitter.Node, wrappers, sends map[string]bool, src []byte, kinds *tsutil.KindTable) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		name := enclosingFunction(p, src, kinds)
		if wrappers[name] {
			return name
		}
		if kinds.Of(p) == "function_declaration" || kinds.Of(p) == "arrow_function" {
			break
		}
	}
	return ""
}

func memberExpr(n *sitter.Node) (string, bool) {
	if n == nil || n.Kind() != "member_expression" {
		return "", false
	}
	return "", true
}

func findConfiguredEventInArgument(m *machineModel, args []*sitter.Node, argIndex int, eventPath, file string, src []byte, kinds *tsutil.KindTable, a *Analyzer) (string, string, bool) {
	arg := eventArgumentAt(args, argIndex, eventPath, src, kinds)
	arg = unwrapExpression(arg, kinds)
	if tag, ok := eventObjectTag(arg, src, kinds); ok {
		return tag, "", true
	}
	tags, ok, dep := a.eventArgumentTags(m, file, []*sitter.Node{arg}, src, kinds)
	if ok && len(tags) == 1 {
		return tags[0], dep, true
	}
	return "", dep, false
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
