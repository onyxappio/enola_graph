package graphsession

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
)

// encodeStateJSON keeps the existing json.Marshal byte representation without
// Marshal's second full-size copy of the encoder buffer. Encoder still buffers
// the JSON value; this is not a bounded-memory JSON serializer. Hashing happens
// over exactly the bytes written, before the caller syncs and renames the file.
func encodeStateJSON(out io.Writer, st *State) (stateFingerprint, error) {
	h := sha256.New()
	w := jsonWithoutFinalNewline{out: io.MultiWriter(out, h)}
	if err := json.NewEncoder(&w).Encode(st); err != nil {
		return stateFingerprint{}, err
	}
	if !w.hasTail || w.tail != '\n' {
		return stateFingerprint{}, fmt.Errorf("state JSON encoder missing final newline")
	}
	fp := stateFingerprint{size: w.size}
	copy(fp.digest[:], h.Sum(nil))
	return fp, nil
}

// Hold one byte across writes, so removing Encoder's final newline does not
// depend on its current single-Write implementation or trim embedded newlines.
type jsonWithoutFinalNewline struct {
	out     io.Writer
	tail    byte
	hasTail bool
	size    int64
}

func (w *jsonWithoutFinalNewline) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.hasTail {
		if err := w.write([]byte{w.tail}); err != nil {
			return 0, err
		}
	}
	if err := w.write(p[:len(p)-1]); err != nil {
		return 0, err
	}
	w.tail, w.hasTail = p[len(p)-1], true
	return len(p), nil
}

func (w *jsonWithoutFinalNewline) write(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	n, err := w.out.Write(p)
	w.size += int64(n)
	if err != nil {
		return err
	}
	if n != len(p) {
		return io.ErrShortWrite
	}
	return nil
}
