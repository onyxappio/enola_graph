package facts

import "testing"

// The anchored-literal prefix pre-pass in MatchAny is an optimization with no
// semantic budget, so these regressions spend a corpus on that rather than a few
// examples. The corpus is built from the shapes where a raw prefix comparison
// could drift from the matcher: unnormalized paths, backslashes, which are an
// escape to the matcher and an ordinary byte to a comparison, patterns that do
// not compile at all, non-ASCII names where byte and rune length differ, and
// above all a directory against a SIBLING that merely starts with its name.
var prefixFastPathPatterns = []string{
	// Anchored literal prefixes: the forms the pre-pass compiles.
	"vendor/**", "apps/mobile/e2e/artifacts/**", "worker-reports/**",
	"foo/**", "a/**", "a/b/**", "dist/**",

	// Globs and escapes in the prefix, and "/**/" forms: left to Match.
	"**/build/**", "**/*.Tests/**", "src/*/gen/**", "a?b/**", "a[bc]/**",
	`weird\*dir/**`, `a\b/**`, "**/spec/**/*_spec.rb", "**/src/test/**/*.scala",
	"a/**/b/**", "a/**/**", "vendor/**/*.go",

	// Basename globs and other non-prefix forms.
	"**/*.test.ts", "**/*_test.go", "**/*.png", "vendor/x.go",

	// Malformed or degenerate patterns.
	"[", "[]/**", "a[/**", "**/[z-a]/**", `\`, `\**`, "", "/**", "**", "**/",

	// Non-ASCII and dot-segment patterns.
	"ünïcode/**", "日本語/**", "emoji🙂/**", "./a/**", "../a/**",
}

var prefixFastPathPaths = []string{
	// Directory exact versus sibling prefix: the core trap.
	"foo", "foobar", "foo/bar", "foo.txt", "fo", "vendor", "vendors",
	"vendor/x", "vendor/x/y.go", "vendorx/y", "a", "ab", "a/b", "a/bc",
	"a/b/c", "dist", "distribution/x",

	// Unnormalized paths: dot segments, doubled, trailing and leading separators.
	"./vendor/x", "../vendor/x", "a/./b", "a/../b", "vendor//x", "vendor/",
	"/vendor/x", "//vendor", ".", "..", "", "/", "//",

	// Backslashes as ordinary path bytes, and literal asterisks in a path.
	`a\b`, `a\b/c`, `weird\*dir/x`, `vendor\x`, `\`,
	"a/**/b", "**/build/x", "**", "a/*/b",

	// Non-ASCII, where byte length and rune length differ.
	"ünïcode/f.ts", "ünïcod/f.ts", "ünïcodex", "日本語/x", "日本", "emoji🙂/y", "emoji🙂",

	// Realistic deep paths of the shape this optimizes.
	"apps/mobile/e2e/artifacts/run-1/screen.png", "apps/mobile/e2e/artifacts",
	"apps/mobile/e2e/artifactsx/a.png", "worker-reports/2026/r.json",
	"src/feature/deep/nest/leaf.test.ts", "internal/facts/pathglob_test.go",
	"spec/user_spec.rb", "src/test/scala/a/B.scala",
}

// prefixFastPathSets pairs the patterns into sets: each alone, the whole list in
// both orders so a compiled pattern is sometimes first and sometimes last, and
// sets where a pattern the pre-pass does NOT compile matches before one it does.
func prefixFastPathSets() [][]string {
	sets := make([][]string, 0, len(prefixFastPathPatterns)+8)
	for _, p := range prefixFastPathPatterns {
		sets = append(sets, []string{p})
	}
	all := append([]string(nil), prefixFastPathPatterns...)
	reversed := make([]string, len(all))
	for i, p := range all {
		reversed[len(all)-1-i] = p
	}
	return append(sets, all, reversed,
		[]string{"**/*.png", "apps/mobile/e2e/artifacts/**"},
		[]string{"**/build/**", "build/**"},
		[]string{"vendor/x/**", "vendor/**"},
		[]string{"**/*_test.go", "vendor/**", "**/*.png"},
		[]string{"**/spec/**/*_spec.rb", "spec/**"},
		[]string{"[", "vendor/**"},
		[]string{"a/**/**", "a/**"},
		nil,
	)
}

// TestMatchAnyPrefixFastPathPreservesBooleanSemantics is the no-regression
// property. Match is untouched by the pre-pass, so its boolean IS the behavior
// MatchAny had before; MatchGlob is checked alongside it so a divergence the
// compiled set already had cannot hide behind the compiled one.
func TestMatchAnyPrefixFastPathPreservesBooleanSemantics(t *testing.T) {
	pairs := 0
	for _, patterns := range prefixFastPathSets() {
		set := CompileGlobs(patterns)
		for _, path := range prefixFastPathPaths {
			got := set.MatchAny(path)
			if _, want := set.Match(path); got != want {
				t.Errorf("MatchAny(%q)=%v, compiled Match=%v, patterns=%q", path, got, want, patterns)
			}
			if want := MatchAnyGlob(path, patterns); got != want {
				t.Errorf("MatchAny(%q)=%v, MatchGlob=%v, patterns=%q", path, got, want, patterns)
			}
			pairs++
		}
	}
	t.Logf("compared %d pattern-set/path pairs", pairs)
}

// TestMatchPreservesFirstPatternUnderPrefixFastPath holds the half of the
// contract the pre-pass may not buy anything with: MatchAny can answer out of
// order, but Match must still name the pattern MatchGlob names, including when a
// compiled prefix sits behind a pattern that was not compiled.
func TestMatchPreservesFirstPatternUnderPrefixFastPath(t *testing.T) {
	for _, patterns := range prefixFastPathSets() {
		set := CompileGlobs(patterns)
		for _, path := range prefixFastPathPaths {
			got, gok := set.Match(path)
			want, wok := MatchGlob(path, patterns)
			if got != want || gok != wok {
				t.Errorf("Match(%q)=(%q,%v), MatchGlob=(%q,%v), patterns=%q", path, got, gok, want, wok, patterns)
			}
		}
	}
}

// TestMatchAnyPrefixFastPathRespectsDirectoryBoundary states the sibling trap
// directly rather than leaving it to be one case inside the corpus: "foo/**"
// takes foo and everything beneath it, and leaves every name that merely starts
// with those three bytes.
func TestMatchAnyPrefixFastPathRespectsDirectoryBoundary(t *testing.T) {
	set := CompileGlobs([]string{"foo/**"})
	for path, want := range map[string]bool{
		"foo": true, "foo/bar": true, "foo/b/c": true, "foo//bar": true,
		"foobar": false, "foo.txt": false, "foo-x/y": false, "fo": false,
		"barfoo/x": false, "xfoo": false, `foo\bar`: false, "./foo/bar": false,
	} {
		if got := set.MatchAny(path); got != want {
			t.Errorf("MatchAny(%q)=%v, want %v", path, got, want)
		}
	}
}
