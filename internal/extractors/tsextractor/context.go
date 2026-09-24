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
func (e *TSExtractor) SessionContext(root string, raw map[string][]byte, paths, files []string, disc *Discovery) (map[string]string, map[string]string, map[string]string, *Discovery) {
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
	out := map[string]string{
		"version":       "ts-effective-context-v4",
		"selected root": digest([]any{tsRoot, found}),
		// These six are handed to every extractFile as they are, so a file cannot
		// be excluded from a change to one of them: the run is a Next.js run or it
		// is not. disc.vue, disc.typeORM, disc.drizzle and disc.prisma used to be
		// hashed here alongside them and are deliberately not, because no reader
		// consumes them - extraction asks packageGates.forFile for Vue and the
		// ORMs and packageGates.anyPrisma for Prisma. Hashing them here invalidated
		// every source in the repository over a dependency in the root package.json
		// that only the files the root package owns can see.
		"repository-wide frameworks": digest([]bool{disc.nextJS, disc.nuxt, disc.svelteKit, disc.ember, disc.reactNav, disc.angular}),
		// anyPrisma is the one genuinely repository-wide half of the package gates:
		// it decides whether the run reads schema.prisma at all, and no file owns
		// that decision. The Vue/TypeORM/Drizzle half is selected per file by the
		// nearest owning package, so it is projected per file below - hashing the
		// whole byDir map here dirtied every source in the repository whenever one
		// package.json gained a dependency that only its own files can see.
		"any package prisma gate": digest(disc.gates.anyPrisma),
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
	// The aggregate key and the alias-inclusive per-file digest below are kept
	// exactly as they were, and are what a state written before the structured
	// entries existed is compared against. Without them the first run on such a
	// state cannot tell an unchanged repository from a changed one, and would
	// have to republish the world to find out. A reader that has the structured
	// entries ignores both; State.TSAliasMeta says which reader it is.
	out["package export aliases"] = digest(exportedAliases)
	aliases := disc.aliasRootsFor()
	// One durable entry per declaration rather than one digest over all of them,
	// so a reader can tell which alias moved and to where. Package `exports`
	// aliases are keyed under the empty root because every file sees them;
	// tsconfig aliases keep the root that declared them, so a nested config
	// stays as scoped here as it is in resolution.
	for key, value := range pkgAliases {
		out[aliasContextKey(aliasKindPackage, "", key)] = encodeAliasEntry(AliasEntry{value.replacement, value.suffix, value.exact})
	}
	for _, root := range aliases {
		for key, value := range root.aliases {
			out[aliasContextKey(aliasKindTSConfig, root.dir, key)] = encodeAliasEntry(AliasEntry{value.replacement, value.suffix, value.exact})
		}
	}
	perFile := map[string]string{}
	perFileBase := map[string]string{}
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
		// The alias set itself is not hashed into the structured base. It used to
		// be, and that is
		// what made one published package dirty every source in the repository:
		// mergePackageAliases puts every package alias into every directory's
		// map, so one new entry moved every file's digest even though no file's
		// own resolution moved. What the file carries instead is which alias root
		// it resolves against, which is the part of the alias projection that is
		// genuinely per file; whether a moved declaration reaches this file is
		// asked of its prior record at comparison time, where both sides of the
		// comparison see the same record.
		aliasRoot, hasAliasRoot := aliasRootDirFor(aliases, dir)
		// The same call the extraction makes, not a re-derivation of it: the
		// nearest owning package's Vue and ORM declarations are what extractFile
		// is handed for this file, so they are what this file's context has to
		// carry. Prisma is deliberately absent - the per-file readers never ask
		// for it, and including it here would dirty files over a fact none of
		// them consume.
		fileOrms, fileVue := disc.gates.forFile(packages, file)
		pkg, inNuxt := nuxtPackageForFile(disc.nuxtPkgs, file, packageDirSet(disc.gates))
		perFile[file] = digest([]any{nearestPackageName(packages, dir), normalized, nuxtScopeKey(pkg, inNuxt), []bool{fileVue, fileOrms.typeORM, fileOrms.drizzle}})
		base := digest([]any{nearestPackageName(packages, dir), nuxtScopeKey(pkg, inNuxt), []bool{fileVue, fileOrms.typeORM, fileOrms.drizzle}})
		perFileBase[file] = encodeFileContext(base, aliasRoot, hasAliasRoot)
	}
	return out, perFile, perFileBase, disc
}

// ContextDifferenceDurable is ContextDifference over the keys that justify a
// whole-domain fallback on their own. Alias declarations are excluded: they are
// projected per key precisely so that a file can be asked whether the one that
// moved reaches it, and forcing every owned file here would discard that answer
// before anything could use it. The per-file seed that replaces it is not weaker
// - it dirties every file whose resolution the change can reach, and falls back
// to affected for any file whose prior record cannot rule it out.
func ContextDifferenceDurable(before, after map[string]string, mode AliasStateMode) []string {
	return contextDifference(before, after, true, mode)
}

// ContextDifference gives deterministic invalidation reasons for durable keys.
func ContextDifference(before, after map[string]string) []string {
	return contextDifference(before, after, false, AliasStateLegacy)
}

func contextDifference(before, after map[string]string, durable bool, mode AliasStateMode) []string {
	// The structured alias entries are always skipped by a durable reason: on a
	// state that has them they are answered per file, and on one that does not
	// they are additions with no counterpart, which would force a whole-domain
	// fallback for no observed change. The aggregate key is skipped only once
	// the structured entries are in use, because until then it is the only thing
	// that can say an alias moved.
	skip := func(k string) bool {
		if !durable || mode == AliasStateUnsupported {
			return false
		}
		if IsAliasContextKey(k) {
			return true
		}
		return mode == AliasStateStructured && k == legacyAliasAggregateKey
	}
	var changed []string
	seen := map[string]bool{}
	for k, v := range before {
		if after[k] != v && !skip(k) {
			seen[k] = true
		}
	}
	for k, v := range after {
		if before[k] != v && !skip(k) {
			seen[k] = true
		}
	}
	for k := range seen {
		changed = append(changed, fmt.Sprintf("TS %s changed", k))
	}
	sort.Strings(changed)
	return changed
}
