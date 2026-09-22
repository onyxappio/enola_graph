package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestResolveStorageDependsOnPrefersStorageKind(t *testing.T) {
	storage := facts.Fact{Kind: facts.KindStorage, Name: "src.scanSubjects", File: "src/schema.ts"}
	symbol := facts.Fact{Kind: facts.KindSymbol, Name: "src.scanSubjects", File: "src/schema.ts"}
	from := facts.Fact{
		Kind: facts.KindStorage, Name: "src.scanRuns", File: "src/schema.ts",
		Props:     map[string]any{"fk_constraints": "subjectId->scanSubjects.subjectId"},
		Relations: []facts.Relation{{Kind: facts.RelDependsOn, Target: "src.scanSubjects"}},
	}
	idx := buildIndex([]facts.Fact{storage, symbol, from})
	id, status := idx.resolveRelConstrained("", from.Kind, facts.RelDependsOn, "src.scanSubjects", fkStorageTargetRequired(from, from.Relations[0]))
	if status != graphstream.ResResolved {
		t.Fatalf("status=%s, want resolved", status)
	}
	if id != storage.Identity() {
		t.Fatalf("target id=%s, want storage %s", id, storage.Identity())
	}
	if _, st := idx.resolve("", "src.scanSubjects"); st != graphstream.ResAmbiguous {
		t.Fatalf("bare name resolve status=%s, want ambiguous so the symbol is still indexed", st)
	}
}

func TestResolveFKDependsOnDoesNotBindOrdinarySymbol(t *testing.T) {
	symbol := facts.Fact{Kind: facts.KindSymbol, Name: "src.scanSubjects", File: "src/schema.ts"}
	from := facts.Fact{
		Kind: facts.KindStorage, Name: "src.scanRuns", File: "src/schema.ts",
		Props:     map[string]any{"fk_constraints": "subjectId->scanSubjects.subjectId"},
		Relations: []facts.Relation{{Kind: facts.RelDependsOn, Target: "src.scanSubjects"}},
	}
	idx := buildIndex([]facts.Fact{symbol, from})
	id, status := idx.resolveRelConstrained("", from.Kind, facts.RelDependsOn, "src.scanSubjects", fkStorageTargetRequired(from, from.Relations[0]))
	if status != graphstream.ResUnresolved {
		t.Fatalf("status=%s id=%s, want unresolved when the table is an ordinary symbol", status, id)
	}
}

func TestResolveFKDependsOnUnknownTargetStaysUnresolved(t *testing.T) {
	from := facts.Fact{
		Kind: facts.KindStorage, Name: "src.scanRuns", File: "src/schema.ts",
		Props:     map[string]any{"fk_constraints": "subjectId->missing.subjectId"},
		Relations: []facts.Relation{{Kind: facts.RelDependsOn, Target: "src.missing"}},
	}
	idx := buildIndex([]facts.Fact{from})
	id, status := idx.resolveRelConstrained("", from.Kind, facts.RelDependsOn, "src.missing", fkStorageTargetRequired(from, from.Relations[0]))
	if status != graphstream.ResUnresolved || id != "" {
		t.Fatalf("status=%s id=%s, want unresolved empty id", status, id)
	}
}

func TestResolveStorageDependsOnWithoutFKFallsBackToSymbol(t *testing.T) {
	symbol := facts.Fact{Kind: facts.KindSymbol, Name: "src.User", File: "src/user.ts"}
	from := facts.Fact{
		Kind: facts.KindStorage, Name: "src.AppDbContext", File: "src/db.ts",
		Relations: []facts.Relation{{Kind: facts.RelDependsOn, Target: "src.User"}},
	}
	idx := buildIndex([]facts.Fact{symbol, from})
	id, status := idx.resolveRelConstrained("", from.Kind, facts.RelDependsOn, "src.User", fkStorageTargetRequired(from, from.Relations[0]))
	if status != graphstream.ResResolved {
		t.Fatalf("status=%s, want resolved fallback for non-FK storage depends_on", status)
	}
	if id != symbol.Identity() {
		t.Fatalf("target id=%s, want symbol %s", id, symbol.Identity())
	}
}

func TestEncodeOwnerFKEdgesBindStorageOnly(t *testing.T) {
	storage := facts.Fact{Kind: facts.KindStorage, Name: "src.scanSubjects", File: "src/schema.ts"}
	symbol := facts.Fact{Kind: facts.KindSymbol, Name: "src.scanSubjects", File: "src/schema.ts"}
	from := facts.Fact{
		Kind: facts.KindStorage, Name: "src.scanRuns", File: "src/schema.ts",
		Props:     map[string]any{"fk_constraints": "subjectId->scanSubjects.subjectId"},
		Relations: []facts.Relation{{Kind: facts.RelDependsOn, Target: "src.scanSubjects"}},
	}
	_, present := encodeOwner(ownerOutput{Facts: []facts.Fact{from}}, buildIndex([]facts.Fact{storage, symbol, from}), false)
	got := dependsOnEdge(t, present)
	if got.Resolution != graphstream.ResResolved || got.TargetID != storage.Identity() {
		t.Fatalf("table present: resolution=%s target=%s", got.Resolution, got.TargetID)
	}

	_, replaced := encodeOwner(ownerOutput{Facts: []facts.Fact{from}}, buildIndex([]facts.Fact{symbol, from}), false)
	got = dependsOnEdge(t, replaced)
	if got.Resolution != graphstream.ResUnresolved || got.TargetID != "" {
		t.Fatalf("table replaced by symbol: resolution=%s target=%s", got.Resolution, got.TargetID)
	}

	_, removed := encodeOwner(ownerOutput{Facts: []facts.Fact{from}}, buildIndex([]facts.Fact{from}), false)
	got = dependsOnEdge(t, removed)
	if got.Resolution != graphstream.ResUnresolved || got.TargetID != "" {
		t.Fatalf("table removed: resolution=%s target=%s", got.Resolution, got.TargetID)
	}
}

func dependsOnEdge(t *testing.T, edges []graphstream.Edge) graphstream.Edge {
	t.Helper()
	for _, e := range edges {
		if e.Kind == facts.RelDependsOn {
			return e
		}
	}
	t.Fatal("missing depends_on edge")
	return graphstream.Edge{}
}
