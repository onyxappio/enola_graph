package graphinput

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func build(t *testing.T, root string, o Options) *Policy {
	t.Helper()
	p, e := Build(root, o)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func gitTest(t *testing.T, root string, args ...string) {
	t.Helper()
	if out, e := runGit(root, "", nil, args...); e != nil {
		t.Fatalf("git %v: %v %s", args, e, out)
	}
}
func wantKind(t *testing.T, p *Policy, name string, dir bool, want Kind) {
	t.Helper()
	if d := p.Classify(name, dir); d.Kind != want {
		t.Errorf("%s = %+v, want %v", name, d, want)
	}
}

func TestGitIgnoreNestedOrderedAndTracked(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	files := map[string]string{
		".gitignore":        "*.tmp\n!keep.tmp\nblocked/\n!blocked/rescue.ts\nopen/*\n!open/keep/\n/root-only.ts\n*.bak\n",
		"nested/.gitignore": "!local.tmp\nlocal.tmp\n!again.tmp\n!child.bak\nonly-dir/\n\\#literal\nescaped\\ space.ts\n",
		"keep.tmp":          "", "drop.tmp": "", "nested/local.tmp": "", "nested/again.tmp": "", "nested/child.bak": "", "nested/drop.bak": "",
		"blocked/.gitignore": "!rescue.ts\n", "blocked/rescue.ts": "", "blocked/tracked.ts": "", "open/keep/a.ts": "", "open/drop/a.ts": "",
		"root-only.ts": "", "nested/root-only.ts": "", "nested/only-dir/a.ts": "", "else/only-dir": "", "nested/#literal": "", "nested/escaped space.ts": "",
	}
	for n, b := range files {
		put(t, root, n, b)
	}
	gitTest(t, root, "add", "-f", "blocked/tracked.ts", "drop.tmp")
	p := build(t, root, Options{})
	for _, n := range []string{"keep.tmp", "nested/again.tmp", "nested/child.bak", "blocked/tracked.ts", "drop.tmp", "open/keep/a.ts", "nested/root-only.ts", "else/only-dir"} {
		wantKind(t, p, n, false, Semantic)
	}
	for _, n := range []string{"nested/local.tmp", "nested/drop.bak", "blocked/rescue.ts", "open/drop/a.ts", "root-only.ts", "nested/only-dir/a.ts", "nested/#literal", "nested/escaped space.ts"} {
		wantKind(t, p, n, false, Excluded)
	}
	wantKind(t, p, "blocked", true, Semantic) // must traverse for tracked child
	p = build(t, root, Options{Exclude: []string{"blocked/**", "**/*.tmp"}})
	wantKind(t, p, "blocked/tracked.ts", false, Excluded)
	wantKind(t, p, "drop.tmp", false, Excluded)
}

func TestNonGitAndGlobalIgnoreIsolation(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "*.tmp\n")
	put(t, root, "a.tmp", "")
	put(t, root, "a.ts", "")
	fake := t.TempDir()
	put(t, fake, "global", "*.ts\n")
	put(t, fake, ".gitconfig", "[core]\n excludesFile = "+filepath.Join(fake, "global")+"\n")
	t.Setenv("HOME", fake)
	p := build(t, root, Options{})
	wantKind(t, p, "a.tmp", false, Excluded)
	wantKind(t, p, "a.ts", false, Semantic)
}

