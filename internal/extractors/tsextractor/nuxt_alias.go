package tsextractor

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/factpath"
)

var (
	nuxtSrcDirLiteral      = regexp.MustCompile(`\bsrcDir\s*:\s*['"]([^'"]+)['"]`)
	nuxtSrcDirField        = regexp.MustCompile(`\bsrcDir\s*:`)
	nuxtAliasBlock         = regexp.MustCompile(`\balias\s*:\s*\{([^}]*)\}`)
	nuxtAliasPair          = regexp.MustCompile(`['"](~|@|~~|@@)['"]\s*:\s*['"]([^'"]+)['"]`)
	nuxtRuntimeAliasAssign = regexp.MustCompile(`nuxt\.options\.alias\[\s*['"]([^'"]+)['"]\s*\]\s*=\s*([^\n;]+)`)
	nuxtResolveDecl        = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*[A-Za-z_$][\w$]*\.resolve\(\s*['"](\.[^'"]+)['"]\s*\)`)
	nuxtResolveCall        = regexp.MustCompile(`^[A-Za-z_$][\w$]*\.resolve\(\s*['"](\.[^'"]+)['"]\s*\)$`)
	nuxtRelativeLiteral    = regexp.MustCompile(`^['"](\.[^'"]+)['"]$`)
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

func nuxtRelativeAliasTarget(file, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	if !strings.HasPrefix(rel, ".") {
		return ""
	}
	return factpath.Clean(factpath.Join(factpath.Dir(file), rel))
}

// nuxtRuntimeAliasesFromFile reads statically assigned nuxt.options.alias
// entries whose right-hand side is a same-file resolver.resolve('./…') binding
// or a relative string literal. The replacement is the actual resolved
// directory of that expression, not a guess from the alias name.
func nuxtRuntimeAliasesFromFile(file string, src []byte) []string {
	if src == nil || !bytes.Contains(src, []byte("nuxt.options.alias")) {
		return nil
	}
	idents := map[string]string{}
	for _, m := range nuxtResolveDecl.FindAllSubmatch(src, -1) {
		if t := nuxtRelativeAliasTarget(file, string(m[2])); t != "" {
			idents[string(m[1])] = t
		}
	}
	seen := map[string]bool{}
	var out []string
	add := func(key, target string) {
		key = strings.TrimSpace(key)
		target = factpath.Clean(strings.TrimSpace(target))
		if key == "" || target == "" {
			return
		}
		pair := key + "=>" + target
		if seen[pair] {
			return
		}
		seen[pair] = true
		out = append(out, pair)
	}
	for _, m := range nuxtRuntimeAliasAssign.FindAllSubmatch(src, -1) {
		key := string(m[1])
		rhs := strings.TrimSpace(string(m[2]))
		if loc := nuxtResolveCall.FindStringSubmatch(rhs); loc != nil {
			if t := nuxtRelativeAliasTarget(file, loc[1]); t != "" {
				add(key, t)
			}
			continue
		}
		if loc := nuxtRelativeLiteral.FindStringSubmatch(rhs); loc != nil {
			if t := nuxtRelativeAliasTarget(file, loc[1]); t != "" {
				add(key, t)
			}
			continue
		}
		if t := idents[rhs]; t != "" {
			add(key, t)
		}
	}
	sort.Strings(out)
	return out
}

func applyNuxtRuntimeAliasPairs(aliases map[string]tsAlias, pairs []string) {
	if aliases == nil {
		return
	}
	for _, pair := range pairs {
		key, target, ok := strings.Cut(pair, "=>")
		if !ok || key == "" || target == "" {
			continue
		}
		if _, exists := aliases[key]; !exists {
			aliases[key] = tsAlias{replacement: target, exact: !strings.HasSuffix(key, "/")}
		}
		if strings.HasSuffix(key, "/") {
			continue
		}
		prefix := key + "/"
		repl := target
		if !strings.HasSuffix(repl, "/") {
			repl += "/"
		}
		addNuxtAliasIfAbsent(aliases, prefix, repl)
	}
}

func withNuxtRuntimeAliases(roots []tsAliasRoot, sources map[string][]byte, knownFiles map[string]bool, readSrc func(string) []byte, records map[string]*FileRecord, dirty map[string]bool, nuxtPkgs []string, pkgDirs map[string]bool, pkgDirByName map[string]string) []tsAliasRoot {
	if len(nuxtPkgs) == 0 {
		return roots
	}
	byPkg := map[string][]string{}
	seenPair := map[string]map[string]bool{}
	add := func(file string, pairs []string) {
		if len(pairs) == 0 {
			return
		}
		pkg, ok := nuxtPackageForFile(nuxtPkgs, file, pkgDirs)
		if !ok {
			return
		}
		for _, pair := range pairs {
			if seenPair[pkg][pair] {
				continue
			}
			if seenPair[pkg] == nil {
				seenPair[pkg] = map[string]bool{}
			}
			seenPair[pkg][pair] = true
			byPkg[pkg] = append(byPkg[pkg], pair)
		}
	}
	for file, rec := range records {
		file = filepath.ToSlash(file)
		if rec == nil || len(rec.NuxtAliases) == 0 {
			continue
		}
		if dirty != nil && dirty[file] {
			continue
		}
		if knownFiles != nil && !knownFiles[file] {
			continue
		}
		if sources != nil {
			if _, ok := sources[file]; ok {
				continue
			}
		}
		add(file, rec.NuxtAliases)
	}
	for file, src := range sources {
		add(filepath.ToSlash(file), nuxtRuntimeAliasesFromFile(file, src))
	}
	consumes := nuxtModuleConsumersRead(sources, knownFiles, readSrc, nuxtPkgs, pkgDirByName, pkgDirs)
	visible := map[string][]string{}
	seenVis := map[string]map[string]bool{}
	addVis := func(pkg string, pairs []string) {
		for _, pair := range pairs {
			if seenVis[pkg][pair] {
				continue
			}
			if seenVis[pkg] == nil {
				seenVis[pkg] = map[string]bool{}
			}
			seenVis[pkg][pair] = true
			visible[pkg] = append(visible[pkg], pair)
		}
	}
	for pkg, pairs := range byPkg {
		addVis(pkg, pairs)
	}
	for consumer, mods := range consumes {
		for _, mod := range mods {
			addVis(consumer, byPkg[mod])
		}
	}
	byDir := map[string]int{}
	for i := range roots {
		byDir[roots[i].dir] = i
		cloned := make(map[string]tsAlias, len(roots[i].aliases))
		for k, v := range roots[i].aliases {
			cloned[k] = v
		}
		roots[i].aliases = cloned
	}
	for pkg, pairs := range visible {
		idx, ok := byDir[pkg]
		if !ok {
			roots = append(roots, tsAliasRoot{dir: pkg, aliases: map[string]tsAlias{}})
			idx = len(roots) - 1
			byDir[pkg] = idx
		}
		applyNuxtRuntimeAliasPairs(roots[idx].aliases, pairs)
	}
	return roots
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
