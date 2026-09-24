package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/extractors/pythonextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphstream"
)

// directoryModuleSiblings exists because every extractor that emits a
// directory-shaped module names it after the directory, so a sibling in any
// language can move the module a markdown page declares. It must therefore seed
// on a real sibling of any language, and must not seed on a file no extractor
// claims - such a file emits no facts at all, and the prior contribution map
// that "previously an owner" is read from never held it, so it reads as new on
// every delta and drags every markdown page in its directory into the frozen
// manifest, where the markdown extractor does not rerun to change any of them.

func siblingScopeState() map[string]*FileState {
	return map[string]*FileState{
		"docs/plans/roadmap.md": {Hash: "1", Extractor: "mdintent"},
		"docs/plans/intake.md":  {Hash: "2", Extractor: "mdintent"},
		"src/app/index.ts": {
			Hash: "3", Extractor: "typescript",
			TS: &tsextractor.FileRecord{File: "src/app/index.ts"},
		},
	}
}

func siblingScopePrevious() []string {
	return []string{"docs/plans/roadmap.md", "docs/plans/intake.md", "src/app/index.ts"}
}

// claimOf builds the claimed set the session passes in: every file an active
// extractor owns. Files absent from it are the never-contributing inputs.
func claimOf(files ...string) map[string]bool {
	claimed := map[string]bool{}
	for _, f := range files {
		claimed[f] = true
	}
	return claimed
}

func siblingScopeClaimed(extra ...string) map[string]bool {
	base := []string{"docs/plans/roadmap.md", "docs/plans/intake.md", "src/app/index.ts"}
	return claimOf(append(base, extra...)...)
}

func TestDirectoryModuleSiblingsIgnoresNeverContributingFile(t *testing.T) {
	previous := siblingScopePrevious()
	current := append(append([]string{}, previous...), "docs/plans/expected.json")
	got := directoryModuleSiblings(previous, current, siblingScopeState(), siblingScopeClaimed(), true)
	if len(got) != 0 {
		t.Fatalf("a file no extractor claims seeded markdown owners: %v", got)
	}
}

func TestDirectoryModuleSiblingsIgnoresNeverContributingFileBesideRealAddition(t *testing.T) {
	previous := siblingScopePrevious()
	current := append(append([]string{}, previous...),
		"docs/plans/expected.json", "docs/plans/snapshot.json", "src/app/added.ts")
	got := directoryModuleSiblings(previous, current, siblingScopeState(),
		siblingScopeClaimed("src/app/added.ts"), true)
	if len(got) != 0 {
		t.Fatalf("unclaimed files in a markdown directory seeded owners alongside a real addition elsewhere: %v", got)
	}
}

func TestDirectoryModuleSiblingsIgnoresUnchangedTypeScriptSource(t *testing.T) {
	previous := siblingScopePrevious()
	got := directoryModuleSiblings(previous, append([]string{}, previous...), siblingScopeState(),
		siblingScopeClaimed(), true)
	if len(got) != 0 {
		t.Fatalf("an unchanged file set seeded markdown owners: %v", got)
	}
}

func TestDirectoryModuleSiblingsSeedsProvenTypeScriptAddition(t *testing.T) {
	previous := siblingScopePrevious()
	current := append(append([]string{}, previous...), "docs/plans/helper.ts")
	got := directoryModuleSiblings(previous, current, siblingScopeState(),
		siblingScopeClaimed("docs/plans/helper.ts"), true)
	want := []string{"docs/plans/intake.md", "docs/plans/roadmap.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a TypeScript sibling must reseed the directory pages: got %v want %v", got, want)
	}
}

// The case the TypeScript-only form of this filter got wrong: pythonextractor,
// hclextractor and swiftextractor all emit KindModule{Name: dir}, exactly as
// mdintent does, so a sibling in those languages moves the same module.
func TestDirectoryModuleSiblingsSeedsNonTypeScriptModuleAddition(t *testing.T) {
	for _, sibling := range []string{"docs/plans/tool.py", "docs/plans/main.swift", "docs/plans/main.tf"} {
		previous := siblingScopePrevious()
		current := append(append([]string{}, previous...), sibling)
		got := directoryModuleSiblings(previous, current, siblingScopeState(),
			siblingScopeClaimed(sibling), true)
		want := []string{"docs/plans/intake.md", "docs/plans/roadmap.md"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("sibling %s must reseed the directory pages: got %v want %v", sibling, got, want)
		}
	}
}

