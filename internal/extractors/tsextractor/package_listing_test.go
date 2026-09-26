package tsextractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

// Keep the original read/parse path independent of the listing optimization.
func packageDependencyReference(ctx context.Context, dir, pkg string, scope *inputscope.Scope) bool {
	data, err := overlayReadFile(ctx, filepath.Join(dir, "package.json"), scope)
	if err != nil {
		return false
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil {
		return false
	}
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies"} {
		if deps, ok := manifest[key].(map[string]any); ok {
			if _, ok := deps[pkg]; ok {
				return true
			}
		}
	}
	return false
}

func TestPackageListingReaderParity(t *testing.T) {
	for _, shape := range []string{"empty", "present", "mixed-case", "malformed", "captured", "captured-empty", "directory", "link", "broken-link", "excluded", "unknown", "unreadable"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "nested")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "package.json")
			body := `{"dependencies":{"nuxt":"3"}}`
			write := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			capture := map[string][]byte{}
			options := graphinput.Options{}
			switch shape {
			case "present", "excluded", "unreadable":
				write(path, body)
			case "mixed-case":
				write(filepath.Join(dir, "PaCkAgE.JsOn"), body)
			case "malformed":
				write(path, "{")
			case "captured":
				capture["nested/package.json"] = []byte(body)
			case "captured-empty":
				write(path, body)
				capture["nested/package.json"] = []byte{}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "link", "broken-link":
				target := filepath.Join(root, "manifest")
				if shape == "link" {
					write(target, body)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if shape == "excluded" {
				options.Exclude = []string{"**/package.json"}
			}
			policy, err := graphinput.Build(root, options)
			if err != nil {
				t.Fatal(err)
			}
			scope := &inputscope.Scope{Root: root, Policy: policy}
			if shape == "unreadable" {
				if err := os.Chmod(dir, 0111); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
				if _, err := os.ReadDir(dir); err == nil {
					t.Skip("host bypasses directory permissions")
				}
			}
			run := func(optimized bool) (bool, *overlayProbe) {
				p := newOverlayProbe()
				ctx := withOverlayProbe(withFileOverlay(context.Background(), newFileOverlay(root, capture)), p)
				if shape != "unknown" {
					sharedDiscoveryEntries(ctx, root, scope)
				}
				if optimized {
					return hasPkgDependency(ctx, dir, "nuxt", scope), p
				}
				return packageDependencyReference(ctx, dir, "nuxt", scope), p
			}
			want, reference := run(false)
			got, actual := run(true)
			if got != want {
				t.Fatalf("got %v reference %v", got, want)
			}
			if !reflect.DeepEqual(reference.snapshot(), actual.snapshot()) || !reflect.DeepEqual(reference.statSnapshot(), actual.statSnapshot()) || !reflect.DeepEqual(reference.dirSnapshot(), actual.dirSnapshot()) {
				t.Fatal("observation ledger changed")
			}
		})
	}
}

func TestPackageListingAppearanceInvalidatesDiscovery(t *testing.T) {
	root := retentionRepo(t, map[string]string{"src/a.ts": "export const a = 1"})
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := NewGraph(&inputscope.Scope{Root: root, Policy: policy})
	before, _, _ := e.DiscoveryFor(context.Background(), root, nil, nil)
	if before.nuxt {
		t.Fatal("unexpected Nuxt")
	}
	writeRetentionFile(t, root, "package.json", `{"dependencies":{"nuxt":"3"}}`)
	if reused, _ := e.ReuseDiscovery(root, nil, before); reused != nil {
		t.Fatal("new manifest reused stale discovery")
	}
	after, reused, _ := e.DiscoveryFor(context.Background(), root, nil, before)
	if reused || !after.nuxt {
		t.Fatal("appearing manifest was not detected")
	}
}

func TestPackageListingRequiresProofAndPreservesFirstRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "package.json")
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	p := newOverlayProbe()
	ctx := withOverlayProbe(context.Background(), p)
	if packageManifestAbsentFromListing(ctx, root, path, scope) {
		t.Fatal("unobserved directory treated as empty")
	}
	if _, err := overlayReadDir(ctx, root, scope); err != nil {
		t.Fatal(err)
	}
	p.record(absOverlayKey(path), "first-read")
	if !packageManifestAbsentFromListing(ctx, root, path, scope) {
		t.Fatal("completed empty listing was not reused")
	}
	if p.snapshot()[absOverlayKey(path)] != "first-read" {
		t.Fatal("first observation replaced")
	}
	if packageManifestAbsentFromListing(ctx, root, path, nil) {
		t.Fatal("legacy reader must remain unchanged")
	}
}
