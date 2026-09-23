package graphsession

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentRetiredManifestUpdatesNameConsumer(t *testing.T) {
	root := candidateScopeRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "pkgs/second"), 0755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "pkgs/second/package.json", `{"name":"second","dependencies":{"left-pad":"^1.0.0"}}`)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	configScopeRun(t, eng, root, opts, cons)
	if owner := candidateOwner(t, cons, "pkg:npm/left-pad"); owner != "pkgs/second/package.json" {
		t.Fatalf("unexpected candidate owner %q", owner)
	}
	if err := os.Remove(filepath.Join(root, "pkgs/second/package.json")); err != nil {
		t.Fatal(err)
	}
	res, owners, ids := configScopeRun(t, eng, root, opts, cons)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, configScopeEngine(t, root), root))
	requireOwners(t, owners, ids, "pkgs/second/package.json", "src/use.ts")
	if ownsFile(cons, "pkgs/second/package.json") {
		t.Fatal("retired manifest contribution survived")
	}
	forbidOwners(t, owners, ids, "src/alone.ts")
	requireNoWholeDomainFallback(t, res, ids)
}
