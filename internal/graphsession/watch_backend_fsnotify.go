//go:build !darwin

package graphsession

import "github.com/fsnotify/fsnotify"

type fsnotifyNativeWatcher struct {
	watcher *fsnotify.Watcher
}

func newNativeWatcher() (nativeWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsnotifyNativeWatcher{watcher: w}, nil
}

func (w *fsnotifyNativeWatcher) Events() <-chan fsnotify.Event { return w.watcher.Events }
func (w *fsnotifyNativeWatcher) Errors() <-chan error          { return w.watcher.Errors }
func (w *fsnotifyNativeWatcher) Add(path string) error         { return w.watcher.Add(path) }
func (w *fsnotifyNativeWatcher) Remove(path string) error      { return w.watcher.Remove(path) }
func (w *fsnotifyNativeWatcher) WatchList() []string           { return w.watcher.WatchList() }
func (w *fsnotifyNativeWatcher) Recursive() bool               { return false }
func (w *fsnotifyNativeWatcher) Close() error                  { return w.watcher.Close() }
