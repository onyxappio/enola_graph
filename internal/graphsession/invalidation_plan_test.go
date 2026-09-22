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

func TestAuthoritativePlanSeedsNameDependentOutsideReverseClose(t *testing.T) {
	prev := map[string]*FileState{
		"packages/crypto/src/password.ts": {
			Hash: "1", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File: "packages/crypto/src/password.ts", Declared: []string{"normalizeEmail"},
			},
		},
		"apps/architect-console/src/server/app.ts": {
			Hash: "2", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File:       "apps/architect-console/src/server/app.ts",
				Declared:   []string{"listen"},
				Referenced: []string{"normalizeEmail", "fastify"},
			},
		},
		"independent.ts": {
			Hash: "3", Extractor: "typescript",
			TS: &tsextractor.FileRecord{File: "independent.ts", Declared: []string{"i"}},
		},
	}
	previous := []string{"packages/crypto/src/password.ts", "apps/architect-console/src/server/app.ts", "independent.ts"}
	hashes := map[string]string{"packages/crypto/src/password.ts": "changed", "apps/architect-console/src/server/app.ts": "2", "independent.ts": "3"}
	p, reason, err := planForTest(previous, previous, prev, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeNameDelta {
		t.Fatalf("reason=%s, want the name-delta scope: app.ts is seeded, not a reason to widen to the whole domain", reason)
	}
	if !p.member["apps/architect-console/src/server/app.ts"] {
		t.Fatal("post-parse name dependent omitted from Begin")
	}
	if !p.member["packages/crypto/src/password.ts"] {
		t.Fatal("dirty file omitted from Begin")
	}
	if p.member["independent.ts"] {
		t.Fatalf("scope %v widened past the name dependents", p.manifest())
	}
}

func TestDirtyRouterMountChildrenBeforeBegin(t *testing.T) {
	prev := map[string]*FileState{
		"src/server.ts": {
			Hash: "1", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File: "src/server.ts",
				Router: &tsextractor.RouterDTO{
					RelFile: "src/server.ts",
					Mounts:  []tsextractor.MountDTO{{File: "src/server.ts", Parent: "app", Prefix: "/api", Child: "ordersRouter"}},
					Imports: map[string]tsextractor.ImportRefDTO{"ordersRouter": {File: "src/api/orders.ts", Export: "default"}},
				},
			},
		},
		"src/api/orders.ts": {Hash: "2", Extractor: "typescript", TS: &tsextractor.FileRecord{File: "src/api/orders.ts"}},
	}
	dirty := map[string]bool{"src/server.ts": true}
	newRecs := map[string]*tsextractor.FileRecord{
		"src/server.ts": {
			File: "src/server.ts",
			Router: &tsextractor.RouterDTO{
				RelFile: "src/server.ts",
				Mounts:  []tsextractor.MountDTO{{File: "src/server.ts", Parent: "app", Prefix: "/v2", Child: "ordersRouter"}},
				Imports: map[string]tsextractor.ImportRefDTO{"ordersRouter": {File: "src/api/orders.ts", Export: "default"}},
			},
		},
	}
	got := dirtyRouterMountChildren(prev, dirty, newRecs, nil)
	found := false
	for _, id := range got {
		if id == "src/api/orders.ts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mount prefix change omitted child: %v", got)
	}
	unchanged := dirtyRouterMountChildren(prev, dirty, map[string]*tsextractor.FileRecord{"src/server.ts": prev["src/server.ts"].TS}, nil)
	if len(unchanged) != 0 {
		t.Fatalf("unchanged mounts produced children %v", unchanged)
	}
}

func nestedMountStates() map[string]*FileState {
	return map[string]*FileState{
		"src/server.ts": {
			Hash: "1", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File: "src/server.ts",
				Router: &tsextractor.RouterDTO{
					RelFile: "src/server.ts",
					Roots:   map[string]bool{"app": true},
					Mounts:  []tsextractor.MountDTO{{File: "src/server.ts", Parent: "app", Prefix: "/api", Child: "apiRouter"}},
					Imports: map[string]tsextractor.ImportRefDTO{"apiRouter": {File: "src/api.ts", Export: "default"}},
				},
			},
		},
		"src/api.ts": {
			Hash: "2", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File: "src/api.ts",
				Router: &tsextractor.RouterDTO{
					RelFile: "src/api.ts",
					Routers: map[string]bool{"router": true},
					Exports: map[string]string{"default": "router"},
					Mounts:  []tsextractor.MountDTO{{File: "src/api.ts", Parent: "router", Prefix: "/v1", Child: "ordersRouter"}},
					Imports: map[string]tsextractor.ImportRefDTO{"ordersRouter": {File: "src/api/orders.ts", Export: "default"}},
				},
			},
		},
		"src/api/orders.ts": {
			Hash: "3", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File:  "src/api/orders.ts",
				Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "listOrders", File: "src/api/orders.ts"}},
				Router: &tsextractor.RouterDTO{
					RelFile: "src/api/orders.ts",
					Routers: map[string]bool{"router": true},
					Exports: map[string]string{"default": "router"},
					Pending: map[string][]tsextractor.PendingRouteDTO{
						"router": {{Verb: "GET", Path: "/orders", Line: 4, Framework: "express"}},
					},
				},
			},
		},
		"packages/crypto/src/password.ts": {
			Hash: "4", Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File:  "packages/crypto/src/password.ts",
				Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "normalizeEmail", File: "packages/crypto/src/password.ts"}},
			},
		},
	}
}

