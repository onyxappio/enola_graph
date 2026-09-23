package tsextractor

import (
	"context"
	"fmt"
	"path/filepath"
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

	// statReads is every presence decision this snapshot made, and walkedDirs
	// the name set of every directory it fully enumerated. A discovery answer
	// can rest on a file having been absent, or on a directory not yet
	// containing a package, and neither of those leaves a byte behind for
	// sideReads to hold.
	statReads  map[string]string
	walkedDirs map[string]map[string]string
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
	ctx = withDiscoveryWalkCache(withOverlayProbe(withFileOverlay(ctx, ov), probe))
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
	d.statReads = probe.statSnapshot()
	d.walkedDirs = probe.dirSnapshot()
	graphprofile.Since("ts_discovery_build", t, fmt.Sprintf("side_reads=%d stat_reads=%d walked_dirs=%d captured=%d",
		len(d.sideReads), len(d.statReads), len(d.walkedDirs), captured))
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
		if want, seen := d.sideReads[key]; seen {
			if want == observedMissing {
				return false
			}
			if want[len(observedFromOverlay):] != sideReadDigest(b) {
				return false
			}
			continue
		}
		switch d.statReads[key] {
		case observedStatMissing:
			// An answer the snapshot reached because this path was not there.
			// This caller's capture says it is, and carries its bytes.
			return false
		case observedStatDir:
			// The snapshot saw a directory; the capture carries file bytes for
			// the same path. Whatever moved, the snapshot did not see it.
			return false
		case observedStatFile:
			// Present when the snapshot looked and present now. Nothing the
			// snapshot holds rests on the bytes, only on the name.
			continue
		}
		if names, walked := d.walkedDirs[absOverlayKey(filepath.Dir(key))]; walked { //factpath:host — overlay byAbs keys are abs host paths
			// The snapshot enumerated this directory, so it can answer both
			// whether this name was in it and what it was. A package.json or a
			// config that appears in a walked directory afterwards is a
			// discovery input the snapshot decided without, and so is one the
			// snapshot enumerated as something other than the file whose bytes
			// this caller's capture is carrying.
			if kind := names[filepath.Base(key)]; kind != observedEntryFile {
				return false
			}
		}
		// Otherwise the snapshot neither read this file, nor asked whether it
		// was there, nor enumerated the directory holding it, so no answer it
		// holds rests on it.
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

// ReuseDiscovery answers a run with a snapshot an earlier run built, when this
// run can prove it again, and nil otherwise. It never builds one: a caller with
// nothing proven keeps exactly the behaviour it had before, which is to let
// whichever reader needs a discovery build its own.
//
// Across runs the capture proof is necessary but not sufficient. A capture
// carries the content inputs a session already knows about, so a configuration
// file that did not exist when the snapshot ran is absent from the capture too,
// and absence from a capture is not evidence of absence on disk. Within a run
// that gap is closed at the other end, by the run-end fence re-observing the
// analysis inputs; a snapshot retained past its run has outlived that fence and
// has to re-ask for itself.
//
// So every observation this snapshot made is asked again: the bytes it read,
// the presences it decided on, and the directory contents it enumerated. The
// capture proof covers only the reads a capture carries, and the first
// discovery of a resident session carries almost none - a package.json the
// snapshot read from the live tree is invisible to a capture fence, and a
// dependency edit to it changes framework detection, package names and aliases
// without contradicting a single captured byte.
//
// The cost is bounded by what the snapshot actually observed rather than by the
// size of the tree, and every part of it is reported separately so a retained
// snapshot accounts for the work it did and not only the work it avoided.
func (e *TSExtractor) ReuseDiscovery(root string, sources map[string][]byte, retained *Discovery) (*Discovery, DiscoveryRecheck) {
	if retained == nil {
		return nil, DiscoveryRecheck{}
	}
	ov := newFileOverlay(root, sources)
	if !retained.reusableFor(root, e.inputScope, ov) {
		return nil, DiscoveryRecheck{}
	}
	cost, unmoved := retained.reobserve(ov)
	if !unmoved {
		return nil, cost
	}
	return retained, cost
}

// DiscoveryRecheck is what proving a retained snapshot cost, split by the kind
// of observation re-asked. It is work this reuse introduces, so it is reported
// apart from the discovery builds it avoids and never folded into them.
type DiscoveryRecheck struct {
	// Names is the presence re-observations, Bytes the side reads re-read and
	// re-digested, Dirs the directories re-enumerated.
	Names int
	Bytes int
	Dirs  int
}

// Add returns the two costs summed, for a caller accumulating across the
// extractions of one run.
func (c DiscoveryRecheck) Add(o DiscoveryRecheck) DiscoveryRecheck {
	return DiscoveryRecheck{Names: c.Names + o.Names, Bytes: c.Bytes + o.Bytes, Dirs: c.Dirs + o.Dirs}
}

func (c DiscoveryRecheck) String() string {
	return fmt.Sprintf("names=%d bytes=%d dirs=%d", c.Names, c.Bytes, c.Dirs)
}

// DiscoveryFor answers with a retained snapshot this run proves, and otherwise
// builds a new one. The bool says which happened, so a caller counts the work it
// actually did rather than the work it asked for.
func (e *TSExtractor) DiscoveryFor(ctx context.Context, root string, sources map[string][]byte, retained *Discovery) (*Discovery, bool, DiscoveryRecheck) {
	reused, cost := e.ReuseDiscovery(root, sources, retained)
	if reused != nil {
		return reused, true, cost
	}
	return e.newDiscovery(ctx, root, newFileOverlay(root, sources), len(sources)), false, cost
}

// reobserve re-asks the tree about everything this snapshot decided on, for a
// caller proving a snapshot that has outlived the run which built it.
//
// Three kinds of observation, because the snapshot rests on three kinds of
// fact. Bytes: a package.json, a tsconfig or an external extends target the
// snapshot read decides framework gates, package names and alias roots, and
// when the read was answered by the live tree no capture fence can contradict
// it. Presence: a configuration file that was absent, or present, when a
// detector stat'd it. Membership: what a directory the snapshot walked actually
// contained, which is what catches a package or a nested configuration created
// since - including one in a directory that never appeared in any capture.
//
// Reads the caller's own capture carries are skipped here and not trusted: the
// capture proof in reusableFor has already compared them byte for byte, and
// those are the run's fenced bytes rather than something this snapshot may
// re-read behind the run's back.
//
// The first disagreement stops the walk. The cost returned is what was actually
// spent reaching that point, not what a full re-observation would have cost,
// because that is the number the caller is accounting for.
func (d *Discovery) reobserve(ov *fileOverlay) (DiscoveryRecheck, bool) {
	cost := DiscoveryRecheck{}
	for key, want := range d.statReads {
		cost.Names++
		now := observedStatMissing
		if info, err := d.scope.Stat(key); err == nil {
			now = observedStatFile
			if info.IsDir() {
				now = observedStatDir
			}
		}
		if now != want {
			return cost, false
		}
	}
	for key, want := range d.sideReads {
		if _, carried := overlayBytes(ov, key); carried {
			continue
		}
		if strings.HasPrefix(want, observedFromOverlay) {
			// Answered from the snapshot's own capture and not carried by this
			// caller's. reusableFor refuses that outright, so reaching here
			// would mean the two fences disagree; treat it as a refusal rather
			// than re-reading a file the snapshot never read from the tree.
			return cost, false
		}
		cost.Bytes++
		b, err := d.scope.ReadFile(key)
		if err != nil {
			if want != observedMissing {
				return cost, false
			}
			continue
		}
		if want != observedFromLive+sideReadDigest(b) {
			return cost, false
		}
	}
	for dir, want := range d.walkedDirs {
		cost.Dirs++
		entries, err := d.scope.ReadDir(dir)
		if err != nil {
			// The snapshot enumerated this directory; a listing that now fails
			// is a change to what it enumerated, whatever the cause.
			return cost, false
		}
		if len(entries) != len(want) {
			return cost, false
		}
		for _, e := range entries {
			// Kind as well as name: a plain file replaced by a directory of the
			// same name leaves this listing identical, while the walks that
			// stepped over a file now descend into whatever it became.
			if want[e.Name()] != entryKind(e) {
				return cost, false
			}
		}
	}
	return cost, true
}
