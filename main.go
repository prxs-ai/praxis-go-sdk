package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	CardProtocol = protocol.ID("/ai-agent/card/1.0.0")
	
	DefaultP2PPort = 0
	DefaultHTTPPort = 8000
)

type AgentSkill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags,omitempty"`
	Examples     []string `json:"examples,omitempty"`
	InputModes   []string `json:"inputModes,omitempty"`
	OutputModes  []string `json:"outputModes,omitempty"`
}

type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url"`
}

type AgentCapabilities struct {
	Streaming               *bool `json:"streaming,omitempty"`
	PushNotifications       *bool `json:"pushNotifications,omitempty"`
	StateTransitionHistory  *bool `json:"stateTransitionHistory,omitempty"`
}

type AgentCard struct {
	Name                string             `json:"name"`
	Description         string             `json:"description"`
	URL                 string             `json:"url"`
	Version             string             `json:"version"`
	ProtocolVersion     string             `json:"protocolVersion"`
	Provider            *AgentProvider     `json:"provider,omitempty"`
	Capabilities        AgentCapabilities  `json:"capabilities"`
	DefaultInputModes   []string           `json:"defaultInputModes"`
	DefaultOutputModes  []string           `json:"defaultOutputModes"`
	Skills              []AgentSkill       `json:"skills"`
	SecuritySchemes     interface{}        `json:"securitySchemes,omitempty"`
	Security            interface{}        `json:"security,omitempty"`
}

type P2PAgent struct {
	host        host.Host
	httpServer  *http.Server
	agentCard   ExtendedAgentCard
	connections map[string]peer.ID
	mu          sync.RWMutex
	logger      *logrus.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	mcpBridge   *MCPBridge
	mcpEnabled  bool
	llmAgent    *LLMAgentImpl
	llmEnabled  bool
	discovery   *PeerDiscovery
}

func NewP2PAgent() (*P2PAgent, error) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	
	ctx, cancel := context.WithCancel(context.Background())
	
	agentName := getEnv("AGENT_NAME", "go-agent")
	agentVersion := getEnv("AGENT_VERSION", "1.0.0")
	agentDescription := getEnv("AGENT_DESCRIPTION", "Go P2P Agent")
	agentURL := getEnv("AGENT_URL", "http://localhost:8000")
	
	
	streaming := true
	pushNotifications := false
	stateTransitionHistory := false
	
	baseCard := AgentCard{
		Name:            agentName,
		Description:     agentDescription,
		URL:             agentURL,
		Version:         agentVersion,
		ProtocolVersion: "0.2.5",
		Provider: &AgentProvider{
			Organization: "Praxis AI",
			URL:          "https://praxis.ai",
		},
		Capabilities: AgentCapabilities{
			Streaming:              &streaming,
			PushNotifications:      &pushNotifications,
			StateTransitionHistory: &stateTransitionHistory,
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []AgentSkill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Simple echo skill that returns the input message",
				Tags:        []string{"utility", "echo"},
				Examples:    []string{"echo hello world", "repeat this message"},
				InputModes:  []string{"text/plain"},
				OutputModes: []string{"text/plain"},
			},
		},
	}
	
	card := ExtendedAgentCard{
		AgentCard:  baseCard,
		MCPServers: []MCPCapability{}, 
	}
	
	mcpEnabled := strings.ToLower(getEnv("MCP_ENABLED", "true")) == "true"
	llmEnabled := strings.ToLower(getEnv("LLM_ENABLED", "true")) == "true"
	
	agent := &P2PAgent{
		agentCard:   card,
		connections: make(map[string]peer.ID),
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		mcpEnabled:  mcpEnabled,
		llmEnabled:  llmEnabled,
	}
	
	return agent, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


func expandEnvVars(s string) string {
	expanded := os.ExpandEnv(s)
	
	if strings.Contains(s, "${OPENAI_API_KEY}") {
		originalKey := "${OPENAI_API_KEY}"
		envKey := os.Getenv("OPENAI_API_KEY")
		if len(envKey) > 0 {
			log.Printf("🔑 [DEBUG] API Key substitution: %s -> %s (first 20 chars)", originalKey, envKey[:min(20, len(envKey))])
		}
	}
	return expanded
}

func getContainerIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "0.0.0.0"
	}
	
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}
	
	return "0.0.0.0"
}

