package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/sirupsen/logrus"
)

// PeerDiscovery implements the Discovery interface
type PeerDiscovery struct {
	host            host.Host
	dht             *dht.IpfsDHT
	pubsub          *pubsub.PubSub
	config          DiscoveryConfig
	logger          *logrus.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	peerMap         map[string]peer.ID
	peerMapLock     sync.RWMutex
	discoveredPeers map[peer.ID]string
}

// NewDiscovery creates a new peer discovery service
func NewDiscovery(host host.Host, config DiscoveryConfig, logger *logrus.Logger) (Discovery, error) {
	if !config.Enabled {
		logger.Info("Peer discovery is disabled")
		return nil, errors.New("peer discovery is disabled")
	}

	ctx, cancel := context.WithCancel(context.Background())

	discovery := &PeerDiscovery{
		host:            host,
		config:          config,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		peerMap:         make(map[string]peer.ID),
		discoveredPeers: make(map[peer.ID]string),
	}

	return discovery, nil
}

// Start initializes and starts the discovery service
func (d *PeerDiscovery) Start() error {
	d.logger.Info("Starting peer discovery...")

	// Set up mDNS discovery if enabled
	if d.config.EnableMDNS {
		if err := d.setupMDNS(); err != nil {
			d.logger.Warnf("Failed to setup mDNS: %v", err)
		}
	}

	// Set up DHT discovery if enabled
	if d.config.EnableDHT {
		if err := d.setupDHT(); err != nil {
			d.logger.Warnf("Failed to setup DHT: %v", err)
		}
	}

	// Set up pubsub for peer discovery
	if err := d.setupPubSub(); err != nil {
		d.logger.Warnf("Failed to setup pubsub: %v", err)
	}

	// Register this peer with a name
	// TODO: Replace with proper name from config
	name := fmt.Sprintf("peer-%s", d.host.ID().String()[:8])
	if err := d.RegisterPeer(name); err != nil {
		d.logger.Warnf("Failed to register peer: %v", err)
	}

	return nil
}

// Shutdown stops the discovery service and cleans up resources
func (d *PeerDiscovery) Shutdown() error {
	d.logger.Info("Shutting down peer discovery...")
	d.cancel()
	return nil
}

// GetPeerCount returns the number of discovered peers
func (d *PeerDiscovery) GetPeerCount() int {
	d.peerMapLock.RLock()
	defer d.peerMapLock.RUnlock()
	return len(d.peerMap)
}

// ConnectToPeerByName attempts to connect to a peer by name
func (d *PeerDiscovery) ConnectToPeerByName(peerName string) error {
	d.logger.Infof("Looking for peer: %s", peerName)

	// Check if we already know this peer
	d.peerMapLock.RLock()
	peerID, exists := d.peerMap[peerName]
	d.peerMapLock.RUnlock()

	if exists {
		d.logger.Infof("Found peer %s in local map with ID: %s", peerName, peerID)
		return nil
	}

	// If not found, try to discover it
	d.logger.Infof("Peer %s not found in local map, trying discovery...", peerName)

	// Wait for discovery to find the peer
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if we've discovered the peer
			d.peerMapLock.RLock()
			peerID, exists = d.peerMap[peerName]
			d.peerMapLock.RUnlock()

			if exists {
				d.logger.Infof("Discovered peer %s with ID: %s", peerName, peerID)
				return nil
			}

			// Try active discovery
			d.advertiseAndFind()

		case <-timeout:
			return fmt.Errorf("timeout while trying to discover peer: %s", peerName)

		case <-d.ctx.Done():
			return fmt.Errorf("discovery service shutting down")
		}
	}
}

// ResolvePeerName resolves a peer name to a peer ID
func (d *PeerDiscovery) ResolvePeerName(peerName string) (peer.ID, error) {
	d.peerMapLock.RLock()
	peerID, exists := d.peerMap[peerName]
	d.peerMapLock.RUnlock()

	if !exists {
		return "", fmt.Errorf("peer name not found: %s", peerName)
	}

	return peerID, nil
}

// RegisterPeer registers this peer with the discovery service
func (d *PeerDiscovery) RegisterPeer(name string) error {
	// Store the peer name for this host
	d.peerMapLock.Lock()
	d.peerMap[name] = d.host.ID()
	d.discoveredPeers[d.host.ID()] = name
	d.peerMapLock.Unlock()

	d.logger.Infof("Registered peer name '%s' for ID: %s", name, d.host.ID())

	// Advertise the peer name via pubsub
	if d.pubsub != nil {
		go d.advertisePeerInfo(name)
	}

	return nil
}

// setupMDNS initializes multicast DNS discovery
func (d *PeerDiscovery) setupMDNS() error {
	d.logger.Info("Setting up mDNS discovery...")

	// Create a new mDNS service
	service := mdns.NewMdnsService(d.host, d.config.Rendezvous, d)
	if err := service.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS service: %w", err)
	}

	d.logger.Info("mDNS discovery started")
	return nil
}

