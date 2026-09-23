package tsextractor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphprofile"
)

// Discovery is one run's repository-wide TypeScript discovery: the selected TS
// root, the framework and ORM gates, the per-package gates, the package names,
// the alias roots and the decoded package.json export declarations.
//
// Every extraction of a run needs exactly these, and before this value each one
// read them again for itself. SessionContext alone walked the whole tree four
// times for Nuxt, because detectNuxt is defined as collectNuxtPackages and the
// alias fallback asks for both; each planner preview then repeated the Nuxt,
// package-gate, package-name and alias walks a third and fourth time.
//
// The snapshot is built once, is never mutated afterwards, and travels only
// through the caller that built it. There is no process-global cache: two runs
// of the same repository never share one, and a caller that does not thread it
// keeps its previous behaviour exactly.
type Discovery struct {
	root  string
	scope *inputscope.Scope

	tsRoot      string
	tsRootFound bool

	nextJS    bool
	vue       bool
	nuxt      bool
	svelteKit bool
	ember     bool
	reactNav  bool
	angular   bool
	typeORM   bool
	drizzle   bool
	prisma    bool

	nuxtPkgs      []string
	gates         packageGates
	pkgNames      map[string]string
	aliasRoots    []tsAliasRoot
	exportSources []packageExportSource

	// sideReads is every configuration read this snapshot made, recorded as the
	// digest of the bytes the reader was actually handed and where they came
	// from: this run's capture, the live tree, or nothing at all. Reuse is
	// checked against these observations rather than against an assumption that
	// two callers were handed the same capture.
	sideReads map[string]string
}

// NewDiscovery reads the repository's discovery inputs once, through the same
// readers an extraction would run for itself and under the same captured
// overlay, and freezes the result for the rest of the run. sources is the
// capture the run has already fenced; nil reads the live tree, which is what an
// uncaptured caller does today.
func (e *TSExtractor) NewDiscovery(ctx context.Context, root string, sources map[string][]byte) *Discovery {
	return e.newDiscovery(ctx, root, newFileOverlay(root, sources), len(sources))
}

// newDiscovery is the shared body. A caller that has already built the overlay
// passes it rather than a source map, because newFileOverlay copies every
// captured byte and a run has no reason to hold two copies of its own capture.
func (e *TSExtractor) newDiscovery(ctx context.Context, root string, ov *fileOverlay, captured int) *Discovery {
	scope := e.inputScope
	probe := newOverlayProbe()
	ctx = withOverlayProbe(withFileOverlay(ctx, ov), probe)
	d := &Discovery{root: root, scope: scope}
	t := time.Now()

	d.tsRoot, d.tsRootFound = findTSRoot(ctx, root, scope)
	d.typeORM, d.drizzle, d.prisma = detectORMs(ctx, root, scope)
	d.nextJS = detectNextJS(ctx, root, scope)
	d.vue = detectVue(ctx, root, scope)

	tDisc := time.Now()
	d.nuxtPkgs = collectNuxtPackages(ctx, root, scope)
	// detectNuxt is len(collectNuxtPackages)>0, so this one walk is also the
	// answer every detectNuxt call in the run was asking for.
	d.nuxt = len(d.nuxtPkgs) > 0
	graphprofile.Since("ts_disc_nuxt_packages", tDisc, fmt.Sprintf("pkgs=%d", len(d.nuxtPkgs)))

	d.svelteKit = detectSvelteKit(ctx, root, scope)
	d.ember = detectEmber(ctx, root, scope)
	d.reactNav = detectReactNavigation(ctx, root, scope)
	d.angular = detectAngular(ctx, root, scope)

	tDisc = time.Now()
	d.gates = collectPackageGates(ctx, root, scope)
	graphprofile.Since("ts_disc_package_gates", tDisc, "")

	tDisc = time.Now()
	d.pkgNames = collectPackageNames(ctx, root, scope)
	graphprofile.Since("ts_disc_package_names", tDisc, fmt.Sprintf("names=%d", len(d.pkgNames)))

	tDisc = time.Now()
	roots := collectTSAliasRoots(ctx, root, scope)
	if d.svelteKit {
		roots = withSvelteKitAliasFallbacks(ctx, root, roots, scope)
	}
	if d.nuxt {
		roots = withNuxtAliasFallbacks(ctx, root, roots, d.nuxtPkgs, scope)
	}
	d.aliasRoots = roots
	graphprofile.Since("ts_disc_alias_roots", tDisc, fmt.Sprintf("roots=%d", len(d.aliasRoots)))

	tDisc = time.Now()
	d.exportSources = collectPackageExportSources(ctx, root, scope)
	graphprofile.Since("ts_disc_package_exports", tDisc, fmt.Sprintf("packages=%d", len(d.exportSources)))

	d.sideReads = probe.snapshot()
	graphprofile.Since("ts_discovery_build", t, fmt.Sprintf("side_reads=%d captured=%d", len(d.sideReads), captured))
	return d
}

