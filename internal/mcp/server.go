package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"go-p2p-agent/internal/config"
)

// ServerManager defines the interface for managing MCP servers
type ServerManager interface {
	// Start starts the server
	Start(ctx context.Context) error
	
	// Shutdown stops the server
	Shutdown() error
	
	// InvokeTool invokes a tool on the server
	InvokeTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error)
	
	// ListTools returns the list of tools available on the server
	ListTools() ([]MCPTool, error)
	
	// ListResources returns the list of resources available on the server
	ListResources() ([]MCPResource, error)
	
	// GetStatus returns the status of the server
	GetStatus() string
	
	// GetTransport returns the transport type of the server
	GetTransport() string
}

// StdioServerManager implements the ServerManager interface for stdio-based servers
type StdioServerManager struct {
	config      config.MCPServerConfig
	limits      config.MCPLimits
	logger      *logrus.Logger
	cmd         *exec.Cmd
	tools       []MCPTool
	resources   []MCPResource
	status      string
	mutex       sync.RWMutex
	initialized bool
	lastSeen    time.Time
}

// NewStdioServerManager creates a new stdio server manager
func NewStdioServerManager(config config.MCPServerConfig, limits config.MCPLimits, logger *logrus.Logger) (*StdioServerManager, error) {
	if config.Transport != "stdio" {
		return nil, fmt.Errorf("invalid transport for stdio manager: %s", config.Transport)
	}
	
	if config.Command == "" {
		return nil, fmt.Errorf("command is required for stdio transport")
	}
	
	return &StdioServerManager{
		config:    config,
		limits:    limits,
		logger:    logger,
		status:    "created",
		tools:     make([]MCPTool, 0),
		resources: make([]MCPResource, 0),
	}, nil
}

// Start starts the server
func (s *StdioServerManager) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.logger.Infof("Starting stdio server: %s", s.config.Name)
	
	// Prepare command
	s.cmd = exec.Command(s.config.Command, s.config.Args...)
	
	// Set working directory if specified
	if s.config.WorkDir != "" {
		s.cmd.Dir = s.config.WorkDir
	}
	
	// Set environment variables
	s.cmd.Env = os.Environ()
	for key, value := range s.config.Env {
		s.cmd.Env = append(s.cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}
	
	// Set up pipes for stdin/stdout
	_, err := s.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	
	_, err = s.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	// Start the command
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	
	s.status = "starting"
	
	// Initialize the server
	// This would involve sending an initialization request and waiting for a response
	// For brevity, we'll just set it as initialized here
	s.initialized = true
	s.status = "active"
	s.lastSeen = time.Now()
	
	// Fetch tools and resources
	// Again, for brevity, we'll just create dummy data
	s.tools = []MCPTool{
		{
			Name:        "echo",
			Description: "Echoes the input",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type": "string",
					},
				},
			},
			OutputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"result": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
	
	s.resources = []MCPResource{
		{
			URI:         "hello.txt",
			Name:        "Hello Text",
			Description: "A simple text file",
			MimeType:    "text/plain",
		},
	}
	
	s.logger.Infof("Stdio server started: %s", s.config.Name)
	
	return nil
}

// Shutdown stops the server
func (s *StdioServerManager) Shutdown() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.logger.Infof("Shutting down stdio server: %s", s.config.Name)
	
	if s.cmd == nil || s.cmd.Process == nil {
		s.logger.Warn("Server process not running")
		return nil
	}
	
	// Send termination signal
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		s.logger.Warnf("Failed to send interrupt signal: %v", err)
		
		// Force kill if interrupt fails
		if err := s.cmd.Process.Kill(); err != nil {
			s.logger.Errorf("Failed to kill process: %v", err)
			return fmt.Errorf("failed to kill process: %w", err)
		}
	}
	
	// Wait for process to exit
	if err := s.cmd.Wait(); err != nil {
		s.logger.Warnf("Process exited with error: %v", err)
	}
	
	s.status = "shutdown"
	s.logger.Infof("Stdio server shutdown complete: %s", s.config.Name)
	
	return nil
}

// InvokeTool invokes a tool on the server
func (s *StdioServerManager) InvokeTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	if s.status != "active" {
		return nil, fmt.Errorf("server is not active: %s", s.status)
	}
	
	s.logger.Infof("Invoking tool %s on server %s", toolName, s.config.Name)
	
	// Check if tool exists
	var foundTool *MCPTool
	for _, tool := range s.tools {
		if tool.Name == toolName {
			foundTool = &tool
			break
		}
	}
	
	if foundTool == nil {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
	
	// In a real implementation, we would send the request to the server
	// and wait for a response. For this example, we'll just echo the input.
	if toolName == "echo" {
		message, ok := params["message"].(string)
		if !ok {
			return nil, fmt.Errorf("message parameter must be a string")
		}
		
		return map[string]interface{}{
			"result": message,
		}, nil
	}
	
	return nil, fmt.Errorf("tool implementation not available: %s", toolName)
}

// ListTools returns the list of tools available on the server
func (s *StdioServerManager) ListTools() ([]MCPTool, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.tools, nil
}

