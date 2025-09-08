package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
)

func main() {
	// Create MCP server
	mcpServer := server.NewMCPServer(
		"Filesystem MCP Server",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
	)

	// Define filesystem tools
	// Read file tool
	readTool := mcpTypes.NewTool("read_file",
		mcpTypes.WithDescription("Read contents of a file"),
		mcpTypes.WithString("path",
			mcpTypes.Required(),
			mcpTypes.Description("Path to the file to read"),
		),
	)
	mcpServer.AddTool(readTool, readFileHandler)

	// Write file tool
	writeTool := mcpTypes.NewTool("write_file",
		mcpTypes.WithDescription("Write content to a file"),
		mcpTypes.WithString("path",
			mcpTypes.Required(),
			mcpTypes.Description("Path to the file to write"),
		),
		mcpTypes.WithString("content",
			mcpTypes.Required(),
			mcpTypes.Description("Content to write to the file"),
		),
	)
	mcpServer.AddTool(writeTool, writeFileHandler)

	// List directory tool
	listTool := mcpTypes.NewTool("list_directory",
		mcpTypes.WithDescription("List contents of a directory"),
		mcpTypes.WithString("path",
			mcpTypes.Required(),
			mcpTypes.Description("Path to the directory to list"),
		),
	)
	mcpServer.AddTool(listTool, listDirectoryHandler)

	// Create directory tool
	createDirTool := mcpTypes.NewTool("create_directory",
		mcpTypes.WithDescription("Create a new directory"),
		mcpTypes.WithString("path",
			mcpTypes.Required(),
			mcpTypes.Description("Path of the directory to create"),
		),
	)
	mcpServer.AddTool(createDirTool, createDirectoryHandler)

	// Delete file tool
	deleteTool := mcpTypes.NewTool("delete_file",
		mcpTypes.WithDescription("Delete a file"),
		mcpTypes.WithString("path",
			mcpTypes.Required(),
			mcpTypes.Description("Path to the file to delete"),
		),
	)
	mcpServer.AddTool(deleteTool, deleteFileHandler)

	// Start stdio server (for now, as HTTP requires additional setup)
	log.Println("Starting MCP Filesystem Server with stdio transport")
	
	// Use ServeStdio for simplicity
	if err := server.ServeStdio(mcpServer); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// Handler functions
func readFileHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	path, ok := args["path"].(string)
	if !ok {
		return errorResult("path parameter is required"), nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to read file: %v", err)), nil
	}

	return &mcpTypes.CallToolResult{
		Content: []mcpTypes.Content{
			mcpTypes.TextContent{
				Type: "text",
				Text: string(content),
			},
		},
	}, nil
}

func writeFileHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	path, ok := args["path"].(string)
	if !ok {
		return errorResult("path parameter is required"), nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return errorResult("content parameter is required"), nil
	}

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to write file: %v", err)), nil
	}

	return &mcpTypes.CallToolResult{
		Content: []mcpTypes.Content{
			mcpTypes.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path),
			},
		},
	}, nil
}

func listDirectoryHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	path, ok := args["path"].(string)
	if !ok {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to list directory: %v", err)), nil
	}

	var result string
	for _, entry := range entries {
		info, _ := entry.Info()
		result += fmt.Sprintf("%s %10d %s\n", 
			entry.Type().String(), 
			info.Size(), 
			entry.Name())
	}

	return &mcpTypes.CallToolResult{
		Content: []mcpTypes.Content{
			mcpTypes.TextContent{
				Type: "text",
				Text: result,
			},
		},
	}, nil
}

func createDirectoryHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	path, ok := args["path"].(string)
	if !ok {
		return errorResult("path parameter is required"), nil
	}

	err := os.MkdirAll(path, 0755)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to create directory: %v", err)), nil
	}

	return &mcpTypes.CallToolResult{
		Content: []mcpTypes.Content{
			mcpTypes.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Successfully created directory: %s", path),
			},
		},
	}, nil
}

func deleteFileHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	path, ok := args["path"].(string)
	if !ok {
		return errorResult("path parameter is required"), nil
	}

	err := os.Remove(path)
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to delete file: %v", err)), nil
	}

	return &mcpTypes.CallToolResult{
		Content: []mcpTypes.Content{
			mcpTypes.TextContent{
				Type: "text",
				Text: fmt.Sprintf("Successfully deleted: %s", path),
			},
		},
	}, nil
}

func errorResult(message string) *mcpTypes.CallToolResult {
	return &mcpTypes.CallToolResult{
		IsError: true,
		Content: []mcpTypes.Content{
			mcpTypes.TextContent{
				Type: "text",
				Text: message,
			},
		},
	}
}