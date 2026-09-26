package tsextractor

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Config inventory uses the visitor-bearing walker. Compare the complete ordered
// callbacks and probe ledgers against the frozen pre-optimization walk.
func TestAliasListingPreservesInventoryVisits(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		label := "all"
		if excluded {
			label = "excluded-package"
		}
		t.Run(label, func(t *testing.T) {
			root := discoveryWalkFixture(t)
			for name, body := range map[string]string{
				"tsconfig.json":                        `{"compilerOptions":{"paths":{"@root/*":["src/*"]}}}`,
				"packages/ui/tsconfig.json":            `{"compilerOptions":{"paths":{"@ui/*":["src/*"]}}}`,
				"packages/fallback/tsconfig.json":      `invalid`,
				"packages/fallback/tsconfig.base.json": `{"compilerOptions":{"paths":{"@fallback/*":["src/*"]}}}`,
			} {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			options := graphinput.Options{}
			if excluded {
				options.Exclude = []string{"packages/ui"}
			}
			policy, err := graphinput.Build(root, options)
			if err != nil {
				t.Fatal(err)
			}
			scope := &inputscope.Scope{Root: root, Policy: policy}
			if excluded && scope.Allowed(filepath.Join(root, "packages/ui"), true) {
				t.Fatal("fixture failed to exclude directory")
			}
			type visit struct {
				Dir     string
				Entries []string
			}
			collect := func(dst *[]visit) func(string, []fs.DirEntry) {
				return func(dir string, entries []fs.DirEntry) {
					v := visit{Dir: dir}
					for _, e := range entries {
						v.Entries = append(v.Entries, e.Name()+":"+entryKind(e))
					}
					*dst = append(*dst, v)
				}
			}
			before, after := newOverlayProbe(), newOverlayProbe()
			var wantRoots, gotRoots []tsAliasRoot
			var want, got []visit
			aliasListingReferenceWalk(withOverlayProbe(context.Background(), before), root, root, &wantRoots, collect(&want), scope)
			walkTSAliasRootsVisit(withOverlayProbe(context.Background(), after), root, root, &gotRoots, collect(&got), scope)
			if len(want) == 0 {
				t.Fatal("visitor oracle was not exercised")
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("inventory callback sequence differs:\n%v\n%v", want, got)
			}
			expectedDirs := map[string]bool{"": true, "packages/fallback": true}
			if !excluded {
				expectedDirs["packages/ui"] = true
			}
			if len(wantRoots) != len(expectedDirs) {
				t.Fatalf("expected %d real alias roots, got %v", len(expectedDirs), wantRoots)
			}
			for _, r := range wantRoots {
				if !expectedDirs[r.dir] || len(r.aliases) == 0 {
					t.Fatalf("unexpected or empty alias root: %v", r)
				}
				delete(expectedDirs, r.dir)
			}
			if len(expectedDirs) != 0 {
				t.Fatalf("missing roots: %v", expectedDirs)
			}
			if !reflect.DeepEqual(wantRoots, gotRoots) {
				t.Fatal("alias roots differ")
			}
			if !reflect.DeepEqual(before.snapshot(), after.snapshot()) || !reflect.DeepEqual(before.statSnapshot(), after.statSnapshot()) || !reflect.DeepEqual(before.dirSnapshot(), after.dirSnapshot()) {
				t.Fatal("probe ledgers differ")
			}
		})
	}
}
