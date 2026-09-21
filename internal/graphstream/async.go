package graphstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type jobKind int

const (
	kindOther jobKind = iota
	kindBegin
	kindLocal
	kindScope
	kindResolved
	kindEnd
)

type asyncJob struct {
	ctx     context.Context
	msgID   string
	subject string
	payload []byte
	seq     int64
	kind    jobKind
	meta    envelopeMetadata
}

type envelopeMetadata struct {
	kind   jobKind
	isEnd  bool
	endRun string
}

func inspectPayload(p []byte) envelopeMetadata {
	var probe struct {
		Type  string `json:"type"`
		Phase string `json:"phase"`
		RunID string `json:"run_id"`
	}
	if json.Unmarshal(p, &probe) != nil {
		// Preserve independent-probe behavior for malformed/non-protocol inputs.
		return envelopeMetadata{kind: classifyPayloadFallback(p), isEnd: endType(p), endRun: endRunID(p)}
	}
	m := envelopeMetadata{}
	switch probe.Type {
	case TypeBeginReplace:
		m.kind = kindBegin
	case TypeEndReplace:
		m.kind, m.isEnd, m.endRun = kindEnd, true, probe.RunID
	case TypeBatch:
		switch probe.Phase {
		case PhaseLocal:
			m.kind = kindLocal
		case PhaseScope:
			m.kind = kindScope
		default:
			m.kind = kindResolved
		}
	}
	return m
}

func classifyPayloadFallback(p []byte) jobKind {
	var probe struct {
		Type  string `json:"type"`
		Phase string `json:"phase"`
	}
	if json.Unmarshal(p, &probe) != nil {
		return kindOther
	}
	switch probe.Type {
	case TypeBeginReplace:
		return kindBegin
	case TypeEndReplace:
		return kindEnd
	case TypeBatch:
		switch probe.Phase {
		case PhaseLocal:
			return kindLocal
		case PhaseScope:
			return kindScope
		default:
			return kindResolved
		}
	default:
		return kindOther
	}
}

// asyncPub admits payloads into a bounded volatile queue, then a single commit
// worker appends each group to the journal and fsyncs before sink delivery.
//
// Exact bounds:
//   - pending+committing+ready hold at most maxItems messages waiting for
//     group-commit or a worker.
//   - Up to maxInFlight additional messages may be inside Sink.Publish.
//   - Queue bytes count pending+committing+ready payload and must stay <= maxBytes.
//     In-flight payload is not included; peak outstanding bytes are maxBytes
//     plus those in-flight payloads.
//   - A single payload larger than maxBytes is rejected immediately.
//
// Publish returning nil is queue admission, not durability. The commit worker
// appends the group and fsyncs before a job becomes ready for the sink.
// Flush waits until pending, committing, ready, and in-flight are empty, then
// fsyncs acks. Do not checkpoint before Flush.
type asyncPub struct {
	flushers        int
	diagnostics     *transportDiagnostics
	maxItems        int
	maxBytes        int64
	maxInFlight     int
	mu              sync.Mutex
	pending         []asyncJob
	pendingBytes    int64
	committing      int
	committingBytes int64
	ready           []asyncJob
	readyBytes      int64
	inFlight        int
	inFlightBytes   int64
	queuedPay       map[string][]byte
	nextSeq         int64
	unfinished      map[int64]jobKind
	err             error
	closed          bool
	wake            chan struct{}
	deliverWake     chan struct{}
	barrier         chan struct{}
	space           chan struct{}
	idle            chan struct{}
	done            chan struct{}
	stop            context.CancelFunc
	stopCtx         context.Context
	wg              sync.WaitGroup
}

func (a *asyncPub) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (a *asyncPub) setErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err == nil {
		a.err = err
	}
	a.signal(a.space)
	a.signal(a.idle)
	a.signal(a.deliverWake)
	a.signal(a.wake)
	a.signal(a.barrier)
}

func (a *asyncPub) getErr() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

func (a *asyncPub) snapshotClosed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

func (a *asyncPub) queueLen() int {
	return len(a.pending) + a.committing + len(a.ready)
}

func (a *asyncPub) queueBytes() int64 {
	return a.pendingBytes + a.committingBytes + a.readyBytes
}

func (a *asyncPub) isIdle() bool {
	return len(a.pending) == 0 && a.committing == 0 && len(a.ready) == 0 && a.inFlight == 0
}

