package engine

import (
	"reflect"
	"sync"
	"testing"

	"github.com/enola-labs/enola/internal/config"
)

func TestConcurrentInventory(t *testing.T) {
	root := t.TempDir()
	writeDeep(t, root, "src/a.ts", "export const a = 1")
	writeDeep(t, root, "src/a.test.ts", "a()")
	writeDeep(t, root, "private/a.ts", "ignored")
	cfg := config.Default()
	cfg.Ignore = append(cfg.Ignore, "private/**", "**/*.test.ts")
	cfg.TestGlobs = []string{"**/*.test.ts"}
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := e.Inventory(root)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			for range 10 {
				got, err := e.Inventory(root)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("Inventory mismatch: %v", err)
				}
				if !e.matchesTestGlob("src/a.test.ts") {
					t.Error("helper lost test match")
				}
				if _, ok := e.ignoreMatch("private/a.ts"); !ok {
					t.Error("helper lost ignore match")
				}
			}
		})
	}
	wg.Wait()
}
