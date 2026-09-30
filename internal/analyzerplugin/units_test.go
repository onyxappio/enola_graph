package analyzerplugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/facts"
)

func TestTopologicalPlanOrdersProducersAndRejectsInvalidGraphs(t *testing.T) {
	units := []UnitDecl{
		{ID: "file:z.ts", Kind: "file", Consumes: []string{"model:m"}},
		{ID: "model:m", Kind: "model"},
		{ID: "file:a.ts", Kind: "file", Consumes: []string{"model:m"}},
	}
	got, err := TopologicalPlan(units)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "model:m" || got[1].ID != "file:a.ts" || got[2].ID != "file:z.ts" {
		t.Fatalf("unexpected topological order: %#v", got)
	}
	for _, tc := range []struct {
		name  string
		units []UnitDecl
	}{
		{"cycle", []UnitDecl{{ID: "a", Kind: "model", Consumes: []string{"b"}}, {ID: "b", Kind: "model", Consumes: []string{"a"}}}},
		{"missing", []UnitDecl{{ID: "a", Kind: "file", Consumes: []string{"missing"}}}},
		{"duplicate", []UnitDecl{{ID: "a", Kind: "file"}, {ID: "a", Kind: "file"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePlan(tc.units); err == nil {
				t.Fatal("expected invalid plan error")
			}
		})
	}
}

func TestValidateResultChecksVocabularyClaimAndOwnerDomain(t *testing.T) {
	p := Loaded{Manifest: Manifest{Name: "toy", Claims: Claims{Machines: []string{"demo"}}, OwnerDomain: []string{"src/**"}}}
	u := UnitDecl{ID: "model:demo", Kind: "model"}
	valid := UnitResult{Unit: u.ID, Owners: map[string]OwnerResult{"src/machine.ts": {Nodes: []Node{
		{Kind: facts.KindFSMMachine, Name: "demo"},
		{Kind: facts.KindFSMState, Name: "demo/state:idle"},
		{Kind: facts.KindFSMEvent, Name: "demo/event:reset"},
		{Kind: facts.KindFSMTransition, Name: "demo/transition:idle-reset"},
		{Kind: facts.KindFSMCommand, Name: "demo/command:refresh"},
	}}}}
	if err := ValidateResult(p, u, valid); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	bad := valid
	bad.Owners = map[string]OwnerResult{"outside/machine.ts": {Nodes: []Node{{Kind: facts.KindFSMMachine, Name: "demo"}}}}
	if err := ValidateResult(p, u, bad); err == nil {
		t.Fatal("out-of-domain owner accepted")
	}
	bad = valid
	bad.Owners = map[string]OwnerResult{"src/machine.ts": {Nodes: []Node{{Kind: facts.KindFSMMachine, Name: "other"}}}}
	if err := ValidateResult(p, u, bad); err == nil {
		t.Fatal("unclaimed machine accepted")
	}
	bad = valid
	bad.Owners = map[string]OwnerResult{"src/machine.ts": {Nodes: []Node{{Kind: "symbol", Name: "forged"}}}}
	if err := ValidateResult(p, u, bad); err == nil {
		t.Fatal("unregistered vocabulary accepted")
	}
}

func TestRepositoryGlobMatchesRecursiveZeroDepthAndBraceForms(t *testing.T) {
	for _, path := range []string{"src/file.ts", "src/nested/file.ts"} {
		if !MatchRepositoryGlob("src/**/file.ts", path) {
			t.Fatalf("recursive glob missed %q", path)
		}
	}
	if !MatchRepositoryGlob("src/{a,b}.ts", "src/b.ts") {
		t.Fatal("brace alternative did not match")
	}
	if MatchRepositoryGlob("src/**/file.ts", "outside/file.ts") {
		t.Fatal("recursive glob matched outside its prefix")
	}
	if err := ValidateRepositoryGlob("../outside/*.ts"); err == nil {
		t.Fatal("repository-escaping glob was accepted")
	}
}

func TestCanonicalizeResultIgnoresNodeAndRelationOrder(t *testing.T) {
	a := UnitResult{Unit: "file:a", Owners: map[string]OwnerResult{"src/a.ts": {Nodes: []Node{
		{Kind: facts.KindFSMState, Name: "demo/state:b", Relations: []Relation{{Kind: "to_state", Target: "demo/state:z"}, {Kind: "to_state", Target: "demo/state:a"}}},
		{Kind: facts.KindFSMState, Name: "demo/state:a"},
	}}}}
	b := UnitResult{Unit: "file:a", Owners: map[string]OwnerResult{"src/a.ts": {Nodes: []Node{
		{Kind: facts.KindFSMState, Name: "demo/state:a"},
		{Kind: facts.KindFSMState, Name: "demo/state:b", Relations: []Relation{{Kind: "to_state", Target: "demo/state:a"}, {Kind: "to_state", Target: "demo/state:z"}}},
	}}}}
	a = CanonicalizeResult(a)
	b = CanonicalizeResult(b)
	aDigest, err := CanonicalDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	bDigest, err := CanonicalDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if aDigest != bDigest {
		t.Fatalf("array ordering changed canonical digest: %s != %s", aDigest, bDigest)
	}
}

func TestValidateResultRequiresDistinctOccurrencesForDuplicateIdentities(t *testing.T) {
	p := Loaded{Manifest: Manifest{Name: "toy", Claims: Claims{Machines: []string{"demo"}}, OwnerDomain: []string{"src/**"}}}
	u := UnitDecl{ID: "file:a", Kind: "file"}
	base := Node{Kind: facts.KindFSMState, Name: "demo/state:idle"}
	result := UnitResult{Unit: u.ID, Owners: map[string]OwnerResult{"src/a.ts": {Nodes: []Node{
		base,
		{Kind: base.Kind, Name: base.Name, Props: map[string]any{"occurrence": "second"}},
	}}}}
	if err := ValidateResult(p, u, result); err == nil {
		t.Fatal("duplicate identity accepted when only one node has an occurrence")
	}
	result.Owners["src/a.ts"] = OwnerResult{Nodes: []Node{
		{Kind: base.Kind, Name: base.Name, Props: map[string]any{"occurrence": "first"}},
		{Kind: base.Kind, Name: base.Name, Props: map[string]any{"occurrence": "second"}},
	}}
	if err := ValidateResult(p, u, result); err != nil {
		t.Fatalf("distinct occurrences rejected: %v", err)
	}
}

func TestRuntimeDigestTracksExecutableBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho runtime-one\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := RuntimeDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho runtime-two\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := RuntimeDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("runtime byte change did not alter its fingerprint")
	}
}

