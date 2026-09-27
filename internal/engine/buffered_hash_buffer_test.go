package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

type scratchObservingReader struct {
	t       *testing.T
	source  *bytes.Reader
	scratch []byte
	reads   int
}

func (r *scratchObservingReader) Read(p []byte) (int, error) {
	r.reads++
	if len(p) != len(r.scratch) || &p[0] != &r.scratch[0] {
		r.t.Fatal("readHash did not reuse the supplied scratch buffer")
	}
	return r.source.Read(p)
}

func (r *scratchObservingReader) WriteTo(io.Writer) (int64, error) {
	r.t.Fatal("readHash bypassed its bounded buffer through WriterTo")
	return 0, nil
}

func TestBufferedHashUsesSuppliedScratch(t *testing.T) {
	payload := bytes.Repeat([]byte("bounded-buffer"), 1000)
	scratch := make([]byte, 64)
	reader := &scratchObservingReader{t: t, source: bytes.NewReader(payload), scratch: scratch}
	got, n, err := readHash(reader, scratch, sha256.New())
	want := sha256.Sum256(payload)
	if err != nil || n != int64(len(payload)) || got != hex.EncodeToString(want[:]) || reader.reads < 2 {
		t.Fatalf("unexpected hash result: hash=%s bytes=%d error=%v reads=%d", got, n, err, reader.reads)
	}
}
