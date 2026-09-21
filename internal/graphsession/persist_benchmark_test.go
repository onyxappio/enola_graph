package graphsession

import (
	"encoding/json"
	"fmt"
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
