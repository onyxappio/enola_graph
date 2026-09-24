package tsextractor

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enola-labs/enola/internal/factpath"
)

// Alias declarations are projected into the session context one key at a time,
// in readable form, rather than as a single digest over the whole map.
//
// A digest can only say that some alias moved. Every reader of that answer then
// has to assume the worst, which is what made publishing an unrelated package
// reparse the repository: one new `exports` entry moved the one digest, and the
// digest is a repository-wide key, so the whole name-resolution domain fell back.
//
// Per-key entries say which alias moved and what it pointed at before and after,
// so the decision can be made per file against that file's own prior record.
// The entries stay a pure function of the configuration bytes - no cached record
// is consulted to build them - because they are persisted and compared against a
// later run's recomputation, and the captured-input fence requires a projection
// that does not move when the cache behind it is rewritten mid-run.
const aliasContextPrefix = "ts alias "

// legacyAliasAggregateKey is the single digest that preceded the per-key
// entries. It is still emitted so that a state written before this change can
// still answer whether anything moved.
const legacyAliasAggregateKey = "package export aliases"

// AliasMetaVersion marks a state whose alias projection is the structured one.
// Absent means the state predates it, which is not the same as a state that
// declares no aliases: the first cannot be compared per key at all, the second
// compares per key and finds nothing.
const AliasMetaVersion = "v1"

// AliasStateMode is how a stored state's alias projection may be read.
type AliasStateMode int

const (
	// AliasStateLegacy is a state written before the structured entries. It
	// carries the aggregate digest and the alias-inclusive per-file digests,
	// and is compared by those.
	AliasStateLegacy AliasStateMode = iota
	// AliasStateStructured is a state written at AliasMetaVersion and complete.
	AliasStateStructured
	// AliasStateUnsupported is a state whose marker this build does not know,
	// or one that claims the marker without the map that gives it meaning -
	// a downgrade after an upgrade, or a state written by a version that
	// projects something else. Neither comparison is valid against it, so it
	// is not compared: everything is treated as moved.
	AliasStateUnsupported
)

// AliasStateModeFor decides how a stored state may be read. files is the number
// of per-file contexts it carries and bases the number of structured bases; a
// state that claims the marker while owning files without bases is incomplete,
// which is the one case where a truncated write must not read as "no aliases".
func AliasStateModeFor(meta string, files, bases int) AliasStateMode {
	switch meta {
	case "":
		return AliasStateLegacy
	case AliasMetaVersion:
		if files > 0 && bases == 0 {
			return AliasStateUnsupported
		}
		return AliasStateStructured
	default:
		return AliasStateUnsupported
	}
}

// AliasEntry is one alias declaration as the context records it.
type AliasEntry struct {
	Replacement string `json:"r"`
	Suffix      string `json:"s,omitempty"`
	Exact       bool   `json:"e,omitempty"`
}

// AliasChange is one alias key that differs between two session contexts. Prev
// is nil for a key this run added, Cur is nil for one it removed.
type AliasChange struct {
	// Package is set for a package `exports` alias, which every file sees
	// wherever it lives. Root is the declaring alias root for a tsconfig alias,
	// which only files resolving against that exact root see.
	Package bool
	Root    string
	Key     string
	Prev    *AliasEntry
	Cur     *AliasEntry
}

// A package `exports` alias and a tsconfig alias declared at the repository root
// are different declarations with different reach, and both would key under the
// empty root, so the kind is part of the key. Without it a repository-root
// tsconfig alias would be read as a package alias and judged against files under
// a nested root, which never see it: aliasesForDir returns the nearest root
// alone, not a merge down the chain.
const (
	aliasKindPackage  = "pkg"
	aliasKindTSConfig = "cfg"
)

func aliasContextKey(kind, root, key string) string {
	b, _ := json.Marshal([3]string{kind, root, key})
	return aliasContextPrefix + string(b)
}

