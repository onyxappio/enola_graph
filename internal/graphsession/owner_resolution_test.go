package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestChangedCandidateNamesIgnoresAssemblyOrder(t *testing.T) {
	a := buildIndex([]facts.Fact{
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "b.ts"},
	})
	b := buildIndex([]facts.Fact{
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "b.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
	})
	if got := changedCandidateNames(a, b, nil); len(got) != 0 {
		t.Fatalf("order-only candidate change = %v", got)
	}
}

func TestChangedCandidateNamesDetectsAddedCollision(t *testing.T) {
	a := buildIndex([]facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}})
	b := buildIndex([]facts.Fact{
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "b.ts"},
	})
	if got := changedCandidateNames(a, b, nil); !got["Foo"] {
		t.Fatalf("added collision not detected: %v", got)
	}
}

func TestChangedCandidateNamesIgnoresDuplicateCandidateFacts(t *testing.T) {
	a := buildIndex([]facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}})
	b := buildIndex([]facts.Fact{
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
	})
	if got := changedCandidateNames(a, b, nil); len(got) != 0 {
		t.Fatalf("duplicate candidate fact changed domain = %v", got)
	}
}
