package tsextractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/factpath"
	"github.com/enola-labs/enola/internal/facts"
)

func wave14Blob(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "wave14", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func wave14FileRef(ff []facts.Fact, file string) *facts.Fact {
	for i := range ff {
		if ff[i].Kind == facts.KindFileRef && ff[i].File == file {
			return &ff[i]
		}
	}
	return nil
}

func wave14HasCall(f *facts.Fact, target, file string) bool {
	if f == nil {
		return false
	}
	for _, rel := range f.Relations {
		if rel.Kind == facts.RelCalls && rel.Target == target && (file == "" || rel.TargetFile == file) {
			return true
		}
	}
	return false
}

func wave14Symbols(ff []facts.Fact, file, suffix string) []facts.Fact {
	var out []facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && (file == "" || f.File == file) && strings.HasSuffix(f.Name, "."+suffix) {
			out = append(out, f)
		}
	}
	return out
}

func wave14FactsJSON(t *testing.T, ff []facts.Fact) []byte {
	t.Helper()
	copyFacts := append([]facts.Fact(nil), ff...)
	sort.Slice(copyFacts, func(i, j int) bool {
		a, b := copyFacts[i], copyFacts[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Line < b.Line
	})
	data, err := json.Marshal(copyFacts)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wave14AssertSessionColdParity(t *testing.T, root string, files []string, records map[string]*FileRecord, dirty map[string]bool) *SessionResult {
	t.Helper()
	delta, err := New().ExtractSession(context.Background(), root, files, records, dirty, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	cold, err := New().ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := wave14FactsJSON(t, delta.Facts), wave14FactsJSON(t, cold.Facts); !reflect.DeepEqual(got, want) {
		t.Fatal("delta facts differ from a fresh cold extraction")
	}
	return delta
}

func wave14AssertNoChange(t *testing.T, root string, files []string, records map[string]*FileRecord) {
	t.Helper()
	unchanged, err := New().ExtractSession(context.Background(), root, files, records, map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Stats.FilesParsed != 0 {
		t.Fatalf("unchanged session parsed %d files, want 0", unchanged.Stats.FilesParsed)
	}
}

func TestExtract_Wave14AmbientNamesDoNotBindSiblingModules(t *testing.T) {
	files := map[string]string{
		"packages/utils/vite.config.ts":                         wave14Blob(t, "q/packages/utils/vite.config.ts"),
		"packages/utils/vitest.config.ts":                       wave14Blob(t, "q/packages/utils/vitest.config.ts"),
		"packages/shared-lands-components/vite.config.ts":       wave14Blob(t, "q/packages/shared-lands-components/vite.config.ts"),
		"packages/shared-lands-components/vitest.config.ts":     wave14Blob(t, "q/packages/shared-lands-components/vitest.config.ts"),
		"apps/mobile/scripts/_t191-cursor1b-etalon-metrics.mjs": wave14Blob(t, "q/apps/mobile/scripts/_t191-cursor1b-etalon-metrics.mjs"),
		"apps/mobile/scripts/compare-screenshots.mjs":           wave14Blob(t, "q/apps/mobile/scripts/compare-screenshots.mjs"),
		"apps/mobile/scripts/deny-figma-network.cjs":            wave14Blob(t, "q/apps/mobile/scripts/deny-figma-network.cjs"),
		"apps/mobile/scripts/ensure-splash-storyboard-shape.js": wave14Blob(t, "q/apps/mobile/scripts/ensure-splash-storyboard-shape.js"),
		"src/consumer.ts": `import { run as importedRun } from './real'
function own() {}
export function consumer() {
  own()
  importedRun()
  onlySibling()
  require('some-package')
  resolve(__dirname)
  callLater()
  {
    const callLater = () => {}
    callLater()
  }
  callLater()
  declaredLater()
}
function callLater() {}
function declaredLater() {}
`,
		"src/real.ts": `export function run() {}`,
		"src/sibling.ts": `export function onlySibling() {}
export function require() {}
export function __dirname() {}`,
	}
	ff := extractAll(t, files, false)

	for _, tc := range []struct {
		file string
		bad  []string
	}{
		{"packages/utils/vite.config.ts", []string{"packages/utils.__dirname"}},
		{"packages/shared-lands-components/vitest.config.ts", []string{"packages/shared-lands-components.__dirname"}},
		{"apps/mobile/scripts/deny-figma-network.cjs", []string{"apps/mobile/scripts.require"}},
		{"apps/mobile/scripts/ensure-splash-storyboard-shape.js", []string{"apps/mobile/scripts.require", "apps/mobile/scripts.__dirname"}},
		{"src/consumer.ts", []string{"src.onlySibling", "src.require", "src.__dirname"}},
	} {
		ref := wave14FileRef(ff, tc.file)
		for _, bad := range tc.bad {
			if wave14HasCall(ref, bad, "") {
				t.Errorf("%s gained unimported sibling reference %s: %+v", tc.file, bad, ref.Relations)
			}
		}
	}
	consumer := wave14Symbols(ff, "src/consumer.ts", "consumer")
	if len(consumer) != 1 {
		t.Fatalf("consumer function facts=%v, want exactly one", consumer)
	}
	if !wave14HasCall(&consumer[0], "src.own", "src/consumer.ts") {
		t.Errorf("same-file local call was lost: %+v", consumer[0].Relations)
	}
	for _, local := range []string{"callLater", "declaredLater"} {
		if !wave14HasCall(&consumer[0], "src."+local, "src/consumer.ts") {
			t.Errorf("same-file local call to %s (including late/block-scoped lookup) was lost: %+v", local, consumer[0].Relations)
		}
	}
	if !wave14HasCall(&consumer[0], "src.run", "src/real.ts") {
		t.Errorf("imported alias did not resolve to its source symbol: %+v", consumer[0].Relations)
	}
	for _, name := range []string{"onlySibling", "require", "__dirname"} {
		if wave14HasCall(&consumer[0], "src."+name, "src/sibling.ts") {
			t.Errorf("unbound name %s bound to a sibling module: %+v", name, consumer[0].Relations)
		}
	}
	ref := wave14FileRef(ff, "src/consumer.ts")
	if !wave14HasCall(ref, "src.own", "src/consumer.ts") || !wave14HasCall(ref, "src.run", "src/real.ts") {
		t.Errorf("file_ref lost local/import bindings: %+v", ref)
	}
}

func TestExtract_Wave14ScriptModeGlobalsRemainExplicit(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/consumer.ts": `globalHelper()`,
		"src/globals.ts":  `function globalHelper() {}`,
	}, false)
	ref := wave14FileRef(ff, "src/consumer.ts")
	if !wave14HasCall(ref, "src.globalHelper", "") {
		t.Fatalf("classic TypeScript script-mode global was not retained: %+v", ref)
	}
}

func TestExtractSession_Wave14AmbientSiblingLifecycleEqualsCold(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	consumer := `import { run as importedRun } from './actual'
function own() {}
export function useNames() { own(); importedRun(); ambientName() }
useNames()
`
	actual := `export function run() {}`
	sibling := `function ambientName() {}`
	write("package.json", `{"name":"wave14-q"}`)
	write("src/consumer.ts", consumer)
	write("src/actual.ts", actual)
	write("src/sibling.ts", sibling)
	files := []string{"package.json", "src/actual.ts", "src/consumer.ts", "src/sibling.ts"}
	sort.Strings(files)
	state := wave14AssertSessionColdParity(t, root, files, nil, nil)
	wave14AssertNoChange(t, root, files, state.Records)
	assertNoAmbient := func(result *SessionResult) {
		t.Helper()
		ref := wave14FileRef(result.Facts, "src/consumer.ts")
		if wave14HasCall(ref, "src.ambientName", "") {
			t.Fatalf("unbound ambientName resolved to a sibling: %+v", ref.Relations)
		}
		use := wave14Symbols(result.Facts, "src/consumer.ts", "useNames")
		if len(use) != 1 || !wave14HasCall(&use[0], "src.own", "src/consumer.ts") || !wave14HasCall(&use[0], "src.run", "src/actual.ts") {
			t.Fatalf("own/imported bindings changed: %+v", use)
		}
		if wave14HasCall(&use[0], "src.ambientName", "src/sibling.ts") {
			t.Fatalf("unbound ambientName bound to sibling source: %+v", use[0].Relations)
		}
	}
	assertNoAmbient(state)

	write("src/sibling.ts", `export function ambientName() {}`)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/sibling.ts": true})
	wave14AssertNoChange(t, root, files, state.Records)
	assertNoAmbient(state)

	write("src/sibling.ts", `export function renamedAmbient() {}`)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/sibling.ts": true})
	wave14AssertNoChange(t, root, files, state.Records)
	assertNoAmbient(state)

	if err := os.Remove(filepath.Join(root, "src/sibling.ts")); err != nil {
		t.Fatal(err)
	}
	files = []string{"package.json", "src/actual.ts", "src/consumer.ts"}
	sort.Strings(files)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/sibling.ts": true})
	wave14AssertNoChange(t, root, files, state.Records)
	assertNoAmbient(state)

	write("src/sibling.ts", sibling)
	files = append(files, "src/sibling.ts")
	sort.Strings(files)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/sibling.ts": true})
	wave14AssertNoChange(t, root, files, state.Records)
	assertNoAmbient(state)
}

func TestExtract_Wave14VueMacroNamesHandleNewlinesCommentsAndTypes(t *testing.T) {
	files := map[string]string{
		"apps/landings/land-263/components/QuizLayout.vue":                               wave14Blob(t, "v/apps/landings/land-263/components/QuizLayout.vue"),
		"apps/landings/land-test9/components/step/databreach-v1/DownloadLoginScreen.vue": wave14Blob(t, "v/apps/landings/land-test9/components/step/databreach-v1/DownloadLoginScreen.vue"),
		"apps/landings/land-test9/components/step/databreach-v1/ReviewsCarousel.vue":     wave14Blob(t, "v/apps/landings/land-test9/components/step/databreach-v1/ReviewsCarousel.vue"),
		"src/Contracts.vue": `<script setup lang="ts">
interface Props {
  title: string
  /** this comment is not a prop named fakeMember */
  'data-id'?: string
  nested: { inside: string }
  continuation: Promise<
    number
  >
  union:
    | 'one'
    | 'two'
  callback: (event: { value: string }) => void
}
const props = withDefaults(defineProps<Props>(), { title: 'ok' })
defineEmits<{
  save: [value: string]
  retry: []
}>()
defineSlots<{
  default(props: { item: { id: string } }): unknown
  named(): unknown
}>()
function focus() {}
function reset() {}
defineExpose({
  /** exposition doc must not become a name */
  focus,
  reset
})
</script><template><div>{{ props.title }}</div></template>`,
		"src/Runtime.vue": `<script setup lang="ts">
defineProps({ title: String, nested: { value: Number } })
defineEmits(['save', 'cancel'])
</script><template><div /></template>`,
	}
	ff := extractVue(t, files, false)
	for _, tc := range []struct {
		name string
		prop string
		want []string
	}{
		{"QuizLayout", "vue_prop_names", []string{"progressCurrent", "progressTotal", "title"}},
		{"DownloadLoginScreen", "vue_emit_names", []string{"copyCredential", "download", "openApp", "resendClaim", "retry", "support"}},
		{"ReviewsCarousel", "vue_prop_names", []string{"activeIndex", "loop", "slideMs"}},
		{"Contracts", "vue_prop_names", []string{"callback", "continuation", "data-id", "nested", "title", "union"}},
		{"Contracts", "vue_emit_names", []string{"retry", "save"}},
		{"Contracts", "vue_slot_names", []string{"default", "named"}},
		{"Contracts", "vue_exposed_names", []string{"focus", "reset"}},
		{"Runtime", "vue_prop_names", []string{"nested", "title"}},
		{"Runtime", "vue_emit_names", []string{"cancel", "save"}},
	} {
		matches := wave14Symbols(ff, "", tc.name)
		if len(matches) != 1 {
			t.Fatalf("component %s facts=%v, want one", tc.name, matches)
		}
		got, _ := matches[0].Props[tc.prop].([]string)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %s=%v, want %v", tc.name, tc.prop, got, tc.want)
		}
	}
}

func TestExtract_Wave14RootControlBlocksKeepLexicalEntitiesAndCalls(t *testing.T) {
	mainPath := "apps/marketing-web/src/main.ts"
	apiPath := "apps/marketing-web/src/api.ts"
	ff := extractAll(t, map[string]string{
		mainPath: wave14Blob(t, "b/apps/marketing-web/src/main.ts"),
		apiPath:  wave14Blob(t, "b/apps/marketing-web/src/api.ts"),
	}, false)
	wantFunctions := []string{"createStripeEmbeddedCheckout", "delay", "getCheckoutIdempotencyKey", "loadPaymentOffer", "loadStripeEmbeddedCheckoutScript", "mountStripeEmbeddedCheckout", "pollPaymentStatus", "pollScan", "refreshMountedPaymentStatus", "refreshPaymentStatus", "renderOffer", "renderPanel", "renderPaymentStatus", "renderStatus", "setCheckoutDisabled", "validateStripeHostedCheckoutUrl"}
	wantVariables := []string{"activeCheckoutAttempt", "activeMountedPaymentForm", "checkoutEmail", "checkoutForm", "fSessionId", "form", "isCheckoutSubmitting", "offerResponse", "paymentOfferPanel", "paymentStatusPanel", "placeholders", "statusPanel", "stripeEmbeddedCheckoutScriptPromise", "stripeEmbeddedCheckoutScriptUrl", "subject", "subjectType"}
	var foundFunctions, foundVariables []facts.Fact
	for _, f := range ff {
		if f.Kind != facts.KindSymbol || f.File != mainPath || f.Line < 417 || f.Line > 752 {
			continue
		}
		if f.Props["exported"] != false {
			t.Errorf("module-block symbol %s exported=%v, want false", f.Name, f.Props["exported"])
		}
		switch f.Props["symbol_kind"] {
		case facts.SymbolFunc:
			foundFunctions = append(foundFunctions, f)
		case facts.SymbolVariable:
			foundVariables = append(foundVariables, f)
		}
	}
	assertShortNames := func(label string, got []facts.Fact, want []string) {
		t.Helper()
		gotNames := make([]string, 0, len(got))
		for _, f := range got {
			gotNames = append(gotNames, f.Name[strings.LastIndexByte(f.Name, '.')+1:])
		}
		sort.Strings(gotNames)
		sort.Strings(want)
		if !reflect.DeepEqual(gotNames, want) {
			t.Errorf("%s=%v, want %v", label, gotNames, want)
		}
		identities := make(map[string]bool)
		for _, f := range got {
			if identities[f.Name] {
				t.Errorf("duplicate block identity %s", f.Name)
			}
			identities[f.Name] = true
		}
	}
	assertShortNames("module-block functions", foundFunctions, wantFunctions)
	assertShortNames("module-block variables", foundVariables, wantVariables)
	byLine := func(line int, short string) *facts.Fact {
		for i := range ff {
			if ff[i].Kind == facts.KindSymbol && ff[i].File == mainPath && ff[i].Line == line && strings.HasSuffix(ff[i].Name, "."+short) {
				return &ff[i]
			}
		}
		return nil
	}
	renderStatusMatches, renderPanelMatches := wave14Symbols(ff, mainPath, "renderStatus"), wave14Symbols(ff, mainPath, "renderPanel")
	var renderStatus, renderPanel *facts.Fact
	if len(renderStatusMatches) == 1 {
		renderStatus = &renderStatusMatches[0]
	}
	if len(renderPanelMatches) == 1 {
		renderPanel = &renderPanelMatches[0]
	}
	if renderStatus == nil || renderPanel == nil || !wave14HasCall(renderStatus, renderPanel.Name, mainPath) {
		t.Fatalf("renderStatus -> renderPanel missing; renderStatus=%+v renderPanel=%+v", renderStatus, renderPanel)
	}
	pollScan, delay := byLine(482, "pollScan"), byLine(747, "delay")
	if pollScan == nil || delay == nil || !wave14HasCall(pollScan, delay.Name, mainPath) {
		t.Fatalf("pollScan -> delay missing; pollScan=%+v delay=%+v", pollScan, delay)
	}
	if !wave14HasCall(pollScan, "apps/marketing-web/src.getScanStatus", apiPath) {
		t.Errorf("pollScan -> imported getScanStatus missing: %+v", pollScan.Relations)
	}
	ref := wave14FileRef(ff, mainPath)
	loadPaymentOffer := wave14Symbols(ff, mainPath, "loadPaymentOffer")
	if ref == nil || len(loadPaymentOffer) != 1 || !wave14HasCall(ref, loadPaymentOffer[0].Name, mainPath) {
		t.Fatalf("module-level loadPaymentOffer call missing: file_ref=%+v symbol=%v", ref, loadPaymentOffer)
	}

	controls := extractAll(t, map[string]string{
		"src/blocks.ts": `export {}
if (ready) {
  function same() { leftOnly() }
  function leftOnly() {}
  const value = 1
  same()
} else {
  function same() { rightOnly() }
  function rightOnly() {}
  const value = 2
  same()
}
try {
  function recover() { fromTry() }
  function fromTry() {}
  const state = 3
  recover()
} catch (error) {
  function recover() { fromCatch() }
  function fromCatch() {}
  const state = 4
  recover()
}
same()
recover()
value
state`,
		"src/unrelated.ts": `export function same() {}`,
		"src/outside.ts": `export {}
same()
recover()
value
state`,
	}, false)
	for _, short := range []string{"same", "value", "recover", "state"} {
		matches := wave14Symbols(controls, "src/blocks.ts", short)
		if len(matches) != 2 {
			t.Errorf("sibling block declarations %s=%v, want two distinct symbols", short, matches)
		}
		if len(matches) == 2 && matches[0].Name == matches[1].Name {
			t.Errorf("sibling scopes shared %s identity %q", short, matches[0].Name)
		}
	}
	for _, tc := range []struct{ caller, callee string }{{"same", "leftOnly"}, {"same", "rightOnly"}, {"recover", "fromTry"}, {"recover", "fromCatch"}} {
		callers, callees := wave14Symbols(controls, "src/blocks.ts", tc.caller), wave14Symbols(controls, "src/blocks.ts", tc.callee)
		found := false
		for _, caller := range callers {
			for _, callee := range callees {
				if wave14HasCall(&caller, callee.Name, "src/blocks.ts") {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("direct block call %s -> %s missing; callers=%v callees=%v", tc.caller, tc.callee, callers, callees)
		}
	}
	blockRef := wave14FileRef(controls, "src/blocks.ts")
	for _, unscoped := range []string{"src.same", "src.value", "src.recover", "src.state"} {
		if wave14HasCall(blockRef, unscoped, "") {
			t.Errorf("module-level call outside a block resolved to a block-local identity %s: %+v", unscoped, blockRef)
		}
	}
	outsideRef := wave14FileRef(controls, "src/outside.ts")
	for _, short := range []string{"same", "value", "recover", "state"} {
		for _, local := range wave14Symbols(controls, "src/blocks.ts", short) {
			if wave14HasCall(outsideRef, local.Name, "src/blocks.ts") {
				t.Errorf("out-of-block use leaked into file_ref target %s: %+v", local.Name, outsideRef)
			}
		}
	}
	if wave14HasCall(outsideRef, "src.same", "src/unrelated.ts") || wave14HasCall(outsideRef, "src.same", "") {
		t.Errorf("same-directory sibling leaked into module-block file_ref: %+v", outsideRef)
	}
}

func TestExtract_Wave14WrappedAnonymousDefaultsKeepEntities(t *testing.T) {
	imagePath := "apps/landings/land-test9/assets/imageMap.ts"
	files := map[string]string{
		imagePath: wave14Blob(t, "d/apps/landings/land-test9/assets/imageMap.ts"),
		"apps/landings/land-test9/components/ImageConsumer.ts": `import imageMap from '../assets/imageMap'
export const firstImage = imageMap['personal-details-data-web']`,
		"src/ObjectMap.ts":    `export default { answer: 42 } as const`,
		"src/SatisfiesMap.ts": `export default { answer: 42 } satisfies { answer: number }`,
		"src/NonNullMap.ts":   `export default ((({ answer: 42 } as const)!))`,
		"src/ArrayMap.ts":     `export default ([1, 2] as const)`,
		"src/CallMap.ts":      `export default defineComponent(() => null) satisfies object`,
		"src/FunctionMap.ts":  `export default (function () {}) as unknown as (() => void)`,
		"src/ClassMap.ts":     `export default (class {}) satisfies new () => object`,
		"src/NamedMap.ts": `const localMap = { answer: 42 } as const
export default localMap`,
	}
	ff := extractAll(t, files, false)
	wantKinds := map[string]string{
		imagePath: facts.SymbolVariable, "src/ObjectMap.ts": facts.SymbolVariable, "src/SatisfiesMap.ts": facts.SymbolVariable,
		"src/NonNullMap.ts": facts.SymbolVariable, "src/ArrayMap.ts": facts.SymbolVariable, "src/CallMap.ts": facts.SymbolFunc,
		"src/FunctionMap.ts": facts.SymbolFunc, "src/ClassMap.ts": facts.SymbolClass,
	}
	for path, wantKind := range wantKinds {
		var entities []facts.Fact
		for _, f := range ff {
			if f.Kind == facts.KindSymbol && f.File == path && f.Props["exported"] == true {
				entities = append(entities, f)
			}
		}
		if len(entities) != 1 {
			t.Errorf("anonymous default %s exported entities=%v, want one", path, entities)
			continue
		}
		if entities[0].Props["symbol_kind"] != wantKind {
			t.Errorf("anonymous default %s kind=%v, want %s", path, entities[0].Props["symbol_kind"], wantKind)
		}
		wantName := factpath.Dir(path) + "." + fileSymbolName(path)
		if entities[0].Name != wantName {
			t.Errorf("anonymous default %s name=%s, want %s", path, entities[0].Name, wantName)
		}
	}
	var named []facts.Fact
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.File == "src/NamedMap.ts" {
			named = append(named, f)
		}
	}
	if len(named) != 1 || named[0].Name != "src.localMap" || named[0].Props["exported"] != true {
		t.Errorf("named-local default export duplicated or lost identity: %+v", named)
	}
	defaultNode := wave14Symbols(ff, imagePath, "ImageMap")
	if len(defaultNode) != 1 || defaultNode[0].Props["symbol_kind"] != facts.SymbolVariable || defaultNode[0].Props["exported"] != true {
		t.Fatalf("real imageMap default entity=%v", defaultNode)
	}
	ref := wave14FileRef(ff, "apps/landings/land-test9/components/ImageConsumer.ts")
	if !wave14HasCall(ref, defaultNode[0].Name, imagePath) {
		t.Fatalf("relative default import did not resolve to the default entity: %+v", ref)
	}
}

func TestExtract_Wave14ModuleAndCallbackNewLocalClassesAreFileRefs(t *testing.T) {
	productFile := "scripts/deploy/fixtures/t013-pulumi-handoff/index.mjs"
	landingsFile := "packages/onyx-funnel-checkout/src/declineReason.spec.ts"
	ff := extractAll(t, map[string]string{
		productFile:  wave14Blob(t, "n/product/scripts/deploy/fixtures/t013-pulumi-handoff/index.mjs"),
		landingsFile: wave14Blob(t, "n/landings/packages/onyx-funnel-checkout/src/declineReason.spec.ts"),
		"packages/onyx-funnel-checkout/src/declineReason.ts": wave14Blob(t, "n/landings/packages/onyx-funnel-checkout/src/declineReason.ts"),
	}, false)
	for _, tc := range []struct{ file, target string }{
		{productFile, "scripts/deploy/fixtures/t013-pulumi-handoff.RetentionResource"},
		{landingsFile, "packages/onyx-funnel-checkout/src.FakeSdkStripeError"},
		{landingsFile, "packages/onyx-funnel-checkout/src.FakeWrapperError"},
	} {
		class := wave14Symbols(ff, tc.file, tc.target[strings.LastIndexByte(tc.target, '.')+1:])
		if len(class) != 1 || class[0].Props["symbol_kind"] != facts.SymbolClass {
			t.Fatalf("expected existing class node %s in %s; got %v", tc.target, tc.file, class)
		}
		if !wave14HasCall(wave14FileRef(ff, tc.file), class[0].Name, tc.file) {
			t.Errorf("file_ref did not call same-file class %s: %+v", class[0].Name, wave14FileRef(ff, tc.file))
		}
	}
	if ref := wave14FileRef(ff, landingsFile); ref == nil || !wave14HasCall(ref, "packages/onyx-funnel-checkout/src.isTerminalConfirmFailure", "packages/onyx-funnel-checkout/src/declineReason.ts") {
		t.Errorf("explicit imported-class/file ref control failed: %+v", ref)
	}
	for _, f := range ff {
		if f.Kind == facts.KindSymbol && f.Name == "scripts/deploy/fixtures/t013-pulumi-handoff.RetentionResource.constructor" && !hasRelation(f, facts.RelInstantiates, "scripts/deploy/fixtures/t013-pulumi-handoff.RetentionProvider") {
			t.Errorf("existing in-function instantiates relation changed: %+v", f.Relations)
		}
	}
}

func TestExtract_Wave14NewConstructorShadowAndUnknownDoNotBindSibling(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/consumer.ts": `import { RemoteClass as Alias } from './sibling'
export {}
class LocalClass {}
new LocalClass()
new Alias()
new MissingClass()
function shadow(LocalClass: new () => object) { new LocalClass() }`,
		"src/sibling.ts": `export class RemoteClass {}
export class MissingClass {}`,
	}, false)
	ref := wave14FileRef(ff, "src/consumer.ts")
	locals := wave14Symbols(ff, "src/consumer.ts", "LocalClass")
	remotes := wave14Symbols(ff, "src/sibling.ts", "RemoteClass")
	if len(locals) != 1 || len(remotes) != 1 {
		t.Fatalf("class controls local=%v imported=%v", locals, remotes)
	}
	if !wave14HasCall(ref, locals[0].Name, "src/consumer.ts") || !wave14HasCall(ref, "src.RemoteClass", "src/sibling.ts") {
		t.Errorf("same-file or imported constructor reference missing: %+v", ref)
	}
	if wave14HasCall(ref, "src.MissingClass", "") || wave14HasCall(ref, "src.MissingClass", "src/sibling.ts") {
		t.Errorf("undeclared or parameter-shadowed constructor bound to a sibling: %+v", ref)
	}
	shadow := wave14Symbols(ff, "src/consumer.ts", "shadow")
	if len(shadow) != 1 || wave14HasCall(&shadow[0], "src.LocalClass", "") || wave14HasCall(&shadow[0], "src.LocalClass", "src/consumer.ts") {
		t.Errorf("constructor parameter shadow resolved to file-local class: %+v", shadow)
	}
}

func TestExtractSession_Wave14NewLocalClassMutationAndRestoreEqualsCold(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	consumer := `import { RemoteClass as Alias } from './sibling'
class LocalClass {}
new LocalClass()
new Alias()
new MissingClass()
`
	sibling := `export class RemoteClass {}
export class MissingClass {}`
	write("package.json", `{"name":"wave14-n"}`)
	write("src/consumer.ts", consumer)
	write("src/sibling.ts", sibling)
	files := []string{"package.json", "src/consumer.ts", "src/sibling.ts"}
	sort.Strings(files)
	state := wave14AssertSessionColdParity(t, root, files, nil, nil)
	assertCurrent := func(result *SessionResult, local, imported bool) {
		t.Helper()
		ref := wave14FileRef(result.Facts, "src/consumer.ts")
		if (wave14HasCall(ref, "src.LocalClass", "src/consumer.ts")) != local {
			t.Errorf("local class reference present=%v, want %v: %+v", wave14HasCall(ref, "src.LocalClass", "src/consumer.ts"), local, ref)
		}
		if (wave14HasCall(ref, "src.RemoteClass", "src/sibling.ts")) != imported {
			t.Errorf("imported class reference present=%v, want %v: %+v", wave14HasCall(ref, "src.RemoteClass", "src/sibling.ts"), imported, ref)
		}
		if wave14HasCall(ref, "src.MissingClass", "") || wave14HasCall(ref, "src.MissingClass", "src/sibling.ts") {
			t.Errorf("unimported MissingClass resolved: %+v", ref)
		}
	}
	assertCurrent(state, true, true)
	wave14AssertNoChange(t, root, files, state.Records)

	write("src/consumer.ts", strings.Replace(consumer, "class LocalClass {}", "class LocalClassRenamed {}", 1))
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/consumer.ts": true})
	assertCurrent(state, false, true)
	wave14AssertNoChange(t, root, files, state.Records)

	write("src/consumer.ts", consumer)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/consumer.ts": true})
	assertCurrent(state, true, true)
	wave14AssertNoChange(t, root, files, state.Records)

	unimported := `export {}
class LocalClass {}
new LocalClass()
new RemoteClass()
`
	write("src/consumer.ts", unimported)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/consumer.ts": true})
	assertCurrent(state, true, false)
	wave14AssertNoChange(t, root, files, state.Records)

	write("src/consumer.ts", consumer)
	state = wave14AssertSessionColdParity(t, root, files, state.Records, map[string]bool{"src/consumer.ts": true})
	assertCurrent(state, true, true)
	wave14AssertNoChange(t, root, files, state.Records)
}
