package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

func TestFactsFingerprint_PropertyChange(t *testing.T) {
	base := facts.Fact{Kind: facts.KindSymbol, Name: "constant", File: "note.md", Line: 1}
	old := []facts.Fact{withProps(base, map[string]any{"description": "docs:old"})}
	neu := []facts.Fact{withProps(base, map[string]any{"description": "docs:new"})}
	if factsFingerprint(old) == factsFingerprint(neu) {
		t.Fatal("property-only change must change the fingerprint")
	}
}

func TestFactsFingerprint_PositionChange(t *testing.T) {
	a := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md", Line: 1, Column: 1, EndLine: 1, EndColumn: 5}
	b := a
	b.Line = 2
	if factsFingerprint([]facts.Fact{a}) == factsFingerprint([]facts.Fact{b}) {
		t.Fatal("line change must change the fingerprint")
	}
	b = a
	b.Column = 4
	if factsFingerprint([]facts.Fact{a}) == factsFingerprint([]facts.Fact{b}) {
		t.Fatal("column change must change the fingerprint")
	}
}

func TestFactsFingerprint_RelationOrderAndSplit(t *testing.T) {
	base := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md"}
	left := base
	left.Relations = []facts.Relation{{Kind: "a", Target: "b->c"}}
	right := base
	right.Relations = []facts.Relation{{Kind: "a->b", Target: "c"}}
	if factsFingerprint([]facts.Fact{left}) == factsFingerprint([]facts.Fact{right}) {
		t.Fatal("relation kind/target split must not collide")
	}
	swapped := base
	swapped.Relations = []facts.Relation{{Kind: "names", Target: "b"}, {Kind: "declares", Target: "a"}}
	ordered := base
	ordered.Relations = []facts.Relation{{Kind: "declares", Target: "a"}, {Kind: "names", Target: "b"}}
	if factsFingerprint([]facts.Fact{swapped}) == factsFingerprint([]facts.Fact{ordered}) {
		t.Fatal("relation order is observable and must be hashed")
	}
}

func TestFactsFingerprint_DuplicateMultiplicity(t *testing.T) {
	f := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md", Props: map[string]any{"k": "v"}}
	one := factsFingerprint([]facts.Fact{f})
	two := factsFingerprint([]facts.Fact{f, f})
	if one == two {
		t.Fatal("duplicate facts must contribute to the fingerprint")
	}
	if factsFingerprint([]facts.Fact{f, f}) != factsFingerprint([]facts.Fact{f, f}) {
		t.Fatal("identical duplicate lists must hash equal")
	}
}

func TestFactsFingerprint_DelimiterAmbiguity(t *testing.T) {
	a := facts.Fact{Kind: "k", Name: "a|b", File: "c"}
	b := facts.Fact{Kind: "k", Name: "a", File: "b|c"}
	if factsFingerprint([]facts.Fact{a}) == factsFingerprint([]facts.Fact{b}) {
		t.Fatal("pipe-joined name/file must not collide")
	}
	c := facts.Fact{Kind: "k", Name: "ab", File: "c"}
	d := facts.Fact{Kind: "k", Name: "a", File: "bc"}
	if factsFingerprint([]facts.Fact{c}) == factsFingerprint([]facts.Fact{d}) {
		t.Fatal("length-prefix must separate adjacent strings")
	}
}

func TestFactsFingerprint_JSONNumberRoundTrip(t *testing.T) {
	base := facts.Fact{Kind: facts.KindSymbol, Name: "README.md#title", File: "README.md", Line: 1}
	asInt := []facts.Fact{withProps(base, map[string]any{"level": 1, "title": "Title"})}
	asFloat := []facts.Fact{withProps(base, map[string]any{"level": float64(1), "title": "Title"})}
	if factsFingerprint(asInt) != factsFingerprint(asFloat) {
		t.Fatal("int and float64 JSON numbers must fingerprint equal after state round-trip")
	}
}

func TestFactsFingerprint_UnmarshalableIsNotOmitted(t *testing.T) {
	base := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md"}
	bad := []facts.Fact{withProps(base, map[string]any{"ch": make(chan int)})}
	empty := []facts.Fact{base}
	if factsFingerprint(bad) == factsFingerprint(empty) {
		t.Fatal("unmarshalable props must not fingerprint as missing props")
	}
}

