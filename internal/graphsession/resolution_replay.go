package graphsession

import (
	"path/filepath"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
)

// takeRule says which dependents one hop takes for a single dependency.
type takeRule int

const (
	// takeProvenBindings is the rule for a dependency that is merely dirty: a
	// dependent that proves its own cross-file reads is left alone.
	takeProvenBindings takeRule = iota
	// takeNameScoped is the rule for a dependency whose reparse moved only the
	// names it declares. The rebinding that can follow is enumerated by name,
	// not by edge, so a dependent that proves its bindings and forwards no
	// names of its own is left to nameDependents.
	takeNameScoped
	// takeLocalExportProven is the rule for a dependency whose reparse moved
	// only where its OWN bindings point, while the export surface a consumer
	// binds through came back identical and context-free on both sides. A
	// dependent that binds every such name directly - not through a re-export
	// chain it recorded as a side read - and that binds by import rather than
	// by framework convention is left alone.
	takeLocalExportProven
	// takeEveryDependent is the old reachability rule.
	takeEveryDependent
)

// outgoingOnlySurfaceShift reports whether a reparse moved only where this
// file's own bindings point, leaving the surface consumers bind THROUGH it
// provably identical.
//
// It is the complement of nameOnlySurfaceShift on the same five fields, and the
// proof it needs is one nameOnlySurfaceShift does not: equality of the outgoing
// fields is visible on the record, while equality of the export surface is not.
// Declared cannot stand in for it. Declared holds every declaration whether
// exported or not, and an exported name need not be declared here at all, so a
// file can keep Declared and Reexports byte-identical while an exported name
// stops resolving locally and starts forwarding through one of the very imports
// that moved: `function work` plus `export { work }` becomes `function work`
// plus `import { work as w } from './late'` plus `export { w as work }`. A bare
// export clause records no Reexports entry, because only an export statement
// WITH a source emits the dependency fact that fills it. That transition moves
// the consumer's bound target from this file to './late', and nothing on either
// record says so.
//
// ExportSurfaceContextFree is what closes it. A context-free index names no
// module: every exported name resolved inside the file, under any alias map. So
// two equal context-free surfaces say every name a consumer can bind directly
// from this file still resolves to the same declaration in this file, whatever
// happened to the file's own imports. The forwarding transition above is refused
// because the new index is not context-free - an export specifier that is not
// locally bound sets that false before the import map is even consulted - and an
// unrecorded surface on either side is refused as unknown.
//
// Everything nameOnlySurfaceShift refuses is refused here for the same reasons,
// plus Declared itself: a name that appeared or vanished is a global-index
// question that declaredNameDelta and nameDependents own, and this rule must not
// answer it. ResolutionSpecs are refused too, because a framework resolver path
// is composed outside the import graph that this proof reasons about.
func outgoingOnlySurfaceShift(old, neu *tsextractor.FileRecord) bool {
	if old == nil || neu == nil {
		return false
	}
	if !replayableDependent(old) || !replayableDependent(neu) {
		return false
	}
	if old.Router != nil || neu.Router != nil {
		return false
	}
	if old.NuxtScope != neu.NuxtScope {
		return false
	}
	if !exportSurfaceProvenLocal(old) || !exportSurfaceProvenLocal(neu) {
		return false
	}
	if !eqStrings(old.ExportSurface, neu.ExportSurface) {
		return false
	}
	return eqStrings(old.Declared, neu.Declared) &&
		eqStrings(old.Reexports, neu.Reexports) &&
		eqStrings(old.ResolutionSpecs, neu.ResolutionSpecs) &&
		eqStrings(old.AutoImportDirs, neu.AutoImportDirs) &&
		eqStrings(old.NuxtAliases, neu.NuxtAliases)
}

// exportSurfaceProvenLocal reports whether this record carries a surface that
// was computed AND proven to name no module. Both bits are required: the first
// separates "exports nothing" from "was never indexed", the second separates a
// surface that holds under any alias map from one that does not.
func exportSurfaceProvenLocal(rec *tsextractor.FileRecord) bool {
	return rec != nil && rec.ExportSurfaceRecorded && rec.ExportSurfaceContextFree
}

// locallyBoundConsumer reports whether a dependent binds what it takes from a
// dependency through ordinary import resolution, so that a proven-local export
// surface on that dependency is the whole story for it.
//
// replayableDependent says the record's cross-file reads are recorded at all.
// The rest are refusals of every way a name can arrive without an import
// statement, because the export index says nothing about those: Nuxt
// auto-imports and module aliases, a Nuxt application scope at all, an assembled
// Router, and any parse kind but ts - a Vue or Svelte template binds components
// by convention, which is composition this proof does not cover.
func locallyBoundConsumer(rec *tsextractor.FileRecord) bool {
	if !replayableDependent(rec) {
		return false
	}
	if rec.ParseKind != "ts" || rec.Router != nil {
		return false
	}
	if rec.NuxtScope != "-" {
		return false
	}
	return len(rec.AutoImportDirs) == 0 && len(rec.NuxtAliases) == 0
}

// nameScopedDependent reports whether a dependent's rebinding under a
// name-only surface shift is fully described by the declared-name delta.
//
// It is replayableDependent plus one condition. A file with any Reexports
// forwards a dependency's names as its own, and Reexports is module-level -
// `export * from './a'` and `export { a } from './a'` are recorded identically
// as one dependency pair - so a barrel's own published surface grows or shrinks
// with names its record never mentions. nameDependents matches on Referenced
// and would not take it, so the barrel keeps the old rule.
func nameScopedDependent(rec *tsextractor.FileRecord) bool {
	return replayableDependent(rec) && len(rec.Reexports) == 0
}

