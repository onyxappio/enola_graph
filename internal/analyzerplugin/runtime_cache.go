package analyzerplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// RuntimeLinkedLib stores one dynamically linked library behind the same cheap
// metadata identity used for the executable.
type RuntimeLinkedLib struct {
	Path    string `json:"path"`
	Dev     uint64 `json:"dev"`
	Inode   uint64 `json:"inode"`
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtime_ns"`
	CtimeNs int64  `json:"ctime_ns"`
	Digest  string `json:"digest"`
}

// RuntimeCacheEntry stores the executable fingerprint behind a cheap metadata
// identity. The common case is one stat and zero re-hash. For system Node the
// digest also covers readable linked dynamic libraries (design §8.2).
type RuntimeCacheEntry struct {
	Path    string             `json:"path"`
	Dev     uint64             `json:"dev"`
	Inode   uint64             `json:"inode"`
	Size    int64              `json:"size"`
	MtimeNs int64              `json:"mtime_ns"`
	CtimeNs int64              `json:"ctime_ns"`
	Digest  string             `json:"digest"`
	Linked  []RuntimeLinkedLib `json:"linked,omitempty"`
}

// RuntimeCache is keyed by the resolved absolute executable path.
type RuntimeCache map[string]RuntimeCacheEntry

// CachedRuntimeDigest returns the runtime fingerprint for path, reusing cache
// when the resolved executable and every previously hashed linked library keep
// the same device/inode/size/mtime/ctime. System Node fingerprints include
// digests of readable linked dynamic libraries; scripts and other non-object
// files contribute only their own bytes.
func CachedRuntimeDigest(path string, cache RuntimeCache) (string, RuntimeCacheEntry, error) {
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", RuntimeCacheEntry{}, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", RuntimeCacheEntry{}, err
	}
	meta, err := runtimeFileIdentity(resolved)
	if err != nil {
		return "", RuntimeCacheEntry{}, err
	}
	if cache != nil {
		if prev, ok := cache[resolved]; ok && prev.matches(meta) && prev.Digest != "" && prev.linkedStillValid() {
			return prev.Digest, prev, nil
		}
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return "", RuntimeCacheEntry{}, err
	}
	exeSum := sha256.Sum256(b)
	exeDigest := hex.EncodeToString(exeSum[:])
	linked, unresolved, err := fingerprintLinkedLibraries(resolved)
	if err != nil {
		return "", RuntimeCacheEntry{}, err
	}
	meta.Digest = combineRuntimeDigest(exeDigest, linked, unresolved)
	meta.Linked = linked
	if cache != nil {
		cache[resolved] = meta
	}
	return meta.Digest, meta, nil
}

func combineRuntimeDigest(exeDigest string, linked []RuntimeLinkedLib, unresolved []string) string {
	if len(linked) == 0 && len(unresolved) == 0 {
		return exeDigest
	}
	h := sha256.New()
	_, _ = h.Write([]byte(exeDigest))
	for _, lib := range linked {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(lib.Path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(lib.Digest))
	}
	// unresolved is retained for callers/tests that still pass path-only tokens;
	// production fingerprinting now folds shared-cache libs into linked digests.
	unresolved = append([]string(nil), unresolved...)
	sort.Strings(unresolved)
	for _, path := range unresolved {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte("unresolved:"))
		_, _ = h.Write([]byte(path))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func fingerprintLinkedLibraries(exe string) ([]RuntimeLinkedLib, []string, error) {
	paths, err := linkedLibraryPaths(exe)
	if err != nil {
		return nil, nil, err
	}
	return fingerprintLinkedLibrariesFromPaths(paths)
}

func fingerprintLinkedLibrariesFromPaths(paths []string) ([]RuntimeLinkedLib, []string, error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	sort.Strings(paths)
	out := make([]RuntimeLinkedLib, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil {
			// macOS dyld shared-cache libraries often have no on-disk inode.
			// Bind identity to the dyld-reported UUID so a same-path cache
			// update still invalidates runtime fingerprints.
			digest, digErr := sharedCacheLibraryDigest(path)
			if digErr != nil {
				return nil, nil, fmt.Errorf("shared-cache linked library %s: %w", path, digErr)
			}
			out = append(out, RuntimeLinkedLib{Path: path, Digest: digest})
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("linked library %s is not a regular file", path)
		}
		meta, err := runtimeFileIdentity(path)
		if err != nil {
			return nil, nil, fmt.Errorf("stat linked library %s: %w", path, err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read linked library %s: %w", path, err)
		}
		sum := sha256.Sum256(b)
		meta.Digest = hex.EncodeToString(sum[:])
		out = append(out, RuntimeLinkedLib{
			Path:    meta.Path,
			Dev:     meta.Dev,
			Inode:   meta.Inode,
			Size:    meta.Size,
			MtimeNs: meta.MtimeNs,
			CtimeNs: meta.CtimeNs,
			Digest:  meta.Digest,
		})
	}
	return out, nil, nil
}

const sharedCacheDigestPrefix = "dyld-uuid:"

// sharedCacheDigestFn is the live shared-cache identity probe. Tests may
// substitute it for deterministic same-path UUID invalidation coverage.
var sharedCacheDigestFn = defaultSharedCacheLibraryDigest

func sharedCacheLibraryDigest(path string) (string, error) {
	return sharedCacheDigestFn(path)
}

func defaultSharedCacheLibraryDigest(path string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("dyld_info", path).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("dyld_info: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		uuid := parseDyldInfoUUID(string(out))
		if uuid == "" {
			return "", fmt.Errorf("dyld_info produced no UUID for %s", path)
		}
		return sharedCacheDigestPrefix + uuid, nil
	default:
		return "", fmt.Errorf("linked library %s is missing on disk", path)
	}
}

func parseDyldInfoUUID(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "-uuid:") {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			candidate := strings.TrimSpace(lines[j])
			if candidate == "" || strings.HasPrefix(candidate, "-") {
				break
			}
			fields := strings.Fields(candidate)
			if len(fields) == 1 && strings.Count(fields[0], "-") >= 4 {
				return strings.ToUpper(fields[0])
			}
		}
	}
	return ""
}

