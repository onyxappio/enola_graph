package tsextractor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// The five Product import forms that arrived as "target exists, edge unresolved"
// are one resolver pipeline: bind the specifier to a known file, then name that
// file as RelImports.Target so it matches the KindFileRef fact of the destination.
func TestExtract_ImportEdgesResolveToExistingFiles(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/contracts/package.json": `{
			"name": "@onyx/contracts",
			"main": "./src/index.ts",
			"types": "./src/index.ts"
		}`,
		"packages/contracts/src/index.ts": `
export const FIRST_SESSION_ADVANCE_RECOVERY_LIMIT = 1;
export function catalogTrackingEventSchema() { return 1; }
`,
		"packages/tracking-client/package.json": `{"name":"@onyx/tracking-client"}`,
		"packages/tracking-client/src/core/consumer.ts": `
import { catalogTrackingEventSchema } from '@onyx/contracts';
export function consume() { return catalogTrackingEventSchema(); }
`,
		"packages/tracking-client/tsconfig.json": `{
			"compilerOptions": {
				"paths": { "@onyx/contracts": ["../contracts/src/index.ts"] }
			}
		}`,
		"services/product-api/src/productUsersSync.repair.ts": `
export type ProductUsersRepairPorts = { run(): Promise<string> };
export async function runProductUsersMismatchRepair(_input: unknown): Promise<'matched'> {
  return 'matched';
}
`,
		"services/product-api/src/productUsersSync.reconciliation.ts": `
import type { ProductUsersRepairPorts } from './productUsersSync.repair';
export async function invokeRepair(ports: ProductUsersRepairPorts): Promise<string> {
  const { runProductUsersMismatchRepair } = await import('./productUsersSync.repair');
  return runProductUsersMismatchRepair(ports);
}
`,
		"apps/mobile/src/state/mobileAppMachine.updates.ts": `
export function applyUpdate() { return 1; }
`,
		"apps/mobile/src/behavior/mobileAppInterpreter.ts": `
import * as update from '../state/mobileAppMachine.updates';
export function interpret() { return update.applyUpdate(); }
`,
		"packages/tracking-server/src/publisher.ts": `
export function createTrackingPublisher() { return {}; }
`,
		"packages/tracking-server/src/index.ts": `
export * from './publisher';
`,
	}, false)

	cases := []struct {
		file   string
		target string
	}{
		{"packages/tracking-client/src/core/consumer.ts", "packages/contracts/src/index.ts"},
		{"services/product-api/src/productUsersSync.reconciliation.ts", "services/product-api/src/productUsersSync.repair.ts"},
		{"apps/mobile/src/behavior/mobileAppInterpreter.ts", "apps/mobile/src/state/mobileAppMachine.updates.ts"},
		{"packages/tracking-server/src/index.ts", "packages/tracking-server/src/publisher.ts"},
	}
	for _, tc := range cases {
		if !importEdgeResolves(t, ff, tc.file, tc.target) {
			t.Errorf("%s: missing resolved import edge to %s\n%s", tc.file, tc.target, importDump(ff, tc.file))
		}
	}

	var dynamic bool
	for _, d := range findFactsByKind(ff, facts.KindDependency) {
		if d.File != "services/product-api/src/productUsersSync.reconciliation.ts" {
			continue
		}
		if d.Props["dynamic"] == true && hasRelation(d, facts.RelImports, "services/product-api/src/productUsersSync.repair.ts") {
			dynamic = true
		}
	}
	if !dynamic {
		t.Error("runtime import() of productUsersSync.repair.ts was not emitted as a dynamic internal edge")
	}

	s := facts.NewStore()
	s.Add(ff...)
	var buf bytes.Buffer
	if err := s.WriteJSONL(&buf); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, tc := range cases {
		want[tc.file+"\x00"+tc.target] = false
	}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["kind"] != facts.KindDependency {
			continue
		}
		file, _ := m["file"].(string)
		rels, _ := m["relations"].([]any)
		for _, raw := range rels {
			rel := raw.(map[string]any)
			if rel["kind"] != facts.RelImports {
				continue
			}
			target, _ := rel["target"].(string)
			key := file + "\x00" + target
			if _, ok := want[key]; !ok {
				continue
			}
			if _, has := rel["target_id"]; !has {
				t.Errorf("%s -> %s has no target_id", file, target)
				continue
			}
			want[key] = true
		}
	}
	for key, ok := range want {
		if !ok {
			file, target, _ := strings.Cut(key, "\x00")
			t.Errorf("JSONL missing resolved import %s -> %s", file, target)
		}
	}
}

func TestExtract_PackageJSONNameAliasWithoutTSConfigPaths(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/contracts/package.json": `{"name":"@onyx/contracts","main":"./src/index.ts"}`,
		"packages/contracts/src/index.ts": `export const Token = 1;`,
		"apps/mobile/src/app.ts":          `import { Token } from '@onyx/contracts'; export const t = Token;`,
	}, false)
	if !importEdgeResolves(t, ff, "apps/mobile/src/app.ts", "packages/contracts/src/index.ts") {
		t.Fatalf("package.json name alias did not bind @onyx/contracts:\n%s", importDump(ff, "apps/mobile/src/app.ts"))
	}
}

func TestExtract_TSConfigPathsBeatPackageJSONName(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/contracts/package.json":    `{"name":"@onyx/contracts","main":"./src/index.ts"}`,
		"packages/contracts/src/index.ts":    `export const fromPkg = 1;`,
		"packages/contracts/src/override.ts": `export const fromPath = 1;`,
		"packages/app/tsconfig.json":         `{"compilerOptions":{"paths":{"@onyx/contracts":["../contracts/src/override.ts"]}}}`,
		"packages/app/src/main.ts":           `import { fromPath } from '@onyx/contracts'; export const v = fromPath;`,
	}, false)
	if !importEdgeResolves(t, ff, "packages/app/src/main.ts", "packages/contracts/src/override.ts") {
		t.Fatalf("tsconfig paths should win over package.json main:\n%s", importDump(ff, "packages/app/src/main.ts"))
	}
	if importEdgeResolves(t, ff, "packages/app/src/main.ts", "packages/contracts/src/index.ts") {
		t.Fatal("package.json main leaked through an overlapping tsconfig paths entry")
	}
}

func importEdgeResolves(t *testing.T, ff []facts.Fact, file, target string) bool {
	t.Helper()
	var hasEdge, hasNode bool
	for _, f := range ff {
		if f.Kind == facts.KindFileRef && f.Name == target {
			hasNode = true
		}
		if f.Kind == facts.KindDependency && f.File == file && hasRelation(f, facts.RelImports, target) {
			src, _ := f.Props["source"].(string)
			if src == "internal" {
				hasEdge = true
			}
		}
	}
	return hasEdge && hasNode
}

func importDump(ff []facts.Fact, file string) string {
	var b strings.Builder
	for _, f := range ff {
		if f.Kind != facts.KindDependency || f.File != file {
			continue
		}
		b.WriteString(f.Name)
		b.WriteString(" source=")
		b.WriteString(f.PropString("source"))
		for _, r := range f.Relations {
			b.WriteString(" ")
			b.WriteString(r.Kind)
			b.WriteString("->")
			b.WriteString(r.Target)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
