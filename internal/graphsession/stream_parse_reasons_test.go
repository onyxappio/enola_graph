package graphsession

import (
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
)

func TestStreamReportsActualParseReasonCounts(t *testing.T) {
	root := setupTSRepo(t, map[string]string{"src/a.ts": "export function a(){return 1}", "src/b.ts": "import {a} from './a'; export function b(){return a()}"})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	for i := 0; i < 2; i++ {
		if i == 1 {
			writeFile(t, root, "src/a.ts", "export function a(){return 1}; export const added=2")
		}
		result, sink := admissionRun(t, eng, root, opts, cons)
		_, _, ends, err := DecodeRun(sink.CloneRecords())
		if err != nil || len(ends) != 1 {
			t.Fatalf("ends=%d err=%v", len(ends), err)
		}
		counts := ends[0].Completeness.ParsedByReason
		if !reflect.DeepEqual(counts, result.Invalidation.ParsedByReason) {
			t.Fatalf("stream reasons %v != result %v", counts, result.Invalidation.ParsedByReason)
		}
		total := 0
		for _, n := range counts {
			total += n
		}
		if total != result.ParsedFiles || total == 0 {
			t.Fatalf("reason total=%d parsed=%d", total, result.ParsedFiles)
		}
		assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	}
	result, sink := admissionRun(t, eng, root, opts, cons)
	if result.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 {
		t.Fatal("no-op must remain silent")
	}
}
