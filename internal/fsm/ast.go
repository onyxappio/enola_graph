package fsm

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/enola-labs/enola/internal/extractors/tsutil"
	"github.com/enola-labs/enola/internal/facts"
	sitter "github.com/tree-sitter/go-tree-sitter"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type sourceReader func(string) ([]byte, error)
type moduleResolver func(fromFile, specifier string) (string, bool)

type Analyzer struct {
	specs                     []Spec
	known                     map[string]bool
	read                      sourceReader
	resolve                   moduleResolver
	cache                     map[string][]byte
	importBindings            map[string]map[string]string
	sourceReads               map[string]bool
	modelSources              map[string]bool
	models                    map[string]*machineModel
	constructorUses           map[constructorUseKey]map[string]bool
	collectingConstructorUses bool
	mu                        sync.Mutex
}

type constructorUseKey struct {
	machine string
	file    string
	export  string
}

type machineModel struct {
	spec          Spec
	machine       string
	file          string
	eventFile     string
	eventExport   string
	commandFile   string
	commandExport string
	states        map[string]facts.Fact
	events        map[string]facts.Fact
	commands      map[string]facts.Fact
	facts         []facts.Fact
	reads         map[string]bool
	coverage      map[string]any
	partial       bool
}

// NewAnalyzer builds machine declarations once and reuses the parsed source
// index while each changed file contributes only its own FSM facts.
func NewAnalyzer(specs []Spec, knownFiles []string, read sourceReader, resolve moduleResolver) *Analyzer {
	a := &Analyzer{specs: append([]Spec(nil), specs...), known: map[string]bool{}, read: read, resolve: resolve,
		cache: map[string][]byte{}, importBindings: map[string]map[string]string{}, sourceReads: map[string]bool{}, modelSources: map[string]bool{}, models: map[string]*machineModel{}}
	for _, f := range knownFiles {
		a.known[slash(f)] = true
	}
	for _, spec := range a.specs {
		a.models[spec.ID] = a.buildModel(spec)
		// Model construction is synchronous and precedes interaction extraction.
		// Its complete resolver/type dependency set can therefore be frozen here
		// and reused when only dispatch/handler evidence changes.
		for file := range a.sourceReads {
			a.modelSources[file] = true
		}
	}
	a.indexConstructorUses()
	return a
}

// Rebind creates a session-local analyzer that reuses immutable machine models
// while reading changed interaction evidence through the current repository
// snapshot. The models and specs are read-only after NewAnalyzer returns.
func (a *Analyzer) Rebind(read sourceReader, resolve moduleResolver) *Analyzer {
	if a == nil {
		return nil
	}
	known := make(map[string]bool, len(a.known))
	for file, present := range a.known {
		known[file] = present
	}
	modelSources := make(map[string]bool, len(a.modelSources))
	for file := range a.modelSources {
		modelSources[file] = true
	}
	models := make(map[string]*machineModel, len(a.models))
	for id, model := range a.models {
		models[id] = model
	}
	rebound := &Analyzer{specs: a.specs, known: known, read: read, resolve: resolve,
		cache: map[string][]byte{}, importBindings: map[string]map[string]string{}, sourceReads: map[string]bool{}, modelSources: modelSources, models: models}
	rebound.indexConstructorUses()
	return rebound
}

// indexConstructorUses records exported functions called from configured
// dispatch files. Their literal event constructions are owned by the callee
// file; the caller file is retained as a dependency so removing that use
// refreshes the callee's complete contribution.
func (a *Analyzer) indexConstructorUses() {
	a.constructorUses = map[constructorUseKey]map[string]bool{}
	a.collectingConstructorUses = true
	defer func() { a.collectingConstructorUses = false }()
	for _, spec := range a.specs {
		if spec.Adapter != AdapterReducerInterpreter && spec.Adapter != AdapterRuleTable {
			continue
		}
		model := a.models[spec.ID]
		if model == nil {
			continue
		}
		for _, dispatchFile := range spec.DispatchFiles {
			dispatchFile = slash(dispatchFile)
			source := a.source(dispatchFile)
			if len(source) == 0 {
				continue
			}
			root, kinds, done := parse(dispatchFile, source)
			if done == nil {
				continue
			}
			// Only the configured sink proof is needed to index returned-event
			// constructors. Running the full interaction extractor here also
			// walks every event construction in the dispatch file, then repeats
			// that work when ExtractFile publishes the caller's contribution.
			for _, sink := range model.spec.DispatchSinks {
				if sink.MachineTag != nil {
					a.extractEffectSink(model, dispatchFile, root, source, kinds, sink)
				} else {
					a.extractObjectSink(model, dispatchFile, root, source, kinds, sink)
				}
			}
			done()
		}
	}
}

