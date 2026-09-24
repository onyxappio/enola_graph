// Package graphinput builds immutable graph input snapshots. Build performs IO;
// classification methods do not. Rebuild after Reconcile before using decisions
// for new membership. Consumers must enforce these decisions at side-read boundaries
// as well as inventory boundaries; this package does not wire those consumers.
package graphinput

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphprofile"
)

type Kind uint8

const (
	Excluded Kind = iota
	NameOnly
	Semantic
)

type Decision struct {
	Kind Kind
	// Known is false for paths absent at Build. Except for unconditional exclusions,
	// such a decision is provisional and requires reconciliation before graph use.
	Known  bool
	Reason string
}

type Event uint8

const (
	Content Event = iota
	Membership
)

type Action uint8

const (
	Ignore Action = iota
	ContentChanged
	NamesChanged
	Reconcile
)

type Options struct {
	Exclude []string
	// Nil uses safe cache defaults; non-nil replaces them, including an empty slice.
	CacheExclusions []string
	Semantic        []string
	// ConservativeMedia disables name-only classification for unaudited consumers.
	ConservativeMedia bool
	// StateDirs are literal directories, relative to root or absolute within it.
	StateDirs []string
	// ConfigPaths are policy input dependencies, including absent/external paths.
	ConfigPaths []string
}

type Dependency struct {
	Path   string
	Digest string // SHA256 of contents, or "missing"; absolute path.
}

type Policy struct {
	root                      string
	exclude, caches, semantic *facts.GlobSet
	conservative              bool
	states                    []string
	entries                   map[string]bool // directory bit
	ignored                   map[string]bool
	tracked                   map[string]bool
	trackedDirs               map[string]bool
	admitDirs                 map[string]bool
	deps                      []Dependency
	depSet                    map[string]bool
	gitDirs                   []string
	git                       gitState
	options                   Options
	identity                  string
	admission                 string

	// What the repository walk and the Git discovery observed, kept so a later
	// caller can ask whether observing again would answer the same. Neither is
	// hashed into an identity: they are not facts about the projection, they
	// are the record of how this policy came to be one.
	walkIgnoreFiles []string
}

var defaultCaches = []string{
	"**/node_modules/**", "**/.next/**", "**/.nuxt/**", "**/.svelte-kit/**",
	"**/.vercel/**", "**/.turbo/**", "**/.parcel-cache/**", "**/.npm/**",
	"**/.pnpm-store/**", "**/.yarn/cache/**", "**/.yarn/unplugged/**",
	"**/__pycache__/**", "**/.mypy_cache/**", "**/.pytest_cache/**", "**/.ruff_cache/**",
}

// IsLockfile intentionally recognizes exact ecosystem names, never *.lock.
func IsLockfile(name string) bool {
	switch filepath.Base(name) {
	case "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "go.sum", "Cargo.lock", "Gemfile.lock", "composer.lock", "Pipfile.lock", "poetry.lock", "uv.lock", "pdm.lock", "pubspec.lock", "packages.lock.json", "Package.resolved":
		return true
	}
	return false
}

func (p *Policy) relative(name string) (string, bool) {
	if filepath.IsAbs(name) {
		var err error
		name, err = filepath.Rel(p.root, name)
		if err != nil {
			return "", false
		}
	}
	name = filepath.ToSlash(filepath.Clean(name))
	return name, name != ".." && !strings.HasPrefix(name, "../")
}
func under(name, dir string) bool { return name == dir || strings.HasPrefix(name, dir+"/") }
func (p *Policy) hard(name string) string {
	// Build inserts entries only after hard admission. The policy is immutable,
	// so repeated reader walks can reuse that proof without caching disk state.
	if _, admitted := p.entries[name]; admitted {
		return ""
	}
	for _, s := range strings.Split(name, "/") {
		if s == ".git" || s == ".hg" || s == ".svn" {
			return "vcs"
		}
	}
	if IsLockfile(name) {
		return "lockfile"
	}
	for _, dir := range p.states {
		if under(name, dir) {
			return "state/output"
		}
	}
	for candidate := name; candidate != "."; candidate = filepath.ToSlash(filepath.Dir(candidate)) {
		if _, admitted := p.entries[candidate]; admitted {
			break
		}
		if p.exclude.MatchAny(candidate) {
			return "enola exclusion"
		}
		if p.caches.MatchAny(candidate) {
			return "cache exclusion"
		}
	}
	return ""
}

