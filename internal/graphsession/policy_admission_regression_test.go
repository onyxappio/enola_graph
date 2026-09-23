package graphsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/engine"
	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// These cases are about the difference between the Git index and the graph input
// policy. Staging a file, or untracking one, rewrites the index and moves the raw
// policy identity, but it moves an admission decision only for the names Git
// ignores: those are the only ones the tracked set reaches in Classify. A run
// that treats every index edit as a policy change re-Begins the whole domain for
// a `git add` that changed nothing, which is what the admission fingerprint
// exists to stop.
//
// So each case below pins both halves. A membership edit that moves no decision
// must publish nothing at all - no parse, no event, the same completed
// generation - and a membership edit that does move one must still reconcile,
// still retire what it retires, and still agree exactly with a cold session.

func admissionGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// admissionRepo is a small TypeScript tree with a barrel re-export, inside a Git
// repository with nothing staged yet, so a case can choose what tracking means.
func admissionRepo(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"src/a.ts":      "export function a(){ return 1; }\n",
		"src/barrel.ts": "export { a } from './a';\n",
		"src/use.ts":    "import { a } from './barrel'; export function use(){ return a(); }\n",
		"src/solo.ts":   "export const solo = 7;\n",
	}
	for rel, body := range extra {
		files[rel] = body
	}
	dir := setupTSRepo(t, files)
	admissionGit(t, dir, "init", "-q")
	return dir
}

// admissionEngine is frozenPolicyEngine with the policy options a case needs, so
// the Enola exclusion list can be exercised against the gitignore rules.
func admissionEngine(t *testing.T, dir string, popts graphinput.Options) *engine.Engine {
	t.Helper()
	var attach func(*engine.Engine) *engine.Engine
	attach = func(eng *engine.Engine) *engine.Engine {
		p, err := graphinput.Build(dir, popts)
		if err != nil {
			t.Fatal(err)
		}
		eng.ConfigureGraphInputs(&inputscope.Scope{Root: dir, Policy: p}, func() (*engine.Engine, error) {
			return attach(testEngine(t, dir)), nil
		})
		return eng
	}
	return attach(testEngine(t, dir))
}

// admissionEngineHook runs onBuilt immediately after the policy snapshot the
// transaction plans with is taken. Run rebuilds the graph inputs inside the
// transaction - the second build, the first being this constructor - so this is
// the only place a mutation can land strictly after that snapshot and strictly
// before any decision is taken from it.
func admissionEngineHook(t *testing.T, dir string, popts graphinput.Options, onBuilt func()) *engine.Engine {
	t.Helper()
	builds := 0
	var attach func(*engine.Engine) *engine.Engine
	attach = func(eng *engine.Engine) *engine.Engine {
		p, err := graphinput.Build(dir, popts)
		if err != nil {
			t.Fatal(err)
		}
		if builds++; builds == 2 {
			onBuilt()
		}
		eng.ConfigureGraphInputs(&inputscope.Scope{Root: dir, Policy: p}, func() (*engine.Engine, error) {
			return attach(testEngine(t, dir)), nil
		})
		return eng
	}
	return attach(testEngine(t, dir))
}

