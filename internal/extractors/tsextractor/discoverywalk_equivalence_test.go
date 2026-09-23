package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// discoveryWalkFixture builds a tree with every shape the shared prune rule has
// an opinion about: nested workspace packages, a dot-directory, a testdata
// directory and node_modules directories that each hold a package.json the
// collectors must NOT see, a nested Nuxt app under a root that also declares
// Nuxt, and a package.json that is not valid JSON.
func discoveryWalkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("package.json", `{"name":"root-pkg","dependencies":{"nuxt":"^3","vue":"^3"}}`)
	write("packages/api/package.json", `{"name":"@acme/api","main":"dist/index.js","dependencies":{"typeorm":"^0.3"}}`)
	write("packages/ui/package.json", `{"name":"@acme/ui","types":"dist/index.d.ts","dependencies":{"vue":"^3"}}`)
	write("apps/landings/package.json", `{"name":"@acme/landings","dependencies":{"nuxt":"^3"}}`)
	write("apps/landings/nuxt.config.ts", "export default {}\n")
	write("packages/broken/package.json", `{ this is not json `)

	// The same specifier declared twice, so that ordering is observable: a
	// reordered enumeration changes which declaration downstream resolution
	// keeps.
	write("packages/dup-a/package.json", `{"name":"@acme/dup","main":"./a.js","exports":{".":"./a.js"}}`)
	write("packages/dup-z/package.json", `{"name":"@acme/dup","main":"./z.js","exports":{".":"./z.js"}}`)

	// None of these may be enumerated: the shared prune rule excludes them, and
	// a merged walk that widened the rule would surface them here.
	write("node_modules/evil/package.json", `{"name":"evil-dep"}`)
	write(".hidden/package.json", `{"name":"hidden-pkg"}`)
	write("testdata/fixture/package.json", `{"name":"testdata-pkg"}`)
	write("packages/api/node_modules/nested/package.json", `{"name":"nested-dep"}`)

	return root
}

// Both sides of this comparison run the CURRENT collectors, so it proves that
// opting in to the shared cache changes nothing - not that the shared walk
// reproduces the pre-patch implementation. Equivalence against the pre-patch
// sources is established separately by the independent old-versus-new oracle
// (TestIndependentDiscoveryOracle, run against pristine 8c30274 and against
// these exact files), which is the only thing that can speak for code this tree
// no longer contains.
//
// What this test does own: the cached path and the uncached path must agree on
// every collector output, including ordering, over a tree that exercises each
// shape the prune rule has an opinion about.
func TestSharedDiscoveryWalkMatchesPerCollectorWalks(t *testing.T) {
	root := discoveryWalkFixture(t)
	separate := context.Background()
	shared := withDiscoveryWalkCache(context.Background())

	wantNuxt := collectNuxtPackages(separate, root)
	gotNuxt := collectNuxtPackages(shared, root)
	if !reflect.DeepEqual(wantNuxt, gotNuxt) {
		t.Errorf("nuxt packages differ:\n separate=%#v\n shared  =%#v", wantNuxt, gotNuxt)
	}
	if len(wantNuxt) != 2 {
		t.Fatalf("fixture did not exercise nested Nuxt detection: %#v", wantNuxt)
	}

	wantGates := collectPackageGates(separate, root)
	gotGates := collectPackageGates(shared, root)
	if !reflect.DeepEqual(wantGates, gotGates) {
		t.Errorf("package gates differ:\n separate=%#v\n shared  =%#v", wantGates, gotGates)
	}

	wantNames := collectPackageNames(separate, root)
	gotNames := collectPackageNames(shared, root)
	if !reflect.DeepEqual(wantNames, gotNames) {
		t.Errorf("package names differ:\n separate=%#v\n shared  =%#v", wantNames, gotNames)
	}
	if len(wantNames) == 0 {
		t.Fatal("fixture yielded no package names")
	}
	for _, name := range wantNames {
		switch name {
		case "evil-dep", "hidden-pkg", "testdata-pkg", "nested-dep":
			t.Errorf("pruned directory leaked into package names: %q", name)
		}
	}

	// Export sources are compared as an ordered slice on purpose. The fixture
	// declares the same package name twice, under packages/a and packages/z, so
	// walk order decides which declaration wins downstream; a shared enumeration
	// that reordered entries would silently change resolution here.
	wantExports := collectPackageExportSources(separate, root)
	gotExports := collectPackageExportSources(shared, root)
	if !reflect.DeepEqual(wantExports, gotExports) {
		t.Errorf("export sources differ:\n separate=%#v\n shared  =%#v", wantExports, gotExports)
	}
	dup := 0
	for _, e := range wantExports {
		if e.name == "@acme/dup" {
			dup++
		}
	}
	if dup != 2 {
		t.Fatalf("fixture did not exercise duplicate specifier ordering: %d declarations of @acme/dup", dup)
	}
}

