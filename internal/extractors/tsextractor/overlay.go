package tsextractor

import (
	"context"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"path/filepath"
)

type overlayKey struct{}

// fileOverlay is the per-session capture-and-use map. It is stored on the
// ExtractSession context so concurrent sessions cannot see each other's bytes.
type fileOverlay struct {
	byAbs map[string][]byte
}

func newFileOverlay(repoPath string, sources map[string][]byte) *fileOverlay {
	if len(sources) == 0 {
		return nil
	}
	m := make(map[string][]byte, len(sources))
	for rel, b := range sources {
		full := rel
		if !filepath.IsAbs(full) { //factpath:host
			full = filepath.Join(repoPath, rel) //factpath:host
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		m[absOverlayKey(full)] = cp
	}
	return &fileOverlay{byAbs: m}
}

func withFileOverlay(ctx context.Context, ov *fileOverlay) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ov == nil {
		return ctx
	}
	return context.WithValue(ctx, overlayKey{}, ov)
}

func absOverlayKey(path string) string {
	a, err := filepath.Abs(path) //factpath:host
	if err != nil {
		return filepath.Clean(path) //factpath:host
	}
	return filepath.Clean(a) //factpath:host
}

// overlayReadFile returns captured bytes for path when ctx carries a session
// overlay; otherwise it reads the live filesystem.
func overlayReadFile(ctx context.Context, path string, inputScopes ...*inputscope.Scope) ([]byte, error) {
	inputScope := inputscope.First(inputScopes)
	if !inputScope.Allowed(path, false) {
		return inputScope.ReadFile(path)
	}
	if ctx != nil {
		if ov, ok := ctx.Value(overlayKey{}).(*fileOverlay); ok && ov != nil {
			if b, ok := ov.byAbs[absOverlayKey(path)]; ok {
				out := make([]byte, len(b))
				copy(out, b)
				return out, nil
			}
		}
	}
	return inputScope.ReadFile(path)
}
