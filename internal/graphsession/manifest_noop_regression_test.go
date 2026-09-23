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
// quiet proves nothing here. The first run after the edit is now included -
// proveNonTSNeutrality makes the whole-output comparison from a fenced capture
// before the plan freezes, so the run that first sees the edit declines to
// publish rather than publishing a replacement of what is already there. What
// this test still pins beyond that is the perpetual retrigger: a run that
// proves neutrality has to record the input it was proved against, or every
// later run raises the same need for the same reason.
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
	first := &graphstream.MemorySink{}
	firstRes, err := Run(context.Background(), eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(first.CloneRecords()); n != 0 || firstRes.ParsedFiles != 0 {
		t.Fatalf("the run that first saw the version bump published %d record(s) and parsed %d file(s); a graph-neutral edit must publish nothing", n, firstRes.ParsedFiles)
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
//
// A deletion is deliberately left to publish once. Showing that the stored scan
// digest is the digest of this tree means putting back what moved, and a name
// that is gone cannot be put back from the current inventory - nothing left in
// it says which extractor used to own that path. So the run after a deletion
// takes the conservative branch and the test starts counting from the one after
// it.
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
// on every run after that. Putting the tree back to what the stored scan digest
// described means dropping the newcomer's name rather than restoring a content
// hash it never had, so this addition, unlike the deletion above, publishes
// nothing at all.
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
	first := &graphstream.MemorySink{}
	firstRes, err := Run(context.Background(), eng, root, first, opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(first.CloneRecords()); n != 0 || firstRes.ParsedFiles != 0 {
		t.Fatalf("the run that first saw the added factless manifest published %d record(s) and parsed %d file(s)", n, firstRes.ParsedFiles)
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
