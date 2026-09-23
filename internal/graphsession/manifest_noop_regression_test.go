package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

// A semantically neutral manifest edit - a package's own version - changes the
// file's bytes but none of the facts the manifest extractor derives from it.
// The extractor therefore re-runs once, proves its whole output identical and
// short-circuits. Until that short-circuit recorded the input it had just been
// proved against, the file kept the hash it had before the edit, so every later
// run needed the extractor again for the same reason: each one announced a
// Begin, published the manifest owner and advanced a generation on a repository
// nobody had touched. Root reproduced this both in Product (three unchanged
// runs, 21 owners and ~60KB of events each) and with a two-file CLI fixture.
//
// The assertion is deliberately over repeated runs: one unchanged run looking
// quiet proves nothing here. The first run after the edit still republishes,
// and that is a separate, still-open defect rather than accepted behaviour -
// the plan freezes and a Begin is published before extraction can prove the
// output unchanged, so neutrality is detected too late to suppress it. This
// test pins only the perpetual retrigger: the second and third runs must be
// silent. A graph-neutral edit publishing nothing at all needs neutral-output
// detection before Begin, which this change does not provide.
func TestManifestVersionOnlyEditSettlesToATrueNoOp(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}

	// The edit, left in place - the existing coverage restored the original
	// bytes, which is exactly why the stale hash never showed.
	writeRepoFile(t, root, "package.json", `{"name":"app","type":"module","version":"0.7.2"}`)
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}

	_, settled := committedGeneration(t, opts.StateDir)
	for i := range 3 {
		quiet := &graphstream.MemorySink{}
		res, err := Run(context.Background(), eng, root, quiet, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.ParsedFiles != 0 {
			t.Fatalf("unchanged run %d parsed %d file(s)", i+1, res.ParsedFiles)
		}
		if n := len(quiet.CloneRecords()); n != 0 {
			begins, _, _, decErr := DecodeRun(quiet.CloneRecords())
			if decErr != nil {
				t.Fatal(decErr)
			}
			t.Fatalf("unchanged run %d published %d record(s) in %d replacement(s); a version-only manifest edit must settle after one run", i+1, n, len(begins))
		}
		if _, now := committedGeneration(t, opts.StateDir); now != settled {
			t.Fatalf("unchanged run %d advanced the committed state", i+1)
		}
	}
}

// The same defect with the file gone instead of edited: an owned manifest that
// contributed no facts can leave the repository without changing the
// extractor's fingerprint, so the short-circuit is taken again - and a state
// entry for a file that is no longer there keeps reporting a changed input
// forever. Retiring that entry is the other half of recording what was
// observed.
func TestDeletedFactlessManifestSettlesToATrueNoOp(t *testing.T) {
	root := configScopeRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "packages/side"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "packages/side/package.json", `{"name":"side","version":"1.0.0"}`)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "packages/side/package.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}

	_, settled := committedGeneration(t, opts.StateDir)
	for i := range 2 {
		quiet := &graphstream.MemorySink{}
		res, err := Run(context.Background(), eng, root, quiet, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 {
			t.Fatalf("unchanged run %d after a deletion was not a no-op: parsed=%d records=%d", i+1, res.ParsedFiles, len(quiet.CloneRecords()))
		}
		if _, now := committedGeneration(t, opts.StateDir); now != settled {
			t.Fatalf("unchanged run %d after a deletion advanced the committed state", i+1)
		}
	}
}

// The third shape of the same question: a file that arrives. An added manifest
// the extractor owns but derives nothing from leaves the whole-output
// fingerprint unchanged, so the short-circuit is taken on the very run that
// first sees the file - and if the refresh only covered files already in the
// state, the newcomer would keep an empty seen hash and report a changed input
// on every run after that.
func TestAddedFactlessManifestSettlesToATrueNoOp(t *testing.T) {
	root := configScopeRepo(t)
	eng := configScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "packages/late"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "packages/late/package.json", `{"name":"late","version":"1.0.0"}`)
	if _, err := Run(context.Background(), eng, root, &graphstream.MemorySink{}, opts); err != nil {
		t.Fatal(err)
	}

	_, settled := committedGeneration(t, opts.StateDir)
	for i := range 2 {
		quiet := &graphstream.MemorySink{}
		res, err := Run(context.Background(), eng, root, quiet, opts)
		if err != nil {
			t.Fatal(err)
		}
		if res.ParsedFiles != 0 || len(quiet.CloneRecords()) != 0 {
			t.Fatalf("unchanged run %d after an addition was not a no-op: parsed=%d records=%d", i+1, res.ParsedFiles, len(quiet.CloneRecords()))
		}
		if _, now := committedGeneration(t, opts.StateDir); now != settled {
			t.Fatalf("unchanged run %d after an addition advanced the committed state", i+1)
		}
	}
}
