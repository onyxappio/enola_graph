package graphinput

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIndependentReuseRejectsInheritedGitfileRedirect(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"one", "two"} {
		out, err := exec.Command("git", "init", "-q", filepath.Join(base, name)).CombinedOutput()
		if err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
	}
	parent := filepath.Join(base, "work")
	root := filepath.Join(parent, "sub")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	redirect := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(parent, ".git"), []byte("gitdir: "+filepath.Join(base, name, ".git")+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	redirect("one")
	p, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !p.git.Repository {
		t.Fatal("fixture did not discover inherited gitfile")
	}
	if why, ok := p.ReusableOver(); !ok {
		t.Fatalf("unchanged fixture refused: %s", why)
	}
	redirect("two")
	fresh, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Identity() == p.Identity() {
		t.Fatal("fixture did not change fresh policy identity")
	}
	if why, ok := p.ReusableOver(); ok {
		t.Fatalf("inherited gitfile redirect accepted despite changed fresh policy: %s", why)
	}
}

func TestIndependentReuseRejectsCommonDirRedirect(t *testing.T) {
	base := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	one := filepath.Join(base, "one")
	two := filepath.Join(base, "two")
	root := filepath.Join(base, "linked")
	for _, repo := range []string{one, two} {
		git("init", "-q", repo)
		git("-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init")
	}
	git("-C", one, "worktree", "add", "--detach", root)
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a=1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if why, ok := p.ReusableOver(); !ok {
		t.Fatalf("unchanged fixture refused: %s", why)
	}
	if len(p.git.Dirs) != 2 {
		t.Fatalf("missing Git projection: %+v", p.git)
	}
	if err := os.WriteFile(filepath.Join(p.git.Dirs[0], "commondir"), []byte(filepath.Join(two, ".git")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fresh, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Identity() == p.Identity() {
		t.Fatal("redirect did not change cold policy")
	}
	if why, ok := p.ReusableOver(); ok {
		t.Fatalf("changed common directory accepted: %s", why)
	}
}
