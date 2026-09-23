package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// A package.json dependency is not a repository-wide fact. Vue, TypeORM and
// Drizzle are selected for a file by the nearest package directory that owns it
// (packageGates.forFile, which extractFile is handed directly), so a manifest
// gaining drizzle-orm can only change the facts of the files that manifest owns.
// The session context used to hash the whole gate map into one repository-wide
// key, so every source in the repository reparsed for it.
//
// These cases judge the narrowing by what actually came out: the drizzle tables
// in the applied graph before and after each edit, checked against a cold
// session every time. A case that only counted parses would pass just as well
// against a fix that stopped invalidating files that needed it.

// gateDrizzleSource is a module whose facts exist only if its owning package
// declares drizzle-orm: pgTable is a plain call otherwise, and the storage
// extractor never looks at it.
func gateDrizzleSource(table, name string) string {
	return "import { pgTable, text } from \"drizzle-orm/pg-core\";\n" +
		"export const " + name + " = pgTable(\"" + table + "\", { id: text(\"id\") });\n"
}

// gateRepo is three sibling packages, one of them nesting a fourth, with a
// drizzle-shaped module in each and four root-owned sources from admissionRepo.
// Nothing declares an ORM to begin with, so each case turns exactly one gate on
// or off and every other gate in the tree is a control.
func gateRepo(t *testing.T, root string, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"package.json":                          root,
		"packages/database/package.json":        `{"name":"db","dependencies":{"helper":"1"}}`,
		"packages/database/schema.ts":           gateDrizzleSource("scan_runs", "scanRuns"),
		"packages/database/nested/package.json": `{"name":"nested","dependencies":{"helper":"1"}}`,
		"packages/database/nested/schema.ts":    gateDrizzleSource("nested_rows", "nestedRows"),
		"packages/api/package.json":             `{"name":"api","dependencies":{"helper":"1"}}`,
		"packages/api/schema.ts":                gateDrizzleSource("api_rows", "apiRows"),
	}
	for rel, body := range extra {
		if body == "" {
			delete(files, rel)
			continue
		}
		files[rel] = body
	}
	return admissionRepo(t, files)
}

// gateSources is every TypeScript file gateRepo admits, so a case that expects a
// whole-repository reparse can say so as a number rather than as "a lot".
var gateSources = []string{
	"packages/api/schema.ts",
	"packages/database/nested/schema.ts",
	"packages/database/schema.ts",
	"src/a.ts",
	"src/barrel.ts",
	"src/solo.ts",
	"src/use.ts",
}

