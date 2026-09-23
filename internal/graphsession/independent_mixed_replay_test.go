package graphsession

import (
	"github.com/enola-labs/enola/internal/graphinput"
	"testing"
)

func TestIndependentGlobalReplayKeepsDefaultOriginSideReads(t *testing.T) {
	body := "export function round(n: number) { return n + 1; }\nexport function ceil(n: number) { return n + 2; }\nround(1); ceil(1);\n"
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts":      "import { pick } from './barrel';\nexport function caller() { return pick(1) + future(); }\n",
		"src/barrel.ts": "export { default as pick } from './origin';\n",
		"src/origin.ts": body + "export default round;\n",
		"src/global.ts": "export function existing() { return 0; }\n",
	})
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, dir, opts, cons)
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.round", "src/origin.ts")
	writeFile(t, dir, "src/origin.ts", body+"export default ceil;\n")
	writeFile(t, dir, "src/global.ts", "export function existing() { return 0; }\nexport function future() { return 1; }\n")
	admissionRun(t, eng, dir, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.ceil", "src/origin.ts")
	assertCallResolvedToFile(t, cons, "src/a.ts", "src.future", "src/global.ts")
}