func admissionPolicy(t *testing.T, dir string, popts graphinput.Options) *graphinput.Policy {
	t.Helper()
	p, err := graphinput.Build(dir, popts)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// admissionRun runs one session against a fresh sink and folds it into cons.
func admissionRun(t *testing.T, eng *engine.Engine, dir string, opts Options, cons *Consumer) (*Result, *graphstream.MemorySink) {
	t.Helper()
	sink := &graphstream.MemorySink{}
	res, err := Run(context.Background(), eng, dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cons != nil {
		if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
			t.Fatal(err)
		}
	}
	return res, sink
}

// assertNoPublication is the whole contract of a membership edit that moved no
// decision: nothing parsed, nothing published, the completed generation still
// where the previous run left it.
func assertNoPublication(t *testing.T, res *Result, sink *graphstream.MemorySink, base int64, what string) {
	t.Helper()
	if res.ParsedFiles != 0 {
		t.Fatalf("%s parsed %d files", what, res.ParsedFiles)
	}
	if got := len(sink.CloneRecords()); got != 0 {
		t.Fatalf("%s published %d events", what, got)
	}
	if res.TargetGeneration != base {
		t.Fatalf("%s advanced the generation %d -> %d", what, base, res.TargetGeneration)
	}
	if res.Invalidation.PolicyReconciled {
		t.Fatalf("%s was treated as a policy reconciliation", what)
	}
}

// Pure staging and pure untracking of already-admitted, unignored, unchanged
// files must cost nothing at all, while still being a real probe: the raw policy
// identity has to move across each step, or the case would pass by not testing
// anything.
func TestAdmissionPureStagingAndUntrackingPublishNothing(t *testing.T) {
	dir := admissionRepo(t, nil)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if first.ParsedFiles == 0 {
		t.Fatal("the initial run parsed nothing")
	}
	prev, _ := committedGeneration(t, opts.StateDir)

	for _, step := range []struct {
		what string
		args []string
	}{
		{"staging every source", []string{"add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts"}},
		{"untracking two sources", []string{"rm", "--cached", "-q", "src/a.ts", "src/solo.ts"}},
	} {
		admissionGit(t, dir, step.args...)
		res, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
		assertNoPublication(t, res, sink, prev.Generation, step.what)
		st, _ := committedGeneration(t, opts.StateDir)
		if st.PolicyIdentity == prev.PolicyIdentity {
			t.Fatalf("%s did not move the raw policy identity, so this case proves nothing", step.what)
		}
		if st.PolicyAdmissionIdentity != prev.PolicyAdmissionIdentity {
			t.Fatalf("%s moved the admission identity %s -> %s", step.what, prev.PolicyAdmissionIdentity, st.PolicyAdmissionIdentity)
		}
		if st.Generation != prev.Generation {
			t.Fatalf("%s advanced the committed generation %d -> %d", step.what, prev.Generation, st.Generation)
		}
		prev = st
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// Adding a source is the same amount of work whether or not it is staged: the
// index does not decide admission for a file Git does not ignore. Both runs must
// parse the one new file and freeze the same Begin scope.
func TestAdmissionStagedAndUntrackedAdditionsAreEquivalent(t *testing.T) {
	run := func(stage bool) (*Result, []string) {
		t.Helper()
		dir := admissionRepo(t, nil)
		admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
		opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
		cons := NewConsumer()
		admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
		writeRepoFile(t, dir, "src/fresh.ts", "export const fresh = 1;\n")
		if stage {
			admissionGit(t, dir, "add", "src/fresh.ts")
		}
		res, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
		owners, ids := beginScope(t, sink)
		requireOwners(t, owners, ids, "src/fresh.ts")
		requireNoWholeDomainFallback(t, res, ids)
		assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
		return res, ids
	}
	// Each repository is judged cold-exact inside run; what is compared across
	// the two is the plan, which is the part staging could have changed. The
	// published facts themselves carry a repository identity derived from the
	// path, so they are not comparable between two different trees.
	loose, looseIDs := run(false)
	staged, stagedIDs := run(true)
	if loose.ParsedFiles != 1 || staged.ParsedFiles != 1 {
		t.Fatalf("addition parsed untracked=%d staged=%d, want 1 each", loose.ParsedFiles, staged.ParsedFiles)
	}
	if !reflect.DeepEqual(looseIDs, stagedIDs) {
		t.Fatalf("staging changed the Begin scope\n untracked=%v\n   staged=%v", looseIDs, stagedIDs)
	}
}

// The one membership edit that does move a decision: a file Git ignores is
// admitted only while it is tracked. Both directions have to reconcile, and the
// retiring direction has to clear the owner it leaves behind.
func TestAdmissionTrackedIgnoredFileAdmitsAndRetires(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore": "gen/\n",
		"gen/out.ts": "export const gen = 5;\n",
	})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, firstSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	owners, ids := beginScope(t, firstSink)
	forbidOwners(t, owners, ids, "gen/out.ts")
	base := first.TargetGeneration

	admissionGit(t, dir, "add", "-f", "gen/out.ts")
	admitted, admittedSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if !admitted.Invalidation.PolicyReconciled {
		t.Fatalf("tracking an ignored file did not reconcile the policy: %+v", admitted.Invalidation)
	}
	if admitted.TargetGeneration != base+1 {
		t.Fatalf("admission left the generation at %d", admitted.TargetGeneration)
	}
	owners, ids = beginScope(t, admittedSink)
	requireOwners(t, owners, ids, "gen/out.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))

	admissionGit(t, dir, "rm", "--cached", "-q", "gen/out.ts")
	retired, retiredSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if !retired.Invalidation.PolicyReconciled {
		t.Fatalf("untracking an ignored file did not reconcile the policy: %+v", retired.Invalidation)
	}
	owners, ids = beginScope(t, retiredSink)
	requireOwners(t, owners, ids, "gen/out.ts")
	// Cold equality is what proves the retirement: the oracle never saw the
	// file at all, so any surviving fact from it fails here.
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// A nested negation re-includes a file its parent's rules ignore, and that
// decision belongs to the .gitignore bytes, which the admission fingerprint
// carries as a policy dependency. Tracking the sibling the negation did not
// rescue is a genuine admission change and must reconcile.
func TestAdmissionNestedNegationSurvivesAndTrackingItsSiblingReconciles(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":       "*.gen.ts\n",
		"libs/.gitignore":  "!keep.gen.ts\n",
		"libs/keep.gen.ts": "export const keep = 1;\n",
		"libs/drop.gen.ts": "export const drop = 2;\n",
		"libs/plain.ts":    "export const plain = 3;\n",
	})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, firstSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	owners, ids := beginScope(t, firstSink)
	requireOwners(t, owners, ids, "libs/keep.gen.ts", "libs/plain.ts")
	forbidOwners(t, owners, ids, "libs/drop.gen.ts")

	admissionGit(t, dir, "add", "-f", "libs/drop.gen.ts")
	res, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if !res.Invalidation.PolicyReconciled {
		t.Fatalf("tracking the ignored sibling did not reconcile: %+v", res.Invalidation)
	}
	if res.TargetGeneration != first.TargetGeneration+1 {
		t.Fatalf("admission left the generation at %d", res.TargetGeneration)
	}
	owners, ids = beginScope(t, sink)
	requireOwners(t, owners, ids, "libs/drop.gen.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// An Enola exclusion and a lockfile are decided before the tracked override is
// ever consulted, so their index membership can never move a decision. Staging
// them, and rewriting the lockfile's bytes, must all publish nothing.
func TestAdmissionLockfileAndExcludedArtifactMembershipPublishesNothing(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":        "dist/\n",
		"dist/bundle.ts":    "export const bundle = 1;\n",
		"package-lock.json": "{\"lockfileVersion\":3}\n",
	})
	popts := graphinput.Options{Exclude: []string{"dist/**"}}
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, firstSink := admissionRun(t, admissionEngine(t, dir, popts), dir, opts, cons)
	owners, ids := beginScope(t, firstSink)
	forbidOwners(t, owners, ids, "dist/bundle.ts", "package-lock.json")
	prev, _ := committedGeneration(t, opts.StateDir)
	baseline := admissionPolicy(t, dir, popts)
	distBefore := baseline.Classify(filepath.Join(dir, "dist"), true)

	for _, step := range []struct {
		what string
		do   func()
	}{
		{"staging the lockfile", func() { admissionGit(t, dir, "add", "package-lock.json") }},
		{"staging an excluded artifact", func() { admissionGit(t, dir, "add", "-f", "dist/bundle.ts") }},
		{"rewriting the lockfile", func() { writeRepoFile(t, dir, "package-lock.json", "{\"lockfileVersion\":3,\"n\":1}\n") }},
		{"untracking the excluded artifact", func() { admissionGit(t, dir, "rm", "--cached", "-q", "dist/bundle.ts") }},
	} {
		step.do()
		res, sink := admissionRun(t, admissionEngine(t, dir, popts), dir, opts, cons)
		assertNoPublication(t, res, sink, first.TargetGeneration, step.what)
		st, _ := committedGeneration(t, opts.StateDir)
		if st.PolicyAdmissionIdentity != prev.PolicyAdmissionIdentity {
			t.Fatalf("%s moved the admission identity of a hard-excluded path", step.what)
		}
		if st.Generation != prev.Generation {
			t.Fatalf("%s advanced the committed generation", step.what)
		}
		prev = st
		// The parent is the case the digest narrowing has to answer for: the
		// only tracked descendant dist ever gains is hard-excluded, so the
		// directory query must answer the same as it did before the artifact
		// was staged. Classify is asserted directly because the admission
		// digest claims to stand for it, and an equal digest over a different
		// decision would be the failure worth catching.
		now := admissionPolicy(t, dir, popts)
		if d := now.Classify(filepath.Join(dir, "dist"), true); d.Kind != distBefore.Kind {
			t.Fatalf("%s moved the directory decision for dist: %+v then %+v", step.what, distBefore, d)
		}
		if now.AdmissionIdentity() != baseline.AdmissionIdentity() {
			t.Fatalf("%s moved the admission identity of a hard-excluded path", step.what)
		}
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, popts), dir))
}

