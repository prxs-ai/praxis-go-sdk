package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sirupsen/logrus"
)

// MCPClient implements the Client interface
type MCPClient struct {
	host    host.Host
	bridge  Bridge
	logger  *logrus.Logger
	timeouts struct {
		request  time.Duration
		connect  time.Duration
	}
}

// NewMCPClient creates a new MCP client
func NewMCPClient(host host.Host, bridge Bridge, logger *logrus.Logger) *MCPClient {
	client := &MCPClient{
		host:   host,
		bridge: bridge,
		logger: logger,
	}
	
	// Set default timeouts
	client.timeouts.request = 30 * time.Second
	client.timeouts.connect = 10 * time.Second
	
	return client
}

// InvokeTool invokes a tool on a remote MCP server
func (c *MCPClient) InvokeTool(ctx interface{}, peerID peer.ID, serverName, toolName string, params map[string]interface{}) (interface{}, error) {
	c.logger.Infof("Invoking tool %s on server %s of peer %s", toolName, serverName, peerID)
	
	// Create request
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	request := NewMCPToolCallRequest(requestID, serverName, toolName, params)
	
	// Convert context
	var ctxTimeout context.Context
	var cancel context.CancelFunc
	
	switch ctx := ctx.(type) {
	case context.Context:
		// Use provided context
		ctxTimeout, cancel = context.WithTimeout(ctx, c.timeouts.request)
	default:
		// Create new context with timeout
		ctxTimeout, cancel = context.WithTimeout(context.Background(), c.timeouts.request)
	}
	defer cancel()
	
	// Serialize request
	requestData, err := request.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Open stream
	stream, err := c.host.NewStream(ctxTimeout, peerID, MCPToolsCallProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()
	
	// Send request
	_, err = stream.Write(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	
	// Read response
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse response
	response, err := UnmarshalMCPResponse(responseData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// Check for error
	if response.Error != nil {
		return nil, response.Error
	}
	
	return response.Result, nil
}

// ListRemoteTools lists tools available on a remote peer
func (c *MCPClient) ListRemoteTools(ctx interface{}, peerID peer.ID) (map[string][]MCPTool, error) {
	c.logger.Infof("Listing tools on peer %s", peerID)
	
	// Create request
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	request := NewMCPRequest(requestID, MCPMethodListTools, "*", map[string]interface{}{})
	
	// Convert context
	var ctxTimeout context.Context
	var cancel context.CancelFunc
	
	switch ctx := ctx.(type) {
	case context.Context:
		ctxTimeout, cancel = context.WithTimeout(ctx, c.timeouts.request)
	default:
		ctxTimeout, cancel = context.WithTimeout(context.Background(), c.timeouts.request)
	}
	defer cancel()
	
	// Serialize request
	requestData, err := request.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Open stream
	stream, err := c.host.NewStream(ctxTimeout, peerID, MCPToolsListProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()
	
	// Send request
	_, err = stream.Write(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	
	// Read response
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse response
	response, err := UnmarshalMCPResponse(responseData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// Check for error
	if response.Error != nil {
		return nil, response.Error
	}
	
	// Convert result to tools map
	toolsMap := make(map[string][]MCPTool)
	
	rawMap, ok := response.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	
	for serverName, rawTools := range rawMap {
		toolsList, ok := rawTools.([]interface{})
		if !ok {
			c.logger.Warnf("Invalid tools format for server %s", serverName)
			continue
		}
		
		tools := make([]MCPTool, 0, len(toolsList))
		
		for _, rawTool := range toolsList {
			toolMap, ok := rawTool.(map[string]interface{})
			if !ok {
				c.logger.Warn("Invalid tool format")
				continue
			}
			
			// Convert to MCPTool using json marshaling/unmarshaling as a quick way
			// to convert between map[string]interface{} and struct
			toolJSON, err := json.Marshal(toolMap)
			if err != nil {
				c.logger.Warnf("Failed to marshal tool: %v", err)
				continue
			}
			
			var tool MCPTool
			if err := json.Unmarshal(toolJSON, &tool); err != nil {
				c.logger.Warnf("Failed to unmarshal tool: %v", err)
				continue
			}
			
			tools = append(tools, tool)
		}
		
		toolsMap[serverName] = tools
	}
	
	return toolsMap, nil
}

// ListRemoteResources lists resources available on a remote peer
func (c *MCPClient) ListRemoteResources(ctx interface{}, peerID peer.ID) (map[string][]MCPResource, error) {
	c.logger.Infof("Listing resources on peer %s", peerID)
	
	// Create request
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	request := NewMCPRequest(requestID, MCPMethodListResources, "*", map[string]interface{}{})
	
	// Convert context
	var ctxTimeout context.Context
	var cancel context.CancelFunc
	
	switch ctx := ctx.(type) {
	case context.Context:
		ctxTimeout, cancel = context.WithTimeout(ctx, c.timeouts.request)
	default:
		ctxTimeout, cancel = context.WithTimeout(context.Background(), c.timeouts.request)
	}
	defer cancel()
	
	// Serialize request
	requestData, err := request.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Open stream
	stream, err := c.host.NewStream(ctxTimeout, peerID, MCPResourcesListProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()
	
	// Send request
	_, err = stream.Write(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	
	// Read response
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse response
	response, err := UnmarshalMCPResponse(responseData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// Check for error
	if response.Error != nil {
		return nil, response.Error
	}
	
	// Convert result to resources map
	resourcesMap := make(map[string][]MCPResource)
	
	rawMap, ok := response.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}
	
	for serverName, rawResources := range rawMap {
		resourcesList, ok := rawResources.([]interface{})
		if !ok {
			c.logger.Warnf("Invalid resources format for server %s", serverName)
			continue
		}
		
		resources := make([]MCPResource, 0, len(resourcesList))
		
		for _, rawResource := range resourcesList {
			resourceMap, ok := rawResource.(map[string]interface{})
			if !ok {
				c.logger.Warn("Invalid resource format")
				continue
			}
			
			// Convert to MCPResource
			resourceJSON, err := json.Marshal(resourceMap)
			if err != nil {
				c.logger.Warnf("Failed to marshal resource: %v", err)
				continue
			}
			
			var resource MCPResource
			if err := json.Unmarshal(resourceJSON, &resource); err != nil {
				c.logger.Warnf("Failed to unmarshal resource: %v", err)
				continue
			}
			
			resources = append(resources, resource)
		}
		
		resourcesMap[serverName] = resources
	}
	
	return resourcesMap, nil
}

// ReadRemoteResource reads a resource from a remote peer
func (c *MCPClient) ReadRemoteResource(ctx interface{}, peerID peer.ID, serverName, resourceURI string) ([]byte, error) {
	c.logger.Infof("Reading resource %s from server %s of peer %s", resourceURI, serverName, peerID)
	
	// Create request
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	request := NewMCPRequest(requestID, MCPMethodReadResource, serverName, map[string]interface{}{
		"uri": resourceURI,
	})
	
	// Convert context
	var ctxTimeout context.Context
	var cancel context.CancelFunc
	
	switch ctx := ctx.(type) {
	case context.Context:
		ctxTimeout, cancel = context.WithTimeout(ctx, c.timeouts.request)
	default:
		ctxTimeout, cancel = context.WithTimeout(context.Background(), c.timeouts.request)
	}
	defer cancel()
	
	// Serialize request
	requestData, err := request.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	// Open stream
	stream, err := c.host.NewStream(ctxTimeout, peerID, MCPResourcesReadProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()
	
	// Send request
	_, err = stream.Write(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	
	// Read response
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	// Parse response
	response, err := UnmarshalMCPResponse(responseData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	// Check for error
	if response.Error != nil {
		return nil, response.Error
	}
	
	// Extract resource data
	switch data := response.Result.(type) {
	case string:
		return []byte(data), nil
	case []byte:
		return data, nil
	default:
		// Try to marshal to JSON and return as bytes
		jsonData, err := json.Marshal(response.Result)
		if err != nil {
			return nil, fmt.Errorf("unsupported resource data format")
		}
		return jsonData, nil
	}
}