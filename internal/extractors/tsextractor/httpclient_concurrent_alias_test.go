package tsextractor

import (
	"fmt"
	"sync"
	"testing"
)

func TestExtractHTTPClientFacts_ConcurrentFetchAliases(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; k < 16; k++ {
				source := []byte(fmt.Sprintf("const send%d = globalThis.fetch;\nsend%d('/concurrent/%d', {method:'POST'});\n", i, i, i))
				found := byNameMethod(extractHTTPClientFacts(source, "src/control.ts"))
				want := fmt.Sprintf("/concurrent/%d", i)
				if found[want] != "POST" {
					t.Errorf("worker %d lost its own route: %v", i, found)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
