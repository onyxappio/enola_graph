package graphsession

// Guards for the claimed-name discharge. A name no extractor claims can arrive
// or leave without any fact of the graph depending on it, and the run stays
// quiet. Everything that does depend on such a name still publishes, and the
// cases below are the four readers that do: a document whose link resolves
// against the walked file set, an extractor that will not say which files it
// reads, a manifest that retargets TypeScript resolution, and a source that
// another source imports. Each positive case also asserts the incremental graph
// equals the cold one, because quiet and correct are different claims.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestClaimedMembershipStillPublishesForLinkedTarget(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":    "export const base=1;\n",
		"docs/readme.md": "# Guide\n\nSee [the schema](schema.json) for details.\n",
	})
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)

	// The document already names schema.json, so the file arriving resolves a
	// link that did not resolve before. Nothing owns the JSON, but the markdown
	// extractor reads the name set, and that is the whole difference between an
	// unclaimed name and an unread one.
	writeRepoFile(t, root, "docs/schema.json", "{\"title\":\"schema\"}\n")
	added := &graphstream.MemorySink{}
	afterAdd, err := Run(ctx, eng, root, added, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, added)
	if len(added.CloneRecords()) == 0 || afterAdd.TargetGeneration == before.TargetGeneration {
		t.Fatalf("linked target addition was discharged: events=%d generation=%d->%d", len(added.CloneRecords()), before.TargetGeneration, afterAdd.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))

	if err := os.Remove(filepath.Join(root, "docs/schema.json")); err != nil {
		t.Fatal(err)
	}
	removed := &graphstream.MemorySink{}
	afterDelete, err := Run(ctx, eng, root, removed, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, removed)
	if len(removed.CloneRecords()) == 0 || afterDelete.TargetGeneration == afterAdd.TargetGeneration {
		t.Fatalf("linked target deletion was discharged: events=%d generation=%d->%d", len(removed.CloneRecords()), afterAdd.TargetGeneration, afterDelete.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestClaimedMembershipRefusesWhenOwnershipIsOpaque(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":      "export const base=1;\n",
		"docs/readme.md":   "# Guide\n\nPlain text.\n",
		"docs/unused.json": "{}\n",
	})
	// The bare extractor is detected and declares no owner domain, so no name can
	// be shown to be unclaimed: one of its inputs might be exactly the file this
	// run would otherwise call nobody's.
	eng := multiEngine(t, root, mdintent.New(), noOwnerExtractor{name: "bare"})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)
	if err := os.Remove(filepath.Join(root, "docs/unused.json")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	after, err := Run(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	if after.TargetGeneration == before.TargetGeneration {
		t.Fatalf("opaque ownership was treated as a bounded claim set: generation=%d->%d", before.TargetGeneration, after.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestClaimedMembershipStillPublishesForManifestAddition(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":      "export const base=1;\n",
		"docs/readme.md":   "# Guide\n\nPlain text.\n",
		"docs/unused.json": "{}\n",
	})
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)
	// A nested manifest is JSON like the unclaimed file, and is claimed: it
	// retargets how the sources under it resolve. Nothing else arrives with it,
	// so the manifest is the only thing this run can be reacting to.
	writeFile(t, root, "packages/inner/package.json", "{\"name\":\"inner\",\"version\":\"1.0.0\"}\n")
	sink := &graphstream.MemorySink{}
	after, err := Run(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	if after.TargetGeneration == before.TargetGeneration {
		t.Fatalf("manifest addition was discharged: generation=%d->%d", before.TargetGeneration, after.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestClaimedMembershipStillPublishesForDisappearingImportTarget(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":      "import { dep } from './dep';\nexport const base=dep;\n",
		"src/dep.ts":       "export const dep=1;\n",
		"docs/readme.md":   "# Guide\n\nPlain text.\n",
		"docs/unused.json": "{}\n",
	})
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)
	if err := os.Remove(filepath.Join(root, "src/dep.ts")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	after, err := Run(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	if after.TargetGeneration == before.TargetGeneration {
		t.Fatalf("import target deletion was discharged: generation=%d->%d", before.TargetGeneration, after.TargetGeneration)
	}
	if len(cons.Owners[ownerKey("src/dep.ts")]) != 0 {
		t.Fatal("deleted import target still contributes")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestClaimedMembershipLegacyStateStaysSilentThenUpgrades(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":      "export const base=1;\n",
		"docs/readme.md":   "# Guide\n\nPlain text.\n",
		"docs/unused.json": "{}\n",
	})
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)

	// A state written by a release that had no claim digest at all. The marker is
	// what makes the field a comparison; without it there is nothing to compare,
	// and an absent digest must not read as a claim set that happens to be empty.
	st, err := readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if st.ScanClaimedHash == "" || st.ScanClaimedMeta != claimedScanVersion {
		t.Fatalf("initial run recorded no claim digest: hash=%q meta=%q", st.ScanClaimedHash, st.ScanClaimedMeta)
	}
	st.ScanClaimedHash = ""
	st.ScanClaimedMeta = ""
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}
	legacyPath := statePath(opts.StateDir)
	legacyBytes, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}

	// An unchanged repository on such a state stays exactly as it was: the
	// upgrade is not a reason to write state or move the generation.
	quiet := &graphstream.MemorySink{}
	noop, err := Run(ctx, eng, root, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if noop.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 || noop.TargetGeneration != before.TargetGeneration {
		t.Fatalf("legacy state no-op published: parsed=%d events=%d generation=%d->%d", noop.ParsedFiles, len(quiet.CloneRecords()), before.TargetGeneration, noop.TargetGeneration)
	}
	nowBytes, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(nowBytes) != string(legacyBytes) {
		t.Fatal("legacy state no-op rewrote state")
	}

	// The first membership change on a legacy state may conservatively publish,
	// and it is the run that records the digest.
	if err := os.Remove(filepath.Join(root, "docs/unused.json")); err != nil {
		t.Fatal(err)
	}
	upgrade := &graphstream.MemorySink{}
	afterUpgrade, err := Run(ctx, eng, root, upgrade, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, upgrade)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	st, err = readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if st.ScanClaimedMeta != claimedScanVersion || st.ScanClaimedHash == "" {
		t.Fatalf("upgrade run recorded no claim digest: hash=%q meta=%q", st.ScanClaimedHash, st.ScanClaimedMeta)
	}

	// From here the proof exists, so the next unclaimed change is silent.
	writeRepoFile(t, root, "docs/second.json", "{\"note\":\"still unclaimed\"}\n")
	silent := &graphstream.MemorySink{}
	afterSecond, err := Run(ctx, eng, root, silent, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, silent)
	if len(silent.CloneRecords()) != 0 || afterSecond.ParsedFiles != 0 || afterSecond.TargetGeneration != afterUpgrade.TargetGeneration {
		t.Fatalf("second unclaimed change published: events=%d parsed=%d generation=%d->%d fallback=%v", len(silent.CloneRecords()), afterSecond.ParsedFiles, afterUpgrade.TargetGeneration, afterSecond.TargetGeneration, afterSecond.Fallbacks)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestClaimedMembershipRefusesUnknownMarker(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":      "export const base=1;\n",
		"docs/readme.md":   "# Guide\n\nPlain text.\n",
		"docs/unused.json": "{}\n",
	})
	eng := multiEngine(t, root, mdintent.New())
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)
	// A digest written under some other definition of the claim set is a number
	// this run cannot interpret. It is not a match and it is not a legacy absence
	// either, so the only safe reading is to refuse the comparison - the same
	// reserve an unknown alias projection marker gets.
	st, err := readStateFile(statePath(opts.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if st.ScanClaimedHash == "" {
		t.Fatal("initial run recorded no claim digest to relabel")
	}
	st.ScanClaimedMeta = claimedScanVersion + "-unknown"
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "docs/unused.json")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	after, err := Run(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	if after.TargetGeneration == before.TargetGeneration {
		t.Fatalf("unknown claim marker was compared anyway: generation=%d->%d", before.TargetGeneration, after.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

func TestClaimedMembershipStillPublishesForLinkedTargetWithSecondExtractor(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/base.ts":    "export const base=1;\n",
		"docs/readme.md": "# Guide\n\nSee [the schema](schema.json) for details.\n",
		"keep.namewatch": "watched\n",
	})
	// A second name-set reader alongside the document extractor. Measured on this
	// shape, the neutral set stays empty because the stub is never previewed, so
	// this arm does not isolate the extractor-need term from the non-empty-neutral
	// term; it guards the outcome, that a resolvable link is not discharged just
	// because another extractor is in the engine.
	runs := 0
	eng := multiEngine(t, root, mdintent.New(), declaringStub{emptyDomainStub: emptyDomainStub{name: "namewatch", suffix: ".namewatch", runs: &runs}, names: true})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	ctx := context.Background()
	cons := NewConsumer()
	first := &graphstream.MemorySink{}
	before, err := Run(ctx, eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, first)
	writeRepoFile(t, root, "docs/schema.json", "{\"title\":\"schema\"}\n")
	sink := &graphstream.MemorySink{}
	after, err := Run(ctx, eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	if after.TargetGeneration == before.TargetGeneration {
		t.Fatalf("linked target addition discharged with a second extractor present: generation=%d->%d", before.TargetGeneration, after.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A TypeScript-only engine is the shape the claimed-name comparison newly
// reaches, because it has no non-TypeScript preview and so never entered the
// discharge block before. Opening that door must not also discharge the changes
// that engine does depend on, so each of them is asserted here in the same shape
// as the unclaimed cases that are now silent.
func TestClaimedMembershipTSOnlyStillPublishesClaimedChanges(t *testing.T) {
	for _, step := range []struct {
		name  string
		apply func(t *testing.T, root string)
	}{
		{"source added", func(t *testing.T, root string) {
			writeRepoFile(t, root, "src/fresh.ts", "export const fresh=3;\n")
		}},
		{"import target deleted", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "src/dep.ts")); err != nil {
				t.Fatal(err)
			}
		}},
		{"root manifest edited", func(t *testing.T, root string) {
			writeRepoFile(t, root, "tsconfig.json", "{\"compilerOptions\":{\"baseUrl\":\"src\",\"target\":\"ES2020\"}}\n")
		}},
	} {
		t.Run(step.name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"src/base.ts":      "import { dep } from './dep';\nexport const base=dep;\n",
				"src/dep.ts":       "export const dep=1;\n",
				"docs/unused.json": "{}\n",
			})
			eng := multiEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			ctx := context.Background()
			cons := NewConsumer()
			first := &graphstream.MemorySink{}
			before, err := Run(ctx, eng, root, first, opts)
			if err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, first)
			step.apply(t, root)
			sink := &graphstream.MemorySink{}
			after, err := Run(ctx, eng, root, sink, opts)
			if err != nil {
				t.Fatal(err)
			}
			applyRun(t, cons, sink)
			if after.TargetGeneration == before.TargetGeneration {
				t.Fatalf("%s was discharged in a TypeScript-only engine: generation=%d->%d fallback=%v", step.name, before.TargetGeneration, after.TargetGeneration, after.Fallbacks)
			}
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
			quiet := &graphstream.MemorySink{}
			noop, err := Run(ctx, eng, root, quiet, opts)
			if err != nil {
				t.Fatal(err)
			}
			if noop.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 || noop.TargetGeneration != after.TargetGeneration {
				t.Fatalf("%s followup not quiet: parsed=%d events=%d generation=%d->%d", step.name, noop.ParsedFiles, len(quiet.CloneRecords()), after.TargetGeneration, noop.TargetGeneration)
			}
		})
	}
}
