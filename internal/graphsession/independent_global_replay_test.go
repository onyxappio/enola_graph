package graphsession

import (
	"github.com/enola-labs/enola/internal/graphinput"
	"testing"
)

// A global candidate-name change must rebind consumers even when their local
// source facts are reused. Assert cold equality before the parse-budget check.
func TestIndependentGlobalCandidateReplayAcrossEdits(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/provider.ts": "export function known() { return 1; }\n",
		"src/consumer.ts": "export function consume() { return future(); }\n",
		"src/other.ts":    "export function unrelated() { return 0; }\n",
	})
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, dir, opts, cons)
	for _, tc := range []struct{ name, source string }{
		{"add", "export function known() { return 1; }\nexport function future() { return 2; }\n"},
		{"rename", "export function known() { return 1; }\nexport function renamed() { return 2; }\n"},
		{"restore", "export function known() { return 1; }\nexport function future() { return 3; }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, dir, "src/provider.ts", tc.source)
			res, _ := admissionRun(t, eng, dir, opts, cons)
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
			found, resolved := false, false
			for _, edge := range cons.Edges[ownerKey("src/consumer.ts")] {
				if edge.Kind == "calls" && edge.TargetName == "src.future" {
					found = true
					resolved = edge.TargetID != ""
				}
			}
			if !found || resolved != (tc.name != "rename") {
				t.Fatalf("consumer binding did not change as intended: found=%v resolved=%v edges=%+v", found, resolved, cons.Edges[ownerKey("src/consumer.ts")])
			}
			if res.ParsedFiles != 1 {
				t.Errorf("local parse reuse target: parsed=%d reasons=%+v", res.ParsedFiles, res.Invalidation)
			}
		})
	}
}
