package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// visionTypes are the image media types the API accepts in a tool_result.
// Anything else is described rather than sent, since one unsupported block
// fails the whole request.
var visionTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// resultOf translates an MCP tool result for the model. Only text used to
// survive, so a screenshot tool returned an empty string, and a tool that
// answered with structured content alone looked like it had said nothing.
// Images go to the model as images; embedded text resources as text; what
// cannot be sent (audio, binary blobs, links) is named in one line, so the
// model knows it was there.
func resultOf(res *mcpsdk.CallToolResult) tools.Result {
	r := tools.Result{IsError: res.IsError}
	var parts []string
	for _, c := range res.Content {
		switch c := c.(type) {
		case *mcpsdk.TextContent:
			parts = append(parts, c.Text)
		case *mcpsdk.ImageContent:
			if visionTypes[c.MIMEType] {
				r.Images = append(r.Images, tools.ResultImage{
					MediaType: c.MIMEType,
					Base64:    base64.StdEncoding.EncodeToString(c.Data),
				})
			} else {
				parts = append(parts, fmt.Sprintf("[image omitted: %s is not a supported image type]", orUnknown(c.MIMEType)))
			}
		case *mcpsdk.AudioContent:
			parts = append(parts, fmt.Sprintf("[audio omitted: %s, %s]", orUnknown(c.MIMEType), byteCount(len(c.Data))))
		case *mcpsdk.EmbeddedResource:
			if c.Resource != nil {
				parts = append(parts, resourceText(c.Resource))
			}
		case *mcpsdk.ResourceLink:
			line := "[resource link: " + c.URI
			if c.Name != "" {
				line += " (" + c.Name + ")"
			}
			parts = append(parts, line+" — read it with ReadMcpResource if needed]")
		}
	}
	if len(parts) == 0 && len(r.Images) == 0 && res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			parts = append(parts, string(b))
		}
	}
	r.Content = strings.Join(parts, "\n")
	return r
}

// resourceText is a resource's text, or one line describing a binary one.
func resourceText(rc *mcpsdk.ResourceContents) string {
	if rc.Text != "" || rc.Blob == nil {
		return rc.Text
	}
	return fmt.Sprintf("[binary resource omitted: %s, %s, %s]", rc.URI, orUnknown(rc.MIMEType), byteCount(len(rc.Blob)))
}

func orUnknown(mime string) string {
	if mime == "" {
		return "unknown type"
	}
	return mime
}

func byteCount(n int) string {
	if n == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", n)
}
