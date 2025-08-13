package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
)

const (
	CardProtocol = protocol.ID("/ai-agent/card/1.0.0")
	
	DefaultP2PPort = 0
	DefaultHTTPPort = 8000
)

type AgentSkill struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Path        string      `json:"path"`
	Method      string      `json:"method"`
	ParamsSchema interface{} `json:"params_schema"`
	InputSchema  interface{} `json:"input_schema"`
	OutputSchema interface{} `json:"output_schema"`
}

type AgentCard struct {
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Skills      []AgentSkill `json:"skills"`
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
}

func NewP2PAgent() (*P2PAgent, error) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	
	ctx, cancel := context.WithCancel(context.Background())
	
	agentName := getEnv("AGENT_NAME", "go-agent")
	agentVersion := getEnv("AGENT_VERSION", "1.0.0")
	agentDescription := getEnv("AGENT_DESCRIPTION", "Go P2P Agent")
	
	baseCard := AgentCard{
		Name:        agentName,
		Version:     agentVersion,
		Description: agentDescription,
		Skills: []AgentSkill{
			{
				ID:          "echo",
				Name:        "Echo",
				Description: "Simple echo skill",
				Path:        "/echo",
				Method:      "POST",
				ParamsSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"message": map[string]interface{}{
							"type": "string",
						},
					},
				},
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{
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
		},
	}
	
	card := ExtendedAgentCard{
		AgentCard:  baseCard,
		MCPServers: []MCPCapability{}, // Will be populated when MCP bridge starts
	}
	
	mcpEnabled := strings.ToLower(getEnv("MCP_ENABLED", "true")) == "true"
	
	agent := &P2PAgent{
		agentCard:   card,
		connections: make(map[string]peer.ID),
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		mcpEnabled:  mcpEnabled,
	}
	
	return agent, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
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
	
	a.host.SetStreamHandler(CardProtocol, a.handleCardRequest)
	
	if err := a.StartMCP(); err != nil {
		a.logger.Errorf("❌ [P2P Agent] Failed to start MCP bridge: %v", err)
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
	
	a.logger.Infof("Attempting to connect to peer: %s", peerName)
	
	httpURL := fmt.Sprintf("http://%s:8000/p2p/info", peerName)
	
	var peerInfo struct {
		PeerID    string   `json:"peer_id"`
		Addresses []string `json:"addresses"`
	}
	
	for attempts := 0; attempts < 5; attempts++ {
		resp, err := http.Get(httpURL)
		if err != nil {
			a.logger.Warnf("Attempt %d: Failed to get peer info from %s: %v", attempts+1, httpURL, err)
			time.Sleep(time.Duration(attempts+1) * time.Second)
			continue
		}
		
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			a.logger.Warnf("Attempt %d: HTTP error %d from %s", attempts+1, resp.StatusCode, httpURL)
			time.Sleep(time.Duration(attempts+1) * time.Second)
			continue
		}
		
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			a.logger.Warnf("Attempt %d: Failed to read response: %v", attempts+1, err)
			time.Sleep(time.Duration(attempts+1) * time.Second)
			continue
		}
		
		err = json.Unmarshal(body, &peerInfo)
		if err != nil {
			a.logger.Warnf("Attempt %d: Failed to unmarshal peer info: %v", attempts+1, err)
			time.Sleep(time.Duration(attempts+1) * time.Second)
			continue
		}
		
		break
	}
	
	if peerInfo.PeerID == "" {
		return fmt.Errorf("failed to get peer info for %s", peerName)
	}
	
	peerID, err := peer.Decode(peerInfo.PeerID)
	if err != nil {
		return fmt.Errorf("failed to decode peer ID: %w", err)
	}
	
	for _, addrStr := range peerInfo.Addresses {
		_, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			a.logger.Warnf("Failed to parse address %s: %v", addrStr, err)
			continue
		}
		
		peerAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("%s/p2p/%s", addrStr, peerInfo.PeerID))
		if err != nil {
			a.logger.Warnf("Failed to create peer address: %v", err)
			continue
		}
		
	
		addrInfo, err := peer.AddrInfoFromP2pAddr(peerAddr)
		if err != nil {
			a.logger.Warnf("Failed to get addr info: %v", err)
			continue
		}
		
		
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		err = a.host.Connect(ctx, *addrInfo)
		cancel()
		
		if err != nil {
			a.logger.Warnf("Failed to connect to %s: %v", peerAddr, err)
			continue
		}
		
		a.logger.Infof("Successfully connected to peer %s (%s)", peerName, peerID)
		
	
		a.mu.Lock()
		a.connections[peerName] = peerID
		a.mu.Unlock()
		
		return nil
	}
	
	return fmt.Errorf("failed to connect to any address for peer %s", peerName)
}

// ConnectToPeerDirect connects to a peer using existing P2P connection
// This bypasses HTTP API and uses libp2p directly for bidirectional connectivity
func (a *P2PAgent) ConnectToPeerDirect(peerName string, peerID peer.ID) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}
	
	a.logger.Infof("🔗 [P2P Direct] Connecting to peer %s (%s) via existing P2P", peerName, peerID)
	
	// Check if we can create a stream to this peer (validates connection)
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	
	// Try to open a test stream to verify connectivity
	stream, err := a.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to establish direct P2P connection to %s: %w", peerID, err)
	}
	stream.Close()
	
	// Store the connection
	a.mu.Lock()
	a.connections[peerName] = peerID
	a.mu.Unlock()
	
	a.logger.Infof("✅ [P2P Direct] Successfully connected to peer %s via P2P", peerName)
	return nil
}

