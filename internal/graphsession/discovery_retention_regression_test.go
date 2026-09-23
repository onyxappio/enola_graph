package graphsession

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// retentionFixture is discoveryFixture with the session options exposed, for
// the one guard below that has to race a run it expects to fail.
func retentionFixture(t *testing.T, files map[string]string, opts Options) (string, *Resident, *ChangeQueue, *graphstream.MemorySink) {
	t.Helper()
	dir := admissionRepo(t, files)
	sink := &graphstream.MemorySink{}
	opts.StateDir = filepath.Join(dir, ".enola", "resident")
	r, err := OpenSession(context.Background(), admissionEngine(t, dir, graphinput.Options{}), dir, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	q := NewChangeQueue("test", 8)
	q.Start(context.Background())
	return dir, r, q, sink
}

// An ordinary content edit is the case this whole change exists for: the tree's
// packages, gates, aliases and TS roots did not move, and the run should not go
// looking for them again.
//
// Zero builds and at least one reuse, not "no more than before": a run that
// quietly rebuilt would still pass a weaker bound, and this guard has to fail
// if the retention is dropped anywhere between committing one run and offering
// the snapshot to the next. The name checks are asserted non-zero for the
// opposite reason - proving a retained snapshot costs presence, byte and
// membership re-observations, that cost is work this change introduces, and a
// guard that only counted the savings would let it grow unseen.
func TestResidentContentEditReusesRetainedDiscovery(t *testing.T) {
	dir, r, q, sink := retentionFixture(t, discoveryRepoFiles(), Options{})

	initial := residentApply(t, r, q)
	if initial.Work.TSDiscoveries != 1 || initial.Work.TSDiscoveriesReused != 0 {
		t.Fatalf("the first run had nothing to reuse, yet reported %+v", initial.Work)
	}
	residentCold(t, dir, r, sink)

	for i, body := range []string{
		"export function a() { return 2 }\n",
		"export function a() { return 3 }\n",
	} {
		writeFile(t, dir, "src/a.ts", body)
		q.Add("src/a.ts")
		res := residentApply(t, r, q)
		if res.Reconciled {
			t.Fatalf("content edit %d reconciled: %+v", i, res.Work)
		}
		if res.Work.TSDiscoveries != 0 {
			t.Fatalf("content edit %d rebuilt %d discovery snapshots: %+v", i, res.Work.TSDiscoveries, res.Work)
		}
		if res.Work.TSDiscoveriesReused == 0 {
			t.Fatalf("content edit %d reused no retained snapshot: %+v", i, res.Work)
		}
		if res.Work.TSDiscoveryRechecks.Names == 0 || res.Work.TSDiscoveryRechecks.Bytes == 0 || res.Work.TSDiscoveryRechecks.Dirs == 0 {
			t.Fatalf("content edit %d reused a snapshot without re-observing names, bytes and directories: %+v", i, res.Work)
		}
		t.Logf("content edit %d reuse cost: %s", i, res.Work.TSDiscoveryRechecks)
		residentCold(t, dir, r, sink)
	}
}

// Retention is an offer, never an answer. These two are the cases where the
// offer has to be refused: a configuration the snapshot read changed, and a
// configuration the snapshot read as missing appeared. The second never reaches
// the queue at all, so nothing but the snapshot's own re-observation can catch
// it - which is exactly why it is here.
//
// Each case asserts the rebuild and then the graph, because a counter is not
// the property under test: what matters is that the run publishes the graph a
// cold run over the same tree publishes.
func TestRetainedDiscoveryRefusedWhenObservationsMove(t *testing.T) {
	t.Run("configuration the snapshot read changed", func(t *testing.T) {
		dir, r, q, sink := retentionFixture(t, discoveryRepoFiles(), Options{})
		residentApply(t, r, q)

		writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["src/*"]}}}`)
		q.Add("tsconfig.json")
		res := residentApply(t, r, q)
		if !res.Reconciled {
			t.Fatalf("an alias retarget did not reconcile: %+v", res.Work)
		}
		if res.Work.TSDiscoveriesReused != 0 {
			t.Fatalf("a retargeted alias map was answered from a retained snapshot: %+v", res.Work)
		}
		if res.Work.TSDiscoveries != 1 {
			t.Fatalf("the reconciling run built %d discovery snapshots, want 1: %+v", res.Work.TSDiscoveries, res.Work)
		}
		residentCold(t, dir, r, sink)

		// A refusal costs one rebuild, not retention itself. The reconciling
		// run observed the settled tree and committed, so the content edit
		// after it answers from what that run proved. Without this the fence
		// could be satisfied by never retaining anything again.
		writeFile(t, dir, "src/a.ts", "export function a() { return 2 }\n")
		q.Add("src/a.ts")
		after := residentApply(t, r, q)
		if after.Work.TSDiscoveries != 0 || after.Work.TSDiscoveriesReused == 0 {
			t.Fatalf("retention was not re-established after a reconciling run: %+v", after.Work)
		}
		residentCold(t, dir, r, sink)
	})

	t.Run("configuration the snapshot read as missing appeared", func(t *testing.T) {
		dir, r, q, sink := retentionFixture(t, discoveryRepoFiles(), Options{})
		residentApply(t, r, q)

		// Deliberately not queued: the run is told about a source edit and
		// nothing else, so the only thing standing between it and a snapshot
		// that decided without this file is the presence re-observation.
		writeFile(t, dir, "svelte.config.js", "export default { kit: { alias: { '@svelte': 'src' } } }\n")
		writeFile(t, dir, "src/a.ts", "export function a() { return 2 }\n")
		q.Add("src/a.ts")
		res := residentApply(t, r, q)
		if res.Work.TSDiscoveriesReused != 0 {
			t.Fatalf("a snapshot that never saw svelte.config.js was reused after it appeared: %+v", res.Work)
		}
		if res.Work.TSDiscoveries != 1 {
			t.Fatalf("the refusing run built %d discovery snapshots, want 1: %+v", res.Work.TSDiscoveries, res.Work)
		}
		if res.Work.TSDiscoveryRechecks.Names == 0 {
			t.Fatalf("the offer was refused without re-observing anything: %+v", res.Work)
		}

		q.Add("svelte.config.js")
		residentApply(t, r, q)
		residentCold(t, dir, r, sink)
	})
}

// A run that did not commit proved nothing that outlives it. The snapshot it
// extracted under was observed against a tree that then moved underneath it -
// that is why End refused - so carrying it forward would hand the next run an
// observation no accepted run ever made.
//
// The next run therefore has to build its own, and reach the graph a cold run
// reaches over the settled tree.
func TestRetainedDiscoveryDroppedAfterFailedRun(t *testing.T) {
	files := discoveryRepoFiles()
	moved := false
	dir, r, q, sink := retentionFixture(t, files, Options{})

	residentApply(t, r, q)
	residentCold(t, dir, r, sink)

	// Armed only now: the first run parses these files too, and a race during
	// it would test the run-end fence rather than what retention survives.
	r.opts.OnBeforeParse = func(string) {
		if moved {
			return
		}
		moved = true
		writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["vendor/*"]}}}`)
	}

	writeFile(t, dir, "src/a.ts", "export function a() { return 2 }\n")
	q.Add("src/a.ts")
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err == nil {
		t.Fatal("configuration moved under the run and it committed anyway")
	}
	if !moved {
		t.Fatal("the hook never fired, so nothing was raced")
	}
	r.opts.OnBeforeParse = nil

	writeFile(t, dir, "src/a.ts", "export function a() { return 4 }\n")
	q.Add("src/a.ts")
	res := residentApply(t, r, q)
	if res.Work.TSDiscoveriesReused != 0 {
		t.Fatalf("the run after a refusal reused a snapshot the refused run held: %+v", res.Work)
	}
	if res.Work.TSDiscoveries != 1 {
		t.Fatalf("the run after a refusal built %d discovery snapshots, want 1: %+v", res.Work.TSDiscoveries, res.Work)
	}
	residentCold(t, dir, r, sink)
}
