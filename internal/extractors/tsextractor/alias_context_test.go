package tsextractor

import (
	"os"
	"path/filepath"
	"testing"
)

func completeRecord(specs, unresolved, resolved []string) *FileRecord {
	return &FileRecord{ImportSpecs: specs, UnresolvedSpecs: unresolved, ResolvedFiles: resolved, ImportComplete: true}
}

func TestAliasChangeAffects(t *testing.T) {
	added := func(key, target string) AliasChange {
		e := AliasEntry{Replacement: target}
		return AliasChange{Key: key, Cur: &e}
	}
	removed := func(key, target string) AliasChange {
		e := AliasEntry{Replacement: target}
		return AliasChange{Key: key, Prev: &e}
	}
	solo := map[string]AliasEntry{"@local/new": {Replacement: "packages/new/src/index.ts"}}

	cases := []struct {
		name   string
		change AliasChange
		merged map[string]AliasEntry
		rec    *FileRecord
		want   bool
	}{{
		name:   "no prior record cannot rule the change out",
		change: added("@local/new", "packages/new/src/index.ts"),
		merged: solo,
		rec:    nil,
		want:   true,
	}, {
		name:   "record without a complete import summary cannot rule it out",
		change: added("@local/new", "packages/new/src/index.ts"),
		merged: solo,
		rec:    &FileRecord{ImportSpecs: []string{"src/other.ts"}},
		want:   true,
	}, {
		name:   "complete record naming neither key nor target is untouched",
		change: added("@local/new", "packages/new/src/index.ts"),
		merged: solo,
		rec:    completeRecord([]string{"src/other.ts"}, nil, []string{"src/other.ts"}),
		want:   false,
	}, {
		name:   "unresolved specifier still written as the alias key is touched",
		change: added("@local/new", "packages/new/src/index.ts"),
		merged: solo,
		rec:    completeRecord(nil, []string{"@local/new"}, nil),
		want:   true,
	}, {
		name:   "subpath under the alias key is touched",
		change: added("@local/new", "packages/new/src/index.ts"),
		merged: solo,
		rec:    completeRecord(nil, []string{"@local/new/feature"}, nil),
		want:   true,
	}, {
		name:   "removal reaches a file resolved under the previous target",
		change: removed("@local/new", "packages/new/src"),
		merged: map[string]AliasEntry{},
		rec:    completeRecord([]string{"packages/new/src/index.ts"}, nil, []string{"packages/new/src/index.ts"}),
		want:   true,
	}, {
		name:   "a key competing by prefix with another declaration cannot be judged from normalized specs",
		change: added("@acme/ui", "packages/ui/src/index.ts"),
		merged: map[string]AliasEntry{
			"@acme/":   {Replacement: "legacy/"},
			"@acme/ui": {Replacement: "packages/ui/src/index.ts"},
		},
		rec: completeRecord([]string{"legacy/ui/button.ts"}, nil, []string{"legacy/ui/button.ts"}),
		// The record bound through "@acme/" and its spec is already normalized,
		// so nothing on it can say the new exact key does not now win.
		want: true,
	}, {
		name:   "an isolated key stays isolated when another declaration merely shares no prefix",
		change: added("@solo/x", "packages/solo/src/index.ts"),
		merged: map[string]AliasEntry{
			"@acme/":  {Replacement: "legacy/"},
			"@solo/x": {Replacement: "packages/solo/src/index.ts"},
		},
		rec:  completeRecord([]string{"legacy/ui/button.ts"}, nil, []string{"legacy/ui/button.ts"}),
		want: false,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AliasChangeAffects(tc.change, tc.merged, tc.rec); got != tc.want {
				t.Fatalf("AliasChangeAffects = %v, want %v", got, tc.want)
			}
		})
	}
}

// An unchanged configuration has to produce an empty change list whatever the
// cache holds. Everything else here rests on that: it is what lets an immediate
// second run consult no record at all, and so stay silent.
func TestAliasContextChangesIsEmptyForIdenticalProjections(t *testing.T) {
	ctx := map[string]string{
		aliasContextKey(aliasKindPackage, "", "@local/new"):   encodeAliasEntry(AliasEntry{Replacement: "packages/new/src/index.ts"}),
		aliasContextKey(aliasKindTSConfig, "apps/web", "@/"):  encodeAliasEntry(AliasEntry{Replacement: "apps/web/src/"}),
		"repository-wide frameworks":                          "unchanged",
		aliasContextKey(aliasKindPackage, "", "@local/other"): encodeAliasEntry(AliasEntry{Replacement: "packages/other/index.ts"}),
	}
	same := map[string]string{}
	for k, v := range ctx {
		same[k] = v
	}
	if got := AliasContextChanges(ctx, same); len(got) != 0 {
		t.Fatalf("identical projections reported %d alias changes: %+v", len(got), got)
	}
	added := map[string]string{}
	for k, v := range ctx {
		added[k] = v
	}
	added[aliasContextKey(aliasKindPackage, "", "@local/fresh")] = encodeAliasEntry(AliasEntry{Replacement: "packages/fresh/index.ts"})
	got := AliasContextChanges(ctx, added)
	if len(got) != 1 || got[0].Key != "@local/fresh" || got[0].Prev != nil || got[0].Cur == nil {
		t.Fatalf("one added package alias reported %+v", got)
	}
}