// reusableFor reports whether a snapshot another caller built observed the same
// repository, the same policy scope and the same configuration bytes this
// extraction will observe.
//
// Two checks, both bounded by the size of a capture rather than by the size of
// the tree. Every read the snapshot answered from its own capture must be
// carried, byte for byte, by this caller's capture too: otherwise this caller
// would have read the live tree there and could have seen something else. And
// every file this caller's capture carries that the snapshot read at all must
// agree with what the snapshot read: otherwise this caller's fenced bytes
// contradict an observation already baked into the snapshot.
//
// What this does not prove is that the live tree has not changed underneath an
// uncaptured read. That is deliberate, and it is the run's captured-input fence
// that covers it: a discovery snapshot is one observation of one run, and
// nothing here is retained past it.
func (d *Discovery) reusableFor(root string, scope *inputscope.Scope, ov *fileOverlay) bool {
	if d == nil || d.root != root || d.scope != scope {
		return false
	}
	for key, want := range d.sideReads {
		if !strings.HasPrefix(want, observedFromOverlay) {
			continue
		}
		b, ok := overlayBytes(ov, key)
		if !ok || observedFromOverlay+sideReadDigest(b) != want {
			return false
		}
	}
	if ov == nil {
		return true
	}
	for key, b := range ov.byAbs {
		want, seen := d.sideReads[key]
		if !seen {
			// The snapshot never consulted this file, so no answer it holds
			// rests on it. A file that only becomes a discovery input after a
			// membership or configuration change reaches discovery as a rebuilt
			// snapshot, not as a reused one.
			continue
		}
		if want == observedMissing {
			return false
		}
		if want[len(observedFromOverlay):] != sideReadDigest(b) {
			return false
		}
	}
	return true
}

// overlayBytes reports the captured bytes an overlay carries for an already
// absolute, cleaned key.
func overlayBytes(ov *fileOverlay, key string) ([]byte, bool) {
	if ov == nil {
		return nil, false
	}
	b, ok := ov.byAbs[key]
	return b, ok
}

// aliasRootsFor hands out the run's alias roots. The copy is not politeness:
// withSvelteKitAliasFallbacks and withNuxtAliasFallbacks append, and an append
// into shared spare capacity by one caller would rewrite what the next one
// reads.
func (d *Discovery) aliasRootsFor() []tsAliasRoot {
	out := make([]tsAliasRoot, len(d.aliasRoots))
	copy(out, d.aliasRoots)
	return out
}

// packageAliasesFor resolves the shared package.json declarations against this
// caller's own known-file set, through the same parse the unshared reader runs.
// The walk is shared; the resolution is not, because the known-file sets of a
// context fingerprint and of an extraction genuinely differ.
func (d *Discovery) packageAliasesFor(knownFiles map[string]bool) map[string]tsAlias {
	return aliasesFromExportSources(d.exportSources, knownFiles)
}
