package graphinput

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// A nil memo evaluates every decision directly and takes no ancestor-chain
// short-circuit, so trackedAdmissionWith(nil) runs the pre-memoization algorithm
// exactly. That makes it the reference this file differentiates against: the two
// must agree on the admitted files, the admitted directories and the admitDirs
// set they leave behind, over fixtures built to exercise what a cache can break.
func admissionBothWays(t *testing.T, p *Policy) {
	t.Helper()

	wantFiles, wantDirs := p.trackedAdmissionWith(nil)
	wantAdmit := map[string]bool{}
	for k, v := range p.admitDirs {
		wantAdmit[k] = v
	}

	gotFiles, gotDirs := p.trackedAdmissionWith(newPolicyMemo())
	gotAdmit := p.admitDirs

	if !reflect.DeepEqual(wantFiles, gotFiles) {
		t.Errorf("admitted files differ:\n uncached=%#v\n memoized=%#v", wantFiles, gotFiles)
	}
	if !reflect.DeepEqual(wantDirs, gotDirs) {
		t.Errorf("admitted dirs differ:\n uncached=%#v\n memoized=%#v", wantDirs, gotDirs)
	}
	if !reflect.DeepEqual(wantAdmit, gotAdmit) {
		t.Errorf("admitDirs differ:\n uncached=%#v\n memoized=%#v", wantAdmit, gotAdmit)
	}
}

// memoFixture writes a tree whose tracked names share long ancestor chains, so
// the ancestor loop revisits the same directories many times over - which is
// both the redundancy the memo removes and the thing the short-circuit could
// get wrong.
func memoFixture(t *testing.T, root string) {
	t.Helper()
	put(t, root, ".gitignore", "blocked/\n*.tmp\n!keep.tmp\nshared/\n")
	put(t, root, "shared/.gitignore", "*.gen.ts\n!pinned.gen.ts\n")
	put(t, root, "shared/deep/.gitignore", "!revived.gen.ts\n")

	// One deep chain under many leaves: every leaf walks the same ancestors.
	for i := 0; i < 40; i++ {
		put(t, root, fmt.Sprintf("shared/deep/nest/a/b/c/leaf%d.ts", i), "export const x=1")
		put(t, root, fmt.Sprintf("shared/deep/nest/a/b/other%d.ts", i), "export const y=1")
	}
	put(t, root, "shared/deep/nest/a/b/c/revived.gen.ts", "export const z=1")
	put(t, root, "shared/deep/pinned.gen.ts", "export const p=1")
	put(t, root, "shared/deep/dropped.gen.ts", "export const q=1")

	for _, p := range []string{
		"blocked/deep/x.ts", "blocked/deep/more/y.ts",
		"keep.tmp", "drop.tmp", "pnpm-lock.yaml",
		".state/s.ts", ".state/deep/s2.ts",
		"cache/c.ts", "cache/deep/c2.ts",
		"hard/h.ts", "hard/deep/h2.ts",
		"plain/ok.ts",
	} {
		put(t, root, p, "x")
	}
}

// Each option set moves a different rule that the memo caches: the default set
// exercises gitignore alone, the second adds hard exclusions that overlap the
// ignored subtrees, and the third moves state and cache exclusions underneath
// them, so a cached hard verdict reused across option sets would be caught.
func memoOptionSets() []Options {
	return []Options{
		{},
		{Exclude: []string{"hard/**", "blocked/**"}},
		{Exclude: []string{"**/*.tmp", "shared/deep/nest/a/b/c/**"}, StateDirs: []string{".state"}, CacheExclusions: []string{"cache/**"}},
		{Exclude: []string{"shared/**"}, CacheExclusions: []string{"shared/deep/**"}},
	}
}

func TestPolicyMemoMatchesUncachedAdmission(t *testing.T) {
	for i, opts := range memoOptionSets() {
		t.Run(fmt.Sprintf("options%d", i), func(t *testing.T) {
			root := t.TempDir()
			gitTest(t, root, "init", "-q")
			memoFixture(t, root)
			gitTest(t, root, "add", "-f", ".")
			admissionBothWays(t, build(t, root, opts))
		})
	}
}

