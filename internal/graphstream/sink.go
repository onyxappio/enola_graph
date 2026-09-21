package graphstream

import (
	"context"
	"sync"
)

// Sink publishes one protocol payload. Publish must not return until the
// destination has accepted the bytes (broker ack, or durable local record).
// A bounded publisher sits in front of a network sink; silent loss is forbidden.
type Sink interface {
	Publish(ctx context.Context, subject, msgID string, payload []byte) error
	Flush(ctx context.Context) error
	Close() error
}

// Recorded is one Publish call captured by MemorySink.
type Recorded struct {
	Subject string
	MsgID   string
	Payload []byte
}

// MemorySink records publishes for tests. It is also the reference consumer
// input: tests decode Payload in order.
type MemorySink struct {
	mu      sync.Mutex
	Records []Recorded
	failAt  int // 1-based publish index to fail; 0 means never
	n       int
	err     error
}

// FailAt makes the nth Publish (1-based) return err. Used to exercise retry.
func (s *MemorySink) FailAt(n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failAt = n
	s.err = err
}

func (s *MemorySink) Publish(ctx context.Context, subject, msgID string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if s.failAt != 0 && s.n == s.failAt {
		return s.err
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)
	s.Records = append(s.Records, Recorded{Subject: subject, MsgID: msgID, Payload: cp})
	return nil
}

func (s *MemorySink) Flush(context.Context) error { return nil }
func (s *MemorySink) Close() error                { return nil }

// CloneRecords returns a snapshot of recorded publishes.
func (s *MemorySink) CloneRecords() []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recorded, len(s.Records))
	for i, r := range s.Records {
		cp := make([]byte, len(r.Payload))
		copy(cp, r.Payload)
		out[i] = Recorded{Subject: r.Subject, MsgID: r.MsgID, Payload: cp}
	}
	return out
}
