package tsextractor

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
)

// discoveryEntry is one entry of the shared discovery enumeration, carrying the
// only three things the collectors behind it consult: where the entry is, what
// it is called, and whether it is a directory. Nothing reads an fs.DirEntry's
// other methods here, so nothing keeps one alive past the walk.
type discoveryEntry struct {
	path  string
	name  string
	isDir bool
}

// discoveryPruned is the prune rule of the package-shaped discovery walks. It
// was written out identically in four collectors; it is stated once here so the
// shared enumeration below cannot drift from the walks it replaces.
func discoveryPruned(root, path, name string) bool {
	return path != root && (strings.HasPrefix(name, ".") || tsSkipDirs[name] || name == "testdata")
}

type discoveryWalkCache struct {
	mu      sync.Mutex
	entries map[string][]discoveryEntry
}

type discoveryWalkCacheKey struct{}

// withDiscoveryWalkCache scopes one shared enumeration to one discovery build.
// There is no process-global cache here for the same reason Discovery has none:
// two runs of the same repository never share one.
func withDiscoveryWalkCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, discoveryWalkCacheKey{}, &discoveryWalkCache{entries: map[string][]discoveryEntry{}})
}

func discoveryWalkCacheFrom(ctx context.Context) *discoveryWalkCache {
	c, _ := ctx.Value(discoveryWalkCacheKey{}).(*discoveryWalkCache)
	return c
}

// sharedDiscoveryEntries enumerates the repository once for the collectors that
// want the same tree under the same prune rule, and holds the result for the
// rest of the discovery build.
//
// Four collectors - the Nuxt probe, the package gates, the package names and
// the package export sources - each ran a full traversal of the same tree in
// the same transaction under a byte-identical prune rule. Sharing one traversal
// is observationally identical rather than merely cheaper: overlayWalkDir
// reports each fully enumerated directory through recordDir, which keeps the
// FIRST enumeration of a directory and discards later ones, so the second
// through fourth walks already contributed nothing the first had not recorded.
// The entry order is the walk order, which collectPackageExportSources depends
// on because the first declaration of a specifier wins.
//
// A context carrying no cache walks as before, so a caller outside a discovery
// build keeps its existing behaviour exactly.
func sharedDiscoveryEntries(ctx context.Context, root string, inputScope *inputscope.Scope) []discoveryEntry {
	cache := discoveryWalkCacheFrom(ctx)
	if cache != nil {
		cache.mu.Lock()
		entries, ok := cache.entries[root]
		cache.mu.Unlock()
		if ok {
			return entries
		}
	}

	var out []discoveryEntry
	_ = overlayWalkDir(ctx, root, inputScope, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is skipped rather than failing discovery,
			// which is what each collector did for itself.
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if discoveryPruned(root, path, name) {
				return filepath.SkipDir
			}
			out = append(out, discoveryEntry{path: path, name: name, isDir: true})
			return nil
		}
		out = append(out, discoveryEntry{path: path, name: d.Name()})
		return nil
	})

	if cache != nil {
		cache.mu.Lock()
		cache.entries[root] = out
		cache.mu.Unlock()
	}
	return out
}
