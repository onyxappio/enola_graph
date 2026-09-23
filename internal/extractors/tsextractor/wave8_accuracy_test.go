package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_VueComponentDistinctFromSameNamedType(t *testing.T) {
	ff := extractVue(t, map[string]string{
		"src/Age.vue": `<script setup lang="ts">
type Age = { title: string; value: number }
const ageList: Age[] = []
function handle(age: Age) { return age.value }
</script>
<template><div /></template>
`,
		"src/Consumer.vue": `<script setup lang="ts">
import Age from './Age.vue'
</script>
<template><Age /></template>
`,
		"src/Widget.vue": `<script setup lang="ts">
type AgeOption = { n: number }
export function useWidget() { return 1 }
</script>
<template><div /></template>
`,
	}, false)
	var comps, types []facts.Fact
	for _, f := range ff {
		if f.Kind != facts.KindSymbol || f.File != "src/Age.vue" {
			continue
		}
		if f.Props["web_component"] == "component" {
			comps = append(comps, f)
		}
		if f.Props["symbol_kind"] == facts.SymbolType {
			types = append(types, f)
		}
	}
	if len(comps) != 1 {
		t.Fatalf("Age.vue components=%d", len(comps))
	}
	if comps[0].Props["symbol_kind"] != facts.SymbolFunc {
		t.Fatalf("component symbol_kind=%v", comps[0].Props["symbol_kind"])
	}
	if comps[0].Props["exported"] != true {
		t.Fatal("component must stay the SFC default export")
	}
	if comps[0].Name != "src.Age" {
		t.Fatalf("component name=%q want src.Age (value-space file identity)", comps[0].Name)
	}
	if len(types) != 1 {
		t.Fatalf("local type missing: %d", len(types))
	}
	if types[0].Name != "src.Age#type" {
		t.Fatalf("type-space identity=%q want src.Age#type", types[0].Name)
	}
	if types[0].Props["web_component"] == "component" {
		t.Fatal("type adopted component metadata")
	}
	if types[0].Name == comps[0].Name {
		t.Fatal("FactID collision: type and component share name")
	}
	widget, ok := findFact(ff, "src.Widget")
	if !ok || widget.Props["web_component"] != "component" {
		t.Fatal("unrelated SFC lost component identity")
	}
	if _, ok := findFact(ff, "src.AgeOption"); !ok {
		t.Fatal("non-colliding type must keep its original name")
	}
	consumer, ok := findFact(ff, "src.Consumer")
	if !ok {
		t.Fatal("Consumer missing")
	}
	found := false
	for _, r := range consumer.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.Age" && r.TargetFile == "src/Age.vue" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default consumer must target the runtime component: %+v", consumer.Relations)
	}
}

func TestExtract_SameFileLexicalBindingHasTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/Quiz.ts": `
export function useStep() { return { nextDelayed: (s: string) => s } }
const { nextDelayed } = useStep()
export function handleNextClick() { nextDelayed('start') }
const emit = (e: string) => e
export function onDrag() { emit('move') }
`,
		"src/Feature.ts": `
export function useStep() { return { nextDelayed: (s: string) => s } }
const { nextDelayed } = useStep()
export function handleNextClick() { nextDelayed('other') }
const emit = (e: string) => e
export function onDrag() { emit('other') }
`,
		"src/shadow.ts": `
