package graphinput

import (
	"fmt"
	"path/filepath"
	"testing"
)

// Compare admission reuse against a policy without admission proofs, including
// callers that supply a different directory bit than the original inventory.
func TestAdmissionReuseMatchesUncachedPolicy(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	for name, body := range map[string]string{
		".gitignore": "ignored/\n*.tmp\n",
		"src/a.ts":   "", "src/picture.PNG": "", "src/keep.tmp": "",
		"ignored/tracked.ts": "", "ignored/other.ts": "",
		"nested/.gitignore": "*.ts\n!keep.ts\n",
		"nested/drop.ts":    "", "nested/keep.ts": "",
		"excluded/deep/a.ts": "", "cache/deep/a.ts": "",
		"state/a.ts": "", "nested/node_modules/a.ts": "", "nested/yarn.lock": "",
	} {
		put(t, root, name, body)
	}
	gitTest(t, root, "add", "-f", "ignored/tracked.ts", "src/keep.tmp", "excluded/deep/a.ts")
	for _, opts := range []Options{
		{Exclude: []string{"excluded", "**/cache/**"}, StateDirs: []string{"state"}},
		{Exclude: []string{"excluded/**", "**/*.tmp"}, Semantic: []string{"**/*.PNG"}},
		{CacheExclusions: []string{}, ConservativeMedia: true},
	} {
		p := build(t, root, opts)
		for _, p := range []*Policy{p, p.WithConservativeMedia()} {
			uncached := *p
			uncached.entries = nil
			names := []string{".", "new.ts", "src/new.ts", "src/new/deep.ts", "src/new.PNG", "src/new.tmp", "src/node_modules/new.ts", "src/cache/a.ts", "src/.git/config", "excluded/deep/a.ts", "excluded/new/deep.ts", "cache/deep/a.ts", "cache/new/deep.ts", "state/a.ts", "state/new.ts", "nested/node_modules/a.ts", "nested/yarn.lock", "new/yarn.lock", "ignored/new.ts", "nested/drop.ts", ".git/config", "../outside.ts"}
			for name := range p.entries {
				names = append(names, name)
			}
			for _, name := range names {
				for _, name := range []string{name, filepath.Join(root, name)} {
					for _, dir := range []bool{false, true} {
						want := uncached.Classify(name, dir)
						rel, inRoot := p.relative(name)
						if _, known := p.entries[rel]; inRoot && known {
							want.Known = true
						}
						if got := p.Classify(name, dir); got != want {
							t.Errorf("%s directory=%v: got %+v want %+v", name, dir, got, want)
						}
						wantDep := uncached.ClassifyDependency(name)
						if _, known := p.entries[rel]; inRoot && known {
							wantDep.Known = true
						}
						if got := p.ClassifyDependency(name); got != wantDep {
							t.Errorf("dependency %s: got %+v want %+v", name, got, wantDep)
						}
					}
				}
			}
		}
	}
}

func BenchmarkPolicyAdmission(b *testing.B) {
	root := b.TempDir()
	p, err := Build(root, Options{Exclude: []string{"apps/mobile/e2e/artifacts/**", "worker-reports/**"}})
	if err != nil {
		b.Fatal(err)
	}
	names := make([]string, 1000)
	for i := range names {
		names[i] = fmt.Sprintf("packages/pkg%d/src/components/nested/file%d.ts", i%20, i)
		// Every synthetic path obeys the same hard-admission invariant as Build.
		if p.hard(names[i]) != "" {
			b.Fatal(names[i])
		}
		p.entries[names[i]] = false
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := p.Classify(names[i%len(names)], false); got.Kind != Semantic || !got.Known {
			b.Fatal(got)
		}
	}
}