// The probe is the discovery fence's evidence, so the shared enumeration must
// leave the same record behind as the four separate walks did - the same
// directory enumerations and the same reads, including the negative ones.
// recordDir already keeps only the first enumeration of a directory, which is
// why one walk can stand in for four: walks two through four never added
// anything the first had not recorded.
func TestSharedDiscoveryWalkRecordsTheSameDirectories(t *testing.T) {
	root := discoveryWalkFixture(t)

	record := func(shared bool) (map[string]map[string]string, map[string]string, map[string]string) {
		probe := newOverlayProbe()
		ctx := withOverlayProbe(withFileOverlay(context.Background(), newFileOverlay(root, nil)), probe)
		if shared {
			ctx = withDiscoveryWalkCache(ctx)
		}
		collectNuxtPackages(ctx, root)
		collectPackageGates(ctx, root)
		collectPackageNames(ctx, root)
		collectPackageExportSources(ctx, root)
		return probe.dirSnapshot(), probe.snapshot(), probe.statSnapshot()
	}

	wantDirs, wantReads, wantStats := record(false)
	gotDirs, gotReads, gotStats := record(true)

	if !reflect.DeepEqual(wantDirs, gotDirs) {
		t.Errorf("recorded directory enumerations differ:\n separate=%#v\n shared  =%#v", wantDirs, gotDirs)
	}
	if !reflect.DeepEqual(wantReads, gotReads) {
		t.Errorf("recorded reads differ:\n separate=%#v\n shared  =%#v", wantReads, gotReads)
	}
	if !reflect.DeepEqual(wantStats, gotStats) {
		t.Errorf("recorded stats differ:\n separate=%#v\n shared  =%#v", wantStats, gotStats)
	}
	if len(wantDirs) == 0 {
		t.Fatal("fixture recorded no directory enumerations")
	}
}

// A context carrying no cache must keep walking, so that a caller outside a
// discovery build does not change behaviour by failing to opt in.
func TestSharedDiscoveryWalkWithoutCacheStillEnumerates(t *testing.T) {
	root := discoveryWalkFixture(t)

	entries := sharedDiscoveryEntries(context.Background(), root, nil)
	if len(entries) == 0 {
		t.Fatal("uncached enumeration returned nothing")
	}
	again := sharedDiscoveryEntries(context.Background(), root, nil)
	if !reflect.DeepEqual(entries, again) {
		t.Error("two uncached enumerations of the same tree disagreed")
	}

	for _, e := range entries {
		rel, err := filepath.Rel(root, e.path)
		if err != nil {
			t.Fatal(err)
		}
		switch strings.SplitN(filepath.ToSlash(rel), "/", 2)[0] {
		case "node_modules", ".hidden", "testdata":
			t.Errorf("pruned subtree was enumerated: %q", rel)
		}
	}
}

// A shared enumeration is only safe if what it records still invalidates a
// retained snapshot. The membership half of reobserve is the part that depends
// on walkedDirs, which is exactly what the shared walk now produces, so each
// way a package can appear or move inside a walked directory is checked here.
func TestSharedDiscoveryWalkKeepsRetentionFence(t *testing.T) {
	e := New()

	build := func(root string) *Discovery {
		return e.newDiscovery(context.Background(), root, newFileOverlay(root, nil), 0)
	}

	t.Run("unchanged tree stays reusable", func(t *testing.T) {
		root := discoveryWalkFixture(t)
		if _, ok := build(root).reobserve(nil); !ok {
			t.Error("snapshot of an unchanged tree refused to reobserve")
		}
	})

	// Each of these is a discovery input the snapshot decided without, or
	// decided differently. None may survive reobserve.
	for _, tc := range []struct {
		name  string
		apply func(t *testing.T, root string)
	}{
		{"package added to a walked directory", func(t *testing.T, root string) {
			p := filepath.Join(root, "packages", "late", "package.json")
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(`{"name":"@acme/late"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"package deleted from a walked directory", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "packages", "ui", "package.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"package directory renamed", func(t *testing.T, root string) {
			if err := os.Rename(filepath.Join(root, "packages", "ui"), filepath.Join(root, "packages", "ui2")); err != nil {
				t.Fatal(err)
			}
		}},
		{"nuxt config added beside a package", func(t *testing.T, root string) {
			p := filepath.Join(root, "packages", "ui", "nuxt.config.ts")
			if err := os.WriteFile(p, []byte("export default {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := discoveryWalkFixture(t)
			d := build(root)
			if _, ok := d.reobserve(nil); !ok {
				t.Fatal("snapshot was already stale before the change")
			}
			tc.apply(t, root)
			if _, ok := d.reobserve(nil); ok {
				t.Error("retained snapshot survived a change it should have caught")
			}
		})
	}
}

// The prune rule skips dot-directories everywhere except the repository root
// itself, so a repository that lives in one must still be enumerated. The
// shared walk carries that exception in one place now rather than four, which
// makes losing it a single-line mistake worth pinning.
func TestSharedDiscoveryWalkEnumeratesHiddenRepositoryRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".repo")
	if err := os.MkdirAll(filepath.Join(root, "packages", "api"), 0o750); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"package.json":              `{"name":"hidden-root","dependencies":{"nuxt":"^3"}}`,
		"packages/api/package.json": `{"name":"@acme/api"}`,
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	names := collectPackageNames(withDiscoveryWalkCache(context.Background()), root)
	if len(names) != 2 {
		t.Fatalf("hidden repository root was pruned: %#v", names)
	}
}
