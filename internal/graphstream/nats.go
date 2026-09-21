package graphstream

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSOptions configure the JetStream adapter.
type NATSOptions struct {
	URL          string
	Stream       string
	Subject      string
	MaxPayload   int
	AckWait      time.Duration
	ConnectWait  time.Duration
	RetentionAge time.Duration
}

func (o *NATSOptions) defaults() {
	if o.Stream == "" {
		o.Stream = "ENOLA_GRAPH"
	}
	if o.Subject == "" {
		o.Subject = "enola.graph.>"
	}
	if o.MaxPayload == 0 {
		o.MaxPayload = 512 * 1024
	}
	if o.AckWait == 0 {
		o.AckWait = 10 * time.Second
	}
	if o.ConnectWait == 0 {
		o.ConnectWait = 5 * time.Second
	}
	if o.RetentionAge == 0 {
		o.RetentionAge = 7 * 24 * time.Hour
	}
}

// NATSSink publishes to a JetStream stream using Limits retention so one
// consumer's ack cannot remove messages another project still needs.
type NATSSink struct {
	nc      *nats.Conn
	js      jetstream.JetStream
	stream  string
	subject string
	max     int
	ackWait time.Duration
	ownConn bool
}

// ConnectNATS dials a broker and creates the stream only if it is absent.
func ConnectNATS(ctx context.Context, opts NATSOptions) (*NATSSink, error) {
	opts.defaults()
	if opts.URL == "" {
		return nil, fmt.Errorf("graphstream: nats url is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nc, err := nats.Connect(opts.URL, nats.Timeout(opts.ConnectWait), nats.Name("enola-graph"))
	if err != nil {
		return nil, fmt.Errorf("graphstream nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("graphstream jetstream: %w", err)
	}
	s := &NATSSink{
		nc: nc, js: js, stream: opts.Stream,
		subject: concreteSubject(opts.Subject),
		max:     opts.MaxPayload, ackWait: opts.AckWait, ownConn: true,
	}
	if err := s.ensureStream(ctx, opts); err != nil {
		nc.Close()
		return nil, err
	}
	return s, nil
}

func concreteSubject(pattern string) string {
	return ConcretePublishSubject(pattern, "local", "default", "run")
}

// ConcretePublishSubject maps a stream wildcard to one publish subject inside it.
func ConcretePublishSubject(pattern, repoID, contextID, runID string) string {
	if pattern == "" {
		return SubjectForRun(repoID, contextID, runID)
	}
	base := pattern
	switch {
	case strings.HasSuffix(base, ".>"):
		base = strings.TrimSuffix(base, ".>")
	case strings.HasSuffix(base, ".*"):
		base = strings.TrimSuffix(base, ".*")
	default:
		return pattern
	}
	return base + "." + encodeToken(runID)
}

func (s *NATSSink) ensureStream(ctx context.Context, opts NATSOptions) error {
	_, err := s.js.Stream(ctx, opts.Stream)
	if err == nil {
		return nil
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("graphstream stream %s: %w", opts.Stream, err)
	}
	_, err = s.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:       opts.Stream,
		Subjects:   []string{opts.Subject},
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     opts.RetentionAge,
		Storage:    jetstream.FileStorage,
		Duplicates: 2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("graphstream stream %s: %w", opts.Stream, err)
	}
	return nil
}

func (s *NATSSink) Publish(ctx context.Context, subject, msgID string, payload []byte) error {
	if s.max > 0 && len(payload) > s.max {
		return fmt.Errorf("graphstream: payload %d exceeds max %d; split the batch", len(payload), s.max)
	}
	if subject == "" {
		subject = s.subject
	}
	pubCtx := ctx
	if s.ackWait > 0 {
		var cancel context.CancelFunc
		pubCtx, cancel = context.WithTimeout(ctx, s.ackWait)
		defer cancel()
	}
	msg := &nats.Msg{Subject: subject, Data: payload, Header: nats.Header{}}
	if msgID != "" {
		msg.Header.Set(jetstream.MsgIDHeader, msgID)
	}
	ack, err := s.js.PublishMsg(pubCtx, msg)
	if err != nil {
		return fmt.Errorf("graphstream publish %s: %w", msgID, err)
	}
	if ack == nil {
		return fmt.Errorf("graphstream publish %s: empty ack", msgID)
	}
	if ack.Stream != "" && ack.Stream != s.stream {
		return fmt.Errorf("graphstream publish %s: ack stream %s want %s", msgID, ack.Stream, s.stream)
	}
	return nil
}

func (s *NATSSink) Flush(ctx context.Context) error {
	if s.nc == nil {
		return nil
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return s.nc.Flush()
	}
	return s.nc.FlushTimeout(time.Until(deadline))
}

func (s *NATSSink) Close() error {
	if s.nc != nil && s.ownConn {
		s.nc.Close()
	}
	return nil
}

// JS returns the JetStream context for tests.
func (s *NATSSink) JS() jetstream.JetStream { return s.js }

// SubjectForRun is the concrete JetStream subject for one analysis run.
func SubjectForRun(repoID, contextID, runID string) string {
	return fmt.Sprintf("enola.graph.%s.%s.%s", encodeToken(repoID), encodeToken(contextID), encodeToken(runID))
}

func encodeToken(s string) string {
	if s == "" {
		s = "_"
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString([]byte(s)))
}
