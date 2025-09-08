package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
)

type FileSystemServer struct {
	allowedDirs []string
}

func NewFileSystemServer(dirs []string) *FileSystemServer {
	return &FileSystemServer{
		allowedDirs: dirs,
	}
}

func (s *FileSystemServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("Received request: method=%s", method)

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
					"name":    "MCP Filesystem Server",
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
						"name":        "read_text_file",
						"description": "Read complete contents of a file as text",
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
						"description": "Create new file or overwrite existing",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "File location",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "File content",
								},
							},
							"required": []string{"path", "content"},
						},
					},
					{
						"name":        "list_directory",
						"description": "List directory contents with [FILE] or [DIR] prefixes",
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
					{
						"name":        "create_directory",
						"description": "Create new directory or ensure it exists",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Directory path to create",
								},
							},
							"required": []string{"path"},
						},
					},
					{
						"name":        "search_files",
						"description": "Recursively search for files/directories matching pattern",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Starting directory",
								},
								"pattern": map[string]interface{}{
									"type":        "string",
									"description": "Search pattern",
								},
							},
							"required": []string{"path", "pattern"},
						},
					},
					{
						"name":        "get_file_info",
						"description": "Get detailed file/directory metadata",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "Path to get info for",
								},
							},
							"required": []string{"path"},
						},
					},
					{
						"name":        "list_allowed_directories",
						"description": "List all directories the server is allowed to access",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			},
		}

	case "tools/call":
		// Extract tool name and arguments
		params, _ := request["params"].(map[string]interface{})
		toolName, _ := params["name"].(string)
		arguments, _ := params["arguments"].(map[string]interface{})

		log.Printf("Calling tool: %s with args: %v", toolName, arguments)

		var result interface{}
		var toolErr error

		switch toolName {
		case "read_text_file":
			path, _ := arguments["path"].(string)
			result, toolErr = s.readFile(path)

		case "write_file":
			path, _ := arguments["path"].(string)
			content, _ := arguments["content"].(string)
			result, toolErr = s.writeFile(path, content)

		case "list_directory":
			path, _ := arguments["path"].(string)
			result, toolErr = s.listDirectory(path)

		case "create_directory":
			path, _ := arguments["path"].(string)
			result, toolErr = s.createDirectory(path)

		case "search_files":
			path, _ := arguments["path"].(string)
			pattern, _ := arguments["pattern"].(string)
			result, toolErr = s.searchFiles(path, pattern)

		case "get_file_info":
			path, _ := arguments["path"].(string)
			result, toolErr = s.getFileInfo(path)

		case "list_allowed_directories":
			result = s.allowedDirs
			toolErr = nil

		default:
			toolErr = fmt.Errorf("unknown tool: %s", toolName)
		}

		if toolErr != nil {
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"isError": true,
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("Error: %v", toolErr),
						},
					},
				},
			}
		} else {
			response = map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{
						{
							"type": "text",
							"text": fmt.Sprintf("%v", result),
						},
					},
				},
			}
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
				"message": fmt.Sprintf("Method not found: %s", method),
			},
		}
	}

	// Send SSE response
	responseData, _ := json.Marshal(response)
	fmt.Fprintf(w, "data: %s\n\n", responseData)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *FileSystemServer) isPathAllowed(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, dir := range s.allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, absDir) {
			return true
		}
	}
	return false
}

func (s *FileSystemServer) readFile(path string) (string, error) {
	if !s.isPathAllowed(path) {
		return "", fmt.Errorf("access denied: path not in allowed directories")
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *FileSystemServer) writeFile(path string, content string) (string, error) {
	if !s.isPathAllowed(path) {
		return "", fmt.Errorf("access denied: path not in allowed directories")
	}

	err := ioutil.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path), nil
}

func (s *FileSystemServer) listDirectory(path string) ([]string, error) {
	if !s.isPathAllowed(path) {
		return nil, fmt.Errorf("access denied: path not in allowed directories")
	}

	entries, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, entry := range entries {
		prefix := "[FILE]"
		if entry.IsDir() {
			prefix = "[DIR]"
		}
		result = append(result, fmt.Sprintf("%s %s", prefix, entry.Name()))
	}
	return result, nil
}

func (s *FileSystemServer) createDirectory(path string) (string, error) {
	if !s.isPathAllowed(path) {
		return "", fmt.Errorf("access denied: path not in allowed directories")
	}

	err := os.MkdirAll(path, 0755)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created directory: %s", path), nil
}

func (s *FileSystemServer) searchFiles(path string, pattern string) ([]string, error) {
	if !s.isPathAllowed(path) {
		return nil, fmt.Errorf("access denied: path not in allowed directories")
	}

	var matches []string
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		matched, _ := filepath.Match(pattern, filepath.Base(p))
		if matched || strings.Contains(p, pattern) {
			matches = append(matches, p)
		}
		return nil
	})
	
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func (s *FileSystemServer) getFileInfo(path string) (map[string]interface{}, error) {
	if !s.isPathAllowed(path) {
		return nil, fmt.Errorf("access denied: path not in allowed directories")
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"name":     info.Name(),
		"size":     info.Size(),
		"mode":     info.Mode().String(),
		"modified": info.ModTime().Format("2006-01-02 15:04:05"),
		"isDir":    info.IsDir(),
	}, nil
}

func main() {
	// Get allowed directories from command line args or use defaults
	allowedDirs := os.Args[1:]
	if len(allowedDirs) == 0 {
		// Default to current directory and shared directory
		allowedDirs = []string{".", "./shared", "./configs"}
	}

	log.Printf("Starting MCP Filesystem Server with access to: %v", allowedDirs)
	
	server := NewFileSystemServer(allowedDirs)
	
	http.HandleFunc("/mcp", server.ServeHTTP)
	
	log.Println("MCP Filesystem Server listening on http://localhost:3000/mcp")
	if err := http.ListenAndServe(":3000", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}