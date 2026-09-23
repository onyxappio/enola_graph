package tsextractor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"path/filepath"
)

type overlayKey struct{}

type probeKey struct{}

// A recorded observation says which bytes a discovery read actually used and
// where they came from. The distinction matters: a path the capture did not
// carry was answered by the live tree, and recording that as "absent" would
// claim the capture fenced a read it never saw.
const (
	observedFromOverlay = "o:"
	observedFromLive    = "l:"
	observedMissing     = "m:"
)

// overlayProbe records every configuration read a discovery pass makes, with
// the digest of the bytes the reader was actually handed. Reuse is then checked
// against those observations rather than against an assumption that two callers
// were handed the same capture.
type overlayProbe struct {
	mu   sync.Mutex
	seen map[string]string
}

func newOverlayProbe() *overlayProbe {
	return &overlayProbe{seen: map[string]string{}}
}

func withOverlayProbe(ctx context.Context, p *overlayProbe) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, probeKey{}, p)
}

func sideReadDigest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// record keeps the first observation of a path. A discovery pass that reads the
// same package.json twice read the same bytes both times, and keeping the first
// makes the recorded set independent of walk interleaving.
func (p *overlayProbe) record(key, digest string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if _, ok := p.seen[key]; !ok {
		p.seen[key] = digest
	}
	p.mu.Unlock()
}

func (p *overlayProbe) snapshot() map[string]string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]string, len(p.seen))
	for k, v := range p.seen {
		out[k] = v
	}
	return out
}

func probeFrom(ctx context.Context) *overlayProbe {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(probeKey{}).(*overlayProbe)
	return p
}

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
// overlay; otherwise it reads the live filesystem. Either way the decision and
// the bytes are recorded on ctx's probe, if it carries one, so a snapshot built
// from these reads can later be checked against the bytes another caller will
// observe instead of being trusted on the strength of having been built.
func overlayReadFile(ctx context.Context, path string, inputScopes ...*inputscope.Scope) ([]byte, error) {
	inputScope := inputscope.First(inputScopes)
	probe := probeFrom(ctx)
	if !inputScope.Allowed(path, false) {
		// Policy refusal is not an observation of the tree: the same path under
		// the same scope is refused again, and recording it would make the
		// refusal look like a byte the capture could contradict.
		return inputScope.ReadFile(path)
	}
	if ctx != nil {
		if ov, ok := ctx.Value(overlayKey{}).(*fileOverlay); ok && ov != nil {
			if b, ok := ov.byAbs[absOverlayKey(path)]; ok {
				out := make([]byte, len(b))
				copy(out, b)
				probe.record(absOverlayKey(path), observedFromOverlay+sideReadDigest(b))
				return out, nil
			}
		}
	}
	data, err := inputScope.ReadFile(path)
	if err != nil {
		probe.record(absOverlayKey(path), observedMissing)
		return nil, err
	}
	probe.record(absOverlayKey(path), observedFromLive+sideReadDigest(data))
	return data, nil
}