func (a *asyncPub) markDoneLocked(seq int64) {
	delete(a.unfinished, seq)
	a.signal(a.barrier)
}

func (a *asyncPub) predsOKLocked(job asyncJob) bool {
	if job.kind == kindBegin {
		return true
	}
	// Completed history cannot block delivery. Keep only outstanding jobs, so
	// gate work and memory are bounded by queue capacity plus in-flight jobs.
	for seq, kind := range a.unfinished {
		if seq >= job.seq {
			continue
		}
		if job.kind == kindEnd || kind == kindBegin || (job.kind == kindResolved && kind == kindScope) {
			return false
		}
	}
	return true
}

// EnableAsync admits each Publish into a bounded volatile in-memory queue with
// no journal mutex or disk I/O on the caller. Durable history checks, append,
// and fsync happen on the commit worker. Exact bound: at most maxItems queued
// (pending+committing+ready) plus maxInFlight in-flight jobs; queue bytes count
// queued payload only (peak bytes = maxBytes plus in-flight payloads).
func (p *Publisher) EnableAsync(maxItems int, maxBytes int64) {
	if p == nil {
		return
	}
	if maxItems <= 0 {
		maxItems = 32
	}
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	maxInFlight := 8
	if p.MaxInFly > 1 {
		maxInFlight = p.MaxInFly
	}
	if maxItems < maxInFlight {
		maxInFlight = maxItems
	}
	if maxInFlight < 1 {
		maxInFlight = 1
	}
	stopCtx, stop := context.WithCancel(context.Background())
	p.async = &asyncPub{
		diagnostics: newTransportDiagnostics(),
		maxItems:    maxItems,
		maxBytes:    maxBytes,
		maxInFlight: maxInFlight,
		queuedPay:   map[string][]byte{},
		unfinished:  map[int64]jobKind{},
		wake:        make(chan struct{}, 1),
		deliverWake: make(chan struct{}, 1),
		barrier:     make(chan struct{}, 1),
		space:       make(chan struct{}, 1),
		idle:        make(chan struct{}, 1),
		done:        make(chan struct{}),
		stop:        stop,
		stopCtx:     stopCtx,
	}
	a := p.async
	a.wg.Add(1 + maxInFlight)
	go func() {
		defer a.wg.Done()
		p.commitPump()
	}()
	for i := 0; i < maxInFlight; i++ {
		go func() {
			defer a.wg.Done()
			p.deliverPump()
		}()
	}
	go func() {
		a.wg.Wait()
		close(a.done)
	}()
}

func (p *Publisher) commitPump() {
	a := p.async
	for {
		a.mu.Lock()
		for len(a.pending) == 0 && !a.closed {
			a.mu.Unlock()
			select {
			case <-a.wake:
			case <-a.stopCtx.Done():
				a.mu.Lock()
				a.closed = true
				continue
			}
			a.mu.Lock()
		}
		if len(a.pending) == 0 {
			closed := a.closed
			a.mu.Unlock()
			a.signal(a.idle)
			a.signal(a.deliverWake)
			if closed {
				return
			}
			continue
		}
		p.coalesceLocked()
		group := a.pending
		groupBytes := a.pendingBytes
		a.pending = nil
		a.pendingBytes = 0
		a.committing = len(group)
		a.committingBytes = groupBytes
		fail := a.err
		closed := a.closed
		a.mu.Unlock()

		clearCommit := func(dropIDs bool) {
			a.mu.Lock()
			a.committing = 0
			a.committingBytes = 0
			if dropIDs {
				for _, job := range group {
					delete(a.queuedPay, job.msgID)
				}
			}
			a.mu.Unlock()
			a.signal(a.space)
			a.signal(a.idle)
			a.signal(a.deliverWake)
		}

		toReady := group
		readyBytes := groupBytes
		var toSkip []asyncJob
		if p.Journal != nil {
			var appendErr error
			var keep []asyncJob
			var keepBytes int64
			for _, job := range group {
				skip, err := p.Journal.asyncAdmit(job.msgID, job.subject, job.payload)
				if err != nil {
					appendErr = err
					break
				}
				if skip {
					toSkip = append(toSkip, job)
					continue
				}
				if err := p.Journal.appendClassifiedUnsynced(JournalEntry{MsgID: job.msgID, Subject: job.subject, Payload: job.payload}, &job.meta); err != nil {
					appendErr = err
					break
				}
				keep = append(keep, job)
				keepBytes += int64(len(job.payload))
			}
			if appendErr == nil && len(keep) > 0 {
				started := diagnosticStart(a.diagnostics)
				appendErr = p.Journal.Sync()
				a.recordSync(len(keep), keepBytes, started)
			}
			if appendErr != nil {
				a.setErr(appendErr)
				clearCommit(true)
				continue
			}
			toReady = keep
			readyBytes = keepBytes
		}
		if fail != nil || closed {
			clearCommit(true)
			if closed {
				a.mu.Lock()
				empty := len(a.pending) == 0 && a.committing == 0
				a.mu.Unlock()
				if empty {
					return
				}
			}
			continue
		}

		a.mu.Lock()
		a.committing = 0
		a.committingBytes = 0
		for _, job := range toSkip {
			delete(a.queuedPay, job.msgID)
			a.markDoneLocked(job.seq)
		}
		if a.err != nil || a.closed {
			for _, job := range toReady {
				delete(a.queuedPay, job.msgID)
			}
			a.mu.Unlock()
			a.signal(a.space)
			a.signal(a.idle)
			a.signal(a.deliverWake)
			continue
		}
		a.ready = append(a.ready, toReady...)
		a.readyBytes += readyBytes
		a.mu.Unlock()
		a.signal(a.deliverWake)
		a.signal(a.space)
		a.signal(a.idle)
	}
}