// gateFramework is the point of the whole change: which files' facts the applied
// graph currently attributes to a framework the owning package gates. Entries
// are "<file> <fact>".
func gateFramework(c *Consumer, framework string) []string {
	out := []string{}
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Props != nil && n.Props["framework"] == framework {
				out = append(out, n.File+" "+n.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func gateTables(c *Consumer) []string { return gateFramework(c, "drizzle") }

func requireTables(t *testing.T, c *Consumer, what string, want ...string) {
	t.Helper()
	requireFramework(t, c, "drizzle", what, want...)
}

func requireFramework(t *testing.T, c *Consumer, framework, what string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := gateFramework(c, framework); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: %s facts %v, want %v", what, framework, got, want)
	}
}

// gateStep is one edit judged the three ways that matter together: how much was
// reparsed, which owners the replacement froze at Begin, and whether the result
// still equals a session that started from nothing.
type gateStep struct {
	res    *Result
	owners map[string]bool
	ids    []string
}

func gateApply(t *testing.T, eng *engine.Engine, dir string, opts Options, cons *Consumer) gateStep {
	t.Helper()
	res, sink := admissionRun(t, eng, dir, opts, cons)
	owners, ids := beginScope(t, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
	return gateStep{res: res, owners: owners, ids: ids}
}

// requireParsed with the fallback check the narrow cases need as well: covering
// the edited file by freezing the whole name-resolution domain would satisfy
// every equality assertion here and say nothing about the gate projection.
func (s gateStep) requireLocal(t *testing.T, want int, what string) {
	t.Helper()
	s.requireParsed(t, want, what)
	requireNoWholeDomainFallback(t, s.res, s.ids)
}

func (s gateStep) requireParsed(t *testing.T, want int, what string) {
	t.Helper()
	if s.res.ParsedFiles != want {
		t.Fatalf("%s parsed %d files, want %d: scope=%v invalidation=%+v", what, s.res.ParsedFiles, want, s.ids, s.res.Invalidation)
	}
}

func gateStart(t *testing.T, dir string) (*engine.Engine, Options, *Consumer) {
	t.Helper()
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	gateApply(t, eng, dir, opts, cons)
	return eng, opts, cons
}

// The nearest owning package wins, and nothing above or beside it moves. Each
// step turns one manifest's drizzle declaration on and asserts that exactly that
// package's table appeared - in particular that enabling the parent package did
// not reach into the nested one, which has its own manifest and shadows it.
func TestPackageGateScopeFollowsNearestOwningPackage(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18"}}`, nil)
	eng, opts, cons := gateStart(t, dir)
	requireTables(t, cons, "no package declares an ORM")

	writeFile(t, dir, "packages/database/package.json", `{"name":"db","dependencies":{"helper":"1","drizzle-orm":"0.30.0"}}`)
	step := gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "enabling drizzle in packages/database")
	requireOwners(t, step.owners, step.ids, "packages/database/schema.ts")
	forbidOwners(t, step.owners, step.ids, "packages/database/nested/schema.ts", "packages/api/schema.ts", "src/a.ts", "src/barrel.ts", "src/solo.ts", "src/use.ts")
	requireTables(t, cons, "drizzle enabled in packages/database only",
		"packages/database/schema.ts packages/database.scanRuns")

	// The nested manifest shadows its parent, so the step above left it alone and
	// this one is what turns it on.
	writeFile(t, dir, "packages/database/nested/package.json", `{"name":"nested","dependencies":{"helper":"1","drizzle-orm":"0.30.0"}}`)
	step = gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "enabling drizzle in the nested package")
	requireOwners(t, step.owners, step.ids, "packages/database/nested/schema.ts")
	forbidOwners(t, step.owners, step.ids, "packages/database/schema.ts", "packages/api/schema.ts")
	requireTables(t, cons, "drizzle enabled in both database packages",
		"packages/database/nested/schema.ts packages/database/nested.nestedRows",
		"packages/database/schema.ts packages/database.scanRuns")

	// Turning the parent back off must retire the parent's table and leave the
	// nested one standing, which a whole-repository invalidation would also do -
	// so the parse count is what says it was not one.
	writeFile(t, dir, "packages/database/package.json", `{"name":"db","dependencies":{"helper":"1"}}`)
	step = gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "removing drizzle from packages/database")
	requireOwners(t, step.owners, step.ids, "packages/database/schema.ts")
	forbidOwners(t, step.owners, step.ids, "packages/database/nested/schema.ts", "packages/api/schema.ts")
	requireTables(t, cons, "drizzle removed from the parent package only",
		"packages/database/nested/schema.ts packages/database/nested.nestedRows")
}

// The root manifest is a package like any other: it owns the files no nearer
// manifest claims, and it owns only those. Before the split this was the second
// half of the same defect - the root declaration was also hashed into the
// repository-wide framework key, so it reparsed the packages that shadow it.
func TestPackageGateScopeRootManifestOwnsOnlyUnclaimedFiles(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18"}}`, nil)
	eng, opts, cons := gateStart(t, dir)

	writeFile(t, dir, "package.json", `{"name":"app","dependencies":{"react":"18","drizzle-orm":"0.30.0"}}`)
	step := gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 4, "enabling drizzle in the root manifest")
	requireOwners(t, step.owners, step.ids, "src/a.ts", "src/barrel.ts", "src/solo.ts", "src/use.ts")
	forbidOwners(t, step.owners, step.ids, "packages/api/schema.ts", "packages/database/schema.ts", "packages/database/nested/schema.ts")
	// None of the root-owned sources is drizzle-shaped, so the facts are unchanged
	// and the three shadowed packages keep answering for themselves.
	requireTables(t, cons, "root drizzle does not reach the nested packages")
}