func (p *Policy) gitIgnored(name string) bool {
	for candidate := name; candidate != "."; candidate = filepath.ToSlash(filepath.Dir(candidate)) {
		if p.ignored[candidate] {
			return true
		}
	}
	return false
}

func (p *Policy) Classify(name string, directory bool) Decision {
	rel, ok := p.relative(name)
	if !ok {
		return Decision{Excluded, true, "outside root"}
	}
	if why := p.hard(rel); why != "" {
		return Decision{Excluded, true, why}
	}
	_, known := p.entries[rel]
	// The directory override consults admitDirs rather than every tracked
	// ancestor. A gitignored directory earns an exemption from the tracked files
	// under it that this policy could otherwise admit; one whose only tracked
	// descendants are hard-excluded exempts nothing, because every file under it
	// is excluded either by that hard rule or by gitignore with no tracking to
	// override it. Keeping the two sets equal is what lets the admission digest
	// stand for the decision function: see trackedAdmission.
	if p.gitIgnored(rel) && !p.tracked[rel] && !(directory && p.admitDirs[rel]) {
		return Decision{Excluded, known, "gitignore"}
	}
	if !directory && !p.conservative && !p.semantic.MatchAny(rel) && media(rel) {
		return Decision{NameOnly, known, "opaque media"}
	}
	return Decision{Semantic, known, "semantic"}
}

// ClassifyEvent handles one side of a rename; callers classify both old and new.
// Directory changes and missing inventory request reconciliation. Even excluded
// metadata can invalidate policy: check dependencies before normal classification.
func (p *Policy) ClassifyEvent(name string, directory bool, event Event) Action {
	abs := name
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(p.root, abs)
	}
	abs = filepath.Clean(abs)
	if IsLockfile(abs) {
		return Ignore
	}
	if p.depSet[abs] {
		return Reconcile
	}
	rel, ok := p.relative(name)
	if ok && (rel == ".git" || filepath.Base(rel) == ".gitignore") && p.hard(rel) == "" {
		return Reconcile
	}
	if ok && rel == ".git" {
		return Reconcile
	}
	for _, dir := range p.gitDirs {
		if under(filepath.ToSlash(abs), filepath.ToSlash(dir)) {
			if filepath.Base(abs) == "index" || filepath.Base(abs) == "HEAD" || filepath.Base(abs) == "config" || strings.HasPrefix(filepath.Base(abs), "sharedindex.") {
				return Reconcile
			}
		}
	}
	d := p.Classify(name, directory)
	if d.Kind == Excluded {
		return Ignore
	}
	if !d.Known || directory {
		return Reconcile
	}
	if event == Membership {
		return NamesChanged
	}
	if d.Kind == NameOnly {
		return Ignore
	}
	return ContentChanged
}

func (p *Policy) Identity() string           { return p.identity }
func (p *Policy) Dependencies() []Dependency { return append([]Dependency(nil), p.deps...) }

