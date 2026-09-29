package analyzerplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	// Keep tree-sitter-typescript among this package's module dependencies so
	// HostGrammar can resolve the exact pinned dialect sources.
	_ "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

const treeSitterTypeScriptModule = "github.com/tree-sitter/tree-sitter-typescript"

// HostGrammarIdentity is the exact pinned TypeScript grammar the host uses.
// The host compiles tree-sitter-typescript dialect sources (parser.c/scanner.c);
// their content digest is part of plugin identity and hello negotiation.
type HostGrammarIdentity struct {
	Label  string // e.g. tree-sitter-typescript@v0.23.2
	Digest string // sha256 over sorted dialect grammar source bytes
}

var (
	hostGrammarOnce sync.Once
	hostGrammar     HostGrammarIdentity
	hostGrammarErr  error
)

// HostGrammar returns the pinned host TypeScript grammar label and content
// digest. The result is cached process-wide.
func HostGrammar() (HostGrammarIdentity, error) {
	hostGrammarOnce.Do(func() {
		hostGrammar, hostGrammarErr = loadHostGrammarIdentity()
	})
	return hostGrammar, hostGrammarErr
}

// GrammarHelloValue is the grammar string advertised at plugin hello.
func GrammarHelloValue() (string, error) {
	g, err := HostGrammar()
	if err != nil {
		return "", err
	}
	return g.Label + "#" + g.Digest, nil
}

func loadHostGrammarIdentity() (HostGrammarIdentity, error) {
	version, dir, err := treeSitterTypeScriptModuleDir()
	if err != nil {
		return HostGrammarIdentity{}, err
	}
	// Logical relative names keep the digest content-bound and portable across
	// module-cache install paths. Absolute paths would encode GOMODCACHE.
	artifacts := []struct {
		logical string
		path    string
	}{
		{"typescript/src/parser.c", filepath.Join(dir, "typescript", "src", "parser.c")},
		{"typescript/src/scanner.c", filepath.Join(dir, "typescript", "src", "scanner.c")},
		{"tsx/src/parser.c", filepath.Join(dir, "tsx", "src", "parser.c")},
		{"tsx/src/scanner.c", filepath.Join(dir, "tsx", "src", "scanner.c")},
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].logical < artifacts[j].logical })
	h := sha256.New()
	for _, art := range artifacts {
		b, err := os.ReadFile(art.path)
		if err != nil {
			return HostGrammarIdentity{}, fmt.Errorf("read pinned grammar artifact %s: %w", art.logical, err)
		}
		_, _ = h.Write([]byte(art.logical))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	digest := hex.EncodeToString(h.Sum(nil))
	label := treeSitterTypeScriptModuleVersionLabel(version)
	return HostGrammarIdentity{Label: label, Digest: digest}, nil
}

func treeSitterTypeScriptModuleVersionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "tree-sitter-typescript"
	}
	return "tree-sitter-typescript@" + version
}

func treeSitterTypeScriptModuleDir() (version, dir string, err error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", fmt.Errorf("build info unavailable; cannot locate %s", treeSitterTypeScriptModule)
	}
	version = ""
	for _, d := range bi.Deps {
		if d.Path == treeSitterTypeScriptModule {
			version = d.Version
			break
		}
	}
	if version == "" {
		// Fall back to the replace/root module's go.mod require via main module
		// deps when running tests under the enola module path.
		for _, d := range bi.Deps {
			if strings.HasPrefix(d.Path, treeSitterTypeScriptModule) {
				version = d.Version
				break
			}
		}
	}
	if version == "" {
		return "", "", fmt.Errorf("module %s is not among build dependencies", treeSitterTypeScriptModule)
	}
	cache := os.Getenv("GOMODCACHE")
	if cache == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", "", fmt.Errorf("GOMODCACHE unset and home unavailable: %w", homeErr)
		}
		cache = filepath.Join(home, "go", "pkg", "mod")
	}
	dir = filepath.Join(cache, treeSitterTypeScriptModule+"@"+version)
	if st, statErr := os.Stat(dir); statErr != nil || !st.IsDir() {
		return "", "", fmt.Errorf("pinned grammar module dir %s: %w", dir, statErr)
	}
	return version, dir, nil
}

// GrammarPathsForTest lists the dialect grammar source paths used for the host
// digest. Exported for tests only.
func GrammarPathsForTest() ([]string, error) {
	_, dir, err := treeSitterTypeScriptModuleDir()
	if err != nil {
		return nil, err
	}
	paths := []string{
		filepath.Join(dir, "typescript", "src", "parser.c"),
		filepath.Join(dir, "typescript", "src", "scanner.c"),
		filepath.Join(dir, "tsx", "src", "parser.c"),
		filepath.Join(dir, "tsx", "src", "scanner.c"),
	}
	sort.Strings(paths)
	return paths, nil
}
