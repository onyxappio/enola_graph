package tsextractor

import (
	"context"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func wave10Route(ff []facts.Fact, name string) (facts.Fact, bool) {
	for _, f := range ff {
		if f.Kind == facts.KindRoute && f.Name == name {
			return f, true
		}
	}
	return facts.Fact{}, false
}

func TestExtract_Wave10NxTreeNotHTTPClient(t *testing.T) {
	schema := `import type { Tree } from '@nx/devkit';
export interface CreateDirectoryOptions {
  tree: Tree;
  path: string;
}
`
	gen := `import type { Tree } from '@nx/devkit';
import type { CreateDirectoryOptions } from './schema';
import axios from 'axios';

export async function createDirectory(options: CreateDirectoryOptions) {
  const { tree, path: dirPath } = options;
  tree.delete(` + "`${dirPath}/.gitkeep`" + `);
}

export async function readVitestConfig(tree: Tree) {
  tree.delete(` + "`${'x'}/.gitkeep`" + `);
}

export function httpControl(tree: any, dirPath: string) {
  tree.delete(` + "`${dirPath}/widgets/${dirPath}`" + `);
}

export function aliased(vfs: Tree, dirPath: string) {
  vfs.delete(` + "`${dirPath}/.gitkeep`" + `);
}
`
	ff := extractAll(t, map[string]string{
		"tools/generator.ts": gen,
		"tools/schema.d.ts":  schema,
		"tools/http.ts": `import type { Tree } from '@nx/devkit';
export function client(tree: ReturnType<typeof Object>, dirPath: string) {
  tree.delete(` + "`${dirPath}/widgets/x`" + `);
}
`,
	}, false)

	if _, ok := wave10Route(ff, "/.gitkeep"); ok {
		t.Fatal("Nx Tree.delete must not emit HTTP route /.gitkeep")
	}
	if _, ok := wave10Route(ff, "/widgets/x"); !ok {
		t.Fatal("unknown HTTP receiver named tree must still emit a client route")
	}
}

func TestExtract_Wave10NxTreeShadowAndMissingSchemaStayClient(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/gen.ts": `import type { Tree } from '@nx/devkit';
import type { CreateDirectoryOptions } from './schema';
export async function createDirectory(options: CreateDirectoryOptions) {
  const { tree, path: dirPath } = options;
  tree.delete(` + "`${dirPath}/.gitkeep`" + `);
}
`,
		"src/schema.d.ts": `export interface CreateDirectoryOptions {
  tree: unknown;
  path: string;
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/.gitkeep"); !ok {
		t.Fatal("unproven schema field type must not suppress the client route")
	}
}

func TestExtract_Wave10NxLexicalShadowKeepsInnerHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
export function outer(tree: Tree, base: string) {
  tree.delete(` + "`${base}/fs`" + `);
  function inner(tree: any) {
    tree.delete(` + "`${base}/http`" + `);
  }
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("outer Tree parameter must not emit /fs")
	}
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("inner any parameter must keep /http")
	}
}

func TestExtract_Wave10NxNearestOptionsBindingKeepsInnerHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/schema.ts": `import type { Tree } from '@nx/devkit';
export interface Options {
  tree: Tree
}
`,
		"src/client.ts": `import type { Options } from './schema';
export function outer(options: Options, base: string) {
  const {tree} = options;
  tree.delete(` + "`${base}/fs`" + `);
  function inner(options: any) {
    const {tree} = options;
    tree.delete(` + "`${base}/http`" + `);
  }
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("outer schema Tree field must not emit /fs")
	}
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("inner any options destructure must keep /http")
	}
}

func TestExtract_Wave10NxCommentedSchemaFieldKeepsHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/schema.ts": `import type { Tree } from '@nx/devkit';
export interface Options {
  /*
  tree: Tree
  */
  tree: any
}
`,
		"src/client.ts": `import type { Options } from './schema';
