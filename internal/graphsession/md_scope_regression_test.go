package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

// The markdown extractor owns every .md file in a repository, and seeding the
// frozen manifest with all of them made an unrelated TypeScript edit announce
// hundreds of owners it never touched. These cases hold the narrowed manifest
// to the same standard as the rest of membership planning: the concrete Begin
// scope is asserted, and the applied graph is judged against a cold session,
// because a manifest that is merely smaller can also be missing an owner.
//
// The narrowing works by compiling the pages once BEFORE Begin and comparing
// each page's contribution against the stored one, so the cases that matter are
// the ones where a page's own bytes did not change: what it links to, what
// resolves, and what other owners name.

// mdScopeEngine returns the engine and the markdown extractor it registered, so
// a case can ask how many times its pages were compiled.
func mdScopeEngine(t *testing.T, dir string) (*engine.Engine, *mdintent.Extractor) {
	t.Helper()
	md := mdintent.New()
	return multiEngine(t, dir, md), md
}

func mdScopeOpts(t *testing.T) Options {
	t.Helper()
	return Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
}

// mdScopeInitial runs the analysis that leaves state behind and returns the
// consumer holding it.
func mdScopeInitial(t *testing.T, eng *engine.Engine, root string, opts Options) *Consumer {
	t.Helper()
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	return cons
}

