package graphsession

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
)

// vanishedDuringRun reports whether err says an input this run had already
// captured stopped existing while the run was still reading the tree.
//
// The distinction matters because ErrInputsChanged is the watch loop's retry
// signal (see Watch in changes.go): a file the editor, a branch switch or a
// build step removed mid-run is a change the next reconcile will read
// correctly, while a file that is present but cannot be read is a real fault
// that has to reach the operator instead of spinning. So only the two errno
// shapes that mean "the path is gone" qualify: ENOENT for the file itself, and
// ENOTDIR for a directory on the way to it that was replaced by a file, which
// is what a directory swap looks like from inside an open call.
//
// Permission, I/O, transport and malformed-configuration failures are
// deliberately not included: they are not evidence that the inputs changed.
func vanishedDuringRun(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.ENOTDIR {
		return true
	}
	return false
}

// classifyVanished returns err wrapped as ErrInputsChanged when it is a mid-run
// disappearance and as the given hard failure otherwise. Both messages keep the
// original error so the reason a run was refused stays readable either way.
func classifyVanished(err error, what, path, refusing string) error {
	if vanishedDuringRun(err) {
		return fmt.Errorf("%w: %s %s disappeared during the run; %s: %w", ErrInputsChanged, what, path, refusing, err)
	}
	return fmt.Errorf("%s %s unreadable; %s: %w", what, path, refusing, err)
}
