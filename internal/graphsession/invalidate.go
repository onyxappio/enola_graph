package graphsession

import (
	"path/filepath"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
)

// invalidateTS computes the file-granularity reparse set from content-dirty
// seeds by reverse-closing normalized file dependencies to a fixed point.
// Old records supply the previous import graph; the current owned/hash set is
// the new filename context (new, deleted, and renamed paths).
//
// broadenAll is set when the stored graph cannot be used as a file dependency
// graph (no normalized edges despite imports). The caller must re-extract every
// owned TypeScript file and report an explicit fallback.
func invalidateTS(dirty map[string]bool, prev map[string]*tsextractor.FileRecord, owned []string, hashes map[string]string) (map[string]bool, bool, string) {
	if dirty == nil {
		dirty = map[string]bool{}
	}
	if prev == nil {
		prev = map[string]*tsextractor.FileRecord{}
	}
	known := sessionKnownFiles(owned)

	withSpecs := 0
	withResolved := 0
	for _, rec := range prev {
		if rec == nil {
			continue
		}
		if len(rec.ImportSpecs) > 0 {
			withSpecs++
		}
		if len(rec.ResolvedFiles) > 0 {
			withResolved++
		}
	}
	if withSpecs > 0 && withResolved == 0 {
		storedResolution := false
		for _, rec := range prev {
			if rec == nil {
				continue
			}
			if rec.ImportComplete || len(rec.UnresolvedSpecs) > 0 || len(rec.ResolvedFiles) > 0 {
				storedResolution = true
				break
			}
		}
		if !storedResolution {
			return dirty, true, "stored import graph has no file-normalized dependencies; re-extracting TypeScript"
		}
	}

	filenameChanged := false
	deleted := map[string]bool{}
	for _, f := range owned {
		if prev[f] == nil && prev[filepath.ToSlash(f)] == nil {
			dirty[f] = true
			filenameChanged = true
		}
	}
	for path, rec := range prev {
		if rec == nil {
			continue
		}
		slash := filepath.ToSlash(path)
		if _, still := hashes[path]; !still {
			if _, still2 := hashes[slash]; !still2 {
				dirty[path] = true
				deleted[slash] = true
				filenameChanged = true
			}
		}
	}

	// Deleted files always reverse-close importers. Content-only dirty files
	// wait until extraction shows a changed import/export surface.
	for p, d := range reverseClose(deleted, prev) {
		if d {
			dirty[p] = true
		}
	}

	if filenameChanged {
		priorKnown := priorKnownFiles(prev)
		for path, rec := range prev {
			if rec == nil {
				continue
			}
			if recordRebound(rec, priorKnown, known) {
				dirty[path] = true
			}
		}
	}

	return dirty, false, ""
}

// sessionKnownFiles mirrors the extractor's knownFiles: TypeScript sources only,
// so Angular templates in the owned set are not resolution targets here either.
// The planner builds the new resolution context with the same function, from the
// same inventory the extractor receives.
func sessionKnownFiles(files []string) map[string]bool {
	known := make(map[string]bool, len(files))
	for _, f := range files {
		if slash := filepath.ToSlash(f); tsextractor.IsSessionSource(slash, false) {
			known[slash] = true
		}
	}
	return known
}

// priorKnownFiles rebuilds the resolution context the cached records were parsed
// against, so an old target can be compared with the new one.
func priorKnownFiles(prev map[string]*tsextractor.FileRecord) map[string]bool {
	known := make(map[string]bool, len(prev))
	for path := range prev {
		if slash := filepath.ToSlash(path); tsextractor.IsSessionSource(slash, false) {
			known[slash] = true
		}
	}
	return known
}

// recordRebound replays one cached record's whole import surface against the two
// resolution universes. UnresolvedSpecs is replayed alongside ImportSpecs even
// though summarizeFacts always records an unresolved specifier in both: a record
// written before that was true would otherwise go unchecked. A specifier that is
// still unresolved is not dirt by itself - only a moved target is.
func recordRebound(rec *tsextractor.FileRecord, priorKnown, known map[string]bool) bool {
	if rec == nil {
		return false
	}
	for _, spec := range rec.ImportSpecs {
		if importRebound(spec, priorKnown, known) {
			return true
		}
	}
	for _, spec := range rec.UnresolvedSpecs {
		if importRebound(spec, priorKnown, known) {
			return true
		}
	}
	return false
}

