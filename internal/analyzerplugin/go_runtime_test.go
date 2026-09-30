package analyzerplugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestGoExecutableUsesVersionedHooksAndHostCallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a small Go plugin fixture")
	}
	repo := t.TempDir()
	pluginDir := filepath.Join(repo, "tools", "task-graph")
	entry := filepath.Join(pluginDir, "dist", "task-graph")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", entry, "./testdata/go_plugin")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Go plugin fixture: %v\n%s", err, output)
	}
	manifest := "" +
		"api: enola.plugin/v2\n" +
		"name: task-graph\n" +
		"runtime:\n  kind: go-executable\n  entry: dist/task-graph\n" +
		"identity_files: [dist/task-graph]\n" +
		"hooks: [analysis.plan@1, analysis.unit@1]\n" +
		"owner_domain: [docs/**]\n" +
		"limits:\n  hello_timeout_ms: 10000\n  unit_timeout_ms: 10000\n  run_timeout_ms: 30000\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "enola-plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(repo, []Config{{Path: "tools/task-graph"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := Start(context.Background(), loaded[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := func(_ context.Context, message map[string]any) (map[string]any, error) {
		switch message["op"] {
		case "list":
			return map[string]any{"paths": []string{"docs/T-1.md"}}, nil
		case "read":
			return map[string]any{"text": "# Example task"}, nil
		default:
			t.Fatalf("unexpected host callback: %#v", message)
			return nil, nil
		}
	}
	units, err := client.Plan(context.Background(), "file-set", handler)
	if err != nil {
		client.Abort()
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].ID != "markdown:docs/T-1.md" {
		client.Abort()
		t.Fatalf("plan = %#v", units)
	}
	results, err := client.Run(context.Background(), units, nil, handler)
	if err != nil {
		client.Abort()
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	var contribution UnitResult
	encoded, err := json.Marshal(results[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &contribution); err != nil {
		t.Fatal(err)
	}
	if contribution.Unit != units[0].ID {
		t.Fatalf("result unit = %q", contribution.Unit)
	}
	node := contribution.Owners["docs/T-1.md"].Nodes[0]
	if node.Kind != "task" || node.Relations[0].TargetKind != "task" || node.Props["heading"] != "# Example task" {
		t.Fatalf("generic contribution = %#v", node)
	}
	if err := ValidateResult(loaded[0], units[0], contribution); err != nil {
		t.Fatalf("generic result validation: %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Close(closeCtx); err != nil {
		t.Fatal(err)
	}

	if got, want := loaded[0].Manifest.Hooks, []string{HookAnalysisPlanV1, HookAnalysisUnitV1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical hooks = %v, want %v", got, want)
	}
}