func TestDirtyRouterMountChildrenNestedBeforeBegin(t *testing.T) {
	prev := nestedMountStates()
	dirty := map[string]bool{"src/server.ts": true}
	newRecs := map[string]*tsextractor.FileRecord{
		"src/server.ts": {
			File: "src/server.ts",
			Router: &tsextractor.RouterDTO{
				RelFile: "src/server.ts",
				Roots:   map[string]bool{"app": true},
				Mounts:  []tsextractor.MountDTO{{File: "src/server.ts", Parent: "app", Prefix: "/v2", Child: "apiRouter"}},
				Imports: map[string]tsextractor.ImportRefDTO{"apiRouter": {File: "src/api.ts", Export: "default"}},
			},
		},
	}
	got := dirtyRouterMountChildren(prev, dirty, newRecs, nil)
	found := map[string]bool{}
	for _, id := range got {
		found[id] = true
	}
	if !found["src/api.ts"] || !found["src/api/orders.ts"] {
		t.Fatalf("nested mount omitted descendants: %v", got)
	}
}

func TestComposedRouteOwnerDeltaNestedMounts(t *testing.T) {
	prev := nestedMountStates()
	prev["src/consumer.ts"] = &FileState{
		Hash: "5", Extractor: "typescript",
		TS: &tsextractor.FileRecord{
			File: "src/consumer.ts",
			Facts: []facts.Fact{{
				Kind: facts.KindSymbol, Name: "routeConsumer", File: "src/consumer.ts",
				Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "/api/v1/orders"}},
			}},
		},
	}
	prev["src/new-consumer.ts"] = &FileState{
		Hash: "6", Extractor: "typescript",
		TS: &tsextractor.FileRecord{
			File: "src/new-consumer.ts",
			Facts: []facts.Fact{{
				Kind: facts.KindSymbol, Name: "newRouteConsumer", File: "src/new-consumer.ts",
				Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "/v2/v1/orders"}},
			}},
		},
	}
	dirty := map[string]bool{"src/server.ts": true}
	newRecs := map[string]*tsextractor.FileRecord{
		"src/server.ts": {
			File: "src/server.ts",
			Router: &tsextractor.RouterDTO{
				RelFile: "src/server.ts",
				Roots:   map[string]bool{"app": true},
				Mounts:  []tsextractor.MountDTO{{File: "src/server.ts", Parent: "app", Prefix: "/v2", Child: "apiRouter"}},
				Imports: map[string]tsextractor.ImportRefDTO{"apiRouter": {File: "src/api.ts", Export: "default"}},
			},
		},
	}
	got := composedRouteOwnerDelta(prev, dirty, newRecs, nil)
	found := map[string]bool{}
	for _, id := range got {
		found[id] = true
	}
	if !found["src/api/orders.ts"] {
		t.Fatalf("composed nested child omitted: %v", got)
	}
	if !found["src/consumer.ts"] {
		t.Fatalf("consumer of old composed route candidate omitted: %v", got)
	}
	if !found["src/new-consumer.ts"] {
		t.Fatalf("consumer of new composed route candidate omitted: %v", got)
	}
	if found["packages/crypto/src/password.ts"] {
		t.Fatalf("unrelated owner entered composed route delta: %v", got)
	}
	bodyDirty := map[string]bool{"packages/crypto/src/password.ts": true}
	bodyNew := map[string]*tsextractor.FileRecord{
		"packages/crypto/src/password.ts": {
			File:  "packages/crypto/src/password.ts",
			Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "normalizeEmail", File: "packages/crypto/src/password.ts"}},
		},
	}
	if extra := composedRouteOwnerDelta(prev, bodyDirty, bodyNew, nil); len(extra) != 0 {
		t.Fatalf("body edit grew composed route owners %v", extra)
	}
}

func TestComposedRouteFactsBlankVersusTaggedRepo(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	blank := []facts.Fact{{Kind: facts.KindRoute, Name: "/health", File: app, Relations: []facts.Relation{{Kind: "handler", Target: "listen"}}}}
	tagged := []facts.Fact{{Repo: "product-scope", Kind: facts.KindRoute, Name: "/health", File: app, Relations: []facts.Relation{{Kind: "handler", Target: "listen"}}}}
	scope := map[string]bool{}
	addChangedRouteFiles(scope, append(blank, tsextractor.ComposedMountRoutes(nil)...), append(tagged, tsextractor.ComposedMountRoutes(nil)...))
	if len(scope) != 0 {
		t.Fatalf("blank vs tagged composed domain grew scope %v", scope)
	}
}

