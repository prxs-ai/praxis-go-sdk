package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type MCPBridge struct {
	host            host.Host
	serverManager   *MCPServerManager
	protocolHandler *MCPProtocolHandler
	client          *MCPClient
	config          *MCPBridgeConfig
	logger          *logrus.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	
	activeRequests  map[string]context.CancelFunc
	requestCount    uint64
	mu              sync.RWMutex
	
	stats           MCPBridgeStats
	statsMu         sync.RWMutex
}

type MCPBridgeStats struct {
	TotalRequests     uint64    `json:"total_requests"`
	SuccessfulRequests uint64   `json:"successful_requests"`
	FailedRequests    uint64    `json:"failed_requests"`
	ActiveRequests    int       `json:"active_requests"`
	StartTime         time.Time `json:"start_time"`
	LastRequest       time.Time `json:"last_request"`
}

func NewMCPBridge(host host.Host, configPath string, logger *logrus.Logger) (*MCPBridge, error) {
	config, err := LoadMCPConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	bridge := &MCPBridge{
		host:           host,
		config:         config,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
		activeRequests: make(map[string]context.CancelFunc),
		stats: MCPBridgeStats{
			StartTime: time.Now(),
		},
	}
	
	bridge.serverManager = NewMCPServerManager(config, logger)
	
	bridge.protocolHandler = NewMCPProtocolHandler(host, bridge, logger)
	
	bridge.client = NewMCPClient(host, bridge.protocolHandler, logger)
	
	return bridge, nil
}

func LoadMCPConfig(configPath string) (*MCPBridgeConfig, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := &MCPBridgeConfig{
			Enabled:  true,
			Servers:  []MCPServerConfig{},
			Limits:   DefaultMCPLimits,
			LogLevel: "info",
		}
		return defaultConfig, nil
	}
	
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	var configWrapper struct {
		MCPBridge MCPBridgeConfig `yaml:"mcp_bridge"`
	}
	
	if err := yaml.Unmarshal(data, &configWrapper); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	
	config := configWrapper.MCPBridge
	
	if config.Limits.MaxConcurrentRequests == 0 {
		config.Limits = DefaultMCPLimits
	}
	
	for i := range config.Servers {
		if config.Servers[i].Timeout == 0 {
			config.Servers[i].Timeout = 30 * time.Second
		}
	}
	
	return &config, nil
}

func (b *MCPBridge) Start() error {
	b.logger.Info("🚀 [MCP Bridge] Starting MCP bridge...")
	
	if !b.config.Enabled {
		b.logger.Info("📴 [MCP Bridge] MCP bridge is disabled")
		return nil
	}
	
	if err := b.serverManager.Start(); err != nil {
		return fmt.Errorf("failed to start server manager: %w", err)
	}
	
	b.logger.Info("✅ [MCP Bridge] MCP bridge started successfully")
	return nil
}

func (b *MCPBridge) ProcessRequest(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	b.updateStats(true, false)
	
	b.logger.Infof("🔧 [MCP Bridge] Processing %s request for server '%s' (ID: %s)", 
		request.Method, request.ServerName, request.ID)
	
	if err := b.checkRateLimits(); err != nil {
		b.updateStats(false, true)
		return nil, err
	}
	
	reqCtx, cancel := context.WithCancel(ctx)
	b.addActiveRequest(request.ID, cancel)
	defer b.removeActiveRequest(request.ID)
	
	var response *MCPResponse
	var err error
	
	switch request.Method {
	case MCPMethodToolCall:
		response, err = b.handleToolCall(reqCtx, request)
	case MCPMethodListTools:
		response, err = b.handleListTools(reqCtx, request)
	case MCPMethodListResources:
		response, err = b.handleListResources(reqCtx, request)
	case MCPMethodReadResource:
		response, err = b.handleReadResource(reqCtx, request)
	case MCPMethodPing:
		response, err = b.handlePing(reqCtx, request)
	case MCPMethodInitialize:
		response, err = b.handleInitialize(reqCtx, request)
	default:
		err = &MCPError{
			Code:    MCPErrorMethodNotFound,
			Message: fmt.Sprintf("method not found: %s", request.Method),
		}
	}
	
	if err != nil {
		b.updateStats(false, true)
		return nil, err
	}
	
	b.updateStats(false, false)
	b.logger.Infof("✅ [MCP Bridge] Successfully processed request %s", request.ID)
	return response, nil
}

