package tsextractor

import (
	"context"

	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/tsutil"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
)

// TypeScript storage modelling — TypeORM, Drizzle and Prisma.
//
// tsextractor emitted ZERO storage facts before this, making TypeScript the only backend
// language in enola that models no tables: Go, Java, Kotlin, Python and Ruby all do. A
// database-backed Node service therefore reported no storage at all in explore,
// impact_analysis, llm_context and --explain's data surface.
//
// Facts are named after the DECLARATION (dir + "." + name) with the physical table in a
// `table` prop — the dominant convention (Kotlin's Room detectRoomStorage, Java's JPA
// detectJpaStorage, Ruby's ActiveRecord models). The class/const still emits its own
// symbol fact; the storage fact is a companion, not a replacement.

// ormDeps are the package.json dependencies that switch each ORM on. Detection is gated
// on the dependency, so a class coincidentally decorated `@Entity`, or a helper named
// `pgTable`, models nothing in a repo that does not use the ORM.
const (
	depTypeORM = "typeorm"
	depDrizzle = "drizzle-orm"
	depPrisma  = "@prisma/client"
)

// drizzleTableFns are Drizzle's per-dialect table constructors.
var drizzleTableFns = map[string]bool{
	"pgTable": true, "sqliteTable": true, "mysqlTable": true,
}

// detectORMs reports which ORMs the repo declares, reusing the same package.json
// primitive (and the same tsRoot + repo-root fallback) that Vue/Nuxt detection uses.
func detectORMs(ctx context.Context, repoPath string, inputScopes ...*inputscope.Scope) (typeorm, drizzle, prisma bool) {
	inputScope := inputscope.First(inputScopes)
	tsRoot, _ := findTSRoot(ctx, repoPath, inputScope)
	has := func(pkg string) bool {
		return hasPkgDependency(ctx, tsRoot, pkg, inputScope) || (tsRoot != repoPath && hasPkgDependency(ctx, repoPath, pkg, inputScope))
	}
	return has(depTypeORM), has(depDrizzle), has(depPrisma)
}

// typeORMEntityStorage emits a storage fact for a class decorated `@Entity()` or
// `@Entity("users")`.
//
// tsextractor read NO decorators before this — there was not one occurrence in the whole
// package — so this is the extractor's first decorator support. Decorator nodes hang off
// the class_declaration itself, which is why the class branch is the hook.
func typeORMEntityStorage(kinds *tsutil.KindTable, node *sitter.Node, src []byte, className, relFile, dir string, line int) *facts.Fact {
	name, arg, ok := classDecorator(kinds, node, src, "Entity")
	if !ok || name == "" {
		return nil
	}
	table := arg
	if table == "" {
		// `@Entity()` with no argument: TypeORM defaults the table to the class name.
		table = className
	}
	return &facts.Fact{
		Kind: facts.KindStorage,
		Name: dir + "." + className,
		File: relFile,
		Line: line,
		Props: map[string]any{
			"storage_kind": "entity",
			"language":     "typescript",
			"framework":    "typeorm",
			"table":        table,
		},
		Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: dir}},
	}
}

// classDecorator finds a decorator by name on a class node and returns its first string
// argument, if any. Handles both `@Entity` and `@Entity("users")` shapes.
//
// The decorator hangs off DIFFERENT nodes depending on export:
//
//	class_declaration        →  decorator, class, type_identifier, class_body   (bare class)
//	export_statement         →  decorator, export, class_declaration            (exported class)
//
// so a search of the class node alone finds nothing for `@Entity() export class X` — which
// is the shape essentially every real entity uses. Look at the class node, then at its
// parent.
func classDecorator(kinds *tsutil.KindTable, node *sitter.Node, src []byte, want string) (name, arg string, ok bool) {
	if n, a, found := decoratorIn(kinds, node, src, want); found {
		return n, a, true
	}
	if parent := node.Parent(); parent != nil && kindOf(kinds, parent) == "export_statement" {
		return decoratorIn(kinds, parent, src, want)
	}
	return "", "", false
}

// decoratorIn scans one node's immediate children for a named decorator and returns
// its first string argument. A thin wrapper over decoratorArgsIn, which owns the
// scan — @Controller({path: "…"}) needs the arguments NODE rather than its first
// string, and the two must not drift into separate traversals.
func decoratorIn(kinds *tsutil.KindTable, node *sitter.Node, src []byte, want string) (name, arg string, ok bool) {
	args, found := decoratorArgsIn(kinds, node, src, want)
	if !found {
		return "", "", false
	}
	return want, firstStringArg(kinds, args, src), true
}

