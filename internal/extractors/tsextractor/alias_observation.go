package tsextractor

import (
	"context"
	"io/fs"
	"sort"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
)

// aliasDirectoryEntries can reuse the discovery probe's completed listing.
// The alias walker only consults names and IsDir. Its inventory callback may
// need real DirEntries, so that path always keeps the filesystem reader.
// Reuse is limited to the policy-bound discovery build; its existing retained-discovery
// reobservation fence still validates these names before cross-run reuse.
// The probe and cache are created together under one immutable policy in
// newDiscovery; they must not be shared across policies. Within that build,
// aliases use the first completed enumeration, including when the live directory
// later changes or becomes unreadable. This reduces independent observations;
// it does not itself prove that filesystem inputs stayed stable during a run.
// No additional retained listing or relationship index is created.
func aliasDirectoryEntries(ctx context.Context, dir string, scope *inputscope.Scope, namesOnly bool) ([]fs.DirEntry, error) {
	if ctx != nil && namesOnly && scope != nil && scope.Policy != nil && scope.Allowed(dir, true) && discoveryWalkCacheFrom(ctx) != nil {
		if probe := probeFrom(ctx); probe != nil {
			probe.mu.Lock()
			// Map membership means complete: recordDir never stores partial listings.
			names, complete := probe.dirs[absOverlayKey(dir)]
			var entries []fs.DirEntry
			if complete {
				entries = make([]fs.DirEntry, 0, len(names))
				for name, kind := range names {
					entries = append(entries, aliasObservedEntry{name: name, kind: kind})
				}
			}
			probe.mu.Unlock()
			if complete {
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
				return entries, nil
			}
		}
	}
	return overlayReadDir(ctx, dir, scope)
}

// This is deliberately not a general filesystem entry: the observation carries
// neither mode bits nor file metadata. Only the alias walker consumes it.
type aliasObservedEntry struct{ name, kind string }

func (e aliasObservedEntry) Name() string { return e.name }
func (e aliasObservedEntry) IsDir() bool  { return e.kind == observedEntryDir }
func (e aliasObservedEntry) Type() fs.FileMode {
	switch e.kind {
	case observedEntryDir:
		return fs.ModeDir
	case observedEntryLink:
		return fs.ModeSymlink
	default:
		return 0
	}
}
func (e aliasObservedEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrInvalid }
