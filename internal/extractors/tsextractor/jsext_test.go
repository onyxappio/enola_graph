package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestExtract_JSESMExtensionSubstitutesTS(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"apps/mobile/.storybook/main.ts": `
import { figmaCatalogIndexer } from './figma-catalog-indexer.js';
import { plugin } from './plugins/one.js';
import { real } from './real.js';
import { missing } from './absent.js';
import { jsonlike } from './data.json';
export const run = figmaCatalogIndexer;
`,
		"apps/mobile/.storybook/figma-catalog-indexer.ts": `export function figmaCatalogIndexer() { return 1; }`,
		"apps/mobile/.storybook/plugins/one.ts":           `export function plugin() { return 1; }`,
		"apps/mobile/.storybook/real.js":                  `export function real() { return 1; }`,
	}, false)

	if !importEdgeResolves(t, ff, "apps/mobile/.storybook/main.ts", "apps/mobile/.storybook", "apps/mobile/.storybook/figma-catalog-indexer.ts") {
		t.Fatalf(".js specifier must bind the .ts sibling:\n%s", importDump(ff, "apps/mobile/.storybook/main.ts"))
	}
	if !importEdgeResolves(t, ff, "apps/mobile/.storybook/main.ts", "apps/mobile/.storybook/plugins", "apps/mobile/.storybook/plugins/one.ts") {
		t.Fatalf("nested .js specifier must bind .ts:\n%s", importDump(ff, "apps/mobile/.storybook/main.ts"))
	}
	if !importEdgeResolves(t, ff, "apps/mobile/.storybook/main.ts", "apps/mobile/.storybook", "apps/mobile/.storybook/real.js") {
		t.Fatalf("genuine .js file must stay .js:\n%s", importDump(ff, "apps/mobile/.storybook/main.ts"))
	}
	if importEdgeResolves(t, ff, "apps/mobile/.storybook/main.ts", "apps/mobile/.storybook", "apps/mobile/.storybook/absent.js") ||
		importEdgeResolves(t, ff, "apps/mobile/.storybook/main.ts", "apps/mobile/.storybook", "apps/mobile/.storybook/absent.ts") {
		t.Fatal("missing .js specifier must stay unresolved")
	}
	for _, f := range ff {
		if f.Kind == facts.KindDependency && f.File == "apps/mobile/.storybook/main.ts" && f.PropString(facts.PropTargetFile) == "apps/mobile/.storybook/data.json" {
			t.Fatal("must not strip a non-JS/TS suffix")
		}
	}
}

func TestExtract_JSESMExtensionPrefersTSOverJS(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/use.ts":  `import { x } from './both.js'; export const v = x;`,
		"src/both.ts": `export const x = 1;`,
		"src/both.js": `export const x = 2;`,
	}, false)
	if !importEdgeResolves(t, ff, "src/use.ts", "src", "src/both.ts") {
		t.Fatalf("TS substitution order prefers .ts:\n%s", importDump(ff, "src/use.ts"))
	}
}

func TestExtract_MJSSubstitutesMTS(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/use.ts":  `import { x } from './mod.mjs'; export const v = x;`,
		"src/mod.mts": `export const x = 1;`,
	}, false)
	if !importEdgeResolves(t, ff, "src/use.ts", "src", "src/mod.mts") {
		t.Fatalf(".mjs must substitute .mts:\n%s", importDump(ff, "src/use.ts"))
	}
}
