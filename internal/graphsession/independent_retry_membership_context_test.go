package graphsession

import (
	"context"
	"errors"
	"testing"

	"github.com/enola-labs/enola/internal/graphinput"
	"github.com/enola-labs/enola/internal/graphstream"
)

// A new file can shadow a still-present index module between attempts.
// Retrying must resolve the unchanged importer against the new inventory.
func TestIndependentRetryResolutionPrecedenceRemainsColdEqual(t *testing.T) {
	root := aliasBaseRepo(t, map[string]string{
		"app/tsconfig.json":    `{"compilerOptions":{"baseUrl":".","paths":{"@app/*":["./lib/*"]}}}`,
		"app/src/dep/index.ts": "export function util(){return 1;}\n",
		"app/other/util.ts":    "export function util(){return 2;}\n",
		"app/src/consumer.ts":  "import {util} from './dep'; export function used(){return util();}\n",
		"app/src/trigger.ts":   "export const trigger=1;\n",
	})
	eng := admissionEngine(t, root, graphinput.Options{})
	ctx := context.Background()
	sink := &graphstream.MemorySink{}
	r, err := OpenSession(ctx, eng, root, sink, Options{StateDir: t.TempDir(), AuthoritativeFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err = r.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "app/src/consumer.ts", "import {util} from './dep'; export function used(){return util();} export const added=1;\n")
	writeFile(t, root, "app/src/trigger.ts", "export const trigger=2;\n")
	r.mu.Lock()
	input, reason := r.contentInputs([]string{"app/src/consumer.ts", "app/src/trigger.ts"}, &WorkCounters{})
	r.mu.Unlock()
	if reason != "" || input == nil {
		t.Fatalf("capture: %s", reason)
	}
	writeFile(t, root, "app/src/trigger.ts", "export const trigger=3;\n")
	r.mu.Lock()
	_, _, err = r.transaction(ctx, input, true)
	r.mu.Unlock()
	if !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("expected refusal: %v", err)
	}
	writeFile(t, root, "app/src/dep.ts", "export function util(){return 9;}\n")
	if _, err = r.reconcile(ctx, false); err != nil {
		t.Fatal(err)
	}
	cons := NewConsumer()
	applyRun(t, cons, sink)
	assertAppliedEqualsCold(t, cons, coldConsumer(t, eng, root))
}
