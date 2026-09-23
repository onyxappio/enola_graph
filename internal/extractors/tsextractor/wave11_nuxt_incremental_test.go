package tsextractor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func wave11SessionFiles(t *testing.T, files map[string]string) (dir string, names []string) {
	t.Helper()
	dir = setupTSProject(t, files, false)
	for rel := range files {
		names = append(names, rel)
	}
	return dir, names
}

func TestExtractSession_Wave11NuxtIncrementalRegisterConsumer(t *testing.T) {
	registered := `export default function setup() {
  addImportsDir(resolver.resolve('./runtime/composables/'))
}
`
	unregistered := `export default function setup() {
}
`
	origin := "packages/shared-lands-components/src/Stepper.useStep"
	local := "apps/landings/composables.useLocalFlag"
	meta := "packages/landings-module/src/runtime/composables.setLandPageMetadata"
	consumer := "apps/landings/pages/CommunityProof.vue"
	moduleRel := "packages/landings-module/src/module.ts"

	dir, names := wave11SessionFiles(t, wave11NuxtLandingsFiles(registered))
	ext := New()
	ctx := context.Background()
	cold, err := ext.ExtractSession(ctx, dir, names, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionBound(t, "cold", cold.Facts, origin, meta, local, true, true)

	warm, err := ext.ExtractSession(ctx, dir, names, cold.Records, map[string]bool{}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if warm.Stats.FilesParsed != 0 {
		t.Fatalf("warmup parsed %d, want 0", warm.Stats.FilesParsed)
	}
	assertSessionBound(t, "warmup", warm.Facts, origin, meta, local, true, true)

	consumerPath := filepath.Join(dir, consumer)
	body, err := os.ReadFile(consumerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(consumerPath, append(body, []byte("\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	delta, err := ext.ExtractSession(ctx, dir, names, warm.Records, map[string]bool{consumer: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if delta.Stats.FilesParsed == 0 {
		t.Fatal("consumer edit parsed no files")
	}
	assertSessionBound(t, "consumer-edit", delta.Facts, origin, meta, local, true, true)

	if err := os.WriteFile(filepath.Join(dir, moduleRel), []byte(unregistered), 0o644); err != nil {
		t.Fatal(err)
	}
	unreg, err := ext.ExtractSession(ctx, dir, names, delta.Records, map[string]bool{moduleRel: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionBound(t, "unregister", unreg.Facts, origin, meta, local, false, false)

	if err := os.WriteFile(filepath.Join(dir, moduleRel), []byte(registered), 0o644); err != nil {
		t.Fatal(err)
	}
	restored, err := ext.ExtractSession(ctx, dir, names, unreg.Records, map[string]bool{moduleRel: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionBound(t, "restore", restored.Facts, origin, meta, local, true, true)

	cfgRel := "apps/landings/nuxt.config.ts"
	if err := os.WriteFile(filepath.Join(dir, cfgRel), []byte("export default defineNuxtConfig({ modules: [] })\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dropped, err := ext.ExtractSession(ctx, dir, names, restored.Records, map[string]bool{cfgRel: true}, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionBound(t, "config-modules-removed", dropped.Facts, origin, meta, local, false, false)
}

func assertSessionBound(t *testing.T, label string, ff []facts.Fact, origin, meta, local string, wantStep, wantMeta bool) {
	t.Helper()
	got := fileRefTargets(ff, "apps/landings/pages/CommunityProof.vue")
	if hasTarget(got, origin) != wantStep {
		t.Errorf("%s useStep bound=%v want=%v refs=%v", label, hasTarget(got, origin), wantStep, got)
	}
	if hasTarget(got, meta) != wantMeta {
		t.Errorf("%s setLandPageMetadata bound=%v want=%v refs=%v", label, hasTarget(got, meta), wantMeta, got)
	}
	loc := fileRefTargets(ff, "apps/landings/pages/Local.vue")
	if !hasTarget(loc, local) {
		t.Errorf("%s locally declared composable lost: %v", label, loc)
	}
}

func TestNuxtContextReadsStayBounded(t *testing.T) {
	moduleSrc := []byte(`export default function setup() { addImportsDir(resolver.resolve('./runtime/composables/')) }`)
	cfgSrc := []byte(`import landingModule from 'landings-module'
export default defineNuxtConfig({ modules: [landingModule] })`)
	records := map[string]*FileRecord{
		"packages/module/src/register.ts": {File: "packages/module/src/register.ts", AutoImportDirs: []string{"packages/module/src/runtime/composables"}},
	}
	nuxtPkgs := []string{"apps/site", "packages/module"}
	pkgDirs := map[string]bool{"apps/site": true, "packages/module": true, "packages/unrelated": true}
	pkgDirByName := map[string]string{"landings-module": "packages/module", "module": "packages/module"}
	known := map[string]bool{
		"apps/site/nuxt.config.ts":           true,
		"packages/module/src/register.ts":    true,
		"packages/module/src/runtime/composables/useStep.ts": true,
	}
	for i := 0; i < 2000; i++ {
		known[fmt.Sprintf("packages/unrelated/src/file%d.ts", i)] = true
	}

	for _, withNuxt := range []bool{false, true} {
		t.Run(fmt.Sprintf("nuxt_%v", withNuxt), func(t *testing.T) {
			pkgs := []string(nil)
			if withNuxt {
				pkgs = nuxtPkgs
			}
			reads := 0
			unrelated := 0
			read := func(rel string) []byte {
				reads++
				if strings.HasPrefix(rel, "packages/unrelated/") {
					unrelated++
				}
				switch rel {
				case "apps/site/nuxt.config.ts":
					return cfgSrc
				case "packages/module/src/register.ts":
					return moduleSrc
				default:
					return []byte("export const unrelated = 1")
				}
			}
			extra := extraDirsByNuxtPackageRead(nil, known, read, pkgs, pkgDirs, records, nil)
			cons := nuxtModuleConsumersRead(nil, known, read, pkgs, pkgDirByName, pkgDirs)
			if unrelated != 0 {
				t.Fatalf("unrelated reads=%d total=%d", unrelated, reads)
			}
			if !withNuxt {
				if reads != 0 {
					t.Fatalf("no Nuxt package still read %d files", reads)
				}
				if len(extra) != 0 || len(cons) != 0 {
					t.Fatalf("empty visibility extra=%v cons=%v", extra, cons)
				}
				return
			}
			if reads == 0 {
				t.Fatal("expected bounded nuxt.config overlay reads")
			}
			if reads > 4 {
				t.Fatalf("registration reads=%d, want a small config set", reads)
			}
			if len(extra["packages/module"]) == 0 {
				t.Fatalf("missing extra dirs from record: %v", extra)
			}
			if len(cons["apps/site"]) == 0 {
				t.Fatalf("missing consumers from nuxt.config: %v", cons)
			}
		})
	}

	t.Run("dirty_record_ignored_until_sources", func(t *testing.T) {
		dirty := map[string]bool{"packages/module/src/register.ts": true}
		extra := extraDirsByNuxtPackageRead(nil, known, nil, nuxtPkgs, pkgDirs, records, dirty)
		if len(extra["packages/module"]) != 0 {
			t.Fatalf("stale dirty AutoImportDirs leaked: %v", extra)
		}
		src := map[string][]byte{"packages/module/src/register.ts": moduleSrc}
		extra = extraDirsByNuxtPackageRead(src, known, nil, nuxtPkgs, pkgDirs, records, dirty)
		if len(extra["packages/module"]) == 0 {
			t.Fatal("fresh dirty registration missing")
		}
	})
}