func (p *Publisher) deliverPump() {
	a := p.async
	for {
		a.mu.Lock()
		for len(a.ready) == 0 && !a.closed {
			a.mu.Unlock()
			select {
			case <-a.deliverWake:
			case <-a.stopCtx.Done():
				a.mu.Lock()
				a.closed = true
				continue
			}
			a.mu.Lock()
		}
		if len(a.ready) == 0 {
			closed := a.closed
			a.mu.Unlock()
			a.signal(a.idle)
			if closed {
				return
			}
			continue
		}
		if a.err != nil || a.closed {
			job := a.ready[0]
			a.ready = a.ready[1:]
			need := int64(len(job.payload))
			a.readyBytes -= need
			if a.readyBytes < 0 {
				a.readyBytes = 0
			}
			delete(a.queuedPay, job.msgID)
			a.mu.Unlock()
			a.signal(a.space)
			a.signal(a.idle)
			a.signal(a.deliverWake)
			a.signal(a.barrier)
			continue
		}
		if !a.predsOKLocked(a.ready[0]) {
			a.mu.Unlock()
			select {
			case <-a.barrier:
			case <-a.deliverWake:
			case <-a.stopCtx.Done():
			}
			continue
		}
		job := a.ready[0]
		a.ready = a.ready[1:]
		need := int64(len(job.payload))
		a.readyBytes -= need
		if a.readyBytes < 0 {
			a.readyBytes = 0
		}
		a.inFlight++
		a.inFlightBytes += need
		if len(a.ready) > 0 {
			a.signal(a.deliverWake)
		}
		a.mu.Unlock()
		a.signal(a.space)

		subj := job.subject
		if subj == "" {
			subj = p.Subject
		}
		parent := job.ctx
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := mergeStop(parent, a.stopCtx)
		err := p.Sink.Publish(ctx, subj, job.msgID, job.payload)
		cancel()
		if err != nil {
			a.setErr(err)
			a.mu.Lock()
			a.inFlight--
			a.inFlightBytes -= need
			if a.inFlightBytes < 0 {
				a.inFlightBytes = 0
			}
			delete(a.queuedPay, job.msgID)
			a.mu.Unlock()
			a.signal(a.idle)
			a.signal(a.space)
			a.signal(a.barrier)
			continue
		}
		if p.Journal != nil {
			if err := p.Journal.ackUnsynced(job.msgID); err != nil {
				a.setErr(err)
			}
		}
		a.mu.Lock()
		a.inFlight--
		a.inFlightBytes -= need
		if a.inFlightBytes < 0 {
			a.inFlightBytes = 0
		}
		delete(a.queuedPay, job.msgID)
		a.markDoneLocked(job.seq)
		a.mu.Unlock()
		a.signal(a.idle)
		a.signal(a.space)
		a.signal(a.deliverWake)
	}
}

func mergeStop(parent, stop context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stopAfter := context.AfterFunc(stop, cancel)
	return ctx, func() {
		stopAfter()
		cancel()
	}
}

