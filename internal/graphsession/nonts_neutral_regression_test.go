package graphsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

// A run asks two questions about a non-TypeScript extractor it needs: whether
// its output moved at all, and - when it did - which owners its candidate names
// retarget. Both are answered by snapshotting that extractor's inputs and
// extracting from the snapshot, so a run that asks them separately reads the
// tree twice and fences two captures of the same bytes. The counter is the
// guard: it fails if the candidate-scope preview stops reusing the proof's
// extraction, which is invisible in graph output and would otherwise only show
// up as wall time.
func TestNeutralProofAndCandidateScopeShareOneCapture(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	r, err := OpenSession(context.Background(), eng, root, &graphstream.MemorySink{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "initial"}); err != nil {
		t.Fatal(err)
	}

	// A real dependency edit: the manifest's output moves, so the run publishes
	// and needs the candidate delta as well as the neutrality answer.
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.0","dependencies":{"left-pad":"^1.0.0"}}`)
	changed, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "dependency"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Result.OwnersPublished == 0 {
		t.Fatal("a dependency edit published nothing")
	}
	if changed.Work.NonTSCaptures != 1 {
		t.Fatalf("a publishing run made %d fenced capture(s) of the manifest inputs, want exactly 1 shared between the neutrality proof and the candidate scope", changed.Work.NonTSCaptures)
	}

	// A version-only edit on top: the proof answers the only question left, and
	// there is no second question to ask because nothing is published.
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.1","dependencies":{"left-pad":"^1.0.0"}}`)
	neutral, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "version"})
	if err != nil {
		t.Fatal(err)
	}
	if neutral.Result.OwnersPublished != 0 || neutral.Result.ParsedFiles != 0 {
		t.Fatalf("a version-only edit published %d owner(s) and parsed %d file(s)", neutral.Result.OwnersPublished, neutral.Result.ParsedFiles)
	}
	if neutral.Work.NonTSCaptures != 1 {
		t.Fatalf("a graph-neutral edit made %d fenced capture(s), want exactly 1", neutral.Work.NonTSCaptures)
	}

	// And a true no-op raises no need at all, so there is nothing to capture.
	idle, err := r.ApplyChanges(context.Background(), ChangeBatch{Reconcile: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	if idle.Work.NonTSCaptures != 0 {
		t.Fatalf("an unchanged tree made %d fenced capture(s), want none", idle.Work.NonTSCaptures)
	}
}

// Recording what a run observed is a commit, even when it publishes nothing:
// the next run trusts those hashes and will not look again. So it goes behind
// the same fence a publication does - the captured bytes read back, and every
// preview's own derivation re-proved - and a tree that moved underneath has to
// refuse rather than write down an observation of a revision that is gone.
func TestNeutralObservationCommitsOnlyUnderTheCapturedInputFence(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.0"}`)
	body, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}

	s := &session{abs: root, capturedSources: map[string][]byte{"package.json": append([]byte(nil), body...)}}
	if err := s.revalidateCapturedInputs("refusing to record the run's observations"); err != nil {
		t.Fatalf("an unmoved tree failed its own fence: %v", err)
	}
	if s.work.CapturedReads != 1 {
		t.Fatalf("the fence read %d captured source(s), want 1", s.work.CapturedReads)
	}

	writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.1"}`)
	err = s.revalidateCapturedInputs("refusing to record the run's observations")
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("bytes edited under the run ended the fence with %v, want ErrInputsChanged", err)
	}

	// A source the run captured and can no longer read is the same refusal: an
	// observation of a file that is not there is not an observation.
	if err := os.Remove(filepath.Join(root, "package.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.revalidateCapturedInputs("refusing to record the run's observations"); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a captured source that vanished ended the fence with %v, want ErrInputsChanged", err)
	}

	// The per-file re-read is only half of it. An extractor whose inputs are an
	// enumeration rather than a fixed set re-derives its own digest, and that
	// refusal has to reach the caller unchanged.
	writeRepoFile(t, root, "package.json", `{"name":"app","version":"1.0.1"}`)
	moved := fmt.Errorf("%w: manifest context changed during the run; refusing successful EndReplace", ErrInputsChanged)
	s = &session{abs: root, previewFences: []func() error{func() error { return moved }}}
	if err := s.revalidateCapturedInputs("refusing to record the run's observations"); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a preview whose own derivation moved ended the fence with %v, want ErrInputsChanged", err)
	}
}

// countingNotes counts the live extractions its extractor performs, which the
// shared fixture cannot: a fenced capture and a read of the live tree both end
// in facts, and only the second is a tree the frozen plan has no fence over.
type countingNotes struct {
	capturingStub
	live *int
}

func (c countingNotes) Extract(ctx context.Context, repo string, files []string) ([]facts.Fact, error) {
	if c.live != nil {
		*c.live++
	}
	return c.capturingStub.stubExtractor.Extract(ctx, repo, files)
}

// The need for a non-TypeScript extractor is raised from input hashes, and the
// proof that discharges it is made from a fenced capture. The extraction site
// used to ask the hash question again, get the same yes - the bytes on disk
// really are not the bytes the state records - and re-extract from the live
// tree after the plan was frozen. That is how an input edited since the capture
// reached the graph as an owner the frozen manifest does not carry, and the run
// died on the scope audit rather than on the fence that owns the refusal.
//
// So an owner-declaring extractor whose output is proven unchanged extracts
// exactly once, from the capture, and never from the tree. Its untouched owners
// stay out of the frozen manifest, while a file whose own bytes moved is still
// replaced - from the contribution the state already holds, which the proof says
// is current.
func TestProvenNeutralExtractorNeitherRereadsTheTreeNorSeedsUntouchedOwners(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"docs/guide.md": "# Guide\n\nThe source is [a](../src/a.ts).\n",
		"notes/one.txt": "one\n",
		"notes/two.txt": "two\n",
	})
	var captured, live int
	notes := countingNotes{capturingStub: capturingStub{
		stubExtractor: stubExtractor{name: "txtnotes", detect: true, suffix: ".txt",
			fact: func(rel string) []facts.Fact {
				return []facts.Fact{{Kind: facts.KindSymbol, Name: "note:" + rel, File: rel}}
			}},
		extracted: &captured,
	}, live: &live}
	eng := multiEngine(t, root, mdintent.New(), notes)
	opts := mdScopeOpts(t)
	cons := mdScopeInitial(t, eng, root, opts)

	// A note's bytes move but its facts are named from the file, so this
	// extractor's whole output is the output the stored state carries. The
	// source add is what gives the run something to publish at all.
	writeFile(t, root, "src/added.ts", "export const added = 1;\n")
	writeFile(t, root, "notes/one.txt", "one edited\n")
	captured, live = 0, 0
	sink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, sink, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	if live != 0 {
		t.Fatalf("a proven-neutral extractor read the live tree %d time(s) after the plan was frozen", live)
	}
	if captured != 1 {
		t.Fatalf("the proof compiled %d time(s) from its capture, want exactly one", captured)
	}
	owners, ids := beginScope(t, sink)
	// The edited note's own owner is replaced like any other changed file; what
	// the proof removes is the seed of the rest of the extractor's domain.
	requireOwners(t, owners, ids, "src/added.ts", "notes/one.txt")
	forbidOwners(t, owners, ids, "notes/two.txt", "docs/guide.md")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
