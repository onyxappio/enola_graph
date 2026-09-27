package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBufferedHashesMatchReadFile(t *testing.T) {
	root := t.TempDir()
	data := map[string][]byte{"empty": {}, "binary": {0, 255, 13, 10, 0}, "large": bytes.Repeat([]byte("large\x00payload"), 20000)}
	for name, b := range data {
		if e := os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Mkdir(filepath.Join(root, "dir"), 0700); e != nil {
		t.Fatal(e)
	}
	names := []string{"empty", "binary", "large", "missing", "dir", "large"}
	if e := os.Symlink("large", filepath.Join(root, "link")); e == nil {
		names = append(names, "link")
	}
	if e := os.Symlink("missing", filepath.Join(root, "broken")); e == nil {
		names = append(names, "broken")
	}
	want := map[string]string{}
	var nbytes int64
	for _, p := range names {
		b, e := os.ReadFile(filepath.Join(root, p))
		if e != nil {
			continue
		}
		sum := sha256.Sum256(b)
		want[p] = hex.EncodeToString(sum[:])
		nbytes += int64(len(b))
	}
	got, n := boundedFileHashes(root, names)
	if !reflect.DeepEqual(got, want) || n != nbytes {
		t.Fatalf("hashes or byte counts differ: got%v/%d want%v/%d", got, n, want, nbytes)
	}
	empty, n := boundedFileHashes(root, nil)
	if len(empty) != 0 || n != 0 {
		t.Fatal("nonempty empty-input result")
	}
}
func TestBufferedHashesSameSizeRestoredMtime(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "source.ts")
	if e := os.WriteFile(p, []byte("export const n = 1"), 0600); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := boundedFileHashes(root, []string{"source.ts"})
	if e = os.WriteFile(p, []byte("export const n = 2"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(p, time.Now(), st.ModTime()); e != nil {
		t.Fatal(e)
	}
	after, _ := boundedFileHashes(root, []string{"source.ts"})
	if before["source.ts"] == after["source.ts"] {
		t.Fatal("same-size restored-mtime edit missed")
	}
}

type failingHashReader struct{ err error }

func (r failingHashReader) Read(b []byte) (int, error) {
	copy(b, "partial")
	return len("partial"), r.err
}
func TestBufferedHashRejectsPartialFailure(t *testing.T) {
	sentinel := errors.New("read failure")
	h := sha256.New()
	sum, n, e := readHash(failingHashReader{sentinel}, make([]byte, 64), h)
	if !errors.Is(e, sentinel) || sum != "" || n != 0 {
		t.Fatal("partial failure became a successful hash")
	}
	sum, n, e = readHash(failingHashReader{io.EOF}, make([]byte, 64), h)
	want := sha256.Sum256([]byte("partial"))
	if e != nil || n != 7 || sum != hex.EncodeToString(want[:]) {
		t.Fatal("data returned with EOF lost")
	}
	sum, n, e = readHash(bytes.NewReader(nil), make([]byte, 64), h)
	empty := sha256.Sum256(nil)
	if e != nil || n != 0 || sum != hex.EncodeToString(empty[:]) {
		t.Fatal("digest not reset between files")
	}
}
