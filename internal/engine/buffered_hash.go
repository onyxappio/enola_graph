package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// readHash reads every byte. The scratch buffer bounds per-worker allocation;
// it is not an input cache and no metadata can establish content equality.
// scratch must be nonempty; each caller owns its scratch and digest.
func readHash(r io.Reader, scratch []byte, digest hash.Hash) (string, int64, error) {
	digest.Reset()
	// Hide WriterTo: os.File otherwise bypasses CopyBuffer's supplied buffer.
	n, err := io.CopyBuffer(digest, struct{ io.Reader }{r}, scratch)
	if err != nil {
		return "", 0, err
	}
	// CopyBuffer is finished; EncodeToString copies the appended digest before
	// the next read reuses scratch, so no returned value aliases that buffer.
	return hex.EncodeToString(digest.Sum(scratch[:0])), n, nil
}

func boundedFileHashes(repo string, files []string) (map[string]string, int64) {
	type result struct {
		digest string // A successful SHA256 hex digest is never empty, even for an empty file.
		bytes  int64
	}
	results := make([]result, len(files))
	// The bound is per invocation, not a process-wide concurrency limit.
	workers := min(4, len(files))
	work := func(jobs <-chan int) {
		scratch := make([]byte, 64<<10)
		digest := sha256.New()
		for i := range jobs {
			f, err := os.Open(filepath.Join(repo, files[i]))
			if err != nil {
				continue
			}
			sum, n, err := readHash(f, scratch, digest)
			_ = f.Close() // os.ReadFile also ignores Close errors.
			if err == nil {
				results[i] = result{sum, n}
			}
		}
	}
	if workers > 0 {
		jobs := make(chan int, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); work(jobs) }()
		}
		for i := range files {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}
	hashes := make(map[string]string, len(files))
	var bytes int64
	// Preserve input-order duplicate-key resolution, independent of completion order.
	for i, r := range results {
		if r.digest != "" {
			hashes[files[i]] = r.digest
			bytes += r.bytes
		}
	}
	return hashes, bytes
}
