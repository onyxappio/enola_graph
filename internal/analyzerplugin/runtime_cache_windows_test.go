//go:build windows

package analyzerplugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsSameSizePreservedMtimeInvalidatesRuntimeDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.exe")
	original := []byte("runtime-payload-aaaaaaaa")
	replacement := []byte("runtime-payload-bbbbbbbb")
	if len(original) != len(replacement) {
		t.Fatal("test payloads must be equal length")
	}
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := RuntimeCache{}
	first, entry, err := CachedRuntimeDigest(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if entry.CtimeNs == 0 {
		t.Fatalf("windows identity missing change-time key: %#v", entry)
	}
	preservedMtime := time.Unix(0, entry.MtimeNs)
	if err := os.WriteFile(path, replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := setWindowsFileTimes(path, preservedMtime, preservedMtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(original)) {
		t.Fatalf("size changed: %d", info.Size())
	}
	if !info.ModTime().Equal(preservedMtime) {
		t.Fatalf("mtime was not preserved: got %v want %v", info.ModTime(), preservedMtime)
	}
	second, entry2, err := CachedRuntimeDigest(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("same-size rewrite with preserved mtime reused stale digest; identity=%#v -> %#v", entry, entry2)
	}
	if entry2.CtimeNs == entry.CtimeNs {
		t.Fatalf("change time did not advance after rewrite: %#v", entry2)
	}
}

func setWindowsFileTimes(path string, atime, mtime time.Time) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_WRITE_ATTRIBUTES|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var basic fileBasicInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&basic)),
		uint32(unsafe.Sizeof(basic)),
	); err != nil {
		return err
	}
	basic.LastAccessTime = windowsNsecFiletime(atime.UnixNano())
	basic.LastWriteTime = windowsNsecFiletime(mtime.UnixNano())
	return windows.SetFileInformationByHandle(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic)))
}