func TestDependencyIndexProvenIgnoresUnresolvedSpecs(t *testing.T) {
	prev := map[string]*FileState{
		"a.ts": {TS: &tsextractor.FileRecord{File: "a.ts", Declared: []string{"a"}, ResolvedFiles: []string{"b.ts"}}},
		"b.ts": {TS: &tsextractor.FileRecord{File: "b.ts", Declared: []string{"b"}}},
	}
	if !dependencyIndexProven(prev) {
		t.Fatal("small resolved graph should be treated as proven")
	}
	prev["c.ts"] = &FileState{TS: &tsextractor.FileRecord{File: "c.ts", UnresolvedSpecs: []string{"./theme.css"}}}
	if !dependencyIndexProven(prev) {
		t.Fatal("unresolved CSS/external specs must not unprove reverse-close")
	}
	incomplete := map[string]*FileState{
		"a.ts": {TS: &tsextractor.FileRecord{File: "a.ts", ImportSpecs: []string{"./b"}}},
	}
	if dependencyIndexProven(incomplete) {
		t.Fatal("incomplete import records must not prove completeness")
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

// planForTest derives the membership delta the same way session.go does, from a
// fixture whose current file list is also the whole inventory. Production passes
// inv.Files, which can be wider than the policy-filtered current list.
func planForTest(previous, current []string, prevFiles map[string]*FileState, hashes map[string]string, wholeDomain bool, extraOwners []string) (*fileInvalidationPlan, string, error) {
	return authoritativeFilePlan(previous, current, prevFiles, hashes, wholeDomain, extraOwners,
		membershipScope(previous, current, current, prevFiles))
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
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
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
	p, reason, err := planForTest(prev, current, nil, map[string]string{"a.ts": "a", "b.ts": "b"}, true, nil)
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

// src/a.ts carries resolved edges but no cached specifiers, so its resolution
// cannot be replayed against the new file set and membership must widen.
func TestAuthoritativeFilePlanMembershipWithoutCachedSpecsUsesWholeDomain(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts"}
	current := []string{"src/a.ts", "src/b.ts", "src/new.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{File: "src/a.ts", ResolvedFiles: []string{"src/b.ts"}, Declared: []string{"A"}}},
		"src/b.ts": {Hash: "b1", TS: &tsextractor.FileRecord{File: "src/b.ts", Declared: []string{"B"}}},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1", "src/new.ts": "n1"}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
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
	p, reason, err := planForTest(prev, current, state, map[string]string{"kept.ts": "k1"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	// The retired owner stays in scope so its prior contribution is cleared.
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "gone.ts"}); err != nil {
		t.Fatal(err)
	}
	// kept.ts neither imports nor references gone.ts, so it is not republished.
	if p.member["kept.ts"] {
		t.Fatalf("plan = %v, want kept.ts excluded", p.member)
	}
}

// Adding a file nothing resolves to must not pull unrelated owners into Begin.
func TestAuthoritativeFilePlanUnrelatedAddStaysBounded(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts"}
	current := []string{"src/a.ts", "src/b.ts", "src/helper.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{
			File: "src/a.ts", ImportSpecs: []string{"src/b"}, ResolvedFiles: []string{"src/b.ts"},
			Declared: []string{"A"}, ImportComplete: true,
		}},
		"src/b.ts": {Hash: "b1", TS: &tsextractor.FileRecord{File: "src/b.ts", Declared: []string{"B"}, ImportComplete: true}},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1", "src/helper.ts": "h1"}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/helper.ts"}); err != nil {
		t.Fatal(err)
	}
	if len(p.manifest()) != 1 {
		t.Fatalf("manifest = %v, want only the added file", p.manifest())
	}
}

// A previously unresolved internal import becomes satisfiable once its folder
// index is added, so the importer must already be a frozen owner.
func TestAuthoritativeFilePlanAddedIndexSatisfiesUnresolvedImport(t *testing.T) {
	prev := []string{"src/consumer.ts", "src/other.ts"}
	current := []string{"src/consumer.ts", "src/other.ts", "src/widgets/index.ts"}
	state := map[string]*FileState{
		"src/consumer.ts": {Hash: "c1", TS: &tsextractor.FileRecord{
			File: "src/consumer.ts", ImportSpecs: []string{"src/widgets"},
			UnresolvedSpecs: []string{"src/widgets"}, ImportComplete: true,
		}},
		"src/other.ts": {Hash: "o1", TS: &tsextractor.FileRecord{
			File: "src/other.ts", ImportSpecs: []string{"react"}, ImportComplete: true,
		}},
	}
	hashes := map[string]string{"src/consumer.ts": "c1", "src/other.ts": "o1", "src/widgets/index.ts": "w1"}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	for _, want := range []string{"src/consumer.ts", "src/widgets/index.ts"} {
		if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: want}); err != nil {
			t.Fatal(err)
		}
	}
	if p.member["src/other.ts"] {
		t.Fatalf("plan = %v, want src/other.ts excluded", p.member)
	}
}

// An already resolved specifier rebinds when the added path wins the extension
// precedence over the folder index it used to resolve to. Nothing about the
// importer's own bytes changed, so only the resolution replay can find it.
func TestAuthoritativeFilePlanAddedFileWinsOverExistingIndex(t *testing.T) {
	prev := []string{"src/consumer.ts", "src/foo/index.ts", "src/other.ts"}
	current := []string{"src/consumer.ts", "src/foo/index.ts", "src/other.ts", "src/foo.ts"}
	state := map[string]*FileState{
		"src/consumer.ts": {Hash: "c1", TS: &tsextractor.FileRecord{
			File: "src/consumer.ts", ImportSpecs: []string{"src/foo"},
			ResolvedFiles: []string{"src/foo/index.ts"}, ImportComplete: true,
		}},
		"src/foo/index.ts": {Hash: "i1", TS: &tsextractor.FileRecord{
			File: "src/foo/index.ts", Declared: []string{"Foo"}, ImportComplete: true,
		}},
		"src/other.ts": {Hash: "o1", TS: &tsextractor.FileRecord{
			File: "src/other.ts", ImportSpecs: []string{"react"}, ImportComplete: true,
		}},
	}
	hashes := map[string]string{
		"src/consumer.ts": "c1", "src/foo/index.ts": "i1", "src/other.ts": "o1", "src/foo.ts": "f1",
	}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	for _, want := range []string{"src/consumer.ts", "src/foo.ts"} {
		if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: want}); err != nil {
			t.Fatal(err)
		}
	}
	// src/other.ts imports only an external module, so no added path can rebind
	// it and it must stay out of the frozen scope.
	if p.member["src/other.ts"] {
		t.Fatalf("plan = %v, want src/other.ts excluded", p.member)
	}
}

