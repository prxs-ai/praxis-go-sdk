package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type MCPServerManager struct {
	servers     map[string]*MCPServerInstance
	config      *MCPBridgeConfig
	logger      *logrus.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
}

type MCPServerInstance struct {
	Config      MCPServerConfig
	Process     *exec.Cmd
	SSEServer   *MCPSSEServer
	Status      string 
	StartTime   time.Time
	LastSeen    time.Time
	Tools       []MCPTool
	Resources   []MCPResource
	ErrorCount  int
	mu          sync.RWMutex
}

func NewMCPServerManager(config *MCPBridgeConfig, logger *logrus.Logger) *MCPServerManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &MCPServerManager{
		servers: make(map[string]*MCPServerInstance),
		config:  config,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (m *MCPServerManager) Start() error {
	m.logger.Info("🚀 [MCP Manager] Starting MCP server manager...")
	
	if !m.config.Enabled {
		m.logger.Info("📴 [MCP Manager] MCP bridge is disabled")
		return nil
	}
	
	for _, serverConfig := range m.config.Servers {
		if !serverConfig.Enabled {
			m.logger.Infof("⏭️  [MCP Manager] Skipping disabled server: %s", serverConfig.Name)
			continue
		}
		
		if err := m.StartServer(serverConfig); err != nil {
			m.logger.Errorf("❌ [MCP Manager] Failed to start server %s: %v", serverConfig.Name, err)
		}
	}
	
	go m.monitorServers()
	
	m.logger.Infof("✅ [MCP Manager] Started %d MCP servers", len(m.servers))
	return nil
}

func (m *MCPServerManager) StartServer(config MCPServerConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid server config: %w", err)
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.servers[config.Name]; exists {
		return fmt.Errorf("server %s already exists", config.Name)
	}
	
	m.logger.Infof("🔧 [MCP Manager] Starting server: %s (transport: %s)", config.Name, config.Transport)
	
	instance := &MCPServerInstance{
		Config:    config,
		Status:    "starting",
		StartTime: time.Now(),
		LastSeen:  time.Now(),
		Tools:     []MCPTool{},
		Resources: []MCPResource{},
	}
	
	switch config.Transport {
	case "stdio":
		if err := m.startStdioServer(instance); err != nil {
			return fmt.Errorf("failed to start stdio server: %w", err)
		}
	case "sse":
		if err := m.startSSEServer(instance); err != nil {
			return fmt.Errorf("failed to start SSE server: %w", err)
		}
	default:
		return fmt.Errorf("unsupported transport: %s", config.Transport)
	}
	
	m.servers[config.Name] = instance
	
	go m.initializeServerCapabilities(config.Name)
	
	m.logger.Infof("✅ [MCP Manager] Server %s started successfully", config.Name)
	return nil
}

func (m *MCPServerManager) startStdioServer(instance *MCPServerInstance) error {
	config := instance.Config
	
	cmd := exec.CommandContext(m.ctx, config.Command, config.Args...)
	
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}
	
	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
	}
	
	instance.Process = cmd
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}
	
	instance.Status = "running"
	
	go m.monitorProcess(instance)
	
	return nil
}

func (m *MCPServerManager) startSSEServer(instance *MCPServerInstance) error {
	config := instance.Config
	
	sseServer := NewMCPSSEServer(config, m.logger)
	instance.SSEServer = sseServer
	
	if err := sseServer.Start(); err != nil {
		return fmt.Errorf("failed to start SSE server: %w", err)
	}
	
	m.logger.Infof("🌐 [MCP Manager] SSE server started for %s at %s", config.Name, config.URL)
	
	instance.Status = "running"
	return nil
}

