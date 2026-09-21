package graphsession

import (
	"context"
	"errors"
	"github.com/enola-labs/enola/internal/engine"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentFailedReloadRevert(t *testing.T) {
	root, r, q, sink := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
	r.opts.ReloadEngine = func(context.Context) (*engine.Engine, error) {
		e := testEngine(t, root)
		if _, err := os.Stat(filepath.Join(root, "mcp-arch.yaml")); err == nil {
			e.Config().Ignore = append(e.Config().Ignore, "a.ts")
		}
		return e, nil
	}
	independentWrite(t, root, "mcp-arch.yaml", "ignore: [a.ts]\n")
	q.Add("mcp-arch.yaml")
	sink.FailAt(len(sink.CloneRecords())+1, errors.New("injected broker failure"))
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err == nil {
		t.Fatal("wanted failure")
	}
	os.Remove(filepath.Join(root, "mcp-arch.yaml"))
	sink.FailAt(0, nil)
	q.Add("mcp-arch.yaml")
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err != nil {
		t.Fatal(err)
	}
	for _, p := range r.eng.Config().Ignore {
		if p == "a.ts" {
			t.Fatal("recovered after config revert with failed transaction's ignore still active")
		}
	}
}
func TestIndependentExternalAncestorSymlink(t *testing.T) {
	root, r, _, _ := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
	outer := t.TempDir()
	real := filepath.Join(outer, "real")
	os.Mkdir(real, 0755)
	os.WriteFile(filepath.Join(real, "base.json"), []byte("{}"), 0644)
	alias := filepath.Join(outer, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	r.inputs.configPaths = append(r.inputs.configPaths, filepath.Join(alias, "base.json"))
	s := NewFileChangeSource(root, []string{r.opts.StateDir}, 64)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CoverSessionInputs(r); err == nil {
		t.Fatal("coverage accepted external config with symlinked ancestor")
	}
}
func TestIndependentReloadConfigCaptureRace(t *testing.T) {
	root, r, q, _ := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
	r.opts.ReloadEngine = func(context.Context) (*engine.Engine, error) {
		e := testEngine(t, root)
		e.Config().Ignore = append(e.Config().Ignore, "a.ts")
		independentWrite(t, root, "mcp-arch.yaml", "ignore: []\n")
		return e, nil
	}
	independentWrite(t, root, "mcp-arch.yaml", "ignore: [a.ts]\n")
	q.Add("mcp-arch.yaml")
	if _, err := r.ApplyChanges(context.Background(), q.Drain()); err == nil {
		t.Fatal("accepted config changed inside reload: engine excludes a.ts but captured config includes it")
	}
}
