package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"go-p2p-agent/internal/config"
	"go-p2p-agent/pkg/utils"
)

// MCPBridge implements the Bridge interface
type MCPBridge struct {
	host         host.Host
	config       *config.MCPBridgeConfig
	servers      map[string]*ServerManager
	capabilities []MCPCapability
	client       *MCPClient
	logger       *logrus.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	started      bool
	stats        MCPStats
}

// MCPStats contains statistics about the MCP bridge
type MCPStats struct {
	RequestsTotal      int64
	RequestsSuccess    int64
	RequestsError      int64
	AvgResponseTime    time.Duration
	ToolCallsTotal     int64
	ToolCallsSuccess   int64
	ToolCallsError     int64
	LastRequest        time.Time
	LastError          string
	ActiveServers      int
	ServerStats        map[string]ServerStats
}

// ServerStats contains statistics about an MCP server
type ServerStats struct {
	RequestsTotal    int64
	RequestsSuccess  int64
	RequestsError    int64
	AvgResponseTime  time.Duration
	LastRequest      time.Time
	LastError        string
	ToolCount        int
	ResourceCount    int
	Status           string
}

// NewMCPBridge creates a new MCP bridge
func NewMCPBridge(host host.Host, configPath string, logger *logrus.Logger) (Bridge, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create the bridge instance
	bridge := &MCPBridge{
		host:    host,
		servers: make(map[string]*ServerManager),
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		stats: MCPStats{
			ServerStats: make(map[string]ServerStats),
		},
	}

	// Load configuration
	config, err := loadMCPConfig(configPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}
	bridge.config = config

	// Create MCP client
	bridge.client = NewMCPClient(host, bridge, logger)

	return bridge, nil
}

// Start initializes and starts the MCP bridge
func (m *MCPBridge) Start() error {
	m.logger.Info("Starting MCP bridge...")
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		m.logger.Warn("MCP bridge already started")
		return nil
	}
	
	// Check if MCP is enabled
	if !m.config.Enabled {
		m.logger.Info("MCP bridge is disabled")
		return nil
	}
	
	// Initialize servers
	for _, serverConfig := range m.config.Servers {
		if !serverConfig.Enabled {
			m.logger.Infof("Skipping disabled MCP server: %s", serverConfig.Name)
			continue
		}
		
		m.logger.Infof("Initializing MCP server: %s (%s)", serverConfig.Name, serverConfig.Transport)
		
		var manager *ServerManager
		var err error
		
		switch serverConfig.Transport {
		case "stdio":
			manager, err = NewStdioServerManager(serverConfig, m.config.Limits, m.logger)
			if err != nil {
				m.logger.Errorf("Failed to create stdio server manager for %s: %v", serverConfig.Name, err)
				continue
			}
			
		case "sse":
			manager, err = NewSSEServerManager(serverConfig, m.config.Limits, m.logger)
			if err != nil {
				m.logger.Errorf("Failed to create SSE server manager for %s: %v", serverConfig.Name, err)
				continue
			}
			
		default:
			m.logger.Errorf("Unsupported transport type for server %s: %s", serverConfig.Name, serverConfig.Transport)
			continue
		}
		
		// Start the server
		if err := manager.Start(m.ctx); err != nil {
			m.logger.Errorf("Failed to start server %s: %v", serverConfig.Name, err)
			continue
		}
		
		// Add to server map
		m.servers[serverConfig.Name] = manager
		
		// Initialize stats
		m.stats.ServerStats[serverConfig.Name] = ServerStats{
			Status: "active",
		}
		
		m.logger.Infof("MCP server %s started successfully", serverConfig.Name)
	}
	
	m.stats.ActiveServers = len(m.servers)
	
	// Update capabilities
	m.updateCapabilities()
	
	// Set up protocol handlers for P2P
	m.registerProtocolHandlers()
	
	m.started = true
	m.logger.Info("MCP bridge started successfully")
	
	return nil
}

// Shutdown stops the MCP bridge and cleans up resources
func (m *MCPBridge) Shutdown() error {
	m.logger.Info("Shutting down MCP bridge...")
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.started {
		m.logger.Warn("MCP bridge not started")
		return nil
	}
	
	// Cancel context to signal shutdown
	m.cancel()
	
	// Shutdown all servers
	for name, server := range m.servers {
		m.logger.Infof("Shutting down MCP server: %s", name)
		if err := server.Shutdown(); err != nil {
			m.logger.Errorf("Error shutting down server %s: %v", name, err)
		}
	}
	
	m.started = false
	m.logger.Info("MCP bridge shutdown complete")
	
	return nil
}

// GetClient returns an MCP client for making requests
func (m *MCPBridge) GetClient() Client {
	return m.client
}

// GetStats returns statistics about the MCP bridge
func (m *MCPBridge) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return map[string]interface{}{
		"requests_total":      m.stats.RequestsTotal,
		"requests_success":    m.stats.RequestsSuccess,
		"requests_error":      m.stats.RequestsError,
		"avg_response_time":   m.stats.AvgResponseTime.String(),
		"tool_calls_total":    m.stats.ToolCallsTotal,
		"tool_calls_success":  m.stats.ToolCallsSuccess,
		"tool_calls_error":    m.stats.ToolCallsError,
		"last_request":        m.stats.LastRequest,
		"active_servers":      m.stats.ActiveServers,
		"servers":             m.stats.ServerStats,
	}
}

