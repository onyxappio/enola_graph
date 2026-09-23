package graphsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// discoveryFixture opens a resident session over a graph-scoped engine. The
// scope is the point: the run only builds and shares a discovery snapshot when
// the engine carries graph inputs, so a fixture built on the bare test engine
// exercises none of this and reports the same counters whether reuse works or
// not.
func discoveryFixture(t *testing.T, files map[string]string) (string, *Resident, *ChangeQueue, *graphstream.MemorySink) {
	t.Helper()
	dir := admissionRepo(t, files)
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(context.Background(), admissionEngine(t, dir, graphinput.Options{}), dir, sink,
		Options{StateDir: filepath.Join(dir, ".enola", "resident")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	q := NewChangeQueue("test", 8)
	q.Start(context.Background())
	return dir, r, q, sink
}

func discoveryRepoFiles() map[string]string {
	return map[string]string{
		"package.json":  `{"name":"root","dependencies":{"vue":"^3"}}`,
		"tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`,
		"src/a.ts":      "export function a() { return 1 }\n",
		"src/b.ts":      "import { a } from '@app/a'\nexport const b = a()\n",
	}
}

// The repository-wide TypeScript discovery readers - the TS root search, the
// framework and ORM gates, the package name, alias-root and export walks - used
// to run once for the projected session context and then again in full for
// every planner extraction. A run may build one snapshot and every later
// consumer has to prove it may share it and then share it.
//
// Exactly one, not at most one: too many means a consumer rebuilt, and zero
// would mean a build stopped counting itself. With the reuse fence forced to
// refuse, the two counts below are 3 rather than 1, which is what makes this a
// guard on the behaviour rather than a restatement of it.
func TestResidentRunBuildsExactlyOneDiscovery(t *testing.T) {
	dir, r, q, _ := discoveryFixture(t, discoveryRepoFiles())

	initial := residentApply(t, r, q)
	if initial.Work.TSDiscoveries != 1 {
		t.Fatalf("initial run built %d discovery snapshots, want 1", initial.Work.TSDiscoveries)
	}

	// A configuration edit reconciles, which runs the whole preamble again -
	// the projected context and the extraction - and is where a consumer that
	// does not share shows up.
	writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["src/*"]}}}`)
	q.Add("tsconfig.json")
	reconciled := residentApply(t, r, q)
	if !reconciled.Reconciled {
		t.Fatalf("an alias retarget did not reconcile: %+v", reconciled.Work)
	}
	if reconciled.Work.TSDiscoveries != 1 {
		t.Fatalf("a reconciling run built %d discovery snapshots, want 1", reconciled.Work.TSDiscoveries)
	}

	// Nothing retains a snapshot past its run: the next run observes the tree
	// for itself rather than inheriting an observation it did not make.
	writeFile(t, dir, "src/b.ts", "import { a } from '@lib/a'\nexport const b = a() + 1\n")
	q.Add("src/b.ts")
	if next := residentApply(t, r, q); next.Work.TSDiscoveries > 1 {
		t.Fatalf("a later run built %d discovery snapshots", next.Work.TSDiscoveries)
	}
}

// Sharing a snapshot must not make a configuration change invisible. The alias
// map decides whether src/b.ts resolves to src/a.ts at all, so a stale snapshot
// keeps publishing an edge the tree no longer declares - and that is a graph
// difference, not a counter difference.
func TestDiscoveryReuseStillObservesConfigChange(t *testing.T) {
	dir, r, q, sink := discoveryFixture(t, discoveryRepoFiles())
	residentApply(t, r, q)
	residentCold(t, dir, r, sink)

	writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["src/*"]}}}`)
	q.Add("tsconfig.json")
	if res := residentApply(t, r, q); !res.Reconciled {
		t.Fatalf("an alias retarget did not reconcile: %+v", res.Work)
	}
	residentCold(t, dir, r, sink)

	writeFile(t, dir, "package.json", `{"name":"root"}`)
	q.Add("package.json")
	residentApply(t, r, q)
	residentCold(t, dir, r, sink)

	writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["src/*"]}}}`)
	q.Add("tsconfig.json")
	residentApply(t, r, q)
	residentCold(t, dir, r, sink)
}

// completedState reads what a state directory has actually completed, and
// deliberately not what it merely attempted. The payload and ack journals are
// append-only and legitimately grow under a run that streamed before End
// refused it, and commit.json is a manifest over them; hashing those too would
// be an oracle that fails for a correct rollback. What has to stand still is
// the completed state and the record that a transaction was accepted.
func completedState(t *testing.T, stateDir string) string {
	t.Helper()
	sum := sha256.New()
	for _, name := range []string{"state.json", "fences.jsonl", "tombstones.jsonl"} {
		b, err := os.ReadFile(filepath.Join(stateDir, name))
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(sum, "%s:absent:", name)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(sum, "%s:%d:", name, len(b))
		sum.Write(b)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// The reuse fence compares captured bytes, so it cannot see a discovery read
// the capture never carried: those were answered by the live tree, and the
// fence deliberately does not re-read them. That gap is closed at the other end
// of the run rather than inside the snapshot - End re-observes the extractor
// context and the analysis inputs and refuses to publish if either moved.
//
// This pins that claim to runs that actually fail, because reader-level
// reasoning about reusableFor does not establish it. Only configuration moves
// here and the set of source files is identical throughout: adding a source
// file would trip the inventory fence on its own and prove nothing about
// discovery. Both directions the snapshot cannot re-observe are covered - a
// config it read being retargeted, and a config it read as missing appearing -
// and in each case the run must not commit, must leave the previous state
// byte-identical, and must reach the correct graph on the retry.
func TestUncapturedDiscoveryConfigMovingMidRunIsRefusedAtEnd(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		body string
	}{
		{
			name: "config the snapshot read is retargeted",
			rel:  "tsconfig.json",
			body: `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["vendor/*"]}}}`,
		},
		{
			name: "config the snapshot read as missing appears",
			rel:  "src/tsconfig.json",
			body: `{"compilerOptions":{"baseUrl":"..","paths":{"@app/*":["vendor/*"]}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := admissionRepo(t, discoveryRepoFiles())
			eng := admissionEngine(t, dir, graphinput.Options{})
			state := t.TempDir()
			live := &graphstream.MemorySink{}
			opts := Options{StateDir: state, AuthoritativeFiles: true}
			if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
				t.Fatal(err)
			}
			settled := completedState(t, state)
			accepted := NewConsumer()
			applyRun(t, accepted, live)
			published := accepted.Canonical()

			// A content-only edit, so the run has work to do without the file
			// set moving. The configuration then moves underneath it, after
			// the snapshot has already answered for it.
			writeFile(t, dir, "src/a.ts", "export function a() { return 2 }\n")
			moved := false
			racing := opts
			racing.OnBeforeParse = func(string) {
				if moved {
					return
				}
				moved = true
				writeFile(t, dir, tc.rel, tc.body)
			}
			_, err := Run(context.Background(), eng, dir, live, racing)
			if err == nil {
				t.Fatal("configuration moved under the run and End published anyway")
			}
			if !errors.Is(err, ErrInputsChanged) {
				t.Fatalf("End refused the run, but not as an input change: %v", err)
			}
			if !moved {
				t.Fatal("the hook never fired, so nothing was raced")
			}
			t.Logf("%s: fence that fired: %v", tc.name, err)

			if got := completedState(t, state); got != settled {
				t.Fatal("a refused run mutated the completed state")
			}
			// Whatever the refused run streamed before End stopped it, a
			// consumer replaying the whole stream must still be holding the
			// graph the last accepted run left it.
			refused := NewConsumer()
			applyRun(t, refused, live)
			if got := refused.Canonical(); got != published {
				t.Fatalf("a refused run changed what a consumer holds\n after=%s\n before=%s", got, published)
			}

			// The retry sees the settled tree and has to reach the graph a
			// fresh cold run reaches over the same tree - including the
			// configuration that appeared.
			if _, err := Run(context.Background(), eng, dir, live, opts); err != nil {
				t.Fatalf("the retry over the settled tree failed: %v", err)
			}
			coldSink := &graphstream.MemorySink{}
			if _, err := Run(context.Background(), eng, dir, coldSink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true}); err != nil {
				t.Fatal(err)
			}
			retried, cold := NewConsumer(), NewConsumer()
			applyRun(t, retried, live)
			applyRun(t, cold, coldSink)
			assertAppliedEqualsCold(t, retried, cold)
		})
	}
}