func linkedLibraryPaths(exe string) ([]string, error) {
	switch runtime.GOOS {
	case "darwin":
		return linkedLibraryPathsDarwin(exe)
	case "linux":
		return linkedLibraryPathsLinux(exe)
	default:
		// Windows and other targets keep the executable-only fingerprint.
		return nil, nil
	}
}

func linkedLibraryPathsDarwin(exe string) ([]string, error) {
	out, err := exec.Command("otool", "-L", exe).CombinedOutput()
	text := string(out)
	if err != nil {
		return nil, fmt.Errorf("otool -L %s: %w (%s)", exe, err, strings.TrimSpace(text))
	}
	if strings.Contains(text, "is not an object file") {
		return nil, nil
	}
	rpaths, err := machORPaths(exe)
	if err != nil {
		return nil, err
	}
	var paths []string
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i == 0 && strings.HasSuffix(line, ":") {
			continue
		}
		load := line
		if idx := strings.Index(line, " (compatibility"); idx >= 0 {
			load = strings.TrimSpace(line[:idx])
		}
		resolved := resolveMachOLoadPath(exe, load, rpaths)
		if resolved == "" || resolved == exe {
			continue
		}
		paths = append(paths, resolved)
	}
	return paths, nil
}

func machORPaths(exe string) ([]string, error) {
	out, err := exec.Command("otool", "-l", exe).CombinedOutput()
	if err != nil {
		text := string(out)
		if strings.Contains(text, "is not an object file") {
			return nil, nil
		}
		return nil, fmt.Errorf("otool -l %s: %w (%s)", exe, err, strings.TrimSpace(text))
	}
	var paths []string
	lines := strings.Split(string(out), "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.Contains(lines[i], "LC_RPATH") {
			continue
		}
		for j := i + 1; j < len(lines) && j < i+6; j++ {
			line := strings.TrimSpace(lines[j])
			if strings.HasPrefix(line, "path ") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					paths = append(paths, fields[1])
				}
				break
			}
		}
	}
	return paths, nil
}

func resolveMachOLoadPath(exe, load string, rpaths []string) string {
	loaderDir := filepath.Dir(exe)
	expand := func(p string) string {
		p = strings.ReplaceAll(p, "@loader_path", loaderDir)
		p = strings.ReplaceAll(p, "@executable_path", loaderDir)
		return filepath.Clean(p)
	}
	switch {
	case strings.HasPrefix(load, "@rpath/"):
		rest := strings.TrimPrefix(load, "@rpath/")
		for _, rp := range rpaths {
			candidate := filepath.Join(expand(rp), rest)
			if fileExists(candidate) {
				return candidate
			}
		}
		fallback := filepath.Join(loaderDir, rest)
		if fileExists(fallback) {
			return fallback
		}
		return ""
	case strings.HasPrefix(load, "@loader_path/"), strings.HasPrefix(load, "@executable_path/"):
		return expand(load)
	case strings.HasPrefix(load, "@"):
		return ""
	default:
		return load
	}
}

func linkedLibraryPathsLinux(exe string) ([]string, error) {
	out, err := exec.Command("ldd", exe).CombinedOutput()
	text := string(out)
	if err != nil {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "not a dynamic executable") || strings.Contains(lower, "not an elf") {
			return nil, nil
		}
		return nil, fmt.Errorf("ldd %s: %w (%s)", exe, err, strings.TrimSpace(text))
	}
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "linux-vdso.so") {
			continue
		}
		if strings.Contains(line, "not found") {
			return nil, fmt.Errorf("ldd %s: unresolved linked library in %q", exe, line)
		}
		if idx := strings.Index(line, " => "); idx >= 0 {
			rest := strings.TrimSpace(line[idx+4:])
			if rest == "" {
				continue
			}
			path := strings.Fields(rest)[0]
			if path == "" || path == "not" {
				continue
			}
			paths = append(paths, path)
			continue
		}
		// Forms like "/lib64/ld-linux-x86-64.so.2 (0x...)"
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasPrefix(fields[0], "/") {
			paths = append(paths, fields[0])
		}
	}
	return paths, nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (e RuntimeCacheEntry) matches(other RuntimeCacheEntry) bool {
	return e.Path == other.Path &&
		e.Dev == other.Dev &&
		e.Inode == other.Inode &&
		e.Size == other.Size &&
		e.MtimeNs == other.MtimeNs &&
		e.CtimeNs == other.CtimeNs
}

func (e RuntimeCacheEntry) linkedStillValid() bool {
	for _, lib := range e.Linked {
		if strings.HasPrefix(lib.Digest, sharedCacheDigestPrefix) || (lib.Dev == 0 && lib.Inode == 0 && lib.Size == 0 && lib.Digest != "") {
			cur, err := sharedCacheLibraryDigest(lib.Path)
			if err != nil || cur != lib.Digest {
				return false
			}
			continue
		}
		cur, err := runtimeFileIdentity(lib.Path)
		if err != nil {
			return false
		}
		if lib.Path != cur.Path ||
			lib.Dev != cur.Dev ||
			lib.Inode != cur.Inode ||
			lib.Size != cur.Size ||
			lib.MtimeNs != cur.MtimeNs ||
			lib.CtimeNs != cur.CtimeNs {
			return false
		}
	}
	return true
}
