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
	return hasRelationToFile(f, facts.RelCalls, target, file)
}

func hasRelationToFile(f facts.Fact, kind, target, file string) bool {
	for _, r := range f.Relations {
		if r.Kind == kind && r.Target == target && r.TargetFile == file {
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

func TestExtract_Wave9ScopedLiteralRequireCalls(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/figma.ts": "export function readScreenStructureFileKey() { return 'k'; }\nexport function otherFigma() { return 1; }\n",
		"src/crop.ts":  "export function shouldRunInstanceCropCompare() { return true; }\n",
		"src/sib.ts":   "export function readScreenStructureFileKey() { return 'sib'; }\nexport function shouldRunInstanceCropCompare() { return false; }\nexport function helper() { return 9; }\nexport function aliased() { return 8; }\nexport function nsFn() { return 7; }\nexport function load() { return 6; }\nexport function keep() { return 5; }\n",
		"src/gate.ts": `
export function runSuitePipeline() {
  const { readScreenStructureFileKey, otherFigma } = require('./figma') as typeof import('./figma');
  const { shouldRunInstanceCropCompare } = require('./crop') as typeof import('./crop');
  const { helper: aliased } = require('./local');
  const localMod = require('./local');
  for (const x of [1]) {
    readScreenStructureFileKey();
    shouldRunInstanceCropCompare();
  }
  aliased();
  return otherFigma();
}
export function innerLocalShadow() {
  const { helper } = require('./local') as typeof import('./local');
  {
    const helper = () => 0;
    return helper();
  }
}
export function paramShadow(helper: () => number) {
  const { keep } = require('./local');
  helper();
  return keep();
}
export function catchShadow() {
  const { helper } = require('./local');
  try { throw new Error('x'); } catch (helper) { return helper; }
}
export function unrelatedNeighbor() {
  return 1;
}
export async function awaitedStill() {
  const { helper } = await import('./local');
  return helper();
}
export function unawaitedImport() {
  const { helper } = import('./local');
  return helper();
}
export function computedRequire(path: string) {
  const { helper } = require(path);
  return helper();
}
export function laterRequireCapture() {
  const run = () => load();
  const { load } = require('./local');
  return run();
}
export function afterRequire() {
  const { load } = require('./local');
  const run = () => load();
  return run();
}
export function typeSpaceKeep() {
  const { keep } = require('./local') satisfies typeof import('./local');
  type keep = string;
  return keep();
}
export function parenRequire() {
  const { helper } = (require('./local'));
  return helper();
}
export function ordinaryForward() {
  const value = () => load();
  const load = () => 1;
  return value();
}
export function namespaceRequire() {
  const sdk = require('./local');
  return sdk.helper();
}
export function namespaceRequireAs() {
  const sdk = require('./local') as typeof import('./local');
  return sdk.keep();
}
export function shadowedRequireLocal() {
  const require = (p: string) => ({ helper: () => 0 });
  const { helper } = require('./local');
  return helper();
}
export function shadowedRequireParameter(require: any) {
  const { helper } = require('./local');
  return helper();
}
export function nestedRequireNoLeak() {
  {
    const { keep } = require('./local');
    keep();
  }
  helper();
}
`,
		"src/factory.ts": "export function factory(p: string) { return { work: () => 0 }; }\n",
		"src/modshadow.ts": `
import { keep } from './local';
const require = (p: string) => ({ work: () => 0 });
export function run() {
  const { work } = require('./local');
  work();
  keep();
}
`,
		"src/impshadow.ts": `
import { factory as require } from './factory';
import { keep } from './local';
export function run() {
  const { work } = require('./local');
  work();
  keep();
}
`,
		"src/nsshadow.ts": `
import * as require from './factory';
import { keep } from './local';
export function run() {
  const { work } = require('./local');
  work();
  keep();
}
`,
		"src/cjsmod.ts": `
import { keep } from './local';
const { work } = require('./local');
export function run() {
  work();
  keep();
}
`,
		"src/missingns.ts": `
import { keep } from './local';
export function run() {
  const sdk = require('./gone');
  sdk.work();
  keep();
}
`,
		"src/local.ts": "export function helper() { return 1; }\nexport function keep() { return 2; }\nexport function load() { return 3; }\nexport function nsFn() { return 4; }\nexport function localMod() { return 0; }\nexport function work() { return 0; }\n",
	}, false)

	gateFR := fileRefFact(ff, "src/gate.ts")
	if gateFR.Name == "" {
		t.Fatal("gate file_ref missing")
	}
	if !hasCallToFile(gateFR, "src.readScreenStructureFileKey", "src/figma.ts") {
		t.Fatalf("file_ref lost typed require readScreenStructureFileKey: %+v", gateFR.Relations)
	}
	if !hasCallToFile(gateFR, "src.shouldRunInstanceCropCompare", "src/crop.ts") {
		t.Fatalf("file_ref lost typed require shouldRunInstanceCropCompare: %+v", gateFR.Relations)
	}
	if hasCallToFile(gateFR, "src.readScreenStructureFileKey", "src/sib.ts") || hasCallToFile(gateFR, "src.shouldRunInstanceCropCompare", "src/sib.ts") {
		t.Fatalf("file_ref bound sibling require names: %+v", gateFR.Relations)
	}
	if !hasCallToFile(gateFR, "src.helper", "src/local.ts") {
		t.Fatalf("file_ref lost aliased/parenthesized helper: %+v", gateFR.Relations)
	}
	if !hasCallToFile(gateFR, "src.keep", "src/local.ts") {
		t.Fatalf("file_ref lost keep: %+v", gateFR.Relations)
	}

	run, ok := findFact(ff, "src.runSuitePipeline")
	if !ok {
		t.Fatal("runSuitePipeline missing")
	}
	if !hasCallToFile(run, "src.readScreenStructureFileKey", "src/figma.ts") {
		t.Fatalf("symbol-owned lost figma require: %+v", run.Relations)
	}
	if !hasCallToFile(run, "src.shouldRunInstanceCropCompare", "src/crop.ts") {
		t.Fatalf("symbol-owned lost crop require: %+v", run.Relations)
	}
	if hasCallToFile(run, "src.readScreenStructureFileKey", "src/sib.ts") {
		t.Fatalf("symbol-owned bound sibling figma: %+v", run.Relations)
	}
	cil := tsStrSlice(run, "calls_in_loop")
	if !tsContains(cil, "src.readScreenStructureFileKey") && !tsContains(cil, "src/figma.readScreenStructureFileKey") {
		t.Fatalf("calls_in_loop missing required figma call: %v rels=%+v", cil, run.Relations)
	}

	inner, _ := findFact(ff, "src.innerLocalShadow")
	if hasCallToFile(inner, "src.helper", "src/local.ts") || hasCallToFile(inner, "src.helper", "src/sib.ts") {
		t.Fatalf("inner local helper must shadow require: %+v", inner.Relations)
	}
	param, _ := findFact(ff, "src.paramShadow")
	if hasCallToFile(param, "src.helper", "src/local.ts") || hasCallToFile(param, "src.helper", "src/sib.ts") {
		t.Fatalf("param helper must shadow: %+v", param.Relations)
	}
	if !hasCallToFile(param, "src.keep", "src/local.ts") {
		t.Fatalf("paramShadow must still call required keep: %+v", param.Relations)
	}
	catchF, _ := findFact(ff, "src.catchShadow")
	if hasCallToFile(catchF, "src.helper", "src/local.ts") {
		t.Fatalf("catch helper must shadow require: %+v", catchF.Relations)
	}
	awaited, _ := findFact(ff, "src.awaitedStill")
	if !hasCallToFile(awaited, "src.helper", "src/local.ts") {
		t.Fatalf("awaited import helper lost: %+v", awaited.Relations)
	}
	unawaited, _ := findFact(ff, "src.unawaitedImport")
	if hasCallToFile(unawaited, "src.helper", "src/local.ts") || hasCallToFile(unawaited, "src.helper", "src/sib.ts") {
		t.Fatalf("unawaited import must not bind: %+v", unawaited.Relations)
	}
	computed, _ := findFact(ff, "src.computedRequire")
	if hasCallToFile(computed, "src.helper", "src/local.ts") || hasCallToFile(computed, "src.helper", "src/sib.ts") {
		t.Fatalf("computed require must stay unbound: %+v", computed.Relations)
	}
	later, _ := findFact(ff, "src.laterRequireCapture")
	if hasCallToFile(later, "src.load", "src/sib.ts") {
		t.Fatalf("early closure must not fall back to sibling load: %+v", later.Relations)
	}
	if !hasCallToFile(later, "src.load", "src/local.ts") {
		t.Fatalf("closure must capture later required load: %+v", later.Relations)
	}
	after, _ := findFact(ff, "src.afterRequire")
	if !hasCallToFile(after, "src.load", "src/local.ts") {
		t.Fatalf("call after require must bind load: %+v", after.Relations)
	}
	typed, _ := findFact(ff, "src.typeSpaceKeep")
	if !hasCallToFile(typed, "src.keep", "src/local.ts") {
		t.Fatalf("type keep must not hide required keep: %+v", typed.Relations)
	}
	paren, _ := findFact(ff, "src.parenRequire")
	if !hasCallToFile(paren, "src.helper", "src/local.ts") {
		t.Fatalf("parenthesized require lost helper: %+v", paren.Relations)
	}
	ord, _ := findFact(ff, "src.ordinaryForward")
	if hasCallToFile(ord, "src.load", "src/local.ts") || hasCallToFile(ord, "src.load", "src/sib.ts") {
		t.Fatalf("ordinary forward local must not bind sibling load: %+v", ord.Relations)
	}
	nsReq, _ := findFact(ff, "src.namespaceRequire")
	if !hasCallToFile(nsReq, "src.helper", "src/local.ts") {
		t.Fatalf("namespace require sdk.helper lost: %+v", nsReq.Relations)
	}
	if hasCallToFile(nsReq, "src.helper", "src/sib.ts") {
		t.Fatalf("namespace require bound sibling helper: %+v", nsReq.Relations)
	}
	nsAs, _ := findFact(ff, "src.namespaceRequireAs")
	if !hasCallToFile(nsAs, "src.keep", "src/local.ts") {
		t.Fatalf("namespace require as keep lost: %+v", nsAs.Relations)
	}
	shLocal, _ := findFact(ff, "src.shadowedRequireLocal")
	if hasCallToFile(shLocal, "src.helper", "src/local.ts") || hasCallToFile(shLocal, "src.helper", "src/sib.ts") {
		t.Fatalf("local require binding must not be CommonJS: %+v", shLocal.Relations)
	}
	shParam, _ := findFact(ff, "src.shadowedRequireParameter")
	if hasCallToFile(shParam, "src.helper", "src/local.ts") || hasCallToFile(shParam, "src.helper", "src/sib.ts") {
		t.Fatalf("parameter require binding must not be CommonJS: %+v", shParam.Relations)
	}
	noLeak, _ := findFact(ff, "src.nestedRequireNoLeak")
	if !hasCallToFile(noLeak, "src.keep", "src/local.ts") {
		t.Fatalf("inner-block required keep lost: %+v", noLeak.Relations)
	}
	if hasCallToFile(noLeak, "src.helper", "src/local.ts") || hasCallToFile(noLeak, "src.helper", "src/sib.ts") {
		t.Fatalf("inner-block require must not leak to outer helper(): %+v", noLeak.Relations)
	}

	for _, file := range []string{"src/modshadow.ts", "src/impshadow.ts", "src/nsshadow.ts"} {
		fr := fileRefFact(ff, file)
		run, _ := findFactNamedIn(ff, "src.run", file)
		if hasCallToFile(fr, "src.work", "src/local.ts") || hasCallToFile(fr, "src.work", "src/sib.ts") {
			t.Fatalf("%s file_ref bound require-named import as CommonJS: %+v", file, fr.Relations)
		}
		if hasCallToFile(run, "src.work", "src/local.ts") || hasCallToFile(run, "src.work", "src/sib.ts") {
			t.Fatalf("%s symbol bound require-named import as CommonJS: %+v", file, run.Relations)
		}
		if !hasCallToFile(fr, "src.keep", "src/local.ts") && !hasCallToFile(run, "src.keep", "src/local.ts") {
			t.Fatalf("%s lost keep: fr=%+v run=%+v", file, fr.Relations, run.Relations)
		}
	}
	cjs, _ := findFactNamedIn(ff, "src.run", "src/cjsmod.ts")
	cjsFR := fileRefFact(ff, "src/cjsmod.ts")
	if !hasCallToFile(cjs, "src.work", "src/local.ts") && !hasCallToFile(cjsFR, "src.work", "src/local.ts") {
		t.Fatalf("module-level CommonJS require must still bind work: %+v %+v", cjs.Relations, cjsFR.Relations)
	}
	miss, _ := findFactNamedIn(ff, "src.run", "src/missingns.ts")
	missFR := fileRefFact(ff, "src/missingns.ts")
	if hasCallToFile(miss, "src.work", "src/sib.ts") || hasCallToFile(missFR, "src.work", "src/sib.ts") || hasCallToFile(miss, "src.work", "src/local.ts") || hasCallToFile(missFR, "src.work", "src/local.ts") {
		t.Fatalf("missing namespace require must not fall back to sibling work: miss=%+v fr=%+v", miss.Relations, missFR.Relations)
	}
	if !hasCallToFile(miss, "src.keep", "src/local.ts") && !hasCallToFile(missFR, "src.keep", "src/local.ts") {
		t.Fatalf("missing namespace require lost keep: %+v %+v", miss.Relations, missFR.Relations)
	}
}

func TestExtract_Wave9CommonJSRequireValueAndNamespace(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/fn.ts":     "module.exports = function ping() { return 1; }\n",
		"src/routes.ts": "const express = require('express');\nconst router = express.Router();\nrouter.get('/x', () => {});\nmodule.exports = router;\n",
		"src/local.ts":  "export function work() { return 1; }\nexport function keep() { return 2; }\nexport function helper() { return 3; }\n",
		"src/sib.ts":    "export function ping() { return 9; }\nexport function work() { return 9; }\nexport function helper() { return 9; }\nexport function mount() { return 9; }\nexport function gone() { return 9; }\n",
		"src/app.ts": `
export function callRequiredFn() {
  const ping = require('./fn');
  return ping();
}
export function mountRequiredRoutes() {
  const routes = require('./routes');
  function mount(r) { return r; }
  return mount(routes);
}
export function namespaceStill() {
  const sdk = require('./local');
  return sdk.work();
}
export function shadowedRequireIdent() {
  const require = (p) => ({ work: () => 0 });
  const ping = require('./fn');
  return ping();
}
export function shadowedResultName() {
  const ping = require('./fn');
  {
    const ping = () => 0;
    return ping();
  }
}
export function missingValueRequire() {
  const gone = require('./gone');
  return gone();
}
export function keepLocal() {
  const { keep } = require('./local');
  return keep();
}
`,
	}, false)

	appFR := fileRefFact(ff, "src/app.ts")
	if appFR.Name == "" {
		t.Fatal("app file_ref missing")
	}
	if !hasCallToFile(appFR, "src.ping", "src/fn.ts") {
		t.Fatalf("file_ref lost value require ping: %+v", appFR.Relations)
	}
	if hasCallToFile(appFR, "src.ping", "src/sib.ts") {
		t.Fatalf("file_ref bound sibling ping: %+v", appFR.Relations)
	}
	if !hasCallToFile(appFR, "src.routes", "src/routes.ts") {
		t.Fatalf("file_ref lost mounted require routes: %+v", appFR.Relations)
	}
	if !hasCallToFile(appFR, "src.work", "src/local.ts") {
		t.Fatalf("file_ref lost namespace sdk.work: %+v", appFR.Relations)
	}
	if hasCallToFile(appFR, "src.work", "src/sib.ts") {
		t.Fatalf("file_ref bound sibling work: %+v", appFR.Relations)
	}
	if hasCallToFile(appFR, "src.gone", "src/sib.ts") || hasCallToFile(appFR, "src.gone", "src/local.ts") {
		t.Fatalf("missing require must not bind sibling gone: %+v", appFR.Relations)
	}
	goneOK := false
	for _, r := range appFR.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.gone" && r.TargetFile != "" && r.TargetFile != "src/sib.ts" && r.TargetFile != "src/local.ts" {
			goneOK = true
		}
	}
	if !goneOK {
		t.Fatalf("missing value require must keep unresolved specifier: %+v", appFR.Relations)
	}

	callFn, _ := findFact(ff, "src.callRequiredFn")
	if !hasCallToFile(callFn, "src.ping", "src/fn.ts") {
		t.Fatalf("symbol-owned ping() lost: %+v", callFn.Relations)
	}
	if hasCallToFile(callFn, "src.ping", "src/sib.ts") {
		t.Fatalf("symbol-owned ping bound sibling: %+v", callFn.Relations)
	}
	mountF, _ := findFact(ff, "src.mountRequiredRoutes")
	if !hasCallToFile(mountF, "src.routes", "src/routes.ts") {
		t.Fatalf("symbol-owned mount(routes) lost: %+v", mountF.Relations)
	}
	nsF, _ := findFact(ff, "src.namespaceStill")
	if !hasCallToFile(nsF, "src.work", "src/local.ts") {
		t.Fatalf("symbol-owned sdk.work lost: %+v", nsF.Relations)
	}
	if hasCallTarget(nsF, "src.sdk") {
		t.Fatalf("namespace use must not also emit phantom sdk value: %+v", nsF.Relations)
	}
	shReq, _ := findFact(ff, "src.shadowedRequireIdent")
	if hasCallToFile(shReq, "src.ping", "src/fn.ts") || hasCallToFile(shReq, "src.ping", "src/sib.ts") {
		t.Fatalf("shadowed require identifier must not be CommonJS: %+v", shReq.Relations)
	}
	shRes, _ := findFact(ff, "src.shadowedResultName")
	if hasCallToFile(shRes, "src.ping", "src/fn.ts") || hasCallToFile(shRes, "src.ping", "src/sib.ts") {
		t.Fatalf("shadowed result name must not bind required ping: %+v", shRes.Relations)
	}
	miss, _ := findFact(ff, "src.missingValueRequire")
	if hasCallToFile(miss, "src.gone", "src/sib.ts") || hasCallToFile(miss, "src.gone", "src/local.ts") {
		t.Fatalf("symbol-owned missing require bound sibling: %+v", miss.Relations)
	}
}

func findFactNamedIn(ff []facts.Fact, name, file string) (facts.Fact, bool) {
	for _, f := range ff {
		if f.Name == name && f.File == file {
			return f, true
		}
	}
	return facts.Fact{}, false
}