// A state written before admission was fingerprinted carries no evidence about
// which rules produced it, so the first run over it reconciles conservatively -
// and the run after that is quiet again.
func TestAdmissionStateWithoutAdmissionFingerprintReconciles(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)

	st, err := loadCommittedState(opts.StateDir)
	if err != nil || st == nil {
		t.Fatalf("state: %v %+v", err, st)
	}
	if st.PolicyAdmissionIdentity == "" {
		t.Fatal("the run did not record an admission fingerprint at all")
	}
	st.PolicyAdmissionIdentity = ""
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}

	migrated, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if !migrated.Invalidation.PolicyReconciled {
		t.Fatalf("a state with no admission fingerprint was adopted without reconciling: %+v", migrated.Invalidation)
	}
	if migrated.TargetGeneration != first.TargetGeneration+1 {
		t.Fatalf("the migrating run published nothing: generation %d", migrated.TargetGeneration)
	}
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))

	quiet, quietSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	assertNoPublication(t, quiet, quietSink, migrated.TargetGeneration, "the run after the migration")
}

// The raw identity is still stored, and a stale one still has to be repaired -
// but repairing bookkeeping is not publishing. The generation must not move and
// the stored value must end up at the policy's real identity, or every later run
// would recompute the same difference forever.
func TestAdmissionStaleRawIdentityRefreshesBookkeepingWithoutPublishing(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)

	st, err := loadCommittedState(opts.StateDir)
	if err != nil || st == nil {
		t.Fatalf("state: %v %+v", err, st)
	}
	st.PolicyIdentity = "stale-policy"
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}

	res, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	assertNoPublication(t, res, sink, first.TargetGeneration, "a stale raw policy identity")
	after, _ := committedGeneration(t, opts.StateDir)
	if after.PolicyIdentity != admissionPolicy(t, dir, graphinput.Options{}).Identity() {
		t.Fatalf("bookkeeping was not refreshed: stored %q", after.PolicyIdentity)
	}
	if after.Generation != st.Generation {
		t.Fatalf("the refresh advanced the committed generation %d -> %d", st.Generation, after.Generation)
	}
}

