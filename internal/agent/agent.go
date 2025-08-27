package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	"praxis-go-sdk/internal/config"
	"praxis-go-sdk/internal/llm"
	"praxis-go-sdk/internal/mcp"
	"praxis-go-sdk/internal/p2p"
	"praxis-go-sdk/pkg/agentcard"
)

// P2PAgent implements the Agent interface
type P2PAgent struct {
	config    *Config
	host      p2p.Host
	mcpBridge mcp.Bridge
	llmClient llm.Client
	logger    *logrus.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	started   bool
	mu        sync.RWMutex
}

// NewAgent creates a new P2P agent
func NewAgent(cfg *Config, logger *logrus.Logger) (Agent, error) {
	ctx, cancel := context.WithCancel(context.Background())

	agent := &P2PAgent{
		config:  cfg,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
		started: false,
	}

	return agent, nil
}

// Start initializes and starts the agent
func (a *P2PAgent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		a.logger.Warn("Agent already started")
		return nil
	}

	// Start P2P host
	if a.config.AppConfig.P2P.Enabled {
		a.logger.Info("Starting P2P host...")
		host, err := p2p.NewP2PHost(&a.config.AppConfig.P2P, a.config.Card, a.logger)
		if err != nil {
			return fmt.Errorf("failed to create P2P host: %w", err)
		}

		a.host = host

		if err := a.host.Start(); err != nil {
			return fmt.Errorf("failed to start P2P host: %w", err)
		}
		a.logger.Info("P2P host started successfully")
	} else {
		a.logger.Info("P2P host is disabled")
	}

	// Start MCP bridge
	if a.config.AppConfig.MCP.Enabled {
		a.logger.Info("Starting MCP bridge...")
		bridge, err := mcp.NewMCPBridge(nil, "config/mcp_config.yaml", a.logger)
		if err != nil {
			a.logger.Warnf("Failed to create MCP bridge: %v", err)
		} else {
			a.mcpBridge = bridge

			if err := a.mcpBridge.Start(); err != nil {
				a.logger.Warnf("Failed to start MCP bridge: %v", err)
			} else {
				a.logger.Info("MCP bridge started successfully")

				// Update agent card with MCP capabilities
				a.updateMCPCapabilities()
			}
		}
	} else {
		a.logger.Info("MCP bridge is disabled")
	}

	// Start LLM client
	if a.config.AppConfig.LLM.Enabled {
		a.logger.Info("Starting LLM client...")
		client, err := llm.NewClient(&a.config.AppConfig.LLM, a.mcpBridge, a.logger)
		if err != nil {
			a.logger.Warnf("Failed to create LLM client: %v", err)
		} else {
			a.llmClient = client
			a.logger.Info("LLM client started successfully")

			// Test LLM health
			if err := a.llmClient.Health(); err != nil {
				a.logger.Warnf("LLM health check failed: %v", err)
			} else {
				a.logger.Info("LLM health check passed")
			}
		}
	} else {
		a.logger.Info("LLM client is disabled")
	}

	a.started = true
	a.logger.Info("Agent started successfully")
	return nil
}

// Shutdown stops the agent and cleans up resources
func (a *P2PAgent) Shutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.started {
		a.logger.Warn("Agent not started")
		return nil
	}

	a.logger.Info("Shutting down agent...")

	// Cancel context to signal shutdown
	a.cancel()

	// Shutdown LLM client (no specific shutdown needed)
	a.logger.Info("LLM client shutdown complete")

	// Shutdown MCP bridge
	if a.mcpBridge != nil {
		if err := a.mcpBridge.Shutdown(); err != nil {
			a.logger.Errorf("Failed to shutdown MCP bridge: %v", err)
		} else {
			a.logger.Info("MCP bridge shutdown complete")
		}
	}

	// Shutdown P2P host
	if a.host != nil {
		if err := a.host.Shutdown(); err != nil {
			a.logger.Errorf("Failed to shutdown P2P host: %v", err)
		} else {
			a.logger.Info("P2P host shutdown complete")
		}
	}

	a.started = false
	a.logger.Info("Agent shutdown complete")
	return nil
}