func TestCachedRuntimeDigestReusesUnchangedMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho runtime-one\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := RuntimeCache{}
	first, entry, err := CachedRuntimeDigest(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Digest == "" || cache[entry.Path].Digest != first {
		t.Fatalf("cache was not populated: %#v", entry)
	}
	second, entry2, err := CachedRuntimeDigest(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if second != first || entry2.Digest != first {
		t.Fatalf("unchanged metadata rehashed unexpectedly: %s vs %s", first, second)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho runtime-two\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	third, _, err := CachedRuntimeDigest(path, cache)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("metadata/content change did not refresh runtime digest")
	}
}

func TestRuntimeCacheEntryChangeTimeInvalidatesDigest(t *testing.T) {
	// Design §8.2: same-size replacement with preserved last-write time must not
	// reuse a cached digest. CtimeNs carries Windows ChangeTime / Unix ctime.
	base := RuntimeCacheEntry{
		Path: "/node", Dev: 1, Inode: 2, Size: 64, MtimeNs: 100, CtimeNs: 200, Digest: "old",
	}
	same := base
	if !base.matches(same) {
		t.Fatal("identical identity must match")
	}
	changed := base
	changed.CtimeNs = 201
	if base.matches(changed) {
		t.Fatal("change-time bump with identical size/mtime must invalidate cache identity")
	}
	cache := RuntimeCache{base.Path: base}
	meta := changed
	meta.Digest = ""
	if prev, ok := cache[base.Path]; !ok || prev.matches(meta) {
		t.Fatal("cached runtime entry must miss after change-time-only rewrite")
	}
}

func TestLoadStrictManifestAndStableIdentity(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "tools", "toy")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `api: enola.plugin/v1
name: toy
runtime:
  kind: node
  version: 22.1.0
  entry: dist/plugin.mjs
identity_files: [dist/plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [demo]
owner_domain: [src/**]
`
	if err := os.MkdirAll(filepath.Join(pluginDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "dist", "plugin.mjs"), []byte("console.log('toy')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, []Config{{Path: "tools/toy", Config: map[string]any{"enabled": true}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Identity == "" || loaded[0].RuntimeDigest == "" {
		t.Fatalf("incomplete loaded plugin: %#v", loaded)
	}
	if !Trusted([]string{"toy"}, "toy") || Trusted(nil, "toy") {
		t.Fatal("trust allow-list mismatch")
	}
	if _, err := Load(root, []Config{{Path: "tools/missing"}}, nil); err == nil {
		t.Fatal("configured missing plugin was accepted")
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockManifest := strings.Replace(manifest, "identity_files: [dist/plugin.mjs]", "identity_files: [dist/plugin.mjs, pnpm-lock.yaml]", 1)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(lockManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, []Config{{Path: "tools/toy"}}, nil); err == nil || !strings.Contains(err.Error(), "dependency lockfile") {
		t.Fatalf("lockfile identity input error = %v", err)
	}
}

func TestCanonicalizeResultOrdersAnchorsByPropsAndRelations(t *testing.T) {
	a := UnitResult{Unit: "file:a", Owners: map[string]OwnerResult{"src/a.ts": {Anchors: []Anchor{
		{Symbol: "caller", Line: 1, Props: map[string]any{"tag": "b"}, Relations: []Relation{{Kind: "fsm_dispatches", Target: "demo/event:go"}}},
		{Symbol: "caller", Line: 1, Props: map[string]any{"tag": "a"}, Relations: []Relation{{Kind: "fsm_dispatches", Target: "demo/event:go"}}},
	}}}}
	b := UnitResult{Unit: "file:a", Owners: map[string]OwnerResult{"src/a.ts": {Anchors: []Anchor{
		{Symbol: "caller", Line: 1, Props: map[string]any{"tag": "a"}, Relations: []Relation{{Kind: "fsm_dispatches", Target: "demo/event:go"}}},
		{Symbol: "caller", Line: 1, Props: map[string]any{"tag": "b"}, Relations: []Relation{{Kind: "fsm_dispatches", Target: "demo/event:go"}}},
	}}}}
	a = CanonicalizeResult(a)
	b = CanonicalizeResult(b)
	left, err := CanonicalDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("anchor prop ordering changed digest: %s != %s", left, right)
	}
	if a.Owners["src/a.ts"].Anchors[0].Props["tag"] != "a" {
		t.Fatalf("anchors were not totally ordered by props: %#v", a.Owners["src/a.ts"].Anchors)
	}
}

func TestUnitDeclsEqualCoversKindParamsConsumes(t *testing.T) {
	base := UnitDecl{ID: "u", Kind: "file", Params: map[string]any{"file": "a.ts"}, Consumes: []string{"model:x"}}
	if !UnitDeclsEqual(base, UnitDecl{ID: "u", Kind: "file", Params: map[string]any{"file": "a.ts"}, Consumes: []string{"model:x"}}) {
		t.Fatal("equal decls reported different")
	}
	if UnitDeclsEqual(base, UnitDecl{ID: "u", Kind: "model", Params: map[string]any{"file": "a.ts"}, Consumes: []string{"model:x"}}) {
		t.Fatal("kind change compared equal")
	}
	if UnitDeclsEqual(base, UnitDecl{ID: "u", Kind: "file", Params: map[string]any{"file": "b.ts"}, Consumes: []string{"model:x"}}) {
		t.Fatal("params change compared equal")
	}
	if UnitDeclsEqual(base, UnitDecl{ID: "u", Kind: "file", Params: map[string]any{"file": "a.ts"}, Consumes: []string{"model:y"}}) {
		t.Fatal("consumes change compared equal")
	}
}

func TestCachedRuntimeDigestIncludesReadableLinkedLibraries(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	cache := RuntimeCache{}
	digest, entry, err := CachedRuntimeDigest(node, cache)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		t.Fatal("empty runtime digest")
	}
	switch runtime.GOOS {
	case "darwin", "linux":
		if len(entry.Linked) == 0 {
			t.Fatalf("system node on %s produced no linked-library digests", runtime.GOOS)
		}
	default:
		if len(entry.Linked) != 0 {
			t.Fatalf("unexpected linked libraries on %s: %#v", runtime.GOOS, entry.Linked)
		}
	}
	again, entry2, err := CachedRuntimeDigest(node, cache)
	if err != nil {
		t.Fatal(err)
	}
	if again != digest || entry2.Digest != digest {
		t.Fatal("unchanged system node rehashed despite valid linked-library metadata cache")
	}

	script := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptDigest, scriptEntry, err := CachedRuntimeDigest(script, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(scriptEntry.Linked) != 0 {
		t.Fatalf("non-object script reported linked libraries: %#v", scriptEntry.Linked)
	}
	if scriptDigest == "" {
		t.Fatal("script runtime digest empty")
	}
}

func TestCombineRuntimeDigestTracksLinkedLibraryContentAndUnresolvedSet(t *testing.T) {
	base := combineRuntimeDigest("exe", []RuntimeLinkedLib{{Path: "/opt/lib/a.dylib", Digest: "aaa"}}, nil)
	changedContent := combineRuntimeDigest("exe", []RuntimeLinkedLib{{Path: "/opt/lib/a.dylib", Digest: "bbb"}}, nil)
	if base == changedContent {
		t.Fatal("linked-library content change did not invalidate runtime digest")
	}
	samePathNewUUID := combineRuntimeDigest("exe", []RuntimeLinkedLib{{Path: "/usr/lib/libSystem.B.dylib", Digest: sharedCacheDigestPrefix + "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"}}, nil)
	samePathOldUUID := combineRuntimeDigest("exe", []RuntimeLinkedLib{{Path: "/usr/lib/libSystem.B.dylib", Digest: sharedCacheDigestPrefix + "11111111-2222-3333-4444-555555555555"}}, nil)
	if samePathNewUUID == samePathOldUUID {
		t.Fatal("same-path shared-cache UUID change did not invalidate runtime digest")
	}
}

func TestParseDyldInfoUUIDDeterministic(t *testing.T) {
	fixture := `/usr/lib/libSystem.B.dylib [arm64e]:
    -platform:
        platform     minOS      sdk
 zippered(macOS/Catalyst)     26.3      26.3
    -uuid:
        6ae3a2bd-839c-3dea-854c-1c4a10567c26
    -segments:
        unslid-addr    segment
`
	got := parseDyldInfoUUID(fixture)
	want := "6AE3A2BD-839C-3DEA-854C-1C4A10567C26"
	if got != want {
		t.Fatalf("parseDyldInfoUUID = %q, want %q", got, want)
	}
	if parseDyldInfoUUID("no uuid here\n") != "" {
		t.Fatal("missing UUID was accepted")
	}
}

func TestSharedCacheLinkedStillValidSamePathUUIDChange(t *testing.T) {
	path := "/virtual/libSystem.B.dylib"
	old := sharedCacheDigestPrefix + "11111111-2222-3333-4444-555555555555"
	cur := sharedCacheDigestPrefix + "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"
	prev := sharedCacheDigestFn
	t.Cleanup(func() { sharedCacheDigestFn = prev })
	sharedCacheDigestFn = func(p string) (string, error) {
		if p != path {
			t.Fatalf("unexpected path %q", p)
		}
		return cur, nil
	}
	entry := RuntimeCacheEntry{Linked: []RuntimeLinkedLib{{Path: path, Digest: old}}}
	if entry.linkedStillValid() {
		t.Fatal("same-path UUID change was accepted as still valid")
	}
	entry.Linked[0].Digest = cur
	if !entry.linkedStillValid() {
		t.Fatal("matching UUID digest was treated as stale")
	}
	sharedCacheDigestFn = func(string) (string, error) {
		return "", fmt.Errorf("dyld_info unavailable")
	}
	if entry.linkedStillValid() {
		t.Fatal("fail-closed shared-cache probe error was accepted as still valid")
	}
}

func TestSharedCacheLibraryDigestUsesDyldUUID(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("dyld shared-cache UUID probe is darwin-only")
	}
	path := "/usr/lib/libSystem.B.dylib"
	digest, err := sharedCacheLibraryDigest(path)
	if err != nil {
		// Prefer live probe when dyld can resolve the load path, whether or not
		// the path has an on-disk inode. Fail closed only when dyld_info fails.
		t.Fatalf("sharedCacheLibraryDigest(%s): %v", path, err)
	}
	if !strings.HasPrefix(digest, sharedCacheDigestPrefix) || len(digest) <= len(sharedCacheDigestPrefix) {
		t.Fatalf("shared-cache digest = %q", digest)
	}
	again, err := sharedCacheLibraryDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if again != digest {
		t.Fatalf("dyld UUID digest unstable: %s vs %s", digest, again)
	}
	entry := RuntimeCacheEntry{Linked: []RuntimeLinkedLib{{Path: path, Digest: digest}}}
	if !entry.linkedStillValid() {
		t.Fatal("unchanged shared-cache UUID was treated as stale")
	}
	entry.Linked[0].Digest = sharedCacheDigestPrefix + "00000000-0000-0000-0000-000000000000"
	if entry.linkedStillValid() {
		t.Fatal("same-path UUID change was accepted as still valid")
	}
}

