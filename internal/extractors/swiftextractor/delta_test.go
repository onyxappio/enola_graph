package swiftextractor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeltaContextRootIncludesAndMarkers(t *testing.T) {
	root := t.TempDir()
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("project.yml", "include:\n - private/TARGETS.data\n - path: disabled.data\n   enable: false\n")
	write("private/targets.data", "include: [nested.data]\ntargets: {}\n")
	write("nested.data", "targets: {}\n")
	write("disabled.data", "targets: {}\n")
	e := New()
	initial := e.DeltaContext(root)
	write("nested.data", "targets: {Nested: {type: framework}}\n")
	write("disabled.data", "targets: {Disabled: {type: framework}}\n")
	if e.DeltaContext(root) != initial {
		t.Fatal("invented recursive or disabled include semantics")
	}
	write("private/targets.data", "targets: {Core: {type: framework}}\n")
	included := e.DeltaContext(root)
	if included == initial {
		t.Fatal("case-insensitive include fallback not observed")
	}
	if err := os.MkdirAll(filepath.Join(root, "App/Assets.xcassets"), 0755); err != nil {
		t.Fatal(err)
	}
	marked := e.DeltaContext(root)
	if marked == included {
		t.Fatal("empty asset marker not observed")
	}
	if err := os.Remove(filepath.Join(root, "App/Assets.xcassets")); err != nil {
		t.Fatal(err)
	}
	if e.DeltaContext(root) != included {
		t.Fatal("marker removal not observed")
	}
	write("Info.plist", "")
	if e.DeltaContext(root) == included {
		t.Fatal("empty root Info.plist existence not observed")
	}
}
