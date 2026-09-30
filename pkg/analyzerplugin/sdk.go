// Package analyzerplugin is the Go authoring SDK for repository-owned Enola
// analyzers. It hides the NDJSON framing and host callback protocol from plugin
// code. Handlers are registered by versioned hook ID, so adding a hook does not
// require every plugin to add a new method to a shared interface.
package analyzerplugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
)

const (
	APIVersion       = "enola.plugin/v2"
	HookAnalysisPlan = "analysis.plan@1"
	HookAnalysisUnit = "analysis.unit@1"
	DefaultMaxFrame  = 64 << 20
)

type HookID string

type handler func(context.Context, Host, json.RawMessage) (json.RawMessage, error)

// Registry contains only the hooks this plugin implements. Register is a
// package function because Go does not allow methods with their own type
// parameters; each registered handler still gets typed request and response
// values.
type Registry struct {
	mu       sync.RWMutex
	handlers map[HookID]handler
}

func NewRegistry() *Registry { return &Registry{handlers: make(map[HookID]handler)} }

// Register adds a typed handler under a stable, versioned hook ID.
func Register[Request, Response any](r *Registry, id HookID, fn func(context.Context, Host, Request) (Response, error)) error {
	if r == nil || id == "" || fn == nil {
		return errors.New("hook registration requires a registry, ID, and handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[HookID]handler)
	}
	if _, exists := r.handlers[id]; exists {
		return fmt.Errorf("hook %q is already registered", id)
	}
	r.handlers[id] = func(ctx context.Context, host Host, raw json.RawMessage) (json.RawMessage, error) {
		var request Request
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, fmt.Errorf("decode request for hook %q: %w", id, err)
		}
		response, err := fn(ctx, host, request)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("encode response for hook %q: %w", id, err)
		}
		return encoded, nil
	}
	return nil
}

func (r *Registry) hook(id HookID) (handler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[id]
	return h, ok
}

func (r *Registry) HookIDs() []HookID {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]HookID, 0, len(r.handlers))
	for id := range r.handlers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

type UnitDecl struct {
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Params   map[string]any `json:"params,omitempty"`
	Consumes []string       `json:"consumes,omitempty"`
}

type PlanRequest struct {
	FilesDigest string `json:"files_digest"`
}

type PlanResponse struct {
	Units []UnitDecl `json:"units"`
}

type AnalyzeUnitRequest struct {
	Unit      UnitDecl                   `json:"unit"`
	Summaries map[string]json.RawMessage `json:"summaries,omitempty"`
}

type Contribution struct {
	Unit    string                       `json:"unit"`
	Owners  map[string]OwnerContribution `json:"owners"`
	Summary any                          `json:"summary,omitempty"`
}

type OwnerContribution struct {
	Nodes       []Node       `json:"nodes,omitempty"`
	Anchors     []Anchor     `json:"anchors,omitempty"`
	Enrichments []Enrichment `json:"enrichments,omitempty"`
}

type Node struct {
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Owner     string         `json:"owner,omitempty"`
	Line      int            `json:"line,omitempty"`
	EndLine   int            `json:"end_line,omitempty"`
	Props     map[string]any `json:"props,omitempty"`
	Relations []Relation     `json:"relations,omitempty"`
}

type Relation struct {
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetFile string `json:"target_file,omitempty"`
}

type Anchor struct {
	Owner          string         `json:"owner,omitempty"`
	Symbol         string         `json:"symbol"`
	Line           int            `json:"line"`
	EndLine        int            `json:"end_line,omitempty"`
	SourceIdentity string         `json:"fsm_source_identity,omitempty"`
	Relations      []Relation     `json:"relations,omitempty"`
	Props          map[string]any `json:"props,omitempty"`
}

type Enrichment struct {
	Owner     string         `json:"owner,omitempty"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Props     map[string]any `json:"props,omitempty"`
	Relations []Relation     `json:"relations,omitempty"`
}

type Document struct {
	Text    string
	Missing bool
}

type ModuleResolution struct {
	Resolved   string   `json:"resolved,omitempty"`
	File       string   `json:"file,omitempty"`
	ModuleDir  string   `json:"module_dir,omitempty"`
	ReplaySpec string   `json:"replay_spec,omitempty"`
	External   bool     `json:"external,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Found      bool     `json:"found"`
}

type ExportResolution struct {
	Target string `json:"target,omitempty"`
	File   string `json:"file,omitempty"`
	Found  bool   `json:"found"`
}

// Host exposes repository data through host-recorded callbacks. Every answer
// participates in plugin unit invalidation; plugins should not read repository
// paths directly.
type Host interface {
	// Config returns the repository-configured plugin value as a defensive JSON
	// copy. Plugin authors can decode it with DecodeConfig[T].
	Config() json.RawMessage
	List(context.Context, string) ([]string, error)
	Read(context.Context, string) (Document, error)
	Probe(context.Context, string) (bool, error)
	Summary(context.Context, string, any) error
	ResolveModule(context.Context, string, string) (ModuleResolution, error)
	ResolveExport(context.Context, string, string) (ExportResolution, error)
}

// DecodeConfig unmarshals the repository-configured plugin value into a typed
// Go configuration. An omitted or null config produces the type's zero value.
func DecodeConfig[T any](host Host) (T, error) {
	var config T
	if host == nil {
		return config, errors.New("cannot decode plugin config from a nil host")
	}
	raw := bytes.TrimSpace(host.Config())
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return config, nil
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return config, fmt.Errorf("decode plugin config: %w", err)
	}
	return config, nil
}

// Serve runs a registered plugin on stdin/stdout. stderr remains available for
// plugin diagnostics. The host owns process limits and kills the process when a
// handler exceeds its deadline.
func Serve(registry *Registry) error { return ServeIO(registry, os.Stdin, os.Stdout) }

