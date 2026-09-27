package graphsession

import (
	"os"
	"path/filepath"
	"testing"
)

// A fresh CLI builds one graph input policy resolving its target and, before
// this, an identical second one at the top of its first run. The second can be
// skipped only by proving the same property a cheaper way - that the files the
// policy declared as its inputs have not moved since it read them - so these
// guards are about where the proof comes from, never about a policy being
// assumed current.
//
// The counters are asserted exactly, in both directions. A guard that only
// checked the skip would pass with the proof removed, and a guard that only
// checked the rebuild would pass with the skip never taken.
func TestFreshEngineProvesGraphInputsInsteadOfRebuilding(t *testing.T) {
	dir, r, q, sink := proofFixture(t, discoveryRepoFiles(), Options{FreshEngine: true})

	initial := residentApply(t, r, q)
	if initial.Work.GraphInputRebuildsProven != 1 || initial.Work.GraphInputRebuilds != 0 {
		t.Fatalf("the first run of a freshly constructed engine rebuilt its policy: %+v", initial.Work)
	}
	if initial.Work.PolicyBuilds != 0 {
		t.Fatalf("a run that built no policy still reported PolicyBuilds=%d: %+v", initial.Work.PolicyBuilds, initial.Work)
	}
	residentCold(t, dir, r, sink)

	// The claim covers one run. The engine has now been used, so the window a
	// declared-input recheck would have to cover is no longer bounded and the
	// next non-fast run has to build its own policy again.
	writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["lib/*"]}}}`+"\n")
	q.Add("tsconfig.json")
	second := residentApply(t, r, q)
	if !second.Reconciled {
		t.Fatalf("a tsconfig retarget did not reconcile: %+v", second.Work)
	}
	if second.Work.GraphInputRebuilds != 1 || second.Work.GraphInputRebuildsProven != 0 {
		t.Fatalf("the second run reused the fresh-engine claim: %+v", second.Work)
	}
	residentCold(t, dir, r, sink)
}

// The negative control. Without the caller's claim nothing changes, which is
// what says the skip above is the claim's doing and not something the session
// decided on its own.
func TestWithoutFreshEngineClaimTheRebuildStillHappens(t *testing.T) {
	dir, r, q, sink := proofFixture(t, discoveryRepoFiles(), Options{})

	initial := residentApply(t, r, q)
	if initial.Work.GraphInputRebuilds != 1 || initial.Work.GraphInputRebuildsProven != 0 {
		t.Fatalf("a session with no fresh-engine claim skipped its rebuild: %+v", initial.Work)
	}
	residentCold(t, dir, r, sink)
}

// The claim bounds a window; it does not assert the window was quiet. A policy
// input that moves between construction and the run is exactly the case the
// recheck exists for, and it must rebuild rather than proceed on a policy that
// read the old bytes.
//
// `.gitignore` is used because the policy declares it whether or not it exists,
// so writing one moves a declared digest from "missing" to real without adding
// any source file and without touching the configuration bracket, which would
// have failed the run for a different reason and proved nothing about this one.
func TestMovedDeclaredInputForcesRebuildDespiteFreshEngine(t *testing.T) {
	dir, r, q, sink := proofFixture(t, discoveryRepoFiles(), Options{FreshEngine: true})

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initial := residentApply(t, r, q)
	if initial.Work.GraphInputRebuilds != 1 || initial.Work.GraphInputRebuildsProven != 0 {
		t.Fatalf("a moved declared policy input was accepted as proof: %+v", initial.Work)
	}
	residentCold(t, dir, r, sink)
}
