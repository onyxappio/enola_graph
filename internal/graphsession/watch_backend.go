package graphsession

import (
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// nativeWatcher is the small event source contract shared by the platform
// backends. Events are normalized to fsnotify operations so the graph policy
// and resident change handling stay platform-independent.
type nativeWatcher interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Add(string) error
	Remove(string) error
	WatchList() []string
	Recursive() bool
	Close() error
}

// watcherCovers reports whether a directory is already covered by a native
// watch. Recursive backends cover descendants; directory-based backends only
// cover the exact registered directory.
func watcherCovers(w nativeWatcher, path string) bool {
	path = filepath.Clean(path)
	for _, watched := range w.WatchList() {
		watched = filepath.Clean(watched)
		if path == watched || (w.Recursive() && strings.HasPrefix(path, watched+string(filepath.Separator))) {
			return true
		}
	}
	return false
}
