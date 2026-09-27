package graphsession

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// Compare the checkpoint barrier against the former payload-copy/decode path.
// Journal construction and durable writes are outside the timed region.
func BenchmarkCheckpointEndProof(b *testing.B) {
	j, err := graphstream.OpenJournal(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = j.Close() })
	for i := 0; i < 32; i++ {
		payload, err := json.Marshal(map[string]any{"type": "batch", "run_id": "bench", "data": strings.Repeat("x", 256<<10)})
		if err != nil {
			b.Fatal(err)
		}
		id := fmt.Sprintf("bench:batch:%d", i)
		if err := j.Append(graphstream.JournalEntry{MsgID: id, Subject: "s", Payload: payload}); err != nil {
			b.Fatal(err)
		}
		if err := j.Ack(id); err != nil {
			b.Fatal(err)
		}
	}
	if err := j.Append(graphstream.JournalEntry{MsgID: "bench:end", Subject: "s", Payload: []byte(`{"type":"end_replace","run_id":"bench"}`)}); err != nil {
		b.Fatal(err)
	}
	if err := j.Ack("bench:end"); err != nil {
		b.Fatal(err)
	}
	legacy := func(j *graphstream.Journal, runID string) bool {
		for _, e := range j.Entries() {
			if !e.Acked {
				continue
			}
			var p struct {
				Type  string `json:"type"`
				RunID string `json:"run_id"`
			}
			if json.Unmarshal(e.Payload, &p) == nil && p.Type == graphstream.TypeEndReplace && p.RunID == runID {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		name  string
		check func(*graphstream.Journal, string) bool
	}{{"payload_copy_decode", legacy}, {"retained_metadata", journalHasAckedEnd}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !tc.check(j, "bench") {
					b.Fatal("acknowledged EndReplace not found")
				}
			}
		})
	}
}

// BenchmarkProductStateDecode profiles real checkpoint decoding without disk IO.
// It intentionally preserves the API's full Facts result contract.
func BenchmarkProductStateDecode(b *testing.B) {
	path := os.Getenv("ENOLA_STATE_BENCH_PATH")
	if path == "" {
		b.Skip("set ENOLA_STATE_BENCH_PATH to a Product state.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("checkpoint bytes=%d", len(raw))
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st, err := decodeStateBytes(path, raw)
		if err != nil {
			b.Fatal(err)
		}
		if len(st.Files) == 0 {
			b.Fatal("empty checkpoint")
		}
	}
}

// Diagnostic only: a compact representation is NOT a supported state format.
// A production change would require versioning and explicit recovery/migration.
func BenchmarkProductStateWithoutDuplicateSummaries(b *testing.B) {
	path := os.Getenv("ENOLA_STATE_BENCH_PATH")
	if path == "" {
		b.Skip("set ENOLA_STATE_BENCH_PATH")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	original, err := decodeStateBytes(path, raw)
	if err != nil {
		b.Fatal(err)
	}
	compact := *original
	compact.Files = make(map[string]*FileState, len(original.Files))
	restore := map[string]bool{}
	for path, f := range original.Files {
		if f == nil {
			compact.Files[path] = nil
			continue
		}
		copy := *f
		if f.TS != nil && reflect.DeepEqual(f.Declared, f.TS.Declared) && reflect.DeepEqual(f.Referenced, f.TS.Referenced) && reflect.DeepEqual(f.Imports, f.TS.ResolvedFiles) && reflect.DeepEqual(f.Reexports, f.TS.Reexports) {
			restore[path] = true
			copy.Declared = nil
			copy.Referenced = nil
			copy.Imports = nil
			copy.Reexports = nil
		}
		compact.Files[path] = &copy
	}
	reduced, err := json.Marshal(&compact)
	if err != nil {
		b.Fatal(err)
	}
	decode := func(data []byte, restoreLists bool) *State {
		st, err := decodeStateBytes(path, data)
		if err != nil {
			b.Fatal(err)
		}
		if restoreLists {
			for p := range restore {
				f := st.Files[p]
				f.Declared = f.TS.Declared
				f.Referenced = f.TS.Referenced
				f.Imports = f.TS.ResolvedFiles
				f.Reexports = f.TS.Reexports
			}
		}
		return st
	}
	if !reflect.DeepEqual(original, decode(reduced, true)) {
		b.Fatal("restored checkpoint differs")
	}
	b.Logf("original=%d compact=%d restored_owners=%d", len(raw), len(reduced), len(restore))
	for _, c := range []struct {
		name    string
		data    []byte
		restore bool
	}{{"current", raw, false}, {"prototype", reduced, true}} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = decode(c.data, c.restore)
			}
		})
	}
}
