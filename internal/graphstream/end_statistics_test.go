package graphstream

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEndStatisticsAreOptionalAndIgnoredByLegacyDecode(t *testing.T) {
	old := EndReplace{Type: TypeEndReplace, RunID: "r", Completeness: Completeness{Status: "success"}}
	raw, err := Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("statistics")) {
		t.Fatal("absent statistics changed legacy envelope")
	}
	var parsed EndReplace
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Statistics != nil {
		t.Fatal("missing statistics not unknown", err)
	}
	old.Statistics = &RunStatistics{TransactionDurationNS: 1234567890}
	raw, err = Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Type         string       `json:"type"`
		RunID        string       `json:"run_id"`
		Completeness Completeness `json:"completeness"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil || legacy.RunID != "r" || legacy.Completeness.Status != "success" {
		t.Fatal("additive statistics broke legacy", err)
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Statistics == nil || parsed.Statistics.TransactionDurationNS != 1234567890 {
		t.Fatal("statistics roundtrip", err)
	}
	again, err := Marshal(parsed)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("retained statistics changed bytes", err)
	}
}
