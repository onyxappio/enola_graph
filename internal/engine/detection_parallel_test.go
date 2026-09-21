package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enola-labs/enola/internal/config"
	"github.com/enola-labs/enola/internal/facts"
)

type detectorFunc struct {
	name   string
	detect func() (bool, error)
}

func (d detectorFunc) Name() string                { return d.name }
func (d detectorFunc) Detect(string) (bool, error) { return d.detect() }
func (d detectorFunc) Extract(context.Context, string, []string) ([]facts.Fact, error) {
	return nil, nil
}

func TestIndependentDetectionOverlapsWithOpaqueBarriers(t *testing.T) {
	cfg := config.Default()
	cfg.Extractors = []string{"first", "second", "opaque", "last"}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var running atomic.Int32
	var barrier atomic.Bool
	for _, name := range []string{"first", "second"} {
		eng.RegisterIndependentExtractor(detectorFunc{name, func() (bool, error) {
			running.Add(1)
			entered <- struct{}{}
			<-release
			running.Add(-1)
			return true, nil
		}})
	}
	eng.RegisterExtractor(detectorFunc{"opaque", func() (bool, error) {
		if running.Load() != 0 {
			t.Error("opaque detector overlapped independent detection")
		}
		barrier.Store(true)
		return true, nil
	}})
	eng.RegisterIndependentExtractor(detectorFunc{"last", func() (bool, error) {
		if !barrier.Load() {
			t.Error("later detector crossed opaque barrier")
		}
		return true, fmt.Errorf("failed detector")
	}})
	done := make(chan map[string]bool, 1)
	go func() { done <- eng.DetectExtractors(t.TempDir(), nil) }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			<-done
			t.Fatal("independent detectors did not overlap")
		}
	}
	close(release)
	got := <-done
	if len(got) != 3 || !got["first"] || !got["second"] || !got["opaque"] || got["last"] {
		t.Fatalf("detected = %v", got)
	}
}

func TestDetectionHonorsDisabledAndOpaqueOrder(t *testing.T) {
	cfg := config.Default()
	cfg.Extractors = []string{"enabled"}
	eng, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	eng.RegisterIndependentExtractor(detectorFunc{"disabled", func() (bool, error) { t.Error("disabled detector called"); return true, nil }})
	eng.RegisterExtractor(detectorFunc{"enabled", func() (bool, error) { return true, nil }})
	got := eng.DetectExtractors(t.TempDir(), nil)
	if len(got) != 1 || !got["enabled"] {
		t.Fatalf("detected = %v", got)
	}
}
