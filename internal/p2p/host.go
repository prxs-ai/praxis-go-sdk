package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	"github.com/sirupsen/logrus"

	"praxis-go-sdk/internal/config"
	"praxis-go-sdk/pkg/agentcard"
	"praxis-go-sdk/pkg/utils"
)

// P2PHost implements the Host interface
type P2PHost struct {
	host        host.Host
	agentCard   *agentcard.ExtendedAgentCard
	connections map[string]peer.ID
	discovery   Discovery
	logger      *logrus.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	config      *config.P2PConfig
}

// NewP2PHost creates a new P2P host
func NewP2PHost(cfg *config.P2PConfig, card *agentcard.ExtendedAgentCard, logger *logrus.Logger) (Host, error) {
	ctx, cancel := context.WithCancel(context.Background())

	host := &P2PHost{
		agentCard:   card,
		connections: make(map[string]peer.ID),
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		config:      cfg,
	}

	return host, nil
}

// Start initializes and starts the P2P host
func (h *P2PHost) Start() error {
	h.logger.Info("Starting P2P host...")

	containerIP := utils.GetContainerIP()
	h.logger.Infof("Using container IP: %s", containerIP)

	// Use configured port or default
	p2pPort := h.config.Port
	if p2pPort == 0 {
		p2pPort = DefaultP2PPort
	}

	// Create listen address
	listenAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", containerIP, p2pPort))
	if err != nil {
		return fmt.Errorf("failed to create listen address: %w", err)
	}

	// Configure libp2p options
	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddr),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
	}

	// Add security if enabled
	if h.config.Secure {
		h.logger.Info("Using Noise security transport")
		opts = append(opts, libp2p.Security(noise.ID, noise.New))
	} else {
		h.logger.Warning("Using INSECURE transport!")
	}

	// Create the libp2p host
	host, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	h.host = host
	h.logger.Infof("P2P host created with ID: %s", host.ID())
	h.logger.Infof("Listening on addresses: %v", host.Addrs())

	// Register protocol handlers
	h.host.SetStreamHandler(CardProtocol, h.handleCardRequest)
	h.logger.Info("Registered stream handler for card protocol")

	// Initialize discovery
	discoveryConfig := DiscoveryConfig{
		Enabled:        h.config.EnableMDNS || h.config.EnableDHT,
		Rendezvous:     h.config.Rendezvous,
		EnableMDNS:     h.config.EnableMDNS,
		EnableDHT:      h.config.EnableDHT,
		BootstrapNodes: h.config.BootstrapNodes,
	}

	discovery, err := NewDiscovery(h.host, discoveryConfig, h.logger)
	if err != nil {
		h.logger.Errorf("Failed to initialize discovery: %v", err)
	} else {
		h.discovery = discovery
		if err := h.discovery.Start(); err != nil {
			h.logger.Errorf("Failed to start discovery: %v", err)
		} else {
			h.logger.Infof("Peer discovery started with rendezvous: %s", h.config.Rendezvous)
		}
	}

	return nil
}

// Shutdown stops the P2P host and cleans up resources
func (h *P2PHost) Shutdown() error {
	h.logger.Info("Shutting down P2P host...")

	// Cancel context to signal shutdown
	h.cancel()

	// Shutdown discovery
	if h.discovery != nil {
		if err := h.discovery.Shutdown(); err != nil {
			h.logger.Errorf("Failed to shutdown discovery: %v", err)
		}
	}

	// Close libp2p host
	if h.host != nil {
		if err := h.host.Close(); err != nil {
			h.logger.Errorf("Failed to close libp2p host: %v", err)
			return err
		}
	}

	h.logger.Info("P2P host shutdown complete")
	return nil
}

// GetID returns the peer ID of this host
func (h *P2PHost) GetID() peer.ID {
	if h.host == nil {
		return ""
	}
	return h.host.ID()
}

// GetAddresses returns the multiaddresses of this host
func (h *P2PHost) GetAddresses() []string {
	if h.host == nil {
		return nil
	}

	addresses := make([]string, len(h.host.Addrs()))
	for i, addr := range h.host.Addrs() {
		addresses[i] = addr.String()
	}
	return addresses
}

// ConnectToPeer connects to a peer by name using discovery
func (h *P2PHost) ConnectToPeer(peerName string) error {
	if h.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}

	if h.discovery == nil {
		return fmt.Errorf("peer discovery not initialized")
	}

	h.logger.Infof("Attempting to connect to peer: %s", peerName)

	// Try to connect via discovery
	err := h.discovery.ConnectToPeerByName(peerName)
	if err != nil {
		return fmt.Errorf("failed to connect via discovery: %w", err)
	}

	// Resolve peer ID
	peerID, err := h.discovery.ResolvePeerName(peerName)
	if err != nil {
		return fmt.Errorf("failed to resolve peer name: %w", err)
	}

	// Store connection
	h.mu.Lock()
	h.connections[peerName] = peerID
	h.mu.Unlock()

	h.logger.Infof("Successfully connected to peer: %s (%s)", peerName, peerID)
	return nil
}

