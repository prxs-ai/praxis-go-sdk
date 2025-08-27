package main

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	srv := server.NewMCPServer("echo", "1.0.0")
	tool := mcp.Tool{
		Name:        "echo",
		Description: "echo text back",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"text": map[string]any{"type": "string"},
			},
			Required: []string{"text"},
		},
	}
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{mcp.NewTextContent(req.GetString("text", ""))},
		}, nil
	})
	if err := server.ServeStdio(srv); err != nil {
		panic(err)
	}
	select {}
}
