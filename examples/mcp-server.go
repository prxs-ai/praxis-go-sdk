package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

type MCPServer struct {
	name    string
	version string
	tools   []Tool
	resources []Resource
}

func NewMCPServer() *MCPServer {
	serverName := getEnv("MCP_SERVER_NAME", "context-server")
	
	return &MCPServer{
		name:    serverName,
		version: "1.0.0",
		tools: []Tool{
			{
				Name:        "greet",
				Description: "Generate a greeting message",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type": "string",
							"description": "Name to greet",
						},
						"style": map[string]interface{}{
							"type": "string",
							"enum": []string{"formal", "casual", "enthusiastic"},
							"description": "Greeting style",
							"default": "casual",
						},
					},
					"required": []string{"name"},
				},
			},
			{
				Name:        "calculate",
				Description: "Perform basic mathematical calculations",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"operation": map[string]interface{}{
							"type": "string",
							"enum": []string{"add", "subtract", "multiply", "divide"},
							"description": "Mathematical operation to perform",
						},
						"a": map[string]interface{}{
							"type": "number",
							"description": "First number",
						},
						"b": map[string]interface{}{
							"type": "number",
							"description": "Second number",
						},
					},
					"required": []string{"operation", "a", "b"},
				},
			},
			{
				Name:        "get_time",
				Description: "Get the current server time",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"format": map[string]interface{}{
							"type": "string",
							"enum": []string{"iso", "unix", "human"},
							"default": "iso",
							"description": "Time format to return",
						},
					},
				},
			},
			{
				Name:        "context_search",
				Description: "Search through available context data",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type": "string",
							"description": "Search query",
						},
						"limit": map[string]interface{}{
							"type": "integer",
							"default": 10,
							"description": "Maximum number of results",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		resources: []Resource{
			{
				URI:         "context://server-info",
				Name:        "Server Information",
				Description: "Information about this MCP server",
				MimeType:    "application/json",
			},
			{
				URI:         "context://available-tools",
				Name:        "Available Tools",
				Description: "List of all available tools",
				MimeType:    "application/json",
			},
		},
	}
}

func (s *MCPServer) handleRequest(req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleToolCall(req)
	case "resources/list":
		return s.handleListResources(req)
	case "resources/read":
		return s.handleReadResource(req)
	case "ping":
		return s.handlePing(req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

func (s *MCPServer) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{"listChanged": true},
			"resources": map[string]interface{}{"listChanged": true},
		},
		"serverInfo": map[string]interface{}{
			"name":    s.name,
			"version": s.version,
		},
	}
	
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *MCPServer) handleListTools(req *JSONRPCRequest) *JSONRPCResponse {
	result := map[string]interface{}{
		"tools": s.tools,
	}
	
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *MCPServer) handleToolCall(req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}
	
	toolName, ok := params["name"].(string)
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Missing tool name",
			},
		}
	}
	
	arguments, ok := params["arguments"].(map[string]interface{})
	if !ok {
		arguments = make(map[string]interface{})
	}
	
	result := s.executeTool(toolName, arguments)
	
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *MCPServer) executeTool(toolName string, args map[string]interface{}) interface{} {
	switch toolName {
	case "greet":
		return s.executeGreet(args)
	case "calculate":
		return s.executeCalculate(args)
	case "get_time":
		return s.executeGetTime(args)
	case "context_search":
		return s.executeContextSearch(args)
	default:
		return map[string]interface{}{
			"error": fmt.Sprintf("Unknown tool: %s", toolName),
		}
	}
}

func (s *MCPServer) executeGreet(args map[string]interface{}) interface{} {
	name, ok := args["name"].(string)
	if !ok {
		return map[string]interface{}{"error": "Name is required"}
	}
	
	style, ok := args["style"].(string)
	if !ok {
		style = "casual"
	}
	
	var greeting string
	switch style {
	case "formal":
		greeting = fmt.Sprintf("Good day, %s. It is a pleasure to make your acquaintance.", name)
	case "enthusiastic":
		greeting = fmt.Sprintf("Hey there, %s! It's absolutely fantastic to meet you!", name)
	default:
		greeting = fmt.Sprintf("Hello, %s! Nice to meet you.", name)
	}
	
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": greeting,
			},
		},
		"isError": false,
	}
}

func (s *MCPServer) executeCalculate(args map[string]interface{}) interface{} {
	operation, ok := args["operation"].(string)
	if !ok {
		return map[string]interface{}{"error": "Operation is required"}
	}
	
	a, ok := args["a"].(float64)
	if !ok {
		return map[string]interface{}{"error": "First number (a) is required"}
	}
	
	b, ok := args["b"].(float64)
	if !ok {
		return map[string]interface{}{"error": "Second number (b) is required"}
	}
	
	var result float64
	var opSymbol string
	
	switch operation {
	case "add":
		result = a + b
		opSymbol = "+"
	case "subtract":
		result = a - b
		opSymbol = "-"
	case "multiply":
		result = a * b
		opSymbol = "*"
	case "divide":
		if b == 0 {
			return map[string]interface{}{"error": "Cannot divide by zero"}
		}
		result = a / b
		opSymbol = "/"
	default:
		return map[string]interface{}{"error": "Unknown operation"}
	}
	
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%.2f %s %.2f = %.2f", a, opSymbol, b, result),
			},
		},
		"isError": false,
		"result":  result,
	}
}

