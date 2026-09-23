package tsextractor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// The five Product import forms that arrived as "target exists, edge unresolved"
// are one resolver pipeline: bind the specifier to a known file, then name the
// owning module directory as RelImports.Target so it matches KindModule.Name.
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

	t.Run("tsconfig-package-alias", func(t *testing.T) {
		if !importEdgeResolves(t, ff, "packages/tracking-client/src/core/consumer.ts", "packages/contracts/src", "packages/contracts/src/index.ts") {
			t.Fatalf("missing resolved alias import:\n%s", importDump(ff, "packages/tracking-client/src/core/consumer.ts"))
		}
	})
	t.Run("relative-import-type", func(t *testing.T) {
		if !importEdgeResolves(t, ff, "services/product-api/src/productUsersSync.reconciliation.ts", "services/product-api/src", "services/product-api/src/productUsersSync.repair.ts") {
			t.Fatalf("missing resolved import type:\n%s", importDump(ff, "services/product-api/src/productUsersSync.reconciliation.ts"))
		}
	})
	t.Run("runtime-dynamic-import", func(t *testing.T) {
		var dynamic bool
		for _, d := range findFactsByKind(ff, facts.KindDependency) {
			if d.File != "services/product-api/src/productUsersSync.reconciliation.ts" {
				continue
			}
			if d.Props["dynamic"] == true && hasRelation(d, facts.RelImports, "services/product-api/src") &&
				d.PropString(facts.PropTargetFile) == "services/product-api/src/productUsersSync.repair.ts" {
				dynamic = true
			}
		}
		if !dynamic {
			t.Fatal("runtime import() of productUsersSync.repair.ts was not emitted as a dynamic internal edge")
		}
		if !importEdgeResolves(t, ff, "services/product-api/src/productUsersSync.reconciliation.ts", "services/product-api/src", "services/product-api/src/productUsersSync.repair.ts") {
			t.Fatal("dynamic import module edge missing")
		}
	})
	t.Run("namespace-import", func(t *testing.T) {
		if !importEdgeResolves(t, ff, "apps/mobile/src/behavior/mobileAppInterpreter.ts", "apps/mobile/src/state", "apps/mobile/src/state/mobileAppMachine.updates.ts") {
			t.Fatalf("missing resolved namespace import:\n%s", importDump(ff, "apps/mobile/src/behavior/mobileAppInterpreter.ts"))
		}
	})
	t.Run("star-reexport", func(t *testing.T) {
		if !importEdgeResolves(t, ff, "packages/tracking-server/src/index.ts", "packages/tracking-server/src", "packages/tracking-server/src/publisher.ts") {
			t.Fatalf("missing resolved export *:\n%s", importDump(ff, "packages/tracking-server/src/index.ts"))
		}
	})

	s := facts.NewStore()
	s.Add(ff...)
	var buf bytes.Buffer
	if err := s.WriteJSONL(&buf); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"packages/tracking-client/src/core/consumer.ts\x00packages/contracts/src":                 false,
		"services/product-api/src/productUsersSync.reconciliation.ts\x00services/product-api/src": false,
		"apps/mobile/src/behavior/mobileAppInterpreter.ts\x00apps/mobile/src/state":               false,
		"packages/tracking-server/src/index.ts\x00packages/tracking-server/src":                   false,
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
	if !importEdgeResolves(t, ff, "apps/mobile/src/app.ts", "packages/contracts/src", "packages/contracts/src/index.ts") {
		t.Fatalf("package.json name alias did not bind @onyx/contracts:\n%s", importDump(ff, "apps/mobile/src/app.ts"))
	}
}

