//go:build windows

package analyzerplugin

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileBasicInfo mirrors Windows FILE_BASIC_INFO for GetFileInformationByHandleEx.
// ChangedTime is the metadata change time; it advances on content replacement even
// when LastWriteTime is restored, so it is the cache-invalidation key design §8.2
// requires alongside size/mtime.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangedTime    int64
	FileAttributes uint32
	_              uint32
}

func runtimeFileIdentity(path string) (RuntimeCacheEntry, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return RuntimeCacheEntry{}, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return RuntimeCacheEntry{}, fmt.Errorf("open %s for runtime identity: %w", path, err)
	}
	defer windows.CloseHandle(handle)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return RuntimeCacheEntry{}, fmt.Errorf("GetFileInformationByHandle %s: %w", path, err)
	}
	var basic fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&basic)),
		uint32(unsafe.Sizeof(basic)),
	); err != nil {
		// Prefer correctness: without ChangeTime the cache cannot prove the
		// executable bytes are unchanged after a same-size/mtime rewrite.
		return RuntimeCacheEntry{}, fmt.Errorf("GetFileInformationByHandleEx(FileBasicInfo) %s: %w", path, err)
	}
	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	inode := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	return RuntimeCacheEntry{
		Path:    path,
		Dev:     uint64(info.VolumeSerialNumber),
		Inode:   inode,
		Size:    size,
		MtimeNs: windowsFiletimeNsec(basic.LastWriteTime),
		CtimeNs: windowsFiletimeNsec(basic.ChangedTime),
	}, nil
}

func windowsFiletimeNsec(v int64) int64 {
	ft := windows.Filetime{
		LowDateTime:  uint32(uint64(v)),
		HighDateTime: uint32(uint64(v) >> 32),
	}
	return ft.Nanoseconds()
}

func windowsNsecFiletime(nsec int64) int64 {
	ft := windows.NsecToFiletime(nsec)
	return int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime)
}
