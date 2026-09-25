package graphsession

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
	"github.com/enola-labs/enola/internal/graphstream"
)

// The legacy* functions below are the pre-optimization bodies, kept verbatim as
// the oracle for the rewritten ones. Every equality test in this file compares
// the current implementation against them, so the optimization is held to exact
// output equality rather than to a restatement of what it was meant to do.

func legacyFileContributionFacts(files map[string]*FileState, repo string) []facts.Fact {
	var ff []facts.Fact
	for _, rec := range files {
		ff = append(ff, fileFacts(rec)...)
		if rec != nil {
			for _, contrib := range rec.Contrib {
				ff = append(ff, contrib...)
			}
		}
	}
	return legacyPublishedResolutionFacts(ff, repo)
}

func legacyPublishedResolutionFacts(ff []facts.Fact, repo string) []facts.Fact {
	out := make([]facts.Fact, 0, len(ff))
	for _, f := range ff {
		if ownerOf(f).Kind == graphstream.OwnerSynthetic {
			continue
		}
		out = append(out, canonResolutionFact(f, repo))
	}
	return out
}

func legacyOverlayStablePublished(base, published []facts.Fact, unstable map[string]bool) []facts.Fact {
	have := make(map[string]bool, len(base))
	for _, f := range base {
		have[candidateIdentity(f)] = true
	}
	out := append([]facts.Fact(nil), base...)
	for _, f := range published {
		o := ownerOf(f)
		if o.Kind == graphstream.OwnerFile && unstable[o.ID] {
			continue
		}
		k := candidateIdentity(f)
		if have[k] {
			continue
		}
		out = append(out, f)
		have[k] = true
	}
	return out
}

func legacyBuildIndex(ff []facts.Fact) *idIndex {
	idx := &idIndex{byName: map[string][]facts.Fact{}}
	for _, f := range ff {
		idx.byName[f.Name] = append(idx.byName[f.Name], f)
	}
	return idx
}

func legacyGroupOwners(ff []facts.Fact) []ownerOutput {
	order := make([]graphstream.OwnerRef, 0)
	grouped := map[string]*ownerOutput{}
	for _, f := range ff {
		o := ownerOf(f)
		key := o.String()
		g, ok := grouped[key]
		if !ok {
			g = &ownerOutput{Owner: o}
			grouped[key] = g
			order = append(order, o)
		}
		g.Facts = append(g.Facts, f)
	}
	out := make([]ownerOutput, 0, len(order))
	for _, o := range order {
		out = append(out, *grouped[o.String()])
	}
	return out
}

func legacyCandidateFingerprint(idx *idIndex, name string, ignoredFiles map[string]bool) []string {
	if idx == nil {
		return nil
	}
	if ignoredFiles == nil {
		ignoredFiles = map[string]bool{}
	}
	rows := make([]string, 0, len(idx.byName[name]))
	for _, f := range idx.byName[name] {
		if ignoredFiles[canonicalFactFile(f.File)] {
			continue
		}
		rows = append(rows, candidateIdentity(f))
	}
	sort.Strings(rows)
	return rows
}

func legacyChangedCandidateNames(old, next *idIndex, ignored ...map[string]bool) map[string]bool {
	ignoredFiles := map[string]bool{}
	if len(ignored) > 0 && ignored[0] != nil {
		ignoredFiles = ignored[0]
	}
	changed := map[string]bool{}
	all := map[string]bool{}
	if old != nil {
		for name := range old.byName {
			all[name] = true
		}
	}
	if next != nil {
		for name := range next.byName {
			all[name] = true
		}
	}
	for name := range all {
		if !slicesEqual(legacyCandidateFingerprint(old, name, ignoredFiles), legacyCandidateFingerprint(next, name, ignoredFiles)) {
			changed[name] = true
		}
	}
	return changed
}

// factMultiset is an order-insensitive comparison key covering every Fact field,
// for the one output whose order is a map walk in both implementations.
func factMultiset(ff []facts.Fact) []string {
	out := make([]string, 0, len(ff))
	for _, f := range ff {
		out = append(out, fmt.Sprintf("%#v", f))
	}
	sort.Strings(out)
	return out
}