func (a *Analyzer) hasConstructorUses(machine, file string) bool {
	if a == nil {
		return false
	}
	file = slash(file)
	for key := range a.constructorUses {
		if key.machine == machine && key.file == file {
			return true
		}
	}
	return false
}

func (a *Analyzer) constructorExports(machine, file string) []string {
	file = slash(file)
	var exports []string
	for key := range a.constructorUses {
		if key.machine == machine && key.file == file {
			exports = append(exports, key.export)
		}
	}
	sort.Strings(exports)
	return exports
}

func (a *Analyzer) constructorCallers(machine, file, export string) []string {
	key := constructorUseKey{machine: machine, file: slash(file), export: export}
	callers := make([]string, 0, len(a.constructorUses[key]))
	for caller := range a.constructorUses[key] {
		callers = append(callers, caller)
	}
	sort.Strings(callers)
	return callers
}

// ExtractFile returns local FSM declarations/relations owned by relFile and
// files read to prove those facts. All cross-file references remain explicit
// relations; this pass does not materialize transitive properties.
func (a *Analyzer) ExtractFile(relFile string, src []byte) ([]facts.Fact, []string) {
	if a == nil || len(a.specs) == 0 {
		return nil, nil
	}
	relFile = slash(relFile)
	if src == nil {
		return nil, nil
	}
	var out []facts.Fact
	reads := map[string]bool{}
	for _, spec := range a.specs {
		m := a.models[spec.ID]
		if m == nil {
			continue
		}
		isMachineFile := relFile == slash(spec.File)
		isDispatchFile := containsPath(spec.DispatchFiles, relFile)
		isHandlerFile := containsPath(spec.HandlerFiles, relFile) || (spec.EffectRunner != nil && slash(spec.EffectRunner.Module) == relFile)
		isConstructorFile := a.hasConstructorUses(spec.ID, relFile)
		isTypedConstructorFile := a.sourceHasConfiguredEventReturnType(relFile, src, m)
		if isMachineFile {
			out = append(out, m.facts...)
			for f := range m.reads {
				if f != relFile {
					reads[f] = true
				}
			}
		}
		if (isDispatchFile || isHandlerFile || isConstructorFile || isTypedConstructorFile) && (spec.Adapter == AdapterReducerInterpreter || spec.Adapter == AdapterRuleTable) {
			ff, rr := a.extractInteractions(m, relFile, src, isConstructorFile || isTypedConstructorFile)
			out = append(out, ff...)
			if len(ff) > 0 {
				for f := range m.reads {
					if f != relFile {
						reads[f] = true
					}
				}
			}
			for f := range rr {
				if f != relFile {
					reads[f] = true
				}
			}
		}
	}
	readList := make([]string, 0, len(reads))
	for f := range reads {
		readList = append(readList, f)
	}
	sort.Strings(readList)
	return out, readList
}

