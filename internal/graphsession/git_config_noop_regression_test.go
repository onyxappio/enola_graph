package graphsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// These cases are about the difference between the repository's Git
// configuration and the graph input policy. Writing a key into .git/config
// rewrites a file the policy declares as a dependency, and hashing those bytes
// made an unrelated key - a remote URL, a user identity, a branch's upstream,
// whatever a fetch, a gc or a credential helper writes - move the policy
// identity and republish the entire name-resolution domain with no source
// change.
//
// The replacement is a projection rather than an exemption. A policy reads Git
// through discovery, the tracked name set, and the ignore evaluation; the
// ignore evaluation runs against an isolated bare git dir with
// core.excludesFile and core.ignoreCase supplied as command-line overrides, so
// repository configuration cannot reach it, and the other two are hashed as
// their results. So these cases pin both halves: a key that moves none of those
// results must cost nothing at all, and a key that moves discovery must still
// move the identity.
//
// Nothing here names a key the implementation knows about. The keys below are
// ordinary Git settings chosen because they are unrelated to what this policy
// reads, and the case would be worthless if the fix recognised any of them.

// gitConfigNoopKeys are unrelated settings, covering a new section, an
// overwrite of an existing key, and a removal.
var gitConfigNoopKeys = []struct {
	what string
	args []string
}{
	{"an identity", []string{"config", "--local", "user.email", "someone@example.com"}},
	{"a remote URL", []string{"config", "--local", "remote.origin.url", "https://example.com/r.git"}},
	{"a branch upstream", []string{"config", "--local", "branch.main.remote", "origin"}},
	{"a signing preference", []string{"config", "--local", "commit.gpgsign", "false"}},
	{"a diff algorithm", []string{"config", "--local", "diff.algorithm", "histogram"}},
	{"an overwrite of an existing key", []string{"config", "--local", "remote.origin.url", "https://example.com/other.git"}},
	{"a removal", []string{"config", "--local", "--unset", "commit.gpgsign"}},
}

func gitConfigBytes(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The policy-level contract, stated on the two identities directly so a
// failure points at the digest rather than at a session. Each write has to
// change the configuration bytes, or the case would pass by mutating nothing.
func TestGitConfigUnrelatedKeysMoveNeitherIdentity(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":     "gen/\n",
		"gen/tracked.ts": "export const tracked = 1;\n",
	})
	admissionGit(t, dir, "add", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	admissionGit(t, dir, "add", "-f", "gen/tracked.ts")

	base := admissionPolicy(t, dir, graphinput.Options{})
	rawID, admitID := base.Identity(), base.AdmissionIdentity()
	for _, step := range gitConfigNoopKeys {
		before := gitConfigBytes(t, dir)
		admissionGit(t, dir, step.args...)
		if after := gitConfigBytes(t, dir); string(after) == string(before) {
			t.Fatalf("%s did not change .git/config at all", step.what)
		}
		p := admissionPolicy(t, dir, graphinput.Options{})
		if p.Identity() != rawID {
			t.Fatalf("%s moved the raw policy identity", step.what)
		}
		if p.AdmissionIdentity() != admitID {
			t.Fatalf("%s moved the admission identity", step.what)
		}
	}

	// The contrast, so the digests are not merely insensitive to everything:
	// the configuration file is still a declared dependency the run's own input
	// fence re-reads, and a key with a real discovery effect still moves both.
	deps := map[string]bool{}
	for _, d := range base.Dependencies() {
		deps[filepath.Clean(d.Path)] = true
	}
	if !deps[filepath.Clean(filepath.Join(dir, ".git", "config"))] {
		t.Fatal("the configuration file stopped being a declared dependency")
	}
	admissionGit(t, dir, "config", "--local", "core.bare", "true")
	bare := admissionPolicy(t, dir, graphinput.Options{})
	if bare.Identity() == rawID {
		t.Fatal("a configuration change that removes the worktree left the raw identity standing")
	}
	if bare.AdmissionIdentity() == admitID {
		t.Fatal("a configuration change that removes the worktree left the admission identity standing")
	}
}

// partitionGitDeps splits a dependency list into the entries the identities
// hash and the index entries whose bytes decide the tracked set, dropping the
// Git control files whose bytes the identities deliberately no longer read.
func partitionGitDeps(t *testing.T, dir string, deps []graphinput.Dependency) (hashed, index []graphinput.Dependency) {
	t.Helper()
	for _, d := range deps {
		base := filepath.Base(d.Path)
		if !strings.Contains(filepath.ToSlash(d.Path), "/.git/") {
			hashed = append(hashed, d)
			continue
		}
		if base == "index" {
			index = append(index, d)
		}
	}
	if len(index) == 0 {
		t.Fatal("no index dependency was declared, so the tracked-set premise cannot be checked")
	}
	return hashed, index
}

