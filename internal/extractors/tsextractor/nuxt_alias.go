package tsextractor

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
)

var (
	nuxtSrcDirLiteral = regexp.MustCompile(`\bsrcDir\s*:\s*['"]([^'"]+)['"]`)
	nuxtSrcDirField   = regexp.MustCompile(`\bsrcDir\s*:`)
	nuxtAliasBlock    = regexp.MustCompile(`\balias\s*:\s*\{([^}]*)\}`)
	nuxtAliasPair     = regexp.MustCompile(`['"](~|@|~~|@@)['"]\s*:\s*['"]([^'"]+)['"]`)
)

// withNuxtAliasFallbacks adds Nuxt's documented default aliases (~/@ → srcDir,
// ~~/@@ → rootDir) for each detected Nuxt package when those prefixes are not
// already present from tsconfig. Generated .nuxt/tsconfig.json is not required.
// Explicit tsconfig paths win. Static nuxt.config alias literals are applied
// next. A static srcDir literal sets the default source root; an explicit
// srcDir expression is not treated as "use the package root".
func withNuxtAliasFallbacks(ctx context.Context, repoPath string, roots []tsAliasRoot, pkgs []string, inputScopes ...*inputscope.Scope) []tsAliasRoot {
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
		cfg := readNuxtConfig(ctx, repoPath, pkg, inputScope)
		rootDir := pkg
		if rootDir != "" && !strings.HasSuffix(rootDir, "/") {
			rootDir += "/"
		}
		for prefix, repl := range cfg.aliases {
			target := joinNuxtAliasTarget(pkg, repl)
			if target != "" && !strings.HasSuffix(target, "/") {
				target += "/"
			}
			addNuxtAliasIfAbsent(roots[idx].aliases, prefix, target)
		}
		srcDir, srcKnown := nuxtSourceDir(ctx, repoPath, pkg, cfg, inputScope)
		if srcDir != "" && !strings.HasSuffix(srcDir, "/") {
			srcDir += "/"
		}
		if srcKnown {
			addNuxtAliasIfAbsent(roots[idx].aliases, "~/", srcDir)
			addNuxtAliasIfAbsent(roots[idx].aliases, "@/", srcDir)
		}
		addNuxtAliasIfAbsent(roots[idx].aliases, "~~/", rootDir)
		addNuxtAliasIfAbsent(roots[idx].aliases, "@@/", rootDir)
	}
	return roots
}

func joinNuxtAliasTarget(pkg, raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "./")
	if pkg == "" {
		return factpath.Clean(raw)
	}
	if raw == "" || raw == "." {
		return factpath.Clean(pkg)
	}
	return factpath.Clean(factpath.Join(pkg, raw))
}

func addNuxtAliasIfAbsent(aliases map[string]tsAlias, prefix, replacement string) {
	if _, exists := aliases[prefix]; exists {
		return
	}
	aliases[prefix] = tsAlias{replacement: replacement}
}

type nuxtConfigFacts struct {
	aliases          map[string]string
	srcDirLiteral    string
	srcDirUnknown    bool
	hasSrcDirLiteral bool
}

func readNuxtConfig(ctx context.Context, repoPath, pkg string, inputScopes ...*inputscope.Scope) nuxtConfigFacts {
	inputScope := inputscope.First(inputScopes)
	pkgAbs := repoPath
	if pkg != "" {
		pkgAbs = filepath.Join(repoPath, filepath.FromSlash(pkg))
	}
	out := nuxtConfigFacts{aliases: map[string]string{}}
	for _, name := range []string{"nuxt.config.ts", "nuxt.config.js", "nuxt.config.mjs"} {
		data, err := overlayReadFile(ctx, filepath.Join(pkgAbs, name), inputScope)
		if err != nil {
			continue
		}
		if m := nuxtSrcDirLiteral.FindSubmatch(data); m != nil {
			raw := strings.TrimSpace(string(m[1]))
			if raw != "" && !filepath.IsAbs(raw) && !strings.Contains(raw, "..") {
				out.srcDirLiteral = raw
				out.hasSrcDirLiteral = true
			} else {
				out.srcDirUnknown = true
			}
		} else if nuxtSrcDirField.FindIndex(data) != nil {
			out.srcDirUnknown = true
		}
		if block := nuxtAliasBlock.FindSubmatch(data); block != nil {
			for _, pair := range nuxtAliasPair.FindAllSubmatch(block[1], -1) {
				key := string(pair[1])
				val := strings.TrimSpace(string(pair[2]))
				if val == "" || filepath.IsAbs(val) || strings.Contains(val, "..") {
					continue
				}
				out.aliases[key+"/"] = val
			}
		}
		return out
	}
	return out
}

func nuxtSourceDir(ctx context.Context, repoPath, pkg string, cfg nuxtConfigFacts, inputScopes ...*inputscope.Scope) (string, bool) {
	inputScope := inputscope.First(inputScopes)
	if cfg.srcDirUnknown {
		return "", false
	}
	if cfg.hasSrcDirLiteral {
		if pkg == "" {
			return factpath.Clean(cfg.srcDirLiteral), true
		}
		return factpath.Clean(factpath.Join(pkg, cfg.srcDirLiteral)), true
	}
	pkgAbs := repoPath
	if pkg != "" {
		pkgAbs = filepath.Join(repoPath, filepath.FromSlash(pkg))
	}
	appDir := filepath.Join(pkgAbs, "app")
	if info, err := overlayStat(ctx, appDir, inputScope); err == nil && info.IsDir() {
		if pkg == "" {
			return "app", true
		}
		return factpath.Join(pkg, "app"), true
	}
	return pkg, true
}
