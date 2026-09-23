package graphsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/extractors/manifestextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// A non-TypeScript extractor publishes facts whose names the graph resolver
// indexes without a kind filter, so its output decides where references in
// other files resolve. Nothing in the file ownership of that extractor says so,
// and no import edge connects the two: a manifest that starts, stops or
// ambiguously declares a package retargets sources that no consumer reports as
// changed. The frozen Begin has to carry those owners, which is what the
// pre-Begin candidate closure is for.
//
// Every case here is judged against a cold session, because a bounded Begin
// that missed one of those owners still produces a graph, just the wrong one.

// candidateScopeRepo is configScopeRepo plus a source whose specifier is the
// package URL a manifest dependency declares. The import resolves only while
// the dependency exists, and src/use.ts never changes in any of these cases.
func candidateScopeRepo(t *testing.T) string {
	t.Helper()
	root := configScopeRepo(t)
	writeRepoFile(t, root, "src/use.ts", "import x from 'pkg:npm/left-pad';\nexport const used = x;\n")
	return root
}

const (
	pkgNoDep   = `{"name":"app","type":"module","version":"0.7.1"}`
	pkgWithDep = `{"name":"app","type":"module","version":"0.7.1","dependencies":{"left-pad":"^1.0.0"}}`
)

// Adding a dependency makes an unchanged source's specifier resolve for the
// first time. This is the failure the candidate closure was written for.
func TestNonTSCandidateAdditionCoversUnchangedResolutionOwner(t *testing.T) {
	root := candidateScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", pkgWithDep)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "package.json", "src/use.ts")
	forbidOwners(t, owners, ids, "src/alone.ts")
	if why := wholeDomainFallback(res); why != "" {
		t.Fatalf("a dependency addition fell back to the whole domain: %s", why)
	}
	if res.ParsedFiles != 0 {
		t.Fatalf("the newly resolving source was reparsed %d time(s); its bytes did not change", res.ParsedFiles)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// Removing it is the same transition backwards: the specifier stops resolving,
// so the source that still imports it has to be republished unresolved.
func TestNonTSCandidateRemovalCoversUnchangedResolutionOwner(t *testing.T) {
	root := candidateScopeRepo(t)
	writeRepoFile(t, root, "package.json", pkgWithDep)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", pkgNoDep)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "package.json", "src/use.ts")
	if why := wholeDomainFallback(res); why != "" {
		t.Fatalf("a dependency removal fell back to the whole domain: %s", why)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// The manifest extractor publishes one candidate per package URL, so a name
// declared by two modules is emitted once, attributed to the manifest that wins
// the deduplication. Dropping it from that manifest therefore neither adds nor
// removes the name: it moves the candidate to the other module, retargeting the
// resolution edge of an unchanged source while the set of declared names stays
// exactly as it was. A closure that compared names alone would see nothing
// here, which is why it compares the candidates the names resolve to.
func TestNonTSCandidateRelocationBetweenModulesCoversResolutionOwner(t *testing.T) {
	root := candidateScopeRepo(t)
	writeRepoFile(t, root, "package.json", pkgWithDep)
	if err := os.MkdirAll(filepath.Join(root, "pkgs/second"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "pkgs/second/package.json", `{"name":"second","type":"module","version":"1.0.0","dependencies":{"left-pad":"^1.0.0"}}`)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	if owner := candidateOwner(t, cons, "pkg:npm/left-pad"); owner != "package.json" {
		t.Fatalf("the deduplicated candidate starts at %q, want package.json", owner)
	}

	writeRepoFile(t, root, "package.json", pkgNoDep)
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)

	requireOwners(t, owners, ids, "package.json", "src/use.ts")
	if why := wholeDomainFallback(res); why != "" {
		t.Fatalf("a relocated candidate fell back to the whole domain: %s", why)
	}
	if owner := candidateOwner(t, cons, "pkg:npm/left-pad"); owner != "pkgs/second/package.json" {
		t.Fatalf("the candidate did not move to the surviving module: owner %q", owner)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// The closure plans from a snapshot the run fingerprinted, not from the tree,
// and that difference is only real if the facts it plans with are compiled from
// the snapshot's bytes. Edit-and-restore is the case that tells them apart: a
// second read of the tree would agree with the first and prove nothing.
func TestManifestExtractCapturedUsesTheSnapshotNotTheTree(t *testing.T) {
	root := candidateScopeRepo(t)
	writeRepoFile(t, root, "package.json", pkgWithDep)
	p, err := graphinput.Build(root, graphinput.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ext := manifestextractor.NewGraph(&inputscope.Scope{Root: root, Policy: p})

	src, sum := ext.CaptureContext(root)
	if _, ok := src["package.json"]; !ok {
		t.Fatalf("the capture does not contain the manifest it enumerated: %v", keysOf(src))
	}
	// A transient edit, live on disk while the extraction runs.
	writeRepoFile(t, root, "package.json", pkgNoDep)
	captured, err := ext.ExtractCaptured(context.Background(), root, []string{"package.json"}, src)
	if err != nil {
		t.Fatal(err)
	}
	if !declaresCandidate(captured, "pkg:npm/left-pad") {
		t.Fatalf("the extraction followed the edited tree instead of the snapshot: %v", candidateNames(captured))
	}
	// The digest moved with the tree, so the session's fence catches the edit
	// even though the extraction itself was unaffected by it.
	if now := ext.DeltaContext(root); now == sum {
		t.Fatal("the context digest did not move with the edited manifest, so no fence could see it")
	}
	// Restoring the bytes restores the digest: the fence is a statement about
	// the inputs, not about how many times they were written.
	writeRepoFile(t, root, "package.json", pkgWithDep)
	if now := ext.DeltaContext(root); now != sum {
		t.Fatalf("restored bytes produced a different context digest %s, want %s", now, sum)
	}
	live, err := ext.Extract(context.Background(), root, []string{"package.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !declaresCandidate(live, "pkg:npm/left-pad") {
		t.Fatalf("extracting from the restored tree disagrees with the snapshot: %v", candidateNames(live))
	}
}

// The same edit seen from the session: the closure planned from the snapshot,
// and the manifest it planned from is no longer on disk when the run tries to
// commit. It must refuse, leave the completed generation untouched, and then
// succeed once the tree stops moving.
func TestNonTSPreviewedContextEditedAfterBeginFailsTheRunAndRecovers(t *testing.T) {
	root := candidateScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	kept, before := committedGeneration(t, opts.StateDir)

	writeRepoFile(t, root, "package.json", pkgWithDep)
	edited := &mdScopeBeginSink{onBegin: func() {
		writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.1","dependencies":{"left-pad":"^1.0.0","right-pad":"^2.0.0"}}`)
	}}
	if _, err := Run(context.Background(), eng, root, edited, opts); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a manifest edited after the plan ended the run with %v, want ErrInputsChanged", err)
	}
	forbidSuccessfulEnd(t, &edited.MemorySink)
	now, after := committedGeneration(t, opts.StateDir)
	if after != before {
		t.Fatalf("the failed run rewrote the completed state: generation %d/%v became %d/%v",
			kept.Generation, kept.LastComplete, now.Generation, now.LastComplete)
	}
	if !now.LastComplete {
		t.Fatal("the failed run left the completed generation marked incomplete")
	}

	// Recovery: the next run sees the tree the previous one could not commit,
	// and has to converge on exactly what a cold session builds from it.
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)
	requireOwners(t, owners, ids, "package.json", "src/use.ts")
	if why := wholeDomainFallback(res); why != "" {
		t.Fatalf("the recovering run fell back to the whole domain: %s", why)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}

// A run over an untouched tree must not advance the generation at all. This is
// the check that the closure is not paying for itself by republishing: it reads
// the manifest context, finds it unchanged, and produces no transaction.
func TestNonTSCandidateClosureLeavesUnchangedRunsAsNoOps(t *testing.T) {
	root := candidateScopeRepo(t)
	writeRepoFile(t, root, "package.json", pkgWithDep)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}
	quiet := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, root, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseGeneration != res.TargetGeneration {
		t.Fatalf("an unchanged run moved generation %d to %d", res.BaseGeneration, res.TargetGeneration)
	}
	if res.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 {
		t.Fatalf("an unchanged run published: parsed=%d records=%d fallbacks=%v",
			res.ParsedFiles, len(quiet.CloneRecords()), res.Fallbacks)
	}
}

// candidateOwner returns the file that owns the node published under name.
func candidateOwner(t *testing.T, cons *Consumer, name string) string {
	t.Helper()
	var found []string
	for owner, nodes := range cons.Owners {
		for _, n := range nodes {
			if n.Name == name {
				found = append(found, owner)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("candidate %q is owned by %v, want exactly one owner", name, found)
	}
	return strings.TrimPrefix(found[0], graphstream.OwnerFile+":")
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func candidateNames(fs []facts.Fact) []string {
	var out []string
	for _, f := range fs {
		if f.Name != "" {
			out = append(out, f.Name)
		}
	}
	return out
}

func declaresCandidate(fs []facts.Fact, name string) bool {
	for _, f := range fs {
		if f.Name == name {
			return true
		}
	}
	return false
}

// The boundary is explicit rather than optimistic. An extractor that declares
// file ownership but cannot extract from a snapshot of its inputs has no
// bounded candidate delta available, so the run keeps the whole prior/current
// domain and says so. This is the conservative half of the closure, and it is
// what every owner-declaring extractor without ExtractCaptured gets today.
func TestNonTSUnfenceableExtractorKeepsTheWholeDomain(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts":      "export const a = 1;\n",
		"notes/one.txt": "one\n",
	})
	notes := stubExtractor{name: "txtnotes", detect: true, suffix: ".txt",
		fact: func(rel string) []facts.Fact {
			return []facts.Fact{{Kind: facts.KindSymbol, Name: "note:" + rel, File: rel}}
		}}
	eng := multiEngine(t, root, notes)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "src/added.ts", "export const added = 1;\n")
	res, _, _ := configScopeRun(t, eng, root, opts, cons)

	var declined string
	for _, fb := range res.Fallbacks {
		if fb.Extractor == "txtnotes" && fb.Scope == "all prior/current file owners" {
			declined = fb.Reason
		}
	}
	if declined == "" {
		t.Fatalf("an extractor that cannot be previewed did not record a whole-domain fallback: %v", res.Fallbacks)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, multiEngine(t, root, notes), root))
}

// nonTSABASink holds a mid-run edit live across the whole extraction window and
// restores it at a deterministic barrier: the first resolved-phase batch. That
// batch is published after every extractor has run and before the run reads its
// sources back, so an implementation that re-read the tree at the extraction
// site would see the transient bytes, while the comparisons that follow see the
// original ones and let the run succeed.
type nonTSABASink struct {
	graphstream.MemorySink
	onBegin    func()
	onResolved func()
	begun      sync.Once
	restored   sync.Once
	sawResolve bool
}

func (s *nonTSABASink) Publish(ctx context.Context, subject, id string, payload []byte) error {
	var p struct {
		Type  string `json:"type"`
		Phase string `json:"phase"`
	}
	json.Unmarshal(payload, &p)
	switch {
	case p.Type == graphstream.TypeBeginReplace && s.onBegin != nil:
		s.begun.Do(s.onBegin)
	case p.Type == graphstream.TypeBatch && p.Phase == graphstream.PhaseResolved:
		s.sawResolve = true
		if s.onResolved != nil {
			s.restored.Do(s.onResolved)
		}
	}
	return s.MemorySink.Publish(ctx, subject, id, payload)
}

// The preview extracts before Begin and the extraction site publishes what the
// preview produced. That is only worth anything if the published facts really do
// describe the snapshot: a manifest that declares a package after Begin and stops
// declaring it again before the run reads its sources back would, under a live
// re-read at the extraction site, put a package into the graph that never existed
// in any committed state of the tree and still pass every hash comparison, since
// all of them run after the restore.
//
// So the transient declaration is held live for exactly the window an extraction
// would read in, and released at the first resolved batch. The run succeeds,
// because by the time anything compares bytes the tree is what it was, and
// pkg:npm/transient must be absent from the published graph.
func TestNonTSCapturedFactsSurviveAnEditLiveAcrossExtraction(t *testing.T) {
	root := candidateScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)

	writeRepoFile(t, root, "package.json", pkgWithDep)
	aba := &nonTSABASink{
		onBegin: func() {
			writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.1","dependencies":{"left-pad":"^1.0.0","transient":"^1.0.0"}}`)
		},
		onResolved: func() { writeRepoFile(t, root, "package.json", pkgWithDep) },
	}
	if _, err := Run(context.Background(), eng, root, aba, opts); err != nil {
		t.Fatal(err)
	}
	if !aba.sawResolve {
		t.Fatal("no resolved batch was published, so the transient declaration was never held across an extraction")
	}
	if err := cons.ApplyRecords(aba.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	for owner, nodes := range cons.Owners {
		for _, n := range nodes {
			if n.Name == "pkg:npm/transient" {
				t.Fatalf("a declaration that existed only mid-run was published under %s", owner)
			}
		}
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
}