func (b *MCPBridge) handleToolCall(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	if request.ToolName == "" {
		return nil, &MCPError{
			Code:    MCPErrorInvalidParams,
			Message: "tool name is required for tool call",
		}
	}
	
	server, exists := b.serverManager.GetServer(request.ServerName)
	if !exists {
		return nil, &MCPError{
			Code:    MCPErrorNotFound,
			Message: fmt.Sprintf("server not found: %s", request.ServerName),
		}
	}
	
	server.mu.RLock()
	status := server.Status
	server.mu.RUnlock()
	
	if status != "running" {
		return nil, &MCPError{
			Code:    MCPErrorServerError,
			Message: fmt.Sprintf("server %s is not running (status: %s)", request.ServerName, status),
		}
	}
	
	var result interface{}
	var err error
	
	switch server.Config.Transport {
	case "stdio":
		result, err = b.executeStdioToolCall(ctx, server, request)
	case "sse":
		result, err = b.executeSSEToolCall(ctx, server, request)
	default:
		return nil, &MCPError{
			Code:    MCPErrorInternalError,
			Message: fmt.Sprintf("unsupported transport: %s", server.Config.Transport),
		}
	}
	
	if err != nil {
		return nil, err
	}
	
	return NewMCPResponse(request.ID, result), nil
}

func (b *MCPBridge) executeStdioToolCall(ctx context.Context, server *MCPServerInstance, request *MCPRequest) (interface{}, error) {
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      request.ID,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      request.ToolName,
			"arguments": request.Params,
		},
	}
	
	reqData, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, &MCPError{
			Code:    MCPErrorInternalError,
			Message: "failed to marshal tool request",
			Data:    err.Error(),
		}
	}
	
	
	cmd := exec.CommandContext(ctx, "echo", string(reqData))
	output, err := cmd.Output()
	if err != nil {
		return nil, &MCPError{
			Code:    MCPErrorServerError,
			Message: "tool execution failed",
			Data:    err.Error(),
		}
	}
	
	result := map[string]interface{}{
		"output": string(output),
		"tool":   request.ToolName,
		"server": request.ServerName,
	}
	
	return result, nil
}

func (b *MCPBridge) executeSSEToolCall(ctx context.Context, server *MCPServerInstance, request *MCPRequest) (interface{}, error) {
	if server.SSEServer == nil {
		return nil, &MCPError{
			Code:    MCPErrorServerError,
			Message: "SSE server not initialized",
			Data:    server.Config.Name,
		}
	}

	// Создаем JSON-RPC 2.0 запрос для SSE сервера
	sseRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      request.ID,
		"params": map[string]interface{}{
			"name":      request.ToolName,
			"arguments": request.Params,
		},
	}

	reqData, err := json.Marshal(sseRequest)
	if err != nil {
		return nil, &MCPError{
			Code:    MCPErrorInternalError,
			Message: "failed to marshal SSE request",
			Data:    err.Error(),
		}
	}

	// Отправляем запрос к SSE серверу
	sseURL := server.Config.URL + "/message"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", sseURL, bytes.NewBuffer(reqData))
	if err != nil {
		return nil, &MCPError{
			Code:    MCPErrorInternalError,
			Message: "failed to create HTTP request",
			Data:    err.Error(),
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &MCPError{
			Code:    MCPErrorServerError,
			Message: "SSE server request failed",
			Data:    err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &MCPError{
			Code:    MCPErrorServerError,
			Message: fmt.Sprintf("SSE server returned status %d", resp.StatusCode),
			Data:    nil,
		}
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &MCPError{
			Code:    MCPErrorInternalError,
			Message: "failed to read SSE response",
			Data:    err.Error(),
		}
	}

	var sseResponse map[string]interface{}
	if err := json.Unmarshal(respData, &sseResponse); err != nil {
		return nil, &MCPError{
			Code:    MCPErrorParseError,
			Message: "failed to parse SSE response",
			Data:    err.Error(),
		}
	}

	// Проверяем на JSON-RPC ошибки
	if errorData, exists := sseResponse["error"]; exists {
		return nil, &MCPError{
			Code:    MCPErrorServerError,
			Message: "SSE server returned error",
			Data:    errorData,
		}
	}

	// Возвращаем результат
	if result, exists := sseResponse["result"]; exists {
		return result, nil
	}

	return sseResponse, nil
}

func (b *MCPBridge) handleListTools(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	var tools []MCPTool
	
	if request.ServerName == "all" {
		tools = b.serverManager.GetAllTools()
	} else {
		serverTools, err := b.serverManager.GetServerTools(request.ServerName)
		if err != nil {
			return nil, &MCPError{
				Code:    MCPErrorNotFound,
				Message: err.Error(),
			}
		}
		tools = serverTools
	}
	
	result := map[string]interface{}{
		"tools": tools,
	}
	
	return NewMCPResponse(request.ID, result), nil
}

func (b *MCPBridge) handleListResources(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	var resources []MCPResource
	
	if request.ServerName == "all" {
		resources = b.serverManager.GetAllResources()
	} else {
		serverResources, err := b.serverManager.GetServerResources(request.ServerName)
		if err != nil {
			return nil, &MCPError{
				Code:    MCPErrorNotFound,
				Message: err.Error(),
			}
		}
		resources = serverResources
	}
	
	result := map[string]interface{}{
		"resources": resources,
	}
	
	return NewMCPResponse(request.ID, result), nil
}

func (b *MCPBridge) handleReadResource(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	uri, ok := request.Params["uri"].(string)
	if !ok {
		return nil, &MCPError{
			Code:    MCPErrorInvalidParams,
			Message: "uri parameter is required",
		}
	}
	
	
	result := map[string]interface{}{
		"uri":     uri,
		"content": "Resource content placeholder",
		"mime_type": "text/plain",
	}
	
	return NewMCPResponse(request.ID, result), nil
}

func (b *MCPBridge) handlePing(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	result := map[string]interface{}{
		"pong":      true,
		"timestamp": time.Now().Unix(),
		"server":    request.ServerName,
	}
	
	return NewMCPResponse(request.ID, result), nil
}

func (b *MCPBridge) handleInitialize(ctx context.Context, request *MCPRequest) (*MCPResponse, error) {
	result := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{"list": true, "call": true},
			"resources": map[string]interface{}{"list": true, "read": true},
		},
		"server_info": map[string]interface{}{
			"name":    "go-agent-mcp-bridge",
			"version": "1.0.0",
		},
	}
	
	return NewMCPResponse(request.ID, result), nil
}