func (p *Publisher) enqueue(ctx context.Context, msgID string, payload []byte) error {
	a := p.async
	need := int64(len(payload))
	if need > a.maxBytes {
		return fmt.Errorf("graphstream: payload %d exceeds async maxBytes %d", need, a.maxBytes)
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	meta := inspectPayload(cp)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.stopCtx.Err(); err != nil {
			if ge := a.getErr(); ge != nil {
				return ge
			}
			return fmt.Errorf("graphstream: publisher closed")
		}
		a.mu.Lock()
		if a.err != nil {
			err := a.err
			a.mu.Unlock()
			return err
		}
		if a.closed {
			a.mu.Unlock()
			return fmt.Errorf("graphstream: publisher closed")
		}
		if prev, ok := a.queuedPay[msgID]; ok {
			same := bytes.Equal(prev, cp)
			a.mu.Unlock()
			if same {
				return nil
			}
			return fmt.Errorf("graphstream: msg id %s reused with different payload", msgID)
		}
		if a.queueLen() < a.maxItems && a.queueBytes()+need <= a.maxBytes {
			a.nextSeq++
			seq := a.nextSeq
			kind := meta.kind
			a.unfinished[seq] = kind
			a.pending = append(a.pending, asyncJob{ctx: ctx, msgID: msgID, subject: p.Subject, payload: cp, seq: seq, kind: kind, meta: meta})
			a.pendingBytes += need
			a.queuedPay[msgID] = cp
			a.mu.Unlock()
			a.signal(a.wake)
			return nil
		}
		a.mu.Unlock()
		started := diagnosticStart(a.diagnostics)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.stopCtx.Done():
			if ge := a.getErr(); ge != nil {
				return ge
			}
			return fmt.Errorf("graphstream: publisher closed")
		case <-a.space:
		}
		a.recordStall(started)
	}
}

func (p *Publisher) waitIdle(ctx context.Context) error {
	a := p.async
	a.mu.Lock()
	a.flushers++
	a.mu.Unlock()
	a.signal(a.wake)
	defer func() { a.mu.Lock(); a.flushers--; a.mu.Unlock() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		idle := a.isIdle()
		err := a.err
		a.mu.Unlock()
		if err != nil {
			return err
		}
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.stopCtx.Done():
			if ge := a.getErr(); ge != nil {
				return ge
			}
			a.mu.Lock()
			idle = a.isIdle()
			a.mu.Unlock()
			if idle {
				return fmt.Errorf("graphstream: publisher closed")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-a.idle:
			case <-a.done:
				if ge := a.getErr(); ge != nil {
					return ge
				}
				return fmt.Errorf("graphstream: publisher closed")
			}
		case <-a.idle:
		}
	}
}

// CloseAsync cancels in-flight sink publishes and stops the workers. It does
// not wait indefinitely: the workers' Publish calls are cancelled via stopCtx
// (sinks must honor context). Double-close is safe. Buffered journal writes
// are synced so unacked payloads survive for replay.
func (p *Publisher) CloseAsync() {
	if p == nil || p.async == nil {
		return
	}
	a := p.async
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	if a.stop != nil {
		a.stop()
	}
	a.signal(a.wake)
	a.signal(a.deliverWake)
	a.signal(a.barrier)
	a.signal(a.space)
	a.signal(a.idle)
	<-a.done
	a.reportDiagnostics()
	if p.Journal != nil {
		_ = p.Journal.Sync()
	}
}

// Coalesce only within the existing queue bounds. The deadline prevents a
// sparse producer from waiting for a full group; Flush and shutdown bypass it.
// Called with a.mu held and returns with it held.
func (p *Publisher) coalesceLocked() {
	a := p.async
	if p.Journal == nil {
		return
	}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for len(a.pending) < 16 && len(a.pending) < a.maxItems && a.pendingBytes < a.maxBytes && a.flushers == 0 && !a.closed && a.err == nil {
		for _, job := range a.pending {
			if job.kind == kindBegin || job.kind == kindScope || job.kind == kindEnd {
				return
			}
		}
		if timer == nil {
			timer = time.NewTimer(3 * time.Millisecond)
		}
		a.mu.Unlock()
		expired := false
		select {
		case <-timer.C:
			expired = true
		case <-a.wake:
		case <-a.stopCtx.Done():
		}
		a.mu.Lock()
		if expired {
			return
		}
	}
}