// Renaming a file module to a folder index rebinds its importer and retires the
// old owner, both before Begin.
func TestAuthoritativeFilePlanRenameRebindsImporterAndClearsOldOwner(t *testing.T) {
	prev := []string{"src/consumer.ts", "src/target.ts"}
	current := []string{"src/consumer.ts", "src/target/index.ts"}
	state := map[string]*FileState{
		"src/consumer.ts": {Hash: "c1", TS: &tsextractor.FileRecord{
			File: "src/consumer.ts", ImportSpecs: []string{"src/target"},
			ResolvedFiles: []string{"src/target.ts"}, ImportComplete: true,
		}},
		"src/target.ts": {Hash: "t1", TS: &tsextractor.FileRecord{
			File: "src/target.ts", Declared: []string{"Target"}, ImportComplete: true,
		}},
	}
	hashes := map[string]string{"src/consumer.ts": "c1", "src/target/index.ts": "t2"}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	for _, want := range []string{"src/consumer.ts", "src/target.ts", "src/target/index.ts"} {
		if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: want}); err != nil {
			t.Fatal(err)
		}
	}
}

// A removed owner that is not a TypeScript source has consumers the import
// graph cannot see, so membership keeps the wider fallback.
func TestAuthoritativeFilePlanNonTSRemovalKeepsWholeDomain(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts", "config/routes.json"}
	current := []string{"src/a.ts", "src/b.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{
			File: "src/a.ts", ImportSpecs: []string{"src/b"}, ResolvedFiles: []string{"src/b.ts"}, ImportComplete: true,
		}},
		"src/b.ts":           {Hash: "b1", TS: &tsextractor.FileRecord{File: "src/b.ts", ImportComplete: true}},
		"config/routes.json": {Hash: "r1"},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1"}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembership {
		t.Fatalf("reason = %q", reason)
	}
	for _, want := range []string{"src/a.ts", "src/b.ts", "config/routes.json"} {
		if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: want}); err != nil {
			t.Fatal(err)
		}
	}
}

// An added file that no cached import can reach is not claimed by the planner.
// The prior owner map records contributions, not the prior inventory, so it
// cannot prove such a file is new; session.go plans it from the extractor that
// owns it (ownedFiles plus retireExtractorOwners), which reads real input
// hashes. The TypeScript add still narrows instead of widening the domain.
func TestAuthoritativeFilePlanNonTSAddComesFromExtractorOwners(t *testing.T) {
	prev := []string{"src/a.ts"}
	current := []string{"src/a.ts", "src/new.ts", "openapi/spec.yaml"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{
			File: "src/a.ts", ImportSpecs: []string{"react"}, ImportComplete: true,
		}},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/new.ts": "n1", "openapi/spec.yaml": "y1"}
	yaml := graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "openapi/spec.yaml"}

	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/new.ts"}); err != nil {
		t.Fatal(err)
	}
	if err := p.check(yaml); err == nil {
		t.Fatal("planner claimed an owner it cannot prove; session.go must supply it")
	}
	if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: "src/a.ts"}); err == nil {
		t.Fatal("src/a.ts imports only a bare external specifier and must stay out of scope")
	}

	withOwner, _, err := planForTest(prev, current, state, hashes, false, []string{"openapi/spec.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if err := withOwner.check(yaml); err != nil {
		t.Fatal(err)
	}
}

// A retired owner that is not a TypeScript source reaches the caller as a
// retired identity, so the pre-Begin name and route deltas can seed it as an
// old->empty contribution instead of discovering it after Begin is frozen.
func TestMembershipScopeReportsRetiredNonTSOwner(t *testing.T) {
	prev := []string{"src/a.ts", "docs/guide.md"}
	current := []string{"src/a.ts"}
	state := map[string]*FileState{
		"src/a.ts":      {Hash: "a1", TS: &tsextractor.FileRecord{File: "src/a.ts", ImportComplete: true}},
		"docs/guide.md": {Hash: "g1", Extractor: "mdintent"},
	}
	md := membershipScope(prev, current, current, state)
	if !md.changed {
		t.Fatal("membership did not report the removal")
	}
	if md.proven {
		t.Fatal("a retired non-TypeScript owner has consumers the import graph cannot enumerate")
	}
	if len(md.retired) != 1 || md.retired[0] != "docs/guide.md" {
		t.Fatalf("retired = %v", md.retired)
	}
}

// A deleted TypeScript router file must disappear from the projected record set.
// While overlayTSRecords could only add records, the cached mounts survived into
// the "after" graph and a delete looked like no route change at all.
func TestOverlayDropsRetiredRecords(t *testing.T) {
	prev := map[string]*tsextractor.FileRecord{
		"src/a.ts": {File: "src/a.ts"},
		"src/b.ts": {File: "src/b.ts"},
	}
	next := overlayTSRecords(prev, nil, map[string]bool{"src/b.ts": true})
	if next["src/a.ts"] == nil {
		t.Fatal("kept record lost")
	}
	if next["src/b.ts"] != nil {
		t.Fatal("retired record survived the overlay")
	}
}

