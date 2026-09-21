package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/manifestextractor"
	"github.com/enola-labs/enola/internal/extractors/swiftextractor"
	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/pkg/plugin"
)

func TestBuiltinHiddenInputDelta(t *testing.T) {
	for _, tc := range []struct {
		name   string
		ext    plugin.Extractor
		files  map[string]string
		change func(*testing.T, string)
	}{
		{"uv-lock", manifestextractor.New(), map[string]string{"pyproject.toml": "[project]\nname = \"app\"\ndependencies = [\"requests>=2\"]\n", "uv.lock": "[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\n"}, func(t *testing.T, root string) {
			independentWrite(t, root, "uv.lock", "[[package]]\nname = \"requests\"\nversion = \"2.32.0\"\n")
		}},
		{"pruned-manifest", manifestextractor.New(), map[string]string{"package.json": "{}", "private/package.json": `{"dependencies":{"a":"1.0.0"}}`}, func(t *testing.T, root string) {
			independentWrite(t, root, "private/package.json", `{"dependencies":{"a":"2.0.0"}}`)
		}},
		{"swift-include", swiftextractor.New(), map[string]string{"Sources/Core/Value.swift": "public struct Value {}", "project.yml": "include:\n  - private/targets.data\n", "private/targets.data": "targets:\n  Core:\n    type: framework\n    sources: [Sources]\n"}, func(t *testing.T, root string) {
			independentWrite(t, root, "private/targets.data", "targets:\n  Core:\n    type: framework\n    sources: [Sources/Core]\n")
		}},
		{"swift-empty-assets", swiftextractor.New(), map[string]string{"Sources/View.swift": "import SwiftUI\nstruct ContentView: View { var body: some View { Text(\"Hi\") } }"}, func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "private/Assets.xcassets"), 0755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := setupTSRepo(t, tc.files)
			eng := independentEngine(t, root, []string{"private/**"}, tc.ext)
			applied := NewConsumer()
			initial, _ := independentRun(t, eng, root, "state", applied)
			noop, n := independentRun(t, eng, root, "state", applied)
			if n != 0 || noop.ParsedFiles != 0 || noop.TargetGeneration != initial.TargetGeneration {
				t.Fatal("unchanged input was not a noop")
			}
			tc.change(t, root)
			delta, n := independentRun(t, eng, root, "state", applied)
			cold := NewConsumer()
			fresh, _ := independentRun(t, eng, root, "cold", cold)
			if factsFingerprint(initial.Facts) == factsFingerprint(fresh.Facts) {
				t.Fatal("fixture did not change extracted graph")
			}
			if n == 0 || delta.TargetGeneration == initial.TargetGeneration {
				t.Fatal("hidden input change lost")
			}
			assertAppliedEqualsCold(t, applied, cold)
		})
	}
}

func TestSessionConfigPathsRetainPrunedConfig(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"a.ts": "export const a = 1;", "private/tsconfig.json": `{}`})
	result, err := tsextractor.New().ExtractSession(context.Background(), root, []string{"a.ts"}, nil, nil, tsextractor.SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.ConfigPaths, "private/tsconfig.json") {
		t.Fatalf("ConfigPaths narrowed: %v", result.ConfigPaths)
	}
}

func TestManifestLockInputsAndPrecedence(t *testing.T) {
	for _, lock := range []string{"uv.lock", "poetry.lock", "Pipfile.lock", "bun.lockb", "pnpm-lock.yaml"} {
		t.Run(lock, func(t *testing.T) {
			manifest, body := "private/pyproject.toml", "[project]\nname = \"app\"\ndependencies = [\"requests>=2\"]\n"
			content := "[[package]]\nname = \"requests\"\nversion = \"2.32.0\"\n"
			if lock == "bun.lockb" || lock == "pnpm-lock.yaml" {
				manifest, body, content = "private/package.json", `{"dependencies":{"requests":"^2"}}`, "nonempty lock"
			}
			root := setupTSRepo(t, map[string]string{manifest: body})
			ext := manifestextractor.New()
			// Keep the manifest discoverable to the existing FileListDetector but
			// exclude its bytes from Files, as default JSON/YAML profiles do.
			eng := independentEngine(t, root, []string{manifest}, ext)
			applied := NewConsumer()
			initial, _ := independentRun(t, eng, root, "state", applied)
			if !ext.ContentInput(lock) || !ext.OwnsFile(lock) {
				t.Fatal("lock not declared as input and owned")
			}
			independentWrite(t, root, lock, content)
			inv, err := eng.Inventory(root)
			if err != nil {
				t.Fatal(err)
			}
			targets := filesToHash(eng, inv, nil, map[string]bool{"manifests": true})
			if !slices.Contains(targets, lock) {
				t.Fatal("lock bytes omitted from shared hashing")
			}
			delta, n := independentRun(t, eng, root, "state", applied)
			cold := NewConsumer()
			fresh, _ := independentRun(t, eng, root, "cold", cold)
			if factsFingerprint(initial.Facts) == factsFingerprint(fresh.Facts) || n == 0 || delta.TargetGeneration == initial.TargetGeneration {
				t.Fatal("lock appearance fixture lost")
			}
			assertAppliedEqualsCold(t, applied, cold)
			if lock == "uv.lock" || lock == "poetry.lock" {
				independentWrite(t, root, "private/"+lock, "[[package]]\nname = \"requests\"\nversion = \"2.33.0\"\n")
				independentRun(t, eng, root, "state", applied)
				nearer := NewConsumer()
				independentRun(t, eng, root, "nearer", nearer)
				assertAppliedEqualsCold(t, applied, nearer)
				if err := os.Remove(filepath.Join(root, "private", lock)); err != nil {
					t.Fatal(err)
				}
				independentRun(t, eng, root, "state", applied)
				assertAppliedEqualsCold(t, applied, cold)
			}
		})
	}
}
