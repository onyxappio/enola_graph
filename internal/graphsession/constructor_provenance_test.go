package graphsession

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestPublishedConstructorTargetFileResolvesExactSymbol(t *testing.T) {
	tests := []struct {
		name       string
		construct  string
		wantName   string
		wantTarget string
	}{
		{
			name: "local declaration beats same-named sibling",
			construct: `import { Imported } from './external';
export {};
class Local {}
function makeLocal() { return new Local(); }
function makeImported() { return new Imported(); }
`,
			wantName:   "src.Local",
			wantTarget: "src/construct.ts",
		},
		{
			name: "import alias beats same-named sibling",
			construct: `import { Imported as Local } from './external';
export {};
function makeLocal() { return new Local(); }
`,
			wantName:   "src.Imported",
			wantTarget: "src/external.ts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupTSRepo(t, map[string]string{
				"src/construct.ts": tt.construct,
				"src/external.ts":  "export class Imported {}\n",
				"src/sibling.ts":   "export class Local {}\n",
			})
			eng := testEngine(t, dir)
			sink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, sink, Options{StateDir: filepath.Join(dir, ".enola", "state")}); err != nil {
				t.Fatal(err)
			}
			c := applyGraph(t, sink)
			assertInstantiationResolvedToFile(t, c, "src/construct.ts", "src.makeLocal", tt.wantName, tt.wantTarget)
		})
	}
}

func TestEncodeOwnerInstantiationWithoutTargetFileStaysAmbiguous(t *testing.T) {
	local := facts.Fact{Kind: facts.KindSymbol, Name: "src.Local", File: "src/construct.ts", Repo: "r"}
	sibling := facts.Fact{Kind: facts.KindSymbol, Name: "src.Local", File: "src/sibling.ts", Repo: "r"}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "src.make", File: "src/construct.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelInstantiates, Target: "src.Local"}},
	}
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, buildIndex([]facts.Fact{local, sibling, from}), false)
	if len(edges) != 1 || edges[0].Resolution != graphstream.ResAmbiguous || edges[0].TargetID != "" {
		t.Fatalf("instantiation without target_file must stay ambiguous: %+v", edges)
	}
}

func TestEncodeOwnerMissingInstantiationTargetFileDoesNotFallBack(t *testing.T) {
	sibling := facts.Fact{Kind: facts.KindSymbol, Name: "src.Local", File: "src/sibling.ts", Repo: "r"}
	from := facts.Fact{
		Kind: facts.KindSymbol, Name: "src.make", File: "src/construct.ts", Repo: "r",
		Relations: []facts.Relation{{Kind: facts.RelInstantiates, Target: "src.Local", TargetFile: "src/missing.ts"}},
	}
	_, edges := encodeOwner(ownerOutput{Owner: ownerOf(from), Facts: []facts.Fact{from}}, buildIndex([]facts.Fact{sibling, from}), false)
	if len(edges) != 1 || edges[0].Resolution != graphstream.ResUnresolved || edges[0].TargetID != "" {
		t.Fatalf("missing proven target must not bind same-name sibling: %+v", edges)
	}
}

func assertInstantiationResolvedToFile(t *testing.T, c *Consumer, owner, fromName, targetName, targetFile string) {
	t.Helper()
	var found bool
	for _, e := range c.Edges[ownerKey(owner)] {
		if e.Kind != facts.RelInstantiates || e.TargetName != targetName {
			continue
		}
		from := nodeByID(c, e.FromID)
		if from.Name != fromName {
			continue
		}
		found = true
		if e.Resolution != graphstream.ResResolved || e.TargetID == "" {
			t.Fatalf("%s instantiates %s resolution=%s target=%q", fromName, targetName, e.Resolution, e.TargetID)
		}
		if got := nodeByID(c, e.TargetID).File; got != targetFile {
			t.Fatalf("%s instantiates %s in %s, want %s", fromName, targetName, got, targetFile)
		}
	}
	if !found {
		t.Fatalf("missing instantiates edge %s -> %s owned by %s", fromName, targetName, owner)
	}
}
