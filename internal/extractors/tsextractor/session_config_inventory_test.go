package tsextractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestSessionCanOmitUnusedConfigInventoryWithoutChangingExtraction(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"package.json":          `{"name":"fixture"}`,
		"tsconfig.json":         `{"extends":"./private/base.json","compilerOptions":{"paths":{"@lib/*":["./lib/*"]}}}`,
		"private/base.json":     `{"compilerOptions":{"strict":true}}`,
		"private/tsconfig.json": `{}`,
		"lib/x.ts":              "export function x(){return 1}",
		"src/user.ts":           "import {x} from '@lib/x'; export function user(){return x()}",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ext := New()
	files := []string{"src/user.ts", "lib/x.ts"}
	full, err := ext.ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(full.ConfigPaths, "private/tsconfig.json") || !slices.Contains(full.ConfigPaths, "private/base.json") {
		t.Fatalf("missing independent config inputs: %v", full.ConfigPaths)
	}
	omitted, err := ext.ExtractSession(context.Background(), root, files, nil, nil, SessionHooks{SkipConfigPaths: true})
	if err != nil {
		t.Fatal(err)
	}
	if omitted.ConfigPaths != nil {
		t.Fatalf("unused inventory retained: %v", omitted.ConfigPaths)
	}
	full.ConfigPaths = nil
	for _, result := range []*SessionResult{full, omitted} {
		slices.SortFunc(result.Facts, func(a, b facts.Fact) int {
			x, _ := json.Marshal(a)
			y, _ := json.Marshal(b)
			return strings.Compare(string(x), string(y))
		})
	}
	if !reflect.DeepEqual(full, omitted) {
		t.Fatal("omitting informational config paths changed extraction")
	}
}