func parseAliasContextKey(k string) (kind, root, key string, ok bool) {
	if !strings.HasPrefix(k, aliasContextPrefix) {
		return "", "", "", false
	}
	var parts [3]string
	if err := json.Unmarshal([]byte(k[len(aliasContextPrefix):]), &parts); err != nil {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// IsAliasContextKey reports whether a session context key is an alias
// declaration. Callers that force a whole-domain fallback on any context
// movement skip these: an alias key is answered per file instead, by
// AliasChangeAffects, and forcing on it as well would make that answer moot.
func IsAliasContextKey(k string) bool {
	return strings.HasPrefix(k, aliasContextPrefix)
}

func encodeAliasEntry(e AliasEntry) string {
	b, _ := json.Marshal(e)
	return string(b)
}

func decodeAliasEntry(s string) (AliasEntry, bool) {
	var e AliasEntry
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		return AliasEntry{}, false
	}
	return e, true
}

// AliasContextChanges lists the alias declarations that differ between a stored
// session context and this run's. The result is derived from the two projections
// alone, so an unchanged configuration yields an empty list whatever the cache
// holds - which is what keeps an immediate no-op run from dirtying anything.
func AliasContextChanges(before, after map[string]string) []AliasChange {
	seen := map[string]bool{}
	var out []AliasChange
	add := func(k string) {
		if seen[k] {
			return
		}
		seen[k] = true
		kind, root, key, ok := parseAliasContextKey(k)
		if !ok {
			return
		}
		ch := AliasChange{Package: kind == aliasKindPackage, Root: root, Key: key}
		if v, has := before[k]; has {
			if e, ok := decodeAliasEntry(v); ok {
				ch.Prev = &e
			}
		}
		if v, has := after[k]; has {
			if e, ok := decodeAliasEntry(v); ok {
				ch.Cur = &e
			}
		}
		out = append(out, ch)
	}
	for k, v := range before {
		if IsAliasContextKey(k) && after[k] != v {
			add(k)
		}
	}
	for k, v := range after {
		if IsAliasContextKey(k) && before[k] != v {
			add(k)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package
		}
		if out[i].Root != out[j].Root {
			return out[i].Root < out[j].Root
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// AliasesForRoot rebuilds the alias map a file resolves against: the package
// `exports` aliases, overridden by the tsconfig aliases declared at the file's
// nearest alias root. It mirrors aliasesForDir followed by mergePackageAliases -
// the nearest root alone, and tsconfig winning ties. hasRoot is false for a file
// that resolves against no tsconfig root at all.
func AliasesForRoot(ctx map[string]string, root string, hasRoot bool) map[string]AliasEntry {
	out := map[string]AliasEntry{}
	for k, v := range ctx {
		kind, _, key, ok := parseAliasContextKey(k)
		if !ok || kind != aliasKindPackage {
			continue
		}
		if e, ok := decodeAliasEntry(v); ok {
			out[key] = e
		}
	}
	if !hasRoot {
		return out
	}
	for k, v := range ctx {
		kind, r, key, ok := parseAliasContextKey(k)
		if !ok || kind != aliasKindTSConfig || r != root {
			continue
		}
		if e, ok := decodeAliasEntry(v); ok {
			out[key] = e
		}
	}
	return out
}

// AliasChangeAffects answers whether one moved alias key can change what a file
// already resolved, judged against the record that file contributed before the
// change.
//
// It is deliberately conservative in three places. A file with no record, or one
// whose import summary is incomplete, is affected: an empty resolved and
// unresolved pair is a valid graph for a file whose imports are all external, so
// absence of evidence there is not evidence of absence. A key that competes with
// another key by prefix is affected: which of the two a specifier bound through
// is not recoverable from a record, because ImportSpecs are already
// alias-normalized replay paths rather than the specifier as written. And both
// the previous and the current target are matched, so a retarget or a removal
// reaches the files that had resolved through the old one.
func AliasChangeAffects(ch AliasChange, merged map[string]AliasEntry, rec *FileRecord) bool {
	if rec == nil || !rec.ImportComplete {
		return true
	}
	if !aliasKeyIsolated(merged, ch.Key) {
		return true
	}
	if ch.Prev != nil && aliasTouchesRecord(*ch.Prev, ch.Key, rec) {
		return true
	}
	if ch.Cur != nil && aliasTouchesRecord(*ch.Cur, ch.Key, rec) {
		return true
	}
	return false
}

// aliasKeyIsolated reports that no other declared key is a prefix of this one or
// has this one as a prefix, so no specifier could have bound through a competing
// key that this change reorders.
func aliasKeyIsolated(merged map[string]AliasEntry, key string) bool {
	for other := range merged {
		if other == key {
			continue
		}
		if strings.HasPrefix(other, key) || strings.HasPrefix(key, other) {
			return false
		}
	}
	return true
}

func aliasTouchesRecord(e AliasEntry, key string, rec *FileRecord) bool {
	target := factpath.Clean(e.Replacement)
	for _, group := range [][]string{rec.ImportSpecs, rec.UnresolvedSpecs, rec.Reexports, rec.ResolvedFiles} {
		for _, spec := range group {
			spec = filepath.ToSlash(spec)
			if spec == key || strings.HasPrefix(spec, key) {
				return true
			}
			if target != "" && (spec == target || strings.HasPrefix(spec, target+"/")) {
				return true
			}
		}
	}
	return false
}

// File contexts carry the alias root their file resolves against next to the
// digest of the rest of the projection, so the per-file comparison can ask which
// alias declarations that file could even see. The digest is hex, so the first
// space separates the two unambiguously whatever a directory is named.
func encodeFileContext(base, root string, found bool) string {
	if !found {
		return base + " -"
	}
	return base + " r:" + root
}

// FileAliasRoot recovers the alias root from a stored file context, and reports
// whether the file resolves against one at all.
func FileAliasRoot(fileContext string) (string, bool) {
	i := strings.IndexByte(fileContext, ' ')
	if i < 0 {
		return "", false
	}
	tail := fileContext[i+1:]
	if !strings.HasPrefix(tail, "r:") {
		return "", false
	}
	return tail[len("r:"):], true
}

func aliasRootDirFor(roots []tsAliasRoot, dir string) (string, bool) {
	dir = filepath.ToSlash(dir)
	best := ""
	bestLen := -1
	for i := range roots {
		r := &roots[i]
		if r.dir != "" && dir != r.dir && !strings.HasPrefix(dir, r.dir+"/") {
			continue
		}
		if len(r.dir) > bestLen {
			best = r.dir
			bestLen = len(r.dir)
		}
	}
	return best, bestLen >= 0
}
