package engine

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitInfo_HookGitDirDoesNotClaimTempCheckout is the pre-push failure:
// git exports GIT_DIR/GIT_WORK_TREE into the hook process, and gitInfo used
// to honour them over `git -C <temp>`. A fixture then inherited this clone's
// remote name (enola_graph) and lost per-repo identity.
func TestGitInfo_HookGitDirDoesNotClaimTempCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	gitDir, err := exec.Command("git", "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		t.Fatalf("absolute-git-dir: %v", err)
	}
	workTree, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("show-toplevel: %v", err)
	}
	t.Setenv("GIT_DIR", strings.TrimSpace(string(gitDir)))
	t.Setenv("GIT_WORK_TREE", strings.TrimSpace(string(workTree)))

	tmp := t.TempDir()
	if gi := gitInfo(tmp, ""); gi != nil {
		t.Fatalf("non-git temp inherited hook git identity: %+v", gi)
	}
	if isGitTopLevel(tmp) {
		t.Fatalf("temp dir %s reported as this clone's top-level under GIT_DIR", tmp)
	}
	if got := repoLabelFor(tmp, "github.com/onyxappio/enola_graph"); got != filepath.Base(tmp) {
		t.Fatalf("repoLabelFor(%s) = %q, want directory name", tmp, got)
	}
}
