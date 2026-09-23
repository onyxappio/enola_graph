package tsextractor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
)

// The readers in this package decide on three things: bytes, presence, and what
// a directory contained. overlayReadFile already answers the first under the
// run's capture and records what it answered. These two do the same for the
// other two - they do not change what a reader sees, they record what it saw,
// so a snapshot built from those reads can later be checked against the bytes
// another caller will be handed rather than trusted because it exists.
//
// Presence is deliberately still answered by the tree. A capture carries the
// content inputs a session already knows about, not an image of the filesystem,
// so absence from a capture is not absence on disk and answering Stat from the
// overlay would invent a fact no reader established.

// overlayStat stats through the policy scope and records the presence the
// reader was told about.
func overlayStat(ctx context.Context, path string, inputScopes ...*inputscope.Scope) (os.FileInfo, error) {
	inputScope := inputscope.First(inputScopes)
	info, err := inputScope.Stat(path)
	probe := probeFrom(ctx)
	if probe == nil {
		return info, err
	}
	if !inputScope.Allowed(path, false) {
		// As in overlayReadFile: a policy refusal is not an observation of the
		// tree. The same path under the same scope is refused again, and
		// recording it would make the refusal look like a name a capture could
		// contradict.
		return info, err
	}
	switch {
	case err != nil:
		probe.recordStat(absOverlayKey(path), observedStatMissing)
	case info.IsDir():
		probe.recordStat(absOverlayKey(path), observedStatDir)
	default:
		probe.recordStat(absOverlayKey(path), observedStatFile)
	}
	return info, err
}

// overlayReadDir lists through the policy scope and records the names the
// reader was handed. A failed listing reports no directory and records nothing.
func overlayReadDir(ctx context.Context, dir string, inputScopes ...*inputscope.Scope) ([]os.DirEntry, error) {
	inputScope := inputscope.First(inputScopes)
	entries, err := inputScope.ReadDir(dir)
	if err != nil {
		return entries, err
	}
	if probe := probeFrom(ctx); probe != nil {
		names := make(map[string]string, len(entries))
		for _, e := range entries {
			names[e.Name()] = entryKind(e)
		}
		probe.recordDir(absOverlayKey(dir), names)
	}
	return entries, err
}

// overlayWalkDir runs a discovery walk and records the name set of every
// directory the walk actually enumerated.
//
// Only completed enumerations are kept. A directory the callback skipped, or
// one whose listing failed, never reported its contents to anyone, and
// recording a partial name set for it would let the snapshot answer for names
// no reader looked at. A walk that stops early - SkipAll, or an error the
// callback returns - abandons its whole enumeration record for the same reason:
// the directories still open when it stopped are indistinguishable here from
// the ones it finished.
func overlayWalkDir(ctx context.Context, root string, inputScope *inputscope.Scope, fn fs.WalkDirFunc) error {
	probe := probeFrom(ctx)
	if probe == nil {
		return inputScope.WalkDir(root, fn)
	}
	names := map[string]map[string]string{}
	enumerated := map[string]struct{}{}
	aborted := false
	err := inputScope.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		parent := ""
		if d != nil && path != root {
			parent = absOverlayKey(filepath.Dir(path))
			set, ok := names[parent]
			if !ok {
				set = map[string]string{}
				names[parent] = set
			}
			set[d.Name()] = entryKind(d)
		}
		ret := fn(path, d, walkErr)
		switch {
		case ret == nil:
			if d != nil && d.IsDir() && walkErr == nil {
				enumerated[absOverlayKey(path)] = struct{}{}
			}
		case errors.Is(ret, filepath.SkipDir):
			// Returned for a directory it means "do not descend"; returned for
			// a file it means "stop listing the directory this file is in".
			// Either way some directory's contents are now only partly seen.
			if d != nil && d.IsDir() {
				delete(enumerated, absOverlayKey(path))
			} else if parent != "" {
				delete(enumerated, parent)
			}
		default:
			aborted = true
		}
		return ret
	})
	if err != nil {
		aborted = true
	}
	if aborted {
		return err
	}
	for dir := range enumerated {
		set, ok := names[dir]
		if !ok {
			// Enumerated and empty: the walk descended and the directory
			// reported nothing. That is a real observation - a package created
			// there afterwards was not present - so it is recorded as such.
			set = map[string]string{}
		}
		probe.recordDir(dir, set)
	}
	return err
}