// setupDHT initializes the DHT for peer discovery
func (d *PeerDiscovery) setupDHT() error {
	d.logger.Info("Setting up DHT discovery...")

	// Create a new DHT
	var err error
	d.dht, err = dht.New(d.ctx, d.host)
	if err != nil {
		return fmt.Errorf("failed to create DHT: %w", err)
	}

	// Bootstrap the DHT
	if err := d.dht.Bootstrap(d.ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	// Connect to bootstrap nodes
	for _, addr := range d.config.BootstrapNodes {
		d.logger.Infof("Connecting to bootstrap node: %s", addr)
		// Parse the multiaddress
		// ... (implementation omitted for brevity)
	}

	d.logger.Info("DHT discovery started")
	return nil
}

// setupPubSub initializes the pubsub system for peer discovery
func (d *PeerDiscovery) setupPubSub() error {
	d.logger.Info("Setting up pubsub discovery...")

	// Create a new pubsub service
	var err error
	d.pubsub, err = pubsub.NewGossipSub(d.ctx, d.host)
	if err != nil {
		return fmt.Errorf("failed to create pubsub: %w", err)
	}

	// Subscribe to the discovery topic
	topic, err := d.pubsub.Join(fmt.Sprintf("%s-discovery", d.config.Rendezvous))
	if err != nil {
		return fmt.Errorf("failed to join pubsub topic: %w", err)
	}

	// Subscribe to messages
	subscription, err := topic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic: %w", err)
	}

	// Handle incoming messages
	go d.handlePubSubMessages(subscription)

	d.logger.Info("Pubsub discovery started")
	return nil
}

// handlePubSubMessages processes incoming pubsub messages
func (d *PeerDiscovery) handlePubSubMessages(subscription *pubsub.Subscription) {
	for {
		msg, err := subscription.Next(d.ctx)
		if err != nil {
			if d.ctx.Err() != nil {
				// Context cancelled, shutting down
				return
			}
			d.logger.Errorf("Error getting next pubsub message: %v", err)
			continue
		}

		// Skip messages from ourselves
		if msg.ReceivedFrom == d.host.ID() {
			continue
		}

		// Process the message (e.g., extract peer name)
		peerName := string(msg.Data)
		d.logger.Infof("Received pubsub announcement from %s: %s", msg.ReceivedFrom, peerName)

		// Store the peer name
		d.peerMapLock.Lock()
		d.peerMap[peerName] = msg.ReceivedFrom
		d.discoveredPeers[msg.ReceivedFrom] = peerName
		d.peerMapLock.Unlock()
	}
}

// advertisePeerInfo periodically advertises peer information
func (d *PeerDiscovery) advertisePeerInfo(name string) {
	topic, err := d.pubsub.Join(fmt.Sprintf("%s-discovery", d.config.Rendezvous))
	if err != nil {
		d.logger.Errorf("Failed to join pubsub topic for advertising: %v", err)
		return
	}

	// Advertise periodically
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Initial advertisement
	if err := topic.Publish(d.ctx, []byte(name)); err != nil {
		d.logger.Errorf("Failed to publish peer info: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := topic.Publish(d.ctx, []byte(name)); err != nil {
				d.logger.Errorf("Failed to publish peer info: %v", err)
			}
		case <-d.ctx.Done():
			return
		}
	}
}

// advertiseAndFind actively tries to discover peers
func (d *PeerDiscovery) advertiseAndFind() {
	// Use DHT to find peers if available
	if d.dht != nil {
		// Refresh the DHT
		if err := d.dht.Bootstrap(d.ctx); err != nil {
			d.logger.Errorf("Failed to bootstrap DHT: %v", err)
		}

		// Find providers for our rendezvous key
		// This is just a simple approach, a more sophisticated one would use a proper
		// peer routing system
	}

	// Re-advertise on pubsub
	if d.pubsub != nil {
		topic, err := d.pubsub.Join(fmt.Sprintf("%s-discovery", d.config.Rendezvous))
		if err != nil {
			d.logger.Errorf("Failed to join pubsub topic for advertising: %v", err)
			return
		}

		// Get our peer name
		var peerName string
		d.peerMapLock.RLock()
		peerName = d.discoveredPeers[d.host.ID()]
		d.peerMapLock.RUnlock()

		if peerName != "" {
			if err := topic.Publish(d.ctx, []byte(peerName)); err != nil {
				d.logger.Errorf("Failed to publish peer info: %v", err)
			}
		}
	}
}

// HandlePeerFound implements the mdns.Notifee interface
func (d *PeerDiscovery) HandlePeerFound(pi peer.AddrInfo) {
	// Skip ourselves
	if pi.ID == d.host.ID() {
		return
	}

	d.logger.Infof("Found peer via mDNS: %s", pi.ID)

	// Try to connect to the peer
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()

	if err := d.host.Connect(ctx, pi); err != nil {
		d.logger.Warnf("Failed to connect to discovered peer %s: %v", pi.ID, err)
		return
	}

	d.logger.Infof("Connected to discovered peer: %s", pi.ID)

	// For now, just store with a generic name
	// In a real system, we would exchange peer names after connecting
	tempName := fmt.Sprintf("mdns-peer-%s", pi.ID.String()[:8])
	d.peerMapLock.Lock()
	d.peerMap[tempName] = pi.ID
	d.discoveredPeers[pi.ID] = tempName
	d.peerMapLock.Unlock()
}