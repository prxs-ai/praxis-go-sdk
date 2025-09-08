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

type TransportManager struct {
	clients map[string]*MCPClientWrapper
	factory *ClientFactory
	logger  *logrus.Logger
	mu      sync.RWMutex
}

func NewTransportManager(logger *logrus.Logger) *TransportManager {
	if logger == nil {
		logger = logrus.New()
	}

	return &TransportManager{
		clients: make(map[string]*MCPClientWrapper),
		factory: NewClientFactory(logger),
		logger:  logger,
	}
}

func (tm *TransportManager) RegisterSSEEndpoint(name, url string, headers map[string]string) {
	config := ClientConfig{
		Type:    ClientTypeSSE,
		Address: url,
		Headers: headers,
		Logger:  tm.logger,
	}
	tm.factory.RegisterConfig(name, config)
}

func (tm *TransportManager) RegisterHTTPEndpoint(name, url string, headers map[string]string) {
	config := ClientConfig{
		Type:    ClientTypeStreamableHTTP,
		Address: url,
		Headers: headers,
		Logger:  tm.logger,
	}
	tm.factory.RegisterConfig(name, config)
}

func (tm *TransportManager) RegisterSTDIOEndpoint(name, command string, args []string) {
	config := ClientConfig{
		Type:    ClientTypeSTDIO,
		Command: command,
		Args:    args,
		Logger:  tm.logger,
	}
	tm.factory.RegisterConfig(name, config)
}

func (tm *TransportManager) GetClient(name string) (*MCPClientWrapper, error) {
	return tm.factory.GetOrCreateClient(name)
}

func (tm *TransportManager) CallRemoteTool(ctx context.Context, clientName, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	client, err := tm.GetClient(clientName)
	if err != nil {
		return nil, fmt.Errorf("failed to get client %s: %w", clientName, err)
	}

	return client.CallTool(ctx, toolName, args)
}

func (tm *TransportManager) Close() {
	tm.factory.CloseAll()
}

type ResilientSSEClient struct {
	baseURL     string
	headers     map[string]string
	client      *client.Client
	ctx         context.Context
	cancel      context.CancelFunc
	reconnectCh chan struct{}
	mutex       sync.RWMutex
	logger      *logrus.Logger
}

func NewResilientSSEClient(baseURL string, headers map[string]string, logger *logrus.Logger) *ResilientSSEClient {
	ctx, cancel := context.WithCancel(context.Background())

	if logger == nil {
		logger = logrus.New()
	}

	rsc := &ResilientSSEClient{
		baseURL:     baseURL,
		headers:     headers,
		ctx:         ctx,
		cancel:      cancel,
		reconnectCh: make(chan struct{}, 1),
		logger:      logger,
	}

	go rsc.reconnectLoop()
	return rsc
}

func (rsc *ResilientSSEClient) connect() error {
	rsc.mutex.Lock()
	defer rsc.mutex.Unlock()

	if rsc.client != nil {
		rsc.client.Close()
	}

	c, err := client.NewSSEMCPClient(rsc.baseURL)
	if err != nil {
		return fmt.Errorf("failed to create SSE client: %w", err)
	}

	ctx, cancel := context.WithTimeout(rsc.ctx, 30*time.Second)
	defer cancel()

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "praxis-resilient-sse",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	if _, err := c.Initialize(ctx, initRequest); err != nil {
		c.Close()
		return fmt.Errorf("failed to initialize SSE client: %w", err)
	}

	rsc.client = c
	rsc.logger.Info("SSE client connected successfully")
	return nil
}

func (rsc *ResilientSSEClient) reconnectLoop() {
	for {
		select {
		case <-rsc.ctx.Done():
			return
		case <-rsc.reconnectCh:
			rsc.logger.Info("Attempting to reconnect SSE client...")

			for attempt := 1; attempt <= 5; attempt++ {
				if err := rsc.connect(); err != nil {
					rsc.logger.Errorf("Reconnection attempt %d failed: %v", attempt, err)

					backoff := time.Duration(attempt) * time.Second
					select {
					case <-time.After(backoff):
					case <-rsc.ctx.Done():
						return
					}
				} else {
					rsc.logger.Info("Reconnected successfully")
					break
				}
			}
		}
	}
}

func (rsc *ResilientSSEClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rsc.mutex.RLock()
	client := rsc.client
	rsc.mutex.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("client not connected")
	}

	result, err := client.CallTool(ctx, req)
	if err != nil {
		select {
		case rsc.reconnectCh <- struct{}{}:
		default:
		}
		return nil, fmt.Errorf("connection error: %w", err)
	}

	return result, err
}

func (rsc *ResilientSSEClient) Close() error {
	rsc.cancel()

	rsc.mutex.Lock()
	defer rsc.mutex.Unlock()

	if rsc.client != nil {
		return rsc.client.Close()
	}

	return nil
}

type StreamableHTTPClientPool struct {
	clients chan *MCPClientWrapper
	factory func() *MCPClientWrapper
	maxSize int
	baseURL string
	logger  *logrus.Logger
}

func NewStreamableHTTPClientPool(baseURL string, maxSize int, logger *logrus.Logger) *StreamableHTTPClientPool {
	if logger == nil {
		logger = logrus.New()
	}

	pool := &StreamableHTTPClientPool{
		clients: make(chan *MCPClientWrapper, maxSize),
		maxSize: maxSize,
		baseURL: baseURL,
		logger:  logger,
		factory: func() *MCPClientWrapper {
			config := ClientConfig{
				Type:    ClientTypeStreamableHTTP,
				Address: baseURL,
				Logger:  logger,
			}
			client, err := NewMCPClient(config)
			if err != nil {
				logger.Errorf("Failed to create client for pool: %v", err)
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := client.Initialize(ctx); err != nil {
				logger.Errorf("Failed to initialize client for pool: %v", err)
				client.Close()
				return nil
			}

			return client
		},
	}

	for i := 0; i < maxSize; i++ {
		if client := pool.factory(); client != nil {
			pool.clients <- client
		}
	}

	return pool
}

func (pool *StreamableHTTPClientPool) Get() *MCPClientWrapper {
	select {
	case c := <-pool.clients:
		return c
	default:
		return pool.factory()
	}
}

func (pool *StreamableHTTPClientPool) Put(c *MCPClientWrapper) {
	if c == nil {
		return
	}

	select {
	case pool.clients <- c:
	default:
		c.Close()
	}
}

func (pool *StreamableHTTPClientPool) CallTool(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	c := pool.Get()
	if c == nil {
		return nil, fmt.Errorf("failed to get client from pool")
	}
	defer pool.Put(c)

	return c.CallTool(ctx, name, args)
}

func (pool *StreamableHTTPClientPool) Close() {
	close(pool.clients)
	for c := range pool.clients {
		c.Close()
	}
}
