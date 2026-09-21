package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/extractors/asyncapiextractor"
	"github.com/enola-labs/enola/internal/extractors/grpcextractor"
	"github.com/enola-labs/enola/internal/extractors/mdintent"
	"github.com/enola-labs/enola/internal/extractors/openapiextractor"
	"github.com/enola-labs/enola/pkg/plugin"
)

func TestDetectorOriginalScope(t *testing.T) {
	for _, tc := range []struct {
		name, file, body string
		ext              plugin.Extractor
		want             bool
	}{
		{"markdown-archive", "_archive/page.md", "# hi", mdintent.New(), false},
		{"markdown-deep", "a/b/c/d/e/f/page.md", "# hi", mdintent.New(), false},
		{"markdown-pruned", "private/page.md", "# hi", mdintent.New(), true},
		{"grpc-testdata", "testdata/service.proto", "syntax = \"proto3\";", grpcextractor.New(), false},
		{"grpc-pruned", "private/service.proto", "syntax = \"proto3\";", grpcextractor.New(), true},
		{"openapi-pruned", "private/spec.openapi.yaml", "openapi: 3.0.0\ninfo: {}\npaths: {}\n", openapiextractor.New(), true},
		{"openapi-absolute", "api.yaml", "openapi: 3.0.0\ninfo: {}\npaths: {}\n", openapiextractor.New(), true},
		{"asyncapi-pruned", "private/api.json", `{"asyncapi":"2.6.0","info":{},"channels":{}}`, asyncapiextractor.New(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.name == "openapi-absolute" {
				root = filepath.Join(root, "openapi")
				if err := os.MkdirAll(root, 0755); err != nil {
					t.Fatal(err)
				}
			}
			writeDeep(t, root, tc.file, tc.body)
			cfg := config.Default()
			cfg.Ignore = []string{"private/**"} // relaxed defaults expose original detector skips
			eng, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			inv, err := eng.Inventory(root)
			if err != nil {
				t.Fatal(err)
			}
			direct, err := tc.ext.Detect(root)
			if err != nil || direct != tc.want {
				t.Fatalf("bad fixture: direct=%v want=%v err=%v", direct, tc.want, err)
			}
			eng.RegisterIndependentExtractor(tc.ext)
			if got := eng.DetectExtractors(root, inv.AllNames)[tc.ext.Name()]; got != direct {
				t.Fatalf("concurrent detection=%v direct=%v", got, direct)
			}
			got, err := eng.DetectExtractor(tc.ext, root, inv.AllNames)
			if err != nil || got != direct {
				t.Fatalf("engine detection=%v direct=%v err=%v", got, direct, err)
			}
		})
	}
}
