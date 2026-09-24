package graphsession

import (
	"context"
	"github.com/enola-labs/enola/internal/graphstream"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentEmptyPythonDomainRetiresLastOwner(t *testing.T) {
	root := siblingScopeRepo(t)
	if err := os.Remove(filepath.Join(root, "tools/build.py")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "requirements.txt", "# retain Python detection\n")
	writeFile(t, root, "docs/plans/helper.py", "def unique_python_helper():\n    return 1\n")
	eng := siblingScopeEngine(t, root)
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true}
	cons := NewConsumer()
	initial := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, initial, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, initial)
	if len(cons.Owners[ownerKey("docs/plans/helper.py")]) == 0 {
		t.Fatal("fixture has no prior Python contribution")
	}
	if err := os.Remove(filepath.Join(root, "docs/plans/helper.py")); err != nil {
		t.Fatal(err)
	}
	sink := &graphstream.MemorySink{}
	r, err := Run(context.Background(), eng, root, sink, opts)
	if err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, sink)
	owners, ids := beginScope(t, sink)
	requireOwners(t, owners, ids, "docs/plans/helper.py")
	if len(cons.Owners[ownerKey("docs/plans/helper.py")]) != 0 {
		t.Fatal("retired Python owner still contributes")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
	quiet := &graphstream.MemorySink{}
	n, err := Run(context.Background(), eng, root, quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet.CloneRecords()) != 0 || n.ParsedFiles != 0 || n.TargetGeneration != r.TargetGeneration {
		t.Fatalf("retirement followup not quiet: events=%d parsed=%d generation=%d->%d", len(quiet.CloneRecords()), n.ParsedFiles, r.TargetGeneration, n.TargetGeneration)
	}
	writeFile(t, root, "docs/plans/reborn.py", "def reborn_helper():\n    return 2\n")
	added := &graphstream.MemorySink{}
	if _, err := Run(context.Background(), eng, root, added, opts); err != nil {
		t.Fatal(err)
	}
	applyRun(t, cons, added)
	if len(cons.Owners[ownerKey("docs/plans/reborn.py")]) == 0 {
		t.Fatal("empty domain did not notice first new Python source")
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))

}
