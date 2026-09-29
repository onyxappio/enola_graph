package analyzerplugin

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
)

// Handler answers a host callback. Callbacks always run synchronously against
// the caller's captured repository view; implementations should record every
// answer in the active unit's observation set.
type Handler func(context.Context, map[string]any) (map[string]any, error)

// UnitDecl is a stable, plugin-defined analysis unit. Summary dependencies are
// declared up front so a host can settle them in topological order.
type UnitDecl struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Params   map[string]any `json:"params,omitempty"`
	Consumes []string       `json:"consumes,omitempty"`
}

type WireMessage map[string]any

type Client struct {
	cmd         *exec.Cmd
	workDir     string
	stdin       io.WriteCloser
	frames      chan frameResult
	maxFrame    int
	unitTimeout time.Duration
	runDeadline time.Time
	nextID      atomic.Int64
	closed      bool
}

type frameResult struct {
	raw []byte
	err error
}

// Start starts one plugin process for a changed analysis run. Repository code
// runs from an empty temporary cwd with a reduced environment, never with the
// repository path as an argv or environment value. Runtime version is verified
// from the plugin hello response of this same process; there is no separate
// `node -p` probe.
func Start(ctx context.Context, p Loaded, handler Handler) (*Client, error) {
	runtimePath := p.Runtime
	if runtimePath == "" {
		return nil, fmt.Errorf("plugin %q has no resolved Node executable", p.Manifest.Name)
	}
	liveRuntimeDigest, _, err := CachedRuntimeDigest(runtimePath, nil)
	if err != nil {
		return nil, fmt.Errorf("plugin %q runtime fingerprint at start: %w", p.Manifest.Name, err)
	}
	if p.RuntimeDigest != "" && liveRuntimeDigest != p.RuntimeDigest {
		return nil, fmt.Errorf("plugin %q runtime fingerprint changed since load", p.Manifest.Name)
	}
	cwd, err := os.MkdirTemp("", "enola-plugin-")
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(cwd)
		}
	}()
	// Execute the exact bytes that contributed to Identity. Rereading the entry
	// path would allow a concurrent replacement to run unrelated code under the
	// previously verified identity.
	entryBytes := append([]byte(nil), p.EntryBytes...)
	if len(entryBytes) == 0 {
		entryBytes, err = os.ReadFile(p.Entry)
		if err != nil {
			return nil, fmt.Errorf("read plugin %q entry bundle: %w", p.Manifest.Name, err)
		}
		if p.EntryDigest != "" {
			sum := sha256.Sum256(entryBytes)
			if hex.EncodeToString(sum[:]) != p.EntryDigest {
				return nil, fmt.Errorf("plugin %q entry bundle changed since identity was computed", p.Manifest.Name)
			}
		}
	} else if p.EntryDigest != "" {
		sum := sha256.Sum256(entryBytes)
		if hex.EncodeToString(sum[:]) != p.EntryDigest {
			return nil, fmt.Errorf("plugin %q staged entry bytes do not match identity digest", p.Manifest.Name)
		}
	}
	entryName := "plugin" + filepath.Ext(p.Entry)
	if filepath.Ext(entryName) == "" {
		entryName += ".mjs"
	}
	entryCopy := filepath.Join(cwd, entryName)
	if err := os.WriteFile(entryCopy, entryBytes, 0o500); err != nil {
		return nil, fmt.Errorf("stage plugin %q entry bundle: %w", p.Manifest.Name, err)
	}
	cmd := exec.CommandContext(ctx, runtimePath, "./"+entryName)
	cmd.Dir = cwd
	cmd.Env = []string{"TZ=UTC", "LANG=C", "LC_ALL=C", "NODE_OPTIONS=", "PATH=" + filepath.Dir(runtimePath)}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{W: &stderr, N: 8192}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin %q: %w", p.Manifest.Name, err)
	}
	maxFrame := p.Manifest.Limits.MaxFrameBytes
	if maxFrame <= 0 {
		maxFrame = DefaultMaxFrame
	}
	unitTimeoutMS := p.Manifest.Limits.UnitTimeoutMS
	if unitTimeoutMS <= 0 {
		unitTimeoutMS = DefaultUnitMS
	}
	runTimeoutMS := p.Manifest.Limits.RunTimeoutMS
	if runTimeoutMS <= 0 {
		runTimeoutMS = DefaultTimeoutMS
	}
	c := &Client{
		cmd: cmd, workDir: cwd, stdin: stdin, frames: make(chan frameResult, 1), maxFrame: maxFrame,
		unitTimeout: time.Duration(unitTimeoutMS) * time.Millisecond,
		runDeadline: time.Now().Add(time.Duration(runTimeoutMS) * time.Millisecond),
	}
	go c.readFrames(stdout)
	cleanup = false
	// Keep stderr attached to the process for diagnostics; Wait below reaps the
	// process after shutdown or any protocol failure.
	_ = stderr
	versions, _ := json.Marshal(map[string]string{"platform": runtime.GOOS, "arch": runtime.GOARCH})
	helloTimeout := p.Manifest.Limits.HelloTimeoutMS
	if helloTimeout <= 0 {
		helloTimeout = DefaultHelloMS
	}
	helloBudget := time.Duration(helloTimeout) * time.Millisecond
	if rem := time.Until(c.runDeadline); rem > 0 && helloBudget > rem {
		helloBudget = rem
	}
	helloCtx, cancelHello := context.WithTimeout(ctx, helloBudget)
	defer cancelHello()
	grammarHello, err := GrammarHelloValue()
	if err != nil {
		c.Abort()
		return nil, fmt.Errorf("plugin %q host grammar: %w", p.Manifest.Name, err)
	}
	resp, err := c.Call(helloCtx, WireMessage{"op": "hello", "host_api": APIVersion, "grammar": grammarHello, "vocab": p.Manifest.Vocabularies, "config": p.Config.Config, "runtime": json.RawMessage(versions), "concurrency": p.Manifest.Limits.Concurrency}, handler, "hello_ack")
	if err != nil {
		c.Abort()
		return nil, fmt.Errorf("plugin %q hello: %w", p.Manifest.Name, err)
	}
	if got, _ := resp["api"].(string); got != APIVersion {
		c.Abort()
		return nil, fmt.Errorf("plugin %q negotiated unsupported host api %q", p.Manifest.Name, got)
	}
	reported, _ := resp["node"].(string)
	if reported == "" {
		c.Abort()
		return nil, fmt.Errorf("plugin %q hello omitted process.versions.node", p.Manifest.Name)
	}
	if reported != p.Manifest.Runtime.Version {
		c.Abort()
		return nil, fmt.Errorf("plugin %q requires Node %s; hello reported %s", p.Manifest.Name, p.Manifest.Runtime.Version, reported)
	}
	return c, nil
}

