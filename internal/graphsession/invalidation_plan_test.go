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

func TestAuthoritativePlanWholeDomainWhenNameDependentOutsideReverseClose(t *testing.T) {
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
	p, reason, err := authoritativeFilePlan(previous, previous, prev, hashes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reason != frozenScopeWholeDomain {
		t.Fatalf("reason=%s, want whole-domain because app.ts references dirty declared names without a file edge", reason)
	}
	if !p.member["apps/architect-console/src/server/app.ts"] {
		t.Fatal("post-parse name dependent omitted from Begin")
	}
	if !p.member["packages/crypto/src/password.ts"] {
		t.Fatal("dirty file omitted from Begin")
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
	got := dirtyRouterMountChildren(prev, dirty, newRecs)
	found := false
	for _, id := range got {
		if id == "src/api/orders.ts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mount prefix change omitted child: %v", got)
	}
	unchanged := dirtyRouterMountChildren(prev, dirty, map[string]*tsextractor.FileRecord{"src/server.ts": prev["src/server.ts"].TS})
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
	got := dirtyRouterMountChildren(prev, dirty, newRecs)
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
	got := composedRouteOwnerDelta(prev, dirty, newRecs)
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
	if extra := composedRouteOwnerDelta(prev, bodyDirty, bodyNew); len(extra) != 0 {
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
	})
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