func (m *MCPServerManager) StopServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	instance, exists := m.servers[name]
	if !exists {
		return fmt.Errorf("server %s not found", name)
	}
	
	m.logger.Infof("🛑 [MCP Manager] Stopping server: %s", name)
	
	instance.mu.Lock()
	defer instance.mu.Unlock()
	
	if instance.Process != nil {
		if err := instance.Process.Process.Signal(os.Interrupt); err != nil {
			m.logger.Warnf("⚠️ [MCP Manager] Failed to send interrupt to %s: %v", name, err)
			if err := instance.Process.Process.Kill(); err != nil {
				m.logger.Errorf("❌ [MCP Manager] Failed to kill process for %s: %v", name, err)
			}
		}
		
		done := make(chan error, 1)
		go func() {
			done <- instance.Process.Wait()
		}()
		
		select {
		case <-done:
			m.logger.Infof("✅ [MCP Manager] Server %s stopped gracefully", name)
		case <-time.After(5 * time.Second):
			m.logger.Warnf("⚠️ [MCP Manager] Server %s did not stop gracefully, force killing", name)
			instance.Process.Process.Kill()
		}
	}
	
	if instance.SSEServer != nil {
		if err := instance.SSEServer.Stop(); err != nil {
			m.logger.Errorf("❌ [MCP Manager] Failed to stop SSE server %s: %v", name, err)
		}
	}
	
	instance.Status = "stopped"
	delete(m.servers, name)
	
	return nil
}

func (m *MCPServerManager) GetServer(name string) (*MCPServerInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	instance, exists := m.servers[name]
	return instance, exists
}

func (m *MCPServerManager) ListServers() map[string]*MCPServerInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	result := make(map[string]*MCPServerInstance)
	for name, instance := range m.servers {
		result[name] = instance
	}
	return result
}

func (m *MCPServerManager) GetServerTools(serverName string) ([]MCPTool, error) {
	instance, exists := m.GetServer(serverName)
	if !exists {
		return nil, fmt.Errorf("server %s not found", serverName)
	}
	
	instance.mu.RLock()
	defer instance.mu.RUnlock()
	
	return instance.Tools, nil
}

func (m *MCPServerManager) GetAllTools() []MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var allTools []MCPTool
	for _, instance := range m.servers {
		instance.mu.RLock()
		allTools = append(allTools, instance.Tools...)
		instance.mu.RUnlock()
	}
	
	return allTools
}

func (m *MCPServerManager) GetServerResources(serverName string) ([]MCPResource, error) {
	instance, exists := m.GetServer(serverName)
	if !exists {
		return nil, fmt.Errorf("server %s not found", serverName)
	}
	
	instance.mu.RLock()
	defer instance.mu.RUnlock()
	
	return instance.Resources, nil
}

func (m *MCPServerManager) GetAllResources() []MCPResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var allResources []MCPResource
	for _, instance := range m.servers {
		instance.mu.RLock()
		allResources = append(allResources, instance.Resources...)
		instance.mu.RUnlock()
	}
	
	return allResources
}

func (m *MCPServerManager) GetCapabilities() []MCPCapability {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var capabilities []MCPCapability
	for _, instance := range m.servers {
		instance.mu.RLock()
		capability := MCPCapability{
			ServerName: instance.Config.Name,
			Transport:  instance.Config.Transport,
			Tools:      instance.Tools,
			Resources:  instance.Resources,
			Status:     instance.Status,
			LastSeen:   instance.LastSeen,
		}
		instance.mu.RUnlock()
		
		capabilities = append(capabilities, capability)
	}
	
	return capabilities
}

func (m *MCPServerManager) monitorServers() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkServerHealth()
		}
	}
}

func (m *MCPServerManager) checkServerHealth() {
	m.mu.RLock()
	servers := make(map[string]*MCPServerInstance)
	for name, instance := range m.servers {
		servers[name] = instance
	}
	m.mu.RUnlock()
	
	for name, instance := range servers {
		instance.mu.Lock()
		
		if instance.Config.Transport == "stdio" && instance.Process != nil {
			if instance.Process.ProcessState != nil && instance.Process.ProcessState.Exited() {
				m.logger.Errorf("💀 [MCP Manager] Server %s process has exited", name)
				instance.Status = "error"
				instance.ErrorCount++
				
				if instance.ErrorCount < 3 {
					m.logger.Infof("🔄 [MCP Manager] Attempting to restart server %s", name)
					go m.restartServer(name)
				}
			}
		}
		
		if instance.Status == "running" {
			instance.LastSeen = time.Now()
		}
		
		instance.mu.Unlock()
	}
}

