package tsextractor

import (
	"testing"
)

func TestIndependentURLBoundary(t *testing.T) {
	src := []byte("const request = {$url: '/api/x', method: 'POST'};")
	got, want := extractHTTPClientFacts(src, "src/a.ts"), baselineExtractHTTPClientFacts(src, "src/a.ts")
	if !factsEqual(got, want) {
		t.Fatalf("prefilter changed baseline: got=%v want=%v", scanoptFactSummary(got), scanoptFactSummary(want))
	}
	if len(want) != 1 || want[0].Name != "/api/x" {
		t.Fatalf("baseline must emit route /api/x: %v", scanoptFactSummary(want))
	}
}

func TestHTTPClientPrefilterWordBoundariesMatchBaseline(t *testing.T) {
	cases := []struct {
		name, file, src string
	}{
		{
			name: "dollar-prefixed url property",
			file: "src/a.ts",
			src:  "const request = {$url: '/api/x', method: 'POST'};",
		},
		{
			name: "plain url property control",
			file: "src/a.ts",
			src:  "const request = {url: '/api/x', method: 'POST'};",
		},
		{
			name: "underscore-prefixed url is not a word boundary",
			file: "src/a.ts",
			src:  "const request = {_url: '/api/x', method: 'POST'};",
		},
		{
			name: "digit-prefixed url is not a word boundary",
			file: "src/a.ts",
			src:  "const request = {1url: '/api/x', method: 'POST'};",
		},
		{
			name: "identifier-suffixed url is not a word boundary",
			file: "src/a.ts",
			src:  "const request = {myurl: '/api/x', method: 'POST'};",
		},
		{
			name: "url followed by underscore is not url:",
			file: "src/a.ts",
			src:  "const request = {url_: '/api/x', method: 'POST'};",
		},
		{
			name: "url followed by dollar then colon does not match url\\s*:",
			file: "src/a.ts",
			src:  "const request = {url$: '/api/x', method: 'POST'};",
		},
		{
			name: "unicode prefix is a regexp word boundary",
			file: "src/a.ts",
			src:  "const request = {üurl: '/api/x', method: 'POST'};",
		},
		{
			name: "dollar-prefixed fetch matches (?:^|[^\\w])fetch",
			file: "src/a.ts",
			src:  "const request = $fetch('/api/x', { method: 'POST' });",
		},
		{
			name: "prefetch is not fetch",
			file: "src/a.ts",
			src:  "prefetch('/api/x', { method: 'POST' });",
		},
		{
			name: "refetch is not fetch",
			file: "src/a.ts",
			src:  "query.refetch('/api/x', { method: 'POST' });",
		},
		{
			name: "dollar-prefixed makeRequest",
			file: "src/a.ts",
			src:  "$makeRequest('/api/x', { method: 'PUT' });",
		},
		{
			name: "url property ident with dollar prefix",
			file: "src/a.ts",
			src:  "const path = '/api/x';\nconst request = {$url: path, method: 'POST'};",
		},
		{
			name: "spaced dollar url property",
			file: "src/a.ts",
			src:  "const request = {$url : '/api/x', method: 'POST'};",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			got := extractHTTPClientFacts(src, tc.file)
			want := baselineExtractHTTPClientFacts(src, tc.file)
			if !factsEqual(got, want) {
				t.Fatalf("prefilter changed baseline: got=%v want=%v", scanoptFactSummary(got), scanoptFactSummary(want))
			}
			if urlProperty.Match(src) && !hasURLPropertySignal(src) {
				t.Fatal("hasURLPropertySignal is not a necessary condition for \\burl\\s*:")
			}
			hasFetch, _, _, _ := possibleHTTPClientSignal(src)
			if httpClientCall.Match(src) && !hasFetch {
				t.Fatal("hasRegexpWordToken is not a necessary condition for (?:^|[^\\w])(fetch|makeRequest)")
			}
		})
	}
}
