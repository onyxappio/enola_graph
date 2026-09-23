package graphsession

import (
	"github.com/enola-labs/enola/internal/graphinput"
	"testing"
)

func TestIndependentExportAdditionReusesUnchangedImporterParse(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{
		"src/a.ts": "export function a() { return 1; }\n",
		"src/b.ts": "import { a } from './a';\nexport function b() { return a(); }\n",
	})
	eng := admissionEngine(t, dir, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	admissionRun(t, eng, dir, opts, cons)
	writeFile(t, dir, "src/a.ts", "export function a() { return 1; }\nexport function added() { return 2; }\n")
	res, _ := admissionRun(t, eng, dir, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, dir))
	if res.ParsedFiles != 1 {
		t.Fatalf("unchanged importer local facts were reparsed: parsed=%d reasons=%+v", res.ParsedFiles, res.Invalidation)
	}
}
