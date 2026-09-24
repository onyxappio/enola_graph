package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
)

func TestIndependentLocalExportBecomesForwardedWithStableDeclarations(t *testing.T) {
	for _, authoritative := range []bool{true, false} {
		name := "legacy"
		if authoritative {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			root := setupTSRepo(t, map[string]string{
				"late/late.ts": "export function work() { return 2; }\n",
				"mid/mid.ts":   "function work() { return 1; }\nexport { work };\n",
				"use/use.ts":   "import { work } from '../mid/mid';\nexport const result = work();\n",
			})
			eng := testEngine(t, root)
			opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: authoritative}
			cons := bodyScopeStart(t, eng, root, opts)
			record := func(path string) *tsextractor.FileRecord {
				st, err := loadCommittedState(opts.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				rec := lookupState(st.Files, path)
				if rec == nil || rec.TS == nil {
					t.Fatalf("missing record %s", path)
				}
				return rec.TS
			}
			before := record("mid/mid.ts")
			for _, p := range record("use/use.ts").SideReads {
				if p == "mid/mid.ts" {
					t.Fatal("old locally bound consumer unexpectedly has mid side read")
				}
			}
			writeFile(t, root, "mid/mid.ts", "import { work as imported } from '../late/late';\nfunction work() { return 1; }\nexport { imported as work };\n")
			bodyScopeDeltaCold(t, eng, root, opts, cons)
			after := record("mid/mid.ts")
			if !eqStrings(before.Declared, after.Declared) {
				t.Fatalf("declared moved: %v -> %v", before.Declared, after.Declared)
			}
			if len(before.Reexports) != 0 || len(after.Reexports) != 0 {
				t.Fatalf("unexpected explicit reexports: %v -> %v", before.Reexports, after.Reexports)
			}
			if !surfaceChanged(before, after) {
				t.Fatal("expected outgoing surface change")
			}
			t.Logf("stable Declared=%v, Reexports empty; outgoing %v -> %v; old local binding has no SideReads", after.Declared, before.ResolvedFiles, after.ResolvedFiles)

			// The reverse transition also changes a consumer without changing the
			// declared names; neither endpoint alone proves the binding is stable.
			writeFile(t, root, "mid/mid.ts", "function work() { return 1; }\nexport { work };\n")
			bodyScopeDeltaCold(t, eng, root, opts, cons)
			restored := record("mid/mid.ts")
			if !eqStrings(before.Declared, restored.Declared) || len(restored.ResolvedFiles) != 0 {
				t.Fatalf("local export was not restored: %+v", restored)
			}
		})
	}
}