// lastBeginScope is beginScope for a stream that carries more than one run: a
// resident publishes every reconcile into the same sink, so the run under test
// is the final Begin/End pair rather than the only one.
func lastBeginScope(t *testing.T, sink *graphstream.MemorySink) (map[string]bool, []string) {
	t.Helper()
	bs, _, ends, err := DecodeRun(sink.CloneRecords())
	if err != nil || len(bs) == 0 || len(bs) != len(ends) {
		t.Fatalf("expected matched Begin/End pairs: begins=%d ends=%d err=%v", len(bs), len(ends), err)
	}
	b, e := bs[len(bs)-1], ends[len(ends)-1]
	if b.OwnerScopeDigest != e.OwnerScopeDigest || b.OwnerScopeCount != e.OwnerScopeLen {
		t.Fatalf("owner scope grew after Begin: begin=%d/%s end=%d/%s",
			b.OwnerScopeCount, b.OwnerScopeDigest, e.OwnerScopeLen, e.OwnerScopeDigest)
	}
	owners := map[string]bool{}
	ids := make([]string, 0, len(b.OwnerScope))
	for _, o := range b.OwnerScope {
		owners[o.ID] = true
		ids = append(ids, o.ID)
	}
	sort.Strings(ids)
	return owners, ids
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ownsSection reports whether a markdown symbol by that name is in the applied
// graph. Sections are named by the fragment that addresses them, so an edited
// heading is visible as one name leaving and another arriving.
func ownsSection(c *Consumer, name string) bool {
	for _, nodes := range c.Owners {
		for _, n := range nodes {
			if n.Name == name {
				return true
			}
		}
	}
	return false
}

// stateDigest fingerprints everything the session persisted. A run that fails
// before it publishes anything may leave nothing at all behind, so this is the
// right standard for a pre-Begin refusal.
func stateDigest(t *testing.T, dir string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(dir, p)
		sum.Write([]byte(filepath.ToSlash(rel)))
		sum.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// committedGeneration returns the completed state a consumer would recover
// from, and its digest. A run that fails AFTER Begin has already journaled and
// acknowledged envelopes it published in good faith - commit.json tracks the
// size and checksum of those journal files and legitimately moves - so whole
// directory equality asserts something untrue there. Promotion is what must not
// move: state.json is written by promoting pending-state.json, so holding both
// still holds the generation still.
func committedGeneration(t *testing.T, dir string) (*State, string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := readStateFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return st, hex.EncodeToString(sum[:])
}

// forbidSuccessfulEnd fails if the stream carries an EndReplace declaring
// success. A refusal that still published one would have told every consumer
// the replacement landed.
func forbidSuccessfulEnd(t *testing.T, sink *graphstream.MemorySink) {
	t.Helper()
	for _, r := range sink.CloneRecords() {
		var e graphstream.EndReplace
		if json.Unmarshal(r.Payload, &e); e.Type != graphstream.TypeEndReplace {
			continue
		}
		if e.Completeness.Status == "success" {
			t.Fatalf("the refused run published a successful EndReplace (%s)", r.MsgID)
		}
	}
}

// An unrelated TypeScript add changes the name set, which the markdown
// extractor consumes, so it reruns — but no page's contribution moves. The
// pages must stay out of the frozen manifest, and the rerun must compile them
// once, not once to plan with and again to publish.
func TestMDScopeUnrelatedSourceAddKeepsUnchangedPagesOutOfBegin(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe source is [a](../src/a.ts).\n",
		"docs/notes.md": "# Notes\n\nSee [guide](guide.md).\n",
	})
	eng, md := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	writeFile(t, root, "src/added.ts", "export const added = 1;\n")
	compiled := md.ExtractCalls()
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "src/added.ts")
	forbidOwners(t, owners, ids, "docs/guide.md", "docs/notes.md")
	requireNoWholeDomainFallback(t, delta, ids)
	if got := md.ExtractCalls() - compiled; got != 1 {
		t.Fatalf("mdintent compiled %d times in one delta, want exactly one", got)
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A link that pointed at nothing resolves the moment its target is added, and
// the page that carries it never changed. Cached relations cannot show this —
// an unresolved link is absent from them — so the page has to be compiled
// before Begin for the manifest to carry it.
func TestMDScopeLinkResolvingOnAddedTargetIncludesPage(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nLater lives in [later](../src/later.ts).\n",
		"docs/notes.md": "# Notes\n\nNothing links out.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	writeFile(t, root, "src/later.ts", "export const later = 1;\n")
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/guide.md", "src/later.ts")
	forbidOwners(t, owners, ids, "docs/notes.md")
	requireNoWholeDomainFallback(t, delta, ids)

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// Links resolve against the walked file set, not the filesystem, so a directory
// exists only while it holds a kept file. Deleting the last child takes a link
// away from a page whose own bytes are untouched.
func TestMDScopeDeletedLastDirectoryChildIncludesLinkingPage(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":         "export const a = 1;\n",
		"src/area/only.ts": "export const only = 1;\n",
		"docs/guide.md":    "# Guide\n\nThe area is [area](../src/area).\n",
		"docs/notes.md":    "# Notes\n\nNothing links out.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	if err := os.RemoveAll(filepath.Join(root, "src/area")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/guide.md", "src/area/only.ts")
	forbidOwners(t, owners, ids, "docs/notes.md")
	requireNoWholeDomainFallback(t, delta, ids)

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A narrowed replacement still has to retire what it drops: the page's old
// sections must be gone from the applied graph, and the pages that did not move
// must stay out of the manifest.
func TestMDScopeModifiedPageRetiresItsOldContribution(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\n## Old section\n\nProse.\n",
		"docs/keep.md":  "# Keep\n\nNothing links out.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !ownsSection(cons, "docs/guide.md#old-section") {
		t.Fatal("fixture produced no section fact for the heading under test")
	}

	writeFile(t, root, "docs/guide.md", "# Guide\n\n## New section\n\nProse.\n")
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/guide.md")
	forbidOwners(t, owners, ids, "docs/keep.md")
	requireNoWholeDomainFallback(t, delta, ids)
	if ownsSection(cons, "docs/guide.md#old-section") {
		t.Fatal("the retired section survived the delta")
	}
	if !ownsSection(cons, "docs/guide.md#new-section") {
		t.Fatal("the new section was not published")
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A deleted page must lose its whole contribution. Retiring a published owner
// that was never a TypeScript source still widens the frozen manifest to the
// prior/current domain - the cached import graph cannot enumerate its consumers
// - so what this case holds is the retirement itself: the narrowed markdown
// seed must not cost a deleted page its removal.
func TestMDScopeRemovedPageRetiresItsContribution(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nProse.\n",
		"docs/old.md":   "# Old\n\nProse.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if !ownsFile(cons, "docs/old.md") {
		t.Fatal("fixture produced no fact owned by docs/old.md")
	}

	if err := os.Remove(filepath.Join(root, "docs/old.md")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/old.md")
	if ownsFile(cons, "docs/old.md") {
		t.Fatal("facts owned by the deleted docs/old.md survived the delta")
	}

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// docs/a.md declares a relation to docs/b.md and nothing else, so its own facts
// are byte-identical whether or not anything answers to that name. Rewriting
// b.md from an intent page into an ordinary document makes "docs/b.md" appear
// as a candidate name, and a.md now resolves against it - a change a.md's own
// contribution cannot show. The session's dirty set is built from TypeScript
// sources, so only the markdown candidate-name union puts a.md inside Begin.
func TestMDScopeGlobalNameConsumerIncludedThoughItsPageIsUnchanged(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":     "export const a = 1;\n",
		"docs/a.md":    "---\nenola_intent:\n  page:\n    type: decision\n    status: living\n    relations:\n      - {rel: depends-on, to: docs/b.md}\n---\n\n# A\n\nProse.\n",
		"docs/b.md":    "---\nenola_intent:\n  page:\n    type: decision\n    status: living\n---\n\n# B\n\nProse.\n",
		"docs/keep.md": "# Keep\n\nNothing links out.\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	if ownsSection(cons, "docs/b.md") {
		t.Fatal("fixture already publishes the document name the delta is supposed to introduce")
	}

	writeFile(t, root, "docs/b.md", "# B\n\nProse.\n")
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/a.md", "docs/b.md")
	forbidOwners(t, owners, ids, "docs/keep.md")
	requireNoWholeDomainFallback(t, delta, ids)

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A delta and a watch reconcile are the same transaction reached two ways.
// Given the same tree they must freeze the same manifest, or the narrowing
// would be a property of the entry point rather than of the planner.
func TestMDScopeDeltaAndWatchFreezeTheSameManifest(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nLater lives in [later](../src/later.ts).\n",
		"docs/notes.md": "# Notes\n\nNothing links out.\n",
	})
	eng, _ := mdScopeEngine(t, root)

	deltaOpts := mdScopeOpts(t)
	mdScopeInitial(t, eng, root, deltaOpts)

	watchSink := &graphstream.MemorySink{}
	watchOpts := mdScopeOpts(t)
	r, err := OpenSession(context.Background(), eng, root, watchSink, watchOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	q := NewChangeQueue("md-scope", 8)
	q.Start(context.Background())
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "src/later.ts", "export const later = 1;\n")

	deltaSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, deltaSink, deltaOpts); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "test reconcile"}); err != nil {
		t.Fatal(err)
	}

	_, deltaIDs := beginScope(t, deltaSink)
	_, watchIDs := lastBeginScope(t, watchSink)
	if !reflect.DeepEqual(deltaIDs, watchIDs) {
		t.Fatalf("delta froze %v, watch froze %v", deltaIDs, watchIDs)
	}
}

