package mcp

import (
	"encoding/base64"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Only text blocks used to survive: a screenshot tool returned "", and a tool
// answering in structuredContent alone looked silent.
func TestResultOfKeepsNonTextContent(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	r := resultOf(&mcpsdk.CallToolResult{Content: []mcpsdk.Content{
		&mcpsdk.TextContent{Text: "shot taken"},
		&mcpsdk.ImageContent{MIMEType: "image/png", Data: png},
		&mcpsdk.ImageContent{MIMEType: "image/tiff", Data: png},
		&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{URI: "file:///a.txt", Text: "embedded body"}},
		&mcpsdk.EmbeddedResource{Resource: &mcpsdk.ResourceContents{URI: "file:///b.bin", MIMEType: "application/zip", Blob: []byte("PK")}},
		&mcpsdk.ResourceLink{URI: "file:///c.md", Name: "c"},
		&mcpsdk.AudioContent{MIMEType: "audio/wav", Data: []byte{1}},
	}})

	if len(r.Images) != 1 || r.Images[0].MediaType != "image/png" ||
		r.Images[0].Base64 != base64.StdEncoding.EncodeToString(png) {
		t.Errorf("images = %+v, want the one PNG, base64-encoded", r.Images)
	}
	for _, want := range []string{
		"shot taken",
		"image/tiff is not a supported image type",
		"embedded body",
		"binary resource omitted: file:///b.bin, application/zip, 2 bytes",
		"resource link: file:///c.md (c)",
		"audio omitted: audio/wav, 1 byte",
	} {
		if !strings.Contains(r.Content, want) {
			t.Errorf("content lacks %q:\n%s", want, r.Content)
		}
	}
}

func TestResultOfFallsBackToStructuredContent(t *testing.T) {
	r := resultOf(&mcpsdk.CallToolResult{StructuredContent: map[string]any{"count": 3}})
	if r.Content != `{"count":3}` {
		t.Errorf("content = %q, want the structured JSON", r.Content)
	}
	// Text wins when both are present: the spec has servers mirror one in the other.
	r = resultOf(&mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: "three"}},
		StructuredContent: map[string]any{"count": 3},
		IsError:           true,
	})
	if r.Content != "three" || !r.IsError {
		t.Errorf("got %+v, want the text alone and the error flag kept", r)
	}
}
