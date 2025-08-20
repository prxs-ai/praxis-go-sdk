package p2p

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// Constants for P2P protocols
const (
	// CardProtocol is the protocol ID for agent card exchange
	CardProtocol = protocol.ID("/ai-agent/card/1.0.0")

	// Default port values
	DefaultP2PPort = 0 // Random port
)

// Host provides the P2P functionality for the agent
type Host interface {
	// Start initializes and starts the P2P host
	Start() error
	
	// Shutdown stops the P2P host and cleans up resources
	Shutdown() error
	
	// GetID returns the peer ID of this host
	GetID() peer.ID
	
	// GetAddresses returns the multiaddresses of this host
	GetAddresses() []string
	
	// ConnectToPeer connects to a peer by name using discovery
	ConnectToPeer(peerName string) error
	
	// ConnectToPeerDirect connects to a peer by ID directly
	ConnectToPeerDirect(peerName string, peerID peer.ID) error
	
	// ConnectToPeerWithAddr connects to a peer with a known address
	ConnectToPeerWithAddr(peerName string, peerID peer.ID, addr string) error
	
	// GetPeerByName looks up a peer ID by name
	GetPeerByName(peerName string) (peer.ID, error)
	
	// RequestData sends a request to a peer with the given protocol
	RequestData(peerName string, protocolID protocol.ID) ([]byte, error)
}

// DiscoveryConfig contains configuration for peer discovery
type DiscoveryConfig struct {
	// Enabled indicates whether discovery is enabled
	Enabled bool
	
	// Rendezvous is the string used for peer discovery
	Rendezvous string
	
	// EnableMDNS enables local network discovery using mDNS
	EnableMDNS bool
	
	// EnableDHT enables DHT-based discovery
	EnableDHT bool
	
	// BootstrapNodes is a list of bootstrap nodes for DHT
	BootstrapNodes []string
}

// Discovery provides peer discovery functionality
type Discovery interface {
	// Start initializes and starts the discovery service
	Start() error
	
	// Shutdown stops the discovery service and cleans up resources
	Shutdown() error
	
	// GetPeerCount returns the number of discovered peers
	GetPeerCount() int
	
	// ConnectToPeerByName attempts to connect to a peer by name
	ConnectToPeerByName(peerName string) error
	
	// ResolvePeerName resolves a peer name to a peer ID
	ResolvePeerName(peerName string) (peer.ID, error)
	
	// RegisterPeer registers this peer with the discovery service
	RegisterPeer(name string) error
}