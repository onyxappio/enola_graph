package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_NuxtDefaultTildeAliasWithoutGeneratedConfig(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/landings/land-test9/package.json":               `{"dependencies":{"nuxt":"^4.3.1","vue":"^3.0.0"}}`,
		"apps/landings/land-test9/nuxt.config.ts":             `export default defineNuxtConfig({ modules: [] })`,
		"apps/landings/land-test9/tsconfig.json":              `{ "extends": "./.nuxt/tsconfig.json" }`,
		"apps/landings/land-test9/plugin.ts":                  "import { resolvePreconnectOrigins } from '~/utils/preconnectOrigins'\nexport const origins = resolvePreconnectOrigins([])\n",
		"apps/landings/land-test9/app.vue":                    "<script setup lang=\"ts\">import { resolvePreconnectOrigins } from '~/utils/preconnectOrigins'</script><template><div /></template>\n",
		"apps/landings/land-test9/utils/preconnectOrigins.ts": `export function resolvePreconnectOrigins(values: unknown[]): string[] { return []; }`,
		"apps/other/package.json":                             `{"dependencies":{"vue":"^3.0.0"}}`,
		"apps/other/src/x.vue":                                `<script setup>import { n } from '~/missing'</script><template></template>`,
	}, false)
	if !importEdgeResolves(t, ff, "apps/landings/land-test9/plugin.ts", "apps/landings/land-test9/utils", "apps/landings/land-test9/utils/preconnectOrigins.ts") {
		t.Fatalf("~/utils must resolve from TS:\n%s\nall=%v", importDump(ff, "apps/landings/land-test9/plugin.ts"), factNames(ff))
	}
	if !importEdgeResolves(t, ff, "apps/landings/land-test9/app.vue", "apps/landings/land-test9/utils", "apps/landings/land-test9/utils/preconnectOrigins.ts") {
		t.Fatalf("~/utils must resolve inside the Nuxt package:\n%s\nall=%v", importDump(ff, "apps/landings/land-test9/app.vue"), factNames(ff))
	}
	for _, f := range ff {
		if f.Kind == facts.KindDependency && f.File == "apps/other/src/x.vue" && f.PropString(facts.PropTargetFile) == "apps/landings/land-test9/utils/preconnectOrigins.ts" {
			t.Fatal("Nuxt ~/ alias must not leak across package boundaries")
		}
	}
}

func TestExtract_NuxtAppDirIsSourceRoot(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"package.json":                   `{"dependencies":{"nuxt":"^4.3.1","vue":"^3.0.0"}}`,
		"nuxt.config.ts":                 `export default defineNuxtConfig({})`,
		"app/utils/preconnectOrigins.ts": `export function resolvePreconnectOrigins() { return []; }`,
		"app.vue":                        `<script setup lang="ts">import { resolvePreconnectOrigins } from '~/utils/preconnectOrigins'</script><template></template>`,
	}, false)
	if !importEdgeResolves(t, ff, "app.vue", "app/utils", "app/utils/preconnectOrigins.ts") {
		t.Fatalf("Nuxt4 app/ layout must be srcDir:\n%s", importDump(ff, "app.vue"))
	}
}

func TestExtract_ExplicitTSConfigTildeBeatsNuxtDefault(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"package.json":                `{"dependencies":{"nuxt":"^4.3.1","vue":"^3.0.0"}}`,
		"nuxt.config.ts":              `export default defineNuxtConfig({})`,
		"tsconfig.json":               `{"compilerOptions":{"paths":{"~/*":["./custom/*"]}}}`,
		"custom/preconnectOrigins.ts": `export function resolvePreconnectOrigins() { return 1; }`,
		"utils/preconnectOrigins.ts":  `export function resolvePreconnectOrigins() { return 2; }`,
		"app.vue":                     `<script setup lang="ts">import { resolvePreconnectOrigins } from '~/preconnectOrigins'</script><template></template>`,
	}, false)
	if !importEdgeResolves(t, ff, "app.vue", "custom", "custom/preconnectOrigins.ts") {
		t.Fatalf("explicit tsconfig paths must win:\n%s", importDump(ff, "app.vue"))
	}
}

func TestExtract_NuxtSrcDirLiteralOverride(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"package.json":                   `{"dependencies":{"nuxt":"^4.3.1","vue":"^3.0.0"}}`,
		"nuxt.config.ts":                 `export default defineNuxtConfig({ srcDir: 'src' })`,
		"src/utils/preconnectOrigins.ts": `export function resolvePreconnectOrigins() { return []; }`,
		"app.vue":                        `<script setup lang="ts">import { resolvePreconnectOrigins } from '~/utils/preconnectOrigins'</script><template></template>`,
	}, false)
	if !importEdgeResolves(t, ff, "app.vue", "src/utils", "src/utils/preconnectOrigins.ts") {
		t.Fatalf("static srcDir must be respected:\n%s", importDump(ff, "app.vue"))
	}
}