const nextDelayed = (s: string) => s
export function handleNextClick(nextDelayed: (s: string) => string) { nextDelayed('param') }
export function localShadow() { const emit = () => {}; emit() }
`,
	}, false)
	handle, _ := findFact(ff, "src.handleNextClick")
	if handle.File != "src/Quiz.ts" {
		for _, f := range ff {
			if f.Name == "src.handleNextClick" && f.File == "src/Quiz.ts" {
				handle = f
			}
		}
	}
	found := false
	for _, r := range handle.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.nextDelayed" {
			found = true
			if r.TargetFile != "src/Quiz.ts" {
				t.Fatalf("destructured call TargetFile=%q want owner file: %+v", r.TargetFile, handle.Relations)
			}
		}
	}
	if !found {
		t.Fatalf("missing nextDelayed call: %+v", handle.Relations)
	}
	var quizEmit facts.Fact
	for _, f := range ff {
		if f.Name == "src.onDrag" && f.File == "src/Quiz.ts" {
			quizEmit = f
		}
	}
	found = false
	for _, r := range quizEmit.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.emit" {
			found = true
			if r.TargetFile != "src/Quiz.ts" {
				t.Fatalf("plain const call TargetFile=%q", r.TargetFile)
			}
		}
	}
	if !found {
		t.Fatalf("missing emit call: %+v", quizEmit.Relations)
	}
	var param facts.Fact
	for _, f := range ff {
		if f.Name == "src.handleNextClick" && f.File == "src/shadow.ts" {
			param = f
		}
	}
	for _, r := range param.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.nextDelayed" && r.TargetFile == "src/shadow.ts" {
			t.Fatalf("parameter shadow bound file-scope: %+v", param.Relations)
		}
	}
}

func TestExtract_JSXFileRefKeepsImportedTargetFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/LoaderScreen.tsx": `import { LoaderLogo } from './LoaderLogo'
export function LoaderScreen() { return <LoaderLogo /> }
`,
		"src/LoaderLogo.tsx":     `export function LoaderLogo() { return null }`,
		"src/LoaderLogo.web.tsx": `export function LoaderLogo() { return null }`,
		"src/Hex.tsx": `export function HexBase() { return null }
export function Node() { return <HexBase /> }
`,
		"src/HexOther.tsx": `export function HexBase() { return null }`,
		"src/native.tsx":   `export function div() { return <div /> }`,
	}, false)
	var refs []facts.Relation
	for _, f := range ff {
		if f.Kind == facts.KindFileRef && f.File == "src/LoaderScreen.tsx" {
			refs = f.Relations
		}
	}
	resolved, ambiguous := 0, 0
	for _, r := range refs {
		if r.Kind != facts.RelCalls || r.Target != "src.LoaderLogo" {
			continue
		}
		if r.TargetFile == "src/LoaderLogo.tsx" {
			resolved++
		}
		if r.TargetFile == "" {
			ambiguous++
		}
		if r.TargetFile == "src/LoaderLogo.web.tsx" {
			t.Fatalf("JSX file_ref bound the unimported platform sibling: %+v", refs)
		}
	}
	if resolved != 1 {
		t.Fatalf("want one imported LoaderLogo file_ref, got resolved=%d refs=%+v", resolved, refs)
	}
	if ambiguous != 0 {
		t.Fatalf("extra ambiguous JSX file_ref: %+v", refs)
	}
	screen, _ := findFact(ff, "src.LoaderScreen")
	ok := false
	for _, r := range screen.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.LoaderLogo" && r.TargetFile == "src/LoaderLogo.tsx" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("function-scope JSX lost TargetFile: %+v", screen.Relations)
	}
	node, _ := findFact(ff, "src.Node")
	ok = false
	for _, r := range node.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.HexBase" && r.TargetFile == "src/Hex.tsx" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("same-file JSX call TargetFile missing: %+v", node.Relations)
	}
	native, _ := findFact(ff, "src.div")
	for _, r := range native.Relations {
		if r.Kind == facts.RelCalls && r.Target == "src.div" {
			t.Fatalf("intrinsic tag emitted a call: %+v", native.Relations)
		}
	}
}

func TestExtract_AwaitImportDestructureLexicalCalls(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/caller.ts": `export async function first() {
  const { work } = await import('./deps/a');
  work();
}
export async function second() {
  const { work } = await import('./deps/b');
  work();
}
export async function aliased() {
  const { work: renamed } = await import('./deps/a');
  renamed();
}
export async function enclosing() {
  const { work } = await import('./deps/b');
  function nested() { work(); }
  nested();
}
export function parameter(work: () => void) { work(); }
export function localShadow() { const work = () => {}; work(); }
export function outsider() { work(); }
export async function nonliteral(path: string) {
  const { work } = await import(path);
  work();
}
`,
		"src/deps/a.ts": "export function work() { return 'a' }\n",
		"src/deps/b.ts": "export function work() { return 'b' }\n",
	}, false)
	want := map[string]string{
		"src.first":     "src/deps/a.ts",
		"src.second":    "src/deps/b.ts",
		"src.aliased":   "src/deps/a.ts",
		"src.enclosing": "src/deps/b.ts",
	}
	for name, file := range want {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		found := false
		for _, r := range f.Relations {
			if r.Kind == facts.RelCalls && r.Target == "src/deps.work" && r.TargetFile == file {
				found = true
			}
			if r.Kind == facts.RelCalls && r.TargetFile != "" && r.TargetFile != file && (r.TargetFile == "src/deps/a.ts" || r.TargetFile == "src/deps/b.ts") {
				t.Fatalf("%s bound the other module: %+v", name, f.Relations)
			}
		}
		if !found {
			t.Fatalf("%s missing imported work call: %+v", name, f.Relations)
		}
	}
	for _, name := range []string{"src.parameter", "src.localShadow", "src.outsider", "src.nonliteral"} {
		f, _ := findFact(ff, name)
		for _, r := range f.Relations {
			if r.Kind == facts.RelCalls && (r.TargetFile == "src/deps/a.ts" || r.TargetFile == "src/deps/b.ts") {
				t.Fatalf("%s leaked a dynamic import binding: %+v", name, f.Relations)
			}
		}
	}
	caller, _ := findFact(ff, "src.caller.ts")
	_ = caller
	var depEdge bool
	for _, f := range ff {
		if f.Kind == facts.KindDependency && f.File == "src/caller.ts" {
			for _, r := range f.Relations {
				if r.Kind == facts.RelImports {
					depEdge = true
				}
			}
		}
	}
	if !depEdge {
		for _, f := range ff {
			if f.File == "src/caller.ts" {
				for _, r := range f.Relations {
					if r.Kind == facts.RelImports {
						depEdge = true
					}
				}
			}
		}
	}
	if !depEdge {
		t.Fatalf("wave1 dependency imports edge missing")
	}
}

func TestExtract_ExtensionlessDeclarationOnlyModule(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/generator.ts": `import type { TsLibGeneratorSchema } from './schema'
export function run(s: TsLibGeneratorSchema) { return s }
`,
		"src/schema.d.ts": `export interface TsLibGeneratorSchema { name: string }