func (c *Client) readFrames(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := readFrame(br, c.maxFrame)
		if len(line) > 0 {
			c.frames <- frameResult{raw: line}
		}
		if err != nil {
			c.frames <- frameResult{err: err}
			close(c.frames)
			return
		}
	}
}

func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(out)+len(part) > max {
			return nil, fmt.Errorf("plugin frame exceeds %d bytes", max)
		}
		out = append(out, part...)
		if err == nil {
			return bytes.TrimSpace(out), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(out) > 0 {
			return bytes.TrimSpace(out), nil
		}
		return nil, err
	}
}

func (c *Client) runRemaining() (time.Duration, error) {
	if c == nil {
		return 0, errors.New("nil plugin client")
	}
	if c.runDeadline.IsZero() {
		return time.Duration(DefaultTimeoutMS) * time.Millisecond, nil
	}
	remaining := time.Until(c.runDeadline)
	if remaining <= 0 {
		return 0, errors.New("plugin run deadline exceeded")
	}
	return remaining, nil
}

func (c *Client) requestBudget(unitScoped bool) (time.Duration, error) {
	remaining, err := c.runRemaining()
	if err != nil {
		return 0, err
	}
	budget := remaining
	if unitScoped {
		unit := c.unitTimeout
		if unit <= 0 {
			unit = time.Duration(DefaultUnitMS) * time.Millisecond
		}
		if unit < budget {
			budget = unit
		}
	}
	return budget, nil
}

