package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// aliasRootSummary renders the alias roots a snapshot holds, so two snapshots
// can be compared on what they actually decided rather than on a struct that
// carries a map and cannot be compared directly.
func aliasRootSummary(d *Discovery) string {
	parts := make([]string, 0, len(d.aliasRoots))
	for _, r := range d.aliasRoots {
		names := make([]string, 0, len(r.aliases))
		for name := range r.aliases {
			names = append(names, name)
		}
		sort.Strings(names)
		parts = append(parts, r.dir+":"+strings.Join(names, ","))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func retentionRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		writeRetentionFile(t, root, rel, body)
	}
	return root
}

func writeRetentionFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A retained snapshot is offered to a later run, and the capture that run
// carries proves only the reads that capture contains. Almost nothing a
// discovery pass reads is in there: package.json, tsconfig.json, an external
// extends target and every framework marker are read from the live tree, so a
// fence built only out of captured bytes agrees with a snapshot that is already
// wrong.
//
// Each case below changes exactly one uncaptured input and asserts two things:
// that a snapshot built afresh answers differently, so the case is real, and
// that the retained one is refused. The first assertion is what stops this from
// passing against a discovery that stopped reading the input at all.
func TestRetainedDiscoveryRefusesUncapturedObservationChange(t *testing.T) {
	base := map[string]string{
		"package.json":       `{"name":"before","dependencies":{"vue":"^3"}}`,
		"tsconfig.json":      `{"extends":"./tsconfig.base.json","compilerOptions":{"baseUrl":"."}}`,
		"tsconfig.base.json": `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`,
		"src/a.ts":           "export const a = 1\n",
		// An ordinary file whose name a directory could later take.
		"packages": "placeholder\n",
	}
	cases := []struct {
		name string
		what string
		edit func(t *testing.T, root string)
		// same reports whether two snapshots answer alike, and is what makes
		// each case prove it changed something a discovery actually reads.
		same func(a, b *Discovery) bool
	}{
		{
			name: "live package bytes change",
			what: "bytes",
			edit: func(t *testing.T, root string) {
				writeRetentionFile(t, root, "package.json", `{"name":"after"}`)
			},
			same: func(a, b *Discovery) bool {
				return a.pkgNames["."] == b.pkgNames["."] && a.vue == b.vue
			},
		},
		{
			name: "external extends target changes",
			what: "bytes",
			edit: func(t *testing.T, root string) {
				writeRetentionFile(t, root, "tsconfig.base.json",
					`{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["vendor/*"]}}}`)
			},
			same: func(a, b *Discovery) bool {
				return aliasRootSummary(a) == aliasRootSummary(b)
			},
		},
		{
			name: "package appears in a directory the snapshot walked",
			what: "membership",
			edit: func(t *testing.T, root string) {
				writeRetentionFile(t, root, "src/nested/package.json", `{"name":"nested"}`)
			},
			same: func(a, b *Discovery) bool {
				return len(a.pkgNames) == len(b.pkgNames)
			},
		},
		{
			name: "entry the snapshot listed as a file becomes a directory",
			what: "membership",
			edit: func(t *testing.T, root string) {
				// The parent listing keeps the same names, and the snapshot has
				// no enumeration of this path because it was never a directory
				// to enumerate. Only the kind it was listed as catches this.
				if err := os.Remove(filepath.Join(root, "packages")); err != nil {
					t.Fatal(err)
				}
				writeRetentionFile(t, root, "packages/new/package.json", `{"name":"@fixture/new"}`)
			},
			same: func(a, b *Discovery) bool {
				return len(a.pkgNames) == len(b.pkgNames)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := retentionRepo(t, base)
			e := New()
			before := e.NewDiscovery(context.Background(), root, nil)
			tc.edit(t, root)
			cold := e.NewDiscovery(context.Background(), root, nil)
			if tc.same(before, cold) {
				t.Fatalf("the edit changed nothing a fresh discovery observes, so this case proves nothing")
			}
			reused, cost := e.ReuseDiscovery(root, nil, before)
			if reused != nil {
				t.Fatalf("a snapshot that decided on the old %s was retained anyway (cost %s)", tc.what, cost)
			}
			t.Logf("%s: refused after %s", tc.name, cost)
		})
	}
}

// The other direction, and the one that makes the guard above a fence rather
// than a ban: an unchanged tree must still prove, or retention is dead code
// that passes every refusal test.
func TestRetainedDiscoveryProvesUnchangedTree(t *testing.T) {
	root := retentionRepo(t, map[string]string{
		"package.json":       `{"name":"before","dependencies":{"vue":"^3"}}`,
		"tsconfig.json":      `{"extends":"./tsconfig.base.json","compilerOptions":{"baseUrl":"."}}`,
		"tsconfig.base.json": `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`,
		"src/a.ts":           "export const a = 1\n",
	})
	e := New()
	before := e.NewDiscovery(context.Background(), root, nil)

	// A content edit to a source file, carried as this caller's capture: the
	// bytes a run actually changes are the bytes a capture actually holds, and
	// none of them are a discovery input.
	sources := map[string][]byte{filepath.Join(root, "src", "a.ts"): []byte("export const a = 2\n")}
	reused, cost := e.ReuseDiscovery(root, sources, before)
	if reused != before {
		t.Fatalf("an unchanged tree refused its own snapshot (cost %s)", cost)
	}
	if cost.Names == 0 || cost.Bytes == 0 || cost.Dirs == 0 {
		t.Fatalf("the snapshot was proven without re-observing names, bytes and directories: %s", cost)
	}
	t.Logf("proof cost on an unchanged tree: %s", cost)
}