// A retired provider has to reach the pre-Begin name delta as an old->empty
// contribution. Its consumers are resolved by global name, not along import
// edges, so a markdown owner that mentions the deleted symbol is unreachable by
// reverse-closing the TypeScript graph: seeding the retired identity is the only
// thing that puts it inside Begin.
func TestNameDeltaSeedsRetiredOwnerForNonTSConsumers(t *testing.T) {
	prev := map[string]*FileState{
		"src/gone.ts": {Hash: "g1", TS: &tsextractor.FileRecord{
			File:     "src/gone.ts",
			Declared: []string{"Widget"},
			Facts:    []facts.Fact{{Kind: facts.KindSymbol, Name: "Widget", File: "src/gone.ts"}},
		}},
		"docs/guide.md": {Hash: "d1", Extractor: "mdintent", Facts: []facts.Fact{
			{Kind: facts.KindIntent, Name: "guide", File: "docs/guide.md",
				Relations: []facts.Relation{{Kind: facts.RelNames, Target: "Widget"}}},
		}},
		"src/unrelated.ts": {Hash: "u1", TS: &tsextractor.FileRecord{
			File:  "src/unrelated.ts",
			Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "Other", File: "src/unrelated.ts"}},
		}},
	}
	// The retired identity has no preview record, so its contribution is empty.
	got := ownersForNameDelta(prev, map[string]bool{"src/gone.ts": true}, map[string][]facts.Fact{})
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	if !seen["docs/guide.md"] {
		t.Fatalf("markdown consumer of the retired symbol missing: %v", got)
	}
	if seen["src/unrelated.ts"] {
		t.Fatalf("unrelated owner pulled in: %v", got)
	}
}

// Deleting one of two providers of the same global name changes which candidate
// wins, so every owner that mentions the name has to be republished even though
// the surviving provider's own bytes did not change.
func TestNameDeltaSeedsRetiredOwnerOnCandidateCollision(t *testing.T) {
	prev := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{
			File:     "src/a.ts",
			Declared: []string{"Widget"},
			Facts:    []facts.Fact{{Kind: facts.KindSymbol, Name: "Widget", File: "src/a.ts"}},
		}},
		"src/b.ts": {Hash: "b1", TS: &tsextractor.FileRecord{
			File:     "src/b.ts",
			Declared: []string{"Widget"},
			Facts:    []facts.Fact{{Kind: facts.KindSymbol, Name: "Widget", File: "src/b.ts"}},
		}},
		"src/uses.ts": {Hash: "u1", TS: &tsextractor.FileRecord{
			File:       "src/uses.ts",
			Referenced: []string{"Widget"},
			Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "uses", File: "src/uses.ts",
				Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "Widget"}}}},
		}},
		"src/independent.ts": {Hash: "i1", TS: &tsextractor.FileRecord{
			File:  "src/independent.ts",
			Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "i", File: "src/independent.ts"}},
		}},
	}
	got := ownersForNameDelta(prev, map[string]bool{"src/a.ts": true}, map[string][]facts.Fact{})
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	for _, want := range []string{"src/b.ts", "src/uses.ts"} {
		if !seen[want] {
			t.Fatalf("collision consumer %s missing: %v", want, got)
		}
	}
	if seen["src/independent.ts"] {
		t.Fatalf("unrelated owner pulled in: %v", got)
	}
}

// Cached ImportSpecs are the extractor's own post-resolution targets, so an
// alias-mapped import is stored as its repo-relative path and a file added at
// that path rebinds the importer. A specifier that still looks like a bare
// package is not claimed, which is what keeps an ordinary content edit narrow.
func TestMembershipReboundFollowsResolvedAliasTarget(t *testing.T) {
	previous := []string{"src/app.ts", "src/lib/util/index.ts", "src/pkg.ts"}
	current := append(append([]string{}, previous...), "src/lib/util.ts")
	state := map[string]*FileState{
		"src/app.ts": {Hash: "a1", TS: &tsextractor.FileRecord{
			File: "src/app.ts", ImportSpecs: []string{"src/lib/util"},
			ResolvedFiles: []string{"src/lib/util/index.ts"}, ImportComplete: true,
		}},
		"src/lib/util/index.ts": {Hash: "i1", TS: &tsextractor.FileRecord{
			File: "src/lib/util/index.ts", ImportComplete: true,
		}},
		"src/pkg.ts": {Hash: "p1", TS: &tsextractor.FileRecord{
			File: "src/pkg.ts", ImportSpecs: []string{"@scope/ui"}, ImportComplete: true,
		}},
	}
	md := membershipScope(previous, current, current, state)
	if !md.changed || !md.proven {
		t.Fatalf("membership = %+v", md)
	}
	if len(md.rebound) != 1 || md.rebound[0] != "src/app.ts" {
		t.Fatalf("rebound = %v, want only src/app.ts", md.rebound)
	}
}