// A blanket trigger must not fire on an alias key, or the per-file answer that
// replaced it never gets asked.
func TestContextDifferenceDurableIgnoresAliasKeysOnly(t *testing.T) {
	before := map[string]string{
		aliasContextKey(aliasKindPackage, "", "@local/new"): encodeAliasEntry(AliasEntry{Replacement: "a"}),
		"selected root": "x",
	}
	after := map[string]string{
		aliasContextKey(aliasKindPackage, "", "@local/new"): encodeAliasEntry(AliasEntry{Replacement: "b"}),
		"selected root": "x",
	}
	if got := ContextDifferenceDurable(before, after, AliasStateStructured); len(got) != 0 {
		t.Fatalf("alias-only movement forced a durable reason: %v", got)
	}
	if got := ContextDifference(before, after); len(got) != 1 {
		t.Fatalf("ContextDifference should still report every key, got %v", got)
	}
	after["selected root"] = "y"
	if got := ContextDifferenceDurable(before, after, AliasStateStructured); len(got) != 1 {
		t.Fatalf("non-alias movement must still force, got %v", got)
	}
}

func TestAliasesForRootLetsTSConfigWinAndStaysScoped(t *testing.T) {
	ctx := map[string]string{
		aliasContextKey(aliasKindPackage, "", "@local/new"):         encodeAliasEntry(AliasEntry{Replacement: "packages/new/index.ts"}),
		aliasContextKey(aliasKindPackage, "", "@/"):                 encodeAliasEntry(AliasEntry{Replacement: "pkg/"}),
		aliasContextKey(aliasKindTSConfig, "apps/web", "@/"):        encodeAliasEntry(AliasEntry{Replacement: "apps/web/src/"}),
		aliasContextKey(aliasKindTSConfig, "apps/admin", "@admin/"): encodeAliasEntry(AliasEntry{Replacement: "apps/admin/src/"}),
	}
	web := AliasesForRoot(ctx, "apps/web", true)
	if web["@/"].Replacement != "apps/web/src/" {
		t.Fatalf("tsconfig alias did not override the package alias: %+v", web)
	}
	if web["@local/new"].Replacement != "packages/new/index.ts" {
		t.Fatalf("package alias missing from a rooted file: %+v", web)
	}
	if _, ok := web["@admin/"]; ok {
		t.Fatalf("another root's tsconfig alias leaked into apps/web: %+v", web)
	}
	// A tsconfig at the repository root and a package `exports` alias both live
	// at the empty root but do not have the same reach: aliasesForDir hands a
	// file its nearest root alone, so a file under apps/web never sees the
	// repository-root tsconfig declaration.
	ctx[aliasContextKey(aliasKindTSConfig, "", "@rootcfg/")] = encodeAliasEntry(AliasEntry{Replacement: "root/src/"})
	web = AliasesForRoot(ctx, "apps/web", true)
	if _, ok := web["@rootcfg/"]; ok {
		t.Fatalf("repository-root tsconfig alias leaked into a nested root: %+v", web)
	}
	if got := AliasesForRoot(ctx, "", true); got["@rootcfg/"].Replacement != "root/src/" {
		t.Fatalf("file whose nearest root is the repository root lost its tsconfig alias: %+v", got)
	}

	none := AliasesForRoot(ctx, "", false)
	if _, ok := none["@admin/"]; ok {
		t.Fatalf("a file under no alias root saw a rooted declaration: %+v", none)
	}
	if none["@/"].Replacement != "pkg/" {
		t.Fatalf("file under no root lost the package alias: %+v", none)
	}
	if _, ok := none["@rootcfg/"]; ok {
		t.Fatalf("file resolving against no root saw a tsconfig declaration: %+v", none)
	}
}

