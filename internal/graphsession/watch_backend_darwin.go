//go:build darwin

package graphsession

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsevents"
	"github.com/fsnotify/fsnotify"
)

// fseventsNativeWatcher uses one recursive FSEvents stream for all registered
// roots. Add and Remove restart the stream with the new root set; callers mark
// that registration boundary as requiring reconciliation.
type fseventsNativeWatcher struct {
	mu     sync.Mutex
	pathMu sync.RWMutex
	stream *fsevents.EventStream
	stop   chan struct{}
	roots  []fseventsRoot
	closed bool
	events chan fsnotify.Event
	errors chan error
	wg     sync.WaitGroup
}

type fseventsRoot struct {
	logical  string
	physical string
}

func newNativeWatcher() (nativeWatcher, error) {
	return &fseventsNativeWatcher{
		events: make(chan fsnotify.Event),
		errors: make(chan error, 1),
	}, nil
}

func (w *fseventsNativeWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *fseventsNativeWatcher) Errors() <-chan error          { return w.errors }
func (w *fseventsNativeWatcher) Recursive() bool               { return true }

func (w *fseventsNativeWatcher) Add(path string) error {
	path = filepath.Clean(path)
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve FSEvents root %s: %w", path, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("fsevents watcher is closed")
	}
	for _, root := range w.roots {
		if root.logical == path {
			return nil
		}
	}
	previous := append([]fseventsRoot(nil), w.roots...)
	w.pathMu.Lock()
	w.roots = append(w.roots, fseventsRoot{logical: path, physical: physical})
	w.pathMu.Unlock()
	if err := w.restartLocked(); err != nil {
		w.pathMu.Lock()
		w.roots = previous
		w.pathMu.Unlock()
		_ = w.restartLocked()
		return err
	}
	return nil
}

func (w *fseventsNativeWatcher) Remove(path string) error {
	path = filepath.Clean(path)
	w.mu.Lock()
	defer w.mu.Unlock()
	index := -1
	for i, root := range w.roots {
		if root.logical == path {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	previous := append([]fseventsRoot(nil), w.roots...)
	w.pathMu.Lock()
	w.roots = append(w.roots[:index], w.roots[index+1:]...)
	w.pathMu.Unlock()
	if err := w.restartLocked(); err != nil {
		w.pathMu.Lock()
		w.roots = previous
		w.pathMu.Unlock()
		_ = w.restartLocked()
		return err
	}
	return nil
}

func (w *fseventsNativeWatcher) WatchList() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pathMu.RLock()
	defer w.pathMu.RUnlock()
	paths := make([]string, 0, len(w.roots))
	for _, root := range w.roots {
		paths = append(paths, root.logical)
	}
	return paths
}

func (w *fseventsNativeWatcher) restartLocked() error {
	if w.stream != nil {
		w.stream.Stop()
		close(w.stop)
		w.stream = nil
		w.stop = nil
	}
	if len(w.roots) == 0 {
		return nil
	}
	es := &fsevents.EventStream{
		Paths:   w.physicalRootsLocked(),
		Flags:   fsevents.FileEvents | fsevents.WatchRoot | fsevents.NoDefer,
		Latency: 100 * time.Millisecond,
		Events:  make(chan []fsevents.Event, 8),
	}
	if len(w.roots) == 1 {
		device, err := fsevents.DeviceForPath(w.roots[0].physical)
		if err != nil {
			return fmt.Errorf("resolve FSEvents device: %w", err)
		}
		es.Device = device
	}
	if err := es.Start(); err != nil {
		return fmt.Errorf("start FSEvents stream: %w", err)
	}
	stop := make(chan struct{})
	w.stream = es
	w.stop = stop
	w.wg.Add(1)
	go w.forward(es, stop)
	return nil
}

func (w *fseventsNativeWatcher) physicalRootsLocked() []string {
	w.pathMu.RLock()
	defer w.pathMu.RUnlock()
	paths := make([]string, 0, len(w.roots))
	for _, root := range w.roots {
		paths = append(paths, root.physical)
	}
	return paths
}

func (w *fseventsNativeWatcher) forward(stream *fsevents.EventStream, stop <-chan struct{}) {
	defer w.wg.Done()
	for {
		select {
		case <-stop:
			return
		case batch, ok := <-stream.Events:
			if !ok {
				return
			}
			for _, event := range batch {
				if event.Flags&(fsevents.MustScanSubDirs|fsevents.KernelDropped|fsevents.UserDropped|fsevents.EventIDsWrapped|fsevents.RootChanged|fsevents.Mount|fsevents.Unmount) != 0 {
					w.reportError(fmt.Errorf("FSEvents requires reconciliation for %s (flags=%d)", event.Path, event.Flags))
				}
				if event.Flags&fsevents.HistoryDone != 0 {
					continue
				}
				op := fseventOp(event.Flags)
				if op == 0 {
					continue
				}
				select {
				case w.events <- fsnotify.Event{Name: w.logicalEventPath(event.Path), Op: op}:
				case <-stop:
					return
				}
			}
		}
	}
}

func (w *fseventsNativeWatcher) logicalEventPath(path string) string {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		path = string(os.PathSeparator) + path
	}
	w.pathMu.RLock()
	defer w.pathMu.RUnlock()
	for _, root := range w.roots {
		if path == root.physical {
			return root.logical
		}
		if strings.HasPrefix(path, root.physical+string(filepath.Separator)) {
			return root.logical + strings.TrimPrefix(path, root.physical)
		}
	}
	return path
}

func (w *fseventsNativeWatcher) reportError(err error) {
	select {
	case w.errors <- err:
	default:
	}
}

func fseventOp(flags fsevents.EventFlags) fsnotify.Op {
	var op fsnotify.Op
	if flags&fsevents.ItemCreated != 0 {
		op |= fsnotify.Create
	}
	if flags&fsevents.ItemRemoved != 0 {
		op |= fsnotify.Remove
	}
	if flags&fsevents.ItemRenamed != 0 {
		op |= fsnotify.Rename
	}
	if flags&fsevents.ItemModified != 0 {
		op |= fsnotify.Write
	}
	if flags&(fsevents.ItemInodeMetaMod|fsevents.ItemFinderInfoMod|fsevents.ItemChangeOwner|fsevents.ItemXattrMod) != 0 {
		op |= fsnotify.Chmod
	}
	return op
}

func (w *fseventsNativeWatcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.stream != nil {
		w.stream.Stop()
		close(w.stop)
		w.stream = nil
		w.stop = nil
	}
	w.mu.Unlock()
	w.wg.Wait()
	close(w.events)
	close(w.errors)
	return nil
}