// The frozen scope must be a superset of the reparse set extraction computes:
// scope owners and parses are distinct sets, and a dirty file outside the plan
// is a hard failure at extraction time.
func TestMembershipScopeCoversReparseSet(t *testing.T) {
	prev := []string{"src/consumer.ts", "src/foo/index.ts", "src/broken.ts"}
	current := []string{"src/consumer.ts", "src/foo/index.ts", "src/broken.ts", "src/foo.ts"}
	state := map[string]*FileState{
		"src/consumer.ts": {Hash: "c1", TS: &tsextractor.FileRecord{
			File: "src/consumer.ts", ImportSpecs: []string{"src/foo"},
			ResolvedFiles: []string{"src/foo/index.ts"}, ImportComplete: true,
		}},
		"src/foo/index.ts": {Hash: "i1", TS: &tsextractor.FileRecord{File: "src/foo/index.ts", ImportComplete: true}},
		"src/broken.ts": {Hash: "k1", TS: &tsextractor.FileRecord{
			File: "src/broken.ts", ImportSpecs: []string{"src/never"},
			UnresolvedSpecs: []string{"src/never"}, ImportComplete: true,
		}},
	}
	hashes := map[string]string{
		"src/consumer.ts": "c1", "src/foo/index.ts": "i1", "src/broken.ts": "k1", "src/foo.ts": "f1",
	}
	p, reason, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembershipRe {
		t.Fatalf("reason = %q", reason)
	}
	dirty, broaden, _ := invalidateTS(map[string]bool{}, tsRecordsFromState(state), current, hashes)
	if broaden {
		t.Fatal("fixture forced the extraction-side fallback")
	}
	for f, d := range dirty {
		if !d {
			continue
		}
		if err := p.check(graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: f}); err != nil {
			t.Fatalf("reparsed file outside frozen scope: %v", err)
		}
	}
}

// Whole-domain and delta runs must agree on the owner set for the same delta
// whenever the delta path is not allowed to narrow.
func TestMembershipDeltaMatchesColdWhenFallbackApplies(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts", "config/routes.json"}
	current := []string{"src/a.ts", "src/b.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", TS: &tsextractor.FileRecord{
			File: "src/a.ts", ImportSpecs: []string{"src/b"}, ResolvedFiles: []string{"src/b.ts"}, ImportComplete: true,
		}},
		"src/b.ts":           {Hash: "b1", TS: &tsextractor.FileRecord{File: "src/b.ts", ImportComplete: true}},
		"config/routes.json": {Hash: "r1"},
	}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1"}
	delta, _, err := planForTest(prev, current, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	cold, _, err := planForTest(prev, current, state, hashes, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if delta.digest != cold.digest {
		t.Fatalf("delta digest %q != cold digest %q", delta.digest, cold.digest)
	}
}

func TestComposedRouteFactsAppliesEmberEngineMounts(t *testing.T) {
	engine := "lib/shop/addon/routes.js"
	prev := map[string]*tsextractor.FileRecord{
		"app/router.ts": {
			File: "app/router.ts",
			Facts: []facts.Fact{{
				Kind: facts.KindRoute, Name: "/store", File: "app/router.ts",
				Props: map[string]any{"type": "engine_mount", "ember_engine": "shop", "router": "map", "method": "GET", "framework": "ember"},
			}},
		},
		engine: {
			File: engine,
			Facts: []facts.Fact{{
				Kind: facts.KindRoute, Name: "/cart", File: engine,
				Props: map[string]any{"router": "engine", "ember_engine": "shop", "method": "GET", "framework": "ember"},
			}},
		},
	}
	got := composedRouteFacts(prev)
	found := false
	for _, f := range got {
		if f.File == engine && f.Name == "/store/cart" && f.PropBool("ember_mounted") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ember composition missing /store/cart: %+v", got)
	}
	if prev[engine].Facts[0].Name != "/cart" {
		t.Fatal("ComposeEngineMounts mutated cached FileRecord facts")
	}
	next := overlayTSRecords(prev, map[string]*tsextractor.FileRecord{
		"app/router.ts": {
			File: "app/router.ts",
			Facts: []facts.Fact{{
				Kind: facts.KindRoute, Name: "/v2", File: "app/router.ts",
				Props: map[string]any{"type": "engine_mount", "ember_engine": "shop", "router": "map", "method": "GET", "framework": "ember"},
			}},
		},
	}, nil)
	scope := map[string]bool{}
	addChangedRouteFiles(scope, composedRouteFacts(prev), composedRouteFacts(next))
	if !scope[engine] {
		t.Fatalf("ember mount rewrite omitted child owner: %v", scope)
	}
}

func TestOwnersForNameDeltaSameNameKindChangeIncludesReferencers(t *testing.T) {
	prev := map[string]*FileState{
		"a.ts": {
			TS: &tsextractor.FileRecord{
				File:     "a.ts",
				Declared: []string{"Foo"},
				Facts:    []facts.Fact{{Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}},
			},
		},
		"b.ts": {
			TS: &tsextractor.FileRecord{
				File:       "b.ts",
				Referenced: []string{"Foo"},
				Facts:      []facts.Fact{{Kind: facts.KindSymbol, Name: "use", File: "b.ts", Relations: []facts.Relation{{Kind: facts.RelCalls, Target: "Foo"}}}},
			},
		},
		"independent.ts": {TS: &tsextractor.FileRecord{File: "independent.ts", Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "i", File: "independent.ts"}}}},
	}
	got := ownersForNameDelta(prev, map[string]bool{"a.ts": true}, map[string][]facts.Fact{
		"a.ts": {{Kind: facts.KindRoute, Name: "Foo", File: "a.ts"}},
	})
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	if !seen["a.ts"] || !seen["b.ts"] {
		t.Fatalf("same-name kind change omitted referencer: %v", got)
	}
	if seen["independent.ts"] {
		t.Fatalf("unrelated owner in kind-change delta %v", got)
	}
}

