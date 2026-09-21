package manifestextractor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestWithoutLockfilesInputsAndIdentity(t *testing.T) {
	e, legacy := NewWithoutLockfiles(), New()
	for name := range lockNames {
		for _, rel := range []string{name, "packages/app/" + name} {
			if e.OwnsFile(rel) || e.ContentInput(rel) {
				t.Errorf("owns lock %s", rel)
			}
			if !legacy.OwnsFile(rel) || !legacy.ContentInput(rel) {
				t.Errorf("legacy lost lock %s", rel)
			}
		}
	}
	for name := range manifestReaders {
		if !e.OwnsFile(name) || !e.ContentInput(name) {
			t.Errorf("lost manifest %s", name)
		}
	}
	if e.NameSetInput() {
		t.Fatal("lock names must not be inputs")
	}
	root := t.TempDir()
	if e.ConfigKey() == legacy.ConfigKey() || e.DeltaContext(root) == legacy.DeltaContext(root) {
		t.Fatal("lock modes share cache identity")
	}
	if legacy.ConfigKey() != (&Extractor{}).ConfigKey() {
		t.Fatal("zero value no longer legacy")
	}
}

func TestWithoutLockfilesHistory(t *testing.T) {
	// Cover every parser, both supported and unsupported lock formats, and
	// resolution at the root, intermediate ancestor, and manifest directory.
	root := repoWith(t, map[string]string{
		"packages/app/package.json":     `{"dependencies":{"lodash":"^4.17.0"}}`,
		"packages/app/Gemfile":          "gem 'rails', '~> 7.0'\n",
		"packages/app/Cargo.toml":       "[dependencies]\nserde = \"^1.0\"\n",
		"packages/app/pubspec.yaml":     "dependencies:\n  http: ^1.0.0\n",
		"packages/app/requirements.txt": "requests>=2.0\n",
		"packages/app/pyproject.toml":   "[project]\ndependencies = [\"flask>=3.0\"]\n",
	})
	e := NewWithoutLockfiles()
	extract := func() map[string]facts.Fact {
		t.Helper()
		fs, err := e.Extract(context.Background(), root, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]facts.Fact{}
		for _, f := range fs {
			out[f.Name] = f
		}
		return out
	}
	baseline, key := extract(), e.DeltaContext(root)
	if len(baseline) != 6 {
		t.Fatalf("missing manifest declarations: %v", baseline)
	}
	for _, f := range baseline {
		if f.PropString("resolved_version") != "" || f.PropString("unresolved_lock") != "" || f.PropBool("pinned") {
			t.Fatalf("range advertised as resolved: %+v", f)
		}
	}
	unchanged := func() {
		t.Helper()
		if got := extract(); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("lock changed facts: %v", got)
		}
		if got := e.DeltaContext(root); got != key {
			t.Fatalf("lock changed context: %s != %s", got, key)
		}
	}
	locks := map[string]string{
		"package-lock.json": `{"packages":{"node_modules/lodash":{"version":"4.17.21"}}}`,
		"yarn.lock":         "lodash@^4.17.0:\n  version \"4.17.21\"\n",
		"Gemfile.lock":      "GEM\n  specs:\n    rails (7.1.0)\n",
		"Cargo.lock":        "[[package]]\nname = \"serde\"\nversion = \"1.0.200\"\n",
		"pubspec.lock":      "packages:\n  http:\n    version: \"1.2.0\"\n",
		"uv.lock":           "[[package]]\nname = \"requests\"\nversion = \"2.32.0\"\n",
		"poetry.lock":       "[[package]]\nname = \"flask\"\nversion = \"3.1.0\"\n",
		"Pipfile.lock":      "unsupported", "pnpm-lock.yaml": "unsupported", "bun.lockb": "unsupported",
	}
	for _, dir := range []string{"", "packages", "packages/app"} {
		for name, content := range locks {
			full := filepath.Join(root, dir, name)
			if err := os.WriteFile(full, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			unchanged()
		}
	}
	for _, dir := range []string{"packages/app", "packages", ""} {
		for name := range locks {
			full := filepath.Join(root, dir, name)
			if err := os.WriteFile(full, []byte("changed invalid lock bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			unchanged()
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			unchanged()
			// A dangling symlink produces a read error without relying on uid/mode.
			if err := os.Symlink(filepath.Join(root, "missing-target"), full); err != nil {
				t.Fatal(err)
			}
			unchanged()
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "packages/app/package.json"), []byte(`{"dependencies":{"lodash":"^5.0.0"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if e.DeltaContext(root) == key {
		t.Fatal("manifest edit did not invalidate context")
	}
	if got := extract()["pkg:npm/lodash"].PropString("constraint"); got != "^5.0.0" {
		t.Fatalf("lost manifest edit: %s", got)
	}
}

func TestWithoutLockfilesReadBoundary(t *testing.T) {
	root := repoWith(t, map[string]string{"package-lock.json": "lock bytes", "package.json": "manifest bytes"})
	rc := &readCtx{repoPath: root, withoutLockfiles: true, locks: map[string]map[string]string{
		"package-lock.json": {"lodash": "4.17.21"},
	}}
	if rc.read("package-lock.json") != "" {
		t.Fatal("read lock bytes")
	}
	if rc.read("package.json") != "manifest bytes" {
		t.Fatal("did not read manifest")
	}
	resolved, path := rc.lock("packages/app/package.json", "package-lock.json", func(string) map[string]string {
		t.Fatal("parsed lock in manifest-only mode")
		return nil
	})
	if resolved != nil || path != "" {
		t.Fatal("reused lock cache")
	}
	if rc.exists("packages/app/package.json", "package-lock.json") != "" {
		t.Fatal("reported ancestor lock")
	}
}

func TestWithoutLockfilesPreservesManifestPins(t *testing.T) {
	root := repoWith(t, map[string]string{
		"go.mod":           "module example.com/app\nrequire example.com/lib v1.2.3\n",
		"requirements.txt": "requests==2.32.0\n",
		"uv.lock":          "[[package]]\nname = \"requests\"\nversion = \"9.9.9\"\n",
	})
	fs, err := NewWithoutLockfiles().Extract(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"pkg:golang/example.com/lib": "v1.2.3", "pkg:pypi/requests": "2.32.0"}
	if len(fs) != len(want) {
		t.Fatalf("facts: %v", fs)
	}
	for _, f := range fs {
		if f.PropString("resolved_version") != want[f.Name] || !f.PropBool("pinned") {
			t.Fatalf("lost manifest pin: %+v", f)
		}
	}
}
