package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
)

type ClientType string

const (
	ClientTypeSTDIO          ClientType = "stdio"
	ClientTypeStreamableHTTP ClientType = "streamablehttp"
	ClientTypeSSE            ClientType = "sse"
	ClientTypeInProcess      ClientType = "inprocess"
)

type MCPClientWrapper struct {
	client      *client.Client
	clientType  ClientType
	serverInfo  *mcp.InitializeResult
	logger      *logrus.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	initialized bool
}

type ClientConfig struct {
	Type    ClientType
	Address string
	Command string
	Args    []string
	Headers map[string]string
	Logger  *logrus.Logger
}

func NewMCPClient(config ClientConfig) (*MCPClientWrapper, error) {
	if config.Logger == nil {
		config.Logger = logrus.New()
	}

	ctx, cancel := context.WithCancel(context.Background())

	wrapper := &MCPClientWrapper{
		clientType: config.Type,
		logger:     config.Logger,
		ctx:        ctx,
		cancel:     cancel,
	}

	var err error
	switch config.Type {
	case ClientTypeSTDIO:
		wrapper.client, err = client.NewStdioMCPClient(
			config.Command, nil, config.Args...,
		)
	case ClientTypeStreamableHTTP:
		wrapper.client, err = client.NewStreamableHttpClient(config.Address)
	case ClientTypeSSE:
		wrapper.client, err = client.NewSSEMCPClient(config.Address)
	default:
		return nil, fmt.Errorf("unsupported client type: %s", config.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s client: %w", config.Type, err)
	}

	config.Logger.Infof("Created MCP %s client", config.Type)
	return wrapper, nil
}

func (w *MCPClientWrapper) Initialize(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.initialized {
		return nil
	}

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "praxis-agent",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	var err error
	w.serverInfo, err = w.client.Initialize(ctx, initRequest)
	if err != nil {
		return fmt.Errorf("failed to initialize client: %w", err)
	}

	w.initialized = true
	w.logger.Infof("MCP client initialized. Server: %s v%s",
		w.serverInfo.ServerInfo.Name,
		w.serverInfo.ServerInfo.Version)

	return nil
}

func (w *MCPClientWrapper) ListTools(ctx context.Context) (*mcp.ListToolsResult, error) {
	w.mu.RLock()
	if !w.initialized {
		w.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	w.mu.RUnlock()

	return w.client.ListTools(ctx, mcp.ListToolsRequest{})
}

func (w *MCPClientWrapper) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	w.mu.RLock()
	if !w.initialized {
		w.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	w.mu.RUnlock()

	request := mcp.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = args

	w.logger.Debugf("Calling tool %s with args: %v", name, args)

	result, err := w.client.CallTool(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	return result, nil
}

func (w *MCPClientWrapper) ListResources(ctx context.Context) (*mcp.ListResourcesResult, error) {
	w.mu.RLock()
	if !w.initialized {
		w.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	w.mu.RUnlock()

	return w.client.ListResources(ctx, mcp.ListResourcesRequest{})
}

func (w *MCPClientWrapper) ReadResource(ctx context.Context, uri string) (*mcp.ReadResourceResult, error) {
	w.mu.RLock()
	if !w.initialized {
		w.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	w.mu.RUnlock()

	request := mcp.ReadResourceRequest{}
	request.Params.URI = uri

	return w.client.ReadResource(ctx, request)
}

func (w *MCPClientWrapper) ListPrompts(ctx context.Context) (*mcp.ListPromptsResult, error) {
	w.mu.RLock()
	if !w.initialized {
		w.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	w.mu.RUnlock()

	return w.client.ListPrompts(ctx, mcp.ListPromptsRequest{})
}

func (w *MCPClientWrapper) GetPrompt(ctx context.Context, name string, args map[string]string) (*mcp.GetPromptResult, error) {
	w.mu.RLock()
	if !w.initialized {
		w.mu.RUnlock()
		return nil, fmt.Errorf("client not initialized")
	}
	w.mu.RUnlock()

	request := mcp.GetPromptRequest{}
	request.Params.Name = name
	request.Params.Arguments = args

	return w.client.GetPrompt(ctx, request)
}

func (w *MCPClientWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.cancel()

	if w.client != nil {
		return w.client.Close()
	}

	return nil
}

func (w *MCPClientWrapper) IsInitialized() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.initialized
}

func (w *MCPClientWrapper) GetServerInfo() *mcp.InitializeResult {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.serverInfo
}

type ClientFactory struct {
	configs map[string]ClientConfig
	clients map[string]*MCPClientWrapper
	mu      sync.RWMutex
	logger  *logrus.Logger
}

func NewClientFactory(logger *logrus.Logger) *ClientFactory {
	if logger == nil {
		logger = logrus.New()
	}

	return &ClientFactory{
		configs: make(map[string]ClientConfig),
		clients: make(map[string]*MCPClientWrapper),
		logger:  logger,
	}
}

func (f *ClientFactory) RegisterConfig(name string, config ClientConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs[name] = config
}

func (f *ClientFactory) GetOrCreateClient(name string) (*MCPClientWrapper, error) {
	f.mu.RLock()
	if client, exists := f.clients[name]; exists {
		f.mu.RUnlock()
		return client, nil
	}
	f.mu.RUnlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	if client, exists := f.clients[name]; exists {
		return client, nil
	}

	config, exists := f.configs[name]
	if !exists {
		return nil, fmt.Errorf("no configuration found for client: %s", name)
	}

	client, err := NewMCPClient(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to initialize client %s: %w", name, err)
	}

	f.clients[name] = client
	return client, nil
}

func (f *ClientFactory) CloseAll() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for name, client := range f.clients {
		if err := client.Close(); err != nil {
			f.logger.Errorf("Failed to close client %s: %v", name, err)
		}
	}

	f.clients = make(map[string]*MCPClientWrapper)
}