// ReusableOver asks whether building a policy over this tree now would read
// the same things this one read, and names the first thing that says otherwise.
// It exists for one caller: a process that constructed a policy resolving its
// target and is about to construct a byte-for-byte identical one at the top of
// its first run. The second build is a repository walk, a Git discovery, an
// index read, a check-ignore pass over every name and an identity computation;
// this is the declared reads, the Git discovery and the walk, and nothing else.
//
// That is a proof rather than an assumption because the stages it does not
// repeat are functions of the ones it does. Git discovery is not derived from
// anything else here, because nothing else can answer it: the declared control
// files live inside whichever git dir was found and do not move when discovery
// moves to another repository, an ancestor gitfile is never declared at all,
// and the walk cannot see a .git appear because hard() prunes it. So discovery
// is rerun and its whole projection compared. Tracking is then decided by that
// projection and by the control files, which are declared and re-read here; the
// ignore evaluation is decided by the walked names, the .gitignore contents and
// the index, all of which are covered. What is left - the temporary bare
// repository, the check-ignore pass and the identity computation - is pure
// computation over inputs this has just shown unmoved.
//
// It is deliberately strict about the walk. Any name appearing, disappearing or
// changing between file and directory refuses, because the built policy's
// entry map is what every later classification reads and a policy that never
// saw a name is not a policy that admits it. Ordinary content edits do not move
// that map, which is the case this is for.
//
// The window it proves across is the caller's to bound: this says the tree
// answers the same now, not that it never differed in between.
func (p *Policy) ReusableOver() (string, bool) {
	for _, d := range p.deps {
		digest := "missing"
		b, err := os.ReadFile(d.Path)
		switch {
		case err == nil:
			sum := sha256.Sum256(b)
			digest = hex.EncodeToString(sum[:])
		case !os.IsNotExist(err):
			// Unreadable is not unchanged. Report it as moved and let the
			// caller take the path that reads it again properly.
			return "declared input unreadable: " + d.Path, false
		}
		if digest != d.Digest {
			return "declared input moved: " + d.Path, false
		}
	}
	// Discovery is re-asked rather than inferred from the files above. The
	// declared control files live inside whichever git dir was found, so a
	// redirect moving the discovery to a different repository leaves every one
	// of them unmoved; hard() prunes any path segment named .git before the walk
	// records anything, so the entry map cannot see a repository appear either;
	// and an ancestor gitfile is not declared at all, because Build declares only
	// the gitfile at the root it was given. Nothing already read answers this, so
	// Git is asked the same question Build asked it.
	st, _, err := discoverGit(p.root)
	if err != nil {
		return "git discovery failed: " + err.Error(), false
	}
	if st.Repository != p.git.Repository {
		if st.Repository {
			return "repository appeared: " + st.TopLevel, false
		}
		return "repository disappeared: " + p.git.TopLevel, false
	}
	if st.TopLevel != p.git.TopLevel {
		return "repository moved: " + p.git.TopLevel + " -> " + st.TopLevel, false
	}
	if len(st.Dirs) != len(p.git.Dirs) {
		return "git directory set changed", false
	}
	for i, dir := range st.Dirs {
		if dir != p.git.Dirs[i] {
			return "git directory moved: " + p.git.Dirs[i] + " -> " + dir, false
		}
	}
	seen := map[string]bool{}
	var ignoreFiles []string
	err = filepath.WalkDir(p.root, func(abs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := p.relative(abs)
		if rel == "." {
			return nil
		}
		if p.hard(rel) != "" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dir, known := p.entries[rel]
		if !known {
			return fmt.Errorf("entry appeared: %s", rel)
		}
		if dir != entry.IsDir() {
			return fmt.Errorf("entry changed kind: %s", rel)
		}
		seen[rel] = true
		if entry.Name() == ".gitignore" && entry.Type()&os.ModeSymlink == 0 {
			ignoreFiles = append(ignoreFiles, rel)
		}
		return nil
	})
	if err != nil {
		return err.Error(), false
	}
	if len(seen) != len(p.entries) {
		for rel := range p.entries {
			if !seen[rel] {
				return "entry disappeared: " + rel, false
			}
		}
	}
	// The admitted subset of these is already covered as declared dependencies;
	// this is the raw set, so a rule file becoming a symlink - which the build
	// would stop treating as a rule file while its bytes read identically -
	// cannot pass as unchanged.
	if len(ignoreFiles) != len(p.walkIgnoreFiles) {
		return "ignore file set changed", false
	}
	was := map[string]bool{}
	for _, name := range p.walkIgnoreFiles {
		was[name] = true
	}
	for _, name := range ignoreFiles {
		if !was[name] {
			return "ignore file changed kind: " + name, false
		}
	}
	return "", true
}