func TestOwnersForNameDeltaUnresolvedBecomingDeclarationIncludesReferencers(t *testing.T) {
	prev := map[string]*FileState{
		"a.ts": {
			TS: &tsextractor.FileRecord{
				File:            "a.ts",
				Referenced:      []string{"Foo"},
				UnresolvedSpecs: []string{"./missing"},
				Facts:           []facts.Fact{{Kind: facts.KindSymbol, Name: "x", File: "a.ts"}},
			},
		},
		"b.ts": {
			TS: &tsextractor.FileRecord{
				File:       "b.ts",
				Referenced: []string{"Foo"},
				Facts:      []facts.Fact{{Kind: facts.KindSymbol, Name: "y", File: "b.ts"}},
			},
		},
		"independent.ts": {TS: &tsextractor.FileRecord{File: "independent.ts", Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "i", File: "independent.ts"}}}},
	}
	got := ownersForNameDelta(prev, map[string]bool{"a.ts": true}, map[string][]facts.Fact{
		"a.ts": {{Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}, {Kind: facts.KindSymbol, Name: "x", File: "a.ts"}},
	})
	seen := map[string]bool{}
	for _, n := range got {
		seen[n] = true
	}
	if !seen["b.ts"] {
		t.Fatalf("unresolved name becoming declaration omitted b.ts: %v", got)
	}
	if seen["independent.ts"] {
		t.Fatalf("unrelated owner in unresolved-declaration delta %v", got)
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

// A bare package subpath and a still-broken relative import resolve in neither
// the prior nor the new file set, so adding an unrelated file cannot have moved
// them. Reporting them as rebound dirtied every importer of a subpath package on
// any add, which then fed the declared-name check a repository-wide dirty set.
func TestMembershipScopeIgnoresSpecsUnresolvedInBothUniverses(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts"}
	current := []string{"src/a.ts", "src/b.ts", "src/new.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File:           "src/a.ts",
			ImportSpecs:    []string{"node:fs/promises", "react-native/Libraries/Text", "src/missing/widget"},
			ResolvedFiles:  nil,
			ImportComplete: true,
			Declared:       []string{"A"},
		}},
		"src/b.ts": {Hash: "b1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File:            "src/b.ts",
			ImportSpecs:     []string{"src/missing/widget"},
			UnresolvedSpecs: []string{"src/missing/widget"},
			ImportComplete:  true,
			Declared:        []string{"B"},
		}},
	}
	md := membershipScope(prev, current, current, state)
	if !md.changed || !md.proven {
		t.Fatalf("membership changed=%v proven=%v, want a proven addition", md.changed, md.proven)
	}
	if len(md.rebound) != 0 {
		t.Fatalf("rebound=%v, want none: no cached specifier resolves differently", md.rebound)
	}
}

// The same record set, but the added file is what the broken specifier names.
// That specifier does move, so both replay paths - ImportSpecs and the cached
// UnresolvedSpecs list - have to report their importer.
func TestMembershipScopeReportsSpecThatTheAdditionResolves(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts"}
	current := []string{"src/a.ts", "src/b.ts", "src/missing/widget.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File:           "src/a.ts",
			ImportSpecs:    []string{"node:fs/promises", "src/missing/widget"},
			ImportComplete: true,
		}},
		"src/b.ts": {Hash: "b1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File:            "src/b.ts",
			ImportSpecs:     []string{"node:fs/promises"},
			UnresolvedSpecs: []string{"src/missing/widget"},
			ImportComplete:  true,
		}},
	}
	md := membershipScope(prev, current, current, state)
	if !md.proven {
		t.Fatalf("membership unproven: %s", md.reason)
	}
	want := []string{"src/a.ts", "src/b.ts"}
	if !reflect.DeepEqual(md.rebound, want) {
		t.Fatalf("rebound=%v, want %v", md.rebound, want)
	}
}

// invalidateTS and membershipScope must stay the same predicate, or the frozen
// scope stops being a superset of the reparse set.
func TestInvalidateTSMatchesMembershipReboundOnUnresolvedSpecs(t *testing.T) {
	recs := map[string]*tsextractor.FileRecord{
		"src/a.ts": {File: "src/a.ts", ImportSpecs: []string{"node:fs/promises", "src/missing/widget"}, ImportComplete: true},
		"src/b.ts": {File: "src/b.ts", ImportSpecs: []string{"lodash/debounce"}, ImportComplete: true},
	}
	owned := []string{"src/a.ts", "src/b.ts", "src/new.ts"}
	hashes := map[string]string{"src/a.ts": "a1", "src/b.ts": "b1", "src/new.ts": "n1"}
	dirty, broaden, reason := invalidateTS(map[string]bool{}, recs, owned, hashes)
	if broaden {
		t.Fatalf("broadened: %s", reason)
	}
	if !dirty["src/new.ts"] {
		t.Fatal("the added file itself must be dirty")
	}
	if dirty["src/a.ts"] || dirty["src/b.ts"] {
		t.Fatalf("dirty=%v, want only the added file: no cached specifier rebinds to it", dirty)
	}
}

