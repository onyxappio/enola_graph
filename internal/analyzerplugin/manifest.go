// Package analyzerplugin loads and validates repository-owned graph analyzer
// plugins. The package deliberately owns the protocol and trust boundary only;
// it contains no Product-specific recognizers.
package analyzerplugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/graphinput"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion         = "enola.plugin/v1"
	GoAPIVersion       = "enola.plugin/v2"
	FSMVocabularyV1    = "enola.fsm@1"
	HookAnalysisPlanV1 = "analysis.plan@1"
	HookAnalysisUnitV1 = "analysis.unit@1"
	DefaultMaxFrame    = 64 << 20
	DefaultTimeoutMS   = 60_000
	DefaultHelloMS     = 2_000
	DefaultUnitMS      = 10_000
	DefaultConcurrency = 4
)

// Config is a repository-owned registration. PluginConfig is opaque to Enola,
// but its canonical JSON bytes are included in the plugin identity.
type Config struct {
	Path   string         `yaml:"path" json:"path"`
	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

// Runtime declares the executable contract used to start the plugin bundle.
type Runtime struct {
	Kind    string `yaml:"kind" json:"kind"`
	Version string `yaml:"version" json:"version"`
	Entry   string `yaml:"entry" json:"entry"`
}

type Claims struct {
	Machines []string `yaml:"machines,omitempty" json:"machines,omitempty"`
}

type CandidateSpec struct {
	Include              []string `yaml:"include,omitempty" json:"include,omitempty"`
	PrefilterContainsAny []string `yaml:"prefilter_contains_any,omitempty" json:"prefilter_contains_any,omitempty"`
}

type Limits struct {
	HelloTimeoutMS int `yaml:"hello_timeout_ms,omitempty" json:"hello_timeout_ms,omitempty"`
	UnitTimeoutMS  int `yaml:"unit_timeout_ms,omitempty" json:"unit_timeout_ms,omitempty"`
	RunTimeoutMS   int `yaml:"run_timeout_ms,omitempty" json:"run_timeout_ms,omitempty"`
	MaxFrameBytes  int `yaml:"max_frame_bytes,omitempty" json:"max_frame_bytes,omitempty"`
	Concurrency    int `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
}

// Manifest is the strict on-disk enola-plugin.yaml schema.
type Manifest struct {
	API           string        `yaml:"api" json:"api"`
	Name          string        `yaml:"name" json:"name"`
	Version       string        `yaml:"version,omitempty" json:"version,omitempty"`
	Runtime       Runtime       `yaml:"runtime" json:"runtime"`
	IdentityFiles []string      `yaml:"identity_files" json:"identity_files"`
	Vocabularies  []string      `yaml:"vocabularies" json:"vocabularies"`
	Claims        Claims        `yaml:"claims,omitempty" json:"claims,omitempty"`
	OwnerDomain   []string      `yaml:"owner_domain" json:"owner_domain"`
	Hooks         []string      `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Candidates    CandidateSpec `yaml:"candidates,omitempty" json:"candidates,omitempty"`
	Limits        Limits        `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// Loaded is a validated registration with paths rooted in the repository.
type Loaded struct {
	Config        Config
	Manifest      Manifest
	Dir           string
	Entry         string
	EntryBytes    []byte // exact bundle bytes hashed into Identity; Start must execute these
	EntryDigest   string
	Runtime       string
	RuntimeDigest string
	Identity      string
	IdentityFiles []string
}

// Load validates every configured plugin before graph state or broker activity.
// runtimeCache is optional durable state: when present, unchanged executable
// metadata reuses the prior digest instead of re-hashing bytes.
func Load(repo string, entries []Config, runtimeCache RuntimeCache) ([]Loaded, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	repo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	loaded := make([]Loaded, 0, len(entries))
	names := map[string]bool{}
	machines := map[string]string{}
	for i, entry := range entries {
		if strings.TrimSpace(entry.Path) == "" {
			return nil, fmt.Errorf("analyzer_plugins[%d].path is required", i)
		}
		dir, err := confinedPath(repo, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("analyzer_plugins[%d].path: %w", i, err)
		}
		manifestPath := filepath.Join(dir, "enola-plugin.yaml")
		manifestBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("plugin at %q has no readable enola-plugin.yaml: %w", entry.Path, err)
		}
		var m Manifest
		dec := yaml.NewDecoder(bytes.NewReader(manifestBytes))
		dec.KnownFields(true)
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("plugin %q manifest: %w", entry.Path, err)
		}
		if err := validateManifest(&m); err != nil {
			return nil, fmt.Errorf("plugin %q manifest: %w", entry.Path, err)
		}
		if names[m.Name] {
			return nil, fmt.Errorf("duplicate analyzer plugin name %q", m.Name)
		}
		names[m.Name] = true
		if graphinput.IsLockfile(m.Runtime.Entry) {
			return nil, fmt.Errorf("plugin %q runtime.entry cannot be a dependency lockfile", m.Name)
		}
		for _, machine := range m.Claims.Machines {
			if prior := machines[machine]; prior != "" {
				return nil, fmt.Errorf("plugins %q and %q both claim machine %q", prior, m.Name, machine)
			}
			machines[machine] = m.Name
		}
		entryPath, err := confinedPath(dir, m.Runtime.Entry)
		if err != nil {
			return nil, fmt.Errorf("plugin %q runtime.entry: %w", m.Name, err)
		}
		if _, err := os.Stat(entryPath); err != nil {
			return nil, fmt.Errorf("plugin %q entry %q: %w", m.Name, m.Runtime.Entry, err)
		}
		entryBytes, err := os.ReadFile(entryPath)
		if err != nil {
			return nil, fmt.Errorf("plugin %q entry %q: %w", m.Name, m.Runtime.Entry, err)
		}
		if m.Runtime.Kind == "node" && embedsTypeScriptCompiler(entryBytes) {
			return nil, fmt.Errorf("plugin %q bundle appears to embed the TypeScript compiler package", m.Name)
		}
		// Snapshot every identity-file byte once. identityDigest must hash these
		// snapshots, never reread mutable paths after EntryBytes is captured.
		identityFiles := []string{manifestPath, entryPath}
		identityBytes := map[string][]byte{
			manifestPath: append([]byte(nil), manifestBytes...),
			entryPath:    append([]byte(nil), entryBytes...),
		}
		for _, rel := range m.IdentityFiles {
			if graphinput.IsLockfile(rel) {
				return nil, fmt.Errorf("plugin %q identity file %q cannot be a dependency lockfile", m.Name, rel)
			}
			p, err := confinedPath(dir, rel)
			if err != nil {
				return nil, fmt.Errorf("plugin %q identity file %q: %w", m.Name, rel, err)
			}
			if _, ok := identityBytes[p]; ok {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("plugin %q identity file %q: %w", m.Name, rel, err)
			}
			identityFiles = append(identityFiles, p)
			identityBytes[p] = b
		}
		runtimePath, runtimeDigest := "", ""
		if m.Runtime.Kind == "node" {
			runtimePath, err = exec.LookPath("node")
			if err != nil {
				return nil, fmt.Errorf("plugin %q requires Node %s: %w", m.Name, m.Runtime.Version, err)
			}
			runtimePath, err = filepath.Abs(runtimePath)
			if err != nil {
				return nil, err
			}
			runtimeDigest, _, err = CachedRuntimeDigest(runtimePath, runtimeCache)
			if err != nil {
				return nil, fmt.Errorf("plugin %q runtime fingerprint: %w", m.Name, err)
			}
		}
		identity, err := identityDigest(m, entry.Config, identityBytes, runtimeDigest)
		if err != nil {
			return nil, fmt.Errorf("plugin %q identity: %w", m.Name, err)
		}
		entrySum := sha256.Sum256(entryBytes)
		loaded = append(loaded, Loaded{
			Config: entry, Manifest: m, Dir: dir, Entry: entryPath,
			EntryBytes: append([]byte(nil), entryBytes...), EntryDigest: hex.EncodeToString(entrySum[:]),
			Runtime: runtimePath, RuntimeDigest: runtimeDigest, Identity: identity,
			IdentityFiles: identityFiles,
		})
	}
	return loaded, nil
}

func validateManifest(m *Manifest) error {
	if m.API != APIVersion && m.API != GoAPIVersion {
		return fmt.Errorf("api must be %q or %q", APIVersion, GoAPIVersion)
	}
	if !validID(m.Name) {
		return fmt.Errorf("name must be a stable identifier")
	}
	switch m.API {
	case APIVersion:
		if m.Runtime.Kind != "node" || strings.TrimSpace(m.Runtime.Version) == "" || strings.TrimSpace(m.Runtime.Entry) == "" {
			return errors.New("enola.plugin/v1 runtime must declare kind: node, an exact version, and entry")
		}
	case GoAPIVersion:
		if m.Runtime.Kind != "go-executable" || strings.TrimSpace(m.Runtime.Entry) == "" {
			return errors.New("enola.plugin/v2 runtime must declare kind: go-executable and entry")
		}
	}
	if len(m.IdentityFiles) == 0 {
		return errors.New("identity_files must include the executed bundle")
	}
	if m.API == APIVersion && len(m.Vocabularies) == 0 {
		return errors.New("vocabularies is required")
	}
	seen := map[string]bool{}
	for _, v := range m.Vocabularies {
		if v != FSMVocabularyV1 || (m.API == GoAPIVersion && v != FSMVocabularyV1) {
			return fmt.Errorf("unsupported vocabulary %q", v)
		}
		if seen[v] {
			return fmt.Errorf("duplicate vocabulary %q", v)
		}
		seen[v] = true
	}
	seenHooks := map[string]bool{}
	for _, hook := range m.Hooks {
		if !validHookID(hook) {
			return fmt.Errorf("invalid hook ID %q", hook)
		}
		if seenHooks[hook] {
			return fmt.Errorf("duplicate hook ID %q", hook)
		}
		seenHooks[hook] = true
	}
	if m.API == GoAPIVersion {
		for _, hook := range []string{HookAnalysisPlanV1, HookAnalysisUnitV1} {
			if !seenHooks[hook] {
				return fmt.Errorf("enola.plugin/v2 must declare hook %q", hook)
			}
		}
	}
	if len(m.OwnerDomain) == 0 {
		return errors.New("owner_domain must contain at least one admitted repository glob")
	}
	for _, glob := range append(append([]string{}, m.OwnerDomain...), m.Candidates.Include...) {
		if err := validateRepoGlob(glob); err != nil {
			return err
		}
	}
	sort.Strings(m.IdentityFiles)
	sort.Strings(m.Vocabularies)
	sort.Strings(m.Hooks)
	sort.Strings(m.Claims.Machines)
	sort.Strings(m.OwnerDomain)
	sort.Strings(m.Candidates.Include)
	sort.Strings(m.Candidates.PrefilterContainsAny)
	if m.Limits.HelloTimeoutMS == 0 {
		m.Limits.HelloTimeoutMS = DefaultHelloMS
	}
	if m.Limits.UnitTimeoutMS == 0 {
		m.Limits.UnitTimeoutMS = DefaultUnitMS
	}
	if m.Limits.RunTimeoutMS == 0 {
		m.Limits.RunTimeoutMS = DefaultTimeoutMS
	}
	if m.Limits.MaxFrameBytes == 0 {
		m.Limits.MaxFrameBytes = DefaultMaxFrame
	}
	if m.Limits.Concurrency == 0 {
		m.Limits.Concurrency = DefaultConcurrency
	}
	if m.Limits.HelloTimeoutMS < 1 || m.Limits.UnitTimeoutMS < 1 || m.Limits.RunTimeoutMS < 1 || m.Limits.MaxFrameBytes < 1024 || m.Limits.Concurrency < 1 || m.Limits.Concurrency > 64 {
		return errors.New("limits are outside supported bounds")
	}
	return nil
}

func validHookID(hook string) bool {
	name, version, ok := strings.Cut(hook, "@")
	if !ok || name == "" || version == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	for _, r := range version {
		if r < '0' || r > '9' {
			return false
		}
	}
	return version != "0"
}

func validateRepoGlob(glob string) error {
	if strings.TrimSpace(glob) == "" || filepath.IsAbs(glob) || strings.Contains(glob, "\\") {
		return fmt.Errorf("invalid repository glob %q", glob)
	}
	for _, part := range strings.Split(glob, "/") {
		if part == ".." {
			return fmt.Errorf("repository glob %q escapes the repository", glob)
		}
	}
	if _, err := filepath.Match(glob, ""); err != nil && !strings.Contains(glob, "**") {
		return fmt.Errorf("invalid repository glob %q: %w", glob, err)
	}
	return nil
}

func validID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}

func confinedPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path %q is not allowed", rel)
	}
	joined := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	joinedAbs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(base, joinedAbs)
	if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes its root", rel)
	}
	// Existing symlinks must not move a plugin entry or identity input outside
	// its declared repository/plugin directory.
	resolvedRoot, rootErr := filepath.EvalSymlinks(base)
	resolvedPath, pathErr := filepath.EvalSymlinks(joinedAbs)
	if rootErr == nil && pathErr == nil {
		relPath, err = filepath.Rel(resolvedRoot, resolvedPath)
		if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path %q resolves outside its root", rel)
		}
	}
	return joinedAbs, nil
}

func identityDigest(m Manifest, config map[string]any, fileBytes map[string][]byte, runtimeDigest string) (string, error) {
	var grammar HostGrammarIdentity
	if m.API == APIVersion {
		var err error
		grammar, err = HostGrammar()
		if err != nil {
			return "", fmt.Errorf("host grammar identity: %w", err)
		}
	}
	return identityDigestWithGrammar(m, config, fileBytes, runtimeDigest, grammar)
}

func identityDigestWithGrammar(m Manifest, config map[string]any, fileBytes map[string][]byte, runtimeDigest string, grammar HostGrammarIdentity) (string, error) {
	h := sha256.New()
	write := func(b []byte) { _, _ = h.Write([]byte(fmt.Sprintf("%d:", len(b)))); _, _ = h.Write(b) }
	mb, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	write(mb)
	write(cb)
	write([]byte(runtimeDigest))
	if m.API == APIVersion {
		write([]byte(grammar.Label))
		write([]byte(grammar.Digest))
	}
	paths := make([]string, 0, len(fileBytes))
	for p := range fileBytes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		write([]byte(filepath.ToSlash(p)))
		write(fileBytes[p])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func embedsTypeScriptCompiler(b []byte) bool {
	s := strings.ToLower(string(b))
	for _, marker := range []string{
		`from"typescript"`, `from 'typescript'`, `from"typescript/`, `from 'typescript/`,
		`require("typescript")`, `require('typescript')`, `import("typescript")`, `import('typescript')`,
		`@microsoft/typescript`,
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// Trusted reports whether an operator allow-list explicitly authorizes name.
// The configured repository can never grant itself trust.
func Trusted(allowList []string, name string) bool {
	for _, v := range allowList {
		if v == name {
			return true
		}
	}
	return false
}

// ConfigFingerprint is empty when no analyzer plugins are configured, keeping
// the pre-plugin config hash byte-for-byte stable. Otherwise it fingerprints
// the ordered-by-name registration set and opaque per-plugin config.
func ConfigFingerprint(entries []Config) string {
	if len(entries) == 0 {
		return ""
	}
	cp := append([]Config(nil), entries...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Path < cp[j].Path })
	b, err := json.Marshal(cp)
	if err != nil {
		return "invalid:" + err.Error()
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// AdmittedPath applies the common graph input policy to a plugin callback path.
// Callers still have to check membership in their captured inventory.
func AdmittedPath(path string) bool {
	return !graphinput.IsLockfile(path) && !filepath.IsAbs(path) && !strings.HasPrefix(filepath.Clean(path), "..")
}