func TestExtract_PackageJSONExportsSubpathAndExistingTypes(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/shared-lands-types/package.json": `{
  "name":"shared-lands-types",
  "exports":{
    ".":{"types":"./src/index.ts","import":"./dist/index.mjs"},
    "./enums":{"types":"./src/enums.ts","import":"./dist/enums.mjs"}
  }
}`,
		"packages/shared-lands-types/src/index.ts": `export const X = 1;`,
		"packages/shared-lands-types/src/enums.ts": `export enum GenderEnum { Male = 'm' }`,
		"apps/land/Welcome.ts":                     `import { GenderEnum } from 'shared-lands-types/enums'; export const g = GenderEnum.Male;`,
	}, false)
	if !importEdgeResolves(t, ff, "apps/land/Welcome.ts", "packages/shared-lands-types/src", "packages/shared-lands-types/src/enums.ts") {
		t.Fatalf("exports ./enums did not bind:\n%s", importDump(ff, "apps/land/Welcome.ts"))
	}
	var found bool
	for _, f := range ff {
		if hasRelation(f, facts.RelCalls, "packages/shared-lands-types/src.GenderEnum") {
			found = true
		}
	}
	if !found {
		t.Fatal("GenderEnum reference missing after exports bind")
	}
}

func TestExtract_PackageJSONExportsSkipsMissingDistTypes(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/tracking-client/package.json": `{
  "name":"@onyxappio/tracking-client",
  "types":"./dist/index.d.ts",
  "exports":{
    ".":{"types":"./dist/index.d.ts","react-native":"./src/index.ts","import":"./dist/index.js"},
    "./native":{"types":"./dist/native/index.d.ts","react-native":"./src/native/index.ts"}
  }
}`,
		"packages/tracking-client/src/index.ts":        `export function createEventId() {}`,
		"packages/tracking-client/src/native/index.ts": `export function createNativeTracker() {}`,
		"apps/mobile/src/tracker.ts": `import { createEventId } from '@onyxappio/tracking-client';
import { createNativeTracker } from '@onyxappio/tracking-client/native';
export const a = createEventId;
export const b = createNativeTracker;`,
	}, false)
	if !importEdgeResolves(t, ff, "apps/mobile/src/tracker.ts", "packages/tracking-client/src", "packages/tracking-client/src/index.ts") {
		t.Fatalf("root export should skip missing dist types:\n%s", importDump(ff, "apps/mobile/src/tracker.ts"))
	}
	if !importEdgeResolves(t, ff, "apps/mobile/src/tracker.ts", "packages/tracking-client/src/native", "packages/tracking-client/src/native/index.ts") {
		t.Fatalf("native subpath unresolved:\n%s", importDump(ff, "apps/mobile/src/tracker.ts"))
	}
}

func TestExtract_PackageJSONExportsMissingSubpathStaysExternal(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/lib/package.json": `{"name":"lib","exports":{".":"./src/index.ts"}}`,
		"packages/lib/src/index.ts": `export const X = 1;`,
		"apps/app.ts":               `import { X } from 'lib/missing'; export const y = X;`,
	}, false)
	for _, f := range ff {
		if f.Kind == facts.KindDependency && f.File == "apps/app.ts" && f.PropString("source") == "internal" {
			t.Fatalf("missing export subpath must stay external: %+v", f)
		}
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
	if !importEdgeResolves(t, ff, "packages/app/src/main.ts", "packages/contracts/src", "packages/contracts/src/override.ts") {
		t.Fatalf("tsconfig paths should win over package.json main:\n%s", importDump(ff, "packages/app/src/main.ts"))
	}
	if importEdgeResolves(t, ff, "packages/app/src/main.ts", "packages/contracts/src", "packages/contracts/src/index.ts") {
		t.Fatal("package.json main leaked through an overlapping tsconfig paths entry")
	}
}

func importEdgeResolves(t *testing.T, ff []facts.Fact, file, module, targetFile string) bool {
	t.Helper()
	var hasEdge, hasModule bool
	for _, f := range ff {
		if f.Kind == facts.KindModule && f.Name == module {
			hasModule = true
		}
		if f.Kind == facts.KindDependency && f.File == file && hasRelation(f, facts.RelImports, module) {
			if f.PropString("source") == "internal" && f.PropString(facts.PropTargetFile) == targetFile {
				hasEdge = true
			}
		}
	}
	return hasEdge && hasModule
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
