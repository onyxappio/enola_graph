package detectnames

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/enola-labs/enola/internal/extractors/inputscope"
	"github.com/enola-labs/enola/internal/graphinput"
)

func TestPolicyDiscoveryWalk(t *testing.T) {
	root := t.TempDir()
	paths := []string{".app/package.json", "node_modules/local/package.json", "testdata/package.json", "vendor/package.json", "visible/package.json"}
	for _, rel := range append(append([]string{}, paths...), "blocked/package.json", ".git/package.json", "package-lock.json") {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := graphinput.Build(root, graphinput.Options{CacheExclusions: []string{}, Exclude: []string{"blocked/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := Walk(root, &inputscope.Scope{Root: root, Policy: policy}); !reflect.DeepEqual(got, paths) {
		t.Fatalf("graph names = %v, want %v", got, paths)
	}
	for _, scope := range []*inputscope.Scope{nil, {Root: root}} {
		want := []string{"blocked/package.json", "package-lock.json", "visible/package.json"}
		if got := Walk(root, scope); !reflect.DeepEqual(got, want) {
			t.Fatalf("legacy names = %v, want %v", got, want)
		}
	}
}
