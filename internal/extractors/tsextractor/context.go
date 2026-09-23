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
// The returned snapshot is the one this projection was actually computed from,
// which is not always the one that was handed in: a caller that keeps the
// argument instead would let its planner previews run against a snapshot these
// keys were never derived from, and would not count the build that happened
// here.
func (e *TSExtractor) SessionContext(root string, raw map[string][]byte, paths, files []string, disc *Discovery) (map[string]string, map[string]string, *Discovery) {
	digest := func(v any) string {
		b, _ := json.Marshal(v)
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}
	scope := e.inputScope
	// raw is the configuration this run has already captured and fenced, and it
	// is what the projected keys below are computed from, so it is also what the
	// readers behind them must see. Handing reusableFor a nil overlay claimed
	// the opposite - that this fingerprint observes the live tree - and both
	// rejected a snapshot taken under the capture and then rebuilt one that
	// disagreed with the very bytes being projected.
	//
	// Without a snapshot this fingerprint walked the whole tree four times for
	// Nuxt alone - detectNuxt is collectNuxtPackages, and the alias fallback
	// below asks for both - on top of the gate, name, alias-root and export
	// walks. The snapshot answers each of those once.
	ov := newFileOverlay(root, raw)
	if !disc.reusableFor(root, scope, ov) {
		disc = e.newDiscovery(context.Background(), root, ov, len(raw))
	}
	tsRoot, found := disc.tsRoot, disc.tsRootFound
	typeORM, drizzle, prisma := disc.typeORM, disc.drizzle, disc.prisma
	out := map[string]string{
		"version":                 "ts-effective-context-v3",
		"selected root":           digest([]any{tsRoot, found}),
		"framework and ORM gates": digest([]bool{disc.nextJS, disc.vue, disc.nuxt, disc.svelteKit, disc.ember, disc.reactNav, disc.angular, typeORM, drizzle, prisma}),
		"owning package gates":    digest(disc.gates.activeGates()),
		"nuxt packages":           digest(disc.nuxtPkgs),

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
	type aliasValue struct {
		Replacement, Suffix string
		Exact               bool
	}
	packages := disc.pkgNames
	known := map[string]bool{}
	for _, file := range files {
		known[filepath.ToSlash(file)] = true
	}
	pkgAliases := disc.packageAliasesFor(known)
	exportedAliases := map[string]aliasValue{}
	for key, value := range pkgAliases {
		exportedAliases[key] = aliasValue{value.replacement, value.suffix, value.exact}
	}
	out["package export aliases"] = digest(exportedAliases)
	aliases := disc.aliasRootsFor()
	perFile := map[string]string{}
	angular := disc.angular
	for _, file := range files {
		if !IsSessionSource(file, angular) {
			continue
		}
		dir := factpath.Dir(file)
		normalized := map[string]aliasValue{}
		for key, value := range mergePackageAliases(aliasesForDir(aliases, dir), pkgAliases) {
			normalized[key] = aliasValue{value.replacement, value.suffix, value.exact}
		}
		pkg, inNuxt := nuxtPackageForFile(disc.nuxtPkgs, file, packageDirSet(disc.gates))
		perFile[file] = digest([]any{nearestPackageName(packages, dir), normalized, nuxtScopeKey(pkg, inNuxt)})
	}
	return out, perFile, disc
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
