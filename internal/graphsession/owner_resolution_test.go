package graphsession

import (
	"testing"

	"github.com/enola-labs/enola/internal/extractors/tsextractor"
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

func TestChangedCandidateNamesDetectsIdentityMultisetCountChange(t *testing.T) {
	a := buildIndex([]facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}})
	b := buildIndex([]facts.Fact{
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
		{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"},
	})
	if got := changedCandidateNames(a, b, nil); !got["Foo"] {
		t.Fatalf("Identity() multiset count change not detected: %v", got)
	}
}

func TestIdIndexCandidateFingerprint(t *testing.T) {
	foo := facts.Fact{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts", Line: 3}
	fooBody := facts.Fact{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts", Line: 9, Props: map[string]any{"body": "nfkc"}}
	bar := facts.Fact{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "b.ts"}
	idxOrderA := buildIndex([]facts.Fact{bar, foo, foo})
	idxOrderB := buildIndex([]facts.Fact{fooBody, bar, fooBody})
	fpA := candidateFingerprint(idxOrderA, "Foo", nil)
	fpB := candidateFingerprint(idxOrderB, "Foo", nil)
	if !slicesEqual(fpA, fpB) {
		t.Fatalf("fingerprint not a sorted Identity() multiset:\n a=%v\n b=%v", fpA, fpB)
	}
	if len(fpA) != 3 {
		t.Fatalf("fingerprint len=%d, want 3 (multiset, not unique set)", len(fpA))
	}
	if fpA[0] == fpA[2] {
		t.Fatal("collision identities collapsed")
	}
	idxCollision := buildIndex([]facts.Fact{foo, bar})
	idxSingle := buildIndex([]facts.Fact{foo})
	if slicesEqual(candidateFingerprint(idxCollision, "Foo", nil), candidateFingerprint(idxSingle, "Foo", nil)) {
		t.Fatal("file collision produced identical candidate fingerprint")
	}
	idxEmpty := buildIndex(nil)
	if len(candidateFingerprint(idxEmpty, "Foo", nil)) != 0 {
		t.Fatal("empty index fingerprint")
	}
	slashA := buildIndex([]facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "pkg/a.ts"}})
	slashB := buildIndex([]facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "pkg\\a.ts"}})
	if !slicesEqual(candidateFingerprint(slashA, "Foo", nil), candidateFingerprint(slashB, "Foo", nil)) {
		t.Fatal("candidate Identity() fingerprint depends on path separators")
	}
}