// A manifest appearing takes ownership of the files below it away from the
// manifest above, and a manifest disappearing hands them back. Both are real
// gate transitions for exactly those files, and for no others.
func TestPackageGateScopeManifestAddAndDelete(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18","drizzle-orm":"0.30.0"}}`,
		map[string]string{"packages/api/package.json": ""})
	eng, opts, cons := gateStart(t, dir)
	requireTables(t, cons, "packages/api inherits the root drizzle declaration",
		"packages/api/schema.ts packages/api.apiRows")

	writeFile(t, dir, "packages/api/package.json", `{"name":"api","dependencies":{"helper":"1"}}`)
	step := gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "a manifest appearing over packages/api")
	requireOwners(t, step.owners, step.ids, "packages/api/schema.ts")
	forbidOwners(t, step.owners, step.ids, "src/a.ts", "src/barrel.ts", "src/solo.ts", "src/use.ts", "packages/database/schema.ts")
	requireTables(t, cons, "the new manifest shadows the root declaration")

	if err := os.Remove(filepath.Join(dir, "packages/api/package.json")); err != nil {
		t.Fatal(err)
	}
	step = gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "that manifest being deleted again")
	requireOwners(t, step.owners, step.ids, "packages/api/schema.ts")
	forbidOwners(t, step.owners, step.ids, "src/a.ts", "src/barrel.ts", "src/solo.ts", "src/use.ts", "packages/database/schema.ts")
	requireTables(t, cons, "ownership returns to the root manifest",
		"packages/api/schema.ts packages/api.apiRows")
}

// Prisma is the half of the package gates that really is repository-wide:
// anyPrisma decides whether the run reads schema.prisma at all, and no file owns
// that decision. It stays in the repository-wide key, and a case has to prove it
// still invalidates everything, or the narrowing above would have been a
// suppression that happened to pass the cases it was written for.
func TestPackageGateScopePrismaRemainsRepositoryWide(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18"}}`, nil)
	eng, opts, cons := gateStart(t, dir)

	writeFile(t, dir, "packages/database/package.json", `{"name":"db","dependencies":{"helper":"1","@prisma/client":"5.12.0"}}`)
	step := gateApply(t, eng, dir, opts, cons)
	step.requireParsed(t, len(gateSources), "one package declaring @prisma/client")
	requireOwners(t, step.owners, step.ids, gateSources...)
	var found bool
	for _, r := range step.res.Invalidation.ContextReasons {
		if r == "TS any package prisma gate changed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the repository-wide Prisma gate did not explain the reparse: %v", step.res.Invalidation.ContextReasons)
	}
}

// Malformed manifests are a deliberate conservative boundary rather than a gate
// reading, and the narrowing must not have turned one into the other: a package
// the gate reader can no longer parse invalidates the repository.
func TestPackageGateScopeMalformedManifestStaysConservative(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18"}}`, nil)
	eng, opts, cons := gateStart(t, dir)

	writeFile(t, dir, "packages/api/package.json", `{"name":"api",`)
	step := gateApply(t, eng, dir, opts, cons)
	step.requireParsed(t, len(gateSources), "a manifest becoming unparseable")
	requireOwners(t, step.owners, step.ids, gateSources...)
}