// Call writes one host request, services plugin callbacks while awaiting its
// matching response, and returns the first message using terminalOp.
func (c *Client) Call(ctx context.Context, request WireMessage, handler Handler, terminalOp string) (WireMessage, error) {
	id := c.nextID.Add(1)
	request["id"] = id
	b, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(b) > c.maxFrame {
		return nil, fmt.Errorf("host frame exceeds %d bytes", c.maxFrame)
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("write plugin request: %w", err)
	}
	// Hello is capped by HelloTimeoutMS via ctx and the total run deadline.
	// Plan/unit work is also capped by UnitTimeoutMS.
	budget, err := c.requestBudget(terminalOp != "hello_ack")
	if err != nil {
		c.Abort()
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if rem := time.Until(deadline); rem <= 0 {
			c.Abort()
			return nil, context.DeadlineExceeded
		} else if rem < budget {
			budget = rem
		}
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			c.Abort()
			return nil, fmt.Errorf("plugin request %d timed out", id)
		case f, ok := <-c.frames:
			if !ok {
				return nil, errors.New("plugin stdout closed")
			}
			if f.err != nil {
				return nil, fmt.Errorf("plugin protocol: %w", f.err)
			}
			var msg WireMessage
			if err := json.Unmarshal(f.raw, &msg); err != nil {
				return nil, fmt.Errorf("malformed plugin frame: %w", err)
			}
			if cb, ok := number(msg["cb"]); ok {
				if handler == nil {
					return nil, errors.New("plugin requested an unsupported host callback")
				}
				answer, err := handler(ctx, msg)
				reply := WireMessage{"cb": cb, "ok": err == nil}
				if err != nil {
					reply["error"] = err.Error()
				} else {
					for k, v := range answer {
						reply[k] = v
					}
				}
				if err := c.write(reply); err != nil {
					return nil, err
				}
				if err != nil {
					return nil, fmt.Errorf("plugin callback %q: %w", msg["op"], err)
				}
				continue
			}
			if errText, _ := msg["op"].(string); errText == "error" {
				return nil, fmt.Errorf("plugin error %v: %v", msg["code"], msg["message"])
			}
			msgID, _ := number(msg["id"])
			if msgID != id {
				return nil, fmt.Errorf("unexpected plugin response id %d (want %d)", msgID, id)
			}
			op, _ := msg["op"].(string)
			if op == terminalOp {
				return msg, nil
			}
		}
	}
}

func number(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

func (c *Client) write(v WireMessage) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > c.maxFrame {
		return fmt.Errorf("callback response exceeds %d bytes", c.maxFrame)
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write plugin callback: %w", err)
	}
	return nil
}

// Plan asks the plugin for deterministic unit declarations.
func (c *Client) Plan(ctx context.Context, filesDigest string, handler Handler) ([]UnitDecl, error) {
	resp, err := c.Call(ctx, WireMessage{"op": "plan", "files_digest": filesDigest}, handler, "plan_result")
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(resp["units"])
	if err != nil {
		return nil, err
	}
	var units []UnitDecl
	if err := json.Unmarshal(b, &units); err != nil {
		return nil, fmt.Errorf("invalid plugin unit plan: %w", err)
	}
	if err := ValidatePlan(units); err != nil {
		return nil, err
	}
	return units, nil
}