// The case that isolates the projection from everything else in the digest.
// core.worktree redirects the worktree without touching the index: the tracked
// name set comes back identical, every declared dependency the identity still
// hashes comes back identical, and the admitted subsets are identical too, so
// nothing but discovery has moved. It has moved something real - those tracked
// names now name files in a different tree - and both identities have to say
// so.
//
// This is what the projection is for. Before the fix the git dir reached the
// digest as the path of the configuration dependency; dropping that dependency
// from the identity would have taken discovery out with it, and a repository
// pointed somewhere else would have compared equal to the one it replaced.
func TestGitConfigWorktreeRedirectMovesBothIdentities(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":     "gen/\n",
		"gen/tracked.ts": "export const tracked = 1;\n",
	})
	admissionGit(t, dir, "add", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	admissionGit(t, dir, "add", "-f", "gen/tracked.ts")
	before := admissionPolicy(t, dir, graphinput.Options{})

	elsewhere := t.TempDir()
	admissionGit(t, dir, "config", "--local", "core.worktree", elsewhere)
	after := admissionPolicy(t, dir, graphinput.Options{})

	// The premise, asserted rather than assumed. The configuration file's own
	// bytes moved - that is what the write did, and the raw fence still sees it
	// - so the comparison is over everything else: every dependency the
	// identities still hash, the index whose bytes decide the tracked set, and
	// the leaf decisions the admitted subsets are drawn from. If any of those
	// had moved, this case would prove nothing about discovery.
	beforeDeps, beforeIndex := partitionGitDeps(t, dir, before.Dependencies())
	afterDeps, afterIndex := partitionGitDeps(t, dir, after.Dependencies())
	if !reflect.DeepEqual(beforeDeps, afterDeps) {
		t.Fatalf("the redirect moved a hashed dependency, so this case no longer isolates discovery:\n%v\n%v", beforeDeps, afterDeps)
	}
	if !reflect.DeepEqual(beforeIndex, afterIndex) {
		t.Fatal("the redirect moved the index, so the tracked set is not held still here")
	}
	for _, name := range []string{"src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts", "gen/tracked.ts"} {
		if before.Classify(filepath.Join(dir, name), false) != after.Classify(filepath.Join(dir, name), false) {
			t.Fatalf("the redirect moved the decision for %s, so this case no longer isolates discovery", name)
		}
	}
	if after.Identity() == before.Identity() {
		t.Fatal("a worktree redirect left the raw identity standing")
	}
	if after.AdmissionIdentity() == before.AdmissionIdentity() {
		t.Fatal("a worktree redirect left the admission identity standing")
	}
}

// Repository disappearance is the other end of the projection: it is not a
// silence, because a tree with no repository must not compare equal to the tree
// that had one. A tracked ignored file is admitted only by the index, so losing
// the repository genuinely retires it.
func TestGitConfigRepositoryDisappearanceMovesBothIdentities(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":     "gen/\n",
		"gen/tracked.ts": "export const tracked = 1;\n",
	})
	admissionGit(t, dir, "add", "src/a.ts")
	admissionGit(t, dir, "add", "-f", "gen/tracked.ts")
	before := admissionPolicy(t, dir, graphinput.Options{})
	if before.Classify(filepath.Join(dir, "gen/tracked.ts"), false).Kind == graphinput.Excluded {
		t.Fatal("the fixture never admitted its tracked ignored file")
	}

	if err := os.Rename(filepath.Join(dir, ".git"), filepath.Join(t.TempDir(), "moved.git")); err != nil {
		t.Fatal(err)
	}
	after := admissionPolicy(t, dir, graphinput.Options{})
	if after.Identity() == before.Identity() {
		t.Fatal("losing the repository left the raw identity standing")
	}
	if after.AdmissionIdentity() == before.AdmissionIdentity() {
		t.Fatal("losing the repository left the admission identity standing")
	}
	if after.Classify(filepath.Join(dir, "gen/tracked.ts"), false).Kind != graphinput.Excluded {
		t.Fatal("a tracked ignored file stayed admitted with no index to admit it")
	}
}