// resolutionFixture is a small graph that exercises every branch the rewrite
// touches: duplicate identities in the base, synthetic owners of all three
// shapes, empty and explicit repo labels, backslash paths, multi-fact names, and
// facts carrying full field sets including Props and Relations.
func resolutionFixture() map[string]*FileState {
	full := facts.Fact{
		Kind: facts.KindSymbol, Name: "Full", File: "pkg/full.ts",
		Line: 3, EndLine: 9, Column: 2, EndColumn: 40,
		Repo:      "explicit-repo",
		Props:     map[string]any{"exported": true, "body": "nfkc"},
		Relations: []facts.Relation{{Kind: "calls", Target: "Dup", TargetFile: "pkg/a.ts"}},
	}
	return map[string]*FileState{
		"pkg/a.ts": {
			Facts: []facts.Fact{
				{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/a.ts"},
				{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/a.ts"},
				{Kind: facts.KindSymbol, Name: "Dup", File: "pkg\\a.ts"},
				{Kind: facts.KindModule, Name: "pkg/a", File: "pkg/a"},
				{Kind: facts.KindExtraction, Name: "typescript", File: "pkg/a.ts"},
				{Kind: facts.KindSymbol, Name: "Unfiled", File: ""},
			},
			Contrib: map[string][]facts.Fact{
				"router": {{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/a.ts", Repo: "other-repo"}},
			},
		},
		"pkg/full.ts": {Facts: []facts.Fact{full}},
		"pkg/b.ts": {
			Facts: []facts.Fact{
				{Kind: facts.KindSymbol, Name: "Solo", File: "pkg/b.ts"},
				{Kind: facts.KindFileRef, Name: "pkg/b", File: "pkg/b"},
			},
		},
		"pkg/nil.ts": nil,
	}
}

// rawContributionFacts is the assembled shape BEFORE the published projection:
// no synthetic filter, no canonicalization. groupOwners runs on facts of this
// shape in the session, so it is what the owner-key comparison must be fed.
func rawContributionFacts(files map[string]*FileState) []facts.Fact {
	var ff []facts.Fact
	for _, rec := range files {
		ff = append(ff, fileFacts(rec)...)
		if rec != nil {
			for _, contrib := range rec.Contrib {
				ff = append(ff, contrib...)
			}
		}
	}
	return ff
}

func TestFileContributionFactsMatchesLegacy(t *testing.T) {
	files := resolutionFixture()
	got := factMultiset(fileContributionFacts(files, "repo-label"))
	want := factMultiset(legacyFileContributionFacts(files, "repo-label"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fused contribution projection differs from legacy:\n got=%v\nwant=%v", got, want)
	}
	if len(got) == 0 {
		t.Fatal("fixture produced no published facts")
	}
}

func TestFileContributionFactsRepoLabelsAndSyntheticFilter(t *testing.T) {
	out := fileContributionFacts(resolutionFixture(), "repo-label")
	var filled, explicit, other int
	for _, f := range out {
		if ownerOf(f).Kind == graphstream.OwnerSynthetic {
			t.Fatalf("synthetic owner survived the projection: %+v", f)
		}
		switch f.Repo {
		case "repo-label":
			filled++
		case "explicit-repo":
			explicit++
		case "other-repo":
			other++
		default:
			t.Fatalf("unexpected repo label %q on %+v", f.Repo, f)
		}
	}
	if filled == 0 || explicit != 1 || other != 1 {
		t.Fatalf("repo labels: filled=%d explicit=%d other=%d", filled, explicit, other)
	}

	// Path normalization is asserted on its own terms rather than by excusing a
	// fact from the repo check. canonResolutionFact applies filepath.ToSlash and
	// nothing else, so the backslash path must come through exactly as ToSlash
	// leaves it, which is NOT candidateIdentity's broader backslash rewrite. That
	// asymmetry is deliberate and must survive the rewrite.
	var sawBackslashInput bool
	for _, f := range out {
		if f.Name != "Dup" {
			continue
		}
		for _, in := range rawContributionFacts(resolutionFixture()) {
			if in.Name == "Dup" && in.File == "pkg\\a.ts" {
				sawBackslashInput = true
			}
		}
		if f.File != filepath.ToSlash(f.File) {
			t.Fatalf("projected File is not ToSlash-normalized: %q", f.File)
		}
	}
	if !sawBackslashInput {
		t.Fatal("fixture no longer carries a backslash path")
	}
	var backslashKept bool
	for _, f := range out {
		if f.File == filepath.ToSlash("pkg\\a.ts") && f.File != "pkg/a.ts" {
			backslashKept = true
		}
	}
	if filepath.ToSlash("pkg\\a.ts") != "pkg/a.ts" && !backslashKept {
		t.Fatal("ToSlash rewrote the backslash path but the projection dropped the result")
	}
}

func TestFileContributionFactsPreservesFullFactFields(t *testing.T) {
	out := fileContributionFacts(resolutionFixture(), "repo-label")
	var got *facts.Fact
	for i := range out {
		if out[i].Name == "Full" {
			got = &out[i]
		}
	}
	if got == nil {
		t.Fatal("Full fact missing from projection")
	}
	want := facts.Fact{
		Kind: facts.KindSymbol, Name: "Full", File: "pkg/full.ts",
		Line: 3, EndLine: 9, Column: 2, EndColumn: 40,
		Repo:      "explicit-repo",
		Props:     map[string]any{"exported": true, "body": "nfkc"},
		Relations: []facts.Relation{{Kind: "calls", Target: "Dup", TargetFile: "pkg/a.ts"}},
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("field loss in projection:\n got=%+v\nwant=%+v", *got, want)
	}
}

func TestOverlayStablePublishedMatchesLegacy(t *testing.T) {
	files := resolutionFixture()
	base := fileContributionFacts(files, "repo-label")
	published := publishedResolutionFacts([]facts.Fact{
		{Kind: facts.KindSymbol, Name: "Added", File: "pkg/c.ts", Repo: "repo-label"},
		{Kind: facts.KindSymbol, Name: "Added", File: "pkg/c.ts", Repo: "repo-label"},
		{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/a.ts", Repo: "repo-label"},
		{Kind: facts.KindSymbol, Name: "Unstable", File: "pkg/hot.ts", Repo: "repo-label"},
		{Kind: facts.KindModule, Name: "pkg/c", File: "pkg/c"},
	}, "repo-label")
	unstable := map[string]bool{"pkg/hot.ts": true}

	got := overlayStablePublished(base, published, unstable)
	want := legacyOverlayStablePublished(base, published, unstable)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overlay differs from legacy:\n got=%v\nwant=%v", got, want)
	}
	for _, f := range got {
		if f.Name == "Unstable" {
			t.Fatal("unstable file owner leaked into the overlay")
		}
	}
}

func TestOverlayStablePublishedPreservesBaseDuplicateMultiplicity(t *testing.T) {
	dup := facts.Fact{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/a.ts", Repo: "r"}
	base := []facts.Fact{dup, dup, dup}
	// The same identity arrives twice more from the published side.
	out := overlayStablePublished(base, []facts.Fact{dup, dup}, nil)
	if len(out) != 3 {
		t.Fatalf("overlay len=%d, want 3: base multiplicity must survive and additions must dedup", len(out))
	}
	for _, f := range out {
		if !reflect.DeepEqual(f, dup) {
			t.Fatalf("overlay altered a base fact: %+v", f)
		}
	}
}

func TestOverlayStablePublishedDeduplicatesAdditionsAmongThemselves(t *testing.T) {
	add := facts.Fact{Kind: facts.KindSymbol, Name: "Added", File: "pkg/c.ts", Repo: "r"}
	out := overlayStablePublished(nil, []facts.Fact{add, add, add}, nil)
	if len(out) != 1 {
		t.Fatalf("addition multiplicity = %d, want 1", len(out))
	}
}

func TestOverlayStablePublishedDoesNotAliasOrMutateBase(t *testing.T) {
	props := map[string]any{"exported": true}
	base := []facts.Fact{{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/a.ts", Repo: "r", Props: props}}
	baseCopy := append([]facts.Fact(nil), base...)
	out := overlayStablePublished(base, []facts.Fact{
		{Kind: facts.KindSymbol, Name: "Added", File: "pkg/c.ts", Repo: "r"},
	}, nil)
	if len(out) != 2 {
		t.Fatalf("overlay len=%d, want 2", len(out))
	}
	// Writing through the result must not reach the caller's base slice.
	out[0].Name = "Mutated"
	out[0].File = "elsewhere.ts"
	if !reflect.DeepEqual(base, baseCopy) {
		t.Fatalf("overlay result aliases the base backing array: base=%v", base)
	}
	// Props is a reference field: the copy carries the same map header, exactly as
	// the legacy append([]facts.Fact(nil), base...) did. Pin that unchanged rather
	// than deep-copying it here, and pin that the struct copy is what isolates the
	// two slices.
	if len(out[0].Props) != 1 || out[0].Props["exported"] != true {
		t.Fatalf("copied fact lost its Props: %v", out[0].Props)
	}
	legacyOut := legacyOverlayStablePublished(base, []facts.Fact{
		{Kind: facts.KindSymbol, Name: "Added", File: "pkg/c.ts", Repo: "r"},
	}, nil)
	sharedNow := reflect.ValueOf(out[0].Props).Pointer() == reflect.ValueOf(base[0].Props).Pointer()
	sharedLegacy := reflect.ValueOf(legacyOut[0].Props).Pointer() == reflect.ValueOf(base[0].Props).Pointer()
	if sharedNow != sharedLegacy {
		t.Fatalf("Props sharing changed: now=%v legacy=%v", sharedNow, sharedLegacy)
	}
	if base[0].Props["exported"] != true {
		t.Fatal("base Props mutated")
	}
}

func TestOverlayCapacityIsExactNotBaseAndPublishedSum(t *testing.T) {
	base := make([]facts.Fact, 1000)
	for i := range base {
		base[i] = facts.Fact{Kind: facts.KindSymbol, Name: fmt.Sprintf("N%d", i), File: "pkg/a.ts", Repo: "r"}
	}
	published := append([]facts.Fact(nil), base...)
	published = append(published, facts.Fact{Kind: facts.KindSymbol, Name: "Extra", File: "pkg/c.ts", Repo: "r"})
	out := overlayStablePublished(base, published, nil)
	if len(out) != 1001 {
		t.Fatalf("overlay len=%d, want 1001", len(out))
	}
	if cap(out) != 1001 {
		t.Fatalf("overlay cap=%d, want exactly 1001: sizing by len(base)+len(published) reserves %d for one addition", cap(out), len(base)+len(published))
	}
}

func TestBuildIndexMatchesLegacy(t *testing.T) {
	ff := fileContributionFacts(resolutionFixture(), "repo-label")
	got := buildIndex(ff)
	want := legacyBuildIndex(ff)
	if !reflect.DeepEqual(got.byName, want.byName) {
		t.Fatalf("index differs from legacy:\n got=%v\nwant=%v", got.byName, want.byName)
	}
}

func TestBuildIndexBucketsCannotOverwriteAdjacentBucket(t *testing.T) {
	// Every bucket is carved from one backing array. Without a full slice
	// expression, appending to a bucket would write into whichever bucket happens
	// to follow it. Append to every bucket and assert nothing else moved.
	var ff []facts.Fact
	for i := 0; i < 8; i++ {
		for j := 0; j <= i%3; j++ {
			ff = append(ff, facts.Fact{Kind: facts.KindSymbol, Name: fmt.Sprintf("N%d", i), File: fmt.Sprintf("f%d_%d.ts", i, j), Repo: "r"})
		}
	}
	idx := buildIndex(ff)
	want := legacyBuildIndex(ff)
	for name, bucket := range idx.byName {
		if cap(bucket) != len(bucket) {
			t.Fatalf("bucket %q cap=%d len=%d: spare capacity reaches into the next bucket", name, cap(bucket), len(bucket))
		}
		_ = append(bucket, facts.Fact{Kind: facts.KindSymbol, Name: "INTRUDER", File: "intruder.ts", Repo: "r"})
	}
	if !reflect.DeepEqual(idx.byName, want.byName) {
		t.Fatalf("append to one bucket corrupted another:\n got=%v\nwant=%v", idx.byName, want.byName)
	}
}

func TestGroupOwnersMatchesLegacy(t *testing.T) {
	ff := fileContributionFacts(resolutionFixture(), "repo-label")
	// groupOwners runs before the synthetic filter in the session, so the second
	// input must be genuinely unfiltered: the published projection drops every
	// synthetic owner, so comparing two projected inputs would never exercise the
	// synthetic side of the OwnerRef key conversion at all.
	unfiltered := rawContributionFacts(resolutionFixture())
	var sawModule, sawAggregate, sawUnfiled bool
	for _, f := range unfiltered {
		switch o := ownerOf(f); {
		case o.Kind != graphstream.OwnerSynthetic:
		case strings.HasPrefix(o.ID, "module:"):
			sawModule = true
		case o.ID == "aggregate:typescript":
			sawAggregate = true
		case strings.HasPrefix(o.ID, "unfiled:"):
			sawUnfiled = true
		}
	}
	if !sawModule || !sawAggregate || !sawUnfiled {
		t.Fatalf("unfiltered input lacks synthetic owners: module=%v aggregate=%v unfiled=%v", sawModule, sawAggregate, sawUnfiled)
	}
	for _, in := range [][]facts.Fact{ff, unfiltered} {
		got := groupOwners(in)
		want := legacyGroupOwners(in)
		if len(got) != len(want) {
			t.Fatalf("owner count %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].Owner != want[i].Owner {
				t.Fatalf("owner %d = %v, want %v", i, got[i].Owner, want[i].Owner)
			}
			if !reflect.DeepEqual(got[i].Facts, want[i].Facts) {
				t.Fatalf("owner %v facts differ:\n got=%v\nwant=%v", got[i].Owner, got[i].Facts, want[i].Facts)
			}
		}
	}
}

func TestGroupOwnersBucketsCannotOverwriteAdjacentBucket(t *testing.T) {
	var ff []facts.Fact
	for i := 0; i < 8; i++ {
		for j := 0; j <= i%3; j++ {
			ff = append(ff, facts.Fact{Kind: facts.KindSymbol, Name: fmt.Sprintf("N%d_%d", i, j), File: fmt.Sprintf("f%d.ts", i), Repo: "r"})
		}
	}
	got := groupOwners(ff)
	want := legacyGroupOwners(ff)
	for i := range got {
		if cap(got[i].Facts) != len(got[i].Facts) {
			t.Fatalf("owner %v cap=%d len=%d: spare capacity reaches into the next owner", got[i].Owner, cap(got[i].Facts), len(got[i].Facts))
		}
		_ = append(got[i].Facts, facts.Fact{Kind: facts.KindSymbol, Name: "INTRUDER", File: "intruder.ts", Repo: "r"})
	}
	for i := range want {
		if !reflect.DeepEqual(got[i].Facts, want[i].Facts) {
			t.Fatalf("append to one owner bucket corrupted another at %v:\n got=%v\nwant=%v", got[i].Owner, got[i].Facts, want[i].Facts)
		}
	}
}

func TestChangedCandidateNamesMatchesLegacy(t *testing.T) {
	base := fileContributionFacts(resolutionFixture(), "repo-label")
	next := append(append([]facts.Fact(nil), base...),
		facts.Fact{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/z.ts", Repo: "repo-label"},
		facts.Fact{Kind: facts.KindSymbol, Name: "Fresh", File: "pkg/z.ts", Repo: "repo-label"},
	)
	old, nxt := buildIndex(base), buildIndex(next)
	for _, ignored := range []map[string]bool{nil, {}, {"pkg/a.ts": true}, {"pkg/z.ts": true}} {
		got := changedCandidateNames(old, nxt, ignored)
		want := legacyChangedCandidateNames(old, nxt, ignored)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ignored=%v: changed names differ:\n got=%v\nwant=%v", ignored, got, want)
		}
	}
	// A dropped name and a nil side must behave as before too.
	if got, want := changedCandidateNames(nxt, old, nil), legacyChangedCandidateNames(nxt, old, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("reversed direction differs:\n got=%v\nwant=%v", got, want)
	}
	if got, want := changedCandidateNames(nil, nxt, nil), legacyChangedCandidateNames(nil, nxt, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("nil old side differs:\n got=%v\nwant=%v", got, want)
	}
	if got := changedCandidateNames(nil, nil, nil); len(got) != 0 {
		t.Fatalf("two nil sides = %v", got)
	}
}

func TestCandidateKeyAgreesWithFactIdentityOnTheLegalDomain(t *testing.T) {
	// candidateKey is the tuple that candidateIdentity hashes. Over this sample of
	// NUL-free inputs, tuple equality and id equality agree in both directions.
	// That is sample evidence, not a universal guarantee: a 128-bit truncation
	// collision would make two distinct tuples share an id, and no test can rule
	// that out. The direction of the difference is what matters and is asserted
	// separately below.
	sample := []facts.Fact{
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "pkg/a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "pkg\\a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "pkg/b.ts"},
		{Repo: "r", Kind: facts.KindModule, Name: "Foo", File: "pkg/a.ts"},
		{Repo: "other", Kind: facts.KindSymbol, Name: "Foo", File: "pkg/a.ts"},
		{Repo: "", Kind: facts.KindSymbol, Name: "Foo", File: "pkg/a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "", File: "pkg/a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: ""},
		// Fields outside the identity must not reach the key.
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "pkg/a.ts", Line: 42, Props: map[string]any{"body": "x"}},
	}
	for i, a := range sample {
		for j, b := range sample {
			sameKey := candidateKey(a) == candidateKey(b)
			sameID := candidateIdentity(a) == candidateIdentity(b)
			if sameKey != sameID {
				t.Fatalf("sample %d vs %d: tuple equality %v, identity equality %v\n a=%+v\n b=%+v", i, j, sameKey, sameID, a, b)
			}
		}
	}
}

func TestCandidateKeySeparatesDelimiterInvalidInputsThatTheIdMerges(t *testing.T) {
	// Documented limit, asserted rather than asserted away. FactID joins the four
	// fields with NUL, so moving that delimiter between Repo and Kind produces two
	// facts with the SAME Name -- one idIndex bucket -- whose id bytes are
	// identical but whose identity tuples differ. factIDInto states that no field
	// may contain a NUL, but nothing validates it, so such inputs are reachable.
	a := facts.Fact{Repo: "r", Kind: "k\x00x", Name: "Foo", File: "a.ts"}
	b := facts.Fact{Repo: "r\x00k", Kind: "x", Name: "Foo", File: "a.ts"}
	if candidateIdentity(a) != candidateIdentity(b) {
		t.Skip("NUL-bearing inputs no longer collide under FactID; the limit note needs revisiting")
	}
	if candidateKey(a) == candidateKey(b) {
		t.Fatal("tuple key merged two facts that differ in their identity fields")
	}

	idxA, idxB := buildIndex([]facts.Fact{a}), buildIndex([]facts.Fact{b})
	if len(idxA.byName["Foo"]) != 1 || len(idxB.byName["Foo"]) != 1 {
		t.Fatal("collision facts did not land in the same name bucket")
	}
	// The legacy hash fingerprint cannot tell these two apart.
	if !slicesEqual(legacyCandidateFingerprint(idxA, "Foo", nil), legacyCandidateFingerprint(idxB, "Foo", nil)) {
		t.Fatal("legacy fingerprint already separated them; the collision fixture is wrong")
	}
	if len(legacyChangedCandidateNames(idxA, idxB, nil)) != 0 {
		t.Fatal("legacy reported a change; the collision fixture is wrong")
	}
	// The tuple fingerprint does, and reports the name changed.
	if slicesEqual(candidateFingerprint(idxA, "Foo", nil), candidateFingerprint(idxB, "Foo", nil)) {
		t.Fatal("tuple fingerprint merged delimiter-invalid inputs")
	}
	if !changedCandidateNames(idxA, idxB, nil)["Foo"] {
		t.Fatal("tuple comparison did not report the name changed")
	}
	// Both sides in one bucket: the tuple keeps two entries where the hash keeps
	// two equal ones, so multiplicity is unaffected either way.
	both := buildIndex([]facts.Fact{a, b})
	if len(candidateFingerprint(both, "Foo", nil)) != 2 {
		t.Fatal("tuple fingerprint collapsed a two-fact bucket")
	}
}

func TestResolutionIndexesMatchesLegacyEndToEnd(t *testing.T) {
	copyFiles := func(in map[string]*FileState) map[string]*FileState {
		out := make(map[string]*FileState, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	base := resolutionFixture()

	edited := copyFiles(base)
	editedRec := *base["pkg/a.ts"]
	editedRec.Hash = "changed"
	edited["pkg/a.ts"] = &editedRec

	removed := copyFiles(base)
	delete(removed, "pkg/b.ts")

	added := copyFiles(base)
	added["pkg/c.ts"] = &FileState{Hash: "new", Facts: []facts.Fact{
		{Kind: facts.KindSymbol, Name: "Fresh", File: "pkg/c.ts"},
		{Kind: facts.KindSymbol, Name: "Dup", File: "pkg/c.ts"},
	}}

	cases := []struct {
		name       string
		prev, next map[string]*FileState
	}{
		{"cold: nil prev", nil, base},
		{"cold: empty prev", map[string]*FileState{}, base},
		{"delta: nothing changed", base, copyFiles(base)},
		{"delta: one body hash changed", base, edited},
		{"delta: structural, a file removed", base, removed},
		{"delta: structural, a file added", base, added},
	}

	for _, c := range cases {
		// assembled is the session's allFacts for the NEW state, unfiltered.
		assembled := rawContributionFacts(c.next)

		gotOld, gotNext := resolutionIndexes(c.prev, c.next, assembled, "repo-label")
		wantPublished := legacyPublishedResolutionFacts(assembled, "repo-label")
		wantOldFacts := legacyOverlayStablePublished(legacyFileContributionFacts(c.prev, "repo-label"), wantPublished, changedContentOwners(c.prev, c.next))
		wantOld, wantNext := legacyBuildIndex(wantOldFacts), legacyBuildIndex(wantPublished)

		if !reflect.DeepEqual(gotNext.byName, wantNext.byName) {
			t.Fatalf("%s: next index differs from legacy:\n got=%v\nwant=%v", c.name, gotNext.byName, wantNext.byName)
		}
		// The old index is built over a map walk of prev in both implementations,
		// so bucket order within a name is a map order on both sides; compare the
		// buckets as multisets over every Fact field.
		if len(gotOld.byName) != len(wantOld.byName) {
			t.Fatalf("%s: old index names %d, want %d", c.name, len(gotOld.byName), len(wantOld.byName))
		}
		for n, bucket := range gotOld.byName {
			if !reflect.DeepEqual(factMultiset(bucket), factMultiset(wantOld.byName[n])) {
				t.Fatalf("%s: old index bucket %q differs:\n got=%v\nwant=%v", c.name, n, bucket, wantOld.byName[n])
			}
		}
		if got, want := changedCandidateNames(gotOld, gotNext, nil), legacyChangedCandidateNames(wantOld, wantNext, nil); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: changed candidate names differ:\n got=%v\nwant=%v", c.name, got, want)
		}
	}

	// The cold cases must actually be cold: no prev contributions at all.
	if got := fileContributionFacts(nil, "repo-label"); len(got) != 0 {
		t.Fatalf("nil prev produced %d contribution facts", len(got))
	}
	// On a cold run every file gains a content hash, so changedContentOwners marks
	// every file owner unstable and the overlay contributes nothing from them: the
	// old index is a strict subset of the published one rather than equal to it.
	// That is the pre-existing behavior; assert it holds rather than assuming the
	// overlay reproduces the published set.
	coldOld, coldNext := resolutionIndexes(nil, base, rawContributionFacts(base), "repo-label")
	if len(coldOld.byName) >= len(coldNext.byName) {
		t.Fatalf("cold old index has %d names, next has %d: every owner is unstable on a cold run", len(coldOld.byName), len(coldNext.byName))
	}
	for name, bucket := range coldOld.byName {
		for _, f := range bucket {
			var found bool
			for _, g := range coldNext.byName[name] {
				if candidateKey(f) == candidateKey(g) {
					found = true
				}
			}
			if !found {
				t.Fatalf("cold overlay invented a candidate absent from the published set: %+v", f)
			}
		}
	}
}