// ConnectToPeerDirect connects to a peer by ID directly
func (h *P2PHost) ConnectToPeerDirect(peerName string, peerID peer.ID) error {
	if h.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}

	h.logger.Infof("Connecting to peer %s (%s) directly", peerName, peerID)

	// Try to establish a connection
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	// Test connection with a stream
	stream, err := h.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to establish direct connection to %s: %w", peerID, err)
	}
	stream.Close()

	// Store connection
	h.mu.Lock()
	h.connections[peerName] = peerID
	h.mu.Unlock()

	h.logger.Infof("Successfully connected to peer %s via direct P2P", peerName)
	return nil
}

// ConnectToPeerWithAddr connects to a peer with a known address
func (h *P2PHost) ConnectToPeerWithAddr(peerName string, peerID peer.ID, addr string) error {
	if h.host == nil {
		return fmt.Errorf("P2P host not initialized")
	}

	h.logger.Infof("Connecting to peer %s (%s) at %s", peerName, peerID, addr)

	// Parse multiaddress
	maddr, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return fmt.Errorf("invalid multiaddress %s: %w", addr, err)
	}

	// Add to peerstore
	h.host.Peerstore().AddAddr(peerID, maddr, time.Hour)
	h.logger.Infof("Added address %s for peer %s to peerstore", addr, peerID)

	// Connect to peer
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()

	err = h.host.Connect(ctx, peer.AddrInfo{ID: peerID, Addrs: []multiaddr.Multiaddr{maddr}})
	if err != nil {
		return fmt.Errorf("failed to connect to peer %s at %s: %w", peerID, addr, err)
	}

	// Test connection with a stream
	stream, err := h.host.NewStream(ctx, peerID, CardProtocol)
	if err != nil {
		return fmt.Errorf("failed to establish stream to %s: %w", peerID, err)
	}
	stream.Close()

	// Store connection
	h.mu.Lock()
	h.connections[peerName] = peerID
	h.mu.Unlock()

	h.logger.Infof("Successfully connected to peer %s via P2P at %s", peerName, addr)
	return nil
}

// GetPeerByName looks up a peer ID by name
func (h *P2PHost) GetPeerByName(peerName string) (peer.ID, error) {
	h.mu.RLock()
	peerID, exists := h.connections[peerName]
	h.mu.RUnlock()

	if !exists {
		// Try to connect if not found
		h.mu.RUnlock()

		if err := h.ConnectToPeer(peerName); err != nil {
			return "", fmt.Errorf("peer '%s' not found and auto-connect failed: %w", peerName, err)
		}

		h.mu.RLock()
		peerID, exists = h.connections[peerName]
		if !exists {
			return "", fmt.Errorf("peer '%s' not found even after connection attempt", peerName)
		}
	}

	return peerID, nil
}

// RequestData sends a request to a peer with the given protocol
func (h *P2PHost) RequestData(peerName string, protocolID protocol.ID) ([]byte, error) {
	if h.host == nil {
		return nil, fmt.Errorf("P2P host not initialized")
	}

	// Get peer ID
	peerID, err := h.GetPeerByName(peerName)
	if err != nil {
		return nil, err
	}

	h.logger.Infof("Requesting data from peer %s (%s) with protocol %s", peerName, peerID, protocolID)

	// Open stream with timeout
	ctx, cancel := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancel()

	stream, err := h.host.NewStream(ctx, peerID, protocolID)
	if err != nil {
		h.logger.Errorf("Failed to open stream: %v", err)
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Read response
	responseData, err := io.ReadAll(stream)
	if err != nil {
		h.logger.Errorf("Failed to read from stream: %v", err)
		return nil, fmt.Errorf("failed to read stream: %w", err)
	}

	h.logger.Infof("Received %d bytes from peer %s via P2P stream", len(responseData), peerID)
	return responseData, nil
}

// handleCardRequest handles requests for the agent card
func (h *P2PHost) handleCardRequest(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	timestamp := time.Now().UTC().Format(time.RFC3339)
	protocol := stream.Protocol()

	h.logger.Infof("[%s] Received card request from peer %s via protocol %s", timestamp, peerID, protocol)

	// Serialize agent card
	cardData, err := json.Marshal(h.agentCard)
	if err != nil {
		h.logger.Errorf("[%s] Failed to marshal agent card: %v", timestamp, err)
		errorResponse := map[string]interface{}{
			"error": "Failed to serialize agent card",
			"code":  500,
		}
		errorData, _ := json.Marshal(errorResponse)
		stream.Write(errorData)
		return
	}

	// Send card data
	_, err = stream.Write(cardData)
	if err != nil {
		h.logger.Errorf("[%s] Failed to write card data to stream: %v", timestamp, err)
		return
	}

	h.logger.Infof("[%s] Sent %d bytes card data to peer %s", timestamp, len(cardData), peerID)
}
