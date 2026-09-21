//go:build linux

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
	switch uint64(st.Type) {
	case 0xef53, 0x01021994, 0x794c7630, 0x58465342, 0x9123683e, 0x2fc12fc1:
		return nil
	default:
		return fmt.Errorf("watch filesystem type %x is not audited for local event coverage: %s", st.Type, path)
	}
}