func TestFingerprintLinkedLibrariesFailsClosedOnUnreadableRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission probe")
	}
	dir := t.TempDir()
	lib := filepath.Join(dir, "libfake.dylib")
	if err := os.WriteFile(lib, []byte("dylib-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lib, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(lib, 0o644); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.ReadFile(lib); err == nil {
		t.Skip("this user can read a mode-0 file; fail-closed probe cannot be proven here")
	}
	_, _, err := fingerprintLinkedLibrariesFromPaths([]string{lib})
	if err == nil {
		t.Fatal("unreadable regular linked library was accepted")
	}
}

func TestClonePluginRecordDeepCopy(t *testing.T) {
	original := PluginRecord{
		Identity: "id",
		Plan:     []UnitDecl{{ID: "a", Kind: "file", Consumes: []string{"b"}}},
		Units: map[string]UnitRecord{
			"a": {Decl: UnitDecl{ID: "a", Kind: "file"}, Owners: map[string]OwnerResult{"a.ts": {Nodes: []Node{{Kind: "fsm_state", Name: "x"}}}}},
		},
	}
	cloned := ClonePluginRecord(original)
	cloned.Units["a"] = UnitRecord{OutputDigest: "mutated"}
	cloned.Plan[0].Consumes[0] = "mutated"
	if original.Units["a"].OutputDigest == "mutated" {
		t.Fatal("unit map was shared with clone")
	}
	if original.Plan[0].Consumes[0] == "mutated" {
		t.Fatal("plan slice was shared with clone")
	}
}