`,
		"src/schema.json": `{"type":"object"}`,
		"src/both.ts": `import type { Dual } from './dual'
export function useDual(s: Dual) { return s }
`,
		"src/dual.ts":   `export interface Dual { n: number }
`,
		"src/dual.d.ts": `export interface Dual { n: number }
`,
		"src/json.ts": `import data from './schema.json'
export function useJSON() { return data }
`,
		"src/missing.ts": `import type { Ghost } from './ghost'
export function useGhost(s: Ghost) { return s }
`,
	}, false)
	found := false
	for _, f := range ff {
		if f.File != "src/generator.ts" {
			continue
		}
		for _, r := range f.Relations {
			if r.Kind == facts.RelCalls && r.Target == "src.TsLibGeneratorSchema" {
				found = true
				if r.TargetFile != "src/schema.d.ts" {
					t.Fatalf("declaration fallback TargetFile=%q on %s %s rels=%+v", r.TargetFile, f.Kind, f.Name, f.Relations)
				}
			}
		}
	}
	if !found {
		t.Fatalf("type reference unresolved in generator facts: %v", factNames(ff))
	}
	ok := false
	for _, f := range ff {
		if f.File != "src/both.ts" {
			continue
		}
		for _, r := range f.Relations {
			if r.Kind == facts.RelCalls && r.Target == "src.Dual" && r.TargetFile == "src/dual.ts" {
				ok = true
			}
			if r.Kind == facts.RelCalls && r.TargetFile == "src/dual.d.ts" {
				t.Fatalf("implementation must win over .d.ts: %+v", f.Relations)
			}
		}
	}
	if !ok {
		t.Fatalf("implementation Dual missing: %v", factNames(ff))
	}
	for _, f := range ff {
		if f.File != "src/json.ts" {
			continue
		}
		for _, r := range f.Relations {
			if r.TargetFile == "src/schema.d.ts" {
				t.Fatalf("explicit json import fell back to declaration: %+v", f.Relations)
			}
		}
	}
	for _, f := range ff {
		if f.File != "src/missing.ts" {
			continue
		}
		for _, r := range f.Relations {
			if r.Kind == facts.RelCalls && r.TargetFile == "src/ghost.d.ts" {
				t.Fatalf("missing module invented a declaration: %+v", f.Relations)
			}
		}
	}
}