// The bookkeeping refresh writes a fingerprint for a policy this run planned
// with, so the index moving underneath that policy has to stop it. Here the
// index moves strictly after the snapshot the transaction plans with is taken:
// the run's own input fence catches it first and refuses outright, which is the
// stronger of the two outcomes - nothing published, the completed state not
// rewritten at all - and the next clean run repairs the bookkeeping instead.
//
// The recheck inside the refresh covers the remainder of the same window, after
// that fence has run and before the save, and it is not exercised here: this
// case pins the outer behaviour a caller can observe.
func TestAdmissionIndexMovingUnderThePolicySnapshotRefusesAndRecovers(t *testing.T) {
	dir := admissionRepo(t, nil)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	settled, settledBytes := committedGeneration(t, opts.StateDir)

	admissionGit(t, dir, "add", "src/a.ts", "src/barrel.ts")
	raced := admissionEngineHook(t, dir, graphinput.Options{}, func() {
		admissionGit(t, dir, "add", "src/use.ts")
	})
	racedSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), raced, dir, racedSink, opts); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("an index move under the policy snapshot was not caught: %v", err)
	}
	if got := len(racedSink.CloneRecords()); got != 0 {
		t.Fatalf("the refused run published %d events", got)
	}
	if _, bytes := committedGeneration(t, opts.StateDir); bytes != settledBytes {
		t.Fatal("the refused run rewrote the completed state")
	}

	repaired, repairedSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	assertNoPublication(t, repaired, repairedSink, first.TargetGeneration, "the run after the refusal")
	after, _ := committedGeneration(t, opts.StateDir)
	if after.PolicyIdentity != admissionPolicy(t, dir, graphinput.Options{}).Identity() {
		t.Fatalf("the bookkeeping never recovered: stored %q", after.PolicyIdentity)
	}
	if after.Generation != settled.Generation {
		t.Fatalf("the race advanced the committed generation %d -> %d", settled.Generation, after.Generation)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// Renames and deletes move both the index and the tree. They still have to plan
// from the membership diff rather than the whole domain, and still have to agree
// exactly with a cold session once the barrel is rebound.
func TestAdmissionRenameAndDeleteStayBoundedAndColdExact(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)

	admissionGit(t, dir, "mv", "src/a.ts", "src/renamed.ts")
	writeRepoFile(t, dir, "src/barrel.ts", "export { a } from './renamed';\n")
	admissionGit(t, dir, "add", "src/barrel.ts")
	res, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "src/a.ts", "src/renamed.ts", "src/barrel.ts")
	forbidOwners(t, owners, ids, "src/solo.ts")
	requireNoWholeDomainFallback(t, res, ids)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))

	if err := os.Remove(filepath.Join(dir, "src", "renamed.ts")); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "src/barrel.ts", "export const a = 0;\n")
	admissionGit(t, dir, "rm", "--cached", "-q", "src/renamed.ts")
	res, sink = admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	owners, ids = beginScope(t, sink)
	requireOwners(t, owners, ids, "src/renamed.ts", "src/barrel.ts")
	forbidOwners(t, owners, ids, "src/solo.ts")
	requireNoWholeDomainFallback(t, res, ids)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// The resident sees staging as a `.git/index` event, which is a reconciliation
