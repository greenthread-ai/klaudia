package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// callThrough runs one tool on an in-process MCP server that returns result,
// through the real SDK transport, and returns what the model would get.
func callThrough(t *testing.T, result *mcpsdk.CallToolResult) tools.Result {
	t.Helper()
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "t", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) { return result, nil })
	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ConnectTransport(ctx, "s", clientT)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	m.Add(server)
	defer func() { m.Close(); _ = ss.Wait() }()
	tl := m.Tools(ctx)
	if len(tl) != 1 {
		t.Fatalf("tools = %d", len(tl))
	}
	res, err := tl[0].Execute(ctx, tools.Context{}, json.RawMessage(`{}`))
	if err != nil || len(res) != 1 {
		t.Fatalf("Execute: %v %v", res, err)
	}
	return res[0]
}

// An image-only result used to reach the model as an empty string.
func TestMCPImageResultBecomesImageBlock(t *testing.T) {
	got := callThrough(t, &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
		&mcpsdk.ImageContent{Data: pngHeader, MIMEType: "image/png"},
		&mcpsdk.ImageContent{Data: []byte("<svg/>"), MIMEType: "image/svg+xml"},
	}})
	if len(got.Images) != 1 || got.Images[0].MediaType != "image/png" {
		t.Fatalf("images = %+v, want the PNG as an image block", got.Images)
	}
	if raw, _ := base64.StdEncoding.DecodeString(got.Images[0].Base64); string(raw) != string(pngHeader) {
		t.Errorf("image bytes changed on the way through: %x", raw)
	}
	if !strings.Contains(got.Content, "image/svg+xml") {
		t.Errorf("content %q does not mention the SVG it could not show", got.Content)
	}
}

func TestMCPResourceAndStructuredResults(t *testing.T) {
	got := callThrough(t, &mcpsdk.CallToolResult{Content: []mcpsdk.Content{
		&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{URI: "file:///log.txt", MIMEType: "text/plain", Text: "line one"}},
		&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{URI: "file:///a.bin", MIMEType: "application/octet-stream", Blob: []byte{1, 2, 3}}},
		&mcpsdk.ResourceLink{URI: "https://x/doc", Name: "doc"},
	}})
	for _, want := range []string{"[resource file:///log.txt]\nline one", "resource file:///a.bin: application/octet-stream, 3 bytes", "[resource link: doc https://x/doc]"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content missing %q:\n%s", want, got.Content)
		}
	}

	got = callThrough(t, &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{},
		StructuredContent: map[string]any{"count": 3},
	})
	if !strings.Contains(got.Content, `"count": 3`) {
		t.Errorf("structured-only result = %q, want its JSON", got.Content)
	}

	got = callThrough(t, &mcpsdk.CallToolResult{Content: []mcpsdk.Content{}})
	if got.Content == "" {
		t.Error("an empty result reached the model as an empty string")
	}
}