func TestStartExecutesLoadedEntryBytesNotRereadPath(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "tools", "toy")
	if err := os.MkdirAll(filepath.Join(pluginDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: toy
runtime:
  kind: node
  version: %s
  entry: dist/plugin.mjs
identity_files: [dist/plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [toy]
owner_domain: [src/**]
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	good := `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.op === "hello") {
    process.stdout.write(JSON.stringify({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node }) + "\n");
  } else if (message.op === "shutdown") {
    process.stdout.write(JSON.stringify({ id: message.id, op: "shutdown_ack" }) + "\n");
    rl.close();
  }
});
`
	entry := filepath.Join(pluginDir, "dist", "plugin.mjs")
	if err := os.WriteFile(entry, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, []Config{{Path: "tools/toy"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || len(loaded[0].EntryBytes) == 0 || loaded[0].EntryDigest == "" {
		t.Fatalf("load missing entry binding: %#v", loaded)
	}
	if err := os.WriteFile(entry, []byte("throw new Error('replaced after load')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), loaded[0], nil)
	if err != nil {
		t.Fatalf("Start after path replacement should use loaded bytes: %v", err)
	}
	defer client.Abort()
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Close(closeCtx); err != nil {
		t.Fatal(err)
	}

	replaced := loaded[0]
	replaced.EntryBytes = []byte("throw new Error('tampered staged bytes')\n")
	if _, err := Start(context.Background(), replaced, nil); err == nil {
		t.Fatal("tampered EntryBytes with mismatched EntryDigest were accepted")
	}
}

func TestIdentityDigestUsesCapturedBytesNotRereadPaths(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "tools", "toy")
	if err := os.MkdirAll(filepath.Join(pluginDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: toy
runtime:
  kind: node
  version: %s
  entry: dist/plugin.mjs
identity_files: [dist/plugin.mjs, dist/extra.txt]
vocabularies: [enola.fsm@1]
claims:
  machines: [toy]
owner_domain: [src/**]
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pluginDir, "dist", "plugin.mjs")
	extra := filepath.Join(pluginDir, "dist", "extra.txt")
	if err := os.WriteFile(entry, []byte("export const ready = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, []Config{{Path: "tools/toy"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := loaded[0].Identity
	capturedEntry := append([]byte(nil), loaded[0].EntryBytes...)
	if err := os.WriteFile(extra, []byte("omega"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("throw new Error('replaced')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if string(loaded[0].EntryBytes) != string(capturedEntry) {
		t.Fatal("captured EntryBytes changed after path replacement")
	}
	if loaded[0].Identity != first {
		t.Fatal("loaded identity mutated after path replacement")
	}
	// A reread-based identityDigest would now hash the replaced files. Reload
	// must produce a different identity, proving Load bound the original bytes.
	reloaded, err := Load(root, []Config{{Path: "tools/toy"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded[0].Identity == first {
		t.Fatal("reload after path replacement kept the old identity; race window would be invisible")
	}
	if string(reloaded[0].EntryBytes) == string(capturedEntry) {
		t.Fatal("reloaded EntryBytes unexpectedly matched the pre-replacement snapshot")
	}
}

func TestClientEnforcesUnitAndRunDeadlines(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "tools", "slow")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	writeSlowPlugin := func(t *testing.T, unitDelayMS, runBudgetMS int) Loaded {
		t.Helper()
		manifest := fmt.Sprintf(`api: enola.plugin/v1
name: slow
runtime:
  kind: node
  version: %s
  entry: plugin.mjs
identity_files: [plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [slow]
owner_domain: [src/**]
limits:
  hello_timeout_ms: 2000
  unit_timeout_ms: %d
  run_timeout_ms: %d
`, version, unitDelayMS, runBudgetMS)
		if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		script := `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const send = (value) => process.stdout.write(JSON.stringify(value) + "\n");
rl.on("line", async (line) => {
  const message = JSON.parse(line);
  if (message.op === "hello") {
    send({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node });
  } else if (message.op === "plan") {
    send({ id: message.id, op: "plan_result", units: [{ id: "file:src/a.ts", kind: "file" }] });
  } else if (message.op === "run") {
    await sleep(500);
    for (const unit of message.units) {
      send({ id: message.id, op: "unit_result", unit: unit.id, owners: {}, summary: {} });
    }
    send({ id: message.id, op: "run_done" });
  } else if (message.op === "shutdown") {
    send({ id: message.id, op: "shutdown_ack" });
    rl.close();
  }
});
`
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.mjs"), []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(root, []Config{{Path: "tools/slow"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return loaded[0]
	}

	t.Run("unit timeout", func(t *testing.T) {
		p := writeSlowPlugin(t, 100, 5000)
		client, err := Start(context.Background(), p, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Abort()
		_, err = client.Run(context.Background(), []UnitDecl{{ID: "file:src/a.ts", Kind: "file"}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected unit timeout, got %v", err)
		}
	})

	t.Run("run deadline", func(t *testing.T) {
		// Allow hello to finish, then exhaust the remaining run budget before Run.
		p := writeSlowPlugin(t, 5000, 1000)
		client, err := Start(context.Background(), p, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Abort()
		time.Sleep(850 * time.Millisecond)
		_, err = client.Run(context.Background(), []UnitDecl{{ID: "file:src/a.ts", Kind: "file"}}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected run deadline failure, got %v", err)
		}
	})
}

func TestHostGrammarIdentityIsPinnedAndDeterministic(t *testing.T) {
	first, err := HostGrammar()
	if err != nil {
		t.Fatal(err)
	}
	if first.Label == "" || first.Digest == "" || len(first.Digest) < 32 {
		t.Fatalf("incomplete grammar identity: %+v", first)
	}
	if !strings.Contains(first.Label, "tree-sitter-typescript@") {
		t.Fatalf("grammar label = %q", first.Label)
	}
	second, err := HostGrammar()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("grammar identity unstable: %+v vs %+v", first, second)
	}
	hello, err := GrammarHelloValue()
	if err != nil {
		t.Fatal(err)
	}
	if hello != first.Label+"#"+first.Digest {
		t.Fatalf("hello grammar = %q", hello)
	}
	if strings.Contains(hello, "unbundled") {
		t.Fatal("hello still advertises unbundled placeholder")
	}
}

func TestGrammarDigestInPluginIdentity(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "tools", "toy")
	if err := os.MkdirAll(filepath.Join(pluginDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: toy
runtime:
  kind: node
  version: %s
  entry: dist/plugin.mjs
identity_files: [dist/plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [toy]
owner_domain: [src/**]
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	entryBody := []byte("export const ready = true;\n")
	if err := os.WriteFile(filepath.Join(pluginDir, "dist", "plugin.mjs"), entryBody, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, []Config{{Path: "tools/toy"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := HostGrammar()
	if err != nil {
		t.Fatal(err)
	}
	fileBytes := map[string][]byte{}
	for _, path := range loaded[0].IdentityFiles {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fileBytes[path] = b
	}
	original := loaded[0].Identity
	g.Digest = strings.Repeat("ab", 32)
	recomputed, err := identityDigestWithGrammar(loaded[0].Manifest, loaded[0].Config.Config, fileBytes, loaded[0].RuntimeDigest, g)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed == original {
		t.Fatal("identity did not change when grammar digest changed")
	}
}

func TestHelloNegotiatesPinnedGrammarDigest(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "tools", "toy")
	if err := os.MkdirAll(filepath.Join(pluginDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node required")
	}
	versionBytes, err := exec.Command(node, "-p", "process.versions.node").Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(versionBytes))
	manifest := fmt.Sprintf(`api: enola.plugin/v1
name: toy
runtime:
  kind: node
  version: %s
  entry: dist/plugin.mjs
identity_files: [dist/plugin.mjs]
vocabularies: [enola.fsm@1]
claims:
  machines: [toy]
owner_domain: [src/**]
`, version)
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(t.TempDir(), "hello-grammar.txt")
	script := fmt.Sprintf(`import { createInterface } from "node:readline";
import { writeFileSync } from "node:fs";
const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.op === "hello") {
    writeFileSync(%q, String(message.grammar ?? ""));
    process.stdout.write(JSON.stringify({ id: message.id, op: "hello_ack", api: message.host_api, node: process.versions.node }) + "\n");
  } else if (message.op === "shutdown") {
    process.stdout.write(JSON.stringify({ id: message.id, op: "shutdown_ack" }) + "\n");
    rl.close();
  }
});
`, capture)
	if err := os.WriteFile(filepath.Join(pluginDir, "dist", "plugin.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, []Config{{Path: "tools/toy"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := GrammarHelloValue()
	if err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), loaded[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Abort()
	closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("hello grammar = %q, want pinned %q", got, want)
	}
	if strings.Contains(string(got), "unbundled") || !strings.Contains(string(got), "#") {
		t.Fatalf("hello grammar missing pinned digest: %q", got)
	}
}
