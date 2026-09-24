package graphsession

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestWave13DepthThreeTSConfigAliasDeltaEqualsCold(t *testing.T) {
	const importer = "packages/landings-module/test/unit/composables/useDataLayerConsumer.ts"
	const targetFile = "packages/landings-module/src/runtime/composables/useDataLayer.ts"
	const targetName = "packages/landings-module/src/runtime/composables.useDataLayer"
	const config = "packages/landings-module/test/tsconfig.json"
	const originalConfig = `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["../src/*"]}}}`
	const changedConfig = `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["../src/runtime/*"]}}}`

	for _, authoritative := range []bool{false, true} {
		t.Run(fmt.Sprintf("authoritative_%v", authoritative), func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"packages/landings-module/package.json": `{"name":"@example/landings-module"}`,
				config:                                  originalConfig,
				importer: `import { useDataLayer } from '@/runtime/composables/useDataLayer'
export function exercise() { return useDataLayer({} as never) }
`,
				targetFile: "export function useDataLayer(store: unknown) { return store }\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: filepath.Join(root, ".enola", "wave13-state"), AuthoritativeFiles: authoritative}
			applied := NewConsumer()
			run := func() (*Result, *graphstream.MemorySink) {
				t.Helper()
				sink := &graphstream.MemorySink{}
				res, err := Run(context.Background(), eng, root, sink, opts)
				if err != nil {
					t.Fatal(err)
				}
				applyRun(t, applied, sink)
				return res, sink
			}
			coldCheck := func() {
				t.Helper()
				sink := &graphstream.MemorySink{}
				if _, err := Run(context.Background(), eng, root, sink, Options{
					StateDir: filepath.Join(t.TempDir(), "cold"), AuthoritativeFiles: authoritative, ForceInitial: true,
				}); err != nil {
					t.Fatal(err)
				}
				assertAppliedEqualsCold(t, applied, applyGraph(t, sink))
			}
			writeConfig := func(body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(config)), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			assertResolved := func(want bool) {
				t.Helper()
				for _, edge := range applied.Edges[ownerKey(importer)] {
					if edge.Kind != facts.RelCalls || edge.TargetName != targetName {
						continue
					}
					if want && edge.Resolution != graphstream.ResResolved {
						t.Fatalf("nested paths call has resolution=%s", edge.Resolution)
					}
					if want {
						foundTargetFile := false
						for _, nodes := range applied.Owners {
							for _, node := range nodes {
								if node.ID == edge.TargetID && node.File == targetFile {
									foundTargetFile = true
								}
							}
						}
						if !foundTargetFile {
							t.Fatalf("nested paths call target %q did not identify source file %s", edge.TargetID, targetFile)
						}
					}
					if !want && edge.Resolution == graphstream.ResResolved {
						t.Fatalf("stale nested paths call remained resolved: %+v", edge)
					}
					return
				}
				if want {
					t.Fatalf("missing resolved call to %s from %s; edges=%v", targetName, importer, applied.Edges[ownerKey(importer)])
				}
			}
			assertChangedConfigScope := func(sink *graphstream.MemorySink) {
				t.Helper()
				if authoritative && !beginOwnerSet(t, sink)[importer] {
					t.Fatalf("frozen Begin omitted nested-config consumer %s", importer)
				}
			}

			first, _ := run()
			if first.ParsedFiles == 0 {
				t.Fatal("initial analysis parsed no files")
			}
			assertResolved(true)
			coldCheck()

			writeConfig(changedConfig)
			_, edited := run()
			assertChangedConfigScope(edited)
			assertResolved(false)
			coldCheck()

			if err := os.Remove(filepath.Join(root, filepath.FromSlash(config))); err != nil {
				t.Fatal(err)
			}
			_, deleted := run()
			assertChangedConfigScope(deleted)
			assertResolved(false)
			coldCheck()

			writeConfig(originalConfig)
			_, restored := run()
			assertChangedConfigScope(restored)
			assertResolved(true)
			coldCheck()

			quiet := &graphstream.MemorySink{}
			beforeGeneration := applied.LastGeneration
			noop, err := Run(context.Background(), eng, root, quiet, opts)
			if err != nil || noop.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 || applied.LastGeneration != beforeGeneration {
				t.Fatalf("restored no-change parsed=%d events=%d generation=%d->%d err=%v", noop.ParsedFiles, len(quiet.CloneRecords()), beforeGeneration, applied.LastGeneration, err)
			}
		})
	}
}

func TestPublishedWave13CachedUpgradeFromV317(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/generic.ts": `const imported = load<typeof import('./types')>()
export function afterGenericType() { return imported }
`,
		"src/cjs.js": `function exportedLocal() { return 1 }
module.exports = { nested: { exportedLocal } }
`,
		"src/versioned.ts": `export function versioned() { return 2 }
`,
		"src/queryConsumer.ts": `import { versioned } from './versioned?v=provider-copy'
const options = { versioned }
export function useVersioned() { return options.versioned() }
`,
		"packages/landings-module/package.json":       `{"name":"@example/landings-module"}`,
		"packages/landings-module/test/tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["../src/*"]}}}`,
		"packages/landings-module/test/unit/consumer.ts": `import { useDataLayer } from '@/runtime/composables/useDataLayer'
export function exercise() { return useDataLayer({} as never) }
`,
		"packages/landings-module/src/runtime/composables/useDataLayer.ts": `export function useDataLayer(store: unknown) { return store }
`,
	})
	eng := testEngine(t, root)
	state := filepath.Join(root, ".enola", "wave13-migration")
	opts := Options{StateDir: state, AuthoritativeFiles: true}
	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	committed := applyGraph(t, initial)
	st, err := loadCommittedState(state)
	if err != nil || st == nil {
		t.Fatalf("load committed state: %v %#v", err, st)
	}
	st.ExtractorVersion = "v317"
	if err := saveState(state, st); err != nil {
		t.Fatal(err)
	}
	upgrade := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, upgrade, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.ParsedFiles == 0 {
		t.Fatal("v317 migration reused every TypeScript contribution")
	}
	if err := committed.ApplyRecords(upgrade.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	cold := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, cold, Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ForceInitial: true}); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, committed, applyGraph(t, cold))
	quiet := &graphstream.MemorySink{}
	noop, err := Run(context.Background(), eng, root, quiet, opts)
	if err != nil || noop.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 || noop.BaseGeneration != noop.TargetGeneration {
		t.Fatalf("post-migration no-change parsed=%d events=%d generation=%d->%d err=%v", noop.ParsedFiles, len(quiet.CloneRecords()), noop.BaseGeneration, noop.TargetGeneration, err)
	}
	if engine.ExtractorVersion() == "v317" {
		t.Fatal("cache upgrade test requires a version newer than v317")
	}
}