func (s *MCPServer) executeGetTime(args map[string]interface{}) interface{} {
	format, ok := args["format"].(string)
	if !ok {
		format = "iso"
	}
	
	now := time.Now()
	var timeStr string
	
	switch format {
	case "unix":
		timeStr = fmt.Sprintf("%d", now.Unix())
	case "human":
		timeStr = now.Format("January 2, 2006 at 3:04 PM MST")
	default:
		timeStr = now.Format(time.RFC3339)
	}
	
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Current server time (%s): %s", format, timeStr),
			},
		},
		"isError": false,
		"timestamp": now.Unix(),
		"format": format,
	}
}

func (s *MCPServer) executeContextSearch(args map[string]interface{}) interface{} {
	query, ok := args["query"].(string)
	if !ok {
		return map[string]interface{}{"error": "Query is required"}
	}
	
	limit, ok := args["limit"].(float64)
	if !ok {
		limit = 10
	}
	
	results := []map[string]interface{}{
		{
			"id":    "ctx-1",
			"title": fmt.Sprintf("Context result for: %s", query),
			"content": fmt.Sprintf("This is a simulated context search result for the query '%s'. In a real implementation, this would search through available context data.", query),
			"relevance": 0.95,
			"source": s.name,
		},
	}
	
	if strings.Contains(strings.ToLower(query), "libp2p") {
		results = append(results, map[string]interface{}{
			"id":    "ctx-libp2p",
			"title": "libp2p Information",
			"content": "libp2p is a modular system of protocols, specifications and libraries that enable the development of peer-to-peer network applications.",
			"relevance": 0.90,
			"source": s.name,
		})
	}
	
	if strings.Contains(strings.ToLower(query), "mcp") {
		results = append(results, map[string]interface{}{
			"id":    "ctx-mcp",
			"title": "Model Context Protocol Information",
			"content": "MCP (Model Context Protocol) enables AI assistants to connect with external data sources and tools in a standardized way.",
			"relevance": 0.88,
			"source": s.name,
		})
	}
	
	if int(limit) < len(results) {
		results = results[:int(limit)]
	}
	
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Found %d results for query: %s", len(results), query),
			},
		},
		"isError": false,
		"results": results,
		"query": query,
		"total": len(results),
	}
}

func (s *MCPServer) handleListResources(req *JSONRPCRequest) *JSONRPCResponse {
	result := map[string]interface{}{
		"resources": s.resources,
	}
	
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *MCPServer) handleReadResource(req *JSONRPCRequest) *JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Invalid params",
			},
		}
	}
	
	uri, ok := params["uri"].(string)
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Missing resource URI",
			},
		}
	}
	
	var contents []map[string]interface{}
	
	switch uri {
	case "context://server-info":
		contents = []map[string]interface{}{
			{
				"uri":      uri,
				"mimeType": "application/json",
				"text": fmt.Sprintf(`{"name":"%s","version":"%s","tools":%d,"resources":%d,"uptime":"%s"}`,
					s.name, s.version, len(s.tools), len(s.resources), time.Since(time.Now().Add(-time.Hour))),
			},
		}
	case "context://available-tools":
		toolsJSON, _ := json.Marshal(s.tools)
		contents = []map[string]interface{}{
			{
				"uri":      uri,
				"mimeType": "application/json",
				"text":     string(toolsJSON),
			},
		}
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32602,
				Message: "Resource not found",
			},
		}
	}
	
	result := map[string]interface{}{
		"contents": contents,
	}
	
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *MCPServer) handlePing(req *JSONRPCRequest) *JSONRPCResponse {
	result := map[string]interface{}{
		"pong":      true,
		"server":    s.name,
		"timestamp": time.Now().Unix(),
	}
	
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	server := NewMCPServer()
	scanner := bufio.NewScanner(os.Stdin)
	
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		
		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			errorResp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &RPCError{
					Code:    -32700,
					Message: "Parse error",
					Data:    err.Error(),
				},
			}
			respJSON, _ := json.Marshal(errorResp)
			fmt.Println(string(respJSON))
			continue
		}
		
		response := server.handleRequest(&req)
		respJSON, err := json.Marshal(response)
		if err != nil {
			errorResp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &RPCError{
					Code:    -32603,
					Message: "Internal error",
					Data:    err.Error(),
				},
			}
			respJSON, _ = json.Marshal(errorResp)
		}
		
		fmt.Println(string(respJSON))
	}
}