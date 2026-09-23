package tsextractor

import (
	"encoding/json"
	"testing"
)

func TestParsePackageJSONExportsSubpaths(t *testing.T) {
	known := map[string]bool{
		"packages/shared-lands-types/src/index.ts": true,
		"packages/shared-lands-types/src/enums.ts": true,
	}
	exports := json.RawMessage(`{
    ".": {"types":"./src/index.ts","import":"./dist/index.mjs"},
    "./enums": {"types":"./src/enums.ts","import":"./dist/enums.mjs"}
  }`)
	got := parsePackageJSONExports("shared-lands-types", "packages/shared-lands-types", "./src/index.ts", "", "", "", exports, known)
	if got.exact["shared-lands-types/enums"] != "packages/shared-lands-types/src/enums.ts" {
		t.Fatalf("enums alias = %#v", got.exact)
	}
}

func TestParsePackageJSONExportsSkipsMissingTypes(t *testing.T) {
	known := map[string]bool{
		"packages/tracking-client/src/index.ts":        true,
		"packages/tracking-client/src/native/index.ts": true,
	}
	exports := json.RawMessage(`{
    ".": {"types":"./dist/index.d.ts","react-native":"./src/index.ts"},
    "./native": {"types":"./dist/native/index.d.ts","react-native":"./src/native/index.ts"}
  }`)
	got := parsePackageJSONExports("@onyxappio/tracking-client", "packages/tracking-client", "./dist/index.d.ts", "", "", "", exports, known)
	if got.exact["@onyxappio/tracking-client"] != "packages/tracking-client/src/index.ts" {
		t.Fatalf("root = %#v", got.exact)
	}
	if got.exact["@onyxappio/tracking-client/native"] != "packages/tracking-client/src/native/index.ts" {
		t.Fatalf("native = %#v", got.exact)
	}
}