func (b *MCPBridge) checkRateLimits() error {
	b.mu.RLock()
	activeCount := len(b.activeRequests)
	b.mu.RUnlock()
	
	if activeCount >= b.config.Limits.MaxConcurrentRequests {
		return &MCPError{
			Code:    MCPErrorServerError,
			Message: "too many concurrent requests",
			Data:    map[string]int{"current": activeCount, "limit": b.config.Limits.MaxConcurrentRequests},
		}
	}
	
	return nil
}

func (b *MCPBridge) addActiveRequest(id string, cancel context.CancelFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.activeRequests[id] = cancel
}

func (b *MCPBridge) removeActiveRequest(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.activeRequests, id)
}

func (b *MCPBridge) updateStats(isNewRequest, isError bool) {
	b.statsMu.Lock()
	defer b.statsMu.Unlock()
	
	if isNewRequest {
		b.stats.TotalRequests++
		b.stats.LastRequest = time.Now()
	} else if isError {
		b.stats.FailedRequests++
	} else {
		b.stats.SuccessfulRequests++
	}
	
	b.mu.RLock()
	b.stats.ActiveRequests = len(b.activeRequests)
	b.mu.RUnlock()
}

func (b *MCPBridge) ListAllTools() []MCPTool {
	return b.serverManager.GetAllTools()
}

func (b *MCPBridge) ListAllResources() []MCPResource {
	return b.serverManager.GetAllResources()
}

func (b *MCPBridge) GetCapabilities() []MCPCapability {
	return b.serverManager.GetCapabilities()
}

func (b *MCPBridge) GetClient() *MCPClient {
	return b.client
}

func (b *MCPBridge) GetServerManager() *MCPServerManager {
	return b.serverManager
}

func (b *MCPBridge) IsHealthy() bool {
	return b.serverManager.IsHealthy()
}

func (b *MCPBridge) GetStats() map[string]interface{} {
	b.statsMu.RLock()
	stats := b.stats
	b.statsMu.RUnlock()
	
	serverStats := b.serverManager.GetStats()
	
	return map[string]interface{}{
		"bridge_stats":  stats,
		"server_stats":  serverStats,
		"config":        b.config,
		"healthy":       b.IsHealthy(),
	}
}

func (b *MCPBridge) Shutdown() error {
	b.logger.Info("🔄 [MCP Bridge] Shutting down MCP bridge...")
	
	b.mu.Lock()
	for id, cancel := range b.activeRequests {
		b.logger.Infof("🚫 [MCP Bridge] Cancelling active request: %s", id)
		cancel()
	}
	b.activeRequests = make(map[string]context.CancelFunc)
	b.mu.Unlock()
	
	if b.client != nil {
		if err := b.client.Shutdown(); err != nil {
			b.logger.Errorf("❌ [MCP Bridge] Failed to shutdown client: %v", err)
		}
	}
	
	if b.protocolHandler != nil {
		if err := b.protocolHandler.Shutdown(); err != nil {
			b.logger.Errorf("❌ [MCP Bridge] Failed to shutdown protocol handler: %v", err)
		}
	}
	
	if b.serverManager != nil {
		if err := b.serverManager.Shutdown(); err != nil {
			b.logger.Errorf("❌ [MCP Bridge] Failed to shutdown server manager: %v", err)
		}
	}
	
	b.cancel()
	
	b.logger.Info("✅ [MCP Bridge] MCP bridge shutdown complete")
	return nil
}

func (b *MCPBridge) ReloadConfig(configPath string) error {
	b.logger.Info("🔄 [MCP Bridge] Reloading configuration...")
	
	newConfig, err := LoadMCPConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load new config: %w", err)
	}
	
	
	b.config = newConfig
	b.logger.Info("✅ [MCP Bridge] Configuration reloaded successfully")
	return nil
}

func (b *MCPBridge) AddServer(config MCPServerConfig) error {
	return b.serverManager.StartServer(config)
}

func (b *MCPBridge) RemoveServer(name string) error {
	return b.serverManager.StopServer(name)
}