// Run executes the supplied units as one request. Per-unit results are returned
// as raw JSON-compatible maps and are validated by the graph host before use.
// Only summaries declared in each unit's consumes list are included in the
// request; undeclared summaries are never exposed on the wire.
func (c *Client) Run(ctx context.Context, units []UnitDecl, summaries map[string]any, handler Handler) ([]WireMessage, error) {
	id := c.nextID.Add(1)
	if len(units) == 0 {
		return nil, nil
	}
	declared := DeclaredSummaries(units, summaries)
	// The run stream may contain several unit_result frames before run_done. The
	// shared Call machinery is specialized here to retain all such results.
	request := WireMessage{"id": id, "op": "run", "units": units, "summaries": declared}
	b, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(b) > c.maxFrame {
		return nil, fmt.Errorf("host run frame exceeds %d bytes", c.maxFrame)
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	budget, err := c.requestBudget(true)
	if err != nil {
		c.Abort()
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if rem := time.Until(deadline); rem <= 0 {
			c.Abort()
			return nil, context.DeadlineExceeded
		} else if rem < budget {
			budget = rem
		}
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	var results []WireMessage
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			c.Abort()
			return nil, errors.New("plugin unit timed out")
		case f, ok := <-c.frames:
			if !ok {
				return nil, errors.New("plugin stdout closed during run")
			}
			if f.err != nil {
				return nil, f.err
			}
			var msg WireMessage
			if err := json.Unmarshal(f.raw, &msg); err != nil {
				return nil, err
			}
			if cb, ok := number(msg["cb"]); ok {
				if handler == nil {
					return nil, errors.New("plugin requested an unsupported host callback")
				}
				answer, err := handler(ctx, msg)
				reply := WireMessage{"cb": cb, "ok": err == nil}
				if err != nil {
					reply["error"] = err.Error()
				} else {
					for k, v := range answer {
						reply[k] = v
					}
				}
				if err := c.write(reply); err != nil {
					return nil, err
				}
				if err != nil {
					return nil, fmt.Errorf("plugin callback %q: %w", msg["op"], err)
				}
				continue
			}
			if op, _ := msg["op"].(string); op == "error" {
				return nil, fmt.Errorf("plugin error %v: %v", msg["code"], msg["message"])
			}
			got, _ := number(msg["id"])
			if got != id {
				return nil, fmt.Errorf("unexpected plugin response id %d (want %d)", got, id)
			}
			switch msg["op"] {
			case "unit_result":
				results = append(results, msg)
			case "run_done":
				return results, nil
			default:
				return nil, fmt.Errorf("unexpected plugin run frame %q", msg["op"])
			}
		}
	}
}

// Close sends a best-effort shutdown and reaps the child.
func (c *Client) Close(ctx context.Context) error {
	if c == nil || c.closed {
		return nil
	}
	c.closed = true
	_, _ = c.Call(ctx, WireMessage{"op": "shutdown"}, nil, "shutdown_ack")
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		c.cleanupWorkDir()
		return err
	case <-ctx.Done():
		_ = c.cmd.Process.Kill()
		<-done
		c.cleanupWorkDir()
		return ctx.Err()
	}
}

// Abort kills and reaps a child after a protocol failure.
func (c *Client) Abort() {
	if c == nil || c.closed {
		return
	}
	c.closed = true
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	c.cleanupWorkDir()
}

func (c *Client) cleanupWorkDir() {
	if c == nil || c.workDir == "" {
		return
	}
	_ = os.RemoveAll(c.workDir)
	c.workDir = ""
}

type limitedWriter struct {
	W io.Writer
	N int64
	n int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.n < w.N {
		keep := int(w.N - w.n)
		if keep > n {
			keep = n
		}
		_, _ = w.W.Write(p[:keep])
		w.n += int64(keep)
	}
	return n, nil
}

// RuntimeDigest fingerprints the exact Node executable bytes, not its version
// string. Prefer CachedRuntimeDigest with durable state so unchanged runs pay
// one stat instead of a full re-hash.
func RuntimeDigest(path string) (string, error) {
	digest, _, err := CachedRuntimeDigest(path, nil)
	return digest, err
}

// DeclaredSummaries keeps only summary values named by the units' consumes
// declarations. Direct request access to an undeclared summary is impossible.
func DeclaredSummaries(units []UnitDecl, summaries map[string]any) map[string]any {
	if len(summaries) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	for _, unit := range units {
		for _, dep := range unit.Consumes {
			if value, ok := summaries[dep]; ok {
				out[dep] = value
			}
		}
	}
	return out
}
