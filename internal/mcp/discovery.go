package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
)

// ToolDiscoveryService handles dynamic discovery of MCP tools
type ToolDiscoveryService struct {
	logger *logrus.Logger
}

// NewToolDiscoveryService creates a new discovery service
func NewToolDiscoveryService(logger *logrus.Logger) *ToolDiscoveryService {
	return &ToolDiscoveryService{
		logger: logger,
	}
}

// DiscoveredTool represents a discovered MCP tool
type DiscoveredTool struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	InputSchema mcpTypes.ToolInputSchema  `json:"input_schema"`
	ServerURL   string                    `json:"server_url"`
	ServerName  string                    `json:"server_name"`
}

// DiscoverToolsFromServer discovers tools from an MCP server using simple HTTP client
func (s *ToolDiscoveryService) DiscoverToolsFromServer(ctx context.Context, serverURL string) ([]DiscoveredTool, error) {
	s.logger.Infof("🔍 Discovering tools from MCP server: %s", serverURL)

	// Step 1: Initialize connection
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": mcpTypes.LATEST_PROTOCOL_VERSION,
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "Praxis MCP Discovery",
				"version": "1.0.0",
			},
		},
	}

	serverInfo, err := s.makeSSERequest(ctx, serverURL, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	// Extract server info from response
	result, ok := serverInfo["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid initialize response format")
	}
	
	info, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing server info in response")
	}

	serverName, _ := info["name"].(string)
	serverVersion, _ := info["version"].(string)
	s.logger.Infof("✅ Connected to MCP server: %s (version %s)", serverName, serverVersion)

	// Step 2: List available tools
	s.logger.Debug("Attempting to list tools from server")
	
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	toolsResponse, err := s.makeSSERequest(ctx, serverURL, toolsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Extract tools from response
	result, ok = toolsResponse["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid tools/list response format")
	}

	toolsArray, ok := result["tools"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid tools array in response")
	}

	// Convert to discovered tools
	var discoveredTools []DiscoveredTool
	for _, toolData := range toolsArray {
		toolMap, ok := toolData.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := toolMap["name"].(string)
		description, _ := toolMap["description"].(string)
		
		// Parse input schema
		var inputSchema mcpTypes.ToolInputSchema
		if schemaData, exists := toolMap["inputSchema"]; exists {
			// Convert to JSON and back to parse into proper structure
			schemaBytes, _ := json.Marshal(schemaData)
			json.Unmarshal(schemaBytes, &inputSchema)
		}

		discoveredTool := DiscoveredTool{
			Name:        name,
			Description: description,
			InputSchema: inputSchema,
			ServerURL:   serverURL,
			ServerName:  serverName,
		}
		discoveredTools = append(discoveredTools, discoveredTool)

		s.logger.Debugf("  📦 Found tool: %s - %s", name, description)
	}

	s.logger.Infof("📋 Discovered %d tools from %s", len(discoveredTools), serverName)
	return discoveredTools, nil
}

// DiscoverToolsFromMultipleServers discovers tools from multiple MCP servers
func (s *ToolDiscoveryService) DiscoverToolsFromMultipleServers(ctx context.Context, serverURLs []string) map[string][]DiscoveredTool {
	result := make(map[string][]DiscoveredTool)

	for _, url := range serverURLs {
		tools, err := s.DiscoverToolsFromServer(ctx, url)
		if err != nil {
			s.logger.Errorf("Failed to discover tools from %s: %v", url, err)
			continue
		}
		result[url] = tools
	}

	return result
}

// makeSSERequest makes an HTTP request and parses SSE response format
func (s *ToolDiscoveryService) makeSSERequest(ctx context.Context, serverURL string, request map[string]interface{}) (map[string]interface{}, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Marshal request to JSON
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse SSE format - expect "data: {json}\n\n"
	bodyStr := string(body)
	s.logger.Debugf("Raw SSE response: %s", bodyStr)

	// Extract JSON from SSE format
	if !strings.HasPrefix(bodyStr, "data: ") {
		return nil, fmt.Errorf("invalid SSE format: expected 'data: ' prefix")
	}

	// Find the JSON part
	jsonStart := strings.Index(bodyStr, "data: ") + 6
	jsonEnd := strings.Index(bodyStr[jsonStart:], "\n")
	if jsonEnd == -1 {
		jsonEnd = len(bodyStr) - jsonStart
	} else {
		jsonEnd += jsonStart
	}

	jsonStr := strings.TrimSpace(bodyStr[jsonStart:jsonEnd])
	s.logger.Debugf("Extracted JSON: %s", jsonStr)

	// Parse JSON
	var response map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &response); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Check for JSON-RPC error
	if errData, hasError := response["error"]; hasError {
		return nil, fmt.Errorf("MCP server error: %v", errData)
	}

	return response, nil
}