// decoratorArgsIn scans one node's immediate children for a named decorator and
// returns its call arguments. A bare decorator (@Entity, @Get) yields a nil args
// node with ok=true — present, but carrying nothing.
func decoratorArgsIn(kinds *tsutil.KindTable, node *sitter.Node, src []byte, want string) (args *sitter.Node, ok bool) {
	if node == nil {
		return nil, false
	}
	for i := range node.ChildCount() {
		child := node.Child(i)
		if kindOf(kinds, child) != "decorator" {
			continue
		}
		// The decorator wraps either a bare identifier (@Entity) or a call (@Entity(...)).
		for j := range child.ChildCount() {
			inner := child.Child(j)
			switch kindOf(kinds, inner) {
			case "identifier":
				if nodeText(inner, src) == want {
					return nil, true
				}
			case "call_expression":
				fn := inner.ChildByFieldName("function")
				if fn == nil || nodeText(fn, src) != want {
					continue
				}
				return inner.ChildByFieldName("arguments"), true
			}
		}
	}
	return nil, false
}

// drizzleTableStorage emits a storage fact for `export const orders = pgTable("orders", …)`.
// The call_expression is already in hand at the lexical_declaration branch — it simply
// fails the isComponentWrapper check today.
func drizzleTableStorage(kinds *tsutil.KindTable, call *sitter.Node, src []byte, constName, relFile, dir string, line int) *facts.Fact {
	fn := call.ChildByFieldName("function")
	if fn == nil || !drizzleTableFns[nodeText(fn, src)] {
		return nil
	}
	table := firstStringArg(kinds, call.ChildByFieldName("arguments"), src)
	if table == "" {
		table = constName
	}
	rels := []facts.Relation{{Kind: facts.RelDeclares, Target: dir}}
	props := map[string]any{
		"storage_kind": "entity",
		"language":     "typescript",
		"framework":    "drizzle",
		"table":        table,
	}
	if fks := drizzleExplicitReferences(kinds, call, src); len(fks) > 0 {
		props["fk_constraints"] = strings.Join(fks, " ")
		seen := map[string]bool{}
		for _, spec := range fks {
			// "subjectId->scanSubjects.subjectId"
			right := spec
			if i := strings.IndexByte(spec, '>'); i >= 0 && i+1 < len(spec) {
				right = spec[i+1:]
			}
			tableIdent := right
			if i := strings.IndexByte(right, '.'); i >= 0 {
				tableIdent = right[:i]
			}
			target := dir + "." + tableIdent
			if tableIdent == "" || seen[target] {
				continue
			}
			seen[target] = true
			rels = append(rels, facts.Relation{Kind: facts.RelDependsOn, Target: target})
		}
	}
	return &facts.Fact{
		Kind:      facts.KindStorage,
		Name:      dir + "." + constName,
		File:      relFile,
		Line:      line,
		Props:     props,
		Relations: rels,
	}
}

// drizzleExplicitReferences reads `.references(() => table.column)` on pgTable
// column builders. Naming-only guesses and a bare `references()` identifier are
// ignored.
func drizzleExplicitReferences(kinds *tsutil.KindTable, call *sitter.Node, src []byte) []string {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var columns *sitter.Node
	for i := range args.ChildCount() {
		ch := args.Child(i)
		if kindOf(kinds, ch) == "object" {
			columns = ch
			break
		}
	}
	if columns == nil {
		return nil
	}
	var out []string
	for i := range columns.ChildCount() {
		pair := columns.Child(i)
		if kindOf(kinds, pair) != "pair" {
			continue
		}
		key := pair.ChildByFieldName("key")
		val := pair.ChildByFieldName("value")
		if key == nil || val == nil {
			continue
		}
		col := strings.Trim(nodeText(key, src), `"'`)
		tableIdent, tcol, ok := drizzleReferencesTarget(kinds, val, src)
		if !ok {
			continue
		}
		out = append(out, col+"->"+tableIdent+"."+tcol)
	}
	return out
}