// A record whose specifier list was truncated away while its unresolved list
// survived cannot be replayed: recordRebound would read it as an importer with
// no imports, while invalidateTS replays the same unresolved specifier and marks
// the file dirty. That is exactly the scope-narrower-than-parses hole, so the
// membership delta has to fail closed instead.
func TestMembershipScopeFallsBackForUnresolvedOnlyRecordWithoutSpecs(t *testing.T) {
	prev := []string{"src/a.ts", "src/b.ts"}
	current := []string{"src/a.ts", "src/b.ts", "src/missing/widget.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File:            "src/a.ts",
			UnresolvedSpecs: []string{"src/missing/widget"},
		}},
		"src/b.ts": {Hash: "b1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/b.ts", ImportSpecs: []string{"src/a.ts"}, ResolvedFiles: []string{"src/a.ts"}, ImportComplete: true,
		}},
	}
	md := membershipScope(prev, current, current, state)
	if !md.changed {
		t.Fatal("addition not detected")
	}
	if md.proven {
		t.Fatalf("proven with a truncated record; invalidateTS would dirty src/a.ts outside the frozen scope (rebound=%v)", md.rebound)
	}
	if md.reason != frozenScopeMembership {
		t.Fatalf("reason=%q, want %q", md.reason, frozenScopeMembership)
	}
	p, reason, err := planForTest(prev, current, state, map[string]string{"src/a.ts": "a1", "src/b.ts": "b1", "src/missing/widget.ts": "w1"}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeMembership {
		t.Fatalf("plan reason=%q, want the conservative membership fallback", reason)
	}
	if !p.member["src/a.ts"] {
		t.Fatalf("scope %v omits the file extraction will reparse", p.manifest())
	}
}

// Deleting the target of a resolved specifier is the mirror of the addition
// case: the importer has to be reported even though its own bytes did not move.
func TestMembershipScopeReportsSpecLosingItsTarget(t *testing.T) {
	prev := []string{"src/a.ts", "src/gone.ts"}
	current := []string{"src/a.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/a.ts", ImportSpecs: []string{"src/gone", "lodash/debounce"},
			ResolvedFiles: []string{"src/gone.ts"}, ImportComplete: true,
		}},
		"src/gone.ts": {Hash: "g1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/gone.ts", Declared: []string{"gone"}, ImportComplete: true,
		}},
	}
	md := membershipScope(prev, current, current, state)
	if !md.proven {
		t.Fatalf("membership unproven: %s", md.reason)
	}
	if !reflect.DeepEqual(md.rebound, []string{"src/a.ts"}) {
		t.Fatalf("rebound=%v, want [src/a.ts]", md.rebound)
	}
	if !reflect.DeepEqual(md.retired, []string{"src/gone.ts"}) {
		t.Fatalf("retired=%v, want [src/gone.ts]", md.retired)
	}
}

// An added file that wins resolveModuleFile's exact-file precedence over the
// folder index the specifier used to reach. Nothing is added or removed from the
// importer's text, and both universes resolve the specifier, so only comparing
// the resolved targets catches it.
func TestMembershipScopeReportsShadowedResolutionTarget(t *testing.T) {
	prev := []string{"src/a.ts", "src/util/index.ts"}
	current := []string{"src/a.ts", "src/util/index.ts", "src/util.ts"}
	state := map[string]*FileState{
		"src/a.ts": {Hash: "a1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/a.ts", ImportSpecs: []string{"src/util"},
			ResolvedFiles: []string{"src/util/index.ts"}, ImportComplete: true,
		}},
		"src/util/index.ts": {Hash: "u1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/util/index.ts", Declared: []string{"helper"}, ImportComplete: true,
		}},
	}
	md := membershipScope(prev, current, current, state)
	if !md.proven {
		t.Fatalf("membership unproven: %s", md.reason)
	}
	if !reflect.DeepEqual(md.rebound, []string{"src/a.ts"}) {
		t.Fatalf("rebound=%v, want [src/a.ts]: src/util.ts shadows src/util/index.ts", md.rebound)
	}
}

// The seeded name dependent brings its own reverse dependents with it, and an
// owner that references nothing declared by the change stays outside Begin.
func TestAuthoritativePlanSeedsNameDependentWithReverseClosure(t *testing.T) {
	prev := []string{"src/main.ts", "src/viewer.ts", "src/shell.ts", "src/unrelated.ts"}
	state := map[string]*FileState{
		"src/main.ts": {Hash: "m1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/main.ts", Declared: []string{"escapeHtml"}, ImportComplete: true,
		}},
		"src/viewer.ts": {Hash: "v1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/viewer.ts", Declared: []string{"render"}, Referenced: []string{"escapeHtml"}, ImportComplete: true,
		}},
		"src/shell.ts": {Hash: "s1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/shell.ts", ImportSpecs: []string{"src/viewer"},
			ResolvedFiles: []string{"src/viewer.ts"}, ImportComplete: true,
		}},
		"src/unrelated.ts": {Hash: "u1", Extractor: "typescript", TS: &tsextractor.FileRecord{
			File: "src/unrelated.ts", Referenced: []string{"lodash"}, ImportComplete: true,
		}},
	}
	hashes := map[string]string{"src/main.ts": "m2", "src/viewer.ts": "v1", "src/shell.ts": "s1", "src/unrelated.ts": "u1"}
	p, reason, err := planForTest(prev, prev, state, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeNameDelta {
		t.Fatalf("reason=%q, want the name-delta scope", reason)
	}
	for _, want := range []string{"src/main.ts", "src/viewer.ts", "src/shell.ts"} {
		if !p.member[want] {
			t.Fatalf("scope %v omits %s", p.manifest(), want)
		}
	}
	if p.member["src/unrelated.ts"] {
		t.Fatalf("scope %v widened past the name dependents and their importers", p.manifest())
	}
}