// A previewed extraction is consumed, not discarded, so the run stores the
// contributions and refreshes the per-file hashes it read. Without that the
// prose edit below would leave the stored hash stale and every later run would
// re-extract for the same reason, forever.
func TestMDScopeConsumedPreviewRefreshesHashesAndLeavesATrueNoop(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe source is [a](../src/a.ts).\n",
	})
	eng, md := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	// Prose carries no facts, so the page's contribution is identical while its
	// bytes — and therefore its hash — are not.
	writeFile(t, root, "docs/guide.md", "# Guide\n\nThe source is [a](../src/a.ts).\n\nMore prose, no links.\n")
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))

	compiled := md.ExtractCalls()
	idle := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, idle, opts); err != nil {
		t.Fatal(err)
	}
	if got := md.ExtractCalls() - compiled; got != 0 {
		t.Fatalf("an unchanged repository compiled its pages %d time(s); the consumed preview must have refreshed the stored hashes", got)
	}
	begins, _, _, err := DecodeRun(idle.CloneRecords())
	if err != nil {
		t.Fatal(err)
	}
	if len(begins) != 0 {
		t.Fatalf("an unchanged repository published %d replacement(s)", len(begins))
	}
}

// mdScopeBeginSink runs a callback the moment the transaction publishes its
// Begin. The manifest is frozen and the preview's pages are already read by
// then, so an edit made here lands strictly after the preview and strictly
// before the sources are read back ahead of EndReplace.
type mdScopeBeginSink struct {
	graphstream.MemorySink
	once    sync.Once
	onBegin func()
}

func (s *mdScopeBeginSink) Publish(ctx context.Context, subject, id string, payload []byte) error {
	var p struct{ Type string }
	if json.Unmarshal(payload, &p); p.Type == graphstream.TypeBeginReplace && s.onBegin != nil {
		s.once.Do(s.onBegin)
	}
	return s.MemorySink.Publish(ctx, subject, id, payload)
}