// The memoized decision functions must answer exactly what the direct ones do,
// for every tracked name and every ancestor directory of one - the dir queries
// are the ones the ancestor loop makes, and they are the bulk of the calls.
func TestPolicyMemoDecisionsMatchDirect(t *testing.T) {
	for i, opts := range memoOptionSets() {
		t.Run(fmt.Sprintf("options%d", i), func(t *testing.T) {
			root := t.TempDir()
			gitTest(t, root, "init", "-q")
			memoFixture(t, root)
			gitTest(t, root, "add", "-f", ".")
			p := build(t, root, opts)

			names := map[string]bool{}
			for name := range p.tracked {
				names[name] = true
				for dir := filepath.ToSlash(filepath.Dir(name)); dir != "."; dir = filepath.ToSlash(filepath.Dir(dir)) {
					names[dir] = true
				}
			}
			if len(names) == 0 {
				t.Fatal("fixture produced no tracked names")
			}

			m := newPolicyMemo()
			ordered := make([]string, 0, len(names))
			for n := range names {
				ordered = append(ordered, n)
			}
			sort.Strings(ordered)
			// Twice over, so a second lookup is served from the cache and is
			// compared against the direct answer just like the first.
			for pass := 0; pass < 2; pass++ {
				for _, n := range ordered {
					if got, want := p.hardMemo(m, n), p.hard(n); got != want {
						t.Fatalf("pass %d: hard(%q)=%q memoized=%q", pass, n, want, got)
					}
					if got, want := p.gitIgnoredMemo(m, n), p.gitIgnored(n); got != want {
						t.Fatalf("pass %d: gitIgnored(%q)=%v memoized=%v", pass, n, want, got)
					}
				}
			}
		})
	}
}

// A negated pattern in a nested .gitignore revives a file whose parent
// directory stays ignored, so the per-name answers along one chain genuinely
// differ. A memo keyed on anything coarser than the full name would collapse
// them, and the digests would move.
func TestPolicyMemoKeepsNestedIgnoreDistinctions(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	put(t, root, ".gitignore", "shared/\n")
	put(t, root, "shared/.gitignore", "*.gen.ts\n!pinned.gen.ts\n")
	put(t, root, "shared/pinned.gen.ts", "export const a=1")
	put(t, root, "shared/dropped.gen.ts", "export const b=1")
	put(t, root, "shared/deep/pinned.gen.ts", "export const c=1")
	gitTest(t, root, "add", "-f", ".")

	p := build(t, root, Options{})
	m := newPolicyMemo()
	for _, name := range []string{"shared", "shared/pinned.gen.ts", "shared/dropped.gen.ts", "shared/deep", "shared/deep/pinned.gen.ts"} {
		if got, want := p.gitIgnoredMemo(m, name), p.gitIgnored(name); got != want {
			t.Errorf("gitIgnored(%q)=%v memoized=%v", name, want, got)
		}
	}
	admissionBothWays(t, p)
}

// A hard exclusion is evaluated before the tracked override and wins over it, so
// a tracked, ignored name under a hard-excluded subtree must stay out of both
// lists. Caching hard must not let one in.
func TestPolicyMemoKeepsHardExclusionAheadOfOverride(t *testing.T) {
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	put(t, root, ".gitignore", "vendored/\n")
	for i := 0; i < 20; i++ {
		put(t, root, fmt.Sprintf("vendored/deep/pkg/f%d.ts", i), "export const x=1")
	}
	put(t, root, "vendored/pnpm-lock.yaml", "lock")
	gitTest(t, root, "add", "-f", ".")

	p := build(t, root, Options{Exclude: []string{"vendored/deep/**"}})
	files, dirs := p.trackedAdmissionWith(newPolicyMemo())
	for _, f := range files {
		if filepath.ToSlash(f) == "vendored/pnpm-lock.yaml" {
			t.Error("a lockfile reached the admitted files")
		}
		if len(f) >= len("vendored/deep/") && f[:len("vendored/deep/")] == "vendored/deep/" {
			t.Errorf("hard-excluded file was admitted: %q", f)
		}
	}
	for _, d := range dirs {
		if len(d) >= len("vendored/deep") && d[:len("vendored/deep")] == "vendored/deep" {
			t.Errorf("hard-excluded directory was admitted: %q", d)
		}
	}
	admissionBothWays(t, p)
}

// Both digests are the artifact this change must not move, and computing them
// twice over one immutable policy must land on the same bytes.
func TestPolicyMemoIdentitiesStable(t *testing.T) {
	for i, opts := range memoOptionSets() {
		t.Run(fmt.Sprintf("options%d", i), func(t *testing.T) {
			root := t.TempDir()
			gitTest(t, root, "init", "-q")
			memoFixture(t, root)
			gitTest(t, root, "add", "-f", ".")
			p := build(t, root, opts)

			id, adm := p.Identity(), p.AdmissionIdentity()
			if id == "" || adm == "" {
				t.Fatal("policy built without identities")
			}
			for pass := 0; pass < 3; pass++ {
				if err := p.computeIdentities(); err != nil {
					t.Fatal(err)
				}
				if p.Identity() != id || p.AdmissionIdentity() != adm {
					t.Fatalf("pass %d moved a digest", pass)
				}
			}
		})
	}
}
