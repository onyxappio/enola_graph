package tsextractor

import (
	"context"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/graphinput"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAliasListingHandlesFilesystemShapes(t *testing.T) {
	for _, name := range []string{"empty", "config", "mixed-case", "symlink", "capture", "read-error", "testdata"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := root
			if name == "testdata" {
				dir = filepath.Join(root, "testdata")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := []byte(`{"compilerOptions":{"paths":{"@x/*":["./src/*"]}}}`)
			ctx := context.Background()
			if name == "capture" {
				ctx = withFileOverlay(ctx, newFileOverlay(dir, map[string][]byte{"tsconfig.json": cfg}))
			} else if name != "empty" {
				target := filepath.Join(dir, "tsconfig.json")
				if name == "mixed-case" {
					target = filepath.Join(dir, "TsCoNfIg.JsOn")
				}
				if name == "symlink" {
					target = filepath.Join(root, "actual.json")
				}
				if err := os.WriteFile(target, cfg, 0600); err != nil {
					t.Fatal(err)
				}
				if name == "symlink" {
					if err := os.Symlink(target, filepath.Join(dir, "tsconfig.json")); err != nil {
						t.Fatal(err)
					}
				}
			}
			entries, err := os.ReadDir(dir)
			if name == "read-error" {
				entries = nil
				err = fs.ErrPermission
			}
			want, wok := aliasesAtDir(ctx, dir)
			got, gok := aliasesAtDirFromListing(ctx, dir, entries, err, nil)
			if wok != gok || !reflect.DeepEqual(want, got) {
				t.Fatalf("mismatch want %v/%v got %v/%v", want, wok, got, gok)
			}
			if name == "testdata" {
				roots := collectTSAliasRoots(ctx, root)
				if len(roots) != 1 || roots[0].dir != "testdata" {
					t.Fatalf("testdata root lost: %v", roots)
				}
			}
		})
	}
}

// Frozen walk ordering from final Stage23; parsing helpers remain shared.
func aliasListingReferenceWalk(ctx context.Context, repoPath, dir string, out *[]tsAliasRoot, visit func(string, []fs.DirEntry), inputScope *inputscope.Scope) {
	if aliases, ok := aliasesAtDir(ctx, dir, inputScope); ok {
		rel, err := filepath.Rel(repoPath, dir)
		if err != nil || rel == "." {
			rel = ""
		}
		rel = factpath.Slash(rel)

		// Concatenation, not filepath.Join, to preserve the trailing slash
		// resolveImportPath's `replacement + rest` depends on.
		//
		// Exact entries are qualified the same way: packages/tsconfig.base.json declares
		// "@excalidraw/common": ["./common/src/index.ts"], which means
		// packages/common/src/index.ts. Skipping this resolved one directory too high.
		qualified := make(map[string]tsAlias, len(aliases))
		for prefix, a := range aliases {
			if rel != "" {
				a.replacement = rel + "/" + a.replacement
			}
			qualified[prefix] = a
		}
		*out = append(*out, tsAliasRoot{dir: rel, aliases: qualified})
	}
	entries, err := overlayReadDir(ctx, dir, inputScope)
	if err != nil {
		return
	}
	if visit != nil {
		visit(dir, entries)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || tsSkipDirs[entry.Name()] {
			continue
		}
		aliasListingReferenceWalk(ctx, repoPath, filepath.Join(dir, entry.Name()), out, visit, inputScope)
	}
}

func TestAliasListingWalkPreservesObservationLedger(t *testing.T) {
	for _, name := range []string{"missing", "captured-missing", "policy-excluded", "dangling-link", "invalid-primary", "directory-name", "testdata", "unlistable"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "pkg")
			if name == "testdata" {
				dir = filepath.Join(root, "testdata")
			}
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			cfg := []byte(`{"compilerOptions":{"paths":{"@x/*":["./src/*"]}}}`)
			write := func(path string, b []byte) {
				t.Helper()
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			primary := filepath.Join(dir, "tsconfig.json")
			var sources map[string][]byte
			options := graphinput.Options{}
			switch name {
			case "captured-missing":
				rel, err := filepath.Rel(root, primary)
				if err != nil {
					t.Fatal(err)
				}
				sources = map[string][]byte{rel: cfg}
			case "policy-excluded":
				write(primary, cfg)
				options.Exclude = []string{"**/tsconfig.json"}
			case "dangling-link":
				if err := os.Symlink(filepath.Join(root, "absent-target"), primary); err != nil {
					t.Fatal(err)
				}
			case "invalid-primary":
				write(primary, []byte("not json"))
				write(filepath.Join(dir, "tsconfig.base.json"), cfg)
			case "directory-name":
				if err := os.Mkdir(primary, 0700); err != nil {
					t.Fatal(err)
				}
			case "testdata", "unlistable":
				write(primary, cfg)
			}
			policy, err := graphinput.Build(root, options)
			if err != nil {
				t.Fatal(err)
			}
			scope := &inputscope.Scope{Root: root, Policy: policy}
			if name == "policy-excluded" && scope.Allowed(primary, false) {
				t.Fatal("fixture did not exclude config")
			}
			if name == "unlistable" {
				if err := os.Chmod(dir, 0111); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
				if _, err := os.ReadDir(dir); err == nil {
					t.Skip("host bypasses directory read permission; unreadable-listing case not exercised")
				}
				if _, err := os.ReadFile(primary); err != nil {
					t.Fatalf("config must remain individually readable: %v", err)
				}
			}
			before, after := newOverlayProbe(), newOverlayProbe()
			beforeCtx := withOverlayProbe(withFileOverlay(context.Background(), newFileOverlay(root, sources)), before)
			afterCtx := withOverlayProbe(withFileOverlay(context.Background(), newFileOverlay(root, sources)), after)
			var want, got []tsAliasRoot
			aliasListingReferenceWalk(beforeCtx, root, root, &want, nil, scope)
			walkTSAliasRootsVisit(afterCtx, root, root, &got, nil, scope)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("roots differ: %v / %v", want, got)
			}
			if !reflect.DeepEqual(before.snapshot(), after.snapshot()) {
				t.Fatalf("byte ledger differs: %v / %v", before.snapshot(), after.snapshot())
			}
			if !reflect.DeepEqual(before.statSnapshot(), after.statSnapshot()) {
				t.Fatal("stat ledger differs")
			}
			if !reflect.DeepEqual(before.dirSnapshot(), after.dirSnapshot()) {
				t.Fatal("directory ledger differs")
			}
			if name == "policy-excluded" {
				if _, ok := after.snapshot()[absOverlayKey(primary)]; ok {
					t.Fatal("policy refusal became an observation")
				}
			}
			if name == "missing" && after.snapshot()[absOverlayKey(primary)] != observedMissing {
				t.Fatal("missing-name fence was not exercised")
			}
			if name == "captured-missing" || name == "invalid-primary" || name == "testdata" || name == "unlistable" {
				if len(got) != 1 {
					t.Fatalf("expected one alias root, got %v", got)
				}
			}
		})
	}
}