// The preview reads its pages before Begin, so the hash fence it applies there
// only speaks for that instant. Everything else the run reads is read back and
// compared before EndReplace, and the previewed pages have to be held to the
// same rule: a page edited while the TypeScript half of the same transaction is
// still parsing must fail the run, leaving the completed generation exactly as
// it was, rather than commit a state that describes bytes no longer on disk.
func TestMDScopePageEditedAfterPreviewFailsTheRunAndKeepsTheCompletedState(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe source is [a](../src/a.ts).\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)
	kept, before := committedGeneration(t, opts.StateDir)

	// The add is what makes mdintent rerun at all: it consumes the name set.
	writeFile(t, root, "src/added.ts", "export const added = 1;\n")
	edited := &mdScopeBeginSink{onBegin: func() {
		writeFile(t, root, "docs/guide.md", "# Guide\n\nThe source is [a](../src/a.ts) and [added](../src/added.ts).\n")
	}}
	if _, err := Run(context.Background(), eng, root, edited, opts); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a page edited after the preview ended the run with %v, want ErrInputsChanged", err)
	}
	// The sink is kept, not discarded: the refusal is only a refusal if no
	// consumer was told the replacement succeeded.
	forbidSuccessfulEnd(t, &edited.MemorySink)
	now, after := committedGeneration(t, opts.StateDir)
	if after != before {
		t.Fatalf("the failed run rewrote the completed state: generation %d/%v became %d/%v",
			kept.Generation, kept.LastComplete, now.Generation, now.LastComplete)
	}
	if !now.LastComplete {
		t.Fatal("the failed run left the completed generation marked incomplete")
	}
	// Nor may it leave the next run a staged generation built from the bytes it
	// refused to publish; the refusal has to be complete, not deferred.
	if _, err := os.Stat(filepath.Join(opts.StateDir, "pending-state.json")); err == nil {
		t.Fatal("the failed run left a pending state staged for the next run to adopt")
	}

	// The generation that survived is still a base to build on: with the edit
	// settled, the next run publishes from it and lands where cold lands.
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// A preview that refuses does so before Begin, so nothing has been published
// and nothing has been journaled. Where the post-Begin case can only hold the
// promotion files still, this one holds the whole state directory still: a
// malformed declaration must end the run having touched nothing at all.
func TestMDScopeFatalPreviewRefusesBeforeBeginAndTouchesNothing(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe source is [a](../src/a.ts).\n",
	})
	eng, _ := mdScopeEngine(t, root)
	opts := mdScopeOpts(t)
	mdScopeInitial(t, eng, root, opts)
	before := stateDigest(t, opts.StateDir)

	// The add makes mdintent rerun; the declaration is what it then chokes on.
	writeFile(t, root, "src/added.ts", "export const added = 1;\n")
	writeFile(t, root, "docs/guide.md", "---\nenola_intent:\n  consumes:\n    - {repo: a, target: b, via: rest}\n---\nbody\n")

	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err == nil {
		t.Fatal("a malformed declaration must fail the run, not publish a snapshot that lost it")
	}
	if len(sink.CloneRecords()) != 0 {
		t.Fatalf("the preview refused after publishing %d envelope(s)", len(sink.CloneRecords()))
	}
	forbidSuccessfulEnd(t, sink)
	if after := stateDigest(t, opts.StateDir); after != before {
		t.Fatal("a run that refused before Begin still wrote to the state directory")
	}
}

// The narrowing belongs to mdintent, not to the loop that seeds the manifest.
// A second extractor that declares file ownership has no preview and no
// per-owner comparison, so its whole owner domain must still be seeded — and
// the prepared markdown extraction, which lives on the session until the
// extraction site consumes it, must not be mistaken for a plan for it.
func TestMDScopeNarrowingLeavesOtherOwnerDeclaringExtractorsWhole(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe source is [a](../src/a.ts).\n",
		"notes/one.txt": "one\n",
		"notes/two.txt": "two\n",
	})
	md := mdintent.New()
	notes := stubExtractor{
		name:   "txtnotes",
		detect: true,
		suffix: ".txt",
		fact: func(rel string) []facts.Fact {
			return []facts.Fact{{Kind: facts.KindSymbol, Name: "note:" + rel, File: rel}}
		},
	}
	eng := multiEngine(t, root, md, notes)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	writeFile(t, root, "src/added.ts", "export const added = 1;\n")
	sink := &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)

	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "src/added.ts", "notes/one.txt", "notes/two.txt")
	forbidOwners(t, owners, ids, "docs/guide.md")
	requireNoWholeDomainFallback(t, delta, ids)

	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}

