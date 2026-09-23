package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/extractors/manifestextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// These cases cover raw configuration byte changes: the frozen Begin must stay
// bounded when every active consumer's own projection proves the change did not
// move it, and must still cover the whole domain when one of them cannot.
//
// Every case is judged against a cold session, because a bounded Begin that
// skipped an owner still produces a graph, just the wrong one.

// configScopeEngine is the graph profile with the two consumers that read the
// configuration files the analysis fingerprint hashes: TypeScript projects them
// into its session context, manifests declares them as content inputs.
func configScopeEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	var attach func() *engine.Engine
	attach = func() *engine.Engine {
		eng := testEngine(t, dir)
		p, err := graphinput.Build(dir, graphinput.Options{})
		if err != nil {
			t.Fatal(err)
		}
		scope := &inputscope.Scope{Root: dir, Policy: p}
		eng.RegisterExtractor(manifestextractor.NewGraph(scope))
		eng.ConfigureGraphInputs(scope, func() (*engine.Engine, error) { return attach(), nil })
		return eng
	}
	return attach()
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// configScopeRun runs one session and returns its result, the Begin owner scope
// and the sorted owner IDs for failure messages.
func configScopeRun(t *testing.T, eng *engine.Engine, root string, opts Options, cons *Consumer) (*Result, map[string]bool, []string) {
	t.Helper()
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	owners, ids := beginScope(t, sink)
	return res, owners, ids
}

// wholeDomainFallback returns the reason of the whole-domain fallback a run
// recorded, or "" when it planned a bounded scope.
func wholeDomainFallback(res *Result) string {
	for _, fb := range res.Fallbacks {
		if fb.Scope == "all prior/current file owners" {
			return fb.Reason
		}
	}
	return ""
}

// contextFallback returns the reasons the TypeScript session context gave for
// re-extracting every owned file.
func contextFallback(res *Result) string {
	var out []string
	for _, fb := range res.Fallbacks {
		if fb.Extractor == "typescript" && fb.Scope == "all owned files" {
			out = append(out, fb.Reason)
		}
	}
	return strings.Join(out, " | ")
}

// manifestOnlyGraphEngine is the graph profile without the TypeScript
// extractor, so no session context is projected over the configuration files
// the fingerprint still hashes.
func manifestOnlyGraphEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	var attach func() *engine.Engine
	attach = func() *engine.Engine {
		cfg := config.Default()
		cfg.Repo = dir
		cfg.Output.Dir = ".enola"
		eng, err := engine.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		p, err := graphinput.Build(dir, graphinput.Options{})
		if err != nil {
			t.Fatal(err)
		}
		scope := &inputscope.Scope{Root: dir, Policy: p}
		eng.RegisterExtractor(manifestextractor.NewGraph(scope))
		eng.ConfigureGraphInputs(scope, func() (*engine.Engine, error) { return attach(), nil })
		return eng
	}
	return attach()
}

// tsOnlyGraphEngine is the same profile with the manifest consumer removed, so
// a state written by configScopeEngine names an extractor this run no longer
// has.
func tsOnlyGraphEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	var attach func() *engine.Engine
	attach = func() *engine.Engine {
		eng := testEngine(t, dir)
		p, err := graphinput.Build(dir, graphinput.Options{})
		if err != nil {
			t.Fatal(err)
		}
		eng.ConfigureGraphInputs(&inputscope.Scope{Root: dir, Policy: p}, func() (*engine.Engine, error) { return attach(), nil })
		return eng
	}
	return attach()
}

func configScopeRepo(t *testing.T) string {
	root := setupTSRepo(t, map[string]string{
		"src/leaf.ts":  "export const leaf = 1;\n",
		"src/use.ts":   "import { leaf } from './leaf';\nexport const used = leaf + 1;\n",
		"src/alone.ts": "export const alone = 3;\n",
	})
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.1"}`)
	return root
}