// A same-build initial followed by an idle reconciliation is one generation in
// total. This is the shape the defect broke: the second pass republished the
// whole domain with nothing changed.
func TestGitConfigInitialThenIdleEmitsOneGeneration(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	sink := &graphstream.MemorySink{}
	initial, err := Run(context.Background(), admissionEngine(t, dir, graphinput.Options{}), dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	begins := 0
	for _, rec := range sink.CloneRecords() {
		var b graphstream.BeginReplace
		if json.Unmarshal(rec.Payload, &b); b.Type == graphstream.TypeBeginReplace {
			begins++
		}
	}
	if begins != 1 {
		t.Fatalf("the initial analysis published %d BeginReplace records", begins)
	}

	idle, idleSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	assertNoPublication(t, idle, idleSink, initial.TargetGeneration, "an idle reconciliation over an unchanged tree")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// The fresh-CLI path: every unrelated key in turn, each followed by a complete
// new session over the committed state. Zero records, zero parses, the same
// completed generation, and the stored graph still exactly a cold one.
func TestGitConfigChangesPublishNothingOnAFreshSession(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":     "gen/\n",
		"gen/tracked.ts": "export const tracked = 1;\n",
	})
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	admissionGit(t, dir, "add", "-f", "gen/tracked.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	initial, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	settled, settledBytes := committedGeneration(t, opts.StateDir)

	for _, step := range gitConfigNoopKeys {
		admissionGit(t, dir, step.args...)
		res, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
		assertNoPublication(t, res, sink, initial.TargetGeneration, step.what)
	}
	after, afterBytes := committedGeneration(t, opts.StateDir)
	if after.Generation != settled.Generation {
		t.Fatalf("the configuration writes advanced the committed generation %d -> %d", settled.Generation, after.Generation)
	}
	if afterBytes != settledBytes {
		t.Fatal("the configuration writes rewrote the completed state")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// The resident and watch path. The configuration file is still a declared
// dependency, so the watcher still registers it and a write still arrives as a
// change to reconcile - it just has nothing to do when it gets there. The last
// step proves the session is still live: an actual tracked-ignored admission
// through the same path still reconciles and still publishes.
func TestGitConfigChangesPublishNothingInTheResident(t *testing.T) {
	dir := admissionRepo(t, map[string]string{
		".gitignore":     "gen/\n",
		"gen/tracked.ts": "export const tracked = 1;\n",
	})
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
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
		res, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: "gitconfig", From: watermark, Through: watermark + 1, Covered: true, Paths: paths})
		watermark++
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	config := filepath.Join(dir, ".git", "config")
	base := apply().TargetGeneration

	for _, step := range gitConfigNoopKeys {
		admissionGit(t, dir, step.args...)
		before := len(sink.CloneRecords())
		res := apply(config)
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

	admissionGit(t, dir, "add", "-f", "gen/tracked.ts")
	admitted := apply(filepath.Join(dir, ".git", "index"))
	if admitted.TargetGeneration != base+1 || admitted.ParsedFiles == 0 {
		t.Fatalf("resident admission of a tracked ignored file published nothing: %+v", admitted.Result)
	}
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// The configuration bytes leaving the identity must not leave the mid-run
// fence. The run declares .git/config as an input and re-reads it before it
// will call a replacement successful, so a write landing after Begin still
// fails the run outright with the completed state untouched.
func TestGitConfigEditAfterBeginFailsTheResidentRun(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	sink := &mdScopeBeginSink{}
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	r, err := OpenSession(context.Background(), admissionEngine(t, dir, graphinput.Options{}), dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	watermark := uint64(0)
	apply := func(paths ...string) (*OnlineResult, error) {
		res, err := r.ApplyChanges(context.Background(), ChangeBatch{Epoch: "gitconfig", From: watermark, Through: watermark + 1, Covered: true, Paths: paths})
		watermark++
		return res, err
	}
	if _, err := apply(); err != nil {
		t.Fatal(err)
	}
	kept, keptBytes := committedGeneration(t, opts.StateDir)
	published := len(sink.CloneRecords())

	sink.onBegin = func() { admissionGit(t, dir, "config", "--local", "user.email", "raced@example.com") }
	writeRepoFile(t, dir, "src/added.ts", "export const added = 1;\n")
	if _, err := apply(filepath.Join(dir, "src", "added.ts")); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("a configuration write during the transaction was not caught: %v", err)
	}
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
	cons := NewConsumer()
	if err := cons.ApplyRecords(sink.CloneRecords()); err != nil {
		t.Fatal(err)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// The gitignore files are declared dependencies too, and they are not control
// files, so their bytes stay in the identity. A rule change still has to retire
// the owners it excludes and readmit them when it is withdrawn - the defect's
// fix must not have desensitised the identity to the file that actually decides
// admission.
func TestGitConfigFixLeavesIgnoreRuleChangesReconciling(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	initial, initialSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	owners, ids := beginScope(t, initialSink)
	requireOwners(t, owners, ids, "src/solo.ts")

	writeRepoFile(t, dir, ".gitignore", "src/solo.ts\n")
	retired, retiredSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if retired.TargetGeneration != initial.TargetGeneration+1 {
		t.Fatalf("an ignore rule that excludes an owner published nothing: generation %d", retired.TargetGeneration)
	}
	if len(retiredSink.CloneRecords()) == 0 {
		t.Fatal("the ignore rule change published no events")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))

	writeRepoFile(t, dir, ".gitignore", "")
	readmitted, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if readmitted.TargetGeneration != retired.TargetGeneration+1 {
		t.Fatalf("withdrawing the ignore rule published nothing: generation %d", readmitted.TargetGeneration)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))
}

// A state written under the previous identity schema carries fingerprints that
// no longer describe any policy this build can compute. It has to reconcile
// once, conservatively and completely, and then go quiet - never be adopted on
// the strength of a value whose meaning changed underneath it.
func TestGitConfigOldIdentitySchemaStateMigratesConservatively(t *testing.T) {
	dir := admissionRepo(t, nil)
	admissionGit(t, dir, "add", "tsconfig.json", "package.json", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	first, _ := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)

	st, err := loadCommittedState(opts.StateDir)
	if err != nil || st == nil {
		t.Fatalf("state: %v %+v", err, st)
	}
	// Both fingerprints move together across a schema change, which is what
	// makes this different from the stale-raw-identity case: there is no
	// bookkeeping repair available, only a reconciliation.
	st.PolicyIdentity = "graph-input-v1-era-raw"
	st.PolicyAdmissionIdentity = "graph-input-admission-v1-era"
	if err := saveState(opts.StateDir, st); err != nil {
		t.Fatal(err)
	}

	migrated, sink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	if !migrated.Invalidation.PolicyReconciled {
		t.Fatalf("a state from the previous identity schema was adopted without reconciling: %+v", migrated.Invalidation)
	}
	if migrated.TargetGeneration != first.TargetGeneration+1 {
		t.Fatalf("the migrating run published nothing: generation %d", migrated.TargetGeneration)
	}
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts")
	assertAppliedEqualsCold(t, cons, coldConsumer(t, admissionEngine(t, dir, graphinput.Options{}), dir))

	quiet, quietSink := admissionRun(t, admissionEngine(t, dir, graphinput.Options{}), dir, opts, cons)
	assertNoPublication(t, quiet, quietSink, migrated.TargetGeneration, "the run after the schema migration")
}

// A linked worktree keeps core.worktree in config.worktree once
// extensions.worktreeConfig is on, and its git dir has no config file at all,
// so declaring only config would leave a redirect unwatched and invisible to
// the run's input fence.
func TestGitConfigWorktreeConfigIsDeclaredAndMovesBothIdentities(t *testing.T) {
	main := admissionRepo(t, map[string]string{
		".gitignore":     "gen/\n",
		"gen/tracked.ts": "export const tracked = 1;\n",
	})
	admissionGit(t, main, "add", "src/a.ts", "src/barrel.ts", "src/use.ts", "src/solo.ts", ".gitignore")
	admissionGit(t, main, "add", "-f", "gen/tracked.ts")
	admissionGit(t, main, "commit", "-qm", "init")

	linked := filepath.Join(t.TempDir(), "wt")
	admissionGit(t, main, "worktree", "add", "-q", "-b", "wt", linked)
	before := admissionPolicy(t, linked, graphinput.Options{})

	gitDir := strings.TrimSpace(gitOutput(t, linked, "rev-parse", "--absolute-git-dir"))
	perWorktree := filepath.Join(gitDir, "config.worktree")
	if _, err := os.Stat(perWorktree); err == nil {
		t.Fatal("fixture already had a config.worktree, so its declaration is not being proven here")
	}
	var declared bool
	for _, d := range before.Dependencies() {
		if d.Path == perWorktree {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("config.worktree is not a declared dependency, so a redirect through it is unwatched and unfenced\n%s", perWorktree)
	}

	elsewhere := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	admissionGit(t, linked, "config", "--local", "extensions.worktreeConfig", "true")
	admissionGit(t, linked, "config", "--worktree", "core.worktree", elsewhere)
	after := admissionPolicy(t, linked, graphinput.Options{})

	_, beforeIndex := partitionGitDeps(t, linked, before.Dependencies())
	_, afterIndex := partitionGitDeps(t, linked, after.Dependencies())
	if !reflect.DeepEqual(beforeIndex, afterIndex) {
		t.Fatal("the redirect moved the index, so the tracked set is not held still here")
	}
	if after.Identity() == before.Identity() {
		t.Fatal("a config.worktree redirect left the raw identity standing")
	}
	if after.AdmissionIdentity() == before.AdmissionIdentity() {
		t.Fatal("a config.worktree redirect left the admission identity standing")
	}
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
