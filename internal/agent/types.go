package agent

import (
	"context"

	"go-p2p-agent/internal/config"
	"go-p2p-agent/internal/llm"
	"go-p2p-agent/internal/mcp"
	"go-p2p-agent/internal/p2p"
	"go-p2p-agent/pkg/agentcard"
)

// Agent defines the interface for the P2P agent
type Agent interface {
	// Start initializes and starts the agent
	Start() error

	// Shutdown stops the agent and cleans up resources
	Shutdown() error

	// GetCard returns the agent card
	GetCard() *agentcard.ExtendedAgentCard

	// GetP2PHost returns the P2P host
	GetP2PHost() p2p.Host

	// GetMCPBridge returns the MCP bridge
	GetMCPBridge() mcp.Bridge

	// GetLLMClient returns the LLM client
	GetLLMClient() llm.Client

	// ConnectToPeer connects to a peer by name
	ConnectToPeer(peerName string) error

	// RequestCard requests the agent card from a peer
	RequestCard(peerName string) (*agentcard.ExtendedAgentCard, error)

	// ProcessLLMRequest processes an LLM request
	ProcessLLMRequest(ctx context.Context, req *llm.Request) (*llm.Response, error)
}

// Config contains the agent configuration
type Config struct {
	// AppConfig is the main application configuration
	AppConfig *config.AppConfig

	// Card is the agent card
	Card *agentcard.ExtendedAgentCard
}

// DefaultConfig returns the default agent configuration
func DefaultConfig() *Config {
	appConfig := config.DefaultConfig()

	// Create default agent card
	streaming := true
	pushNotifications := false
	stateTransitionHistory := false

	card := &agentcard.ExtendedAgentCard{
		AgentCard: agentcard.AgentCard{
			Name:            appConfig.Agent.Name,
			Description:     appConfig.Agent.Description,
			URL:             appConfig.Agent.URL,
			Version:         appConfig.Agent.Version,
			ProtocolVersion: "0.2.5",
			Provider: &agentcard.AgentProvider{
				Organization: "Praxis AI",
				URL:          "https://praxis.ai",
			},
			Capabilities: agentcard.AgentCapabilities{
				Streaming:              &streaming,
				PushNotifications:      &pushNotifications,
				StateTransitionHistory: &stateTransitionHistory,
			},
			DefaultInputModes:  []string{"text/plain"},
			DefaultOutputModes: []string{"text/plain"},
			Skills: []agentcard.AgentSkill{
				{
					ID:          "echo",
					Name:        "Echo",
					Description: "Simple echo skill that returns the input message",
					Tags:        []string{"utility", "echo"},
					Examples:    []string{"echo hello world", "repeat this message"},
					InputModes:  []string{"text/plain"},
					OutputModes: []string{"text/plain"},
				},
				{
					ID:          "p2p-communication",
					Name:        "P2P Communication",
					Description: "Peer-to-peer messaging and tool invocation",
					Tags:        []string{"p2p", "communication"},
					InputModes:  []string{"text", "json"},
					OutputModes: []string{"text", "json"},
				},
				{
					ID:          "llm-processing",
					Name:        "LLM Processing",
					Description: "Natural language processing with LLM",
					Tags:        []string{"llm", "ai", "nlp"},
					InputModes:  []string{"text"},
					OutputModes: []string{"text", "json"},
				},
			},
		},
		MCPServers: []agentcard.MCPCapability{},
	}

	return &Config{
		AppConfig: appConfig,
		Card:      card,
	}
}
