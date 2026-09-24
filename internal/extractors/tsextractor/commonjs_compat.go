package tsextractor

import (
	"github.com/enola-labs/enola/internal/extractors/tsutil"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// commonJSBindings records when the module-local identifier wins over Node's
// synthetic CommonJS binding. A local declaration or rebinding disables
// recognition of that spelling throughout this conservative file-level pass.
type commonJSBindings struct {
	localExports bool
	localModule  bool
}

func (b commonJSBindings) shadowed(name string) bool {
	switch name {
	case "exports":
		return b.localExports
	case "module":
		return b.localModule
	default:
		return true
	}
}

func collectCommonJSModuleBindings(kinds *tsutil.KindTable, root *sitter.Node, src []byte) commonJSBindings {
	var bindings commonJSBindings
	if root == nil {
		return bindings
	}
	for i := range root.NamedChildCount() {
		child := root.NamedChild(i)
		if kindOf(kinds, child) == "export_statement" {
			for j := range child.NamedChildCount() {
				markDirectCommonJSDeclaration(kinds, child.NamedChild(j), src, &bindings)
			}
			continue
		}
		if kindOf(kinds, child) == "import_statement" {
			collectCommonJSImportBindings(kinds, child, src, &bindings)
			continue
		}
		markDirectCommonJSDeclaration(kinds, child, src, &bindings)
	}

	// A write to the synthetic variable itself severs its host provenance. Walk
	// module-level control-flow blocks too, but stop at every function boundary;
	// assignments in a callback or declaration body do not rebind the module.
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch kindOf(kinds, node) {
		case "function_declaration", "generator_function_declaration", "function_expression",
			"generator_function", "arrow_function", "method_definition", "class_declaration", "class":
			return
		case "assignment_expression":
			left := node.ChildByFieldName("left")
			if left != nil && kindOf(kinds, left) == "identifier" {
				markCommonJSBindingName(kinds, left, src, &bindings)
			}
		}
		for i := range node.NamedChildCount() {
			walk(node.NamedChild(i))
		}
	}
	for i := range root.NamedChildCount() {
		walk(root.NamedChild(i))
	}
	return bindings
}

func markDirectCommonJSDeclaration(kinds *tsutil.KindTable, node *sitter.Node, src []byte, out *commonJSBindings) {
	if node == nil {
		return
	}
	switch kindOf(kinds, node) {
	case "lexical_declaration", "variable_declaration":
		// A declaration at the program level shadows the host-provided name,
		// even if the declaration appears after an apparent export use.
		markCommonJSBindingNames(kinds, node, src, out)
	case "function_declaration", "generator_function_declaration", "class_declaration":
		markCommonJSBindingName(kinds, node.ChildByFieldName("name"), src, out)
	}
}

func markCommonJSBindingNames(kinds *tsutil.KindTable, declaration *sitter.Node, src []byte, out *commonJSBindings) {
	if declaration == nil {
		return
	}
	for i := range declaration.NamedChildCount() {
		decl := declaration.NamedChild(i)
		if kindOf(kinds, decl) != "variable_declarator" {
			continue
		}
		name := decl.ChildByFieldName("name")
		if name == nil {
			continue
		}
		for _, bound := range bindingNamesFromPattern(kinds, name, src) {
			markCommonJSBindingNameText(bound, out)
		}
	}
}

func markCommonJSBindingName(kinds *tsutil.KindTable, node *sitter.Node, src []byte, out *commonJSBindings) {
	if node == nil {
		return
	}
	markCommonJSBindingNameText(nodeText(node, src), out)
}

func markCommonJSBindingNameText(name string, out *commonJSBindings) {
	switch name {
	case "exports":
		out.localExports = true
	case "module":
		out.localModule = true
	}
}

func collectCommonJSImportBindings(kinds *tsutil.KindTable, stmt *sitter.Node, src []byte, out *commonJSBindings) {
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		switch kindOf(kinds, node) {
		case "import_clause":
			for i := range node.NamedChildCount() {
				child := node.NamedChild(i)
				switch kindOf(kinds, child) {
				case "identifier": // default import
					markCommonJSBindingName(kinds, child, src, out)
				case "namespace_import":
					if name := findChildByKind(kinds, child, "identifier"); name != nil {
						markCommonJSBindingName(kinds, name, src, out)
					}
				case "named_imports":
					for j := range child.NamedChildCount() {
						spec := child.NamedChild(j)
						if kindOf(kinds, spec) != "import_specifier" {
							continue
						}
						local := spec.ChildByFieldName("alias")
						if local == nil {
							local = spec.ChildByFieldName("name")
						}
						markCommonJSBindingName(kinds, local, src, out)
					}
				}
			}
			return
		}
		for i := range node.NamedChildCount() {
			walk(node.NamedChild(i))
		}
	}
	walk(stmt)
}

// commonJSExportTargetBinding recognizes only the host-bound spellings used by
// CommonJS: exports.name, module.exports.name, and the whole module.exports
// assignment. Parenthesized assignment values are handled on the RHS.
func commonJSExportTargetBinding(kinds *tsutil.KindTable, left *sitter.Node, src []byte) string {
	if left == nil || kindOf(kinds, left) != "member_expression" {
		return ""
	}
	obj := left.ChildByFieldName("object")
	prop := left.ChildByFieldName("property")
	if obj == nil || prop == nil || kindOf(kinds, prop) != "property_identifier" {
		return ""
	}
	if kindOf(kinds, obj) == "identifier" && nodeText(obj, src) == "exports" {
		return "exports"
	}
	if kindOf(kinds, obj) == "member_expression" {
		inner := obj.ChildByFieldName("object")
		innerProp := obj.ChildByFieldName("property")
		if inner != nil && kindOf(kinds, inner) == "identifier" && nodeText(inner, src) == "module" &&
			innerProp != nil && kindOf(kinds, innerProp) == "property_identifier" && nodeText(innerProp, src) == "exports" {
			return "module"
		}
	}
	if kindOf(kinds, obj) == "identifier" && nodeText(obj, src) == "module" &&
		nodeText(prop, src) == "exports" {
		return "module"
	}
	return ""
}

func commonJSExportIsHostBinding(kinds *tsutil.KindTable, left *sitter.Node, src []byte, bindings commonJSBindings) bool {
	name := commonJSExportTargetBinding(kinds, left, src)
	return name != "" && !bindings.shadowed(name)
}