func TestChangedResolutionOwnersUnchangedCandidateDomainDoesNotWidenAppTS(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	password := "packages/crypto/src/password.ts"
	independent := "packages/crypto/src/keys.ts"
	appFact := func() facts.Fact {
		return facts.Fact{
			Repo: "r", Kind: facts.KindSymbol, Name: "listen", File: app,
			Relations: []facts.Relation{
				{Kind: "calls", Target: "normalizeEmail"},
				{Kind: "imports", Target: "fastify"},
				{Kind: "imports", Target: "apps/architect-console/src/server.config"},
			},
		}
	}
	oldFiles := map[string]*FileState{
		password: {
			TS: &tsextractor.FileRecord{File: password, Facts: []facts.Fact{{
				Repo: "r", Kind: facts.KindSymbol, Name: "normalizeEmail", File: password, Line: 10,
				Props: map[string]any{"body": "trim"},
			}}},
		},
		app: {TS: &tsextractor.FileRecord{File: app, Facts: []facts.Fact{appFact()}}},
		independent: {TS: &tsextractor.FileRecord{File: independent, Facts: []facts.Fact{{
			Repo: "r", Kind: facts.KindSymbol, Name: "loadKey", File: independent,
			Relations: []facts.Relation{{Kind: "calls", Target: "normalizeEmail"}},
		}}}},
	}
	newFiles := map[string]*FileState{
		password: {
			TS: &tsextractor.FileRecord{File: password, Facts: []facts.Fact{{
				Repo: "r", Kind: facts.KindSymbol, Name: "normalizeEmail", File: password, Line: 12,
				Props: map[string]any{"body": "normalize-NFKC"},
			}}},
		},
		app:         {TS: &tsextractor.FileRecord{File: app, Facts: []facts.Fact{appFact()}}},
		independent: oldFiles[independent],
	}
	old := fileResolutionIndex(oldFiles, "r")
	next := fileResolutionIndex(newFiles, "r")
	if got := changedCandidateNames(old, next, nil); len(got) != 0 {
		t.Fatalf("body-only identity fields changed candidate domain: %v", got)
	}
	published := []facts.Fact{
		newFiles[password].TS.Facts[0],
		newFiles[app].TS.Facts[0],
		newFiles[independent].TS.Facts[0],
		{Repo: "r", Kind: facts.KindRoute, Name: "normalizeEmail", File: app},
		{Repo: "r", Kind: facts.KindModule, Name: "apps/architect-console/src/server", File: "apps/architect-console/src/server"},
	}
	got := changedResolutionOwners(groupOwners(published), old, next, nil)
	for _, o := range got {
		if o.ID == app {
			t.Fatalf("unchanged candidate domain widened %s: %v", app, got)
		}
	}
	if len(got) != 0 {
		t.Fatalf("unchanged candidate domain still widened owners %v", got)
	}
}

func TestChangedResolutionOwnersNewSymbolCollisionWidensAppTS(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	hmac := "packages/crypto/src/hmac.ts"
	other := "packages/crypto/src/keys.ts"
	appFact := facts.Fact{
		Repo: "r", Kind: facts.KindSymbol, Name: "listen", File: app,
		Relations: []facts.Relation{
			{Kind: "calls", Target: "hmacEquals"},
			{Kind: "calls", Target: "invalidationScopeProbe"},
			{Kind: "imports", Target: "fastify"},
		},
	}
	otherFact := facts.Fact{
		Repo: "r", Kind: facts.KindSymbol, Name: "loadKey", File: other,
		Relations: []facts.Relation{{Kind: "imports", Target: "fastify"}},
	}
	oldFiles := map[string]*FileState{
		hmac: {TS: &tsextractor.FileRecord{File: hmac, Facts: []facts.Fact{{
			Repo: "r", Kind: facts.KindSymbol, Name: "hmacEquals", File: hmac,
		}}}},
		app:   {TS: &tsextractor.FileRecord{File: app, Facts: []facts.Fact{appFact}}},
		other: {TS: &tsextractor.FileRecord{File: other, Facts: []facts.Fact{otherFact}}},
	}
	newFiles := map[string]*FileState{
		hmac: {TS: &tsextractor.FileRecord{File: hmac, Facts: []facts.Fact{
			{Repo: "r", Kind: facts.KindSymbol, Name: "hmacEquals", File: hmac},
			{Repo: "r", Kind: facts.KindSymbol, Name: "invalidationScopeProbe", File: hmac},
		}}},
		"packages/crypto/src/extra.ts": {TS: &tsextractor.FileRecord{File: "packages/crypto/src/extra.ts", Facts: []facts.Fact{{
			Repo: "r", Kind: facts.KindSymbol, Name: "hmacEquals", File: "packages/crypto/src/extra.ts",
		}}}},
		app:   {TS: &tsextractor.FileRecord{File: app, Facts: []facts.Fact{appFact}}},
		other: {TS: &tsextractor.FileRecord{File: other, Facts: []facts.Fact{otherFact}}},
	}
	got := changedResolutionOwners(
		groupOwners([]facts.Fact{
			newFiles[hmac].TS.Facts[0],
			newFiles[hmac].TS.Facts[1],
			newFiles["packages/crypto/src/extra.ts"].TS.Facts[0],
			appFact,
			otherFact,
		}),
		fileResolutionIndex(oldFiles, "r"),
		fileResolutionIndex(newFiles, "r"),
		nil,
	)
	foundApp := false
	for _, o := range got {
		if o.ID == app {
			foundApp = true
		}
		if o.ID == other {
			t.Fatalf("collision/new-symbol widened unrelated %s: %v", other, got)
		}
	}
	if !foundApp {
		t.Fatalf("new symbol and hmacEquals collision did not fail-closed widen %s: %v", app, got)
	}
}