// ServeIO is the testable form of Serve.
func ServeIO(registry *Registry, input io.Reader, output io.Writer) error {
	if registry == nil {
		return errors.New("nil hook registry")
	}
	reader := bufio.NewReader(input)
	writer := bufio.NewWriter(output)
	var pluginConfig json.RawMessage
	write := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if len(data) > DefaultMaxFrame {
			return errors.New("plugin response exceeds maximum frame size")
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
		return writer.Flush()
	}
	read := func() ([]byte, error) {
		var data []byte
		for {
			part, err := reader.ReadSlice('\n')
			if len(data)+len(part) > DefaultMaxFrame {
				return nil, errors.New("host request exceeds maximum frame size")
			}
			data = append(data, part...)
			if err == nil {
				return data, nil
			}
			if !errors.Is(err, bufio.ErrBufferFull) {
				return nil, err
			}
		}
	}
	for {
		frame, err := read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var request struct {
			ID        json.RawMessage `json:"id"`
			Op        string          `json:"op"`
			API       string          `json:"host_api"`
			Hook      HookID          `json:"hook"`
			Unit      string          `json:"unit"`
			Payload   json.RawMessage `json:"request"`
			Config    json.RawMessage `json:"config"`
			HostHooks []HookID        `json:"host_hooks"`
		}
		if err := json.Unmarshal(frame, &request); err != nil {
			return fmt.Errorf("decode host request: %w", err)
		}
		switch request.Op {
		case "hello":
			if request.API != APIVersion {
				_ = write(map[string]any{"id": request.ID, "op": "error", "code": "api_mismatch", "message": "host API is not enola.plugin/v2"})
				continue
			}
			pluginConfig = append(pluginConfig[:0], request.Config...)
			if err := write(map[string]any{"id": request.ID, "op": "hello_ack", "api": APIVersion, "hooks": registry.HookIDs()}); err != nil {
				return err
			}
		case "hook_call":
			h, ok := registry.hook(request.Hook)
			if !ok {
				_ = write(map[string]any{"id": request.ID, "op": "error", "code": "unknown_hook", "message": fmt.Sprintf("hook %q is not registered", request.Hook)})
				continue
			}
			host := &rpcHost{read: read, write: write, unit: request.Unit, config: append(json.RawMessage(nil), pluginConfig...)}
			response, err := h(context.Background(), host, request.Payload)
			if err != nil {
				if writeErr := write(map[string]any{"id": request.ID, "op": "error", "code": "hook_failed", "message": err.Error()}); writeErr != nil {
					return writeErr
				}
				continue
			}
			if err := write(map[string]any{"id": request.ID, "op": "hook_result", "result": response}); err != nil {
				return err
			}
		case "shutdown":
			return write(map[string]any{"id": request.ID, "op": "shutdown_ack"})
		default:
			return fmt.Errorf("unsupported host operation %q", request.Op)
		}
	}
}

type rpcHost struct {
	read   func() ([]byte, error)
	write  func(any) error
	unit   string
	config json.RawMessage
	next   uint64
	mu     sync.Mutex
}

func (h *rpcHost) Config() json.RawMessage {
	return append(json.RawMessage(nil), h.config...)
}

func (h *rpcHost) call(ctx context.Context, op string, fields map[string]any, into any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	h.next++
	request := map[string]any{"cb": h.next, "op": op, "unit": h.unit}
	for key, value := range fields {
		request[key] = value
	}
	if err := h.write(request); err != nil {
		return err
	}
	frame, err := h.read()
	if err != nil {
		return err
	}
	var response struct {
		CB    uint64          `json:"cb"`
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Body  json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(frame, &response); err != nil {
		return fmt.Errorf("decode host callback response: %w", err)
	}
	if response.CB != h.next {
		return fmt.Errorf("host callback response ID %d does not match %d", response.CB, h.next)
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	if into != nil {
		if err := json.Unmarshal(frame, into); err != nil {
			return err
		}
	}
	return nil
}

func (h *rpcHost) List(ctx context.Context, glob string) ([]string, error) {
	var response struct {
		Paths []string `json:"paths"`
	}
	err := h.call(ctx, "list", map[string]any{"glob": glob}, &response)
	return response.Paths, err
}

func (h *rpcHost) Read(ctx context.Context, path string) (Document, error) {
	var response struct {
		Text    string `json:"text"`
		Missing bool   `json:"missing"`
	}
	err := h.call(ctx, "read", map[string]any{"path": path}, &response)
	return Document{Text: response.Text, Missing: response.Missing}, err
}

func (h *rpcHost) Probe(ctx context.Context, path string) (bool, error) {
	var response struct {
		Exists bool `json:"exists"`
	}
	err := h.call(ctx, "probe", map[string]any{"path": path}, &response)
	return response.Exists, err
}

func (h *rpcHost) Summary(ctx context.Context, id string, into any) error {
	var response struct {
		Value json.RawMessage `json:"value"`
	}
	if err := h.call(ctx, "summary", map[string]any{"target": id}, &response); err != nil {
		return err
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(response.Value, into)
}

func (h *rpcHost) ResolveModule(ctx context.Context, from, spec string) (ModuleResolution, error) {
	var response ModuleResolution
	err := h.call(ctx, "resolve_module", map[string]any{"from": from, "spec": spec}, &response)
	return response, err
}

func (h *rpcHost) ResolveExport(ctx context.Context, file, name string) (ExportResolution, error) {
	var response ExportResolution
	err := h.call(ctx, "resolve_export", map[string]any{"file": file, "name": name}, &response)
	return response, err
}
