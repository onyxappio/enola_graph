package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// proofFixture is retentionFixture with the policy options the real CLI passes.
// pkg/command/graph.go hands NewGraphEngine the graph output directories as
// StateDirs and internal/config adds the session's own state directory, so a
// fixture that omits them lets the resident's `.enola` tree appear inside the
// walked repository and every proof below would refuse for a reason the CLI
// does not have.
func proofFixture(t *testing.T, files map[string]string, opts Options) (string, *Resident, *ChangeQueue, *graphstream.MemorySink) {
	t.Helper()
	dir := admissionRepo(t, files)
	sink := &graphstream.MemorySink{}
	opts.StateDir = filepath.Join(dir, ".enola", "resident")
	r, err := OpenSession(context.Background(), admissionEngine(t, dir, proofPolicyOptions()), dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	q := NewChangeQueue("test", 8)
	q.Start(context.Background())
	return dir, r, q, sink
}

func proofPolicyOptions() graphinput.Options {
	return graphinput.Options{StateDirs: []string{".enola"}}
}

// coldWithFreshTargetPolicy is the oracle the declared-input proof has to
// answer to. residentCold replays against `r.eng`, which on a proven run is the
// very engine whose policy was never rebuilt, so it would compare a policy
// against itself and agree no matter what the tree did. This builds a new
// engine over the tree as it stands now - the same thing resolveGraphTarget
// would construct for a fresh CLI - and compares the resident's stream against
// that.
func coldWithFreshTargetPolicy(t *testing.T, dir string, sink *graphstream.MemorySink) {
	t.Helper()
	coldSink := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), admissionEngine(t, dir, proofPolicyOptions()), dir, coldSink,
		Options{StateDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	applied, cold := NewConsumer(), NewConsumer()
	applyRun(t, applied, sink)
	applyRun(t, cold, coldSink)
	assertAppliedEqualsCold(t, applied, cold)
}

// Root's objection, tested rather than argued: the set a policy declares is the
// set of paths it read, and a path that did not exist when it was built is not
// in it. Every case here moves the tree between the engine's construction and
// the first transaction - exactly the window a fresh CLI leaves open between
// resolveGraphTarget and its first run - and every case is held to a graph
// built by a policy that observed the tree afterwards.
//
// The counters are logged, not asserted, because which path each case takes is
// the finding. What is asserted is the part that may not vary: the result.
func TestGraphInputProofAgainstMembershipMovingBeforeFirstRun(t *testing.T) {
	cases := []struct {
		name string
		// mustRebuild is whether this case moved something the policy itself
		// observed. Where it did, taking the skip is the defect, and saying so
		// here is what makes this a guard rather than a report: a proof that
		// stopped covering membership would still produce an equal graph in
		// these cases today and the oracle alone would not notice.
		mustRebuild bool
		queue       []string
		mutate      func(t *testing.T, dir string)
	}{
		{
			// A directory that did not exist when the policy walked. Its
			// descendants are all new names, and under a policy that never saw
			// them they would be classified by rules that never considered the
			// directory.
			name:        "absent parent directory appears",
			mustRebuild: true,
			queue:       []string{"src/deep/nested/x.ts"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, "src/deep/nested/x.ts", "export const x = 1;\n")
			},
		},
		{
			// Tracking, not membership: the names are unmoved and only the
			// index says something different about one of them. The index is a
			// declared input, so this is caught by the re-read rather than by
			// the walk, and it is here because the two have to hold together.
			name:        "one descendant becomes tracked",
			mustRebuild: true,
			queue:       nil,
			mutate: func(t *testing.T, dir string) {
				admissionGit(t, dir, "add", "src/a.ts")
			},
		},
		{
			// A tracked file that a new rule would ignore. Git keeps tracking
			// it, so the admission identity's tracked-and-ignored sets are
			// exactly what moves, and both halves of the proof are involved.
			name:        "tracked descendant becomes ignored",
			mustRebuild: true,
			queue:       []string{".gitignore"},
			mutate: func(t *testing.T, dir string) {
				admissionGit(t, dir, "add", "-A")
				writeFile(t, dir, ".gitignore", "src/solo.ts\n")
			},
		},
		{
			// The gitfile redirect. `.git` stops being a directory and becomes
			// a file pointing elsewhere, which moves Git discovery wholesale;
			// the control files the policy declared under the old directory are
			// what notice.
			name:        "git directory becomes a gitfile",
			mustRebuild: true,
			queue:       nil,
			mutate: func(t *testing.T, dir string) {
				external := filepath.Join(t.TempDir(), "gitdir")
				if err := os.Rename(filepath.Join(dir, ".git"), external); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+external+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			// extensions.worktreeConfig carries core.worktree without config
			// ever being touched, so config.worktree is declared even when it
			// does not exist and its appearance has to refuse.
			name:        "worktree config appears",
			mustRebuild: true,
			queue:       nil,
			mutate: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, ".git", "config.worktree"),
					[]byte("[core]\n\tworktree = .\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			// The case the whole change exists for. Bytes moved inside a file
			// the walk already saw, so nothing the policy observed moved.
			name:        "source content edited",
			mustRebuild: false,
			queue:       []string{"src/a.ts"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, "src/a.ts", "export function a(){ return 2; }\n")
			},
		},
		{
			name:        "source added",
			mustRebuild: true,
			queue:       []string{"src/added.ts", "src/use.ts"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, "src/added.ts", "export const added = 3;\n")
				writeFile(t, dir, "src/use.ts",
					"import { a } from './barrel';\nimport { added } from './added';\nexport function use(){ return a() + added; }\n")
			},
		},
		{
			name:        "source deleted",
			mustRebuild: true,
			queue:       []string{"src/solo.ts"},
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "src", "solo.ts")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:        "source renamed",
			mustRebuild: true,
			queue:       []string{"src/solo.ts", "src/renamed.ts"},
			mutate: func(t *testing.T, dir string) {
				if err := os.Rename(filepath.Join(dir, "src", "solo.ts"), filepath.Join(dir, "src", "renamed.ts")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			// The case the declared set cannot contain: this file did not exist
			// when the policy walked, so no digest of it was recorded, and it
			// changes what the tree admits.
			name:        "nested gitignore appears",
			mustRebuild: true,
			queue:       []string{"src/.gitignore"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, "src/.gitignore", "solo.ts\n")
			},
		},
		{
			name:        "nested package appears",
			mustRebuild: true,
			queue:       []string{"src/pkg/package.json", "src/pkg/index.ts"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, "src/pkg/package.json", `{"name":"@scope/pkg","main":"index.ts"}`+"\n")
				writeFile(t, dir, "src/pkg/index.ts", "export const p = 1;\n")
			},
		},
		{
			name:        "git index changes tracking",
			mustRebuild: true,
			queue:       nil,
			mutate: func(t *testing.T, dir string) {
				admissionGit(t, dir, "add", "-A")
			},
		},
		{
			name:        "git config changes",
			mustRebuild: true,
			queue:       nil,
			mutate: func(t *testing.T, dir string) {
				admissionGit(t, dir, "config", "core.ignorecase", "false")
			},
		},
		{
			name:        "root gitignore appears",
			mustRebuild: true,
			queue:       []string{".gitignore"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, ".gitignore", "src/solo.ts\n")
			},
		},
		{
			name:        "config changes",
			mustRebuild: false,
			queue:       []string{"tsconfig.json"},
			mutate: func(t *testing.T, dir string) {
				writeFile(t, dir, "tsconfig.json",
					`{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["lib/*"]}}}`+"\n")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, r, q, sink := proofFixture(t, discoveryRepoFiles(), Options{FreshEngine: true})
			tc.mutate(t, dir)
			for _, rel := range tc.queue {
				q.Add(rel)
			}
			res := residentApply(t, r, q)
			t.Logf("proven=%d rebuilt=%d policyBuilds=%d reconciled=%v",
				res.Work.GraphInputRebuildsProven, res.Work.GraphInputRebuilds, res.Work.PolicyBuilds, res.Reconciled)
			if tc.mustRebuild && res.Work.GraphInputRebuilds != 1 {
				t.Fatalf("a move the policy itself observed was accepted as proof: %+v", res.Work)
			}
			if !tc.mustRebuild && res.Work.GraphInputRebuildsProven != 1 {
				t.Fatalf("an edit that moved nothing the policy observed still rebuilt: %+v", res.Work)
			}
			coldWithFreshTargetPolicy(t, dir, sink)
		})
	}
}
