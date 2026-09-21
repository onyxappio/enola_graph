package graphstream

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Opt-in diagnostics avoid clocks and output on the ordinary publication path.
// Counters describe commit-pump Sync calls (including commit-index durability),
// not the final acknowledgment-only Flush or Close sync.
type transportDiagnostics struct {
	Groups       int64         `json:"groups"`
	Messages     int64         `json:"messages"`
	Bytes        int64         `json:"bytes"`
	GroupSizes   map[int]int64 `json:"group_sizes"`
	SyncNS       int64         `json:"sync_ns"`
	MaxSyncNS    int64         `json:"max_sync_ns"`
	QueueStalls  int64         `json:"queue_stalls"`
	QueueStallNS int64         `json:"queue_stall_ns"`
	once         sync.Once
}

func newTransportDiagnostics() *transportDiagnostics {
	if os.Getenv("ENOLA_TRANSPORT_DIAGNOSTICS") != "1" {
		return nil
	}
	return &transportDiagnostics{GroupSizes: make(map[int]int64)}
}
func diagnosticStart(d *transportDiagnostics) time.Time {
	if d == nil {
		return time.Time{}
	}
	return time.Now()
}
func (a *asyncPub) recordSync(messages int, bytes int64, started time.Time) {
	if a.diagnostics == nil {
		return
	}
	elapsed := time.Since(started).Nanoseconds()
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.diagnostics
	d.Groups++
	d.Messages += int64(messages)
	d.Bytes += bytes
	d.GroupSizes[messages]++
	d.SyncNS += elapsed
	if elapsed > d.MaxSyncNS {
		d.MaxSyncNS = elapsed
	}
}
func (a *asyncPub) recordStall(started time.Time) {
	if a.diagnostics == nil {
		return
	}
	elapsed := time.Since(started).Nanoseconds()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.diagnostics.QueueStalls++
	a.diagnostics.QueueStallNS += elapsed
}
func (a *asyncPub) reportDiagnostics() {
	if a.diagnostics == nil {
		return
	}
	a.diagnostics.once.Do(func() {
		a.mu.Lock()
		b, _ := json.Marshal(a.diagnostics)
		a.mu.Unlock()
		fmt.Fprintf(os.Stderr, "graphstream_transport %s\n", b)
	})
}
