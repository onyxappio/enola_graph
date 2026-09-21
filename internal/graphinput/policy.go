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
	deps                      []Dependency
	depSet                    map[string]bool
	gitDirs                   []string
	identity                  string
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
	if p.gitIgnored(rel) && !p.tracked[rel] && !(directory && p.trackedDirs[rel]) {
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
func Build(root string, options Options) (*Policy, error) {
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
	git := func(args ...string) ([]byte, error) { return runGit(root, "", nil, args...) }
	if _, e := git("rev-parse", "--show-toplevel"); e == nil {
		for _, flag := range []string{"--absolute-git-dir", "--git-common-dir"} {
			out, e := git("rev-parse", flag)
			if e != nil {
				return nil, e
			}
			dir := strings.TrimSpace(string(out))
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, dir)
			}
			p.gitDirs = append(p.gitDirs, dir)
			for _, n := range []string{"index", "HEAD", "config"} {
				if err := addDep(filepath.Join(dir, n)); err != nil {
					return nil, err
				}
			}
		}
		out, e := git("ls-files", "-z", "--cached", "--", ".")
		if e != nil {
			return nil, e
		}
		for _, name := range strings.Split(string(out), "\x00") {
			if name == "" {
				continue
			}
			p.tracked[name] = true
			for dir := filepath.ToSlash(filepath.Dir(name)); dir != "."; dir = filepath.ToSlash(filepath.Dir(dir)) {
				p.trackedDirs[dir] = true
			}
		}
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
	temp, err := os.MkdirTemp("", "enola-graph-ignore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	if _, err := runGit(root, "", nil, "init", "--bare", "--quiet", "--template=", temp); err != nil {
		return nil, err
	}
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
	for _, name := range ignoreFiles {
		if !p.gitIgnored(filepath.ToSlash(filepath.Dir(name))) {
			if err := addDep(name); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(p.deps, func(i, j int) bool { return p.deps[i].Path < p.deps[j].Path })
	// Index bytes are a reconciliation signal, not identity: index stat refreshes and
	// lock-only staging must not change the graph policy identity.
	semanticDeps := []Dependency{}
	for _, d := range p.deps {
		control := false
		for _, dir := range p.gitDirs {
			if d.Path == filepath.Join(dir, "index") || d.Path == filepath.Join(dir, "HEAD") {
				control = true
			}
		}
		if !control {
			semanticDeps = append(semanticDeps, d)
		}
	}
	tracked := []string{}
	for name := range p.tracked {
		if p.hard(name) == "" {
			tracked = append(tracked, name)
		}
	}
	sort.Strings(tracked)
	encoded, err := json.Marshal(struct {
		Version      string
		Options      Options
		Dependencies []Dependency
		Tracked      []string
	}{"graph-input-v1", options, semanticDeps, tracked})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(encoded)
	p.identity = hex.EncodeToString(sum[:])
	return p, nil
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
