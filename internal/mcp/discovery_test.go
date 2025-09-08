package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDiscoveryService_DiscoverToolsFromServer(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create mock MCP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		// Mock response based on request
		var request map[string]interface{}
		json.NewDecoder(r.Body).Decode(&request)
		
		if method, ok := request["method"].(string); ok {
			switch method {
			case "initialize":
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      request["id"],
					"result": map[string]interface{}{
						"protocolVersion": mcpTypes.LATEST_PROTOCOL_VERSION,
						"capabilities": map[string]interface{}{},
						"serverInfo": map[string]interface{}{
							"name":    "Test MCP Server",
							"version": "1.0.0",
						},
					},
				}
				json.NewEncoder(w).Encode(response)
				
			case "tools/list":
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      request["id"],
					"result": map[string]interface{}{
						"tools": []map[string]interface{}{
							{
								"name":        "test_tool_1",
								"description": "Test tool 1 description",
								"inputSchema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"param1": map[string]string{
											"type":        "string",
											"description": "Parameter 1",
										},
									},
								},
							},
							{
								"name":        "test_tool_2",
								"description": "Test tool 2 description",
								"inputSchema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"param2": map[string]string{
											"type":        "number",
											"description": "Parameter 2",
										},
									},
								},
							},
						},
					},
				}
				json.NewEncoder(w).Encode(response)
				
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer server.Close()

	// Create discovery service
	service := NewToolDiscoveryService(logger)

	// Test discovery
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := service.DiscoverToolsFromServer(ctx, server.URL+"/mcp")
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	// Verify first tool
	assert.Equal(t, "test_tool_1", tools[0].Name)
	assert.Equal(t, "Test tool 1 description", tools[0].Description)
	assert.Equal(t, "Test MCP Server", tools[0].ServerName)
	assert.Equal(t, server.URL+"/mcp", tools[0].ServerURL)

	// Verify second tool
	assert.Equal(t, "test_tool_2", tools[1].Name)
	assert.Equal(t, "Test tool 2 description", tools[1].Description)
}

func TestToolDiscoveryService_DiscoverToolsFromMultipleServers(t *testing.T) {
	logger := logrus.New()
	service := NewToolDiscoveryService(logger)

	// Create two mock servers
	server1 := createMockMCPServer(t, "Server1", []string{"tool1", "tool2"})
	defer server1.Close()

	server2 := createMockMCPServer(t, "Server2", []string{"tool3", "tool4"})
	defer server2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Discover from multiple servers
	serverURLs := []string{
		server1.URL + "/mcp",
		server2.URL + "/mcp",
	}

	results := service.DiscoverToolsFromMultipleServers(ctx, serverURLs)
	
	assert.Len(t, results, 2)
	assert.Len(t, results[server1.URL+"/mcp"], 2)
	assert.Len(t, results[server2.URL+"/mcp"], 2)
}

func createMockMCPServer(t *testing.T, name string, toolNames []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		var request map[string]interface{}
		json.NewDecoder(r.Body).Decode(&request)
		
		if method, ok := request["method"].(string); ok {
			switch method {
			case "initialize":
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      request["id"],
					"result": map[string]interface{}{
						"protocolVersion": mcpTypes.LATEST_PROTOCOL_VERSION,
						"capabilities": map[string]interface{}{},
						"serverInfo": map[string]interface{}{
							"name":    name,
							"version": "1.0.0",
						},
					},
				}
				json.NewEncoder(w).Encode(response)
				
			case "tools/list":
				tools := []map[string]interface{}{}
				for _, toolName := range toolNames {
					tools = append(tools, map[string]interface{}{
						"name":        toolName,
						"description": toolName + " description",
						"inputSchema": map[string]interface{}{
							"type": "object",
						},
					})
				}
				
				response := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      request["id"],
					"result": map[string]interface{}{
						"tools": tools,
					},
				}
				json.NewEncoder(w).Encode(response)
			}
		}
	}))
}