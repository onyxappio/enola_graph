package graphsession

import (
	"reflect"
	"testing"

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
