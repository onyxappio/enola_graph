//go:build darwin

package graphsession

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func requireLocalWatch(path string) error {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return err
	}
	if st.Flags&unix.MNT_LOCAL == 0 {
		return fmt.Errorf("watch coverage is not supported for a non-local filesystem: %s", path)
	}
	return nil
}
