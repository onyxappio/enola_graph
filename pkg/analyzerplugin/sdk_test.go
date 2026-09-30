package analyzerplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistryServesTypedVersionedHookAndHostCallback(t *testing.T) {
	type request struct {
		Path string `json:"path"`
	}
	type response struct {
		Text string `json:"text"`
	}
	registry := NewRegistry()
	if err := Register[request, response](registry, "documents.extract@1", func(ctx context.Context, host Host, req request) (response, error) {
		config, err := DecodeConfig[struct {
			Series string `json:"series"`
		}](host)
		if err != nil {
			return response{}, err
		}
		doc, err := host.Read(ctx, req.Path)
		if err != nil {
			return response{}, err
		}
		return response{Text: config.Series + ":" + doc.Text}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Register[request, response](registry, "documents.extract@1", func(context.Context, Host, request) (response, error) {
		return response{}, nil
	}); err == nil {
		t.Fatal("duplicate hook registration was accepted")
	}

	input := strings.Join([]string{
		`{"id":1,"op":"hello","host_api":"enola.plugin/v2","config":{"series":"sample"}}`,
		`{"id":2,"op":"hook_call","hook":"documents.extract@1","unit":"doc:1","request":{"path":"docs/1.md"}}`,
		`{"cb":1,"ok":true,"text":"# Task 1"}`,
		`{"id":3,"op":"shutdown"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := ServeIO(registry, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("wrote %d frames, want hello, callback, result, shutdown: %s", len(lines), output.String())
	}
	var hello, callback, result, shutdown map[string]any
	for i, dst := range []*map[string]any{&hello, &callback, &result, &shutdown} {
		if err := json.Unmarshal([]byte(lines[i]), dst); err != nil {
			t.Fatalf("decode output frame %d: %v", i, err)
		}
	}
	if hello["op"] != "hello_ack" || hello["api"] != APIVersion {
		t.Fatalf("hello frame = %#v", hello)
	}
	if callback["op"] != "read" || callback["path"] != "docs/1.md" || callback["unit"] != "doc:1" {
		t.Fatalf("host callback frame = %#v", callback)
	}
	if result["op"] != "hook_result" {
		t.Fatalf("hook result frame = %#v", result)
	}
	body, ok := result["result"].(map[string]any)
	if !ok || body["text"] != "sample:# Task 1" {
		t.Fatalf("typed hook result = %#v", result["result"])
	}
	if shutdown["op"] != "shutdown_ack" {
		t.Fatalf("shutdown frame = %#v", shutdown)
	}
}
