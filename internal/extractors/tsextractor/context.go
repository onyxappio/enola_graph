package tsextractor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/factpath"
)

// SessionContext projects the actual built-in package readers, not a JSON field
// whitelist. Raw inputs remain separately captured and fenced by graphsession.
// Unprojected configuration families deliberately retain byte-sensitive fallback.
func (e *TSExtractor) SessionContext(root string, raw map[string][]byte, paths, files []string) (map[string]string, map[string]string) {
	digest := func(v any) string {
		b, _ := json.Marshal(v)
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}
	scope := e.inputScope
	tsRoot, found := findTSRoot(root, scope)
	typeORM, drizzle, prisma := detectORMs(root, scope)
	out := map[string]string{
		"version":                 "ts-effective-context-v2",
		"selected root":           digest([]any{tsRoot, found}),
		"framework and ORM gates": digest([]bool{detectNextJS(root, scope), detectVue(root, scope), detectNuxt(root, scope), detectSvelteKit(root, scope), detectEmber(root, scope), detectReactNavigation(root, scope), detectAngular(root, scope), typeORM, drizzle, prisma}),
		"nuxt packages":           digest(collectNuxtPackages(context.Background(), root, scope)),

		"configured clients": e.ConfigKey(),
	}
	// Malformed transitions are explicit conservative boundaries. Valid membership
	// is represented by actual root/gate/per-file reader outputs. Package byte changes
	// are represented by the reader outputs above; declaration consumers such as
	// manifests continue to fingerprint and extract their own full raw inputs.
	packageStatus := map[string]string{}
	configStatus := map[string]string{}
	for _, p := range paths {
		b, exists := raw[filepath.ToSlash(p)]
		if filepath.Base(p) == "package.json" {
			status := "missing"
			if exists {
				var object map[string]any
				if err := json.Unmarshal(b, &object); err != nil || object == nil {
					status = "malformed:" + digest(b)
				} else {
					status = "valid"
				}
			}
			if status != "valid" && status != "missing" {
				packageStatus[filepath.ToSlash(p)] = status
			}
		} else if filepath.Base(p) == "tsconfig.json" || filepath.Base(p) == "tsconfig.base.json" {
			status := "missing"
			if exists {
				var object map[string]any
				var parsed tsConfigAliasFile // the actual alias reader's decoded shape
				normalized := stripJSONC(b)
				objectErr := json.Unmarshal(normalized, &object)
				parseErr := json.Unmarshal(normalized, &parsed)
				extends := strings.TrimSpace(parsed.Extends)
				if parseErr == nil && parsed.CompilerOptions.Paths == nil && extends != "" && resolveTSConfigExtends(p, extends) == "" {
					out["unsupported package extends "+filepath.ToSlash(p)] = digest(b)
				}
				if objectErr != nil || parseErr != nil || object == nil {
					status = "malformed:" + digest(b)
				} else {
					status = "valid"
				}
			}
			if status != "valid" && status != "missing" {
				configStatus[p] = status
			}
		} else {
			out["unprojected config "+filepath.ToSlash(p)] = digest([]any{exists, b})
		}
	}
	out["package validity errors"] = digest(packageStatus)
	out["alias config validity errors"] = digest(configStatus)
	packages := collectPackageNames(root, scope)
	aliases := collectTSAliasRoots(context.Background(), root, scope)
	if detectSvelteKit(root, scope) {
		aliases = withSvelteKitAliasFallbacks(root, aliases, scope)
	}
	if detectNuxt(root, scope) {
		aliases = withNuxtAliasFallbacks(root, aliases, collectNuxtPackages(context.Background(), root, scope), scope)
	}
	perFile := map[string]string{}
	type aliasValue struct {
		Replacement, Suffix string
		Exact               bool
	}
	angular := detectAngular(root, scope)
	for _, file := range files {
		if !IsSessionSource(file, angular) {
			continue
		}
		dir := factpath.Dir(file)
		normalized := map[string]aliasValue{}
		for key, value := range aliasesForDir(aliases, dir) {
			normalized[key] = aliasValue{value.replacement, value.suffix, value.exact}
		}
		perFile[file] = digest([]any{nearestPackageName(packages, dir), normalized})
	}
	return out, perFile
}

// ContextDifference gives deterministic invalidation reasons for durable keys.
func ContextDifference(before, after map[string]string) []string {
	var changed []string
	seen := map[string]bool{}
	for k, v := range before {
		if after[k] != v {
			seen[k] = true
		}
	}
	for k, v := range after {
		if before[k] != v {
			seen[k] = true
		}
	}
	for k := range seen {
		changed = append(changed, fmt.Sprintf("TS %s changed", k))
	}
	sort.Strings(changed)
	return changed
}