// ConnectToPeerPure connects to peer using pure P2P without HTTP dependencies  
func (a *P2PAgent) ConnectToPeerPure(peerName string) error {
	if a.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}
	
	a.logger.Infof("🌐 [P2P Pure] Connecting to %s using pure P2P discovery", peerName)
	
	// Check if peer is already connected
	a.mu.RLock()
	if existingPeerID, exists := a.connections[peerName]; exists {
		a.mu.RUnlock()
		a.logger.Infof("✅ [P2P Pure] Peer %s already connected (%s)", peerName, existingPeerID)
		return nil
	}
	a.mu.RUnlock()
	
	// Get all currently connected peers from libp2p
	connectedPeers := a.host.Network().Peers()
	a.logger.Infof("🔍 [P2P Pure] Scanning %d connected peers for %s", len(connectedPeers), peerName)
	
	for _, connectedPeerID := range connectedPeers {
		// Try to request card via P2P stream to identify the peer
		if err := a.identifyAndConnectPeer(peerName, connectedPeerID); err == nil {
			a.logger.Infof("✅ [P2P Pure] Successfully identified and connected to %s (%s)", peerName, connectedPeerID)
			return nil
		}
	}
	
	// If not found in existing connections, try known peer addresses
	if err := a.connectToKnownPeerAddresses(peerName); err == nil {
		return nil
	}
	
	return fmt.Errorf("pure P2P connection failed: peer %s not found in network", peerName)
}

// identifyAndConnectPeer tries to identify a peer by requesting its card via P2P
func (a *P2PAgent) identifyAndConnectPeer(expectedName string, peerID peer.ID) error {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	
	// Try to open a stream and request the peer's card
	stream, err := a.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to open stream to %s: %w", peerID, err)
	}
	defer stream.Close()
	
	// Read the card response
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return fmt.Errorf("failed to read card from %s: %w", peerID, err)
	}
	
	var card ExtendedAgentCard
	if err := json.Unmarshal(responseData, &card); err != nil {
		return fmt.Errorf("failed to parse card from %s: %w", peerID, err)
	}
	
	// Check if this is the peer we're looking for
	if card.Name == expectedName {
		// Store the connection
		a.mu.Lock()
		a.connections[expectedName] = peerID
		a.mu.Unlock()
		
		a.logger.Infof("🎯 [P2P Pure] Identified peer %s (%s) via card exchange", expectedName, peerID)
		return nil
	}
	
	return fmt.Errorf("peer %s has name %s, not %s", peerID, card.Name, expectedName)
}

// connectToKnownPeerAddresses tries to connect to known peer addresses for discovery
func (a *P2PAgent) connectToKnownPeerAddresses(peerName string) error {
	// Known peer mappings for our setup (can be made configurable)
	knownPeers := map[string][]string{
		"go-agent-1": {"/ip4/172.20.0.2/tcp/4001"},
		"go-agent-2": {"/ip4/172.20.0.3/tcp/4002"},
	}
	
	addresses, exists := knownPeers[peerName]
	if !exists {
		return fmt.Errorf("no known addresses for peer %s", peerName)
	}
	
	a.logger.Infof("🔍 [P2P Pure] Trying to connect to %s at known addresses", peerName)
	
	for _, addrStr := range addresses {
		addr, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			a.logger.Warnf("⚠️ [P2P Pure] Invalid address %s: %v", addrStr, err)
			continue
		}
		
		// Extract peer ID from connection after successful dial
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		
		// This will trigger connection through libp2p transport
		addrInfo, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			// If no peer ID in address, try to connect and discover
			if err := a.host.Connect(ctx, peer.AddrInfo{Addrs: []multiaddr.Multiaddr{addr}}); err != nil {
				a.logger.Warnf("⚠️ [P2P Pure] Failed to connect to %s: %v", addrStr, err)
				continue
			}
			
			// After connection, check connected peers
			connectedPeers := a.host.Network().Peers()
			for _, connectedPeerID := range connectedPeers {
				if err := a.identifyAndConnectPeer(peerName, connectedPeerID); err == nil {
					return nil
				}
			}
		} else {
			// Connect directly with peer ID
			if err := a.host.Connect(ctx, *addrInfo); err != nil {
				a.logger.Warnf("⚠️ [P2P Pure] Failed to connect to %s (%s): %v", peerName, addrInfo.ID, err)
				continue
			}
			
			// Verify this is the right peer
			if err := a.identifyAndConnectPeer(peerName, addrInfo.ID); err == nil {
				return nil
			}
		}
	}
	
	return fmt.Errorf("failed to connect to %s at any known address", peerName)
}

// ConnectToPeerBidirectional establishes bidirectional P2P connection (DEPRECATED - use ConnectToPeerPure)
func (a *P2PAgent) ConnectToPeerBidirectional(peerName string) error {
	// Use pure P2P method instead of HTTP-dependent method
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
	
	// setupDocsRoutes(r) // Removed docs routes for production
	
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	
	r.GET("/card", func(c *gin.Context) {
		c.JSON(http.StatusOK, a.agentCard)
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

func (a *P2PAgent) Shutdown() error {
	a.logger.Info("Shutting down P2P agent...")
	
	a.cancel()
	
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
