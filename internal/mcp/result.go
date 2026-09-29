package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// visionTypes are the image types the model accepts as image blocks.
var visionTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// resultOf turns an MCP tool result into what the model sees.
//
// Only text content used to survive: an image, an embedded resource or a
// structuredContent-only result reached the model as an empty string, which it
// read as "the tool returned nothing" — a screenshot tool that worked looked
// like one that failed. Now:
//   - text is kept as it was;
//   - images in a type the model can view become image blocks, as Read's do;
//   - an embedded resource contributes its text, or its image, or a line
//     saying what binary it was;
//   - audio, resource links and images in other types become a line naming
//     them, so the model knows they exist;
//   - structuredContent is shown as JSON when the result has no text (a server
//     that sends both is expected to put the same data in the text);
//   - a result with nothing in it says so.
func resultOf(res *mcpsdk.CallToolResult) tools.Result {
	var text []string
	var images []tools.ResultImage
	image := func(data []byte, mime, what string) {
		if visionTypes[mime] {
			images = append(images, tools.ResultImage{MediaType: mime, Base64: base64.StdEncoding.EncodeToString(data)})
			return
		}
		text = append(text, fmt.Sprintf("[%s: %s, %d bytes — not a type that can be shown]", what, mime, len(data)))
	}
	for _, c := range res.Content {
		switch c := c.(type) {
		case *mcpsdk.TextContent:
			text = append(text, c.Text)
		case *mcpsdk.ImageContent:
			image(c.Data, c.MIMEType, "image")
		case *mcpsdk.AudioContent:
			text = append(text, fmt.Sprintf("[audio: %s, %d bytes — not shown]", c.MIMEType, len(c.Data)))
		case *mcpsdk.ResourceLink:
			text = append(text, fmt.Sprintf("[resource link: %s %s]", firstNonEmpty(c.Title, c.Name), c.URI))
		case *mcpsdk.EmbeddedResource:
			r := c.Resource
			switch {
			case r == nil:
			case r.Text != "":
				text = append(text, fmt.Sprintf("[resource %s]\n%s", r.URI, r.Text))
			case len(r.Blob) > 0:
				image(r.Blob, r.MIMEType, "resource "+r.URI)
			}
		}
	}
	if len(text) == 0 && res.StructuredContent != nil {
		if b, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
			text = append(text, string(b))
		}
	}
	out := tools.Result{Content: strings.Join(text, "\n"), Images: images, IsError: res.IsError}
	if out.Content == "" && len(images) == 0 {
		out.Content = "(the tool returned no content)"
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