// GetCapabilities returns the MCP capabilities
func (m *MCPBridge) GetCapabilities() []MCPCapability {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Create a copy to avoid race conditions
	capabilities := make([]MCPCapability, len(m.capabilities))
	copy(capabilities, m.capabilities)
	
	return capabilities
}

// ListAllTools returns all available tools across all MCP servers
func (m *MCPBridge) ListAllTools() map[string][]MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	result := make(map[string][]MCPTool)
	
	for name, server := range m.servers {
		tools, err := server.ListTools()
		if err != nil {
			m.logger.Errorf("Failed to list tools for server %s: %v", name, err)
			continue
		}
		
		result[name] = tools
	}
	
	return result
}

// ListAllResources returns all available resources across all MCP servers
func (m *MCPBridge) ListAllResources() map[string][]MCPResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	result := make(map[string][]MCPResource)
	
	for name, server := range m.servers {
		resources, err := server.ListResources()
		if err != nil {
			m.logger.Errorf("Failed to list resources for server %s: %v", name, err)
			continue
		}
		
		result[name] = resources
	}
	
	return result
}

// InvokeLocalTool invokes a tool on a local MCP server
func (m *MCPBridge) InvokeLocalTool(ctx context.Context, serverName, toolName string, params map[string]interface{}) (interface{}, error) {
	m.mu.RLock()
	server, exists := m.servers[serverName]
	m.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("server not found: %s", serverName)
	}
	
	m.stats.ToolCallsTotal++
	m.stats.LastRequest = time.Now()
	
	startTime := time.Now()
	result, err := server.InvokeTool(ctx, toolName, params)
	duration := time.Since(startTime)
	
	// Update stats
	m.mu.Lock()
	serverStats := m.stats.ServerStats[serverName]
	serverStats.RequestsTotal++
	serverStats.LastRequest = time.Now()
	
	if err != nil {
		m.stats.ToolCallsError++
		serverStats.RequestsError++
		serverStats.LastError = err.Error()
		m.stats.LastError = err.Error()
	} else {
		m.stats.ToolCallsSuccess++
		serverStats.RequestsSuccess++
		
		// Update average response time
		if serverStats.AvgResponseTime == 0 {
			serverStats.AvgResponseTime = duration
		} else {
			serverStats.AvgResponseTime = (serverStats.AvgResponseTime + duration) / 2
		}
		
		if m.stats.AvgResponseTime == 0 {
			m.stats.AvgResponseTime = duration
		} else {
			m.stats.AvgResponseTime = (m.stats.AvgResponseTime + duration) / 2
		}
	}
	
	m.stats.ServerStats[serverName] = serverStats
	m.mu.Unlock()
	
	return result, err
}

// updateCapabilities updates the capabilities of the MCP bridge
func (m *MCPBridge) updateCapabilities() {
	m.capabilities = make([]MCPCapability, 0, len(m.servers))
	
	for name, server := range m.servers {
		tools, err := server.ListTools()
		if err != nil {
			m.logger.Errorf("Failed to list tools for server %s: %v", name, err)
			continue
		}
		
		resources, err := server.ListResources()
		if err != nil {
			m.logger.Errorf("Failed to list resources for server %s: %v", name, err)
			// Continue anyway, resources are optional
		}
		
		capability := MCPCapability{
			ServerName: name,
			Transport:  server.GetTransport(),
			Tools:      tools,
			Resources:  resources,
			Status:     server.GetStatus(),
			LastSeen:   time.Now(),
		}
		
		m.capabilities = append(m.capabilities, capability)
		
		// Update stats
		m.mu.Lock()
		serverStats := m.stats.ServerStats[name]
		serverStats.ToolCount = len(tools)
		serverStats.ResourceCount = len(resources)
		serverStats.Status = server.GetStatus()
		m.stats.ServerStats[name] = serverStats
		m.mu.Unlock()
	}
	
	m.logger.Infof("Updated MCP capabilities: %d servers", len(m.capabilities))
}

// registerProtocolHandlers sets up protocol handlers for P2P communication
func (m *MCPBridge) registerProtocolHandlers() {
	// These would be implemented to handle incoming P2P requests
	// for MCP functionality
	// 
	// Example:
	// m.host.SetStreamHandler(MCPToolsCallProtocol, m.handleToolCall)
	// m.host.SetStreamHandler(MCPToolsListProtocol, m.handleToolsList)
	// etc.
}

// loadMCPConfig loads MCP configuration from a file
func loadMCPConfig(path string, logger *logrus.Logger) (*config.MCPBridgeConfig, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Warnf("MCP config file %s not found, using defaults", path)
		return &config.MCPBridgeConfig{
			Enabled: true,
			Limits: config.MCPLimits{
				MaxConcurrentRequests:  100,
				RequestTimeoutMs:       30000,
				MaxResponseSizeBytes:   10485760,
				MaxServersPerNode:      10,
				ConnectionPoolSize:     5,
				RetryAttempts:          3,
				RetryBackoffMs:         1000,
			},
		}, nil
	}
	
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read MCP config file: %w", err)
	}
	
	// Expand environment variables
	configString := utils.ExpandEnvVars(string(data))
	
	// Parse YAML
	var configFile struct {
		MCPBridge config.MCPBridgeConfig `yaml:"mcp_bridge"`
	}
	
	if err := yaml.Unmarshal([]byte(configString), &configFile); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config file: %w", err)
	}
	
	// Apply environment overrides
	configFile.MCPBridge.Enabled = utils.BoolFromEnv("MCP_ENABLED", configFile.MCPBridge.Enabled)
	
	return &configFile.MCPBridge, nil
}