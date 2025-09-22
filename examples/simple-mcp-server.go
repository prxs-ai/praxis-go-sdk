//go:build examples

package main

import (
	"encoding/json"
	"fmt"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"io"
	"log"
	"net/http"
)

type SSETransport struct {
	server *server.MCPServer
}

func NewSSETransport(s *server.MCPServer) *SSETransport {
	return &SSETransport{server: s}
}

func (t *SSETransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse JSON-RPC request
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Handle different methods
	method, _ := request["method"].(string)
	id := request["id"]

	var response map[string]interface{}

	switch method {
	case "initialize":
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"protocolVersion": mcpTypes.LATEST_PROTOCOL_VERSION,
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "Simple MCP Filesystem Server",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "read_file",
						"description": "Read a file from filesystem",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "File path to read",
								},
							},
							"required": []string{"path"},
						},
					},
					{
						"name":        "write_file",
						"description": "Write content to a file",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "File path to write",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "Content to write",
								},
							},
							"required": []string{"path", "content"},
						},
					},
					{
						"name":        "list_files",
						"description": "List files in a directory",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Directory path",
								},
							},
							"required": []string{"path"},
						},
					},
				},
			},
		}

	case "initialized":
		// Just acknowledge
		w.WriteHeader(http.StatusOK)
		return

	default:
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": "Method not found",
			},
		}
	}

	// Send SSE response
	fmt.Fprintf(w, "data: %s\n\n", mustMarshal(response))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func mustMarshal(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func main() {
	// Create SSE transport
	mcpServer := server.NewMCPServer(
		"Simple MCP Server",
		"1.0.0",
	)

	transport := NewSSETransport(mcpServer)

	// Setup HTTP server
	http.HandleFunc("/mcp", transport.ServeHTTP)

	log.Println("Starting Simple MCP Server on http://localhost:3000/mcp")
	if err := http.ListenAndServe(":3000", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