// The preview plans from bytes it captured, and narrowing is only sound if those
// bytes are the ones this run's hashes describe. When they disagree — the tree
// moved under the walk, and an edit that lands back on the original content
// would defeat a before/after re-read — the run fails here, ahead of Begin.
// Falling back to the ordinary extraction is not the conservative answer: it
// would read the moved tree and publish facts the state then files under the
// hashes of a revision that is gone.
func TestMDScopePreviewFailsClosedWhenCapturedBytesDoNotMatchRunHashes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/guide.md", "# Guide\n\nProse.\n")
	body, err := os.ReadFile(filepath.Join(root, "docs/guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)

	files := []string{"docs/guide.md"}
	prev := map[string]*FileState{}
	honest := map[string]string{"docs/guide.md": hex.EncodeToString(sum[:])}
	newSession := func() (*session, *mdintent.Extractor) {
		return &session{abs: root, state: newState("repo", "default", root, "test")}, mdintent.New()
	}

	s, md := newSession()
	stale := map[string]string{"docs/guide.md": strings.Repeat("0", 64)}
	extra, previewed, err := s.prepareMDScope(context.Background(), md, files, prev, stale, "repo", false)
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("bytes the run does not vouch for ended the preview with %v, want ErrInputsChanged", err)
	}
	if extra != nil || previewed || s.preparedMD != nil {
		t.Fatalf("a failed fence still narrowed: extra=%v previewed=%v prepared=%v", extra, previewed, s.preparedMD)
	}
	if n := md.ExtractCalls(); n != 0 {
		t.Fatalf("a refused preview compiled %d page set(s); the fence must come first", n)
	}

	// A page this run hashed and can no longer read. The walk listed it, so the
	// ordinary extraction would skip it and store its owner as contributing
	// nothing — a deletion the repository never made.
	s, md = newSession()
	withGone := map[string]string{"docs/guide.md": honest["docs/guide.md"], "docs/gone.md": strings.Repeat("0", 64)}
	extra, previewed, err = s.prepareMDScope(context.Background(), md, []string{"docs/guide.md", "docs/gone.md"}, prev, withGone, "repo", false)
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("an unreadable hashed page ended the preview with %v, want ErrInputsChanged", err)
	}
	if extra != nil || previewed {
		t.Fatalf("a partial capture still narrowed: extra=%v previewed=%v", extra, previewed)
	}

	// A page this run never hashed is a different matter: nothing can be fenced
	// either way, and that is not evidence of a change. The conservative
	// whole-extractor seed still covers it, so the preview declines quietly.
	s, md = newSession()
	extra, previewed, err = s.prepareMDScope(context.Background(), md, files, prev, map[string]string{}, "repo", false)
	if err != nil || previewed || extra != nil || s.preparedMD != nil {
		t.Fatalf("an unhashed page did not decline quietly: extra=%v previewed=%v prepared=%v err=%v", extra, previewed, s.preparedMD, err)
	}
	if n := md.ExtractCalls(); n != 0 {
		t.Fatalf("a declined preview compiled %d page set(s)", n)
	}

	s, md = newSession()
	_, previewed, err = s.prepareMDScope(context.Background(), md, files, prev, honest, "repo", false)
	if err != nil || !previewed {
		t.Fatalf("preview declined bytes that match the hashes this run records: previewed=%v err=%v", previewed, err)
	}
	if s.preparedMD == nil {
		t.Fatal("a preview that reported narrowing prepared no extraction")
	}
	if n := md.ExtractCalls(); n != 1 {
		t.Fatalf("preview compiled %d time(s), want one", n)
	}
	// The captured bytes join the sources the run reads back before EndReplace;
	// see the fence case above for what that buys.
	if _, ok := s.capturedSources["docs/guide.md"]; !ok {
		t.Fatalf("the previewed bytes are not among the %d source(s) revalidated before EndReplace", len(s.capturedSources))
	}

	// A conservative context keeps the whole-extractor seed and never previews.
	s, md = newSession()
	if extra, previewed, err := s.prepareMDScope(context.Background(), md, files, prev, honest, "repo", true); err != nil || previewed || extra != nil || s.preparedMD != nil {
		t.Fatalf("preview ran in a conservative context: extra=%v previewed=%v err=%v", extra, previewed, err)
	}
	if n := md.ExtractCalls(); n != 0 {
		t.Fatalf("a conservative context compiled %d page set(s)", n)
	}
}

// Extracting from a capture is only equivalent to extracting from disk if every
// content input is in the capture. A missing one has to fail, because a page
// that silently yields nothing is indistinguishable from one whose facts were
// legitimately removed, and that difference decides whether its owner is
// replaced.
func TestMDScopeExtractCapturedRequiresEveryContentInput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/guide.md", "# Guide\n\nProse.\n")
	md := mdintent.New()
	if _, err := md.ExtractCaptured(context.Background(), root, []string{"docs/guide.md"}, map[string][]byte{}); err == nil {
		t.Fatal("extracting a page that is not in the capture must fail")
	}
}