func (a *P2PAgent) StartMCP() error {
	if !a.mcpEnabled {
		a.logger.Info("📴 [P2P Agent] MCP bridge is disabled")
		return nil
	}
	
	a.logger.Info("🚀 [P2P Agent] Starting MCP bridge...")
	
	mcpConfigPath := getEnv("MCP_CONFIG_PATH", "config/mcp_config.yaml")
	bridge, err := NewMCPBridge(a.host, mcpConfigPath, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create MCP bridge: %w", err)
	}
	
	a.mcpBridge = bridge
	
	if err := a.mcpBridge.Start(); err != nil {
		return fmt.Errorf("failed to start MCP bridge: %w", err)
	}
	
	a.updateMCPCapabilities()
	
	a.logger.Info("✅ [P2P Agent] MCP bridge started successfully")
	return nil
}

func (a *P2PAgent) updateMCPCapabilities() {
	if a.mcpBridge == nil {
		return
	}
	
	a.mu.Lock()
	defer a.mu.Unlock()
	
	capabilities := a.mcpBridge.GetCapabilities()
	a.agentCard.MCPServers = capabilities
	
	a.logger.Infof("🔄 [P2P Agent] Updated agent card with %d MCP servers", len(capabilities))
}

func (a *P2PAgent) StartLLM() error {
	if !a.llmEnabled {
		a.logger.Info("📴 [P2P Agent] LLM agent is disabled")
		return nil
	}
	
	a.logger.Info("🤖 [P2P Agent] Starting LLM agent...")
	
	
	llmConfig, err := a.loadLLMConfig()
	if err != nil {
		return fmt.Errorf("failed to load LLM config: %w", err)
	}
	
	
	llmAgent, err := NewLLMAgent(llmConfig, a.mcpBridge, a, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create LLM agent: %w", err)
	}
	
	a.llmAgent = llmAgent
	
	
	if err := a.llmAgent.Health(); err != nil {
		a.logger.Warnf("⚠️ [P2P Agent] LLM health check failed: %v", err)
		
	}
	
	a.logger.Info("✅ [P2P Agent] LLM agent started successfully")
	return nil
}

func (a *P2PAgent) loadLLMConfig() (*LLMConfig, error) {
	llmConfigPath := getEnv("LLM_CONFIG_PATH", "config/llm_config.yaml")
	
	configData, err := os.ReadFile(llmConfigPath)
	if err != nil {
		a.logger.Warnf("⚠️ [P2P Agent] Failed to read LLM config from %s: %v", llmConfigPath, err)
		a.logger.Info("📝 [P2P Agent] Using default LLM configuration")
		
		
		config := DefaultLLMConfig
		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			config.APIKey = apiKey
		} else {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
		}
		
		return &config, nil
	}
	
	
	configString := string(configData)
	a.logger.Infof("🔧 [P2P Agent] Original config: %s", configString)
	configString = expandEnvVars(configString)
	a.logger.Infof("🔧 [P2P Agent] Expanded config: %s", configString)
	
	
	var configFile struct {
		LLMAgent LLMConfig `yaml:"llm_agent"`
	}
	
	if err := yaml.Unmarshal([]byte(configString), &configFile); err != nil {
		return nil, fmt.Errorf("failed to parse LLM config: %w", err)
	}
	
	config := configFile.LLMAgent
	
	if config.APIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required in config or environment")
	}
	
	a.logger.Infof("📋 [P2P Agent] Loaded LLM config: provider=%s, model=%s", config.Provider, config.Model)
	return &config, nil
}

