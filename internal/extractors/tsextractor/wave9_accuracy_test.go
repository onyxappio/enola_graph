package tsextractor

import (
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func fileRefFact(ff []facts.Fact, file string) facts.Fact {
	for _, f := range ff {
		if f.Kind == facts.KindFileRef && f.File == file {
			return f
		}
	}
	return facts.Fact{}
}

func hasCallToFile(f facts.Fact, target, file string) bool {
	for _, r := range f.Relations {
		if r.Kind == facts.RelCalls && r.Target == target && r.TargetFile == file {
			return true
		}
	}
	return false
}

func hasCallTarget(f facts.Fact, target string) bool {
	return hasRelation(f, facts.RelCalls, target)
}

func TestExtract_Wave9ReexportFileRefProvenance(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"pkg/index.ts":    "export { Decision, isAccepted } from './decision';\nexport { resolveProtectMaxSeverityBackground as resolveProtectTodoListBackground, resolveProtectTodoAtmosphere } from './risk';\nexport type { ProtectRiskAtmosphere as ProtectTodoAtmosphere } from './risk';\nexport { TRACKING_BATCH_SAFETY_MAX_BODY_BYTES, TRACKING_ROUTE_BODY_LIMIT_BYTES } from './tracker';\nexport { missing } from './tracker';\nexport { cycleA } from './cycle-a';\n",
		"pkg/decision.ts": "export const Decision = { Accept: 'a' };\nexport function isAccepted() { return true; }\n",
		"pkg/types.ts":    "export type Decision<S> = { state: S };\n",
		"pkg/risk.ts": `
export function resolveProtectMaxSeverityBackground() { return 1; }
export function resolveProtectTodoListBackground() { return 2; }
export function resolveProtectTodoAtmosphere() { return 3; }
export type ProtectRiskAtmosphere = { n: number };
export type ProtectTodoAtmosphere = { n: number };
`,
		"pkg/tracker.ts": `
import { TRACKING_BATCH_SAFETY_MAX_BODY_BYTES, TRACKING_ROUTE_BODY_LIMIT_BYTES } from './runtimePrimitives';
export { TRACKING_BATCH_SAFETY_MAX_BODY_BYTES, TRACKING_ROUTE_BODY_LIMIT_BYTES };
export const defaultMaxBatchSize = 10;
`,
		"pkg/runtimePrimitives.ts": "export const TRACKING_BATCH_SAFETY_MAX_BODY_BYTES = 1;\nexport const TRACKING_ROUTE_BODY_LIMIT_BYTES = 2;\n",
		"pkg/cycle-a.ts":           "export { cycleA } from './cycle-b';\n",
		"pkg/cycle-b.ts":           "export { cycleA } from './cycle-a';\n",
		"pkg/consumer.ts": `
import { Decision, isAccepted, resolveProtectTodoListBackground, resolveProtectTodoAtmosphere, TRACKING_ROUTE_BODY_LIMIT_BYTES, defaultMaxBatchSize } from './index';
export function use(d: typeof Decision) { return isAccepted() && resolveProtectTodoListBackground() && resolveProtectTodoAtmosphere() && TRACKING_ROUTE_BODY_LIMIT_BYTES && defaultMaxBatchSize; }
`,
	}, false)

	idx := fileRefFact(ff, "pkg/index.ts")
	if idx.Name == "" {
		t.Fatal("index file_ref missing")
	}
	if !hasCallToFile(idx, "pkg.Decision", "pkg/decision.ts") {
		t.Fatalf("reexport Decision must target decision.ts: %+v", idx.Relations)
	}
	if hasCallToFile(idx, "pkg.Decision", "pkg/types.ts") {
		t.Fatal("Decision must not target types.ts")
	}
	if !hasCallToFile(idx, "pkg.resolveProtectMaxSeverityBackground", "pkg/risk.ts") {
		t.Fatalf("aliased reexport must target MaxSeverity: %+v", idx.Relations)
	}
	if hasCallTarget(idx, "pkg.resolveProtectTodoListBackground") && hasCallToFile(idx, "pkg.resolveProtectTodoListBackground", "pkg/risk.ts") {
		// alias name as target is wrong when that name is a different function
		for _, r := range idx.Relations {
			if r.Kind == facts.RelCalls && r.Target == "pkg.resolveProtectTodoListBackground" {
				t.Fatalf("aliased reexport used public alias as target: %+v", r)
			}
		}
	}
	if !hasCallToFile(idx, "pkg.resolveProtectTodoAtmosphere", "pkg/risk.ts") {
		t.Fatalf("non-aliased reexport must stay: %+v", idx.Relations)
	}
	if !hasCallToFile(idx, "pkg.ProtectRiskAtmosphere", "pkg/risk.ts") {
		t.Fatalf("type alias reexport must target ProtectRiskAtmosphere: %+v", idx.Relations)
	}
	if !hasCallToFile(idx, "pkg.TRACKING_BATCH_SAFETY_MAX_BODY_BYTES", "pkg/runtimePrimitives.ts") {
		t.Fatalf("chained reexport must follow to runtimePrimitives: %+v", idx.Relations)
	}
	if !hasCallToFile(idx, "pkg.TRACKING_ROUTE_BODY_LIMIT_BYTES", "pkg/runtimePrimitives.ts") {
		t.Fatalf("chained TRACKING_ROUTE missing: %+v", idx.Relations)
	}
	for _, r := range idx.Relations {
		if r.Kind != facts.RelCalls {
			continue
		}
		if strings.Contains(r.Target, "missing") && r.TargetFile == "pkg/runtimePrimitives.ts" {
			t.Fatalf("missing export guessed runtimePrimitives: %+v", r)
		}
		if strings.Contains(r.Target, "cycleA") && r.TargetFile != "" && r.TargetFile != "pkg/cycle-a.ts" && r.TargetFile != "pkg/cycle-b.ts" {
			t.Fatalf("cyclic reexport guessed unrelated file: %+v", r)
		}
	}

	tracker := fileRefFact(ff, "pkg/tracker.ts")
	if !hasCallToFile(tracker, "pkg.TRACKING_ROUTE_BODY_LIMIT_BYTES", "pkg/runtimePrimitives.ts") {
		t.Fatalf("direct tracker reexport/import must still resolve: %+v", tracker.Relations)
	}

	use, _ := findFact(ff, "pkg.use")
	if use.File != "pkg/consumer.ts" {
		for _, f := range ff {
			if f.Name == "pkg.use" && f.File == "pkg/consumer.ts" {
				use = f
			}
		}
	}
	if !hasCallToFile(use, "pkg.resolveProtectMaxSeverityBackground", "pkg/risk.ts") && !hasCallTarget(use, "pkg.resolveProtectMaxSeverityBackground") {
		// consumer imports the public alias; bindImportedSymbol should still follow to MaxSeverity
		if !hasCallTarget(use, "pkg.resolveProtectTodoListBackground") {
			t.Fatalf("consumer calls missing: %+v", use.Relations)
		}
	}
}

