//go:build unix

package analyzerplugin

import (
	"golang.org/x/sys/unix"
)

func runtimeFileIdentity(path string) (RuntimeCacheEntry, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return RuntimeCacheEntry{}, err
	}
	return RuntimeCacheEntry{
		Path:    path,
		Dev:     uint64(st.Dev),
		Inode:   uint64(st.Ino),
		Size:    st.Size,
		MtimeNs: unix.TimespecToNsec(st.Mtim),
		CtimeNs: unix.TimespecToNsec(st.Ctim),
	}, nil
}
