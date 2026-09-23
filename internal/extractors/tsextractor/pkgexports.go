package tsextractor

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/factpath"
)

// packageJSONExports maps published specifiers to in-repo source files named by a
// package.json exports map (and the types/main fallbacks). Parsed once per
// package.json during collectPackageAliases.
type packageJSONExports struct {
	exact  map[string]string // specifier → replacement path
	prefix []tsAliasPrefix
}

type tsAliasPrefix struct {
	prefix, replacement, suffix string
}

// preferredExportConditions is Node's types-first lookup without executing the
// package. Later keys are only used when earlier targets are missing from the
// indexed file set.
var preferredExportConditions = []string{
	"types", "import", "module", "default", "require", "node", "browser", "react-native",
}

func parsePackageJSONExports(pkgName, pkgDir string, types, typings, module, main string, exports json.RawMessage, knownFiles map[string]bool) packageJSONExports {
	out := packageJSONExports{exact: map[string]string{}}
	if pkgName == "" {
		return out
	}
	if len(exports) > 0 {
		var raw any
		if err := json.Unmarshal(exports, &raw); err == nil {
			applyExportValue(out.exact, &out.prefix, pkgName, pkgDir, ".", raw, knownFiles)
		}
	}
	if _, ok := out.exact[pkgName]; !ok {
		for _, s := range []string{types, typings, module, main} {
			if dest, ok := indexedPackageEntry(pkgDir, s, knownFiles); ok {
				out.exact[pkgName] = dest
				break
			}
		}
	}
	if _, ok := out.exact[pkgName]; !ok {
		if dest, ok := indexedPackageEntry(pkgDir, "src/index", knownFiles); ok {
			out.exact[pkgName] = dest
		}
	}
	return out
}

func applyExportValue(exact map[string]string, prefix *[]tsAliasPrefix, pkgName, pkgDir, subpath string, raw any, knownFiles map[string]bool) {
	switch v := raw.(type) {
	case string:
		registerExportTarget(exact, prefix, pkgName, pkgDir, subpath, v, knownFiles)
	case map[string]any:
		if looksLikeExportConditions(v) {
			if dest := firstExistingExportCondition(pkgDir, v, knownFiles); dest != "" {
				registerExportTarget(exact, prefix, pkgName, pkgDir, subpath, dest, knownFiles)
			}
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := k
			if subpath != "." && subpath != "" {
				next = joinExportSubpath(subpath, k)
			}
			applyExportValue(exact, prefix, pkgName, pkgDir, next, v[k], knownFiles)
		}
	case []any:
		for _, item := range v {
			before := len(exact)
			applyExportValue(exact, prefix, pkgName, pkgDir, subpath, item, knownFiles)
			if len(exact) > before {
				return
			}
		}
	}
}

func looksLikeExportConditions(m map[string]any) bool {
	for k := range m {
		if strings.HasPrefix(k, ".") {
			return false
		}
	}
	return len(m) > 0
}

func firstExistingExportCondition(pkgDir string, m map[string]any, knownFiles map[string]bool) string {
	seen := map[string]bool{}
	for _, key := range preferredExportConditions {
		seen[key] = true
		if dest := exportConditionTarget(pkgDir, m[key], knownFiles); dest != "" {
			return dest
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if dest := exportConditionTarget(pkgDir, m[k], knownFiles); dest != "" {
			return dest
		}
	}
	return ""
}

func exportConditionTarget(pkgDir string, raw any, knownFiles map[string]bool) string {
	switch v := raw.(type) {
	case string:
		if _, ok := indexedPackageEntry(pkgDir, v, knownFiles); ok {
			return v
		}
	case []any:
		for _, item := range v {
			if dest := exportConditionTarget(pkgDir, item, knownFiles); dest != "" {
				return dest
			}
		}
	case map[string]any:
		return firstExistingExportCondition(pkgDir, v, knownFiles)
	}
	return ""
}

func registerExportTarget(exact map[string]string, prefix *[]tsAliasPrefix, pkgName, pkgDir, subpath, entry string, knownFiles map[string]bool) {
	dest, ok := indexedPackageEntry(pkgDir, entry, knownFiles)
	if !ok {
		return
	}
	spec := exportSpecifier(pkgName, subpath)
	if spec == "" {
		return
	}
	if strings.Contains(spec, "*") {
		star := strings.IndexByte(spec, '*')
		pre := spec[:star]
		*prefix = append(*prefix, tsAliasPrefix{prefix: pre, replacement: strings.TrimSuffix(dest, "*"), suffix: spec[star+1:]})
		return
	}
	if _, exists := exact[spec]; !exists {
		exact[spec] = dest
	}
}

func exportSpecifier(pkgName, subpath string) string {
	subpath = strings.TrimSpace(subpath)
	if subpath == "" || subpath == "." {
		return pkgName
	}
	if strings.HasPrefix(subpath, "./") {
		return pkgName + "/" + strings.TrimPrefix(subpath, "./")
	}
	if strings.HasPrefix(subpath, "/") {
		return pkgName + subpath
	}
	return pkgName + "/" + subpath
}

func joinExportSubpath(parent, child string) string {
	if parent == "." || parent == "" {
		return child
	}
	if strings.HasPrefix(child, "./") {
		return strings.TrimSuffix(parent, "/") + "/" + strings.TrimPrefix(child, "./")
	}
	return parent
}

func indexedPackageEntry(pkgDir, entry string, knownFiles map[string]bool) (string, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", false
	}
	entry = strings.TrimPrefix(entry, "./")
	rel := factpath.Clean(factpath.Join(pkgDir, entry))
	if _, _, ok := resolveModuleFile(rel, knownFiles); ok {
		return rel, true
	}
	return "", false
}

func applyParsedExports(out map[string]tsAlias, parsed packageJSONExports) {
	for spec, dest := range parsed.exact {
		if _, exists := out[spec]; exists {
			continue
		}
		out[spec] = tsAlias{replacement: dest, exact: true}
	}
	for _, p := range parsed.prefix {
		if _, exists := out[p.prefix]; exists {
			continue
		}
		out[p.prefix] = tsAlias{replacement: p.replacement, suffix: p.suffix, exact: false}
	}
}

func nearestPackageDir(pkgNames map[string]string, dir string) string {
	for d := filepath.ToSlash(dir); ; {
		if _, ok := pkgNames[d]; ok {
			return d
		}
		i := strings.LastIndexByte(d, '/')
		if i < 0 {
			if d == "." {
				if _, ok := pkgNames[""]; ok {
					return ""
				}
				return ""
			}
			d = "."
			continue
		}
		d = d[:i]
	}
}
