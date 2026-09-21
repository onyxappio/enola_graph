package graphsession

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Raw native diagnostics include filtered external siblings. A quiet measurement
// must stop those writers or keep their outputs outside the watched config parent.
func TestNativeQuietExternalConfigOutputLayouts(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		name := "shared-parent"
		if isolated {
			name = "isolated-parent"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root, r, _, sink := residentFixture(t, map[string]string{"a.ts": "export const a=1"}, Options{})
			output := t.TempDir()
			configDir := output
			if isolated {
				configDir = filepath.Join(output, "config")
				if err := os.Mkdir(configDir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path string, b []byte) {
				t.Helper()
				if err := os.WriteFile(path, b, 0644); err != nil {
					t.Fatal(err)
				}
			}
			external := filepath.Join(configDir, "base.json")
			write(external, []byte("{}"))
			b, err := json.Marshal(map[string]string{"extends": external})
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(root, "tsconfig.json"), b)
			reports := []string{filepath.Join(output, "metrics.json"), filepath.Join(output, "checks.json")}
			for _, path := range reports {
				write(path, []byte("[]"))
			}
			source := NewFileChangeSource(root, []string{r.opts.StateDir}, 128)
			if err := source.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			for i := 0; i < 4; i++ {
				if _, err := r.ApplyChanges(ctx, source.Drain()); err != nil {
					t.Fatal(err)
				}
				if err := source.CoverSessionInputs(r); err != nil {
					t.Fatal(err)
				}
			}
			// Allow bounded bootstrap deliveries to finish before the diagnostic.
			time.Sleep(200 * time.Millisecond)
			quiet := source.ObservedEvents()
			time.Sleep(300 * time.Millisecond)
			if got := source.ObservedEvents(); got != quiet {
				t.Fatalf("native feedback without writers: %d -> %d", quiet, got)
			}
			before := source.Drain()
			if before.Reconcile != "" || len(before.Paths) != 0 {
				t.Fatalf("bootstrap did not settle: %+v", before)
			}
			for _, path := range reports {
				sequence := source.ObservedPath(path)
				write(path, []byte("[1]"))
				if !isolated {
					deadline := time.Now().Add(3 * time.Second)
					for source.ObservedPath(path) <= sequence && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
					}
					if source.ObservedPath(path) <= sequence {
						t.Fatalf("raw diagnostic hid sibling write: %s", path)
					}
					t.Logf("filtered native sibling: %s sequence=%d", path, source.ObservedPath(path))
				}
			}
			time.Sleep(300 * time.Millisecond)
			got := source.ObservedEvents()
			if isolated && got != quiet {
				t.Fatalf("isolated output unexpectedly observed: %d -> %d", quiet, got)
			}
			if !isolated && got <= quiet {
				t.Fatal("shared-parent writes absent from raw counter")
			}
			t.Logf("report writes: raw counter %d -> %d", quiet, got)
			quiet = got
			time.Sleep(300 * time.Millisecond)
			if got := source.ObservedEvents(); got != quiet {
				t.Fatalf("native feedback after writers stopped: %d -> %d", quiet, got)
			}
			batch := source.Drain()
			if !batch.Covered || batch.From != batch.Through || batch.Reconcile != "" || len(batch.Paths) != 0 {
				t.Fatalf("sibling output reached graph queue: %+v", batch)
			}
			events := len(sink.CloneRecords())
			res, err := r.ApplyChanges(ctx, batch)
			if err != nil {
				t.Fatal(err)
			}
			if res.Reconciled || res.ParsedFiles != 0 || res.TargetGeneration != res.BaseGeneration || !reflect.DeepEqual(res.Work, WorkCounters{}) || len(sink.CloneRecords()) != events {
				t.Fatalf("filtered siblings performed graph work: %+v", res)
			}
			residentCold(t, root, r, sink)
			// Isolation must still cover a real atomic replacement of the config.
			tmp := filepath.Join(configDir, "replace.tmp")
			write(tmp, []byte(`{"compilerOptions":{"strict":true}}`))
			if err := os.Rename(tmp, external); err != nil {
				t.Fatal(err)
			}
			select {
			case <-source.Ready():
			case <-time.After(3 * time.Second):
				t.Fatal("real config replacement not observed")
			}
			res, err = r.ApplyChanges(ctx, source.Drain())
			if err != nil {
				t.Fatal(err)
			}
			if !res.Reconciled {
				t.Fatal("real config replacement did not reconcile")
			}
			residentCold(t, root, r, sink)
		})
	}
}