func (a *Analyzer) buildModel(spec Spec) *machineModel {
	m := &machineModel{spec: spec, machine: spec.ID, file: slash(spec.File), states: map[string]facts.Fact{}, events: map[string]facts.Fact{}, commands: map[string]facts.Fact{}, reads: map[string]bool{}, coverage: map[string]any{}}
	src := a.source(spec.File)
	if len(src) == 0 {
		m.partial = true
		m.coverage["coverage_status"] = "unknown"
		return m
	}
	m.reads[m.file] = true
	root, kinds, done := parse(spec.File, src)
	if done == nil {
		m.partial = true
		m.coverage["coverage_status"] = "unknown"
		return m
	}
	defer done()
	if spec.EventType != "" {
		m.eventFile, m.eventExport = a.resolveType(m.file, spec.EventType)
		if m.eventFile == "" {
			m.eventFile, m.eventExport = m.file, spec.EventType
		}
		m.reads[m.eventFile] = true
	}
	commandType := spec.CommandType
	if spec.Adapter == AdapterReducerInterpreter && commandType == "" {
		commandType = spec.EffectType
	}
	if commandType != "" {
		m.commandFile, m.commandExport = a.resolveType(m.file, commandType)
		if m.commandFile == "" {
			m.commandFile, m.commandExport = m.file, commandType
		}
		m.reads[m.commandFile] = true
	}
	if spec.Adapter == AdapterRuleTable {
		a.buildRuleTable(m, root, kinds, src)
	} else {
		a.buildInterpreter(m, root, kinds, src)
	}
	return m
}

func (a *Analyzer) source(rel string) []byte {
	rel = slash(rel)
	a.mu.Lock()
	if a.sourceReads == nil {
		a.sourceReads = map[string]bool{}
	}
	if a.cache == nil {
		a.cache = map[string][]byte{}
	}
	a.sourceReads[rel] = true
	if b, ok := a.cache[rel]; ok {
		a.mu.Unlock()
		return b
	}
	a.mu.Unlock()
	if a.read == nil {
		return nil
	}
	b, err := a.read(rel)
	if err != nil {
		return nil
	}
	a.mu.Lock()
	a.cache[rel] = b
	a.mu.Unlock()
	return b
}

// SourceFiles returns the repository files whose contents the analyzer read
// while resolving configured declarations and interactions.
func (a *Analyzer) SourceFiles() []string {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	out := make([]string, 0, len(a.sourceReads))
	for file := range a.sourceReads {
		out = append(out, file)
	}
	a.mu.Unlock()
	sort.Strings(out)
	return out
}