func TestFileAliasRootRoundTrip(t *testing.T) {
	root, ok := FileAliasRoot(encodeFileContext("abc123", "apps/web", true))
	if !ok || root != "apps/web" {
		t.Fatalf("round trip gave %q %v", root, ok)
	}
	if root, ok := FileAliasRoot(encodeFileContext("abc123", "", true)); !ok || root != "" {
		t.Fatalf("repository-root alias root lost: %q %v", root, ok)
	}
	if _, ok := FileAliasRoot(encodeFileContext("abc123", "", false)); ok {
		t.Fatalf("file with no alias root reported one")
	}
}

// TestLegacyPerFileDigestStaysAliasInclusive pins the shape a released state was
// written in. The per-file digest a state before this change carries has the
// whole alias map inside it, and it is the only thing such a state can be
// compared against; if the emitted one stopped matching that shape, every
// existing installation would read its own unchanged repository as fully
// changed. The structured base, which is what a state written from here on is
// compared against, must move for exactly the opposite reason: never for an
// alias declared somewhere else.
func TestLegacyPerFileDigestStaysAliasInclusive(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"root","workspaces":["packages/*"]}`)
	write("src/a.ts", "export const a = 1;\n")
	write("packages/one/package.json", `{"name":"@local/one","main":"./index.ts","types":"./index.ts"}`)
	write("packages/one/index.ts", "export const one = 1;\n")

	ext := New()
	files := []string{"src/a.ts", "packages/one/index.ts"}
	project := func() (map[string]string, map[string]string, map[string]string) {
		paths := []string{"package.json", "packages/one/package.json"}
		raw := map[string][]byte{}
		for _, p := range paths {
			b, err := os.ReadFile(filepath.Join(dir, p))
			if err != nil {
				t.Fatal(err)
			}
			raw[p] = b
		}
		ctx, perFile, perFileBase, _ := ext.SessionContext(dir, raw, paths, files, nil)
		return ctx, perFile, perFileBase
	}

	beforeCtx, beforePerFile, beforeBase := project()
	if _, ok := beforeCtx[legacyAliasAggregateKey]; !ok {
		t.Fatalf("no aggregate alias digest is emitted for a released state to compare against")
	}

	write("packages/two/package.json", `{"name":"@local/two","main":"./index.ts","types":"./index.ts"}`)
	write("packages/two/index.ts", "export const two = 1;\n")
	files = append(files, "packages/two/index.ts")
	afterCtx, afterPerFile, afterBase := project()

	if beforeCtx[legacyAliasAggregateKey] == afterCtx[legacyAliasAggregateKey] {
		t.Fatalf("publishing a package left the aggregate alias digest alone; a released state could not see it move")
	}
	if beforePerFile["src/a.ts"] == afterPerFile["src/a.ts"] {
		t.Fatalf("the legacy per-file digest stopped carrying the alias map; a released state would read every file as unchanged only by accident")
	}
	if beforeBase["src/a.ts"] != afterBase["src/a.ts"] {
		t.Fatalf("the structured base moved for a package the file cannot see: %q became %q", beforeBase["src/a.ts"], afterBase["src/a.ts"])
	}
}

func TestAliasStateModeForMatchesTheVersionExactly(t *testing.T) {
	for _, tc := range []struct {
		name         string
		meta         string
		files, bases int
		want         AliasStateMode
	}{
		{"released state", "", 3, 0, AliasStateLegacy},
		{"current version", AliasMetaVersion, 3, 3, AliasStateStructured},
		{"current version owning nothing", AliasMetaVersion, 0, 0, AliasStateStructured},
		{"current version missing its bases", AliasMetaVersion, 3, 0, AliasStateUnsupported},
		{"a later version", "v2", 3, 3, AliasStateUnsupported},
		{"a version that merely starts the same", AliasMetaVersion + "x", 3, 3, AliasStateUnsupported},
	} {
		if got := AliasStateModeFor(tc.meta, tc.files, tc.bases); got != tc.want {
			t.Fatalf("%s: mode %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestContextDifferenceDurableRefusesToSkipForAnUnreadableState(t *testing.T) {
	before := map[string]string{}
	after := map[string]string{aliasContextKey(aliasKindPackage, "", "@a/"): "x", legacyAliasAggregateKey: "y"}
	if got := ContextDifferenceDurable(before, after, AliasStateStructured); len(got) != 0 {
		t.Fatalf("a structured state reported alias keys as durable reasons: %v", got)
	}
	if got := ContextDifferenceDurable(before, after, AliasStateLegacy); len(got) != 1 {
		t.Fatalf("a released state did not report the aggregate key alone: %v", got)
	}
	if got := ContextDifferenceDurable(before, after, AliasStateUnsupported); len(got) != 2 {
		t.Fatalf("an unreadable state skipped keys it cannot interpret: %v", got)
	}
}