// AdmissionIdentity fingerprints the policy's admission rules rather than the
// index that happens to satisfy them. Identity hashes every non-hard-excluded
// tracked name, so staging or untracking an ordinary source moves it even though
// no decision this package makes moves with it: p.tracked and the tracked
// directory set are read at exactly one place, the gitignore override in
// Classify, and that override only ever applies to a name Git ignores and no
// hard rule already removed. The directory half of that override reads
// p.admitDirs, which is the set hashed here, so the two cannot disagree. AdmissionIdentity therefore hashes the policy inputs - the options
// and the semantic dependencies, which carry the .gitignore bytes - together
// with only that override's own membership.
//
// Equal admission identities mean the two policies are the same decision
// function, applied to equivalent paths. They do not mean the two runs see the
// same inventory: files still appear and disappear on disk under an unchanged
// rule set, and a caller has to keep comparing the observed membership itself.
// Identity stays the raw value for validation, stale-state detection and race
// fencing, and stays the value a caller persists for those uses.
func (p *Policy) AdmissionIdentity() string { return p.admission }

func media(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico", ".bmp", ".mp4", ".mov", ".webm", ".mp3", ".wav", ".ogg", ".woff", ".woff2", ".ttf", ".otf":
		return true
	}
	return false
}

// Build evaluates repository .gitignore files with Git itself, isolated from global
// excludes and .git/info/exclude. The real index is used only for tracked exemption.
// Non-Git directories also support .gitignore. Git is required at build time.
// gitState is the effective Git state Build resolved. The identities hash this
// projection instead of the repository configuration's raw bytes, because the
// only things configuration can move here are discovery and which index is
// enumerated; ignore evaluation runs against an isolated git dir it cannot
// reach. See docs: a key that moves neither result is not an input here.
type gitState struct {
	Repository bool
	TopLevel   string
	Dirs       []string
}

// discoverGit returns a zero state and no error outside a repository, which is
// a supported input rather than a failure.
func discoverGit(root string) (gitState, []string, error) {
	git := func(args ...string) ([]byte, error) { return runGit(root, "", nil, args...) }
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return gitState{}, nil, nil
	}
	st := gitState{Repository: true, TopLevel: filepath.Clean(strings.TrimSpace(string(top)))}
	var dirs []string
	for _, flag := range []string{"--absolute-git-dir", "--git-common-dir"} {
		out, e := git("rev-parse", flag)
		if e != nil {
			return gitState{}, nil, e
		}
		dir := strings.TrimSpace(string(out))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		dirs = append(dirs, dir)
	}
	st.Dirs = append([]string(nil), dirs...)
	return st, dirs, nil
}

// indexNames re-enumerates on every call: the tracked exemption is never
// carried over, so a configuration change selecting a different index is
// observed rather than assumed away.
func indexNames(root string) (tracked, dirs map[string]bool, err error) {
	tracked, dirs = map[string]bool{}, map[string]bool{}
	out, e := runGit(root, "", nil, "ls-files", "-z", "--cached", "--", ".")
	if e != nil {
		return nil, nil, e
	}
	for _, name := range strings.Split(string(out), "\x00") {
		if name == "" {
			continue
		}
		tracked[name] = true
		for dir := filepath.ToSlash(filepath.Dir(name)); dir != "."; dir = filepath.ToSlash(filepath.Dir(dir)) {
			dirs[dir] = true
		}
	}
	return tracked, dirs, nil
}