func (a *P2PAgent) StartP2P() error {
	a.logger.Info("Starting P2P node...")
	
	useNoise := strings.ToLower(getEnv("INSECURE_P2P", "false")) != "true"
	
	containerIP := getContainerIP()
	a.logger.Infof("Using container IP: %s", containerIP)
	
	p2pPortStr := getEnv("P2P_PORT", "0")
	p2pPort, err := strconv.Atoi(p2pPortStr)
	if err != nil {
		p2pPort = DefaultP2PPort
	}
	
	listenAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", containerIP, p2pPort))
	if err != nil {
		return fmt.Errorf("failed to create listen address: %w", err)
	}
	
	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddr),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
	}
	
	if useNoise {
		a.logger.Info("Using Noise security transport")
		opts = append(opts, libp2p.Security(noise.ID, noise.New))
	} else {
		a.logger.Warning("Using INSECURE transport!")
	
	}
	
	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}
	
	a.host = h
	a.logger.Info("✅ [P2P Agent] LibP2P host created successfully")
	
	a.host.SetStreamHandler(CardProtocol, a.handleCardRequest)
	a.logger.Info("✅ [P2P Agent] Stream handler registered for card protocol")
	
	rendezvous := "praxis-agents"
	
	a.logger.Infof("🔍 [P2P Agent] Initializing peer discovery with rendezvous: %s", rendezvous)
	a.discovery = NewPeerDiscovery(h, a.logger, rendezvous)
	if err := a.discovery.Start(); err != nil {
		a.logger.Errorf("❌ [P2P Agent] Failed to start peer discovery: %v", err)
	} else {
		a.logger.Infof("✅ [P2P Agent] Peer discovery started with rendezvous: %s", rendezvous)
		a.logger.Infof("🔍 [P2P Agent] Discovery service status: %d peers discovered", a.discovery.GetPeerCount())
	}
	
	if err := a.StartMCP(); err != nil {
		a.logger.Errorf("❌ [P2P Agent] Failed to start MCP bridge: %v", err)
	}
	
	if err := a.StartLLM(); err != nil {
		a.logger.Errorf("❌ [P2P Agent] Failed to start LLM agent: %v", err)
	}
	
	a.logger.Infof("P2P node started with ID: %s", h.ID())
	a.logger.Infof("Listening on addresses: %v", h.Addrs())
	
	return nil
}

func (a *P2PAgent) handleCardRequest(stream network.Stream) {
	defer stream.Close()
	
	peerID := stream.Conn().RemotePeer()
	timestamp := time.Now().UTC().Format(time.RFC3339)
	protocol := stream.Protocol()
	
	a.logger.Infof("📨 [P2P] [%s] Received card request from peer %s via protocol %s", timestamp, peerID, protocol)
	
	cardData, err := json.Marshal(a.agentCard)
	if err != nil {
		a.logger.Errorf("[%s] Failed to marshal agent card: %v", timestamp, err)
		errorResponse := map[string]interface{}{
			"error": "Failed to serialize agent card",
			"code":  500,
		}
		errorData, _ := json.Marshal(errorResponse)
		stream.Write(errorData)
		return
	}
	
	_, err = stream.Write(cardData)
	if err != nil {
		a.logger.Errorf("❌ [P2P] [%s] Failed to write card data to stream: %v", timestamp, err)
		return
	}
	
	a.logger.Infof("📤 [P2P] [%s] Sent %d bytes card data to peer %s via P2P stream", timestamp, len(cardData), peerID)
}

func (a *P2PAgent) ConnectToPeer(peerName string) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}
	
	if a.discovery == nil {
		return fmt.Errorf("peer discovery not initialized")
	}
	
	a.logger.Infof("🔍 [P2P Discovery] Attempting to connect to peer: %s", peerName)
	
	err := a.discovery.ConnectToPeerByName(peerName)
	if err != nil {
		return fmt.Errorf("failed to connect via discovery: %w", err)
	}
	
	peerID, err := a.discovery.ResolvePeerName(peerName)
	if err != nil {
		return fmt.Errorf("failed to resolve peer name: %w", err)
	}
	
	a.mu.Lock()
	a.connections[peerName] = peerID
	a.mu.Unlock()
	
	a.logger.Infof("✅ [P2P Discovery] Successfully connected to peer: %s (%s)", peerName, peerID)
	return nil
}


func (a *P2PAgent) ConnectToPeerDirect(peerName string, peerID peer.ID) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}
	
	a.logger.Infof("🔗 [P2P Direct] Connecting to peer %s (%s) via existing P2P", peerName, peerID)
	
	
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	
	
	stream, err := a.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to establish direct P2P connection to %s: %w", peerID, err)
	}
	stream.Close()
	
	
	a.mu.Lock()
	a.connections[peerName] = peerID
	a.mu.Unlock()
	
	a.logger.Infof("✅ [P2P Direct] Successfully connected to peer %s via P2P", peerName)
	return nil
}


