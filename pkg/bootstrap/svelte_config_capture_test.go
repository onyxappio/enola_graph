package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/graphsession"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestScopedSvelteConfigCapture(t *testing.T) {
	for _, mode := range []string{"resident", "strict"} {
		for _, dir := range []string{"", "packages/database"} {
			for _, ext := range []string{"js", "ts", "mjs"} {
				t.Run(mode+"/"+dir+"/"+ext, func(t *testing.T) {
					root := t.TempDir()
					graphGit(t, root, "init", "-q")
					graphWrite(t, root, "mcp-arch.yaml", "extractors: [typescript]\nignore: []\n")
					graphWrite(t, root, "package.json", `{"name":"app","dependencies":{"@sveltejs/kit":"2"}}`)
					graphWrite(t, root, "tsconfig.json", "{}")
					graphWrite(t, root, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"paths":{"@stable":["./a.ts"]}}}`)
					graphWrite(t, root, filepath.Join(dir, "a.ts"), "export function a(){return 1}")
					graphWrite(t, root, filepath.Join(dir, "other.ts"), "export function a(){return 2}")
					graphWrite(t, root, filepath.Join(dir, "consumer.ts"), "import {a} from '@chosen';export function consumer(){return a()}")
					config := filepath.ToSlash(filepath.Join(dir, "svelte.config."+ext))
					eng, err := NewGraphEngine(GraphOptions{Repo: root})
					if err != nil {
						t.Fatal(err)
					}
					sink := &graphstream.MemorySink{}
					opts := graphsession.Options{StateDir: t.TempDir()}
					var apply func(...string)
					if mode == "resident" {
						resident, err := graphsession.OpenSession(context.Background(), eng.Analysis(), root, sink, opts)
						if err != nil {
							t.Fatal(err)
						}
						defer resident.Close()
						var watermark uint64
						apply = func(paths ...string) {
							t.Helper()
							res, err := resident.ApplyChanges(context.Background(), graphsession.ChangeBatch{Epoch: "svelte", From: watermark, Through: watermark + 1, Covered: true, Paths: paths})
							watermark++
							if err != nil {
								t.Fatal(err)
							}
							t.Logf("paths=%v parsed=%d reasons=%v", paths, res.ParsedFiles, res.Invalidation.ContextReasons)
						}
					} else {
						apply = func(_ ...string) {
							t.Helper()
							res, err := graphsession.Run(context.Background(), eng.Analysis(), root, sink, opts)
							if err != nil {
								t.Fatal(err)
							}
							t.Logf("strict parsed=%d", res.ParsedFiles)
						}
					}
					apply()
					for _, body := range []string{
						"export default {kit:{alias:{'@chosen':'./a.ts'}}}",
						"export default {kit:{alias:{'@chosen':'./other.ts'}}}",
						"",
					} {
						if body == "" {
							if err := os.Remove(filepath.Join(root, config)); err != nil {
								t.Fatal(err)
							}
						} else {
							graphWrite(t, root, config, body)
						}
						apply(config)
						graphColdEqual(t, root, sink)
					}
					before := len(sink.CloneRecords())
					apply()
					if len(sink.CloneRecords()) != before {
						t.Fatal("unchanged config published events")
					}
				})
			}
		}
	}
}

func TestScopedFrameworkConfigCandidates(t *testing.T) {
	root := t.TempDir()
	graphWrite(t, root, "frontend/package.json", `{"dependencies":{"typescript":"5"}}`)
	graphWrite(t, root, "packages/database/tsconfig.json", `{"compilerOptions":{"paths":{"@stable":["./a.ts"]}}}`)
	paths := tsextractor.ConfigInputPaths(root)
	for _, dir := range []string{"", "frontend"} {
		for _, family := range []string{"svelte", "nuxt", "next"} {
			for _, ext := range []string{"js", "ts", "mjs"} {
				candidate := filepath.ToSlash(filepath.Join(dir, family+".config."+ext))
				if !slices.Contains(paths, candidate) {
					t.Errorf("missing framework candidate %s", candidate)
				}
			}
		}
	}
	for _, ext := range []string{"js", "ts", "mjs"} {
		candidate := "packages/database/svelte.config." + ext
		if !slices.Contains(paths, candidate) {
			t.Errorf("missing alias-root candidate %s", candidate)
		}
	}
}
