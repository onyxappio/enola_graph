package analyzerplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"testing"
)

// Compute the content digest independently from the pinned build dependency.
// This catches a stale generated identity even when go test omits build info.
func TestEmbeddedGrammarMatchesPinnedSources(t *testing.T) {
	b, err := exec.Command("go", "list", "-m", "-json", treeSitterTypeScriptModule).Output()
	if err != nil {
		t.Fatal(err)
	}
	var module struct {
		Version string
		Dir     string
		Sum     string
		Replace *json.RawMessage
	}
	if err := json.Unmarshal(b, &module); err != nil {
		t.Fatal(err)
	}
	if module.Replace != nil || module.Version != pinnedGrammarVersion || module.Sum != pinnedGrammarModuleSum {
		t.Fatal("grammar dependency changed; review it and run go generate ./internal/analyzerplugin")
	}
	h := sha256.New()
	for _, logical := range []string{"tsx/src/parser.c", "tsx/src/scanner.c", "typescript/src/parser.c", "typescript/src/scanner.c"} {
		b, err := os.ReadFile(filepath.Join(module.Dir, filepath.FromSlash(logical)))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = h.Write([]byte(logical))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != pinnedGrammarDigest {
		t.Fatalf("embedded grammar digest %s differs from compiled dependency sources %s", pinnedGrammarDigest, got)
	}
}

func TestHostGrammarWorksWithoutModuleCacheOrBuildMetadata(t *testing.T) {
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "missing-module-cache"))
	for _, info := range []*debug.BuildInfo{nil, {}, {Deps: []*debug.Module{{Path: treeSitterTypeScriptModule, Version: pinnedGrammarVersion, Sum: pinnedGrammarModuleSum}}}} {
		identity, err := hostGrammarForBuild(info)
		if err != nil {
			t.Fatal(err)
		}
		if identity.Label != "tree-sitter-typescript@"+pinnedGrammarVersion || identity.Digest != pinnedGrammarDigest {
			t.Fatalf("unexpected grammar identity: %+v", identity)
		}
	}
	if _, err := HostGrammar(); err != nil {
		t.Fatal(err)
	}
}

func TestHostGrammarRejectsStaleDependencyIdentity(t *testing.T) {
	for _, dependency := range []*debug.Module{
		{Path: treeSitterTypeScriptModule, Version: "v0.0.0", Sum: pinnedGrammarModuleSum},
		{Path: treeSitterTypeScriptModule, Version: pinnedGrammarVersion, Sum: "changed"},
		{Path: treeSitterTypeScriptModule, Version: pinnedGrammarVersion, Sum: pinnedGrammarModuleSum, Replace: &debug.Module{Path: "/local/grammar"}},
	} {
		if _, err := hostGrammarForBuild(&debug.BuildInfo{Deps: []*debug.Module{dependency}}); err == nil {
			t.Fatalf("unverified dependency accepted: %+v", dependency)
		}
	}
}
