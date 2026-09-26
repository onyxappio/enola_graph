package graphsession

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

func TestStateEncodePreservesMarshalBytesAndFingerprint(t *testing.T) {
	st := benchState(4000)
	st.RepoID = "quote\" newline\n <script> & \u2028"
	var out bytes.Buffer
	fp, err := encodeStateJSON(&out, st)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatal("checkpoint JSON bytes changed")
	}
	if fp != fingerprintStateBytes(want) {
		t.Fatal("fingerprint differs from bytes written")
	}
}

func TestStateEncodeRefusesSerializationAndWriteFailures(t *testing.T) {
	st := benchState(1)
	st.Files["app/src/mod0000/component0000.ts"].Facts[1].Props["invalid"] = math.NaN()
	var out bytes.Buffer
	if fp, err := encodeStateJSON(&out, st); err == nil || fp.known() {
		t.Fatal("invalid JSON accepted")
	}
	if out.Len() != 0 {
		t.Fatal("serialization failure leaked bytes")
	}
	st = benchState(1)
	for _, w := range []io.Writer{stateFailWriter{}, stateShortWriter{}} {
		if fp, err := encodeStateJSON(w, st); err == nil || fp.known() {
			t.Fatal("write failure accepted")
		}
	}
}

type stateFailWriter struct{}

func (stateFailWriter) Write(p []byte) (int, error) { return 0, errors.New("injected write failure") }

type stateShortWriter struct{}

func (stateShortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestStateJSONNewlineRemovalAcrossChunkBoundaries(t *testing.T) {
	input := "{\"value\":\"escaped\\nline\"}\n"
	for size := 1; size <= len(input); size++ {
		var out bytes.Buffer
		w := jsonWithoutFinalNewline{out: &out}
		for start := 0; start < len(input); start += size {
			end := min(start+size, len(input))
			if n, err := w.Write([]byte(input[start:end])); err != nil || n != end-start {
				t.Fatalf("chunk %d: %d %v", size, n, err)
			}
		}
		if !w.hasTail || w.tail != '\n' || out.String() != strings.TrimSuffix(input, "\n") || w.size != int64(len(input)-1) {
			t.Fatalf("chunk %d corrupted JSON", size)
		}
	}
}

func BenchmarkStateEncodingCopy(b *testing.B) {
	st := benchState(4000)
	b.Run("marshal", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			raw, err := json.Marshal(st)
			if err != nil {
				b.Fatal(err)
			}
			_ = fingerprintStateBytes(raw)
		}
	})
	b.Run("encoder", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := encodeStateJSON(io.Discard, st); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestStateEncodeFailurePreservesPendingCheckpoint(t *testing.T) {
	dir := t.TempDir()
	st := benchState(1)
	if _, err := writePendingStateFP(dir, st); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(pendingStatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	st.Files["app/src/mod0000/component0000.ts"].Facts[1].Props["invalid"] = math.NaN()
	if fp, err := writePendingStateFP(dir, st); err == nil || fp.known() {
		t.Fatal("invalid checkpoint accepted")
	}
	after, err := os.ReadFile(pendingStatePath(dir))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed encoding replaced prior pending checkpoint", err)
	}
	if _, err := os.Stat(pendingStatePath(dir) + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("failed encoding left temporary state", err)
	}
}
