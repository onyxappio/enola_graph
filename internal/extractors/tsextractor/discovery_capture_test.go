package tsextractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A discovery snapshot has to be a function of the capture it is given. It was
// not: NewDiscovery threaded its context only into the walking readers, so
// findTSRoot, the hasPkgDependency gates behind detectVue/detectORMs and the
// rest, collectPackageNames and the alias fallbacks all read the live tree, and
// two snapshots built from identical captured bytes disagreed as soon as the
// tree moved underneath them. The recorded side reads could not see that
// either, which made the reuse fence unsound rather than merely incomplete.
//
// This is the coordinator's independent repro, kept in tree.
func TestDiscoveryUsesCapturedPackageReaders(t *testing.T) {
	root := t.TempDir()
	captured := []byte(`{"name":"captured-package","dependencies":{"vue":"^3","typeorm":"^1"}}`)
	path := filepath.Join(root, "package.json")
	if err := os.WriteFile(path, captured, 0o600); err != nil {
		t.Fatal(err)
	}
	e := New()
	sources := map[string][]byte{"package.json": captured}

	before := e.NewDiscovery(context.Background(), root, sources)
	if before.pkgNames["."] != "captured-package" || !before.vue || !before.typeORM {
		t.Fatal("fixture did not activate the readers this test is about")
	}

	if err := os.WriteFile(path, []byte(`{"name":"live-package"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	after := e.NewDiscovery(context.Background(), root, sources)
	if after.pkgNames["."] != before.pkgNames["."] {
		t.Errorf("captured package name became the live name %q", after.pkgNames["."])
	}
	if after.vue != before.vue || after.typeORM != before.typeORM {
		t.Errorf("captured gates became live gates: vue=%v typeORM=%v", after.vue, after.typeORM)
	}
	if after.tsRoot != before.tsRoot || after.tsRootFound != before.tsRootFound {
		t.Errorf("captured TS root became the live root: %q/%v", after.tsRoot, after.tsRootFound)
	}
}

// Reuse must turn on the bytes, not on the fact that a snapshot exists. A
// caller whose capture contradicts an observation the snapshot already made has
// to rebuild, and a caller whose capture agrees has to be allowed to share -
// otherwise the fence is either unsound or useless.
func TestDiscoveryReuseTurnsOnCapturedBytesNotOnHavingASnapshot(t *testing.T) {
	root := t.TempDir()
	captured := []byte(`{"name":"captured-package","dependencies":{"vue":"^3"}}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), captured, 0o600); err != nil {
		t.Fatal(err)
	}
	e := New()
	disc := e.NewDiscovery(context.Background(), root, map[string][]byte{"package.json": captured})

	same := newFileOverlay(root, map[string][]byte{"package.json": captured})
	if !disc.reusableFor(root, e.inputScope, same) {
		t.Fatal("a capture identical to the one the snapshot was taken over was refused")
	}

	other := newFileOverlay(root, map[string][]byte{"package.json": []byte(`{"name":"other-package"}`)})
	if disc.reusableFor(root, e.inputScope, other) {
		t.Fatal("a capture contradicting the snapshot's own observation was accepted")
	}

	// A caller with no capture at all reads the live tree, which is not what
	// this snapshot observed, so it cannot inherit the captured answers.
	if disc.reusableFor(root, e.inputScope, nil) {
		t.Fatal("an uncaptured caller inherited a snapshot taken over a capture")
	}

	// The reverse direction: a snapshot taken over the live tree cannot be
	// handed to a caller whose capture supplies different bytes for a file the
	// snapshot actually read.
	live := e.NewDiscovery(context.Background(), root, nil)
	if !live.reusableFor(root, e.inputScope, nil) {
		t.Fatal("an uncaptured snapshot was refused to an uncaptured caller")
	}
	if live.reusableFor(root, e.inputScope, other) {
		t.Fatal("a live-read snapshot was reused under a capture that disagrees with it")
	}
	if !live.reusableFor(root, e.inputScope, newFileOverlay(root, map[string][]byte{"package.json": captured})) {
		t.Fatal("a capture agreeing byte for byte with what the snapshot read live was refused")
	}
}

// A file the snapshot never consulted cannot invalidate it, and a file the
// snapshot found missing cannot be supplied by a later capture without a
// rebuild - the snapshot's answers were computed as though it were absent.
func TestDiscoveryReuseIgnoresUnconsultedFilesAndRefusesAppearingOnes(t *testing.T) {
	root := t.TempDir()
	captured := []byte(`{"name":"captured-package"}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), captured, 0o600); err != nil {
		t.Fatal(err)
	}
	e := New()
	disc := e.NewDiscovery(context.Background(), root, map[string][]byte{"package.json": captured})

	withUnrelated := newFileOverlay(root, map[string][]byte{
		"package.json":  captured,
		"src/README.md": []byte("# not a discovery input\n"),
	})
	if !disc.reusableFor(root, e.inputScope, withUnrelated) {
		t.Fatal("a capture carrying a file discovery never read forced a rebuild")
	}

	appearing := newFileOverlay(root, map[string][]byte{
		"package.json":  captured,
		"tsconfig.json": []byte(`{"compilerOptions":{"baseUrl":"."}}`),
	})
	if _, seen := disc.sideReads[absOverlayKey(filepath.Join(root, "tsconfig.json"))]; !seen {
		t.Skip("this fixture's readers did not probe tsconfig.json, so there is nothing to assert")
	}
	if disc.reusableFor(root, e.inputScope, appearing) {
		t.Fatal("a capture supplying a config file the snapshot read as missing was accepted")
	}
}
