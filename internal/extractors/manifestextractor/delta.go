package manifestextractor

import (
	"crypto/sha256"
	"fmt"
	"path"
	"path/filepath"
	"sort"

	"github.com/enola-labs/enola/internal/extractors/detectnames"
)

// DeltaContext uses the extractor's own discovery scope, which can include
// engine-pruned manifests. All ancestor lock candidates are observed so that
// additions, removals and nearest-lock precedence changes invalidate the cache.
// Manifest-only mode excludes those candidates and versions its context separately.
func (e *Extractor) DeltaContext(repoPath string) string {
	paths := map[string]bool{}
	for _, rel := range detectnames.Walk(repoPath, e.inputScope) {
		if !e.ContentInput(rel) {
			continue
		}
		paths[rel] = true
		if e.withoutLockfiles || manifestReaders[detectnames.Base(rel)] == nil {
			continue
		}
		for dir := path.Dir(rel); ; dir = path.Dir(dir) {
			for name := range lockNames {
				paths[path.Join(dir, name)] = true
			}
			if dir == "." {
				break
			}
		}
	}
	names := make([]string, 0, len(paths))
	for p := range paths {
		names = append(names, p)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, p := range names {
		b, err := e.inputScope.ReadFile(filepath.Join(repoPath, p))
		fmt.Fprintf(h, "%q:%d:%v\n", p, len(b), err)
		h.Write(b)
	}
	if e.withoutLockfiles {
		return fmt.Sprintf("%s:%x", e.ConfigKey(), h.Sum(nil))
	}
	return fmt.Sprintf("manifests-v1:%x", h.Sum(nil))
}