func TestDirectoryModuleSiblingsSeedsNonTypeScriptModuleRemoval(t *testing.T) {
	previous := append(siblingScopePrevious(), "docs/plans/tool.py")
	state := siblingScopeState()
	state["docs/plans/tool.py"] = &FileState{Hash: "4", Extractor: "python"}
	current := siblingScopePrevious()
	got := directoryModuleSiblings(previous, current, state, siblingScopeClaimed(), true)
	want := []string{"docs/plans/intake.md", "docs/plans/roadmap.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a removed python sibling must reseed the directory pages: got %v want %v", got, want)
	}
}

func TestDirectoryModuleSiblingsSeedsNonTypeScriptModuleRename(t *testing.T) {
	previous := append(siblingScopePrevious(), "docs/plans/old_tool.py")
	state := siblingScopeState()
	state["docs/plans/old_tool.py"] = &FileState{Hash: "4", Extractor: "python"}
	current := append(siblingScopePrevious(), "docs/plans/new_tool.py")
	got := directoryModuleSiblings(previous, current, state,
		siblingScopeClaimed("docs/plans/new_tool.py"), true)
	want := []string{"docs/plans/intake.md", "docs/plans/roadmap.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a renamed python sibling must reseed the directory pages: got %v want %v", got, want)
	}
}

// An extractor that cannot name the files it owns leaves the claimed set
// unbounded. Nothing may be narrowed by it, so even an unclaimed addition marks.
func TestDirectoryModuleSiblingsUnboundedClaimSetDoesNotNarrow(t *testing.T) {
	previous := siblingScopePrevious()
	current := append(append([]string{}, previous...), "docs/plans/expected.json")
	got := directoryModuleSiblings(previous, current, siblingScopeState(), siblingScopeClaimed(), false)
	want := []string{"docs/plans/intake.md", "docs/plans/roadmap.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("an unbounded claim set must not narrow: got %v want %v", got, want)
	}
}

func TestDirectoryModuleSiblingsSeedsRetirement(t *testing.T) {
	previous := append(siblingScopePrevious(), "docs/plans/retired.md")
	state := siblingScopeState()
	state["docs/plans/retired.md"] = &FileState{Hash: "4", Extractor: "mdintent"}
	got := directoryModuleSiblings(previous, siblingScopePrevious(), state, siblingScopeClaimed(), true)
	found := false
	for _, f := range got {
		if f == "docs/plans/roadmap.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a retired page must reseed its surviving siblings: %v", got)
	}
}

func TestDirectoryModuleSiblingsLeavesUnrelatedDirectory(t *testing.T) {
	previous := siblingScopePrevious()
	current := append(append([]string{}, previous...), "src/app/added.ts")
	got := directoryModuleSiblings(previous, current, siblingScopeState(),
		siblingScopeClaimed("src/app/added.ts"), true)
	if len(got) != 0 {
		t.Fatalf("an addition in a directory holding no markdown seeded owners: %v", got)
	}
}

// siblingScopeEngine detects TypeScript, markdown and Python, so a .py sibling
// is a real claimed module candidate rather than a synthetic one.
func siblingScopeEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	cfg := config.Default()
	cfg.Repo = dir
	cfg.Output.Dir = ".enola"
	cfg.Extractors = []string{"typescript", "mdintent", "python"}
	eng, err := engine.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterExtractor(tsextractor.New())
	eng.RegisterExtractor(mdintent.New())
	eng.RegisterExtractor(pythonextractor.New())
	return eng
}