// importRebound reports whether a membership change moves where one cached import
// specifier resolves: it becomes satisfiable, it stops resolving, or it still
// resolves but now names a different file because an added path wins the exact /
// extension / folder-index precedence in resolveModuleFile. Cached specs are the
// extractor's own ImportSpecs, already carrying tsconfig alias and
// relative-directory resolution, so this replay is what the next extraction sees.
//
// A specifier that resolves in neither universe is not reported. resolveModuleFile
// only ever consults a finite candidate key set - the target itself, the target
// plus each module extension, and the folder index under it - so failing in both
// means no candidate key exists in either known set, and therefore no added or
// deleted file can have touched this specifier. Reporting it anyway dirtied every
// importer of a bare subpath package (node:fs/promises, react-native/Libraries/*),
// because internalSpec treats any unprefixed specifier containing a slash as
// internal; that is noise, not conservatism.
func importRebound(spec string, priorKnown, known map[string]bool) bool {
	spec = filepath.ToSlash(spec)
	// summarizeFacts resolves against known files before it classifies a
	// specifier as external, so no specifier is exempt from this comparison.
	after, hasAfter := tsextractor.NormalizeImportTarget(spec, known)
	before, hadBefore := tsextractor.NormalizeImportTarget(spec, priorKnown)
	return hadBefore != hasAfter || before != after
}

func reverseClose(seeds map[string]bool, recs map[string]*tsextractor.FileRecord) map[string]bool {
	dirty := map[string]bool{}
	for p, d := range seeds {
		if d {
			dirty[filepath.ToSlash(p)] = true
		}
	}
	importers := map[string][]string{}
	for path, rec := range recs {
		if rec == nil {
			continue
		}
		from := filepath.ToSlash(path)
		for _, dep := range rec.ResolvedFiles {
			importers[filepath.ToSlash(dep)] = append(importers[filepath.ToSlash(dep)], from)
		}
		for _, dep := range rec.SideReads {
			importers[filepath.ToSlash(dep)] = append(importers[filepath.ToSlash(dep)], from)
		}
	}
	changed := true
	for changed {
		changed = false
		snapshot := make([]string, 0, len(dirty))
		for p := range dirty {
			snapshot = append(snapshot, p)
		}
		for _, d := range snapshot {
			for _, user := range importers[d] {
				if !dirty[user] {
					dirty[user] = true
					changed = true
				}
			}
		}
	}
	return dirty
}

