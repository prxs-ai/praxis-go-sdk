package main

import (
	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"
)

type EchoArgs struct {
	Text string `json:"text"`
}

func main() {
	server := mcp.NewServer(stdio.NewStdioServerTransport())
	_ = server.RegisterTool("echo", "echo text back", func(args EchoArgs) (*mcp.ToolResponse, error) {
		return mcp.NewToolResponse(mcp.NewTextContent(args.Text)), nil
	})
	if err := server.Serve(); err != nil {
		panic(err)
	}
	select {}
}