func (a *P2PAgent) ConnectToPeerDirectWithAddr(peerName string, peerID peer.ID, addr string) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}
	
	a.logger.Infof("🔗 [P2P Direct] Connecting to peer %s (%s) at %s", peerName, peerID, addr)
	
	
	maddr, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return fmt.Errorf("invalid multiaddr %s: %w", addr, err)
	}
	
	
	a.host.Peerstore().AddAddr(peerID, maddr, time.Hour)
	a.logger.Infof("🔍 [P2P Direct] Added address %s for peer %s to peerstore", addr, peerID)
	
	
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	
	err = a.host.Connect(ctx, peer.AddrInfo{ID: peerID, Addrs: []multiaddr.Multiaddr{maddr}})
	if err != nil {
		return fmt.Errorf("failed to connect to peer %s at %s: %w", peerID, addr, err)
	}
	
	
	stream, err := a.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to establish stream to %s: %w", peerID, err)
	}
	stream.Close()
	
	
	a.mu.Lock()
	a.connections[peerName] = peerID
	a.mu.Unlock()
	
	a.logger.Infof("✅ [P2P Direct] Successfully connected to peer %s via P2P at %s", peerName, addr)
	return nil
}


func (a *P2PAgent) ConnectToPeerPure(peerName string) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}
	
	a.logger.Infof("🌐 [P2P Pure] Connecting to %s using pure P2P discovery", peerName)
	
	
	a.mu.RLock()
	if existingPeerID, exists := a.connections[peerName]; exists {
		a.mu.RUnlock()
		a.logger.Infof("✅ [P2P Pure] Peer %s already connected (%s)", peerName, existingPeerID)
		return nil
	}
	a.mu.RUnlock()
	
	
	connectedPeers := a.host.Network().Peers()
	a.logger.Infof("🔍 [P2P Pure] Scanning %d connected peers for %s", len(connectedPeers), peerName)
	
	for _, connectedPeerID := range connectedPeers {
		
		if err := a.identifyAndConnectPeer(peerName, connectedPeerID); err == nil {
			a.logger.Infof("✅ [P2P Pure] Successfully identified and connected to %s (%s)", peerName, connectedPeerID)
			return nil
		}
	}
	
	
	if err := a.connectToKnownPeerAddresses(peerName); err == nil {
		return nil
	}
	
	return fmt.Errorf("pure P2P connection failed: peer %s not found in network", peerName)
}


func (a *P2PAgent) identifyAndConnectPeer(expectedName string, peerID peer.ID) error {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	
	
	stream, err := a.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to open stream to %s: %w", peerID, err)
	}
	defer stream.Close()
	
	
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return fmt.Errorf("failed to read card from %s: %w", peerID, err)
	}
	
	var card ExtendedAgentCard
	if err := json.Unmarshal(responseData, &card); err != nil {
		return fmt.Errorf("failed to parse card from %s: %w", peerID, err)
	}
	
	
	if card.Name == expectedName {
		
		a.mu.Lock()
		a.connections[expectedName] = peerID
		a.mu.Unlock()
		
		a.logger.Infof("🎯 [P2P Pure] Identified peer %s (%s) via card exchange", expectedName, peerID)
		return nil
	}
	
	return fmt.Errorf("peer %s has name %s, not %s", peerID, card.Name, expectedName)
}

func (a *P2PAgent) connectToKnownPeerAddresses(peerName string) error {

	knownAddresses := map[string][]string{
		"go-agent-1": {"/ip4/172.20.0.2/tcp/4001"},
		"go-agent-2": {"/ip4/172.20.0.3/tcp/4002"},
	}
	
	addresses, exists := knownAddresses[peerName]
	if !exists {
		return fmt.Errorf("no known addresses for peer %s", peerName)
	}
	
	a.logger.Infof("🔍 [P2P Pure] Trying to connect to %s at known addresses", peerName)
	
	for _, addrStr := range addresses {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		
		if peerInfo, err := a.discoverPeerIDFromHTTP(peerName, addrStr); err == nil {
			
			fullAddr := fmt.Sprintf("%s/p2p/%s", addrStr, peerInfo)
			fullMultiaddr, err := multiaddr.NewMultiaddr(fullAddr)
			if err != nil {
				a.logger.Warnf("⚠️ [P2P Pure] Invalid full multiaddr %s: %v", fullAddr, err)
				continue
			}
			
			addrInfo, err := peer.AddrInfoFromP2pAddr(fullMultiaddr)
			if err != nil {
				a.logger.Warnf("⚠️ [P2P Pure] Invalid multiaddr %s: %v", fullAddr, err)
				continue
			}
			
			if err := a.host.Connect(ctx, *addrInfo); err != nil {
				a.logger.Warnf("⚠️ [P2P Pure] Failed to connect to %s: %v", fullAddr, err)
				continue
			}
			
			a.logger.Infof("🎯 [P2P Pure] Successfully connected to peer at %s", fullAddr)
			
			if err := a.identifyAndConnectPeer(peerName, addrInfo.ID); err == nil {
				return nil
			}
		}
	}
	
	return fmt.Errorf("failed to connect to %s at any known address", peerName)
}

