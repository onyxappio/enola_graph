// Package graphprofile is env-gated wall-time tracing for graphsession delta/noop.
// Enable with ENOLA_GRAPH_PROFILE=1. It is a measurement aid, not a public API.
package graphprofile

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Enabled reports whether phase tracing is on.
func Enabled() bool {
	v := os.Getenv("ENOLA_GRAPH_PROFILE")
	return v != "" && v != "0" && v != "false"
}

// Log writes one phase line to stderr when enabled.
func Log(phase string, d time.Duration, extra string) {
	if !Enabled() {
		return
	}
	if extra == "" {
		fmt.Fprintf(os.Stderr, "[graph-profile] %-32s %7.3fs\n", phase, d.Seconds())
		return
	}
	fmt.Fprintf(os.Stderr, "[graph-profile] %-32s %7.3fs  %s\n", phase, d.Seconds(), extra)
}

// Trace is a sequential phase timer from a single start point. A process runs
// several of these at once and they nest, so every Mark line carries the name
// of the trace it belongs to: only the Marks of one trace partition that
// trace's window, and summing across names double counts the nested ones.
type Trace struct {
	mu   sync.Mutex
	name string
	t0   time.Time
	last time.Time
}

// Start begins an unnamed trace. Returns nil when profiling is off so calls
// stay cheap.
func Start() *Trace { return StartNamed("") }

// StartNamed begins a trace that labels its Mark lines with name.
func StartNamed(name string) *Trace {
	if !Enabled() {
		return nil
	}
	now := time.Now()
	return &Trace{name: name, t0: now, last: now}
}

// Mark records elapsed time since the previous Mark (or Start).
func (t *Trace) Mark(phase string, extra string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	dt := now.Sub(t.last)
	total := now.Sub(t.t0)
	t.last = now
	t.mu.Unlock()
	name := t.name
	if name == "" {
		name = "unnamed"
	}
	if extra == "" {
		fmt.Fprintf(os.Stderr, "[graph-profile] %-32s %7.3fs  total=%6.3fs  trace=%s\n", phase, dt.Seconds(), total.Seconds(), name)
		return
	}
	fmt.Fprintf(os.Stderr, "[graph-profile] %-32s %7.3fs  total=%6.3fs  trace=%s  %s\n", phase, dt.Seconds(), total.Seconds(), name, extra)
}

// Since logs a duration measured by the caller.
func Since(phase string, start time.Time, extra string) {
	Log(phase, time.Since(start), extra)
}
