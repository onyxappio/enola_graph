package tsextractor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
)

var nuxtSrcDirLiteral = regexp.MustCompile(`\bsrcDir\s*:\s*['"]([^'"]+)['"]`)

// withNuxtAliasFallbacks adds Nuxt's documented default aliases (~/@ → srcDir,
// ~~/@@ → rootDir) for each detected Nuxt package when those prefixes are not
// already present from tsconfig. Generated .nuxt/tsconfig.json is not required.
// Explicit tsconfig paths and a static nuxt.config srcDir literal win; an
// expression srcDir is ignored (conservative default).
func withNuxtAliasFallbacks(repoPath string, roots []tsAliasRoot, pkgs []string, inputScopes ...*inputscope.Scope) []tsAliasRoot {
	inputScope := inputscope.First(inputScopes)
	if len(pkgs) == 0 {
		return roots
	}
	byDir := map[string]int{}
	for i := range roots {
		byDir[roots[i].dir] = i
		if roots[i].aliases == nil {
			roots[i].aliases = map[string]tsAlias{}
		}
	}
	for _, pkg := range pkgs {
		idx, ok := byDir[pkg]
		if !ok {
			roots = append(roots, tsAliasRoot{dir: pkg, aliases: map[string]tsAlias{}})
			idx = len(roots) - 1
			byDir[pkg] = idx
		}
		srcDir := nuxtSourceDir(repoPath, pkg, inputScope)
		rootDir := pkg
		if rootDir != "" && !strings.HasSuffix(rootDir, "/") {
			rootDir += "/"
		}
		if srcDir != "" && !strings.HasSuffix(srcDir, "/") {
			srcDir += "/"
		}
		addNuxtAliasIfAbsent(roots[idx].aliases, "~/", srcDir)
		addNuxtAliasIfAbsent(roots[idx].aliases, "@/", srcDir)
		addNuxtAliasIfAbsent(roots[idx].aliases, "~~/", rootDir)
		addNuxtAliasIfAbsent(roots[idx].aliases, "@@/", rootDir)
	}
	return roots
}

func addNuxtAliasIfAbsent(aliases map[string]tsAlias, prefix, replacement string) {
	if _, exists := aliases[prefix]; exists {
		return
	}
	aliases[prefix] = tsAlias{replacement: replacement}
}

func nuxtSourceDir(repoPath, pkg string, inputScopes ...*inputscope.Scope) string {
	inputScope := inputscope.First(inputScopes)
	pkgAbs := repoPath
	if pkg != "" {
		pkgAbs = filepath.Join(repoPath, filepath.FromSlash(pkg))
	}
	if src, ok := staticNuxtSrcDir(pkgAbs, pkg, inputScope); ok {
		return src
	}
	appDir := filepath.Join(pkgAbs, "app")
	if info, err := inputScope.Stat(appDir); err == nil && info.IsDir() {
		if pkg == "" {
			return "app"
		}
		return factpath.Join(pkg, "app")
	}
	return pkg
}

func staticNuxtSrcDir(pkgAbs, pkg string, inputScopes ...*inputscope.Scope) (string, bool) {
	inputScope := inputscope.First(inputScopes)
	for _, name := range []string{"nuxt.config.ts", "nuxt.config.js", "nuxt.config.mjs"} {
		data, err := inputScope.ReadFile(filepath.Join(pkgAbs, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		m := nuxtSrcDirLiteral.FindSubmatch(data)
		if m == nil {
			continue
		}
		raw := strings.TrimSpace(string(m[1]))
		if raw == "" || filepath.IsAbs(raw) || strings.Contains(raw, "..") {
			return "", false
		}
		if pkg == "" {
			return factpath.Clean(raw), true
		}
		return factpath.Clean(factpath.Join(pkg, raw)), true
	}
	return "", false
}
