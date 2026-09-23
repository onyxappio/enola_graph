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
	nuxtSrcDirLiteral   = regexp.MustCompile(`\bsrcDir\s*:\s*['"]([^'"]+)['"]`)
	nuxtSrcDirField     = regexp.MustCompile(`\bsrcDir\s*:`)
	nuxtAliasBlock      = regexp.MustCompile(`\balias\s*:\s*\{([^}]*)\}`)
	nuxtAliasPair       = regexp.MustCompile(`['"](~|@|~~|@@)['"]\s*:\s*['"]([^'"]+)['"]`)
	nuxtKitNamedImport  = regexp.MustCompile(`import\s*\{([^}]+)\}\s*from\s*['"]@nuxt/kit['"]`)
	nuxtKitBindingSpec  = regexp.MustCompile(`([A-Za-z_$][\w$]*)(?:\s+as\s+([A-Za-z_$][\w$]*))?`)
	nuxtIdentDecl       = regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=`)
	nuxtRelativeLiteral = regexp.MustCompile(`^['"](\.[^'"]+)['"]$`)
	nuxtConfigFileName  = regexp.MustCompile(`(?:^|/)nuxt\.config\.(?:ts|js|mjs)$`)
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

// nuxtJSCodeSpans returns [start,end) ranges of JS that are outside comments
// and string/template literals, so alias provenance cannot be read from them.
func nuxtJSCodeSpans(src []byte) [][2]int {
	var spans [][2]int
	start := 0
	i := 0
	flush := func(end int) {
		if end > start {
			spans = append(spans, [2]int{start, end})
		}
	}
	for i < len(src) {
		switch {
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			flush(i)
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			start = i
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			flush(i)
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			} else {
				i = len(src)
			}
			start = i
		case src[i] == '\'' || src[i] == '"' || src[i] == '`':
			flush(i)
			q := src[i]
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == q {
					i++
					break
				}
				i++
			}
			start = i
		default:
			i++
		}
	}
	flush(len(src))
	return spans
}

func nuxtCodeSlice(src []byte) []byte {
	spans := nuxtJSCodeSpans(src)
	out := bytes.Repeat([]byte{' '}, len(src))
	for _, sp := range spans {
		copy(out[sp[0]:sp[1]], src[sp[0]:sp[1]])
	}
	for i, b := range src {
		if b == '\n' || b == '\r' {
			out[i] = b
		}
	}
	return out
}

func nuxtKitCreateResolverLocals(src []byte) map[string]bool {
	spans := nuxtJSCodeSpans(src)
	locals := map[string]bool{}
	for _, loc := range nuxtKitNamedImport.FindAllSubmatchIndex(src, -1) {
		if !nuxtPosInCode(spans, loc[0]) {
			continue
		}
		inner := src[loc[2]:loc[3]]
		for _, spec := range bytes.Split(inner, []byte(",")) {
			sm := nuxtKitBindingSpec.FindSubmatch(bytes.TrimSpace(spec))
			if sm == nil {
				continue
			}
			imported := string(sm[1])
			local := imported
			if len(sm[2]) > 0 {
				local = string(sm[2])
			}
			if imported == "createResolver" {
				locals[local] = true
			}
		}
	}
	code := nuxtCodeSlice(src)
	for local := range locals {
		if nuxtIdentHasAssignment(code, local) {
			delete(locals, local)
		}
	}
	return locals
}

