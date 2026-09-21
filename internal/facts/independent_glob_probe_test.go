package facts

import (
	"fmt"
	"testing"
)

func TestIndependentGlobEquivalence(t *testing.T) {
	groups := map[string][]string{
		"escaped-extension": {`**/*.t\s`, `*.t\s`, `**/src/**/*.t\s`, `**/*.\[x\]`},
		"empty":             {"", "*", "**/", "/**"},
		"malformed":         {"[", "**/[", "**/*.t[", "**/*.ts\\", "**/foo[/**"},
		"ordinary":          {"**/*.ts", "**/foo/**", "**/*.Tests/**", "a/**/b.ts", "**/src/**/*.ts", "**/*", "**/foo\\/**"},
	}
	paths := []string{"", "a.ts", "src/a.ts", "x/src/a.ts", "a.t\\s", "a.[x]", "foo", "foo/a.ts", "foo\\/a.ts", "foo[", "x/foo[/a.ts", "a.Tests/b.ts", "a/b.ts", "/", "a/"}
	for name, pats := range groups {
		t.Run(name, func(t *testing.T) {
			lists := [][]string{nil, pats}
			for _, p := range pats {
				lists = append(lists, []string{p}, []string{p, "**/*"}, []string{"[", p, ""})
			}
			for i, list := range lists {
				for _, path := range paths {
					want, ok := MatchGlob(path, list)
					got, gok := CompileGlobs(list).Match(path)
					if want != got || ok != gok {
						t.Errorf("case %s path=%q patterns=%q original=(%q,%v) compiled=(%q,%v)", fmt.Sprint(i), path, list, want, ok, got, gok)
					}
				}
			}
		})
	}
}