func TestChangedResolutionOwnersAssembledProvenanceDoesNotWidenStableAppTS(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	password := "packages/crypto/src/password.ts"
	appFact := facts.Fact{
		Kind: facts.KindSymbol, Name: "listen", File: app,
		Relations: []facts.Relation{
			{Kind: "calls", Target: "hmacEquals"},
			{Kind: "imports", Target: "apps/architect-console/src/server"},
			{Kind: "imports", Target: "fastify"},
		},
	}
	oldPass := facts.Fact{Kind: facts.KindSymbol, Name: "normalizeEmail", File: password, Line: 10}
	newPass := facts.Fact{Kind: facts.KindSymbol, Name: "normalizeEmail", File: password, Line: 14, Props: map[string]any{"body": "nfkc"}}
	oldFiles := map[string]*FileState{
		password: {Hash: "old", TS: &tsextractor.FileRecord{File: password, Facts: []facts.Fact{oldPass}}},
		app:      {Hash: "app", TS: &tsextractor.FileRecord{File: app, Facts: []facts.Fact{appFact}}},
	}
	newFiles := map[string]*FileState{
		password: {Hash: "new", TS: &tsextractor.FileRecord{File: password, Facts: []facts.Fact{newPass}}},
		app:      {Hash: "app", TS: &tsextractor.FileRecord{File: app, Facts: []facts.Fact{appFact}}},
	}
	assembled := []facts.Fact{
		newPass,
		appFact,
		{Kind: facts.KindRoute, Name: "hmacEquals", File: app, Repo: "r"},
		{Kind: facts.KindSymbol, Name: "apps/architect-console/src/server", File: app},
		{Kind: facts.KindModule, Name: "apps/architect-console/src/server", File: "apps/architect-console/src/server"},
	}
	old, next := resolutionIndexes(oldFiles, newFiles, assembled, "r")
	if got := changedCandidateNames(old, next, nil); len(got) != 0 {
		t.Fatalf("stable owner assembled extras changed candidate domain: %v", got)
	}
	got := changedResolutionOwners(groupOwners(assembled), old, next, nil)
	for _, o := range got {
		if o.ID == app {
			t.Fatalf("body-only stable owner graph expanded %s: %v", app, got)
		}
	}
	if len(got) != 0 {
		t.Fatalf("body-only stable owner graph expanded %v", got)
	}
}

func TestResolutionIndexesDetectsNewNameCollision(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	oldFiles := map[string]*FileState{
		"a.ts": {Hash: "a", TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}}}},
		app:    {Hash: "app", TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "listen", File: app, Relations: []facts.Relation{{Target: "Foo"}}}}}},
	}
	newFiles := map[string]*FileState{
		"a.ts": {Hash: "a", TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}}}},
		"b.ts": {Hash: "b", TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "Foo", File: "b.ts"}}}},
		app:    {Hash: "app", TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Kind: facts.KindSymbol, Name: "listen", File: app, Relations: []facts.Relation{{Target: "Foo"}}}}}},
	}
	assembled := []facts.Fact{
		{Kind: facts.KindSymbol, Name: "Foo", File: "a.ts", Repo: "r"},
		{Kind: facts.KindSymbol, Name: "Foo", File: "b.ts", Repo: "r"},
		{Kind: facts.KindSymbol, Name: "listen", File: app, Repo: "r", Relations: []facts.Relation{{Target: "Foo"}}},
		{Kind: facts.KindRoute, Name: "listen", File: app, Repo: "r"},
	}
	old, next := resolutionIndexes(oldFiles, newFiles, assembled, "r")
	got := changedResolutionOwners(groupOwners(assembled), old, next, nil)
	found := false
	for _, o := range got {
		if o.ID == app {
			found = true
		}
	}
	if !found {
		t.Fatalf("new-file Foo collision did not fail-closed widen %s: %v", app, got)
	}
}