func TestFactsFingerprint_NestedPropsCanonical(t *testing.T) {
	left := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md", Props: map[string]any{
		"b": 1,
		"a": map[string]any{"y": "2", "x": []any{"p", "q"}},
	}}
	right := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md", Props: map[string]any{
		"a": map[string]any{"x": []string{"p", "q"}, "y": "2"},
		"b": int64(1),
	}}
	if factsFingerprint([]facts.Fact{left}) != factsFingerprint([]facts.Fact{right}) {
		t.Fatal("nested map key order and equivalent numeric/array forms must canonicalize")
	}
	reordered := facts.Fact{Kind: facts.KindSymbol, Name: "n", File: "a.md", Props: map[string]any{
		"a": map[string]any{"x": []any{"q", "p"}, "y": "2"},
		"b": 1,
	}}
	if factsFingerprint([]facts.Fact{left}) == factsFingerprint([]facts.Fact{reordered}) {
		t.Fatal("array order inside props is observable")
	}
}

func TestFactsFingerprint_ListOrderIndependent(t *testing.T) {
	a := facts.Fact{Kind: facts.KindSymbol, Name: "a", File: "a.md", Line: 1}
	b := facts.Fact{Kind: facts.KindSymbol, Name: "b", File: "b.md", Line: 2}
	if factsFingerprint([]facts.Fact{a, b}) != factsFingerprint([]facts.Fact{b, a}) {
		t.Fatal("fact list permutation with distinct identities must not change the digest")
	}
}

type fingerprintPropExt struct{ reviewOwnedExt }

func (e fingerprintPropExt) Extract(ctx context.Context, root string, files []string) ([]facts.Fact, error) {
	ff, err := e.reviewOwnedExt.Extract(ctx, root, files)
	for i := range ff {
		ff[i].Props = map[string]any{"description": ff[i].Name}
		ff[i].Name = "constant"
	}
	return ff, err
}

func TestFactsFingerprint_PropertyOnlyDeltaEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export const a = 1;", "note.md": "old"})
	eng := multiEngine(t, dir, fingerprintPropExt{reviewOwnedExt{reviewContentExt{name: "docs"}}})
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: state}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)

	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	s = &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.TargetGeneration < 2 || len(s.CloneRecords()) == 0 {
		t.Fatalf("property-only edit suppressed: gen=%d events=%d", delta.TargetGeneration, len(s.CloneRecords()))
	}
	applyRun(t, cons, s)

	cold := NewConsumer()
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cold, s)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestFactsFingerprint_HeadingLevelDeltaEqualsCold(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"README.md": "# Title\n"})
	eng := mdintentEngine(t, dir, false)
	state := filepath.Join(dir, ".enola", "state")
	cons := NewConsumer()
	s := &graphstream.MemorySink{}
	first, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, s)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("## Title\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s = &graphstream.MemorySink{}
	delta, err := Run(context.Background(), eng, dir, s, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if delta.TargetGeneration == first.TargetGeneration || len(s.CloneRecords()) == 0 {
		t.Fatalf("heading-level edit suppressed: gen %d->%d events=%d", first.TargetGeneration, delta.TargetGeneration, len(s.CloneRecords()))
	}
	applyRun(t, cons, s)

	cold := NewConsumer()
	s = &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, dir, s, Options{StateDir: filepath.Join(dir, ".enola", "cold")}); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cold, s)
	assertAppliedEqualsCold(t, cons, cold)
}

func TestFactsFingerprint_TrueNoopUnchanged(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"README.md": "# Title\n"})
	eng := mdintentEngine(t, dir, false)
	state := filepath.Join(dir, ".enola", "state")
	first, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	second, err := Run(context.Background(), eng, dir, sink, Options{StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	if second.ParsedFiles != 0 || len(sink.CloneRecords()) != 0 || second.TargetGeneration != first.TargetGeneration {
		t.Fatalf("true no-op parsed=%d msgs=%d gen %d->%d",
			second.ParsedFiles, len(sink.CloneRecords()), first.TargetGeneration, second.TargetGeneration)
	}
}

func withProps(f facts.Fact, props map[string]any) facts.Fact {
	f.Props = props
	return f
}