// request: it rebuilds the policy and runs the full transaction. That is the
// path a watch session actually takes, and it has to reach the same conclusion
// as the CLI - nothing to publish for a pure index edit, a real reconciliation
// for one that admits a file.
func TestAdmissionResidentStagingPublishesNothingButStillAdmits(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore": "gen/\n",
		"gen/out.ts": "export const gen = 5;\n",
	})
	sink := &graphstream.MemorySink{}
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	r, err := OpenSession(context.Background(), admissionEngine(t, dir, graphinput.Options{}), dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	watermark := uint64(0)
	apply := func(paths ...string) *OnlineResult {
		t.Helper()
		res, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: "admission", From: watermark, Through: watermark + 1, Covered: true, Paths: paths})
		watermark++
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	index := filepath.Join(dir, ".git", "index")
	base := apply().TargetGeneration

	for _, step := range []struct {
		what string
		args []string
	}{
		{"staging every source", []string{"add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts"}},
		{"untracking one source", []string{"rm", "--cached", "-q", "src/solo.ts"}},
	} {
		admissionGit(t, dir, step.args...)
		before := len(sink.CloneRecords())
		res := apply(index)
		if res.ParsedFiles != 0 {
			t.Fatalf("resident %s parsed %d files", step.what, res.ParsedFiles)
		}
		if got := len(sink.CloneRecords()); got != before {
			t.Fatalf("resident %s published %d events", step.what, got-before)
		}
		if res.TargetGeneration != base {
			t.Fatalf("resident %s advanced the generation %d -> %d", step.what, base, res.TargetGeneration)
		}
	}

	admissionGit(t, dir, "add", "-f", "gen/out.ts")
	admitted := apply(index)
	if admitted.TargetGeneration != base+1 || admitted.ParsedFiles == 0 {
		t.Fatalf("resident admission of a tracked ignored file published nothing: %+v", admitted.Result)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// The policy the transaction plans with is a snapshot, so the resident reads its
// declared inputs back before it will call a replacement successful. An index
// edit that lands while the transaction is publishing therefore fails the run
// with the completed state untouched, and a later reconciliation recovers.
func TestAdmissionIndexEditAfterBeginFailsTheResidentRun(t *testing.T) {
	// Two mutations, because they fail for different reasons and both have to
	// fail. Staging an ordinary source moves the raw identity only: the
	// admitted tree is the same afterwards, and the fence still has to refuse
	// because the index it declared as an input moved under the transaction.
	// Force-adding a gitignored file moves the admission identity itself - the
	// decision function is different afterwards - so a run that ended here
	// would publish a manifest for a tree the policy no longer describes.
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, dir string)
	}{
		{"raw identity only", func(t *testing.T, dir string) { admissionGit(t, dir, "add", "src/a.ts") }},
		{"semantic admission", func(t *testing.T, dir string) { admissionGit(t, dir, "add", "-f", "gen/tracked.ts") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := admissionRepo(t, map[string]string{
				".gitignore":     "gen/\n",
				"gen/tracked.ts": "export const tracked = 1;\n",
			})
			sink := &mdScopeBeginSink{}
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
			r, err := OpenSession(context.Background(), admissionEngine(t, dir, graphinput.Options{}), dir, sink, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			watermark := uint64(0)
			apply := func(paths ...string) (*OnlineResult, error) {
				res, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: "admission", From: watermark, Through: watermark + 1, Covered: true, Paths: paths})
				watermark++
				return res, err
			}
			if _, err := apply(); err != nil {
				t.Fatal(err)
			}
			kept, keptBytes := committedGeneration(t, opts.StateDir)
			published := len(sink.CloneRecords())

			// Armed only now: the bootstrap above published a Begin of its own,
			// and the helper fires once, so arming it earlier would spend the
			// mutation on the initial apply instead of the delta.
			sink.onBegin = func() { tc.mutate(t, dir) }
			writeRepoFile(t, dir, "src/added.ts", "export const added = 1;\n")
			if _, err := apply(filepath.Join(dir, "src", "added.ts")); !errors.Is(err, ErrInputsChanged) {
				t.Fatalf("an index edit during the transaction was not caught: %v", err)
			}
			// Only what this run published: the bootstrap above ended
			// successfully and is supposed to have.
			for _, rec := range sink.CloneRecords()[published:] {
				var e graphstream.EndReplace
				if json.Unmarshal(rec.Payload, &e); e.Type == graphstream.TypeEndReplace && e.Completeness.Status == "success" {
					t.Fatalf("the refused run published a successful EndReplace (%s)", rec.MsgID)
				}
			}
			if st, bytes := committedGeneration(t, opts.StateDir); bytes != keptBytes {
				t.Fatalf("the refused run rewrote the completed state: generation %d then %d", kept.Generation, st.Generation)
			}

			sink.onBegin = nil
			recovered, err := apply(filepath.Join(dir, "src", "added.ts"))
			if err != nil {
				t.Fatalf("the session did not recover: %v", err)
			}
			if recovered.TargetGeneration <= kept.Generation {
				t.Fatalf("the recovery published nothing: generation %d", recovered.TargetGeneration)
			}
			// The recovery has to agree with a policy built after the mutation,
			// not merely succeed: for the semantic case that means the
			// force-added ignored file is admitted by the run that recovers.
			cons := NewConsumer()
			if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
				t.Fatal(err)
			}
			assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
		})
	}
}
