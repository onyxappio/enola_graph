package tsextractor

import (
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestIsMinifiedSource(t *testing.T) {
	longLine := "var x = \"" + strings.Repeat("z", minifiedLineThreshold+100) + "\";"
	if !isMinifiedSource([]byte(longLine)) {
		t.Errorf("isMinifiedSource(one very long line) = false, want true")
	}
	codeChunk := strings.Repeat("a();", minifiedLineThreshold/2)
	bundle := "/* Build */\n" + codeChunk + "\nfunction f(){}\n"
	if !isMinifiedSource([]byte(bundle)) {
		t.Errorf("isMinifiedSource(banner + long code line) = false, want true")
	}

	ordinary := "export function add(a, b) {\n  return a + b;\n}\n"
	if isMinifiedSource([]byte(ordinary)) {
		t.Errorf("isMinifiedSource(ordinary source) = true, want false")
	}
	manyLines := strings.Repeat("const x = compute();\n", 500)
	if isMinifiedSource([]byte(manyLines)) {
		t.Errorf("isMinifiedSource(many short lines) = true, want false")
	}

	commentPad := "export function Icon() {\n  return 1;\n  /* hist\n" + strings.Repeat("M", minifiedLineThreshold+50) + "\n  */\n}\n"
	if isMinifiedSource([]byte(commentPad)) {
		t.Errorf("long comment in a multi-line module must not skip the file")
	}
	stringPad := "export const PATH = \"" + strings.Repeat("M", minifiedLineThreshold+50) + "\";\nexport function draw() { return PATH; }\n"
	if isMinifiedSource([]byte(stringPad)) {
		t.Errorf("long string literal in a multi-line module must not skip the file")
	}
}

func TestExtract_SkipsMinifiedBundle(t *testing.T) {
	longLine := "var bundledLibrary = \"" + strings.Repeat("z", minifiedLineThreshold+100) + "\";"
	files := map[string]string{
		"src/util.ts":             "export function realHelper() {\n  return 1;\n}\n",
		"assets/vendor/bundle.js": longLine,
	}
	got := extractAll(t, files, false)

	// The hand-written symbol is extracted.
	if _, ok := findFact(got, "src.realHelper"); !ok {
		t.Errorf("expected symbol fact for src.realHelper; got %+v", got)
	}
	// The minified bundle contributes no facts at all — no symbols and no module
	// fact for its directory.
	for _, f := range got {
		if strings.Contains(f.File, "assets/vendor") || strings.Contains(f.Name, "assets/vendor") {
			t.Errorf("minified bundle produced a fact it should have been skipped: %+v", f)
		}
	}
	for _, m := range findFactsByKind(got, facts.KindModule) {
		if m.Name == "assets/vendor" {
			t.Errorf("minified-only directory should not emit a module fact; got %+v", m)
		}
	}
}
