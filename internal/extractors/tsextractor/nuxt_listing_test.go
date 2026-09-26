package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

// Independent pre-optimization presence path. Captured bytes do not answer Stat.
func nuxtPresenceReference(ctx context.Context, dir string, scope *inputscope.Scope) bool {
	for _, name := range []string{"nuxt.config.js", "nuxt.config.ts", "nuxt.config.mjs"} {
		if _, err := overlayStat(ctx, filepath.Join(dir, name), scope); err == nil {
			return true
		}
	}
	return hasPkgDependency(ctx, dir, "nuxt", scope)
}

func TestNuxtListingPreservesPresenceAndLedger(t *testing.T) {
	for _, name := range []string{"empty", "js", "ts", "mjs", "mixed-case", "manifest", "captured-config", "captured-manifest", "directory", "link", "broken-link", "excluded", "unlistable"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "apps", "nested")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			primary := filepath.Join(dir, "nuxt.config.ts")
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			options := graphinput.Options{}
			capture := map[string][]byte{}
			switch name {
			case "js", "ts", "mjs":
				write(filepath.Join(dir, "nuxt.config."+name), "export default {}")
			case "mixed-case":
				write(filepath.Join(dir, "NuXt.CoNfIg.Ts"), "export default {}")
			case "manifest":
				write(filepath.Join(dir, "package.json"), `{"dependencies":{"nuxt":"^3"}}`)
			case "captured-config":
				capture["apps/nested/nuxt.config.ts"] = []byte("export default {}")
			case "captured-manifest":
				capture["apps/nested/package.json"] = []byte(`{"dependencies":{"nuxt":"^3"}}`)
			case "directory":
				if err := os.Mkdir(primary, 0700); err != nil {
					t.Fatal(err)
				}
			case "link", "broken-link":
				target := filepath.Join(root, "actual-config")
				if name == "link" {
					write(target, "export default {}")
				}
				if err := os.Symlink(target, primary); err != nil {
					t.Fatal(err)
				}
			case "excluded":
				write(primary, "export default {}")
				options.Exclude = []string{"**/nuxt.config.ts"}
			case "unlistable":
				write(primary, "export default {}")
			}
			policy, err := graphinput.Build(root, options)
			if err != nil {
				t.Fatal(err)
			}
			scope := &inputscope.Scope{Root: root, Policy: policy}
			if name == "unlistable" {
				if err := os.Chmod(dir, 0111); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
				if _, err := os.ReadDir(dir); err == nil {
					t.Skip("host bypasses read permission")
				}
				if _, err := os.Stat(primary); err != nil {
					t.Fatal("fixture must allow direct config Stat", err)
				}
			}
			run := func(optimized bool) (bool, *overlayProbe) {
				p := newOverlayProbe()
				ctx := withOverlayProbe(withFileOverlay(context.Background(), newFileOverlay(root, capture)), p)
				sharedDiscoveryEntries(ctx, root, scope)
				if optimized {
					return detectNuxtAt(ctx, dir, scope), p
				}
				return nuxtPresenceReference(ctx, dir, scope), p
			}
			want, ref := run(false)
			got, actual := run(true)
			expected := name != "empty" && name != "captured-config" && name != "broken-link" && name != "excluded"
			if name == "mixed-case" {
				_, statErr := os.Stat(primary)
				expected = statErr == nil
			}
			if want != expected || got != want {
				t.Fatalf("presence got=%v reference=%v expected=%v", got, want, expected)
			}
			if !reflect.DeepEqual(ref.snapshot(), actual.snapshot()) || !reflect.DeepEqual(ref.statSnapshot(), actual.statSnapshot()) || !reflect.DeepEqual(ref.dirSnapshot(), actual.dirSnapshot()) {
				t.Fatalf("observation ledger changed: reference stats=%v actual=%v", ref.statSnapshot(), actual.statSnapshot())
			}
		})
	}
}

func TestNuxtListingAppearanceInvalidatesRetainedDiscovery(t *testing.T) {
	root := retentionRepo(t, map[string]string{"package.json": `{"name":"app"}`, "src/a.ts": "export const a = 1"})
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := NewGraph(&inputscope.Scope{Root: root, Policy: policy})
	before, _, _ := e.DiscoveryFor(context.Background(), root, nil, nil)
	if before.nuxt {
		t.Fatal("unexpected initial Nuxt detection")
	}
	key := absOverlayKey(filepath.Join(root, "nuxt.config.ts"))
	if before.statReads[key] != observedStatMissing {
		t.Fatal("absence observation missing")
	}
	writeRetentionFile(t, root, "nuxt.config.ts", "export default {}")
	if reused, _ := e.ReuseDiscovery(root, nil, before); reused != nil {
		t.Fatal("new config reused stale discovery")
	}
	after, reused, _ := e.DiscoveryFor(context.Background(), root, nil, before)
	if reused || !after.nuxt {
		t.Fatal("fresh discovery failed to detect appearing config")
	}
}

func TestNuxtListingNeedsCompleteGraphObservation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nuxt.config.ts")
	policy, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scope := &inputscope.Scope{Root: root, Policy: policy}
	p := newOverlayProbe()
	ctx := withOverlayProbe(context.Background(), p)
	if nuxtConfigAbsentFromListing(ctx, path, scope) {
		t.Fatal("unobserved directory treated as empty")
	}
	if _, err := overlayReadDir(ctx, root, scope); err != nil {
		t.Fatal(err)
	}
	if !nuxtConfigAbsentFromListing(ctx, path, scope) {
		t.Fatal("complete absence was not reused")
	}
	if nuxtConfigAbsentFromListing(ctx, path, nil) {
		t.Fatal("legacy profile must preserve direct presence checks")
	}
}
