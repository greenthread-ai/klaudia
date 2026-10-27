// Command mcpserver is a minimal stdio MCP server for the e2e tests: one
// "ping" tool, named after the first argument so two instances are told
// apart. It stands in for a real server (godot, Figma) where a test needs one
// that actually connects.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pingIn struct{}

func main() {
	name := "e2e"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: name, Version: "0.0.1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "ping", Description: "Answer pong"},
		func(context.Context, *mcp.CallToolRequest, pingIn) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong from " + name}}}, nil, nil
		})
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