func TestDefaultsAndMediaOverride(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"package.json", "go.mod", "schema.lock", "a.PNG", "movie.Mp4", "font.WOFF2", "a.svg", "a.map", "a.zip", "src/generated/a.ts", "docs/a.md", "public/a.ts", ".yarn/patches/a.patch", ".yarn/plugins/a.cjs", ".yarnrc.yml", ".pnp.cjs", "nested/.next/a.ts", "nested/.yarn/cache/a.zip", "nested/.yarn/unplugged/a.ts", "nested/node_modules/a.ts", "nested/.parcel-cache/a.ts", "worker-reports/a.ts"} {
		put(t, root, n, "")
	}
	p := build(t, root, Options{})
	for _, n := range []string{"nested/.next/a.ts", "nested/.yarn/cache/a.zip", "nested/.yarn/unplugged/a.ts", "nested/node_modules/a.ts", "nested/.parcel-cache/a.ts"} {
		wantKind(t, p, n, false, Excluded)
	}
	for _, n := range []string{"package.json", "go.mod", "schema.lock", "a.svg", "a.map", "a.zip", "src/generated/a.ts", "docs/a.md", "public/a.ts", ".yarn/patches/a.patch", ".yarn/plugins/a.cjs", ".yarnrc.yml", ".pnp.cjs", "worker-reports/a.ts"} {
		wantKind(t, p, n, false, Semantic)
	}
	for _, n := range []string{"a.PNG", "movie.Mp4", "font.WOFF2"} {
		wantKind(t, p, n, false, NameOnly)
	}
	p = build(t, root, Options{CacheExclusions: []string{}, Semantic: []string{"**/*.PNG"}, Exclude: []string{"worker-reports/**"}})
	wantKind(t, p, "nested/.next/a.ts", false, Semantic)
	wantKind(t, p, "a.PNG", false, Semantic)
	wantKind(t, p, "worker-reports/a.ts", false, Excluded)
	p = build(t, root, Options{ConservativeMedia: true})
	wantKind(t, p, "movie.Mp4", false, Semantic)
}

func TestLocksIdentityAndEvents(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	put(t, root, "a.ts", "one")
	put(t, root, "photo.PNG", "one")
	put(t, root, ".gitignore", "hidden/\n")
	put(t, root, "hidden/a.ts", "")
	p := build(t, root, Options{})
	for _, n := range []string{"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "go.sum", "Cargo.lock", "Gemfile.lock", "composer.lock", "Pipfile.lock", "poetry.lock", "uv.lock", "pdm.lock", "pubspec.lock", "packages.lock.json", "Package.resolved"} {
		put(t, root, "nested/"+n, "lock")
		wantKind(t, p, "nested/"+n, false, Excluded)
		for _, e := range []Event{Content, Membership} {
			if a := p.ClassifyEvent("nested/"+n, false, e); a != Ignore {
				t.Errorf("lock %s = %v", n, a)
			}
		}
	}
	gitTest(t, root, "add", "nested")
	q := build(t, root, Options{})
	if p.Identity() != q.Identity() {
		t.Fatal("lock-only staging changed identity")
	}
	for _, tc := range []struct {
		n    string
		dir  bool
		e    Event
		want Action
	}{
		{"a.ts", false, Content, ContentChanged}, {"photo.PNG", false, Content, Ignore}, {"photo.PNG", false, Membership, NamesChanged},
		{"new.ts", false, Content, Reconcile}, {"new.png", false, Membership, Reconcile}, {".gitignore", false, Content, Reconcile}, {"new/.gitignore", false, Membership, Reconcile},
		{".git/index", false, Content, Reconcile}, {".git/HEAD", false, Content, Reconcile}, {"hidden/a.ts", false, Content, Ignore}, {"nested/.next/new.ts", false, Membership, Ignore},
	} {
		if got := q.ClassifyEvent(tc.n, tc.dir, tc.e); got != tc.want {
			t.Errorf("%s action %v want %v", tc.n, got, tc.want)
		}
	}
	put(t, root, "a.ts", "two")
	put(t, root, "photo.PNG", "two")
	if build(t, root, Options{}).Identity() != q.Identity() {
		t.Fatal("content changed policy identity")
	}
	gitTest(t, root, "add", "a.ts")
	if build(t, root, Options{}).Identity() == q.Identity() {
		t.Fatal("tracked membership did not change identity")
	}
	put(t, root, ".gitignore", "other/\n")
	if build(t, root, Options{}).Identity() == q.Identity() {
		t.Fatal("ignore input did not change identity")
	}
}