func surfaceChanged(old, neu *tsextractor.FileRecord) bool {
	if neu == nil {
		return true
	}
	if old == nil {
		return true
	}
	return !eqStrings(old.Declared, neu.Declared) ||
		!eqStrings(old.Reexports, neu.Reexports) ||
		!eqStrings(old.ResolvedFiles, neu.ResolvedFiles) ||
		!eqStrings(old.UnresolvedSpecs, neu.UnresolvedSpecs) ||
		!eqStrings(old.ImportSpecs, neu.ImportSpecs)
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// replayableDependent reports whether a cached record proves that this file's
// bindings into its dependencies can be rebuilt from the record alone. A
// dependent that proves it only has to be reparsed when a dependency publishes a
// changed import/export surface; one that does not keeps the old rule of being
// reparsed on any dirt underneath it. GraphQL SDL composition and gRPC stubs
// bind across files without going through the import graph, and a record written
// before import resolution was stored proves nothing at all. The check is on the
// DEPENDENT, not on the changed file: an ordinary TypeScript edit can feed a
// consumer whose own analysis reads more than its imports.
func replayableDependent(rec *tsextractor.FileRecord) bool {
	if rec == nil || !rec.ImportComplete {
		return false
	}
	if rec.GraphQLServer || len(rec.GraphQLSDL) > 0 || rec.GRPC != nil {
		return false
	}
	// Only kinds whose binder records its cross-file reads. An empty kind is a
	// record this build never wrote (fillRecord always stores one), so it is a
	// hand-built or foreign record and proves nothing.
	switch rec.ParseKind {
	case "ts", "vue", "svelte":
		return true
	default:
		return false
	}
}

// surfaceDependents is one hop of the observed-surface closure. Dependents of a
// file whose reparse published a changed surface are always taken; dependents of
// a file that is merely dirty are taken only when they cannot prove their own
// bindings, or when broad forces the old reachability rule for the whole session.
//
// One hop, not a fixed point: the caller parses the result and calls again, so
// every further hop is justified by a surface change someone actually observed
// rather than by reachability. Iterating terminates because have only grows.
func surfaceDependents(have, changed map[string]bool, recs, prev map[string]*tsextractor.FileRecord, broad bool) map[string]bool {
	importers := map[string][]string{}
	for path, rec := range recs {
		if rec == nil {
			continue
		}
		from := filepath.ToSlash(path)
		for _, dep := range rec.ResolvedFiles {
			dep = filepath.ToSlash(dep)
			importers[dep] = append(importers[dep], from)
		}
		for _, dep := range rec.SideReads {
			dep = filepath.ToSlash(dep)
			importers[dep] = append(importers[dep], from)
		}
	}
	out := map[string]bool{}
	take := func(dep string, all bool) {
		for _, user := range importers[dep] {
			if have[user] || out[user] {
				continue
			}
			if all || !replayableDependent(prev[user]) {
				out[user] = true
			}
		}
	}
	for f, d := range have {
		if d {
			take(filepath.ToSlash(f), broad)
		}
	}
	for f, d := range changed {
		if d {
			take(filepath.ToSlash(f), true)
		}
	}
	return out
}

// declaredNameDelta reports the names a reparse added to and removed from the
// declared surface, including the names a retired file took with it.
func declaredNameDelta(prev, neu map[string]*tsextractor.FileRecord, retired map[string]bool) (added, removed map[string]bool) {
	added, removed = map[string]bool{}, map[string]bool{}
	for path, rec := range neu {
		if rec == nil {
			continue
		}
		old := prev[filepath.ToSlash(path)]
		have := map[string]bool{}
		if old != nil {
			for _, n := range old.Declared {
				have[n] = true
			}
		}
		now := map[string]bool{}
		for _, n := range rec.Declared {
			now[n] = true
			if !have[n] {
				added[n] = true
			}
		}
		if old != nil {
			for _, n := range old.Declared {
				if !now[n] {
					removed[n] = true
				}
			}
		}
	}
	for path, old := range prev {
		if old == nil || !retired[filepath.ToSlash(path)] {
			continue
		}
		for _, n := range old.Declared {
			removed[n] = true
		}
	}
	return added, removed
}

// nameDependents lists cached owners whose Referenced surface mentions a name the
// delta added or removed. buildIndex resolves Fact.Name globally rather than along
// import edges, so such an owner gains or loses an edge with no file dependency
// connecting it to the change. Owners already in have are skipped.
func nameDependents(prev map[string]*tsextractor.FileRecord, added, removed map[string]bool, have map[string]bool) map[string]bool {
	out := map[string]bool{}
	if len(added) == 0 && len(removed) == 0 {
		return out
	}
	for path, rec := range prev {
		if rec == nil {
			continue
		}
		id := filepath.ToSlash(path)
		if have[id] {
			continue
		}
		for _, n := range rec.Referenced {
			if added[n] || removed[n] {
				out[id] = true
				break
			}
		}
	}
	return out
}

// sideReadDependents lists owners whose recorded side read moved. A named import
// binds through the re-export chain, and the binder records every file it read on
// the way, so a side read is a dependency no import specifier names.
//
// Moved bytes alone are not the question: what the binder resolved through that
// file is decided by what a reparse of it produces, so a body edit under a side
// read changes nothing for the owner. Skipping it therefore needs that reparse
// to have happened and to have come back the same - see sideReadProven. An
// unparsed side read, a deleted one, or an owner that cannot prove its own
// cross-file reads all keep the old byte comparison.
func sideReadDependents(have, proven map[string]bool, prev map[string]*tsextractor.FileRecord, hashes map[string]string, broad bool) map[string]bool {
	out := map[string]bool{}
	for path, rec := range prev {
		if rec == nil || len(rec.SideReadHashes) == 0 {
			continue
		}
		id := filepath.ToSlash(path)
		if have[id] || out[id] {
			continue
		}
		provable := !broad && replayableDependent(rec)
		for side, want := range rec.SideReadHashes {
			got, ok := lookupHash(hashes, side)
			if ok && got == want {
				continue
			}
			if provable && ok && proven[filepath.ToSlash(side)] {
				continue
			}
			out[id] = true
			break
		}
	}
	return out
}

// refreshProvenSideReads writes back the skips sideReadDependents just proved.
// A consumer whose side read moved bytes without moving that file's surface is
// left unparsed, so its cached SideReadHashes still name the old bytes. The
// next delta has no reparse of the side read to explain them with and would
// reparse the consumer for a change already shown not to matter, which only
// defers the work this closure exists to avoid.
//
// It clones: the records for unparsed files are the ones the last completed
// state holds, and the clones reach disk only through the state End commits.
// An entry with no proof behind it is left stale on purpose - that one still
// invalidates the old way next time.
func refreshProvenSideReads(recs map[string]*tsextractor.FileRecord, parsed, proven map[string]bool, hashes map[string]string, broad bool) {
	if broad {
		return
	}
	for path, rec := range recs {
		if rec == nil || len(rec.SideReadHashes) == 0 {
			continue
		}
		id := filepath.ToSlash(path)
		if parsed[id] || !replayableDependent(rec) {
			continue
		}
		var fresh map[string]string
		for side, want := range rec.SideReadHashes {
			got, ok := lookupHash(hashes, side)
			if !ok || got == want {
				continue
			}
			if !proven[filepath.ToSlash(side)] {
				continue
			}
			if fresh == nil {
				fresh = make(map[string]string, len(rec.SideReadHashes))
				for k, v := range rec.SideReadHashes {
					fresh[k] = v
				}
			}
			fresh[side] = got
		}
		if fresh == nil {
			continue
		}
		clone := *rec
		clone.SideReadHashes = fresh
		recs[path] = &clone
	}
}

// sideReadProven reports whether reparsing a side-read source showed that
// nothing an owner could have taken from its bytes moved.
//
// The five fields surfaceChanged compares are the wrong surface here. A named
// import binds through a re-export chain by reading these bytes, and what it
// binds to is an export-name mapping no record field describes: Reexports comes
// from the dependency facts, so it names the module a barrel forwards to and not
// which name arrives as which. `export { round } from './b'` renamed to
// `export { round as renamed } from './b'` moves none of the five, yet every
// consumer of that name has to rebind.
//
// So the proof compares the binder's own view instead: FileRecord.ExportSurface
// is the encoded namedExportIndex, which is the whole of what followNamedExportFile
// reads out of these bytes. An unrecorded surface - a record written before the
// field existed - is unknown, never equal, and so never proven.
//
// Two conditions are kept on top of it, both conservative. The file must resolve
// no import of its own, so that it cannot be a link in a chain whose far end moved
// without this record moving; and Declared and Referenced must hold, because a
// side read is a consumer reading source bytes and the export index is only the
// part of that reading which is modelled here. What remains provable is a file
// that forwards nothing, exports the same names, and merely moved code inside
// them - which is the body edit this optimization exists for.
func sideReadProven(prev, neu *tsextractor.FileRecord) bool {
	if prev == nil || neu == nil {
		return false
	}
	if !passesNoBinding(prev) || !passesNoBinding(neu) {
		return false
	}
	// Resolving no import is not on its own enough, and neither is any field that
	// summarizes declarations. `export default round` changed to `export default
	// ceil`, `export function round` demoted to `function round`, and `export {
	// round, ceil }` narrowed to `export { ceil }` all leave Declared and
	// Referenced exactly as they were while consumers of those names rebind or
	// stop resolving. Each of the three moves the export index, so that is what
	// has to be compared.
	if !prev.ExportSurfaceRecorded || !neu.ExportSurfaceRecorded {
		return false
	}
	if !eqStrings(prev.ExportSurface, neu.ExportSurface) {
		return false
	}
	return eqStrings(prev.Declared, neu.Declared) && eqStrings(prev.Referenced, neu.Referenced)
}

// passesNoBinding reports whether a record describes a file that resolves no
// import of its own, and so cannot be a link in anyone's re-export chain. The
// extractor answers the same question when it decides whether to record an
// export surface at all, so both ask FileRecord rather than each spelling the
// condition out; a narrower extractor would leave the surface unrecorded, which
// reads as unknown and fails closed.
func passesNoBinding(rec *tsextractor.FileRecord) bool {
	return rec.BindsNoImports()
}

// provenSideReadSources narrows a parsed set to the files sideReadProven holds
// for, keyed the way the closures address files.
func provenSideReadSources(parsed map[string]bool, prev, neu map[string]*tsextractor.FileRecord) map[string]bool {
	out := make(map[string]bool, len(parsed))
	for id, p := range parsed {
		if !p {
			continue
		}
		slash := filepath.ToSlash(id)
		if sideReadProven(recordFor(prev, slash), recordFor(neu, slash)) {
			out[slash] = true
		}
	}
	return out
}

// recordFor reads a record map that may be keyed by either path spelling.
func recordFor(m map[string]*tsextractor.FileRecord, id string) *tsextractor.FileRecord {
	if rec, ok := m[id]; ok {
		return rec
	}
	return m[filepath.FromSlash(id)]
}
