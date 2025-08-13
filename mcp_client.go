package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sirupsen/logrus"
)

type MCPClient struct {
	host           host.Host
	protocolHandler *MCPProtocolHandler
	logger         *logrus.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	requestCounter uint64
	mu             sync.Mutex
}

func NewMCPClient(host host.Host, protocolHandler *MCPProtocolHandler, logger *logrus.Logger) *MCPClient {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &MCPClient{
		host:            host,
		protocolHandler: protocolHandler,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		requestCounter:  0,
	}
}

func (c *MCPClient) InvokeTool(ctx context.Context, peerID peer.ID, serverName, toolName string, params map[string]interface{}) (*MCPResponse, error) {
	c.logger.Infof("🔧 [MCP Client] Invoking tool '%s' on server '%s' at peer %s", toolName, serverName, peerID)
	
	requestID := c.generateRequestID()
	
	request := NewMCPToolCallRequest(requestID, serverName, toolName, params)
	
	if deadline, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	} else {
		request.Timeout = time.Until(deadline)
	}
	
	if err := request.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	
	response, err := c.protocolHandler.SendMCPRequest(ctx, peerID, request)
	if err != nil {
		c.logger.Errorf("❌ [MCP Client] Failed to invoke tool '%s': %v", toolName, err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	
	c.logger.Infof("✅ [MCP Client] Successfully invoked tool '%s' on peer %s", toolName, peerID)
	return response, nil
}

func (c *MCPClient) ListRemoteTools(ctx context.Context, peerID peer.ID) ([]MCPTool, error) {
	c.logger.Infof("📋 [MCP Client] Listing tools from peer %s", peerID)
	
	requestID := c.generateRequestID()
	request := NewMCPRequest(requestID, MCPMethodListTools, "all", map[string]interface{}{})
	
	if deadline, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	} else {
		request.Timeout = time.Until(deadline)
	}
	
	response, err := c.protocolHandler.SendMCPRequest(ctx, peerID, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	
	if response.Error != nil {
		return nil, &MCPError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
			Data:    response.Error.Data,
		}
	}
	
	var tools []MCPTool
	if toolsData, ok := response.Result.(map[string]interface{}); ok {
		if toolsList, ok := toolsData["tools"].([]interface{}); ok {
			for _, toolData := range toolsList {
				if toolMap, ok := toolData.(map[string]interface{}); ok {
					tool := MCPTool{
						Name:        getStringField(toolMap, "name"),
						Description: getStringField(toolMap, "description"),
					}
					if inputSchema, ok := toolMap["input_schema"].(map[string]interface{}); ok {
						tool.InputSchema = inputSchema
					}
					if outputSchema, ok := toolMap["output_schema"].(map[string]interface{}); ok {
						tool.OutputSchema = outputSchema
					}
					tools = append(tools, tool)
				}
			}
		}
	}
	
	c.logger.Infof("✅ [MCP Client] Found %d tools on peer %s", len(tools), peerID)
	return tools, nil
}

func (c *MCPClient) ListRemoteResources(ctx context.Context, peerID peer.ID) ([]MCPResource, error) {
	c.logger.Infof("📋 [MCP Client] Listing resources from peer %s", peerID)
	
	requestID := c.generateRequestID()
	request := NewMCPRequest(requestID, MCPMethodListResources, "all", map[string]interface{}{})
	
	if deadline, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	} else {
		request.Timeout = time.Until(deadline)
	}
	
	response, err := c.protocolHandler.SendMCPRequest(ctx, peerID, request)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}
	
	if response.Error != nil {
		return nil, &MCPError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
			Data:    response.Error.Data,
		}
	}
	
	var resources []MCPResource
	if resourcesData, ok := response.Result.(map[string]interface{}); ok {
		if resourcesList, ok := resourcesData["resources"].([]interface{}); ok {
			for _, resourceData := range resourcesList {
				if resourceMap, ok := resourceData.(map[string]interface{}); ok {
					resource := MCPResource{
						URI:         getStringField(resourceMap, "uri"),
						Name:        getStringField(resourceMap, "name"),
						Description: getStringField(resourceMap, "description"),
						MimeType:    getStringField(resourceMap, "mime_type"),
					}
					resources = append(resources, resource)
				}
			}
		}
	}
	
	c.logger.Infof("✅ [MCP Client] Found %d resources on peer %s", len(resources), peerID)
	return resources, nil
}