// GetCard returns the agent card
func (a *P2PAgent) GetCard() *agentcard.ExtendedAgentCard {
	return a.config.Card
}

func (a *P2PAgent) GetRegistryConfig() *config.RegistryConfig {
	return &(a.config.AppConfig).Registry
}

// GetP2PHost returns the P2P host
func (a *P2PAgent) GetP2PHost() p2p.Host {
	return a.host
}

// GetMCPBridge returns the MCP bridge
func (a *P2PAgent) GetMCPBridge() mcp.Bridge {
	return a.mcpBridge
}

// GetLLMClient returns the LLM client
func (a *P2PAgent) GetLLMClient() llm.Client {
	return a.llmClient
}

// ConnectToPeer connects to a peer by name
func (a *P2PAgent) ConnectToPeer(peerName string) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}

	return a.host.ConnectToPeer(peerName)
}

// RequestCard requests the agent card from a peer
func (a *P2PAgent) RequestCard(peerName string) (*agentcard.ExtendedAgentCard, error) {
	if a.host == nil {
		return nil, fmt.Errorf("P2P host not initialized")
	}

	// Get peer ID
	_, err := a.host.GetPeerByName(peerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer ID: %w", err)
	}

	// Request card data
	cardData, err := a.host.RequestData(peerName, p2p.CardProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to request card: %w", err)
	}

	// Parse card data
	var card agentcard.ExtendedAgentCard
	if err := json.Unmarshal(cardData, &card); err != nil {
		// Try to parse as error response
		var errorResp map[string]interface{}
		if json.Unmarshal(cardData, &errorResp) == nil {
			if errorMsg, ok := errorResp["error"].(string); ok {
				return nil, fmt.Errorf("peer error: %s", errorMsg)
			}
		}
		return nil, fmt.Errorf("failed to unmarshal card response: %w", err)
	}

	return &card, nil
}

// ProcessLLMRequest processes an LLM request
func (a *P2PAgent) ProcessLLMRequest(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	if a.llmClient == nil {
		return nil, fmt.Errorf("LLM client not initialized")
	}

	return a.llmClient.ProcessRequest(ctx, req)
}

// updateMCPCapabilities updates the agent card with MCP capabilities
func (a *P2PAgent) updateMCPCapabilities() {
	if a.mcpBridge == nil {
		return
	}

	capabilities := a.mcpBridge.GetCapabilities()

	// Convert to agentcard.MCPCapability
	mcpServers := make([]agentcard.MCPCapability, len(capabilities))
	for i, cap := range capabilities {
		tools := make([]agentcard.MCPTool, len(cap.Tools))
		for j, tool := range cap.Tools {
			inputSchema := map[string]interface{}{
				"type":       tool.InputSchema.Type,
				"properties": tool.InputSchema.Properties,
				"required":   tool.InputSchema.Required,
			}
			tools[j] = agentcard.MCPTool{
				Name:         tool.Name,
				Description:  tool.Description,
				InputSchema:  inputSchema,
				OutputSchema: nil,
			}
		}

		resources := make([]agentcard.MCPResource, len(cap.Resources))
		for j, res := range cap.Resources {
			resources[j] = agentcard.MCPResource{
				URI: res.URI,

				Name:        res.Name,
				Description: res.Description,
				MimeType:    res.MIMEType,
			}
		}

		mcpServers[i] = agentcard.MCPCapability{
			ServerName: cap.ServerName,
			Transport:  cap.Transport,
			Tools:      tools,
			Resources:  resources,
			Status:     cap.Status,
			LastSeen:   cap.LastSeen,
		}
	}

	a.config.Card.MCPServers = mcpServers
	a.logger.Infof("Updated agent card with %d MCP servers", len(mcpServers))
}
