package asyncapiextractor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Keep the original walker as an independent oracle, including post-match
// traversal and error handling rather than only testing candidate predicates.
func serialDetection(root string, probe func(string) bool) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isCandidate(path) && probe(path) {
			found = true
		}
		return nil
	})
	return found, err
}

func TestParallelDetectionMatchesOriginalWalk(t *testing.T) {
	for _, tc := range []struct {
		name, file, body string
		want             bool
	}{
		{"arbitrary-json", "private/arbitrary.json", `{"asyncapi":"2.6.0","channels":{}}`, true},
		{"hidden", ".hidden/a.json", `{"asyncapi":"3.0.0"}`, true},
		{"deep", "a/b/c/d/e/f/g/spec.yaml", "asyncapi: 2.6.0\n", true},
		{"nested-marker", "api.json", `{"other":{"asyncapi":"2.6.0"}}`, false},
		{"invalid", "api.json", `{"asyncapi":"2.6.0"`, false},
		{"testdata", "testdata/api.json", `{"asyncapi":"2.6.0"}`, false},
		{"vendor", "vendor/api.yaml", "asyncapi: 2.6.0\n", false},
		{"after-positive", "a.json", `{"asyncapi":"2.6.0"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, tc.file)
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(tc.body), 0644); err != nil {
				t.Fatal(err)
			}
			// A later skipped directory must not be probed after an earlier match.
			if err := os.MkdirAll(filepath.Join(root, "vendor"), 0755); err != nil {
				t.Fatal(err)
			}
			old, oldErr := serialDetection(root, hasAsyncAPIRoot)
			got, err := New().Detect(root)
			if got != tc.want || got != old || fmt.Sprint(err) != fmt.Sprint(oldErr) {
				t.Fatalf("new=(%v,%v) old=(%v,%v) want=%v", got, err, old, oldErr, tc.want)
			}
		})
	}
	root := filepath.Join(t.TempDir(), "missing")
	old, oldErr := serialDetection(root, hasAsyncAPIRoot)
	got, err := New().Detect(root)
	if got != old || fmt.Sprint(err) != fmt.Sprint(oldErr) {
		t.Fatalf("missing root new=(%v,%v) old=(%v,%v)", got, err, old, oldErr)
	}
}

func TestParallelCandidateProbesAreBoundedAndRediscovered(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 80; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.json", i)), []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	entered := make(chan struct{}, 80)
	release := make(chan struct{})
	done := make(chan struct{})
	var active, peak, count atomic.Int32
	probe := func(string) bool {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		count.Add(1)
		return false
	}
	go func() {
		defer close(done)
		if found, err := detect(root, probe); found || err != nil {
			t.Errorf("detect = %v,%v", found, err)
		}
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			<-done
			t.Fatal("candidate probes did not overlap")
		}
	}
	close(release)
	<-done
	if peak.Load() != 4 || count.Load() != 80 {
		t.Fatalf("peak=%d probes=%d", peak.Load(), count.Load())
	}
	if err := os.WriteFile(filepath.Join(root, "079.json"), []byte(`{"asyncapi":"3.0.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if found, err := New().Detect(root); !found || err != nil {
		t.Fatalf("edited arbitrary JSON not rediscovered: %v,%v", found, err)
	}
	if err := os.Remove(filepath.Join(root, "079.json")); err != nil {
		t.Fatal(err)
	}
	if found, err := New().Detect(root); found || err != nil {
		t.Fatalf("deleted spec retained: %v,%v", found, err)
	}
}

func TestPositiveCandidatePreventsSpeculativeSymlinkRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(root, "z.json")); err != nil {
		t.Fatal(err)
	}
	found, err := detect(root, func(p string) bool {
		if filepath.Base(p) == "z.json" {
			t.Error("symlink probed despite preceding positive candidate")
		}
		return filepath.Base(p) == "a.json"
	})
	if !found || err != nil {
		t.Fatalf("detect = %v,%v", found, err)
	}
}
