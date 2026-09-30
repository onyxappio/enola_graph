package analyzerplugin

import (
	"fmt"
	"runtime/debug"

	_ "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

//go:generate go run gen_grammar.go

const treeSitterTypeScriptModule = "github.com/tree-sitter/tree-sitter-typescript"

// HostGrammarIdentity identifies the pinned TypeScript and TSX parser/scanner
// sources compiled into the host. The digest is generated at development time,
// so released binaries do not require Go or a module cache on the user's host.
type HostGrammarIdentity struct {
	Label  string
	Digest string
}

// HostGrammar returns the content identity embedded in this build. Normal
// executable build metadata also guards against a dependency upgrade or local
// replacement without regenerating the identity. Go test binaries may omit
// dependency metadata; the source/digest regression checks the pin in that case.
func HostGrammar() (HostGrammarIdentity, error) {
	info, _ := debug.ReadBuildInfo()
	return hostGrammarForBuild(info)
}

func hostGrammarForBuild(info *debug.BuildInfo) (HostGrammarIdentity, error) {
	if info != nil {
		for _, dep := range info.Deps {
			if dep.Path != treeSitterTypeScriptModule {
				continue
			}
			if dep.Replace != nil || dep.Version != pinnedGrammarVersion || dep.Sum != pinnedGrammarModuleSum {
				return HostGrammarIdentity{}, fmt.Errorf("host grammar dependency differs from generated identity; regenerate and verify the pinned grammar")
			}
		}
	}
	return HostGrammarIdentity{
		Label:  "tree-sitter-typescript@" + pinnedGrammarVersion,
		Digest: pinnedGrammarDigest,
	}, nil
}

// GrammarHelloValue is the grammar identity advertised at plugin hello.
func GrammarHelloValue() (string, error) {
	g, err := HostGrammar()
	if err != nil {
		return "", err
	}
	return g.Label + "#" + g.Digest, nil
}
