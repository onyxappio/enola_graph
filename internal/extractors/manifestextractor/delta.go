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
	_, sum := e.captureContext(repoPath, false)
	return sum
}

// CaptureContext snapshots every input DeltaContext observes and returns those
// bytes together with the context digest they produce.
//
// It exists so a caller can extract from the snapshot instead of the tree and
// still prove which bytes it used: the digest is computed from the same reads
// that fill the snapshot, so comparing it against the DeltaContext value the
// session already recorded fences the whole input set at once - the walked
// manifests, the engine-pruned ones this extractor discovers for itself, and
// the ancestor lock candidates that do not exist, which contribute their read
// error to the digest exactly as they do here. An edit that is restored before
// a second read cannot pass that comparison, because the digest describes the
// snapshot rather than a later state of the tree.
//
// Unreadable paths are absent from the snapshot, which is what readCtx.read
// already reports them as. DeltaContext is this function's digest and nothing
// else, so the enumeration and the digest cannot drift apart.
//
// Ordinary delta detection asks only for the digest, and it goes through
// captureContext with retention off so that the common path allocates no byte
// map: every manifest and lockfile is read to hash it either way, but only a
// caller that will extract from the snapshot keeps them.
func (e *Extractor) CaptureContext(repoPath string) (map[string][]byte, string) {
	return e.captureContext(repoPath, true)
}

func (e *Extractor) captureContext(repoPath string, retain bool) (map[string][]byte, string) {
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
	var src map[string][]byte
	if retain {
		src = make(map[string][]byte, len(names))
	}
	h := sha256.New()
	for _, p := range names {
		b, err := e.inputScope.ReadFile(filepath.Join(repoPath, p))
		fmt.Fprintf(h, "%q:%d:%v\n", p, len(b), err)
		h.Write(b)
		if retain && err == nil {
			src[p] = b
		}
	}
	if e.withoutLockfiles {
		return src, fmt.Sprintf("%s:%x", e.ConfigKey(), h.Sum(nil))
	}
	return src, fmt.Sprintf("manifests-v1:%x", h.Sum(nil))
}