func TestExtract_Wave9LexicalShadowFileRefAndSymbolCalls(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/app.ts": `
import { isLocalHost } from './local';
export function isLocalRequest(host: string | undefined, origin?: string) {
  return isLocalHost(host) && Boolean(origin);
}
export function restParam(...host: string[]) { return isLocalHost(host[0]); }
export function destructured({ fetchJson, other = 1 }: { fetchJson: (u: string) => Promise<unknown>; other?: number }) {
  return fetchJson('/x');
}
export function defaults(host = 'localhost') { return isLocalHost(host); }
export function afterBlock(imported: string) {
  {
    const host = imported;
    isLocalHost(host);
  }
  return imported;
}
export function catchHost() {
  try { throw new Error('x'); } catch (host) { return isLocalHost(String(host)); }
}
export async function nestedLocals() {
  const value = () => { return string() + array() + object() + number(); };
  const string = () => 's';
  const array = () => 'a';
  const object = () => 'o';
  const number = () => 'n';
  return value();
}
export async function awaitedImport() {
  const { helper } = await import('./local');
  return helper();
}
`,
		"src/local.ts":  "export function isLocalHost(h?: string) { return h === 'localhost'; }\nexport function helper() { return 1; }\n",
		"src/server.ts": "export const host = '0.0.0.0';\nexport async function fetchJson(url: string) { return url; }\nexport function string() { return 'sib'; }\nexport function array() { return []; }\nexport function object() { return {}; }\nexport function number() { return 0; }\nexport function helper() { return 2; }\n",
		"src/Widget.tsx": `
import { Card } from './Card';
export function Page({ Card: CardSlot }: { Card: () => unknown }) {
  return <CardSlot />;
}
export function Real() { return <Card />; }
`,
		"src/Card.tsx": "export function Card() { return null; }\n",
	}, false)

	appFR := fileRefFact(ff, "src/app.ts")
	if appFR.Name == "" {
		t.Fatal("app file_ref missing")
	}
	if hasCallToFile(appFR, "src.host", "src/server.ts") {
		t.Fatalf("param host leaked to server.ts: %+v", appFR.Relations)
	}
	if !hasCallToFile(appFR, "src.isLocalHost", "src/local.ts") {
		t.Fatalf("genuine isLocalHost import missing from file_ref: %+v", appFR.Relations)
	}
	if hasCallToFile(appFR, "src.fetchJson", "src/server.ts") {
		t.Fatalf("destructured fetchJson leaked: %+v", appFR.Relations)
	}
	if hasCallToFile(appFR, "src.string", "src/server.ts") || hasCallToFile(appFR, "src.array", "src/server.ts") {
		t.Fatalf("nested locals leaked via file_ref: %+v", appFR.Relations)
	}
	if !hasCallToFile(appFR, "src.helper", "src/local.ts") {
		t.Fatalf("awaited import helper missing on file_ref: %+v", appFR.Relations)
	}

	nested, ok := findFact(ff, "src.nestedLocals")
	if !ok {
		t.Fatal("nestedLocals missing")
	}
	for _, name := range []string{"src.string", "src.array", "src.object", "src.number"} {
		if hasCallTarget(nested, name) {
			t.Fatalf("symbol-owned nestedLocals must not call sibling %s: %+v", name, nested.Relations)
		}
	}
	list, _ := findFact(ff, "src.destructured")
	if hasCallToFile(list, "src.fetchJson", "src/server.ts") || hasCallTarget(list, "src.fetchJson") {
		t.Fatalf("destructured param fetchJson leaked to sibling: %+v", list.Relations)
	}
	req, _ := findFact(ff, "src.isLocalRequest")
	if !hasCallToFile(req, "src.isLocalHost", "src/local.ts") {
		t.Fatalf("isLocalRequest must still call imported isLocalHost: %+v", req.Relations)
	}
	if hasCallToFile(req, "src.host", "src/server.ts") {
		t.Fatal("symbol-owned host param leaked")
	}

	page, _ := findFact(ff, "src.Page")
	if hasCallToFile(page, "src.Card", "src/Card.tsx") {
		t.Fatalf("shadowed JSX Card param must not call import: %+v", page.Relations)
	}
	real, _ := findFact(ff, "src.Real")
	if !hasCallToFile(real, "src.Card", "src/Card.tsx") {
		t.Fatalf("unshadowed JSX must call Card: %+v", real.Relations)
	}
	widgetFR := fileRefFact(ff, "src/Widget.tsx")
	if hasCallToFile(widgetFR, "src.Card", "src/Card.tsx") {
		// file_ref still records the Real() use
	}
	if !hasCallToFile(widgetFR, "src.Card", "src/Card.tsx") {
		t.Fatalf("file_ref must keep genuine JSX Card use: %+v", widgetFR.Relations)
	}
}

func TestExtract_Wave9TypeAndInterfaceDoNotShadowValueImports(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/dep.ts": "export function callback() { return 1; }\nexport function keep() { return 2; }\nexport type Repo = { id: string };\n",
		"src/type.ts": `
import { callback, keep, Repo } from './dep';
export function run() {
  type callback = string;
  callback();
  keep();
}
export function typed(r: Repo) { return r; }
`,
		"src/iface.ts": `
import { callback, keep } from './dep';
export function run() {
  interface callback { n: number }
  callback();
  keep();
}
`,
	}, false)

	for _, file := range []string{"src/type.ts", "src/iface.ts"} {
		fr := fileRefFact(ff, file)
		if !hasCallToFile(fr, "src.callback", "src/dep.ts") {
			t.Fatalf("%s file_ref lost imported callback: %+v", file, fr.Relations)
		}
		if !hasCallToFile(fr, "src.keep", "src/dep.ts") {
			t.Fatalf("%s file_ref lost keep: %+v", file, fr.Relations)
		}
	}
	typedFR := fileRefFact(ff, "src/type.ts")
	if !hasCallToFile(typedFR, "src.Repo", "src/dep.ts") {
		t.Fatalf("imported type annotation Repo missing: %+v", typedFR.Relations)
	}

	typeRun, ok := findFact(ff, "src.run")
	if !ok {
		t.Fatal("src.run missing")
	}
	// extractAll may emit two src.run facts; check each owner file.
	var typeOwned, ifaceOwned facts.Fact
	for _, f := range ff {
		if f.Name != "src.run" {
			continue
		}
		if f.File == "src/type.ts" {
			typeOwned = f
		}
		if f.File == "src/iface.ts" {
			ifaceOwned = f
		}
	}
	if typeOwned.Name == "" {
		typeOwned = typeRun
	}
	for _, owned := range []facts.Fact{typeOwned, ifaceOwned} {
		if owned.Name == "" {
			t.Fatal("symbol-owned run missing")
		}
		if !hasCallToFile(owned, "src.callback", "src/dep.ts") && !hasCallTarget(owned, "src.callback") {
			t.Fatalf("%s run must call imported callback: %+v", owned.File, owned.Relations)
		}
		if !hasCallToFile(owned, "src.keep", "src/dep.ts") && !hasCallTarget(owned, "src.keep") {
			t.Fatalf("%s run must call keep: %+v", owned.File, owned.Relations)
		}
	}
}
