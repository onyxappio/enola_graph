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
	known := map[string]bool{}
	for _, f := range owned {
		known[filepath.ToSlash(f)] = true
	}

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
		for path, rec := range prev {
			if rec == nil {
				continue
			}
			if len(rec.UnresolvedSpecs) > 0 {
				dirty[path] = true
				continue
			}
			for _, spec := range rec.ImportSpecs {
				if _, ok := tsextractor.NormalizeImportTarget(spec, known); !ok {
					if internalSpec(spec) {
						dirty[path] = true
						break
					}
				}
			}
		}
	}

	return dirty, false, ""
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

func internalSpec(spec string) bool {
	if spec == "" {
		return false
	}
	if spec[0] == '.' {
		return true
	}
	if spec[0] == '@' {
		return false
	}
	for i := 0; i < len(spec); i++ {
		if spec[i] == '/' {
			return true
		}
	}
	return false
}