func (c *MCPClient) ReadResource(ctx context.Context, peerID peer.ID, uri string) (interface{}, error) {
	c.logger.Infof("📖 [MCP Client] Reading resource '%s' from peer %s", uri, peerID)
	
	requestID := c.generateRequestID()
	request := NewMCPRequest(requestID, MCPMethodReadResource, "all", map[string]interface{}{
		"uri": uri,
	})
	
	if deadline, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	} else {
		request.Timeout = time.Until(deadline)
	}
	
	response, err := c.protocolHandler.SendMCPRequest(ctx, peerID, request)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource: %w", err)
	}
	
	if response.Error != nil {
		return nil, &MCPError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
			Data:    response.Error.Data,
		}
	}
	
	c.logger.Infof("✅ [MCP Client] Successfully read resource '%s' from peer %s", uri, peerID)
	return response.Result, nil
}

func (c *MCPClient) PingPeer(ctx context.Context, peerID peer.ID) (time.Duration, error) {
	c.logger.Infof("🏓 [MCP Client] Pinging peer %s", peerID)
	
	start := time.Now()
	
	requestID := c.generateRequestID()
	request := NewMCPRequest(requestID, MCPMethodPing, "system", map[string]interface{}{
		"timestamp": start.Unix(),
	})
	
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request.Timeout = 5 * time.Second
	
	response, err := c.protocolHandler.SendMCPRequest(pingCtx, peerID, request)
	if err != nil {
		return 0, fmt.Errorf("ping failed: %w", err)
	}
	
	duration := time.Since(start)
	
	if response.Error != nil {
		return duration, &MCPError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
			Data:    response.Error.Data,
		}
	}
	
	c.logger.Infof("✅ [MCP Client] Ping to peer %s successful: %v", peerID, duration)
	return duration, nil
}

func (c *MCPClient) GetRemoteCapabilities(ctx context.Context, peerID peer.ID) ([]MCPCapability, error) {
	c.logger.Infof("🔍 [MCP Client] Getting MCP capabilities from peer %s", peerID)
	
	tools, err := c.ListRemoteTools(ctx, peerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tools: %w", err)
	}
	
	resources, err := c.ListRemoteResources(ctx, peerID)
	if err != nil {
		c.logger.Warnf("⚠️ [MCP Client] Failed to get resources from peer %s: %v", peerID, err)
		resources = []MCPResource{}
	}
	
	capability := MCPCapability{
		ServerName: fmt.Sprintf("remote-%s", peerID.String()[:8]),
		Transport:  "libp2p",
		Tools:      tools,
		Resources:  resources,
		Status:     "active",
		LastSeen:   time.Now(),
	}
	
	c.logger.Infof("✅ [MCP Client] Got capabilities from peer %s: %d tools, %d resources", 
		peerID, len(tools), len(resources))
	
	return []MCPCapability{capability}, nil
}

func (c *MCPClient) BatchInvokeTools(ctx context.Context, requests []MCPToolRequest) ([]MCPToolResult, error) {
	c.logger.Infof("🔧 [MCP Client] Batch invoking %d tools", len(requests))
	
	results := make([]MCPToolResult, len(requests))
	var wg sync.WaitGroup
	
	for i, req := range requests {
		wg.Add(1)
		go func(index int, request MCPToolRequest) {
			defer wg.Done()
			
			response, err := c.InvokeTool(ctx, request.PeerID, request.ServerName, request.ToolName, request.Params)
			results[index] = MCPToolResult{
				Index:    index,
				Response: response,
				Error:    err,
			}
		}(i, req)
	}
	
	wg.Wait()
	
	c.logger.Infof("✅ [MCP Client] Completed batch invocation of %d tools", len(requests))
	return results, nil
}

func (c *MCPClient) generateRequestID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.requestCounter++
	return fmt.Sprintf("req-%d-%d", time.Now().Unix(), c.requestCounter)
}

func (c *MCPClient) Shutdown() error {
	c.logger.Info("🔄 [MCP Client] Shutting down MCP client...")
	
	c.cancel()
	
	c.logger.Info("✅ [MCP Client] MCP client shutdown complete")
	return nil
}

type MCPToolRequest struct {
	PeerID     peer.ID
	ServerName string
	ToolName   string
	Params     map[string]interface{}
}

type MCPToolResult struct {
	Index    int
	Response *MCPResponse
	Error    error
}

func getStringField(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func (c *MCPClient) IsHealthy() bool {
	return c.protocolHandler != nil && c.protocolHandler.IsHealthy()
}

func (c *MCPClient) GetStats() map[string]interface{} {
	c.mu.Lock()
	requestCount := c.requestCounter
	c.mu.Unlock()
	
	return map[string]interface{}{
		"total_requests": requestCount,
		"healthy":        c.IsHealthy(),
		"peer_id":        c.host.ID().String(),
	}
}