package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestIndependentGlobalReplayKeepsDefaultOriginSideReads(t *testing.T) {
	body := "export function round(n: number) { return n + 1; }\nexport function ceil(n: number) { return n + 2; }\nround(1); ceil(1);\n"
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":        "import { pick } from './barrel';\nexport function caller() { return pick(1) + future(); }\n",
		"src/imported.ts": "import { future } from './global';\nexport function importedCaller() { return future(); }\n",
		"src/barrel.ts":   "export { default as pick } from './origin';\n",
		"src/origin.ts":   body + "export default round;\n",
		"src/global.ts":   "export function existing() { return 0; }\n",
	})
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, dir, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/origin.ts")

	assertFutureResolution := func(t *testing.T, owner string, want string) {
		t.Helper()
		var found bool
		for _, edge := range cons.Edges[ownerKey(owner)] {
			if edge.Kind != facts.RelCalls || edge.TargetName != "src.future" {
				continue
			}
			found = true
			if edge.Resolution != want {
				t.Fatalf("%s future() resolution=%s, want %s: %+v", owner, edge.Resolution, want, edge)
			}
			if want == graphstream.ResUnresolved && edge.TargetID != "" {
				t.Fatalf("%s future() unresolved edge has target id %q: %+v", owner, edge.TargetID, edge)
			}
			if want == graphstream.ResResolved {
				if edge.TargetID == "" || nodeByID(cons, edge.TargetID).File != "src/global.ts" {
					t.Fatalf("%s future() target id=%q, want src/global.ts: %+v", owner, edge.TargetID, edge)
				}
			}
		}
		if !found {
			t.Fatalf("%s has no future() call edge", owner)
		}
	}
	assertFutureResolution(t, "src/a.ts", graphstream.ResUnresolved)
	assertFutureResolution(t, "src/imported.ts", graphstream.ResUnresolved)

	for _, step := range []struct {
		name        string
		origin      string
		global      string
		defaultName string
		future      string
	}{
		{
			name:        "add-future-and-switch-default",
			origin:      "export default ceil;\n",
			global:      "export function existing() { return 0; }\nexport function future() { return 1; }\n",
			defaultName: "src.ceil",
			future:      graphstream.ResResolved,
		},
		{
			name:        "remove-future-and-switch-default",
			origin:      "export default round;\n",
			global:      "export function existing() { return 0; }\n",
			defaultName: "src.round",
			future:      graphstream.ResUnresolved,
		},
		{
			name:        "restore-future-and-switch-default",
			origin:      "export default ceil;\n",
			global:      "export function existing() { return 0; }\nexport function future() { return 2; }\n",
			defaultName: "src.ceil",
			future:      graphstream.ResResolved,
		},
	} {
		t.Run(step.name, func(t *testing.T) {
			writeFile(t, dir, "src/origin.ts", body+step.origin)
			writeFile(t, dir, "src/global.ts", step.global)
			admissionRun(t, eng, dir, opts, cons)
			assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
			assertCallResolvedToFile(t, cons, "src/a.ts", step.defaultName, "src/origin.ts")
			assertFutureResolution(t, "src/a.ts", graphstream.ResUnresolved)
			assertFutureResolution(t, "src/imported.ts", step.future)
		})
	}
}
