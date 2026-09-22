package graphsession

import (
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestFileInvalidationPlan(t *testing.T) {
	deps := map[string][]string{"a.ts": {"b.ts"}, "b.ts": {"c.ts"}, "c.ts": {"b.ts"}}
	p, err := planFileInvalidation([]string{"c.ts", "c.ts"}, nil, nil, deps, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []graphstream.OwnerRef{{Kind: "file", ID: "a.ts"}, {Kind: "file", ID: "b.ts"}, {Kind: "file", ID: "c.ts"}}
	if !reflect.DeepEqual(p.manifest(), want) {
		t.Fatalf("scope: %v", p.manifest())
	}
	copy := p.manifest()
	copy[0].ID = "injected.ts"
	if !reflect.DeepEqual(p.manifest(), want) || p.digest != graphstream.DigestOwners(want) {
		t.Fatal("manifest mutated")
	}
	if p.check(graphstream.OwnerRef{Kind: "file", ID: "injected.ts"}) == nil {
		t.Fatal("accepted owner outside scope")
	}
	if p.check(graphstream.OwnerRef{Kind: "synthetic", ID: "a.ts"}) == nil {
		t.Fatal("accepted synthetic owner")
	}
}

func TestGrowScopeOutOfScopeOwnerFailsClosed(t *testing.T) {
	p, err := planFileInvalidation([]string{"a.ts"}, nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{opts: Options{AuthoritativeFiles: true}, plan: p}
	s.growScope([]graphstream.OwnerRef{{Kind: graphstream.OwnerFile, ID: "apps/architect-console/src/server/app.ts"}})
	got := s.fileLocalErr()
	if got == nil {
		t.Fatal("out-of-scope owner did not fail closed")
	}
	want := "invalidation plan: owner file:apps/architect-console/src/server/app.ts outside frozen scope"
	if got.Error() != want {
		t.Fatalf("localErr = %q, want %q", got.Error(), want)
	}
	s.growScope([]graphstream.OwnerRef{{Kind: graphstream.OwnerFile, ID: "a.ts"}})
	if s.fileLocalErr().Error() != want {
		t.Fatal("fail-closed localErr was cleared by a later in-scope growScope")
	}
}

func TestFileInvalidationFallbackRetainsDeletedAndRenamedOwners(t *testing.T) {
	p, err := planFileInvalidation([]string{"old.ts", "new.ts"}, []string{"old.ts", "empty.ts"}, []string{"new.ts", "consumer.ts"}, nil, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.ts", "new.ts", "empty.ts", "consumer.ts"} {
		if err := p.check(graphstream.OwnerRef{Kind: "file", ID: name}); err != nil {
			t.Fatal(err)
		}
	}
	// Fallback must not manufacture an idle replacement.
	idle, err := planFileInvalidation(nil, []string{"old.ts"}, []string{"new.ts"}, nil, true, nil)
	if err != nil || len(idle.manifest()) != 0 {
		t.Fatalf("idle: %v %v", idle, err)
	}
}

func TestFileInvalidationFallbackClosesDependentsOutsideDomain(t *testing.T) {
	p, err := planFileInvalidation([]string{"pkg/a.ts"}, nil, nil, map[string][]string{"app.ts": {"pkg/b.ts"}}, true, []string{"pkg/a.ts", "pkg/b.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.check(graphstream.OwnerRef{Kind: "file", ID: "app.ts"}); err != nil {
		t.Fatal(err)
	}
}

func TestFileInvalidationRejectsInvalidPaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../a.ts", "/a.ts", "a/../b.ts", "a\\b.ts", "a\x00.ts", "C:/foo", "c:/windows/a.ts", "C:foo"} {
		if _, err := planFileInvalidation([]string{name}, nil, nil, nil, false, nil); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
}

func TestAuthoritativeFilePlanUsesChangedFilesAndReverseDependents(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts", "src/c.ts", "src/independent.ts"}
	current := append([]string(nil), prev...)
	state := map[string]*FileState{
		"src/a.ts":           {Hash: "a1", TS: &tsextractor.FileRecord{File: "src/a.ts", ResolvedFiles: []string{"src/b.ts"}}},
		"src/b.ts":           {Hash: "b1", TS: &tsextractor.FileRecord{File: "src/b.ts", ResolvedFiles: []string{"src/c.ts"}}},
		"src/c.ts":           {Hash: "c1", TS: &tsextractor.FileRecord{File: "src/c.ts"}},
		"src/independent.ts": {Hash: "i1", TS: &tsextractor.FileRecord{File: "src/independent.ts"}},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1", "src/c.ts": "c2", "src/independent.ts": "i1"}
	p, reason, err := authoritativeFilePlan(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeReverseClose {
		t.Fatalf("reason = %q", reason)
	}
	want := []graphstream.OwnerRef{{Kind: graphstream.OwnerFile, ID: "src/a.ts"}, {Kind: graphstream.OwnerFile, ID: "src/b.ts"}, {Kind: graphstream.OwnerFile, ID: "src/c.ts"}}
	if !reflect.DeepEqual(p.manifest(), want) {
		t.Fatalf("scope = %v, want %v", p.manifest(), want)
	}
	if p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/independent.ts"}) == nil {
		t.Fatal("independent file leaked into narrowed scope")
	}
}

func TestAuthoritativeFilePlanKeepsWholeDomainForGlobalFallback(t *testing.T) {
	prev := []string{"a.ts", "b.ts"}
	current := []string{"a.ts", "b.ts"}
	p, reason, err := authoritativeFilePlan(prev, current, nil, map[string]string{"a.ts": "a", "b.ts": "b"}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeWholeDomain {
		t.Fatalf("reason = %q", reason)
	}
	if len(p.manifest()) != 2 {
		t.Fatalf("scope = %v, want whole domain", p.manifest())
	}
}

func TestAuthoritativeFilePlanMembershipUsesWholeDomain(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts"}
	current := []string{"src/a.ts", "src/b.ts", "src/new.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{File: "src/a.ts", ResolvedFiles: []string{"src/b.ts"}, Declared: []string{"A"}}},
		"src/b.ts": {Hash: "b1", TS: &tsextractor.FileRecord{File: "src/b.ts", Declared: []string{"B"}}},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1", "src/new.ts": "n1"}
	p, reason, err := authoritativeFilePlan(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembership {
		t.Fatalf("reason = %q", reason)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}); err != nil {
		t.Fatal(err)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/b.ts"}); err != nil {
		t.Fatal(err)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/new.ts"}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthoritativeFilePlanDeleteKeepsRetiredOwner(t *testing.T) {
	prev := []string{"gone.ts", "kept.ts"}
	current := []string{"kept.ts"}
	state := map[string]*FileState{
		"gone.ts": {Hash: "g1", TS: &tsextractor.FileRecord{File: "gone.ts", Declared: []string{"Gone"}}},
		"kept.ts": {Hash: "k1", TS: &tsextractor.FileRecord{File: "kept.ts"}},
	}
	p, reason, err := authoritativeFilePlan(prev, current, state, map[string]string{"kept.ts": "k1"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembership {
		t.Fatalf("reason = %q", reason)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "gone.ts"}); err != nil {
		t.Fatal(err)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "kept.ts"}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnersForNameDeltaIncludesAllExtractorReferencers(t *testing.T) {
	prev := map[string]*FileState{
		"hmac.ts": {
			Hash: "h1",
			TS: &tsextractor.FileRecord{
				File:     "hmac.ts",
				Declared: []string{"pkg.hmacEquals"},
				Facts: []facts.Fact{{
					Kind: facts.KindSymbol, Name: "pkg.hmacEquals", File: "hmac.ts",
					Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: "pkg"}},
				}},
			},
		},
		"index.ts": {
			Hash: "i1",
			TS: &tsextractor.FileRecord{
				File: "index.ts",
				Facts: []facts.Fact{{
					Kind: facts.KindDependency, Name: "reexport hmacEquals", File: "index.ts",
					Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "pkg.hmacEquals"}},
				}},
			},
		},
		"md.md": {
			Hash: "m1",
			Facts: []facts.Fact{{
				Kind: facts.KindSymbol, Name: "md.md", File: "md.md",
				Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "pkg.hmacEquals"}},
			}},
		},
		"other.ts": {Hash: "o1", TS: &tsextractor.FileRecord{File: "other.ts", Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "pkg.other", File: "other.ts"}}}},
	}
	newFacts := map[string][]facts.Fact{
		"hmac.ts": {{
			Kind: facts.KindSymbol, Name: "pkg.hmacEqualSafe", File: "hmac.ts",
			Relations: []facts.Relation{{Kind: facts.RelDeclares, Target: "pkg"}},
		}},
	}
	got := ownersForNameDelta(prev, map[string]bool{"hmac.ts": true}, newFacts)
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	if !seen["hmac.ts"] || !seen["index.ts"] || !seen["md.md"] {
		t.Fatalf("name-delta owners %v", got)
	}
	if seen["other.ts"] {
		t.Fatalf("unrelated owner in name-delta %v", got)
	}
	total, withRel := relationBearingOwnerCount(prev)
	if total != 4 || withRel < 2 {
		t.Fatalf("coarse width total=%d withRel=%d", total, withRel)
	}
}

func TestScanHashEquivalentMigratesLegacyWithoutTreatingAsChange(t *testing.T) {
	hashes := map[string]string{"a.ts": "h1", "package-lock.json": "lock"}
	semantic := inventoryDigest([]string{"a.ts"}, hashes)
	legacy := inventoryDigest([]string{"a.ts", "package-lock.json"}, hashes)
	st := &State{ScanHash: legacy}
	if !scanHashEquivalent(st, []string{"a.ts", "package-lock.json"}, []string{"a.ts"}, hashes, semantic) {
		t.Fatal("legacy AllNames digest should be equivalent for migration")
	}
	st.ScanHashVersion = authoritativeScanHashVersion
	st.ScanHash = semantic
	if !scanHashEquivalent(st, []string{"a.ts", "package-lock.json"}, []string{"a.ts"}, hashes, semantic) {
		t.Fatal("current semantic digest should match")
	}
	st.ScanHash = "other"
	if scanHashEquivalent(st, []string{"a.ts", "package-lock.json"}, []string{"a.ts"}, hashes, semantic) {
		t.Fatal("mismatched versioned hash treated as equivalent")
	}
}

func TestIncompleteDependencyRecordsRequireFallback(t *testing.T) {
	if !incompleteDependencyRecords(map[string]*FileState{
		"a.ts": {TS: &tsextractor.FileRecord{ImportSpecs: []string{"./b"}}},
	}) {
		t.Fatal("incomplete import resolution did not require fallback")
	}
	if incompleteDependencyRecords(map[string]*FileState{
		"a.ts": {TS: &tsextractor.FileRecord{ImportSpecs: []string{"./b"}, ImportComplete: true}},
	}) {
		t.Fatal("complete external-only resolution required fallback")
	}
}