// nameOnlySurfaceShift reports whether a reparse moved only the declared-name
// surface of a file, leaving everything a consumer binds THROUGH it untouched.
//
// surfaceChanged answers "did anything observable move"; this answers the
// narrower "did it move in a way that only the name delta describes". The five
// fields it compares split in two: Declared is the set of names other owners
// resolve against globally, while ImportSpecs, ResolvedFiles, UnresolvedSpecs
// and Reexports describe where this file's own bindings point. When only the
// first moves, every consequence for a consumer is that some name appeared or
// vanished, and declaredNameDelta plus nameDependents enumerate exactly the
// cached owners whose Referenced surface mentions one.
//
// Everything else is refused. A record whose binder does not record its
// cross-file reads, a GraphQL SDL or gRPC record that composes outside the
// import graph, a Router whose composed routes are assembled elsewhere, and
// moved AutoImportDirs - which change which names arrive without an import at
// all - each keep the old rule. So does an unrecorded or absent record on
// either side.
//
// The export surface deliberately does not appear here. A consumer's facts
// resolve by declared name, and the binder's named-export view matters to the
// re-export chain, which is what SideReadHashes and sideReadProven already
// judge: a dependency whose bytes moved fails that comparison unless its
// export surface came back identical, so a side-read consumer is taken by
// sideReadDependents in the same hop whatever this function says.
func nameOnlySurfaceShift(old, neu *tsextractor.FileRecord) bool {
	if old == nil || neu == nil {
		return false
	}
	if !replayableDependent(old) || !replayableDependent(neu) {
		return false
	}
	if old.Router != nil || neu.Router != nil {
		return false
	}
	// Same field, same reason, on the dependency side: a file that entered or
	// left a Nuxt application changed how bare names around it bind, which is
	// not a change confined to the names it declares.
	if old.NuxtScope != neu.NuxtScope {
		return false
	}
	return eqStrings(old.ImportSpecs, neu.ImportSpecs) &&
		eqStrings(old.ResolutionSpecs, neu.ResolutionSpecs) &&
		eqStrings(old.ResolvedFiles, neu.ResolvedFiles) &&
		eqStrings(old.UnresolvedSpecs, neu.UnresolvedSpecs) &&
		eqStrings(old.Reexports, neu.Reexports) &&
		eqStrings(old.AutoImportDirs, neu.AutoImportDirs) &&
		eqStrings(old.NuxtAliases, neu.NuxtAliases)
}

// rebindableConsumer reports whether an owner the name delta took can be
// republished from the facts it already has instead of reparsed.
//
// prepareFrozenTS parses a file by reading its own bytes and the filenames it
// names, never another file's record, so a reparse can only produce different
// facts when one of those two moved. A consumer taken purely because a global
// name appeared or vanished has neither: its bytes are unchanged - a moved hash
// would have made it dirty before any closure ran - and the names it resolves
// through an import are baked into its facts as target_file provenance, which
// is why an owner holding a resolved edge into anything that moved this run is
// refused here and reparsed as before.
//
// What is left is the global case buildIndex was written for: a reference with
// no import edge carries no provenance, so the fact is literally the same fact
// and only the index around it changes. Publishing the cached facts into the
// new name universe is the rebinding. An unresolved specifier is refused too,
// because a specifier that starts resolving is a membership question rather
// than a name one, and a forwarding record is refused by nameScopedDependent.
func rebindableConsumer(rec *tsextractor.FileRecord, moved map[string]bool) bool {
	if !nameScopedDependent(rec) {
		return false
	}
	if len(rec.UnresolvedSpecs) > 0 {
		return false
	}
	// Audited against wave10 (remote c648dd7). Two of its mechanisms bind names
	// that no import specifier names, so a record that participates in either
	// cannot be judged from its own import edges:
	//
	//   - Nuxt auto-imports. resolveNuxtAutoComposableCalls and the auto
	//     component index bind bare calls and template tags through directory
	//     convention and the known-file set, using AutoImportDirs collected
	//     across records. A vue or svelte record also binds template tags that
	//     way, so only plain ts records replay here.
	//   - Route composition. composeRouterMounts derives mounts from router
	//     records at session level rather than from this file's own imports.
	//
	// Default-reexport origin side-reads, the third wave10 change, need no rule
	// of their own: bindNamedImportFile records the origin as a side read, a
	// forwarding record is already refused by nameScopedDependent, and any
	// record whose side read moved is refused below and by sideReadDependents.
	// FileRecord.NuxtScope, wave10's other new field, is what actually decides
	// the first of those: it names the Nuxt application owning the file, "-" for
	// none, "." for the repository root app. A consumer inside an app auto-imports
	// whether or not it declares dirs of its own, so only the recorded "-" is
	// proof. An empty scope is a record written before the field existed and
	// proves nothing, so it is refused with the rest.
	if rec.NuxtScope != "-" {
		return false
	}
	if rec.ParseKind != "ts" || len(rec.AutoImportDirs) > 0 || len(rec.NuxtAliases) > 0 || rec.Router != nil {
		return false
	}
	for _, f := range rec.ResolvedFiles {
		if moved[filepath.ToSlash(f)] {
			return false
		}
	}
	for _, f := range rec.SideReads {
		if moved[filepath.ToSlash(f)] {
			return false
		}
	}
	return true
}