// A state written by the previous context version carries the old keys and
// per-file digests with no gate component in them. It must not be read as
// agreement: the run has to reparse conservatively, agree with cold, and leave a
// state behind that is fully in the new shape, or the next run would migrate
// again forever.
func TestPackageGateScopeContextVersionMigration(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18","drizzle-orm":"0.30.0"}}`, nil)
	eng, opts, cons := gateStart(t, dir)
	before := gateTables(cons)

	st, err := readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if st.TSContext["version"] != "ts-effective-context-v4" {
		t.Fatalf("the committed state is not at the current context version: %q", st.TSContext["version"])
	}
	aged := map[string]string{}
	for k, v := range st.TSContext {
		aged[k] = v
	}
	delete(aged, "repository-wide frameworks")
	delete(aged, "any package prisma gate")
	aged["version"] = "ts-effective-context-v3"
	aged["framework and ORM gates"] = "v3"
	aged["owning package gates"] = "v3"
	st.TSContext = aged
	st.TSFileContext = nil
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}

	step := gateApply(t, eng, dir, opts, cons)
	step.requireParsed(t, len(gateSources), "a state left by the previous context version")
	requireOwners(t, step.owners, step.ids, gateSources...)
	if got := gateTables(cons); !reflect.DeepEqual(got, before) {
		t.Fatalf("the migration changed the graph: %v, want %v", got, before)
	}

	st, err = readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if st.TSContext["version"] != "ts-effective-context-v4" {
		t.Fatalf("the migrated state is still at %q", st.TSContext["version"])
	}
	if _, ok := st.TSContext["framework and ORM gates"]; ok {
		t.Fatalf("the migrated state kept a retired key: %v", st.TSContext)
	}
	if len(st.TSFileContext) != len(gateSources) {
		t.Fatalf("the migrated state carries %d per-file contexts, want %d", len(st.TSFileContext), len(gateSources))
	}

	// And the run after the migration is an ordinary no-op again, not a second
	// migration.
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPublication(t, res, sink, step.res.TargetGeneration, "the run after a context migration")
}

// Drizzle is one of three declarations the owning package selects. Vue and
// TypeORM travel the same per-file projection, and each of them changes facts a
// file emits rather than only whether it is reparsed, so each gets its own
// package and its own before-and-after.
func TestPackageGateScopeVueAndTypeORMAreAlsoPerPackage(t *testing.T) {
	dir := gateRepo(t, `{"name":"app","dependencies":{"react":"18"}}`, map[string]string{
		"packages/ui/package.json":  `{"name":"ui","dependencies":{"helper":"1"}}`,
		"packages/ui/use.ts":        "export function useThing() { return null; }\n",
		"packages/orm/package.json": `{"name":"orm","dependencies":{"helper":"1"}}`,
		"packages/orm/entity.ts":    "@Entity(\"users\")\nexport class User { id: number; }\n",
	})
	eng, opts, cons := gateStart(t, dir)
	requireFramework(t, cons, "vue", "before any package declares vue")
	requireFramework(t, cons, "typeorm", "before any package declares typeorm")

	writeFile(t, dir, "packages/ui/package.json", `{"name":"ui","dependencies":{"helper":"1","vue":"3"}}`)
	step := gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "enabling vue in packages/ui")
	requireOwners(t, step.owners, step.ids, "packages/ui/use.ts")
	forbidOwners(t, step.owners, step.ids, "packages/orm/entity.ts", "packages/database/schema.ts", "src/a.ts")
	requireFramework(t, cons, "vue", "vue enabled in packages/ui only", "packages/ui/use.ts packages/ui.useThing")
	requireFramework(t, cons, "typeorm", "the vue edit did not reach packages/orm")

	writeFile(t, dir, "packages/orm/package.json", `{"name":"orm","dependencies":{"helper":"1","typeorm":"0.3.20"}}`)
	step = gateApply(t, eng, dir, opts, cons)
	step.requireLocal(t, 1, "enabling typeorm in packages/orm")
	requireOwners(t, step.owners, step.ids, "packages/orm/entity.ts")
	forbidOwners(t, step.owners, step.ids, "packages/ui/use.ts", "packages/database/schema.ts", "src/a.ts")
	requireFramework(t, cons, "typeorm", "typeorm enabled in packages/orm only", "packages/orm/entity.ts packages/orm.User")
	requireFramework(t, cons, "vue", "the typeorm edit did not retire the vue fact", "packages/ui/use.ts packages/ui.useThing")
}