func TestIgnoredSubtreeAndDirectoryReconciliation(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".gitignore", "blocked/\n")
	put(t, root, "blocked/.gitignore", "one\n")
	put(t, root, "blocked/a.ts", "")
	put(t, root, "plain/a.ts", "")
	put(t, root, "excluded/a.ts", "")
	p := build(t, root, Options{Exclude: []string{"excluded"}})
	if p.ClassifyEvent("blocked/new.ts", false, Membership) != Ignore {
		t.Fatal("new file under ignored parent must be excluded")
	}
	if p.ClassifyEvent("excluded/new/a.ts", false, Membership) != Ignore {
		t.Fatal("literal directory exclusion must cover descendants")
	}
	if p.ClassifyEvent("plain", true, Membership) != Reconcile {
		t.Fatal("directory replacement must reconcile")
	}
	put(t, root, "blocked/.gitignore", "two\n")
	q := build(t, root, Options{Exclude: []string{"excluded"}})
	if p.Identity() != q.Identity() {
		t.Fatal("inactive nested ignore changed identity")
	}
	put(t, root, "plain/.gitignore", "a.ts\n")
	q = build(t, root, Options{Exclude: []string{"excluded"}})
	if p.Identity() == q.Identity() {
		t.Fatal("new active nested ignore did not change identity")
	}
	wantKind(t, q, "plain/a.ts", false, Excluded)
}

func TestLinkedWorktreeIndexDependency(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	put(t, root, "a.ts", "")
	gitTest(t, root, "add", "a.ts")
	gitTest(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "initial")
	linked := filepath.Join(t.TempDir(), "linked")
	gitTest(t, root, "worktree", "add", "--detach", linked)
	p := build(t, linked, Options{})
	out, err := runGit(linked, "", nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(strings.TrimSpace(string(out)), "index")
	if p.ClassifyEvent(index, false, Content) != Reconcile {
		t.Fatal("external linked-worktree index not reconciled")
	}
	if p.ClassifyEvent(".git", false, Content) != Reconcile {
		t.Fatal("gitfile replacement not reconciled")
	}
	found := false
	for _, d := range p.Dependencies() {
		if d.Path == index {
			found = true
		}
	}
	if !found {
		t.Fatal("external index absent from dependency list")
	}
}

func TestImmutablePromotionDependenciesAndExternalInputs(t *testing.T) {
	root := t.TempDir()
	put(t, root, "a.PNG", "")
	put(t, root, "excluded/a.PNG", "")
	put(t, root, "state/data", "")
	external := filepath.Join(t.TempDir(), "config.yaml")
	o := Options{Exclude: []string{"excluded/**"}, ConfigPaths: []string{external}, StateDirs: []string{"state"}}
	p := build(t, root, o)
	id := p.Identity()
	o.Exclude[0] = "changed"
	wantKind(t, p, "excluded/a.PNG", false, Excluded)
	wantKind(t, p, "state/data", false, Excluded)
	if p.ClassifyEvent(external, false, Content) != Reconcile {
		t.Fatal("external config dependency missing")
	}
	deps := p.Dependencies()
	deps[0].Path = "changed"
	if p.Dependencies()[0].Path == "changed" {
		t.Fatal("mutable dependencies")
	}
	q := p.WithConservativeMedia()
	wantKind(t, p, "a.PNG", false, NameOnly)
	wantKind(t, q, "a.PNG", false, Semantic)
	if q.Identity() == id || p.Identity() != id || q.WithConservativeMedia() != q {
		t.Fatal("promotion identity/immutability failure")
	}
	for _, tc := range []struct {
		name string
		kind Kind
	}{{external, Semantic}, {filepath.Join(filepath.Dir(external), "yarn.lock"), Excluded}, {"a.PNG", Semantic}, {"excluded/a.PNG", Excluded}} {
		if d := p.ClassifyDependency(tc.name); d.Kind != tc.kind {
			t.Errorf("dependency %s = %+v", tc.name, d)
		}
	}
	if _, err := Build(root, Options{StateDirs: []string{"."}}); err == nil {
		t.Fatal("root state exclusion accepted")
	}
	put(t, filepath.Dir(external), filepath.Base(external), "changed")
	if build(t, root, Options{Exclude: []string{"excluded/**"}, ConfigPaths: []string{external}, StateDirs: []string{"state"}}).Identity() == id {
		t.Fatal("external config creation did not change identity")
	}
}
