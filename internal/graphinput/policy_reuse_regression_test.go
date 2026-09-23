package graphinput

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func reuseTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func reuseFiles() map[string]string {
	return map[string]string{
		"package.json":   `{"name":"root"}`,
		"src/a.ts":       "export const a = 1;\n",
		"src/b.ts":       "export const b = 2;\n",
		"src/.gitignore": "tmp/\n",
	}
}

// ReusableOver stands in for a second Build over the same tree, so what it has
// to get right is every way the second Build would have answered differently.
// Each case below moves one thing and nothing else.
func TestReusableOverRefusesEveryMoveTheBuildWouldSee(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		reuse  bool
	}{
		{
			name:   "unmoved tree",
			mutate: func(t *testing.T, dir string) {},
			reuse:  true,
		},
		{
			// Content inside a file the walk already saw. Nothing the policy
			// observed moved, and this is the case the skip exists for.
			name: "file content edited",
			mutate: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, "src", "a.ts"), []byte("export const a = 99;\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			reuse: true,
		},
		{
			name: "rule file appears in a walked directory",
			mutate: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("src/b.ts\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			name: "directory appears",
			mutate: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			name: "file disappears",
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "src", "b.ts")); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			name: "file becomes a directory",
			mutate: func(t *testing.T, dir string) {
				abs := filepath.Join(dir, "src", "b.ts")
				if err := os.Remove(abs); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(abs, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			// The bytes are identical, so a digest comparison agrees. A build
			// would stop treating it as a rule file, because the walk skips
			// symlinked .gitignore, so the policy would differ anyway.
			name: "rule file becomes a symlink to the same bytes",
			mutate: func(t *testing.T, dir string) {
				abs := filepath.Join(dir, "src", ".gitignore")
				target := filepath.Join(dir, "src", "ignore-source")
				if err := os.WriteFile(target, []byte("tmp/\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(abs); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, abs); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			// Discovery found no repository, so the absence is what it rested
			// on. A repository appearing redirects everything that follows.
			name: "repository appears",
			mutate: func(t *testing.T, dir string) {
				// A real one. An empty .git directory is not a repository -
				// rev-parse refuses it and a fresh Build answers exactly as
				// before - so creating one would assert a refusal that is not
				// owed.
				reuseGit(t, dir, "init", "-q")
			},
			reuse: false,
		},
		{
			// Unreadable is not unchanged. A proof that cannot be completed has
			// to fail closed, not report agreement it never established.
			name: "walk cannot complete",
			mutate: func(t *testing.T, dir string) {
				sub := filepath.Join(dir, "src")
				if err := os.Chmod(sub, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(sub, 0o755) })
			},
			reuse: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := reuseTree(t, reuseFiles())
			p, err := Build(dir, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if why, ok := p.ReusableOver(); !ok {
				t.Fatalf("the tree it was built over did not prove: %s", why)
			}
			tc.mutate(t, dir)
			why, ok := p.ReusableOver()
			if ok != tc.reuse {
				t.Fatalf("reusable=%v want %v (reason %q)", ok, tc.reuse, why)
			}
			if !ok && why == "" {
				t.Fatal("refused without naming what moved")
			}
			// Whatever it answered has to be what a second build answers. The
			// identities are the build's own statement of what it observed.
			fresh, err := Build(dir, Options{})
			if err != nil {
				if tc.reuse {
					t.Fatalf("proved reusable over a tree that no longer builds: %v", err)
				}
				return
			}
			same := fresh.Identity() == p.Identity() && fresh.AdmissionIdentity() == p.AdmissionIdentity()
			if ok && !same {
				t.Fatalf("proved reusable but a fresh build differs (reason %q)", why)
			}
		})
	}
}

func reuseGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// Root's case (msg_7910952f3c7b), and it is the one the entry map cannot reach:
// `hard` prunes any path segment named .git, so a repository appearing is
// invisible to the walk, and when discovery was inherited from a parent the
// declared control files all live in that parent and do not move either. A
// nearer repository redirects every later answer - the toplevel, the index, the
// tracked set - so the proof has to refuse it, and the only thing that can
// notice is a recorded absence.
func TestReusableOverRefusesRepositoryDiscoveryMovingNearer(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, base, root string)
		reuse  bool
	}{
		{
			name:   "inherited discovery unmoved",
			mutate: func(t *testing.T, base, root string) {},
			reuse:  true,
		},
		{
			// A repository initialised at the policy's own root, nearer than
			// the parent whose discovery this policy inherited.
			name: "nearer repository appears at the root",
			mutate: func(t *testing.T, base, root string) {
				reuseGit(t, root, "init", "-q")
			},
			reuse: false,
		},
		{
			// Nearer than the parent but not at the root: an intermediate
			// directory between them becomes the toplevel instead.
			name: "nearer repository appears at an intermediate directory",
			mutate: func(t *testing.T, base, root string) {
				reuseGit(t, filepath.Dir(root), "init", "-q")
			},
			reuse: false,
		},
		{
			// The gitfile redirect, stated explicitly rather than left to the
			// control-file digests: the bytes that say where the git dir is are
			// themselves a declared input.
			name: "gitfile redirect changes",
			mutate: func(t *testing.T, base, root string) {
				reuseGit(t, root, "init", "-q")
				gitdir := filepath.Join(base, "moved-gitdir")
				if err := os.Rename(filepath.Join(root, ".git"), gitdir); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			// extensions.worktreeConfig carries core.worktree without config
			// ever being touched, so config.worktree is declared even when it
			// does not exist.
			name: "worktree config appears",
			mutate: func(t *testing.T, base, root string) {
				if err := os.WriteFile(filepath.Join(base, ".git", "config.worktree"),
					[]byte("[core]\n\tworktree = .\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			reuse: false,
		},
		{
			// Tracking moves without a name moving.
			name: "index changes",
			mutate: func(t *testing.T, base, root string) {
				reuseGit(t, base, "add", "-A")
			},
			reuse: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := reuseTree(t, map[string]string{
				"outer.ts":          "export const outer = 0;\n",
				"nest/sub/pkg.json": "{}\n",
			})
			reuseGit(t, base, "init", "-q")
			root := filepath.Join(base, "nest", "sub")
			for rel, body := range reuseFiles() {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			p, err := Build(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !p.git.Repository {
				t.Fatal("the fixture did not inherit the parent repository")
			}
			if why, ok := p.ReusableOver(); !ok {
				t.Fatalf("the tree it was built over did not prove: %s", why)
			}
			tc.mutate(t, base, root)
			why, ok := p.ReusableOver()
			if ok != tc.reuse {
				t.Fatalf("reusable=%v want %v (reason %q)", ok, tc.reuse, why)
			}
			fresh, err := Build(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			same := fresh.Identity() == p.Identity() && fresh.AdmissionIdentity() == p.AdmissionIdentity()
			if ok && !same {
				t.Fatalf("proved reusable but a fresh build differs (reason %q)", why)
			}
			if !ok && same && tc.name != "inherited discovery unmoved" {
				t.Logf("refused %q though a fresh build agrees; conservative, not wrong", why)
			}
		})
	}
}
