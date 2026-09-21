package graphsession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/graphstream"
)

func TestConfigRediscoveryRejectsAddedPrunedConfigDuringRun(t *testing.T) {
	dir := setupTSRepo(t, map[string]string{"src/a.ts": "export function a(){return 1}"})
	eng := testEngine(t, dir)
	eng.Config().Ignore = append(eng.Config().Ignore, "private/**")
	state := t.TempDir()
	opts := Options{StateDir: state, OnBeforeParse: func(string) {
		p := filepath.Join(dir, "private", "tsconfig.json")
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"compilerOptions":{"strict":true}}`), 0644); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := Run(context.Background(), eng, dir, &graphstream.MemorySink{}, opts)
	if err == nil || !strings.Contains(err.Error(), "analysis inputs changed") {
		t.Fatalf("new off-inventory config accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state promoted despite changed inputs: %v", err)
	}
}