func nuxtIdentHasAssignment(src []byte, ident string) bool {
	if ident == "" {
		return false
	}
	assign := regexp.MustCompile(`(?:^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(ident) + `\s*=`)
	return len(assign.FindAllIndex(src, -1)) > 0
}

func nuxtIdentAssignedOnceFrom(src []byte, ident string, from map[string]bool) bool {
	if ident == "" || from[ident] {
		return false
	}
	assign := regexp.MustCompile(`(?:^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(ident) + `\s*=`)
	locs := assign.FindAllIndex(src, -1)
	if len(locs) != 1 {
		return false
	}
	decls := nuxtIdentDecl.FindAllSubmatch(src, -1)
	n := 0
	for _, d := range decls {
		if string(d[1]) == ident {
			n++
		}
	}
	return n == 1
}

func nuxtKitResolverIdents(src []byte) map[string]bool {
	create := nuxtKitCreateResolverLocals(src)
	if len(create) == 0 {
		return nil
	}
	code := nuxtCodeSlice(src)
	out := map[string]bool{}
	decl := regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*([A-Za-z_$][\w$]*)\s*\(\s*import\s*\.\s*meta\s*\.\s*url\s*\)`)
	for _, m := range decl.FindAllSubmatch(code, -1) {
		ident := string(m[1])
		callee := string(m[2])
		if !create[callee] {
			continue
		}
		if nuxtIdentAssignedOnceFrom(code, ident, create) {
			out[ident] = true
		}
	}
	return out
}

type nuxtAliasAssign struct {
	key string
	rhs string
}

func nuxtPosInCode(spans [][2]int, pos int) bool {
	for _, sp := range spans {
		if pos >= sp[0] && pos < sp[1] {
			return true
		}
	}
	return false
}

func nuxtRuntimeAliasAssigns(src []byte) []nuxtAliasAssign {
	spans := nuxtJSCodeSpans(src)
	needle := []byte("nuxt.options.alias[")
	var out []nuxtAliasAssign
	for i := 0; i < len(src); {
		j := bytes.Index(src[i:], needle)
		if j < 0 {
			break
		}
		at := i + j
		i = at + len(needle)
		if !nuxtPosInCode(spans, at) {
			continue
		}
		rest := bytes.TrimLeft(src[i:], " \t")
		if len(rest) < 2 {
			continue
		}
		q := rest[0]
		if q != '\'' && q != '"' {
			continue
		}
		k := 1
		for k < len(rest) && rest[k] != q {
			k++
		}
		if k >= len(rest) {
			continue
		}
		key := string(rest[1:k])
		rest = bytes.TrimLeft(rest[k+1:], " \t")
		if len(rest) == 0 || rest[0] != ']' {
			continue
		}
		rest = bytes.TrimLeft(rest[1:], " \t")
		if len(rest) == 0 || rest[0] != '=' {
			continue
		}
		rhs := strings.TrimSpace(string(rest[1:]))
		if cut := strings.IndexAny(rhs, ";\n"); cut >= 0 {
			rhs = strings.TrimSpace(rhs[:cut])
		}
		if key != "" && rhs != "" {
			out = append(out, nuxtAliasAssign{key: key, rhs: rhs})
		}
	}
	return out
}

func nuxtKitResolveCallTarget(file string, rhs string, kitVars map[string]bool) string {
	rhs = strings.TrimSpace(rhs)
	for ident := range kitVars {
		alt := regexp.MustCompile(`^` + regexp.QuoteMeta(ident) + `\s*\.\s*resolve\s*\(\s*['"](\.[^'"]+)['"]\s*\)$`)
		if loc := alt.FindStringSubmatch(rhs); loc != nil {
			return nuxtRelativeAliasTarget(file, loc[1])
		}
	}
	return ""
}

// nuxtRuntimeAliasesFromFile reads statically assigned nuxt.options.alias
// entries whose right-hand side is a same-file createResolver(...).resolve('./…')
// binding from @nuxt/kit, or a relative string literal. Comments and string
// literals cannot contribute. Shadowed or reassigned resolver names are omitted.
func nuxtRuntimeAliasesFromFile(file string, src []byte) []string {
	if src == nil || !bytes.Contains(src, []byte("nuxt.options.alias")) {
		return nil
	}
	code := nuxtCodeSlice(src)
	if !bytes.Contains(code, []byte("nuxt.options.alias")) {
		return nil
	}
	kitVars := nuxtKitResolverIdents(src)
	spans := nuxtJSCodeSpans(src)
	idents := map[string]string{}
	if len(kitVars) > 0 {
		for ident := range kitVars {
			pat := regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*` + regexp.QuoteMeta(ident) + `\s*\.\s*resolve\s*\(\s*['"](\.[^'"]+)['"]\s*\)`)
			for _, loc := range pat.FindAllSubmatchIndex(src, -1) {
				if !nuxtPosInCode(spans, loc[0]) {
					continue
				}
				name := string(src[loc[2]:loc[3]])
				rel := string(src[loc[4]:loc[5]])
				if !nuxtIdentAssignedOnceFrom(nuxtCodeSlice(src), name, kitVars) {
					continue
				}
				if t := nuxtRelativeAliasTarget(file, rel); t != "" {
					if prev, ok := idents[name]; ok && prev != t {
						idents[name] = ""
						continue
					}
					idents[name] = t
				}
			}
		}
	}
	targets := map[string]map[string]bool{}
	add := func(key, target string) {
		key = strings.TrimSpace(key)
		target = factpath.Clean(strings.TrimSpace(target))
		if key == "" || target == "" {
			return
		}
		if targets[key] == nil {
			targets[key] = map[string]bool{}
		}
		targets[key][target] = true
	}
	for _, m := range nuxtRuntimeAliasAssigns(src) {
		key := m.key
		rhs := m.rhs
		if t := nuxtKitResolveCallTarget(file, rhs, kitVars); t != "" {
			add(key, t)
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
	var out []string
	for key, set := range targets {
		if len(set) != 1 {
			continue
		}
		for target := range set {
			out = append(out, key+"=>"+target)
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

func nuxtRuntimeAliasPairsByPackage(sources map[string][]byte, knownFiles map[string]bool, records map[string]*FileRecord, dirty map[string]bool, nuxtPkgs []string, pkgDirs map[string]bool) map[string][]string {
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
	return byPkg
}

func nuxtVisibleRuntimeAliases(byPkg map[string][]string, consumes map[string][]string) map[string][]string {
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
	for pkg, pairs := range visible {
		sort.Strings(pairs)
		visible[pkg] = pairs
	}
	return visible
}

func nuxtRuntimeAliasVisibility(sources map[string][]byte, knownFiles map[string]bool, readSrc func(string) []byte, records map[string]*FileRecord, dirty map[string]bool, nuxtPkgs []string, pkgDirByName map[string]string, pkgDirs map[string]bool) map[string][]string {
	if len(nuxtPkgs) == 0 {
		return nil
	}
	byPkg := nuxtRuntimeAliasPairsByPackage(sources, knownFiles, records, dirty, nuxtPkgs, pkgDirs)
	consumes := nuxtModuleConsumersRead(sources, knownFiles, readSrc, nuxtPkgs, pkgDirByName, pkgDirs)
	return nuxtVisibleRuntimeAliases(byPkg, consumes)
}

func nuxtAliasVisibilityChangedPkgs(sources map[string][]byte, knownFiles map[string]bool, readSrc func(string) []byte, records map[string]*FileRecord, dirty map[string]bool, nuxtPkgs []string, pkgDirByName map[string]string, pkgDirs map[string]bool) map[string]bool {
	if len(nuxtPkgs) == 0 || records == nil {
		return nil
	}
	curr := nuxtRuntimeAliasVisibility(sources, knownFiles, readSrc, records, dirty, nuxtPkgs, pkgDirByName, pkgDirs)
	prev := nuxtRuntimeAliasVisibility(nil, knownFiles, readSrc, records, nil, nuxtPkgs, pkgDirByName, pkgDirs)
	out := map[string]bool{}
	mark := func(pkg string) {
		if pkg != "" {
			out[pkg] = true
		}
	}
	for pkg, pairs := range curr {
		if strings.Join(prev[pkg], "\x00") != strings.Join(pairs, "\x00") {
			mark(pkg)
		}
	}
	for pkg, pairs := range prev {
		if strings.Join(curr[pkg], "\x00") != strings.Join(pairs, "\x00") {
			mark(pkg)
		}
	}
	for file, d := range dirty {
		if !d {
			continue
		}
		if nuxtConfigFileName.MatchString(filepath.ToSlash(file)) {
			if pkg, ok := nuxtPackageForFile(nuxtPkgs, file, pkgDirs); ok {
				mark(pkg)
			}
		}
		rec := records[file]
		if rec == nil {
			rec = records[filepath.ToSlash(file)]
		}
		oldPairs := []string(nil)
		if rec != nil {
			oldPairs = rec.NuxtAliases
		}
		newPairs := []string(nil)
		if src := sources[file]; src != nil {
			newPairs = nuxtRuntimeAliasesFromFile(file, src)
		} else if src := sources[filepath.ToSlash(file)]; src != nil {
			newPairs = nuxtRuntimeAliasesFromFile(file, src)
		}
		if strings.Join(oldPairs, "\x00") == strings.Join(newPairs, "\x00") {
			continue
		}
		if pkg, ok := nuxtPackageForFile(nuxtPkgs, file, pkgDirs); ok {
			mark(pkg)
			consumes := nuxtModuleConsumersRead(sources, knownFiles, readSrc, nuxtPkgs, pkgDirByName, pkgDirs)
			for consumer, mods := range consumes {
				for _, mod := range mods {
					if mod == pkg {
						mark(consumer)
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func withNuxtRuntimeAliases(roots []tsAliasRoot, sources map[string][]byte, knownFiles map[string]bool, readSrc func(string) []byte, records map[string]*FileRecord, dirty map[string]bool, nuxtPkgs []string, pkgDirs map[string]bool, pkgDirByName map[string]string) []tsAliasRoot {
	if len(nuxtPkgs) == 0 {
		return roots
	}
	visible := nuxtRuntimeAliasVisibility(sources, knownFiles, readSrc, records, dirty, nuxtPkgs, pkgDirByName, pkgDirs)
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