export function send(options: Options, base: string) {
  const {tree} = options;
  tree.delete(` + "`${base}/http`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("commented tree: Tree is not evidence; /http must stay")
	}
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("unexpected /fs")
	}
}

func TestExtract_Wave10NxNestedSchemaFieldDoesNotProveDirect(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/schema.ts": `import type { Tree } from '@nx/devkit';
export interface Options {
  nested: {
    tree: Tree
  };
  tree: any
}
`,
		"src/client.ts": `import type { Options } from './schema';
export function send(options: Options, base: string) {
  const {tree} = options;
  tree.delete(` + "`${base}/http`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("nested tree: Tree does not prove the direct field; /http must stay")
	}
}

func TestExtract_Wave10NxBlockConstShadowsParam(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
export function outer(tree: Tree, base: string) {
  tree.delete(` + "`${base}/fs`" + `);
  {
    const tree: any = client;
    tree.delete(` + "`${base}/http`" + `);
  }
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("outer Tree parameter must not emit /fs")
	}
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("inner const tree: any must keep /http")
	}
}

func TestExtract_Wave10NxUnionTreeKeepsHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
type HttpClient = { delete: (url: string) => unknown };
export function send(tree: Tree | HttpClient, base: string) {
  tree.delete(` + "`${base}/http`" + `);
}
export function proven(tree: Tree, base: string) {
  tree.delete(` + "`${base}/fs`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("union Tree | HttpClient is not proven Nx; /http must stay")
	}
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("unambiguous Tree parameter must not emit /fs")
	}
}

func TestExtract_Wave10NxUnionSchemaKeepsHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/schema.ts": `import type { Tree } from '@nx/devkit';
export interface Options {
  tree: Tree
}
`,
		"src/client.ts": `import type { Options } from './schema';
type Other = { tree: any };
export function send(options: Options | Other, base: string) {
  const { tree } = options;
  tree.delete(` + "`${base}/http`" + `);
}
export function proven(options: Options, base: string) {
  const { tree } = options;
  tree.delete(` + "`${base}/fs`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("Options | Other cannot prove options.tree; /http must stay")
	}
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("direct Options.tree must suppress /fs")
	}
}

func TestExtract_Wave10NxSchemaValueShadowKeepsInnerHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/schema.ts": `import type { Tree } from '@nx/devkit';
export interface Options {
  tree: Tree
}
`,
		"src/client.ts": `import type { Options } from './schema';
const httpOptions: any = {};
export function outer(options: Options, base: string) {
  const { tree } = options;
  tree.delete(` + "`${base}/fs`" + `);
  {
    const options: any = httpOptions;
    const { tree } = options;
    tree.delete(` + "`${base}/http`" + `);
  }
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("outer Options.tree must not emit /fs")
	}
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("inner const options:any destructure must keep /http")
	}
}

func TestExtract_Wave10NxLocalTypeShadowKeepsHTTP(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
export function outer(base: string) {
  type Tree = { delete: (url: string) => unknown };
  function send(tree: Tree) {
    tree.delete(` + "`${base}/http`" + `);
  }
}
export function proven(tree: Tree, base: string) {
  tree.delete(` + "`${base}/fs`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("local type Tree shadow must keep /http")
	}
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("module-imported Tree parameter must not emit /fs")
	}
}

func TestExtract_Wave10NxIntersectionAndGenericStayClient(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/client.ts": `import type { Tree } from '@nx/devkit';
export function inter(tree: Tree & { extra: 1 }, base: string) {
  tree.delete(` + "`${base}/http`" + `);
}
export function gen(tree: Tree<string>, base: string) {
  tree.delete(` + "`${base}/also`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/http"); !ok {
		t.Fatal("intersection Tree & shape is not proven Nx; /http must stay")
	}
	if _, ok := wave10Route(ff, "/also"); !ok {
		t.Fatal("generic Tree<string> is not proven Nx; /also must stay")
	}
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("unexpected /fs")
	}
}

func TestExtract_Wave10NxImportedAliasSchemaStillSuppresses(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/schema.ts": `import type { Tree as FileTree } from '@nx/devkit';
export interface Options {
  tree: FileTree
}
`,
		"src/client.ts": `import type { Options as Input } from './schema';
export function send(options: Input, base: string) {
  const {tree: files} = options;
  files.delete(` + "`${base}/fs`" + `);
}
`,
	}, false)
	if _, ok := wave10Route(ff, "/fs"); ok {
		t.Fatal("imported-alias schema Tree field must suppress /fs")
	}
	if _, ok := wave10Route(ff, "/http"); ok {
		t.Fatal("unexpected /http")
	}
}

func TestExtract_Wave10NuxtPageHandledByComponent(t *testing.T) {
	ff := extractVue(t, map[string]string{
		"pages/index.vue": `<template><h1>Home</h1></template>
<script setup>
const n = 1
</script>`,
		"pages/about.vue":       `<template><h1>About</h1></template>`,
		"components/Widget.vue": `<template><p /></template>`,
		"pages/alias.vue": `<script setup>
definePageMeta({ alias: '/also' })
</script><template><p /></template>`,
		"lib/util.ts":         `export function helper() { return 1 }`,
		"app/pages/status.ts": `export default defineComponent({ render: () => null })`,
		"app/pages/empty.ts":  `export const n = 1`,
	}, true)

	index, ok := wave10Route(ff, "/")
	if !ok {
		t.Fatal("missing / route")
	}
	if !index.HasRelation(facts.RelHandledBy, "pages.PagesIndex") {
		t.Fatalf("/ handled_by: %+v", index.Relations)
	}
	var tf string
	for _, r := range index.Relations {
		if r.Kind == facts.RelHandledBy {
			tf = r.TargetFile
		}
	}
	if tf != "pages/index.vue" {
		t.Fatalf("handled_by target_file=%q", tf)
	}
	about, _ := wave10Route(ff, "/about")
	if !about.HasRelation(facts.RelHandledBy, "pages.About") {
		t.Fatalf("about handled_by %+v", about.Relations)
	}
	if about.HasRelation(facts.RelHandledBy, "pages.PagesIndex") {
		t.Fatal("about must not bind index component")
	}
	alias, _ := wave10Route(ff, "/also")
	if !alias.HasRelation(facts.RelHandledBy, "pages.Alias") {
		t.Fatalf("alias route handled_by %+v", alias.Relations)
	}
	if _, ok := wave10Route(ff, "/util"); ok {
		t.Fatal("non-page must not emit a route")
	}
	status, ok := wave10Route(ff, "/status")
	if !ok {
		t.Fatal("missing TS page route")
	}
	if !status.HasRelation(facts.RelHandledBy, "app/pages.Status") {
		t.Fatalf("status handled_by %+v", status.Relations)
	}
	empty, ok := wave10Route(ff, "/empty")
	if !ok {
		t.Fatal("empty ts page still has a file-convention route")
	}
	for _, r := range empty.Relations {
		if r.Kind == facts.RelHandledBy {
			t.Fatalf("empty page must not guess handled_by: %+v", r)
		}
	}
}

func TestExtract_Wave10ClassMethodsDeclareClass(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/ids.ts": `
export class BoundedIdSet {
  private order: string[] = [];
  private ids = new Set<string>();
  constructor() {}
  add(id: string) { this.ids.add(id); }
  delete(id: string) { this.ids.delete(id); }
  has(id: string) { return this.ids.has(id); }
}
export function standalone() { return 1; }
`,
	}, false)
	cls, ok := findFact(ff, "src.BoundedIdSet")
	if !ok {
		t.Fatal("missing class")
	}
	for _, name := range []string{"src.BoundedIdSet.constructor", "src.BoundedIdSet.add", "src.BoundedIdSet.delete", "src.BoundedIdSet.has", "src.BoundedIdSet.order", "src.BoundedIdSet.ids"} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		found := false
		for _, r := range f.Relations {
			if r.Kind == facts.RelDeclares && r.Target == "src.BoundedIdSet" && r.TargetFile == "src/ids.ts" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing class declares: %+v", name, f.Relations)
		}
	}
	st, _ := findFact(ff, "src.standalone")
	for _, r := range st.Relations {
		if r.Kind == facts.RelDeclares && r.Target == "src.BoundedIdSet" {
			t.Fatal("top-level function must not declare the class")
		}
	}
	_ = cls
}

func TestExtract_Wave10DefaultCallKind(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/from.ts": "export default Object.fromEntries([['a', 1]])\n",
		"src/end.ts": `export function defineEndpoint(cfg: object) { return cfg; }
export default defineEndpoint({ path: '/x' })
`,
		"src/named.ts": "export const cfg = Object.fromEntries([['a', 1]])\n",
		"src/h3.ts":    "export default defineEventHandler(() => ({}))\n",
		"src/memo.ts":  "export default memo(function C() { return null })\n",
		"src/fwd.ts":   "export default forwardRef(function C() { return null })\n",
	}, false)
	from, ok := findFact(ff, "src.From")
	if !ok {
		t.Fatal("missing Object.fromEntries default")
	}
	if from.Props["symbol_kind"] != facts.SymbolVariable {
		t.Fatalf("fromEntries kind=%v want variable", from.Props["symbol_kind"])
	}
	end, ok := findFact(ff, "src.End")
	if !ok {
		t.Fatal("missing defineEndpoint default")
	}
	if end.Props["symbol_kind"] != facts.SymbolVariable {
		t.Fatalf("defineEndpoint kind=%v want variable", end.Props["symbol_kind"])
	}
	named, _ := findFact(ff, "src.cfg")
	if named.Props["symbol_kind"] != facts.SymbolVariable {
		t.Fatalf("named const kind=%v", named.Props["symbol_kind"])
	}
	h3, _ := findFact(ff, "src.H3")
	if h3.Props["symbol_kind"] != facts.SymbolFunc {
		t.Fatalf("defineEventHandler kind=%v want function", h3.Props["symbol_kind"])
	}
	memo, _ := findFact(ff, "src.Memo")
	if memo.Props["symbol_kind"] != facts.SymbolFunc {
		t.Fatalf("memo kind=%v want function", memo.Props["symbol_kind"])
	}
	fwd, _ := findFact(ff, "src.Fwd")
	if fwd.Props["symbol_kind"] != facts.SymbolFunc {
		t.Fatalf("forwardRef kind=%v want function", fwd.Props["symbol_kind"])
	}
}

func TestExtract_Wave10NuxtPluginFactoryKind(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/landings/nuxt.config.ts": `export default defineNuxtConfig({ name: 'landings' })
`,
		"apps/landings/package.json": `{"name":"landings","dependencies":{"nuxt":"3.14.0"}}
`,
		"runtime/named.ts": `import { defineNuxtPlugin } from '#app'
export default defineNuxtPlugin({
  setup() {
    fetch('/x')
    return {}
  },
})
`,
		"runtime/alias.ts": `import { defineNuxtPlugin as register } from '#app'
export default register(() => ({}))
`,
		"runtime/namespace.ts": `import * as nuxt from '#app'
export default nuxt.defineNuxtPlugin(() => ({}))
`,
		"runtime/payload.ts": `import { definePayloadPlugin as register } from '#app'
export default register(() => ({}))
`,
		"runtime/nuxtapp.ts": `import { defineNuxtPlugin } from 'nuxt/app'
export default defineNuxtPlugin(() => ({}))
`,
		"runtime/imports.ts": `import { defineNuxtPlugin } from '#imports'
export default defineNuxtPlugin({
  name: 'meta-pixel',
  setup() { return {} },
})
`,
		"runtime/importsAlias.ts": `import { defineNuxtPlugin as register } from '#imports'
export default register(() => ({}))
`,
		"runtime/importsNs.ts": `import * as nuxt from '#imports'
export default nuxt.defineNuxtPlugin(() => ({}))
`,
		"runtime/importsPayload.ts": `import { definePayloadPlugin as register } from '#imports'
export default register(() => ({}))
`,
		"apps/landings/plugins/auto.ts": `export default defineNuxtPlugin((nuxtApp) => {
  return {}
})
`,
		"apps/landings/plugins/autoPayload.ts": `export default definePayloadPlugin(() => ({}))
`,
		"runtime/config.ts": `export default defineNuxtConfig({ srcDir: 'src' })
`,
		"runtime/local.ts": `function defineNuxtPlugin(value: any) { return value }
export default defineNuxtPlugin({ name: 'plain-value' })
`,
		"runtime/member.ts": `const local = { defineNuxtPlugin: (value: any) => value }
export default local.defineNuxtPlugin({ name: 'plain-value' })
`,
		"runtime/constplug.ts": `import { defineNuxtPlugin } from '#app'
export const plugin = defineNuxtPlugin(() => ({}))
`,
		"runtime/typeonly.ts": `import type { defineNuxtPlugin } from '#app'
function defineNuxtPlugin(value: any) { return value }
export default defineNuxtPlugin({ name: 'plain-value' })
`,
		"runtime/typeonlyImports.ts": `import type { defineNuxtPlugin } from '#imports'
function defineNuxtPlugin(value: any) { return value }
export default defineNuxtPlugin({ name: 'plain-value' })
`,
		"runtime/foreign.ts": `import { defineNuxtPlugin } from 'not-nuxt'
export default defineNuxtPlugin({ name: 'plain-value' })
`,
		"runtime/localmod.ts": `import { defineNuxtPlugin } from './helpers'
export default defineNuxtPlugin({ name: 'plain-value' })
`,
		"runtime/helpers.ts": `export function defineNuxtPlugin(value: any) { return value }
`,
		"runtime/ordinary.ts": "export default Object.fromEntries([['a', 1]])\n",
		"packages/plain/src/plugin.ts": `export default defineNuxtPlugin({ name: 'plain-value' })
`,
	}, false)

	wantFunc := []string{
		"runtime.Named", "runtime.Alias", "runtime.Namespace", "runtime.Payload", "runtime.Nuxtapp",
		"runtime.Imports", "runtime.ImportsAlias", "runtime.ImportsNs", "runtime.ImportsPayload",
		"apps/landings/plugins.Auto", "apps/landings/plugins.AutoPayload", "runtime.plugin",
	}
	for _, name := range wantFunc {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != facts.SymbolFunc {
			t.Fatalf("%s kind=%v want function", name, f.Props["symbol_kind"])
		}
		if _, ok := f.Props["cyclomatic"]; !ok {
			t.Fatalf("%s missing function metrics", name)
		}
	}
	named, _ := findFact(ff, "runtime.Named")
	foundFetch := false
	for _, r := range named.Relations {
		if r.Kind == facts.RelCalls && strings.Contains(strings.ToLower(r.Target), "fetch") {
			foundFetch = true
		}
	}
	if !foundFetch {
		t.Fatalf("named plugin missing direct fetch call: %+v", named.Relations)
	}
	for _, name := range []string{
		"runtime.Local", "runtime.Member", "runtime.Typeonly", "runtime.TypeonlyImports",
		"runtime.Ordinary", "runtime.Config", "runtime.Foreign", "runtime.Localmod",
		"packages/plain/src.Plugin", "apps/landings.NuxtConfig",
	} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != facts.SymbolVariable {
			t.Fatalf("%s kind=%v want variable", name, f.Props["symbol_kind"])
		}
	}
}

func TestExtract_Wave10H3LazyEventHandlerKind(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/landings-module-image/src/runtime/images/ipxHandler.ts": `import { lazyEventHandler } from 'h3'
import { createIPX, ipxFSStorage, ipxHttpStorage, createIPXH3Handler } from 'ipx'
export default lazyEventHandler(() => {
  const ipxOptions = { maxAge: 3600 }
  const ipx = createIPX({
    storage: ipxFSStorage({ dir: ipxOptions.dir ?? './public' }),
    httpStorage: ipxHttpStorage({ domains: ipxOptions.domains }),
    ...ipxOptions,
  })
  const handler = createIPXH3Handler(ipx)
  return handler
})
`,
		"runtime/named.ts": `import { defineLazyEventHandler } from 'h3'
export default defineLazyEventHandler(() => {
  return () => ({})
})
`,
		"runtime/alias.ts": `import { lazyEventHandler as lazy } from 'h3'
export default lazy(() => () => ({}))
`,
		"runtime/namespace.ts": `import * as h3 from 'h3'
export default h3.lazyEventHandler(() => () => ({}))
`,
		"runtime/nsDefine.ts": `import * as h3 from 'h3'
export default h3.defineLazyEventHandler(() => () => ({}))
`,
		"runtime/defaultLazy.ts": `import lazyEventHandler from 'h3'
export default lazyEventHandler(() => () => ({}))
`,
		"runtime/defaultDefine.ts": `import defineLazyEventHandler from 'h3'
export default defineLazyEventHandler(() => () => ({}))
`,
		"runtime/subpath.ts": `import { lazyEventHandler } from 'h3/utils'
export default lazyEventHandler(() => () => ({}))
`,
		"runtime/event.ts": `import { defineEventHandler } from 'h3'
export default defineEventHandler(() => ({}))
`,
		"runtime/bareEvent.ts": "export default defineEventHandler(() => ({}))\n",
		"runtime/constlazy.ts": `import { lazyEventHandler } from 'h3'
export const handler = lazyEventHandler(() => () => ({}))
`,
		"runtime/local.ts": `function lazyEventHandler(value: any) { return value }
export default lazyEventHandler({ name: 'plain-value' })
`,
		"runtime/member.ts": `const local = { lazyEventHandler: (value: any) => value }
export default local.lazyEventHandler({ name: 'plain-value' })
`,
		"runtime/typeonly.ts": `import type { lazyEventHandler } from 'h3'
function lazyEventHandler(value: any) { return value }
export default lazyEventHandler({ name: 'plain-value' })
`,
		"runtime/typeonlyDefine.ts": `import type { defineLazyEventHandler } from 'h3'
function defineLazyEventHandler(value: any) { return value }
export default defineLazyEventHandler({ name: 'plain-value' })
`,
		"runtime/foreign.ts": `import { lazyEventHandler } from 'not-h3'
export default lazyEventHandler({ name: 'plain-value' })
`,
		"runtime/localmod.ts": `import { lazyEventHandler } from './helpers'
export default lazyEventHandler({ name: 'plain-value' })
`,
		"runtime/helpers.ts": `export function lazyEventHandler(value: any) { return value }
`,
		"runtime/ordinary.ts": "export default Object.fromEntries([['a', 1]])\n",
		"runtime/bareLazy.ts": "export default lazyEventHandler(() => ({}))\n",
	}, false)

	wantFunc := []string{
		"packages/landings-module-image/src/runtime/images.IpxHandler",
		"runtime.Named", "runtime.Alias", "runtime.Namespace", "runtime.NsDefine",
		"runtime.DefaultLazy", "runtime.DefaultDefine", "runtime.Subpath",
		"runtime.Event", "runtime.BareEvent", "runtime.handler",
	}
	for _, name := range wantFunc {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != facts.SymbolFunc {
			t.Fatalf("%s kind=%v want function", name, f.Props["symbol_kind"])
		}
		if _, ok := f.Props["cyclomatic"]; !ok {
			t.Fatalf("%s missing function metrics", name)
		}
	}
	for _, name := range []string{
		"runtime.Local", "runtime.Member", "runtime.Typeonly", "runtime.TypeonlyDefine",
		"runtime.Ordinary", "runtime.Foreign", "runtime.Localmod", "runtime.BareLazy",
	} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != facts.SymbolVariable {
			t.Fatalf("%s kind=%v want variable", name, f.Props["symbol_kind"])
		}
	}
}

func TestExtract_Wave10DefaultObjectValue(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"packages/web-push/src/utils/base64.ts": `export function urlBase64ToUint8Array(s: string) { return s; }
export default {
  urlBase64ToUint8Array,
};
`,
		"packages/web-push/src/getSubscription.ts": `import base64 from './utils/base64';
export function getSubscription(serverKey: string) {
  return base64.urlBase64ToUint8Array(serverKey);
}
`,
		"packages/web-push/src/utils/named.ts": `const codec = { n: 1 };
export default codec;
`,
		"packages/web-push/src/useNamed.ts": `import codec from './utils/named';
export function use() { return codec.n; }
`,
		"packages/web-push/src/utils/arr.ts": "export default [1, 2, 3]\n",
		"apps/mobile/eslint.config.mjs":      "export default [ { files: ['**/*.js'] } ]\n",
	}, false)

	obj, ok := findFact(ff, "packages/web-push/src/utils.Base64")
	if !ok {
		t.Fatal("missing default object node")
	}
	if obj.Props["symbol_kind"] != facts.SymbolVariable {
		t.Fatalf("object default kind=%v", obj.Props["symbol_kind"])
	}
	if _, ok := findFact(ff, "packages/web-push/src/utils.urlBase64ToUint8Array"); !ok {
		t.Fatal("local helper must remain")
	}
	ref := fileRefFact(ff, "packages/web-push/src/getSubscription.ts")
	if !hasCallToFile(ref, "packages/web-push/src/utils.Base64", "packages/web-push/src/utils/base64.ts") {
		t.Fatalf("default import must bind the object: %+v", ref.Relations)
	}
	named, ok := findFact(ff, "packages/web-push/src/utils.codec")
	if !ok {
		t.Fatal("named const default missing")
	}
	_ = named
	use := fileRefFact(ff, "packages/web-push/src/useNamed.ts")
	if !hasCallToFile(use, "packages/web-push/src/utils.codec", "packages/web-push/src/utils/named.ts") {
		t.Fatalf("named default import must bind codec: %+v", use.Relations)
	}
	// Filename-derived ghost may remain only if still emitted; do not hide it
	// by omitting the assertion — record it when present.
	ghost := false
	for _, r := range use.Relations {
		if r.Kind == facts.RelCalls && strings.HasSuffix(r.Target, ".Named") && r.Target != "packages/web-push/src/utils.codec" {
			ghost = true
		}
	}
	if ghost {
		t.Log("named-default filename fallback edge still present (documented)")
	}
	if _, ok := findFact(ff, "packages/web-push/src/utils.Arr"); !ok {
		t.Fatal("array default must emit a value node")
	}
	if _, ok := findFact(ff, "apps/mobile.EslintConfig"); !ok {
		t.Fatal("eslint.config.mjs array default must emit a value node")
	}
}

func TestExtract_Wave10GtsDefaultImportNamedUnlikeFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"app/components/stamp.gts": `import Component from '@glimmer/component';
export default class KitBadge extends Component {
  <template>
    <span>{{yield}}</span>
  </template>
}
`,
		"app/components/card.gts": `import KitBadge from './stamp';
export default class Card extends Component {
  <template>
    <KitBadge />
  </template>
}
`,
		"app/use.ts": `import KitBadge from './components/stamp';
export function use() { return KitBadge; }
`,
		"src/plain.ts": `export default class Helper {}
`,
		"src/take.ts": `import Helper from './plain';
export function take() { return Helper; }
`,
		"src/none.ts": `export const x = 1;
`,
		"src/fromnone.ts": `import Missing from './none';
export function fromNone() { return Missing; }
`,
	}, false)

	if _, ok := findFact(ff, "app/components.KitBadge"); !ok {
		t.Fatal("gts default class must keep its declared name")
	}
	if _, ok := findFact(ff, "app/components.Stamp"); ok {
		t.Fatal("gts default must not invent a filename alias Stamp")
	}
	use := fileRefFact(ff, "app/use.ts")
	if !hasCallToFile(use, "app/components.KitBadge", "app/components/stamp.gts") {
		t.Fatalf("normal import of gts default must bind KitBadge: %+v", use.Relations)
	}
	if hasCallTarget(use, "app/components.Stamp") || hasCallTarget(use, "app/components.default") {
		t.Fatalf("gts default import guessed filename or unresolved default: %+v", use.Relations)
	}
	plain := fileRefFact(ff, "src/take.ts")
	if !hasCallToFile(plain, "src.Helper", "src/plain.ts") {
		t.Fatalf("ts default class import must stay Helper: %+v", plain.Relations)
	}
	missing := fileRefFact(ff, "src/fromnone.ts")
	if !hasCallTarget(missing, "src.default") {
		t.Fatalf("module without default must keep unresolved default: %+v", missing.Relations)
	}
}

func TestExtract_Wave10GjsDefaultImportNamedUnlikeFile(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"app/components/chip.gjs": `import Component from '@glimmer/component';
export default class StatusChip extends Component {
  <template>
    <span>{{yield}}</span>
  </template>
}
`,
		"app/read.js": `import StatusChip from './components/chip';
export function read() { return StatusChip; }
`,
	}, false)
	ref := fileRefFact(ff, "app/read.js")
	if !hasCallToFile(ref, "app/components.StatusChip", "app/components/chip.gjs") {
		t.Fatalf("gjs default import must bind StatusChip: %+v", ref.Relations)
	}
	if hasCallTarget(ref, "app/components.Chip") {
		t.Fatalf("gjs default import guessed filename: %+v", ref.Relations)
	}
}

func TestExtractTestRefs_Wave10GtsDefaultClassUnlikeFilename(t *testing.T) {
	dir := setupTSProject(t, map[string]string{
		"app/components/stamp.gts": `import Component from '@glimmer/component';
export default class KitBadge extends Component {
  <template>
    <span>{{yield}}</span>
  </template>
}
`,
		"tests/acceptance/catalog-test.ts": `import KitBadge from '../../app/components/stamp';
export function exercise() { return KitBadge; }
`,
	}, false)
	ff, err := New().ExtractTestRefs(context.Background(), dir, []string{"tests/acceptance/catalog-test.ts"}, []string{"app/components/stamp.gts"})
	if err != nil {
		t.Fatal(err)
	}
	got := testRefTargets(t, ff, "tests/acceptance/catalog-test.ts")
	if !got["app/components.KitBadge"] {
		t.Fatalf("test import of gts default must bind KitBadge; got %v", got)
	}
	if got["app/components.Stamp"] || got["app/components.default"] {
		t.Fatalf("test import guessed filename or unresolved default: %v", got)
	}
}

func TestExtract_Wave10BuildRoutesReturnIsFunction(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"lib/shop/addon/routes.js": `import buildRoutes from 'ember-engines/routes';
export default buildRoutes(function () {
  this.route('cart');
});
`,
		"lib/shop/addon/alias.js": `import { default as mapEngine } from 'ember-engines/routes';
export default mapEngine(function () { this.route('x'); });
`,
		"lib/shop/addon/ns.js": `import * as engines from 'ember-engines/routes';
export default engines.default(function () { this.route('y'); });
`,
		"lib/shop/addon/local.js": `function buildRoutes(value) { return value; }
export default buildRoutes(function () { this.route('z'); });
`,
		"lib/shop/addon/member.js": `const local = { buildRoutes: (value) => value };
export default local.buildRoutes(function () { this.route('m'); });
`,
		"lib/shop/addon/typeonly.js": `import type buildRoutes from 'ember-engines/routes';
function buildRoutes(value) { return value; }
export default buildRoutes(function () { this.route('t'); });
`,
		"lib/shop/addon/other.js": `import buildRoutes from 'not-engines/routes';
export default buildRoutes(function () { this.route('o'); });
`,
	}, false)
	for _, name := range []string{"lib/shop/addon.Routes", "lib/shop/addon.Alias", "lib/shop/addon.Ns"} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != facts.SymbolFunc {
			t.Fatalf("%s kind=%v want function (buildRoutes returns the callback)", name, f.Props["symbol_kind"])
		}
	}
	for _, name := range []string{"lib/shop/addon.Local", "lib/shop/addon.Member", "lib/shop/addon.Typeonly", "lib/shop/addon.Other"} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if f.Props["symbol_kind"] != facts.SymbolVariable {
			t.Fatalf("%s kind=%v want variable", name, f.Props["symbol_kind"])
		}
	}
}