// ListResources returns the list of resources available on the server
func (s *StdioServerManager) ListResources() ([]MCPResource, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.resources, nil
}

// GetStatus returns the status of the server
func (s *StdioServerManager) GetStatus() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.status
}

// GetTransport returns the transport type of the server
func (s *StdioServerManager) GetTransport() string {
	return s.config.Transport
}

// SSEServerManager implements the ServerManager interface for SSE-based servers
type SSEServerManager struct {
	config      config.MCPServerConfig
	limits      config.MCPLimits
	logger      *logrus.Logger
	tools       []MCPTool
	resources   []MCPResource
	status      string
	mutex       sync.RWMutex
	initialized bool
	lastSeen    time.Time
	client      *SSEClient
}

// NewSSEServerManager creates a new SSE server manager
func NewSSEServerManager(config config.MCPServerConfig, limits config.MCPLimits, logger *logrus.Logger) (*SSEServerManager, error) {
	if config.Transport != "sse" {
		return nil, fmt.Errorf("invalid transport for SSE manager: %s", config.Transport)
	}
	
	if config.URL == "" {
		return nil, fmt.Errorf("URL is required for SSE transport")
	}
	
	return &SSEServerManager{
		config:    config,
		limits:    limits,
		logger:    logger,
		status:    "created",
		tools:     make([]MCPTool, 0),
		resources: make([]MCPResource, 0),
	}, nil
}

// Start starts the server
func (s *SSEServerManager) Start(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.logger.Infof("Starting SSE server: %s", s.config.Name)
	
	// Initialize SSE client
	s.client = NewSSEClient(s.config.URL, s.logger)
	
	if err := s.client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to SSE server: %w", err)
	}
	
	s.status = "active"
	s.lastSeen = time.Now()
	
	// Fetch tools and resources
	// For simplicity, we'll use dummy data
	s.tools = []MCPTool{
		{
			Name:        "weather",
			Description: "Gets the current weather",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"location": map[string]interface{}{
						"type": "string",
					},
				},
			},
			OutputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"temperature": map[string]interface{}{
						"type": "number",
					},
					"conditions": map[string]interface{}{
						"type": "string",
					},
				},
			},
		},
	}
	
	s.resources = []MCPResource{
		{
			URI:         "weather-icons.json",
			Name:        "Weather Icons",
			Description: "Icons for weather conditions",
			MimeType:    "application/json",
		},
	}
	
	s.logger.Infof("SSE server started: %s", s.config.Name)
	
	return nil
}

// Shutdown stops the server
func (s *SSEServerManager) Shutdown() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.logger.Infof("Shutting down SSE server: %s", s.config.Name)
	
	if s.client != nil {
		if err := s.client.Disconnect(); err != nil {
			s.logger.Warnf("Error disconnecting from SSE server: %v", err)
		}
	}
	
	s.status = "shutdown"
	s.logger.Infof("SSE server shutdown complete: %s", s.config.Name)
	
	return nil
}

// InvokeTool invokes a tool on the server
func (s *SSEServerManager) InvokeTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error) {
	s.mutex.RLock()
	client := s.client
	s.mutex.RUnlock()
	
	if client == nil {
		return nil, fmt.Errorf("SSE client not initialized")
	}
	
	s.logger.Infof("Invoking tool %s on server %s", toolName, s.config.Name)
	
	// In a real implementation, we would send the request to the SSE server
	// For this example, we'll just provide a dummy response
	if toolName == "weather" {
		location, ok := params["location"].(string)
		if !ok {
			return nil, fmt.Errorf("location parameter must be a string")
		}
		
		return map[string]interface{}{
			"temperature": 72.5,
			"conditions":  "sunny",
			"location":    location,
		}, nil
	}
	
	return nil, fmt.Errorf("tool not found: %s", toolName)
}

// ListTools returns the list of tools available on the server
func (s *SSEServerManager) ListTools() ([]MCPTool, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.tools, nil
}

// ListResources returns the list of resources available on the server
func (s *SSEServerManager) ListResources() ([]MCPResource, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.resources, nil
}

// GetStatus returns the status of the server
func (s *SSEServerManager) GetStatus() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	return s.status
}

// GetTransport returns the transport type of the server
func (s *SSEServerManager) GetTransport() string {
	return s.config.Transport
}

// SSEClient is a simple client for SSE servers
type SSEClient struct {
	url    string
	logger *logrus.Logger
}

// NewSSEClient creates a new SSE client
func NewSSEClient(url string, logger *logrus.Logger) *SSEClient {
	return &SSEClient{
		url:    url,
		logger: logger,
	}
}

// Connect connects to the SSE server
func (c *SSEClient) Connect(ctx context.Context) error {
	// In a real implementation, this would establish a connection to the SSE server
	c.logger.Infof("Connected to SSE server: %s", c.url)
	return nil
}

// Disconnect disconnects from the SSE server
func (c *SSEClient) Disconnect() error {
	// In a real implementation, this would close the connection to the SSE server
	c.logger.Infof("Disconnected from SSE server: %s", c.url)
	return nil
}