// ModelSourceFiles returns source files whose contents were read while
// constructing configured machine declarations. A change to one of these
// inputs requires rebuilding the semantic model; interaction-only changes can
// reuse the model and refresh cross-file event/dispatch evidence.
func (a *Analyzer) ModelSourceFiles() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.modelSources))
	for file := range a.modelSources {
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func parse(rel string, src []byte) (*sitter.Node, *tsutil.KindTable, func()) {
	lang := typescript.LanguageTypescript()
	if strings.HasSuffix(rel, ".tsx") || strings.HasSuffix(rel, ".jsx") || strings.HasSuffix(rel, ".gts") || strings.HasSuffix(rel, ".gjs") {
		lang = typescript.LanguageTSX()
	}
	p := sitter.NewParser()
	if err := p.SetLanguage(sitter.NewLanguage(lang)); err != nil {
		p.Close()
		return nil, nil, nil
	}
	tree := p.Parse(src, nil)
	if tree == nil {
		p.Close()
		return nil, nil, nil
	}
	return tree.RootNode(), tsutil.KindsFor(lang), func() { tree.Close(); p.Close() }
}

func slash(value string) string { return filepath.ToSlash(filepath.Clean(value)) }

func text(n *sitter.Node, src []byte) string {
	if n == nil || n.StartByte() > uint(len(src)) || n.EndByte() > uint(len(src)) || n.EndByte() < n.StartByte() {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

func walk(n *sitter.Node, visit func(*sitter.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for i := uint(0); i < n.ChildCount(); i++ {
		walk(n.Child(i), visit)
	}
}

// walkFunctionScope visits one function body without treating returns/calls in
// nested closures or declarations as part of the enclosing function's control
// flow. root is the body node itself; the function body under analysis remains
// fully traversed.
func walkFunctionScope(root *sitter.Node, kinds *tsutil.KindTable, visit func(*sitter.Node)) {
	var descend func(*sitter.Node, bool)
	descend = func(n *sitter.Node, isRoot bool) {
		if n == nil || (!isRoot && isFunctionBoundary(kinds.Of(n))) {
			return
		}
		visit(n)
		for i := uint(0); i < n.ChildCount(); i++ {
			descend(n.Child(i), false)
		}
	}
	descend(root, true)
}

func isFunctionBoundary(kind string) bool {
	switch kind {
	case "function_declaration", "generator_function_declaration", "function_expression",
		"arrow_function", "method_definition":
		return true
	default:
		return false
	}
}

func namedChildren(n *sitter.Node) []*sitter.Node {
	if n == nil {
		return nil
	}
	out := make([]*sitter.Node, 0, n.NamedChildCount())
	for i := uint(0); i < n.NamedChildCount(); i++ {
		out = append(out, n.NamedChild(i))
	}
	return out
}

func stringValue(n *sitter.Node, src []byte, kinds *tsutil.KindTable) (string, bool) {
	if n == nil {
		return "", false
	}
	k := kinds.Of(n)
	if k == "string" || k == "string_fragment" {
		v := strings.TrimSpace(text(n, src))
		if k == "string" && len(v) >= 2 && (v[0] == '\'' || v[0] == '"' || v[0] == '`') {
			return v[1 : len(v)-1], true
		}
		return v, true
	}
	return "", false
}

func objectPair(n *sitter.Node, key string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if n == nil || kinds.Of(n) != "object" {
		return nil
	}
	for _, c := range namedChildren(n) {
		if kinds.Of(c) != "pair" {
			continue
		}
		k := c.ChildByFieldName("key")
		if k == nil || text(k, src) != key {
			continue
		}
		return c.ChildByFieldName("value")
	}
	return nil
}

func functionName(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if name := n.ChildByFieldName("name"); name != nil {
		return text(name, src)
	}
	return ""
}

func calleeName(call *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	if call == nil || kinds.Of(call) != "call_expression" {
		return ""
	}
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	if kinds.Of(fn) == "identifier" {
		return text(fn, src)
	}
	if kinds.Of(fn) == "member_expression" {
		return text(fn.ChildByFieldName("property"), src)
	}
	return ""
}

func callArguments(call *sitter.Node, kinds *tsutil.KindTable) []*sitter.Node {
	if call == nil {
		return nil
	}
	args := call.ChildByFieldName("arguments")
	if args == nil || kinds.Of(args) != "arguments" {
		return nil
	}
	return namedChildren(args)
}

func namedFunction(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	var found *sitter.Node
	walk(root, func(n *sitter.Node) {
		if found == nil && kinds.Of(n) == "function_declaration" && functionName(n, src) == name {
			found = n
		}
	})
	return found
}

func functionBody(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	if fn := namedFunction(root, name, src, kinds); fn != nil {
		return fn.ChildByFieldName("body")
	}
	value := variableValue(root, name, src, kinds)
	if value == nil {
		return nil
	}
	if kinds.Of(value) == "arrow_function" || kinds.Of(value) == "function_expression" {
		return value.ChildByFieldName("body")
	}
	return nil
}

func (a *Analyzer) sourceHasConfiguredEventReturnType(file string, src []byte, m *machineModel) bool {
	if a == nil || m == nil || m.eventFile == "" || m.eventExport == "" || !sourceMayContainTypedEventReturn(src) {
		return false
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return false
	}
	defer done()
	for _, export := range topLevelFunctionExports(root, src, kinds) {
		function, _ := topLevelExportedFunction(root, export, src, kinds)
		if function == nil {
			continue
		}
		returnType := functionReturnTypeName(function, src, kinds)
		if returnType == "" {
			continue
		}
		resolvedFile, resolvedExport := a.resolveType(file, returnType)
		if slash(resolvedFile) == slash(m.eventFile) && resolvedExport == m.eventExport {
			return true
		}
	}
	return false
}

// sourceMayContainTypedEventReturn is a cheap candidate check. It deliberately
// accepts unrelated return types and aliases; the parsed declaration and
// canonical resolver below establish whether the type is the configured event.
// Comments and newlines may appear between the closing parenthesis, colon,
// and identifier in a TypeScript return annotation.
func sourceMayContainTypedEventReturn(src []byte) bool {
	if len(src) == 0 || !sourceContainsWord(src, "export") ||
		(!sourceContainsWord(src, "import") && !sourceContainsWord(src, "type") && !sourceContainsWord(src, "interface")) {
		return false
	}
	for i, value := range src {
		if value != ')' {
			continue
		}
		j := skipSourceTrivia(src, i+1)
		if j >= len(src) || src[j] != ':' {
			continue
		}
		j = skipSourceTrivia(src, j+1)
		if j < len(src) && isIdentifierStart(src[j]) {
			return true
		}
	}
	return false
}

func sourceContainsWord(src []byte, word string) bool {
	if word == "" {
		return false
	}
	for i := 0; i+len(word) <= len(src); i++ {
		matched := true
		for j := range word {
			if src[i+j] != word[j] {
				matched = false
				break
			}
		}
		if !matched || (i > 0 && isIdentifierByte(src[i-1])) ||
			(i+len(word) < len(src) && isIdentifierByte(src[i+len(word)])) {
			continue
		}
		return true
	}
	return false
}

func skipSourceTrivia(src []byte, i int) int {
	for i < len(src) {
		if isSpace(src[i]) {
			i++
			continue
		}
		if i+1 >= len(src) || src[i] != '/' {
			return i
		}
		switch src[i+1] {
		case '/':
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case '*':
			i += 2
			for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
				i++
			}
			if i+1 >= len(src) {
				return len(src)
			}
			i += 2
		default:
			return i
		}
	}
	return i
}

func isSpace(value byte) bool { return value == ' ' || value == '\t' || value == '\n' || value == '\r' }

func isIdentifierByte(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func variableValue(root *sitter.Node, name string, src []byte, kinds *tsutil.KindTable) *sitter.Node {
	var found *sitter.Node
	walk(root, func(n *sitter.Node) {
		if found != nil || kinds.Of(n) != "variable_declarator" {
			return
		}
		if id := n.ChildByFieldName("name"); id != nil && text(id, src) == name {
			found = n.ChildByFieldName("value")
		}
	})
	return found
}

func topLevelExport(root *sitter.Node, exportName string, src []byte, kinds *tsutil.KindTable) bool {
	if root == nil {
		return false
	}
	for _, n := range namedChildren(root) {
		if kinds.Of(n) != "export_statement" {
			continue
		}
		for _, child := range namedChildren(n) {
			switch kinds.Of(child) {
			case "function_declaration", "class_declaration", "abstract_class_declaration",
				"interface_declaration", "type_alias_declaration", "enum_declaration":
				if functionName(child, src) == exportName || text(child.ChildByFieldName("name"), src) == exportName {
					return true
				}
			case "lexical_declaration", "variable_declaration":
				for _, declaration := range namedChildren(child) {
					if kinds.Of(declaration) == "variable_declarator" && text(declaration.ChildByFieldName("name"), src) == exportName {
						return true
					}
				}
			case "export_clause":
				for _, item := range namedChildren(child) {
					local, alias := text(item.ChildByFieldName("name"), src), text(item.ChildByFieldName("alias"), src)
					if alias == "" {
						alias = local
					}
					if alias == exportName {
						return true
					}
				}
			}
		}
	}
	return false
}

func moduleSpec(n *sitter.Node, src []byte, kinds *tsutil.KindTable) string {
	if n == nil {
		return ""
	}
	if source := n.ChildByFieldName("source"); source != nil && kinds.Of(source) == "string" {
		value, _ := stringValue(source, src, kinds)
		return value
	}
	for _, child := range namedChildren(n) {
		if kinds.Of(child) == "string" {
			value, _ := stringValue(child, src, kinds)
			return value
		}
	}
	return ""
}

func (a *Analyzer) importedLocal(file, local string) (string, bool) {
	bindings := a.importBindingsFor(file)
	target, found := bindings[local]
	return target, found
}

// importBindingsFor parses a source file's imports once per Analyzer. Import
// resolution is used by several independent FSM proof walks; reparsing and
// walking the complete file for every local import dominates constructor
// indexing on import-heavy dispatch files. Rebind creates a fresh map so no
// result crosses a source snapshot.
func (a *Analyzer) importBindingsFor(file string) map[string]string {
	file = slash(file)
	a.mu.Lock()
	if a.importBindings == nil {
		a.importBindings = map[string]map[string]string{}
	}
	if cached, ok := a.importBindings[file]; ok {
		a.mu.Unlock()
		return cached
	}
	a.mu.Unlock()

	bindings := map[string]string{}
	src := a.source(file)
	if len(src) > 0 {
		root, kinds, done := parse(file, src)
		if done != nil {
			for _, statement := range namedChildren(root) {
				if kinds.Of(statement) != "import_statement" {
					continue
				}
				specifier := moduleSpec(statement, src, kinds)
				if specifier == "" {
					continue
				}
				base := a.resolvePath(file, specifier)
				if base == "" {
					continue
				}
				var add func(*sitter.Node)
				add = func(node *sitter.Node) {
					if node == nil {
						return
					}
					switch kinds.Of(node) {
					case "import_specifier":
						name := text(node.ChildByFieldName("name"), src)
						alias := text(node.ChildByFieldName("alias"), src)
						if alias == "" {
							alias = name
						}
						if name != "" {
							if _, exists := bindings[alias]; !exists {
								bindings[alias] = base + "#" + name
							}
						}
						return
					case "namespace_import":
						if node.NamedChildCount() > 0 {
							local := text(node.NamedChild(0), src)
							if _, exists := bindings[local]; !exists {
								bindings[local] = base + "#*"
							}
						}
						return
					case "identifier":
						local := text(node, src)
						if _, exists := bindings[local]; !exists {
							bindings[local] = base
						}
						return
					}
					for _, child := range namedChildren(node) {
						add(child)
					}
				}
				for _, clause := range namedChildren(statement) {
					if kinds.Of(clause) == "import_clause" {
						add(clause)
					}
				}
			}
			done()
		}
	}

	a.mu.Lock()
	if cached, ok := a.importBindings[file]; ok {
		bindings = cached
	} else {
		a.importBindings[file] = bindings
	}
	a.mu.Unlock()
	return bindings
}

func (a *Analyzer) resolvePath(from, spec string) string {
	if a.resolve == nil || spec == "" {
		return ""
	}
	resolved, ok := a.resolve(slash(from), spec)
	if !ok {
		return ""
	}
	return slash(resolved)
}

// resolveExport follows named re-exports and aliases until it reaches the
// source declaration. Cycles and unsupported export forms remain unresolved.
func (a *Analyzer) resolveExport(file, exported string, seen map[string]bool) (string, bool) {
	key := slash(file) + "#" + exported
	if seen[key] {
		return "", false
	}
	seen[key] = true
	src := a.source(file)
	if len(src) == 0 {
		return "", false
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return "", false
	}
	defer done()
	explicit := topLevelExport(root, exported, src, kinds)
	if !explicit && !hasExportStar(root, src, kinds) {
		return "", false
	}
	for _, n := range namedChildren(root) {
		if kinds.Of(n) != "export_statement" {
			continue
		}
		spec := moduleSpec(n, src, kinds)
		base := ""
		if spec != "" {
			base = a.resolvePath(file, spec)
		}
		for _, child := range namedChildren(n) {
			if kinds.Of(child) != "export_clause" {
				continue
			}
			for _, item := range namedChildren(child) {
				local := text(item.ChildByFieldName("name"), src)
				alias := text(item.ChildByFieldName("alias"), src)
				if alias == "" {
					alias = local
				}
				if alias != exported {
					continue
				}
				if base != "" {
					return a.resolveExport(base, local, seen)
				}
				if spec != "" {
					return "", false
				}
				if local != exported {
					if target, ok := a.resolveExport(file, local, seen); ok {
						return target, true
					}
					if imported, ok := a.importedLocal(file, local); ok {
						module, name, hasName := strings.Cut(imported, "#")
						if hasName {
							return a.resolveExport(module, name, seen)
						}
					}
					if a.hasLocalDeclaration(file, local) {
						return slash(file) + "#" + local, true
					}
				}
			}
		}
		if strings.Contains(text(n, src), "export *") && spec != "" && base != "" {
			if target, ok := a.resolveExport(base, exported, seen); ok {
				return target, true
			}
		}
	}
	if !explicit {
		// `export * from` has no local export clause. Resolving the requested
		// name through the target file proves that it is actually re-exported.
		for _, n := range namedChildren(root) {
			if kinds.Of(n) != "export_statement" || !strings.Contains(text(n, src), "export *") {
				continue
			}
			if spec := moduleSpec(n, src, kinds); spec != "" {
				if base := a.resolvePath(file, spec); base != "" {
					return a.resolveExport(base, exported, seen)
				}
			}
		}
		return "", false
	}
	return key, true
}

func (a *Analyzer) hasLocalDeclaration(file, name string) bool {
	src := a.source(file)
	if len(src) == 0 {
		return false
	}
	root, kinds, done := parse(file, src)
	if done == nil {
		return false
	}
	defer done()
	declares := func(n *sitter.Node) bool {
		switch kinds.Of(n) {
		case "function_declaration", "class_declaration", "abstract_class_declaration",
			"interface_declaration", "type_alias_declaration", "enum_declaration":
			return text(n.ChildByFieldName("name"), src) == name
		case "lexical_declaration", "variable_declaration":
			for _, d := range namedChildren(n) {
				if kinds.Of(d) == "variable_declarator" && text(d.ChildByFieldName("name"), src) == name {
					return true
				}
			}
		}
		return false
	}
	for _, n := range namedChildren(root) {
		if declares(n) {
			return true
		}
		// TypeScript wraps exported declarations in an export_statement. Inspect
		// only its direct declaration child: walking its whole subtree would let
		// nested locals masquerade as module-level source bindings.
		if kinds.Of(n) == "export_statement" {
			for _, child := range namedChildren(n) {
				if declares(child) {
					return true
				}
			}
		}
	}
	return false
}

func hasExportStar(root *sitter.Node, src []byte, kinds *tsutil.KindTable) bool {
	for _, n := range namedChildren(root) {
		if kinds.Of(n) == "export_statement" && strings.Contains(text(n, src), "export *") {
			return true
		}
	}
	return false
}

func (a *Analyzer) resolvedImport(file, local string) (string, bool) {
	imp, ok := a.importedLocal(file, local)
	if !ok {
		return "", false
	}
	module, exportName, hasExport := strings.Cut(imp, "#")
	if !hasExport {
		return "", false
	}
	return a.resolveExport(module, exportName, map[string]bool{})
}

func nodeLine(n *sitter.Node) int { return int(n.StartPosition().Row) + 1 }

func relation(kind, target string) facts.Relation {
	return facts.Relation{Kind: kind, Target: target}
}

func relationAtFile(kind, target, targetFile string) facts.Relation {
	return facts.Relation{Kind: kind, Target: target, TargetFile: slash(targetFile)}
}

// symbolRelation binds a local spelling to a real same-file declaration or an
// imported/re-exported declaration. The canonical target and TargetFile match
// the TypeScript extractor's symbol identities so streaming can resolve the
// same source symbol without guessing among same-named facts.
func (a *Analyzer) symbolRelation(fromFile, local, relationKind string) (facts.Relation, bool) {
	if local == "" {
		return facts.Relation{}, false
	}
	fromFile = slash(fromFile)
	if resolved, ok := a.resolvedImport(fromFile, local); ok {
		file, exported := targetModule(resolved), targetExport(resolved)
		if file == "" || exported == "" || !a.hasLocalDeclaration(file, exported) {
			return facts.Relation{}, false
		}
		return relationAtFile(relationKind, moduleName(file)+"."+exported, file), true
	}
	if a.hasLocalDeclaration(fromFile, local) {
		return relationAtFile(relationKind, moduleName(fromFile)+"."+local, fromFile), true
	}
	return facts.Relation{}, false
}

func (a *Analyzer) typeRelation(fromFile, typeName, relationKind string) (facts.Relation, bool) {
	file, exported := a.resolveType(fromFile, typeName)
	if file == "" || exported == "" || !a.hasType(file, exported) {
		return facts.Relation{}, false
	}
	return relationAtFile(relationKind, moduleName(file)+"."+exported, file), true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