func siblingScopeRepo(t *testing.T) string {
	t.Helper()
	return setupTSRepo(t, map[string]string{
		"docs/plans/roadmap.md": "# Roadmap\n\nSee [intake](intake.md).\n",
		"docs/plans/intake.md":  "# Intake\n\nSee [roadmap](roadmap.md).\n",
		"docs/plans/notes.json": "{\"note\":1}\n",
		"src/app/index.ts":      "export const app = 1;\n",
		"tools/build.py":        "VALUE = 1\n",
		"pyproject.toml":        "[project]\nname = \"tools\"\n",
	})
}

// The 617-owner defect at integration level. docs/plans holds a JSON file no
// extractor claims, so it never entered the prior contribution map; adding one
// TypeScript file somewhere else must not make that JSON read as "new" and drag
// every markdown page beside it into Begin, where mdintent does not rerun and
// would only resend them unchanged.
func TestDirectoryModuleSiblingsColdUnclaimedFileDoesNotSeedMarkdownOnUnrelatedAddition(t *testing.T) {
	root := siblingScopeRepo(t)
	eng := siblingScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "src/app/added.ts"), []byte("export const added = 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	requireNoWholeDomainFallback(t, delta, ids)
	requireOwners(t, owners, ids, "src/app/added.ts")
	forbidOwners(t, owners, ids, "docs/plans/roadmap.md", "docs/plans/intake.md")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A Python sibling is a real module candidate for the same directory module the
// markdown pages declare, so it must seed them - and the result must equal cold.
func TestDirectoryModuleSiblingsColdPythonSiblingSeedsMarkdown(t *testing.T) {
	root := siblingScopeRepo(t)
	eng := siblingScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "docs/plans/helper.py"), []byte("HELPER = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/plans/roadmap.md", "docs/plans/intake.md")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// Removing the Python sibling again must reseed the surviving pages and equal cold.
func TestDirectoryModuleSiblingsColdPythonSiblingRemovalSeedsMarkdown(t *testing.T) {
	root := siblingScopeRepo(t)
	if err := os.WriteFile(filepath.Join(root, "docs/plans/helper.py"), []byte("HELPER = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := siblingScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "docs/plans/helper.py")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/plans/roadmap.md", "docs/plans/intake.md")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A TypeScript template is claimed by OwnsFile so that editing it moves the
// extractor's cache key, but it can carry no module: the module domain is the
// parsed sources alone. extractorClaimedFiles must ask the narrower question,
// or a page sitting beside a template is seeded on every unrelated delta.
func TestExtractorClaimedFilesExcludesNonModuleCandidate(t *testing.T) {
	root := siblingScopeRepo(t)
	eng := siblingScopeEngine(t, root)
	files := []string{
		"src/app/index.ts",
		"docs/plans/roadmap.md",
		"docs/plans/page.html",
		"docs/plans/notes.json",
		"tools/build.py",
	}
	detected := map[string]bool{"typescript": true, "mdintent": true, "python": true}
	claimed, bounded := extractorClaimedFiles(eng, detected, files)
	if !bounded {
		t.Fatal("every registered extractor declares an owner domain, so the claim set is bounded")
	}
	for _, want := range []string{"src/app/index.ts", "docs/plans/roadmap.md", "tools/build.py"} {
		if !claimed[want] {
			t.Errorf("%s is a module candidate and must be claimed", want)
		}
	}
	for _, unwanted := range []string{"docs/plans/page.html", "docs/plans/notes.json"} {
		if claimed[unwanted] {
			t.Errorf("%s carries no module and must not be claimed", unwanted)
		}
	}
}

// End to end: a template that predates the state, beside markdown, must not be
// read as an addition when an unrelated TypeScript file is added elsewhere.
func TestDirectoryModuleSiblingsColdTemplateDoesNotSeedMarkdownOnUnrelatedAddition(t *testing.T) {
	root := siblingScopeRepo(t)
	if err := os.WriteFile(filepath.Join(root, "docs/plans/page.html"), []byte("<h1>page</h1>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng := siblingScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}

	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(initial.CloneRecords()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "src/app/added.ts"), []byte("export const added = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	requireNoWholeDomainFallback(t, delta, ids)
	requireOwners(t, owners, ids, "src/app/added.ts")
	forbidOwners(t, owners, ids, "docs/plans/roadmap.md", "docs/plans/intake.md")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
