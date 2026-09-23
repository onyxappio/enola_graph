package graphsession

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
)

// Exercise the authoritative contract with a reconstructed engine each time.
// Same-size source changes with restored mtime must not be hidden by a startup
// shortcut, and a stable run must leave the durable checkpoint byte-identical.
func TestIndependentFreshAuthoritativeSameMetadataResolution(t *testing.T) {
	root, _, consumer, opts, _ := replayRepo(t, nil)
	path := filepath.Join(root, "src/a.ts")
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z", "a"} {
		writeFile(t, root, "src/a.ts", "export function "+name+"() { return 1; }\n")
		if err := os.Chtimes(path, original.ModTime(), original.ModTime()); err != nil {
			t.Fatal(err)
		}
		now, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if now.Size() != original.Size() || !now.ModTime().Equal(original.ModTime()) {
			t.Fatal("fixture failed to preserve source size and mtime")
		}
		eng := admissionEngine(t, root, graphinput.Options{})
		changed, _ := admissionRun(t, eng, root, opts, consumer)
		if changed.ParsedFiles == 0 {
			t.Fatal("same-metadata symbol change skipped parsing")
		}
		assertAppliedEqualsCold(t, consumer, coldConsumer(t, eng, root))
		before, err := os.ReadFile(statePath(opts.StateDir))
		if err != nil {
			t.Fatal(err)
		}
		fresh := admissionEngine(t, root, graphinput.Options{})
		noop, sink := admissionRun(t, fresh, root, opts, consumer)
		assertNoPublication(t, noop, sink, changed.TargetGeneration, "fresh engine following same-metadata edit")
		after, err := os.ReadFile(statePath(opts.StateDir))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("stable no-op rewrote durable state")
		}
	}
}