func drizzleReferencesTarget(kinds *tsutil.KindTable, expr *sitter.Node, src []byte) (table, column string, ok bool) {
	n := expr
	for n != nil && kindOf(kinds, n) == "call_expression" {
		fn := n.ChildByFieldName("function")
		if fn != nil && kindOf(kinds, fn) == "member_expression" {
			if prop := fn.ChildByFieldName("property"); prop != nil && nodeText(prop, src) == "references" {
				return drizzleReferencesArrowTarget(kinds, n.ChildByFieldName("arguments"), src)
			}
			n = fn.ChildByFieldName("object")
			continue
		}
		break
	}
	return "", "", false
}

func drizzleReferencesArrowTarget(kinds *tsutil.KindTable, args *sitter.Node, src []byte) (table, column string, ok bool) {
	if args == nil {
		return "", "", false
	}
	var fn *sitter.Node
	for i := range args.ChildCount() {
		ch := args.Child(i)
		k := kindOf(kinds, ch)
		if k == "arrow_function" || k == "function_expression" {
			fn = ch
			break
		}
	}
	if fn == nil {
		return "", "", false
	}
	body := fn.ChildByFieldName("body")
	if body == nil {
		return "", "", false
	}
	if kindOf(kinds, body) != "member_expression" {
		return "", "", false
	}
	obj, prop := body.ChildByFieldName("object"), body.ChildByFieldName("property")
	if obj == nil || prop == nil || kindOf(kinds, obj) != "identifier" {
		return "", "", false
	}
	return nodeText(obj, src), nodeText(prop, src), true
}

// firstStringArg returns the text of the first string literal in an argument list.
func firstStringArg(kinds *tsutil.KindTable, args *sitter.Node, src []byte) string {
	if args == nil {
		return ""
	}
	for i := range args.ChildCount() {
		a := args.Child(i)
		if kindOf(kinds, a) != "string" {
			continue
		}
		return strings.Trim(nodeText(a, src), `"'`+"`")
	}
	return ""
}

// prismaModel matches a `model User {` block header in a Prisma schema.
var prismaModel = regexp.MustCompile(`(?m)^\s*model\s+(\w+)\s*\{`)

// prismaSchemaFiles are the conventional locations of a Prisma schema, relative to the
// repo (or TS) root.
var prismaSchemaFiles = []string{
	filepath.Join("prisma", "schema.prisma"),
	"schema.prisma",
}

// extractPrismaStorage reads schema.prisma OFF-GLOB and emits one storage fact per model.
//
// schema.prisma is a separate DSL, not TypeScript, so tree-sitter never sees it. That is
// not an obstacle: the extractor ALREADY reads non-TS files from disk this way —
// package.json for framework detection and tsconfig.json for path aliases. This is the
// same mechanism, plus a block-header match. (`datasource`/`generator` blocks are not
// models and are ignored by construction.)
func extractPrismaStorage(ctx context.Context, repoPath string, inputScopes ...*inputscope.Scope) (factsOut []facts.Fact, unread []string) {
	inputScope := inputscope.First(inputScopes)
	tsRoot, _ := findTSRoot(ctx, repoPath, inputScope)
	roots := []string{tsRoot}
	if tsRoot != repoPath {
		roots = append(roots, repoPath)
	}

	var out []facts.Fact
	seen := map[string]bool{}
	for _, root := range roots {
		for _, rel := range prismaSchemaFiles {
			abs := filepath.Join(root, rel)
			st, err := inputScope.Stat(abs)
			if err != nil {
				continue
			}
			if st.IsDir() {
				continue
			}
			data, err := overlayReadFile(ctx, abs, inputScope)
			if err != nil {
				unread = append(unread, rel)
				continue
			}
			rawRel, err := filepath.Rel(repoPath, abs)
			if err != nil {
				continue
			}
			relFile := factpath.Slash(rawRel)
			dir := factpath.Dir(relFile)
			src := string(data)
			for _, m := range prismaModel.FindAllStringSubmatchIndex(src, -1) {
				model := src[m[2]:m[3]]
				name := dir + "." + model
				if seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, facts.Fact{
					Kind: facts.KindStorage,
					Name: name,
					File: relFile,
					Line: strings.Count(src[:m[0]], "\n") + 1,
					Props: map[string]any{
						"storage_kind": "entity",
						"language":     "typescript",
						"framework":    "prisma",
						"table":        model,
					},
					Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: dir}},
				})
			}
		}
	}
	return out, unread
}

// ormFlags carries the per-repo ORM detection results into per-file extraction. The
// extractor runs files in parallel, so these are read-only once computed — as the
// existing isNextJS/isVue/isNuxt flags are.
type ormFlags struct {
	typeORM bool
	drizzle bool
}
