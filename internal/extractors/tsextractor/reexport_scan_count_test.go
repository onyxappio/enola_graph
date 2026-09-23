package tsextractor

import (
	"sync"
	"testing"
)

// TestNamedExportCacheScansEachFileOnce pins the property SummaryScans claims to
// report. The index is reached from two directions now - the binder following a
// re-export chain, and the parse loop recording a file's own export surface - so
// a check-then-act cache lets two goroutines miss together, parse the same file
// twice and both increment the counter. The count then reports how the workers
// happened to interleave rather than how many files were scanned, which is what
// made the config-inventory comparison nondeterministic at 2 versus 3.
func TestNamedExportCacheScansEachFileOnce(t *testing.T) {
	src := []byte("export function round(){return 1}\nexport const ceil = 2\n")
	var reads int64
	var mu sync.Mutex
	readSrc := func(string) []byte {
		mu.Lock()
		reads++
		mu.Unlock()
		return src
	}
	cache := newNamedExportCache()
	known := map[string]bool{"src/origin.ts": true}

	const workers = 32
	got := make([]*namedExportIndex, workers)
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(workers)
	for i := range workers {
		go func(i int) {
			defer done.Done()
			start.Wait()
			got[i] = cache.index("src/origin.ts", readSrc, nil, known)
		}(i)
	}
	start.Done()
	done.Wait()

	if n := cache.summaryScans(); n != 1 {
		t.Fatalf("summary scans = %d, want 1 for one file", n)
	}
	if reads != 1 {
		t.Fatalf("source reads = %d, want 1: the file was parsed more than once", reads)
	}
	for i, idx := range got {
		if idx != got[0] {
			t.Fatalf("worker %d got a different index object; callers must share one", i)
		}
		if !idx.local["round"] || !idx.local["ceil"] {
			t.Fatalf("worker %d got an index missing exports: %v", i, idx.surface())
		}
	}
}
