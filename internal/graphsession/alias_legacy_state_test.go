package graphsession

import (
	"context"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphstream"
)

// downgradeAliasState rewrites a committed state into the shape a release
// before the structured alias projection wrote: the aggregate digest and the
// alias-inclusive per-file digests, and nothing per key. This is the state
// every existing installation has on disk, and the only thing distinguishing
// it from one that genuinely declares no aliases is the absent marker.
func downgradeAliasState(t *testing.T, dir string) {
	t.Helper()
	st, err := readStateFile(statePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if st.TSAliasMeta == "" {
		t.Fatalf("the committed state is already legacy-shaped; the downgrade proves nothing")
	}
	if _, ok := st.TSContext["package export aliases"]; !ok {
		t.Fatalf("the committed state has no aggregate alias digest for a legacy reader to compare against")
	}
	trimmed := map[string]string{}
	for k, v := range st.TSContext {
		if tsextractor.IsAliasContextKey(k) {
			continue
		}
		trimmed[k] = v
	}
	if len(trimmed) == len(st.TSContext) {
		t.Fatalf("the committed state carries no per-key alias entries to strip")
	}
	st.TSContext = trimmed
	st.TSFileBase = nil
	st.TSAliasMeta = ""
	if err := saveState(dir, st); err != nil {
		t.Fatal(err)
	}
}

// TestAliasLegacyStateUnchangedRunStaysSilent is the upgrade itself. The first
// run of the new code against a state written by the old one has no per-key
// entries to compare and must not conclude anything from their absence: an
// unchanged repository is unchanged, and pays nothing to be told so.
func TestAliasLegacyStateUnchangedRunStaysSilent(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"src/consumer.ts":                  "import { fresh } from '@local/existing';\nexport const used = fresh;\n",
		"packages/existing/package.json":   `{"name":"@local/existing","main":"./first/index.ts","types":"./first/index.ts"}`,
		"packages/existing/first/index.ts": "export const fresh = 1;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	downgradeAliasState(t, opts.StateDir)

	// Not configScopeRun: the run under test publishes nothing, and that helper
	// requires a Begin to describe.
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(sink.CloneRecords()); n != 0 || res.ParsedFiles != 0 || len(res.Fallbacks) != 0 || res.TargetGeneration != res.BaseGeneration {
		t.Fatalf("first run on a released state was not silent: records=%d parsed=%d generation %d->%d fallbacks=%v",
			n, res.ParsedFiles, res.BaseGeneration, res.TargetGeneration, res.Fallbacks)
	}
}

// TestAliasLegacyStateStillSeesAnAliasChange is the other half. A state with no
// per-key entries cannot be asked which key moved, so the aggregate digest has
// to remain in force for it - conservatively, but never blindly.
func TestAliasLegacyStateStillSeesAnAliasChange(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"src/consumer.ts":                   "import { fresh } from '@local/existing';\nexport const used = fresh;\n",
		"packages/existing/package.json":    `{"name":"@local/existing","main":"./first/index.ts","types":"./first/index.ts"}`,
		"packages/existing/first/index.ts":  "export const fresh = 1;\n",
		"packages/existing/second/index.ts": "export const fresh = 2;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	downgradeAliasState(t, opts.StateDir)

	writeFile(t, root, "packages/existing/package.json", `{"name":"@local/existing","main":"./second/index.ts","types":"./second/index.ts"}`)
	res, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if !containsID(ids, "src/consumer.ts") {
		t.Fatalf("a retarget seen from a released state did not rescope its importer: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
	}
	// The conservative fallback is the point: a state with nothing per key to
	// consult must say so out loud rather than plan a bounded scope it has no
	// evidence for.
	if contextFallback(res) == "" {
		t.Fatalf("an alias change against a released state planned a bounded scope with no per-key evidence: fallbacks=%v", res.Fallbacks)
	}
}

// TestAliasLegacyStateUpgradesAndThenBoundsScope closes the loop: once a run
// has rewritten the state, the state is the structured one, and the addition
// the regression was reported for is bounded from there on.
func TestAliasLegacyStateUpgradesAndThenBoundsScope(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"src/consumer.ts":                  "import { fresh } from '@local/existing';\nexport const used = fresh;\n",
		"packages/existing/package.json":   `{"name":"@local/existing","main":"./first/index.ts","types":"./first/index.ts"}`,
		"packages/existing/first/index.ts": "export const fresh = 1;\n",
	})
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	downgradeAliasState(t, opts.StateDir)

	// Any real change carries the upgrade; nothing is published for the
	// metadata itself.
	writeFile(t, root, "src/unchanged0.ts", "export const value0 = 42;\n")
	configScopeRun(t, eng, root, opts, cons)
	st, err := readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if st.TSAliasMeta != tsextractor.AliasMetaVersion {
		t.Fatalf("a run over a released state did not upgrade it: ts_alias_meta=%q", st.TSAliasMeta)
	}

	writeFile(t, root, "packages/new/package.json", `{"name":"@local/new","main":"./src/index.ts","types":"./src/index.ts"}`)
	writeFile(t, root, "packages/new/src/index.ts", "export const brandNew = 1;\n")
	res, _, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	if res.ParsedFiles > 1 {
		t.Fatalf("an upgraded state did not get the bounded scope: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
	}
}

// TestAliasStateUnknownMetadataIsRefusedNotGuessed covers the two states this
// build must not read: one written by a version that marks something else, and
// one that claims this version without the map the mark refers to. Neither
// comparison is valid against them, so neither is made - the run reconciles
// rather than trusting a projection it cannot interpret. It must still be
// correct, and the next run must be quiet again.
func TestAliasStateUnknownMetadataIsRefusedNotGuessed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(*State)
	}{
		{"unknown version", func(st *State) { st.TSAliasMeta = "v99" }},
		{"marker without bases", func(st *State) { st.TSFileBase = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := aliasBaseRepo(t, map[string]string{
				"src/consumer.ts":                  "import { fresh } from '@local/existing';\nexport const used = fresh;\n",
				"packages/existing/package.json":   `{"name":"@local/existing","main":"./first/index.ts","types":"./first/index.ts"}`,
				"packages/existing/first/index.ts": "export const fresh = 1;\n",
			})
			eng := configScopeEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			cons := NewConsumer()
			configScopeRun(t, eng, root, opts, cons)

			st, err := readStateFile(statePath(opts.StateDir))
			if err != nil {
				t.Fatal(err)
			}
			tc.break_(st)
			if err := saveState(opts.StateDir, st); err != nil {
				t.Fatal(err)
			}

			res, _, ids := configScopeRun(t, eng, root, opts, cons)
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
			if !containsID(ids, "src/consumer.ts") {
				t.Fatalf("a state this build cannot interpret was trusted: parsed=%d scope=%v fallbacks=%v", res.ParsedFiles, ids, res.Fallbacks)
			}

			sink := &graphstream.MemorySink{}
			quiet, err := Run(context.Background(), eng, root, sink, opts)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(sink.CloneRecords()); n != 0 || quiet.ParsedFiles != 0 {
				t.Fatalf("the run after a refused state did not settle: records=%d parsed=%d fallbacks=%v", n, quiet.ParsedFiles, quiet.Fallbacks)
			}
		})
	}
}
