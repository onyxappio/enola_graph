package tsextractor

import (
	"testing"
	"time"
)

// A stalled source read must not stall metadata recording in another worker.
func TestIndependentPeekDoesNotJoinInflightIndex(t *testing.T) {
	cache := newNamedExportCache()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan *namedExportIndex, 1)
	go func() {
		done <- cache.index("src/a.ts", func(string) []byte {
			close(started)
			<-release
			return []byte("export function local() { return 1; }")
		}, nil, map[string]bool{"src/a.ts": true})
	}()
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("index did not reach source read")
	}
	peeked := make(chan *namedExportIndex, 1)
	go func() { peeked <- cache.peek("src/a.ts") }()
	select {
	case idx := <-peeked:
		if idx != nil {
			t.Fatal("incomplete index was published")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peek joined blocked source read")
	}
	close(release)
	released = true
	select {
	case idx := <-done:
		if idx == nil || !idx.contextFree {
			t.Fatal("completed local index lacks proof")
		}
		if cache.peek("src/a.ts") != idx {
			t.Fatal("peek does not return completed index")
		}
		if cache.summaryScans() != 1 {
			t.Fatalf("got %d scans", cache.summaryScans())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released index did not finish")
	}
}