func (a *P2PAgent) discoverPeerIDFromHTTP(peerName string, address string) (string, error) {
	httpPorts := map[string]string{
		"/ip4/172.20.0.2/tcp/4001": "8000", 
		"/ip4/172.20.0.3/tcp/4002": "8001", 
	}
	
	httpPort, exists := httpPorts[address]
	if !exists {
		return "", fmt.Errorf("no HTTP port mapping for address %s", address)
	}
	
	parts := strings.Split(address, "/")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid multiaddr format: %s", address)
	}
	ip := parts[2] 
	
	url := fmt.Sprintf("http://%s:%s/p2p/info", ip, httpPort)
	
	a.logger.Infof("🔍 [P2P Pure] Discovering peer ID from %s", url)
	
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to get peer info from %s: %v", url, err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error %d from %s", resp.StatusCode, url)
	}
	
	var peerInfo struct {
		PeerID string `json:"peer_id"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&peerInfo); err != nil {
		return "", fmt.Errorf("failed to decode peer info: %v", err)
	}
	
	if peerInfo.PeerID == "" {
		return "", fmt.Errorf("empty peer ID received from %s", url)
	}
	
	a.logger.Infof("🎯 [P2P Pure] Discovered peer ID: %s", peerInfo.PeerID)
	return peerInfo.PeerID, nil
}

func (a *P2PAgent) ConnectToPeerBidirectional(peerName string) error {
	return a.ConnectToPeerPure(peerName)
}


func (a *P2PAgent) RequestCard(peerName string) (*ExtendedAgentCard, error) {
	if a.host == nil {
		return nil, fmt.Errorf("P2P host not initialized")
	}
	
	a.mu.RLock()
	peerID, exists := a.connections[peerName]
	a.mu.RUnlock()
	
	if !exists {
		err := a.ConnectToPeer(peerName)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to peer %s: %w", peerName, err)
		}
		
		a.mu.RLock()
		peerID = a.connections[peerName]
		a.mu.RUnlock()
	}
	
	a.logger.Infof("[P2P] Requesting card from peer %s (%s)", peerName, peerID)
	
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	
	a.logger.Infof("[P2P] Opening stream with protocol %s to peer %s", CardProtocol, peerID)
	stream, err := a.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		a.logger.Errorf("[P2P] Failed to open stream: %v", err)
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()
	
	a.logger.Infof("[P2P] Stream opened successfully to %s, reading card data...", peerID)
	
	responseData, err := io.ReadAll(stream)
	if err != nil {
		a.logger.Errorf(" [P2P] Failed to read from stream: %v", err)
		return nil, fmt.Errorf("failed to read stream: %w", err)
	}
	
	a.logger.Infof(" [P2P] Received %d bytes from peer %s via P2P stream", len(responseData), peerID)
	
	var card ExtendedAgentCard
	err = json.Unmarshal(responseData, &card)
	if err != nil {
		var errorResp map[string]interface{}
		if json.Unmarshal(responseData, &errorResp) == nil {
			if errorMsg, ok := errorResp["error"].(string); ok {
				return nil, fmt.Errorf("peer error: %s", errorMsg)
			}
		}
		return nil, fmt.Errorf("failed to unmarshal card response: %w", err)
	}
	
	a.logger.Infof("🎉 [P2P] Successfully received card from peer %s via P2P stream protocol %s", peerName, CardProtocol)
	if len(card.MCPServers) == 0 && card.Skills != nil {
		var baseCard AgentCard
		if err := json.Unmarshal(responseData, &baseCard); err == nil {
			card = ExtendedAgentCard{
				AgentCard:  baseCard,
				MCPServers: []MCPCapability{},
			}
		}
	}
	
	return &card, nil
}

func (a *P2PAgent) StartHTTPServer() error {
	port := getEnv("HTTP_PORT", strconv.Itoa(DefaultHTTPPort))
	
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	
	
	
	r.GET("/card", func(c *gin.Context) {
		c.JSON(http.StatusOK, a.agentCard)
	})
	
	
	r.GET("/.well-known/agent-card.json", func(c *gin.Context) {
		
		streaming := true
		pushNotifications := false
		stateTransitionHistory := false
		
		card := AgentCard{
			Name:            getEnv("AGENT_NAME", "go-agent"),
			Version:         getEnv("AGENT_VERSION", "1.0.0"),
			ProtocolVersion: "1.0.0",
			Description:     getEnv("AGENT_DESCRIPTION", "Go P2P Agent with A2A support"),
			URL:             fmt.Sprintf("http://localhost:%s", getEnv("HTTP_PORT", "8000")),
			Provider: &AgentProvider{
				Organization: "Praxis",
				URL:          "https://praxis.ai",
			},
			Capabilities: AgentCapabilities{
				Streaming:              &streaming,
				PushNotifications:      &pushNotifications,
				StateTransitionHistory: &stateTransitionHistory,
			},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text", "json"},
			Skills: []AgentSkill{
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
					Description: "Natural language processing with OpenAI GPT-4o",
					Tags:        []string{"llm", "ai", "nlp"},
					InputModes:  []string{"text"},
					OutputModes: []string{"text", "json"},
				},
			},
		}
		
		c.JSON(http.StatusOK, card)
	})
	
	
	r.GET("/a2a/agent-card", func(c *gin.Context) {
		
		streaming := true
		pushNotifications := false
		stateTransitionHistory := false
		
		card := AgentCard{
			Name:            getEnv("AGENT_NAME", "go-agent"),
			Version:         getEnv("AGENT_VERSION", "1.0.0"),
			ProtocolVersion: "1.0.0",
			Description:     getEnv("AGENT_DESCRIPTION", "Go P2P Agent with A2A support"),
			URL:             fmt.Sprintf("http://localhost:%s", getEnv("HTTP_PORT", "8000")),
			Provider: &AgentProvider{
				Organization: "Praxis",
				URL:          "https://praxis.ai",
			},
			Capabilities: AgentCapabilities{
				Streaming:              &streaming,
				PushNotifications:      &pushNotifications,
				StateTransitionHistory: &stateTransitionHistory,
			},
			DefaultInputModes:  []string{"text"},
			DefaultOutputModes: []string{"text", "json"},
			Skills: []AgentSkill{
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
					Description: "Natural language processing with OpenAI GPT-4o",
					Tags:        []string{"llm", "ai", "nlp"},
					InputModes:  []string{"text"},
					OutputModes: []string{"text", "json"},
				},
			},
		}
		
		c.JSON(http.StatusOK, card)
	})
	
	
	r.GET("/health", func(c *gin.Context) {
		status := gin.H{
			"status": "healthy",
			"timestamp": time.Now().Unix(),
			"services": gin.H{
				"p2p": a.host != nil,
				"mcp": a.mcpBridge != nil,
				"llm": a.llmAgent != nil,
			},
		}
		c.JSON(http.StatusOK, status)
	})
	
	r.GET("/p2p/info", func(c *gin.Context) {
		if a.host == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "P2P not initialized"})
			return
		}
		
		addresses := make([]string, len(a.host.Addrs()))
		for i, addr := range a.host.Addrs() {
			addresses[i] = addr.String()
		}
		
		c.JSON(http.StatusOK, gin.H{
			"peer_id":   a.host.ID().String(),
			"addresses": addresses,
		})
	})
	
	r.GET("/p2p/status", func(c *gin.Context) {
		if a.host == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "P2P not initialized"})
			return
		}
		
		a.mu.RLock()
		connectionCount := len(a.connections)
		connectionNames := make([]string, 0, len(a.connections))
		for name := range a.connections {
			connectionNames = append(connectionNames, name)
		}
		a.mu.RUnlock()
		
		c.JSON(http.StatusOK, gin.H{
			"peer_id":     a.host.ID().String(),
			"connections": connectionCount,
			"connected_peers": connectionNames,
		})
	})
	
	r.POST("/p2p/connect/:peer_name", func(c *gin.Context) {
		peerName := c.Param("peer_name")
		
		err := a.ConnectToPeer(peerName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status":  "connected",
			"peer":    peerName,
			"message": fmt.Sprintf("Successfully connected to %s", peerName),
		})
	})
	
	r.POST("/p2p/connect-bidirectional/:peer_name", func(c *gin.Context) {
		peerName := c.Param("peer_name")
		
		err := a.ConnectToPeerBidirectional(peerName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status":  "connected",
			"peer":    peerName,
			"method":  "bidirectional",
			"message": fmt.Sprintf("Successfully established bidirectional connection with %s", peerName),
		})
	})
	
	r.POST("/p2p/connect-pure/:peer_name", func(c *gin.Context) {
		peerName := c.Param("peer_name")
		
		err := a.ConnectToPeerPure(peerName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status":  "connected",
			"peer":    peerName,
			"method":  "pure_p2p",
			"message": fmt.Sprintf("Successfully connected to %s using pure P2P discovery", peerName),
		})
	})
	
	r.POST("/p2p/connect-direct/:peer_name/:peer_id", func(c *gin.Context) {
		peerName := c.Param("peer_name")
		peerIDStr := c.Param("peer_id")
		
		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid peer ID: %v", err)})
			return
		}
		
		err = a.ConnectToPeerDirect(peerName, peerID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status":  "connected",
			"peer":    peerName,
			"peer_id": peerID.String(),
			"method":  "direct",
			"message": fmt.Sprintf("Successfully connected to %s via direct P2P", peerName),
		})
	})
	
	r.POST("/p2p/connect-with-addr/:peer_name/:peer_id", func(c *gin.Context) {
		peerName := c.Param("peer_name")
		peerIDStr := c.Param("peer_id")
		
		var request struct {
			Addr string `json:"addr" binding:"required"`
		}
		
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "addr field is required"})
			return
		}
		
		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid peer ID: %v", err)})
			return
		}
		
		err = a.ConnectToPeerDirectWithAddr(peerName, peerID, request.Addr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status":  "connected",
			"peer":    peerName,
			"peer_id": peerID.String(),
			"addr":    request.Addr,
			"method":  "direct_with_addr",
			"message": fmt.Sprintf("Successfully connected to %s via P2P at %s", peerName, request.Addr),
		})
	})
	
	r.POST("/p2p/request-card/:peer_name", func(c *gin.Context) {
		peerName := c.Param("peer_name")
		
		card, err := a.RequestCard(peerName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"peer":   peerName,
			"card":   card,
		})
	})
	
	r.POST("/echo", func(c *gin.Context) {
		var input struct {
			Text string `json:"text"`
		}
		
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"result": fmt.Sprintf("Echo: %s", input.Text),
		})
	})
	
	
	r.POST("/llm/chat", func(c *gin.Context) {
		a.logger.Infof("📥 [LLM API] Received POST request to /llm/chat")
		if a.llmAgent == nil {
			a.logger.Errorf("❌ [LLM API] LLM agent not available")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM agent not available"})
			return
		}
		
		var req LLMRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			a.logger.Errorf("❌ [LLM API] JSON binding failed: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		a.logger.Infof("📋 [LLM API] Parsed request: ID='%s', UserInput='%s' (len=%d)", req.ID, req.UserInput, len(req.UserInput))
		
		
		a.logger.Infof("🔍 [LLM API] Validating request: UserInput='%s', length=%d", req.UserInput, len(req.UserInput))
		
		if req.UserInput == "" || strings.TrimSpace(req.UserInput) == "" {
			a.logger.Warnf("⚠️ [LLM API] Empty user_input detected, replacing with space to prevent null content")
			req.UserInput = " "
		}
		
		
		if req.ID == "" {
			req.ID = fmt.Sprintf("llm_%d", time.Now().UnixNano())
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		
		resp, err := a.llmAgent.ProcessRequest(ctx, &req)
		if err != nil {
			a.logger.Errorf("❌ [LLM API] Request failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, resp)
	})
	
	r.GET("/llm/tools", func(c *gin.Context) {
		if a.llmAgent == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM agent not available"})
			return
		}
		
		tools := a.llmAgent.getAvailableTools()
		c.JSON(http.StatusOK, gin.H{
			"tools": tools,
			"count": len(tools),
		})
	})
	
	r.GET("/llm/status", func(c *gin.Context) {
		if a.llmAgent == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "unavailable",
				"enabled": a.llmEnabled,
			})
			return
		}
		
		metrics := a.llmAgent.GetMetrics()
		
		c.JSON(http.StatusOK, gin.H{
			"status":  "active",
			"enabled": a.llmEnabled,
			"metrics": metrics,
		})
	})
	
	r.GET("/llm/health", func(c *gin.Context) {
		if a.llmAgent == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM agent not available"})
			return
		}
		
		if err := a.llmAgent.Health(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
		})
	})
	
	r.GET("/mcp/status", func(c *gin.Context) {
		if a.mcpBridge == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
			return
		}
		
		stats := a.mcpBridge.GetStats()
		c.JSON(http.StatusOK, gin.H{
			"status": "active",
			"stats":  stats,
		})
	})
	
	r.GET("/mcp/tools", func(c *gin.Context) {
		if a.mcpBridge == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
			return
		}
		
		tools := a.mcpBridge.ListAllTools()
		c.JSON(http.StatusOK, gin.H{
			"tools": tools,
		})
	})
	
	r.GET("/mcp/resources", func(c *gin.Context) {
		if a.mcpBridge == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
			return
		}
		
		resources := a.mcpBridge.ListAllResources()
		c.JSON(http.StatusOK, gin.H{
			"resources": resources,
		})
	})
	
	r.POST("/mcp/invoke/:peer_name/:server_name/:tool_name", func(c *gin.Context) {
		if a.mcpBridge == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
			return
		}
		
		peerName := c.Param("peer_name")
		serverName := c.Param("server_name")
		toolName := c.Param("tool_name")
		
		var params map[string]interface{}
		if err := c.ShouldBindJSON(&params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		a.mu.RLock()
		peerID, exists := a.connections[peerName]
		a.mu.RUnlock()
		
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("peer %s not connected", peerName)})
			return
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		client := a.mcpBridge.GetClient()
		response, err := client.InvokeTool(ctx, peerID, serverName, toolName, params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"status":   "success",
			"response": response,
		})
	})
	
	r.GET("/mcp/peers/:peer_name/tools", func(c *gin.Context) {
		if a.mcpBridge == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
			return
		}
		
		peerName := c.Param("peer_name")
		
		a.mu.RLock()
		peerID, exists := a.connections[peerName]
		a.mu.RUnlock()
		
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("peer %s not connected", peerName)})
			return
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		client := a.mcpBridge.GetClient()
		tools, err := client.ListRemoteTools(ctx, peerID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"peer":  peerName,
			"tools": tools,
		})
	})
	
	a.httpServer = &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}
	
	a.logger.Infof("Starting HTTP server on port %s", port)
	
	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Errorf("HTTP server error: %v", err)
		}
	}()
	
	return nil
}


func (a *P2PAgent) GetPeerByName(peerName string) (peer.ID, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	peerID, exists := a.connections[peerName]
	if !exists {
		
		a.mu.RUnlock()
		
		
		if err := a.ConnectToPeer(peerName); err != nil {
			return "", fmt.Errorf("peer '%s' not found and auto-connect failed: %w", peerName, err)
		}
		
		
		a.mu.RLock()
		peerID, exists = a.connections[peerName]
		if !exists {
			return "", fmt.Errorf("peer '%s' not found even after connection attempt", peerName)
		}
	}
	
	return peerID, nil
}

func (a *P2PAgent) Shutdown() error {
	a.logger.Info("Shutting down P2P agent...")
	
	a.cancel()
	
	if a.discovery != nil {
		if err := a.discovery.Shutdown(); err != nil {
			a.logger.Errorf("Failed to shutdown peer discovery: %v", err)
		}
	}
	
	if a.llmAgent != nil {
		a.logger.Info("🤖 [P2P Agent] Shutting down LLM agent...")
		
		a.logger.Info("✅ [P2P Agent] LLM agent shutdown complete")
	}
	
	if a.mcpBridge != nil {
		if err := a.mcpBridge.Shutdown(); err != nil {
			a.logger.Errorf("Failed to shutdown MCP bridge: %v", err)
		}
	}
	
	if a.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		if err := a.httpServer.Shutdown(ctx); err != nil {
			a.logger.Errorf("Failed to shutdown HTTP server: %v", err)
		}
	}
	
	if a.host != nil {
		if err := a.host.Close(); err != nil {
			a.logger.Errorf("Failed to close P2P host: %v", err)
		}
	}
	
	a.logger.Info("P2P agent shutdown complete")
	return nil
}

func main() {
	agent, err := NewP2PAgent()
	if err != nil {
		logrus.Fatalf("Failed to create P2P agent: %v", err)
	}
	
	if err := agent.StartP2P(); err != nil {
		logrus.Fatalf("Failed to start P2P node: %v", err)
	}
	
	if err := agent.StartHTTPServer(); err != nil {
		logrus.Fatalf("Failed to start HTTP server: %v", err)
	}
	
	<-agent.ctx.Done()
	
	agent.Shutdown()
}