// A version bump is the case the blunt raw-config term punished: no source
// file, no package name, no framework gate and no alias root moves, so the
// manifest owner that actually republishes is the only owner Begin needs.
func TestRawConfigVersionBumpKeepsBeginBounded(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	if reason := wholeDomainFallback(res); reason != "" {
		t.Fatalf("version-only bump fell back to the whole domain (%s); scope was %v", reason, ids)
	}
	requireOwners(t, owners, ids, "package.json")
	forbidOwners(t, owners, ids, "src/leaf.ts", "src/use.ts", "src/alone.ts", "tsconfig.json")
	if res.ParsedFiles != 0 {
		t.Fatalf("parsed=%d, want no TypeScript reparse for a version bump", res.ParsedFiles)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// The combined case: one edited source and the same inert version bump. The
// edit is value-only - leaf keeps its name, its export and its imports - so
// the importer's own contribution cannot move and it stays out of Begin along
// with the untouched third source. The cold comparison below is the authority
// for that: it replays the whole repository and must match byte for byte.
func TestRawConfigVersionBumpWithSourceEditKeepsBeginBounded(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	writeRepoFile(t, root, "src/leaf.ts", "export const leaf = 2;\n")
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	if reason := wholeDomainFallback(res); reason != "" {
		t.Fatalf("combined source and version change fell back to the whole domain (%s); scope was %v", reason, ids)
	}
	requireOwners(t, owners, ids, "package.json", "src/leaf.ts")
	forbidOwners(t, owners, ids, "src/use.ts", "src/alone.ts")
	if res.ParsedFiles != 1 {
		t.Fatalf("parsed=%d, want only the edited leaf reparsed", res.ParsedFiles)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// The package name is a per-file projection: every source's nearest package
// moves even though no source byte did. Those files must be inside the frozen
// Begin, because the extraction site reparses on exactly this comparison and a
// plan that missed them would fail closed.
func TestRawConfigPackageRenameRebuildsAffectedSources(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"renamed","type":"module","version":"0.7.1"}`)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "package.json", "src/leaf.ts", "src/use.ts", "src/alone.ts")
	if res.ParsedFiles == 0 {
		t.Fatalf("package rename reparsed nothing; scope was %v", ids)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// Dependencies are a manifest contribution, not a TypeScript projection. The
// manifest owner republishes from its own declared content input, so the change
// is covered without announcing the sources.
func TestRawConfigDependencyChangeRepublishesManifestOwner(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.1","dependencies":{"left-pad":"^1.0.0"}}`)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "package.json")
	if reason := wholeDomainFallback(res); reason != "" {
		t.Fatalf("dependency change fell back to the whole domain (%s); scope was %v", reason, ids)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// A malformed package.json is a conservative boundary the TypeScript context
// states explicitly, so it must reach the whole domain rather than be planned
// from readers that no longer have anything to read.
func TestRawConfigMalformedPackageFallsBackToWholeDomain(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"app",`)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	if reason := contextFallback(res); !strings.Contains(reason, "package validity errors") {
		t.Fatalf("malformed package.json context reasons = %q; scope was %v", reason, ids)
	}
	requireOwners(t, owners, ids, "package.json", "src/leaf.ts", "src/use.ts", "src/alone.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// A configuration path the TypeScript context does not interpret is digested
// raw, so a change to it is reported as a context difference and keeps the
// conservative whole-domain scope. Nothing here is keyed on a file name: the
// raw digest is the default for every unprojected path.
func TestRawConfigUnprojectedInputFallsBackToWholeDomain(t *testing.T) {
	root := configScopeRepo(t)
	writeRepoFile(t, root, "nx.json", `{"npmScope":"app"}`)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "nx.json", `{"npmScope":"other"}`)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	if reason := contextFallback(res); !strings.Contains(reason, "unprojected config nx.json") {
		t.Fatalf("unprojected config context reasons = %q; scope was %v", reason, ids)
	}
	requireOwners(t, owners, ids, "src/leaf.ts", "src/use.ts", "src/alone.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// Without the TypeScript extractor nothing projects the configuration files the
// fingerprint hashes, so there is no proof of a smaller boundary and the change
// must keep the conservative whole-domain scope.
func TestRawConfigWithoutSessionContextFallsBackToWholeDomain(t *testing.T) {
	root := configScopeRepo(t)
	eng := manifestOnlyGraphEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	res, _, ids := configScopeRun(t, eng, root, opts, cons)

	reason := wholeDomainFallback(res)
	if !strings.Contains(reason, "configuration inputs changed") || !strings.Contains(reason, "no stored TypeScript session context") {
		t.Fatalf("fallback reason = %q; scope was %v", reason, ids)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, manifestOnlyGraphEngine(t, root), root))
}

// An extractor the stored state recorded and this run no longer has is not
// reached by the bounded seed, which walks the extractors this run detected.
// Its owners still have to be retired, so a configuration change alongside its
// removal must keep the whole domain - here the engine context settles it
// first, because dropping a keyed consumer moves that fingerprint too, and
// rawConfigScopeBounded declines for the same reason if it ever does not.
func TestRawConfigWithRemovedConsumerFallsBackToWholeDomain(t *testing.T) {
	root := configScopeRepo(t)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, configScopeEngine(t, root), root, opts, cons)

	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	res, owners, ids := configScopeRun(t, tsOnlyGraphEngine(t, root), root, opts, cons)

	if wholeDomainFallback(res) == "" {
		t.Fatalf("removing an audited consumer planned a bounded scope %v", ids)
	}
	requireOwners(t, owners, ids, "package.json", "src/leaf.ts", "src/use.ts", "src/alone.ts")
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "package.json"}).String()
	for _, n := range cons.Owners[key] {
		if strings.Contains(n.ID, "manifest") {
			t.Fatalf("removed consumer still owns %s", n.ID)
		}
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, tsOnlyGraphEngine(t, root), root))
}

// Alias roots are a per-file projection like the package name. Adding a path
// mapping must rebuild the sources whose specifiers now resolve differently.
func TestRawConfigAliasChangeRebuildsAffectedSources(t *testing.T) {
	root := configScopeRepo(t)
	writeRepoFile(t, root, "src/alias-target.ts", "export const target = 9;\n")
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "tsconfig.json", `{"compilerOptions":{"strict":true,"baseUrl":".","paths":{"@app/*":["src/*"]}}}`)
	configScopeRun(t, eng, root, opts, cons)

	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// Deleting a prior owner alongside an inert configuration change: the owner has
// to be retired by the same bounded plan, and the applied graph has to match a
// cold one that never saw it.
func TestRawConfigVersionBumpWithDeletedOwnerRetiresIt(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	if err := os.Remove(filepath.Join(root, "src/alone.ts")); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	_, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "src/alone.ts", "package.json")
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/alone.ts"}).String()
	if len(cons.Owners[key]) != 0 {
		t.Fatalf("deleted owner still has %d nodes", len(cons.Owners[key]))
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// Restoring the original bytes leaves the configuration fingerprint where it
// started, so the following run must be a true no-op: no Begin, no records.
func TestRawConfigRestoredBytesRemainNoOp(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.1"}`)
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	quiet := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 {
		t.Fatalf("unchanged run was not a no-op: parsed=%d records=%d", res.ParsedFiles, len(quiet.CloneRecords()))
	}
}

// The admission table is what lets a configuration change be planned from
// declared inputs at all. An extractor outside it is not assumed benign: the
// run refuses it, and the predicate the narrowing consults reports it as
// unaudited rather than defaulting to true.
func TestAuditedGraphConsumerGatesUnknownExtractors(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	if !engine.AuditedGraphConsumer(tsextractor.New()) {
		t.Fatal("typescript is not reported as an audited graph consumer")
	}
	if !engine.AuditedGraphConsumer(manifestextractor.New()) {
		t.Fatal("manifests is not reported as an audited graph consumer")
	}
	if engine.AuditedGraphConsumer(stubExtractor{}) {
		t.Fatal("an unaudited extractor was admitted")
	}
	eng.RegisterExtractor(stubExtractor{})
	if err := eng.ValidateGraphConsumers(map[string]bool{stubExtractor{}.Name(): true}); err == nil {
		t.Fatal("ValidateGraphConsumers admitted an unaudited extractor")
	}
}

// aliasEdgeTarget returns the node an owner's single resolved use edge points
// at, together with the name it resolved through, and fails if the owner
// resolved nothing.
func aliasEdgeTarget(t *testing.T, cons *Consumer, owner string) string {
	t.Helper()
	key := (graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: owner}).String()
	var out []string
	for _, e := range cons.Edges[key] {
		if e.Resolution == "resolved" && e.TargetID != "" {
			out = append(out, e.TargetName+"->"+e.TargetID)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%s has %d resolved edge(s) %v among %d edge(s)", owner, len(out), out, len(cons.Edges[key]))
	}
	return out[0]
}

// The earlier alias case changed a mapping no file imported through, so cold
// equality held for a trivial reason. This one has a real importer and two
// candidate modules: repointing the mapping moves the resolved edge from one to
// the other while the importing source's bytes never change. Begin has to carry
// that importer, and the applied edge has to end up where a cold session puts
// it, not merely somewhere consistent.
func TestRawConfigAliasImporterRetargetsBetweenModules(t *testing.T) {
	root := configScopeRepo(t)
	for _, dir := range []string{"src/one", "src/two"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeRepoFile(t, root, "src/one/target.ts", "export const target = 1;\n")
	writeRepoFile(t, root, "src/two/target.ts", "export const target = 2;\n")
	writeRepoFile(t, root, "src/consumer.ts", "import { target } from '@app/target';\nexport const consumed = target;\n")
	writeRepoFile(t, root, "tsconfig.json", `{"compilerOptions":{"strict":true,"baseUrl":".","paths":{"@app/*":["src/one/*"]}}}`)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	before := aliasEdgeTarget(t, cons, "src/consumer.ts")

	writeRepoFile(t, root, "tsconfig.json", `{"compilerOptions":{"strict":true,"baseUrl":".","paths":{"@app/*":["src/two/*"]}}}`)
	_, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "src/consumer.ts")
	after := aliasEdgeTarget(t, cons, "src/consumer.ts")
	if after == before {
		t.Fatalf("the alias importer still resolves to %s; the mapping change moved no edge", before)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// A configuration path the fingerprint hashes can be absent or be a directory,
// and those two states hash differently while being indistinguishable to every
// consumer: analysisFingerprintInputs writes its MISSING or DIR marker and
// skips the path instead of capturing it, so the session context, and therefore
// each extractor, sees the same absence either way.
//
// The narrowing gate accepts the transition on that basis - it records no
// configuration fallback of its own - and the frozen planner then falls back to
// the global name-resolution domain for its own reasons, so the transition is
// never narrowed on the strength of the fingerprint byte alone. What has to
// hold regardless is that the graph it produces is the one a cold session
// builds in the same state, in both directions across the boundary.
func TestRawConfigMissingAndDirectoryConfigAreIndistinguishable(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	// jsconfig.json is hashed as MISSING above; a directory makes it DIR.
	if err := os.MkdirAll(filepath.Join(root, "jsconfig.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, _, _ := configScopeRun(t, eng, root, opts, cons)
	for _, fb := range res.Fallbacks {
		if strings.HasPrefix(fb.Reason, "configuration inputs changed;") {
			t.Fatalf("the MISSING to DIR transition was refused by the configuration gate: %s", fb.Reason)
		}
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))

	// And back. This is a transition in its own right - the stored fingerprint
	// now holds the DIR value - so what it has to produce is convergence, not
	// silence.
	if err := os.RemoveAll(filepath.Join(root, "jsconfig.json")); err != nil {
		t.Fatal(err)
	}
	configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}
