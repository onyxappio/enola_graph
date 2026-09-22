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
		Relations: []facts.Relation{{Kind: facts.RelDependsOn, Target: "src.scanSubjects"}},
	}
	idx := buildIndex([]facts.Fact{storage, symbol, from})
	id, status := idx.resolveRel("", from.Kind, facts.RelDependsOn, "src.scanSubjects")
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
