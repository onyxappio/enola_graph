package graphsession

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
)

func TestDeleteKeepsUnchangedTransitiveConsumerCached(t *testing.T) {
	root := setupTSRepo(t, map[string]string{
		"src/a.ts": "export function a(){return 1}",
		"src/b.ts": "import {a} from './a'; export function stable(){return 1}",
		"src/c.ts": "import {stable} from './b'; export function use(){return stable()}",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	opts := Options{StateDir: t.TempDir(), AuthoritativeFiles: true, ChangedOwnersOnly: true}
	cons := NewConsumer()
	admissionRun(t, eng, root, opts, cons)
	if err := os.Remove(filepath.Join(root, "src/a.ts")); err != nil {
		t.Fatal(err)
	}
	result, _ := admissionRun(t, eng, root, opts, cons)
	// Only b reads the removed module; c still binds the unchanged stable export.
	if result.ParsedFiles != 1 {
		t.Fatalf("want one direct reader parsed, got %d (%v)", result.ParsedFiles, result.Invalidation.ParsedByReason)
	}
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