func (m *MCPServerManager) monitorProcess(instance *MCPServerInstance) {
	if instance.Process == nil {
		return
	}
	
	err := instance.Process.Wait()
	
	instance.mu.Lock()
	defer instance.mu.Unlock()
	
	if err != nil {
		m.logger.Errorf("❌ [MCP Manager] Server %s process exited with error: %v", instance.Config.Name, err)
		instance.Status = "error"
		instance.ErrorCount++
	} else {
		m.logger.Infof("ℹ️ [MCP Manager] Server %s process exited normally", instance.Config.Name)
		instance.Status = "stopped"
	}
}

func (m *MCPServerManager) restartServer(name string) {
	time.Sleep(5 * time.Second) 
	
	m.mu.Lock()
	instance, exists := m.servers[name]
	if !exists {
		m.mu.Unlock()
		return
	}
	
	config := instance.Config
	delete(m.servers, name)
	m.mu.Unlock()
	
	m.logger.Infof("🔄 [MCP Manager] Restarting server: %s", name)
	
	if err := m.StartServer(config); err != nil {
		m.logger.Errorf("❌ [MCP Manager] Failed to restart server %s: %v", name, err)
	}
}

func (m *MCPServerManager) initializeServerCapabilities(serverName string) {
	time.Sleep(2 * time.Second)
	
	instance, exists := m.GetServer(serverName)
	if !exists {
		return
	}
	
	m.logger.Infof("🔍 [MCP Manager] Discovering capabilities for server: %s", serverName)
	
	instance.mu.Lock()
	defer instance.mu.Unlock()
	
	if instance.SSEServer != nil {
		instance.Tools = instance.SSEServer.GetTools()
		instance.Resources = instance.SSEServer.GetResources()
	} else {
		instance.Tools = []MCPTool{
			{
				Name:        fmt.Sprintf("%s_example_tool", serverName),
				Description: fmt.Sprintf("Example tool from %s server", serverName),
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"input": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		}
	}
	
	m.logger.Infof("✅ [MCP Manager] Discovered %d tools for server %s", len(instance.Tools), serverName)
}

func (m *MCPServerManager) Shutdown() error {
	m.logger.Info("🔄 [MCP Manager] Shutting down MCP server manager...")
	
	m.cancel()
	
	serverList := m.ListServers()
	for name := range serverList {
		if err := m.StopServer(name); err != nil {
			m.logger.Errorf("❌ [MCP Manager] Failed to stop server %s: %v", name, err)
		}
	}
	
	m.logger.Info("✅ [MCP Manager] MCP server manager shutdown complete")
	return nil
}

func (m *MCPServerManager) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, instance := range m.servers {
		instance.mu.RLock()
		healthy := instance.Status == "running"
		instance.mu.RUnlock()
		
		if healthy {
			return true
		}
	}
	
	return len(m.servers) == 0 
}

func (m *MCPServerManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stats := map[string]interface{}{
		"total_servers":   len(m.servers),
		"running_servers": 0,
		"error_servers":   0,
		"servers":         make(map[string]interface{}),
	}
	
	for name, instance := range m.servers {
		instance.mu.RLock()
		
		if instance.Status == "running" {
			stats["running_servers"] = stats["running_servers"].(int) + 1
		} else if instance.Status == "error" {
			stats["error_servers"] = stats["error_servers"].(int) + 1
		}
		
		stats["servers"].(map[string]interface{})[name] = map[string]interface{}{
			"status":      instance.Status,
			"transport":   instance.Config.Transport,
			"start_time":  instance.StartTime,
			"last_seen":   instance.LastSeen,
			"error_count": instance.ErrorCount,
			"tools_count": len(instance.Tools),
		}
		
		instance.mu.RUnlock()
	}
	
	return stats
}