func TestChangedResolutionOwnersIgnoresComposedSessionExtras(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	files := map[string]*FileState{
		"packages/crypto/src/hmac.ts": {
			Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File: "packages/crypto/src/hmac.ts",
				Facts: []facts.Fact{{
					Repo: "r", Kind: facts.KindSymbol, Name: "hmacEquals", File: "packages/crypto/src/hmac.ts",
				}},
			},
		},
		app: {
			Extractor: "typescript",
			TS: &tsextractor.FileRecord{
				File: app,
				Facts: []facts.Fact{{
					Repo: "r", Kind: facts.KindSymbol, Name: "listen", File: app,
					Relations: []facts.Relation{{Kind: "calls", Target: "hmacEquals"}},
				}},
			},
		},
	}
	old := fileResolutionIndex(files, "r")
	next := fileResolutionIndex(files, "r")
	published := []facts.Fact{
		files["packages/crypto/src/hmac.ts"].TS.Facts[0],
		files[app].TS.Facts[0],
		{Repo: "r", Kind: facts.KindRoute, Name: "hmacEquals", File: app},
		{Repo: "r", Kind: facts.KindModule, Name: "apps/architect-console/src/server", File: "apps/architect-console/src/server"},
	}
	got := changedResolutionOwners(groupOwners(published), old, next, nil)
	if len(got) != 0 {
		t.Fatalf("cache-vs-composed extras flagged owners %v", got)
	}
}

func TestChangedResolutionOwnersDetectsFileRecordCollision(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	oldFiles := map[string]*FileState{
		"a.ts": {TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}}}},
		app:    {TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "listen", File: app, Relations: []facts.Relation{{Target: "Foo"}}}}}},
	}
	newFiles := map[string]*FileState{
		"a.ts": {TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "a.ts"}}}},
		"b.ts": {TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "Foo", File: "b.ts"}}}},
		app:    {TS: &tsextractor.FileRecord{Facts: []facts.Fact{{Repo: "r", Kind: facts.KindSymbol, Name: "listen", File: app, Relations: []facts.Relation{{Target: "Foo"}}}}}},
	}
	published := []facts.Fact{
		newFiles["a.ts"].TS.Facts[0],
		newFiles["b.ts"].TS.Facts[0],
		newFiles[app].TS.Facts[0],
	}
	got := changedResolutionOwners(groupOwners(published), fileResolutionIndex(oldFiles, "r"), fileResolutionIndex(newFiles, "r"), nil)
	found := false
	for _, o := range got {
		if o.ID == app {
			found = true
		}
	}
	if !found {
		t.Fatalf("collision did not flag %s: %v", app, got)
	}
}

func TestAddChangedRouteFilesComparesRecordRoutes(t *testing.T) {
	app := "apps/architect-console/src/server/app.ts"
	records := []facts.Fact{{Kind: facts.KindSymbol, Name: "listen", File: app}}
	scope := map[string]bool{}
	addChangedRouteFiles(scope, records, records)
	if len(scope) != 0 {
		t.Fatalf("unchanged records expanded scope %v", scope)
	}
	added := []facts.Fact{
		{Kind: facts.KindSymbol, Name: "listen", File: app},
		{Kind: facts.KindRoute, Name: "/health", File: app},
	}
	addChangedRouteFiles(scope, records, added)
	if !scope[app] {
		t.Fatal("FileRecord KindRoute addition was not detected")
	}
}