func Build(root string, options Options) (*Policy, error) {
	// One trace per Build. Its Marks partition this call from here on, so the
	// setup below - Abs, the Policy literal, the state directory loop - is
	// inside the first span rather than before it. The caller runs its own
	// trace around this one, so the two nest and must not be added together.
	btr := graphprofile.StartNamed("graphinput_build")
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	caches := options.CacheExclusions
	if caches == nil {
		caches = defaultCaches
	}
	p := &Policy{root: root, exclude: facts.CompileGlobs(options.Exclude), caches: facts.CompileGlobs(caches), semantic: facts.CompileGlobs(options.Semantic), conservative: options.ConservativeMedia, entries: map[string]bool{}, ignored: map[string]bool{}, tracked: map[string]bool{}, trackedDirs: map[string]bool{}, depSet: map[string]bool{}}
	for _, dir := range options.StateDirs {
		rel, ok := p.relative(dir)
		if !ok {
			continue
		}
		if rel == "." {
			return nil, fmt.Errorf("state/output directory cannot be repository root")
		}
		p.states = append(p.states, rel)
	}
	addDep := func(name string) error {
		if !filepath.IsAbs(name) {
			name = filepath.Join(root, name)
		}
		name = filepath.Clean(name)
		if p.depSet[name] {
			return nil
		}
		p.depSet[name] = true
		b, err := os.ReadFile(name)
		digest := "missing"
		if err == nil {
			sum := sha256.Sum256(b)
			digest = hex.EncodeToString(sum[:])
		} else if !os.IsNotExist(err) {
			return err
		}
		p.deps = append(p.deps, Dependency{name, digest})
		return nil
	}
	for _, name := range options.ConfigPaths {
		if IsLockfile(name) {
			continue
		}
		if err := addDep(name); err != nil {
			return nil, err
		}
	}
	if err := addDep(".gitignore"); err != nil {
		return nil, err
	}
	// A gitfile may redirect to an external linked-worktree index.
	if info, err := os.Lstat(filepath.Join(root, ".git")); err == nil && !info.IsDir() {
		if err := addDep(".git"); err != nil {
			return nil, err
		}
	}
	btr.Mark("declare_config_deps", fmt.Sprintf("deps=%d", len(p.deps)))
	// Still declared though the identities no longer hash them: declaring is what
	// registers the watch and what the run's input fence re-reads.
	st, dirs, err := discoverGit(root)
	if err != nil {
		return nil, err
	}
	p.git, p.gitDirs = st, dirs
	btr.Mark("discover_git", fmt.Sprintf("repo=%v dirs=%d", st.Repository, len(dirs)))
	if st.Repository {
		for _, dir := range dirs {
			for _, n := range gitControlNames {
				if err := addDep(filepath.Join(dir, n)); err != nil {
					return nil, err
				}
			}
		}
		btr.Mark("declare_git_control", fmt.Sprintf("deps=%d", len(p.deps)))
		tracked, trackedDirs, e := indexNames(root)
		if e != nil {
			return nil, e
		}
		p.tracked, p.trackedDirs = tracked, trackedDirs
		btr.Mark("git_ls_files", fmt.Sprintf("tracked=%d dirs=%d", len(tracked), len(trackedDirs)))
	}
	var names []string
	var ignoreFiles []string
	err = filepath.WalkDir(root, func(abs string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := p.relative(abs)
		if rel == "." {
			return nil
		}
		if p.hard(rel) != "" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		p.entries[rel] = entry.IsDir()
		names = append(names, rel)
		if entry.Name() == ".gitignore" && entry.Type()&os.ModeSymlink == 0 {
			ignoreFiles = append(ignoreFiles, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	btr.Mark("walk_tree", fmt.Sprintf("names=%d ignorefiles=%d", len(names), len(ignoreFiles)))
	// The temp repository is torn down by the deferred RemoveAll once Build has
	// returned, so its removal is after this trace's last Mark and inside the
	// caller's graphinput_build span.
	temp, err := os.MkdirTemp("", "enola-graph-ignore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	if _, err := runGit(root, "", nil, "init", "--bare", "--quiet", "--template=", temp); err != nil {
		return nil, err
	}
	btr.Mark("check_ignore_setup", "")
	if len(names) > 0 {
		out, err := runGit(root, temp, []byte(strings.Join(names, "\x00")+"\x00"), "check-ignore", "--no-index", "-z", "--stdin")
		if err != nil {
			if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 1 {
				return nil, fmt.Errorf("git check-ignore: %w", err)
			}
		}
		for _, name := range strings.Split(string(out), "\x00") {
			if name != "" {
				p.ignored[strings.TrimSuffix(name, "/")] = true
			}
		}
	}
	btr.Mark("check_ignore_run", fmt.Sprintf("names=%d ignored=%d", len(names), len(p.ignored)))
	p.walkIgnoreFiles = append([]string(nil), ignoreFiles...)
	for _, name := range ignoreFiles {
		if !p.gitIgnored(filepath.ToSlash(filepath.Dir(name))) {
			if err := addDep(name); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(p.deps, func(i, j int) bool { return p.deps[i].Path < p.deps[j].Path })
	btr.Mark("declare_ignore_files", fmt.Sprintf("deps=%d", len(p.deps)))
	p.options = options
	if err := p.computeIdentities(); err != nil {
		return nil, err
	}
	btr.Mark("compute_identities", fmt.Sprintf("deps=%d entries=%d", len(p.deps), len(p.entries)))
	return p, nil
}

// config.worktree carries core.worktree under extensions.worktreeConfig, so a
// linked worktree can be redirected without config ever being touched; a linked
// worktree's git dir has no config file of its own at all.
var gitControlNames = []string{"index", "HEAD", "config", "config.worktree"}

// controlFile reports whether a dependency is a Git control file under a
// discovered git dir. The gitfile at <root>/.git is deliberately excluded: it
// redirects discovery, so its bytes are an input to the projection.
func (p *Policy) controlFile(path string) bool {
	for _, dir := range p.gitDirs {
		for _, n := range gitControlNames {
			if path == filepath.Join(dir, n) {
				return true
			}
		}
	}
	return false
}

// semanticDeps drops the Git control files from what the identities hash. They
// stay declared, so the watcher and the input fence still see them; what they
// stop being is identity. An index stat refresh, a lock-only staging, or an
// unrelated configuration key must not move the policy identity.
func (p *Policy) semanticDeps() []Dependency {
	out := []Dependency{}
	for _, d := range p.deps {
		if p.controlFile(d.Path) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// policyMemo caches the two pure decision functions for the span of a single
// identity computation. hard and gitIgnored read only entries, states, exclude,
// caches and ignored, and Build fills all five before it computes identities, so
// within that span each name has one answer and computing it twice is waste.
//
// The cache is transaction-local on purpose: it is created per computation and
// discarded with it, so it cannot outlive the immutability it relies on. A nil
// memo evaluates directly, which is what the exported callers still do.
//
// The redundancy this removes is not incidental. computeIdentities evaluates
// hard over every tracked name, then trackedAdmission evaluates it over every
// tracked name again and walks each name's ancestor directories, which revisits
// the same directories once per tracked file beneath them.
type policyMemo struct {
	hard   map[string]string
	git    map[string]bool
	walked map[string]bool
}

func newPolicyMemo() *policyMemo {
	return &policyMemo{hard: map[string]string{}, git: map[string]bool{}, walked: map[string]bool{}}
}

func (p *Policy) hardMemo(m *policyMemo, name string) string {
	if m == nil {
		return p.hard(name)
	}
	if why, ok := m.hard[name]; ok {
		return why
	}
	why := p.hard(name)
	m.hard[name] = why
	return why
}

func (p *Policy) gitIgnoredMemo(m *policyMemo, name string) bool {
	if m == nil {
		return p.gitIgnored(name)
	}
	if ign, ok := m.git[name]; ok {
		return ign
	}
	ign := p.gitIgnored(name)
	m.git[name] = ign
	return ign
}

// trackedAdmission returns the tracked entries whose index membership can move a
// decision: the ones Git ignores and no hard rule already excludes. A hard
// exclusion is evaluated before the override and wins over it, so a tracked
// lockfile, state directory or Enola-excluded path entering or leaving the index
// changes nothing and must not change the digest.
//
// Directories stay in their own list because the override itself distinguishes
// them - it exempts a directory query only - so a name that is a tracked
// directory is not interchangeable with the same name as a tracked file. A
// directory earns its place from the tracked files under it rather than from
// p.trackedDirs, which holds an ancestor of every tracked name including the
// hard-excluded ones: a gitignored directory whose only tracked descendants are
// hard-excluded exempts nothing, since every file under it is already excluded
// by that hard rule or by gitignore with no tracking to override it.
//
// The directory set is published as p.admitDirs and is the set Classify itself
// consults, so the two cannot drift. That is what makes the digest stand for the
// decision function rather than merely for the leaf decisions: staging a build
// artifact under an ignored directory moves neither.
func (p *Policy) trackedAdmission() (files, dirs []string) {
	return p.trackedAdmissionWith(newPolicyMemo())
}

// trackedAdmissionWith is trackedAdmission sharing one memo with the caller, so
// the hard evaluations computeIdentities already paid for are not repeated here.
func (p *Policy) trackedAdmissionWith(m *policyMemo) (files, dirs []string) {
	files = []string{}
	dirSet := map[string]bool{}
	for name := range p.tracked {
		if p.hardMemo(m, name) != "" {
			continue
		}
		if p.gitIgnoredMemo(m, name) {
			files = append(files, name)
		}
		for dir := filepath.ToSlash(filepath.Dir(name)); dir != "."; dir = filepath.ToSlash(filepath.Dir(dir)) {
			// Deciding a directory is deterministic and idempotent, and a pass
			// that reached this directory continued from it up to the root, so
			// every ancestor above it was decided by that pass too. Stopping at
			// the first already-walked directory therefore visits each ancestor
			// chain once without changing which directories dirSet ends up with.
			if m != nil {
				if m.walked[dir] {
					break
				}
				m.walked[dir] = true
			}
			if p.trackedDirs[dir] && p.hardMemo(m, dir) == "" && p.gitIgnoredMemo(m, dir) {
				dirSet[dir] = true
			}
		}
	}
	dirs = []string{}
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}
	sort.Strings(files)
	sort.Strings(dirs)
	p.admitDirs = dirSet
	return files, dirs
}

// computeIdentities fills both digests from the policy's current inputs.
func (p *Policy) computeIdentities() error {
	semanticDeps := p.semanticDeps()
	memo := newPolicyMemo()
	tracked := []string{}
	for name := range p.tracked {
		if p.hardMemo(memo, name) == "" {
			tracked = append(tracked, name)
		}
	}
	sort.Strings(tracked)
	encoded, err := json.Marshal(struct {
		Version      string
		Options      Options
		Dependencies []Dependency
		Git          gitState
		Tracked      []string
	}{"graph-input-v2", p.options, semanticDeps, p.git, tracked})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	p.identity = hex.EncodeToString(sum[:])
	admittedFiles, admittedDirs := p.trackedAdmissionWith(memo)
	// Both identities carry the projection. They differ in what they take from
	// the index - the raw identity hashes every tracked name, the admission
	// identity only the names that can move a decision - but a repository whose
	// discovery moved is a different repository to both of them, and that is
	// the conservative side of an unknown Git effect.
	encoded, err = json.Marshal(struct {
		Version             string
		Options             Options
		Dependencies        []Dependency
		Git                 gitState
		TrackedIgnoredFiles []string
		TrackedIgnoredDirs  []string
	}{"graph-input-admission-v2", p.options, semanticDeps, p.git, admittedFiles, admittedDirs})
	if err != nil {
		return err
	}
	sum = sha256.Sum256(encoded)
	p.admission = hex.EncodeToString(sum[:])
	return nil
}

// RecheckDeclaredInputs re-reads this policy's declared inputs and reports the
// identity pair they produce now. It is a fence, not a rebuild, and its name is
// the whole of its claim: it re-reads the dependencies this policy declared and
// re-enumerates the index, and it reuses the directory walk and the ignore
// evaluation this policy already performed. A rule file that did not exist when
// this policy was built is not consulted here.
//
// That is sound for the caller it exists for - one comparing the pair against
// Identity and AdmissionIdentity before writing bookkeeping on a run that
// published nothing. A declared rule file, meaning the .gitignore files, the
// configuration paths and the Git control files, is re-read; membership is
// re-enumerated rather than trusted; Git discovery is re-run rather than
// carried over, so a worktree or git-dir redirect taking effect inside this
// window moves the pair; and an undeclared rule file is a new admitted file in
// the tree, so it moves that caller's inventory and the run never reaches the
// decision this fence guards. A repository that has disappeared since the
// policy was built declines through a false ok rather than reporting the
// identities of a tree with no index, which would compare equal for the wrong
// reason.
//
// Re-reading the control files no longer moves the pair by itself; their bytes
// are held by the run's own input fence instead.
//
// This policy is left unchanged either way.
func (p *Policy) RecheckDeclaredInputs() (identity, admission string, ok bool, err error) {
	q := *p
	q.deps = nil
	q.depSet = map[string]bool{}
	q.tracked = map[string]bool{}
	q.trackedDirs = map[string]bool{}
	for _, d := range p.deps {
		if q.depSet[d.Path] {
			continue
		}
		q.depSet[d.Path] = true
		b, rerr := os.ReadFile(d.Path)
		digest := "missing"
		if rerr == nil {
			sum := sha256.Sum256(b)
			digest = hex.EncodeToString(sum[:])
		} else if !os.IsNotExist(rerr) {
			return "", "", false, rerr
		}
		q.deps = append(q.deps, Dependency{d.Path, digest})
	}
	// Re-run rather than carried over: the identities read discovery only through
	// the projection now, so re-reading the declared bytes would not see a
	// redirect. q.gitDirs stays the built set, since it classifies the declared
	// paths being re-read.
	st, _, derr := discoverGit(p.root)
	if derr != nil {
		return "", "", false, derr
	}
	if !st.Repository && len(p.gitDirs) > 0 {
		return "", "", false, nil
	}
	q.git = st
	if st.Repository {
		tracked, trackedDirs, e := indexNames(p.root)
		if e != nil {
			return "", "", false, e
		}
		q.tracked, q.trackedDirs = tracked, trackedDirs
	}
	if err := q.computeIdentities(); err != nil {
		return "", "", false, err
	}
	return q.identity, q.admission, true, nil
}

func runGit(root, gitDir string, input []byte, args ...string) ([]byte, error) {
	prefix := []string{"-c", "core.excludesFile=" + os.DevNull, "-c", "core.ignoreCase=false"}
	if gitDir != "" {
		prefix = append(prefix, "--git-dir="+gitDir, "--work-tree="+root)
	}
	cmd := exec.Command("git", append(prefix, args...)...)
	cmd.Dir = root
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Stdin = bytes.NewReader(input)
	return cmd.Output()
}

// ClassifyDependency is for an explicitly declared content dependency, including
// external config extends/includes. It never revives an in-root exclusion. The
// caller owns tracking these external contents and reconciling their changes;
// ConfigPaths describes policy configuration, not arbitrary extraction side inputs.
func (p *Policy) ClassifyDependency(name string) Decision {
	if IsLockfile(name) {
		return Decision{Excluded, true, "lockfile"}
	}
	if _, ok := p.relative(name); !ok {
		for _, part := range strings.Split(filepath.ToSlash(name), "/") {
			if part == ".git" || part == ".hg" || part == ".svn" {
				return Decision{Excluded, true, "vcs"}
			}
		}
		return Decision{Semantic, true, "explicit external dependency"}
	}
	d := p.Classify(name, false)
	if d.Kind == NameOnly {
		d.Kind = Semantic
		d.Reason = "explicit content dependency"
	}
	return d
}

// WithConservativeMedia returns an immutable promotion for unaudited consumers.
// Existing snapshots remain valid; shared state is private and never mutated.
func (p *Policy) WithConservativeMedia() *Policy {
	if p.conservative {
		return p
	}
	q := *p
	q.conservative = true
	sum := sha256.Sum256([]byte(p.identity + "/conservative-media"))
	q.identity = hex.EncodeToString(sum[:])
	return &q
}
