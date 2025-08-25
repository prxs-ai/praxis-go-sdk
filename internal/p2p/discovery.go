package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"
	"github.com/sirupsen/logrus"
)

const (
	DiscoveryServiceTag = "praxis-p2p-mcp"
	DiscoveryInterval   = time.Second * 10
)

type Discovery struct {
	host           host.Host
	mdnsService    mdns.Service
	foundPeers     map[peer.ID]*PeerInfo
	peerHandlers   []PeerHandler
	protocolHandler *P2PProtocolHandler // Reference for automatic card exchange
	logger         *logrus.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
}

type PeerInfo struct {
	ID          peer.ID
	Addrs       []multiaddr.Multiaddr
	FoundAt     time.Time
	LastSeen    time.Time
	AgentCard   interface{}
	IsConnected bool
}

type PeerHandler func(peerInfo *PeerInfo)

func NewDiscovery(host host.Host, logger *logrus.Logger) (*Discovery, error) {
	ctx, cancel := context.WithCancel(context.Background())

	if logger == nil {
		logger = logrus.New()
	}

	discovery := &Discovery{
		host:         host,
		foundPeers:   make(map[peer.ID]*PeerInfo),
		peerHandlers: make([]PeerHandler, 0),
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
	}

	return discovery, nil
}

func (d *Discovery) Start() error {
	d.logger.Info("Starting P2P discovery service")

	notifee := &discoveryNotifee{
		discovery: d,
	}

	mdnsService := mdns.NewMdnsService(d.host, DiscoveryServiceTag, notifee)
	if err := mdnsService.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS service: %w", err)
	}

	d.mdnsService = mdnsService

	go d.runDiscoveryLoop()

	d.logger.Info("P2P discovery service started")

	return nil
}

func (d *Discovery) Stop() error {
	d.logger.Info("Stopping P2P discovery service")

	d.cancel()

	if d.mdnsService != nil {
		if err := d.mdnsService.Close(); err != nil {
			d.logger.Errorf("Failed to close mDNS service: %v", err)
		}
	}

	return nil
}

func (d *Discovery) runDiscoveryLoop() {
	ticker := time.NewTicker(DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.checkPeerConnections()
		}
	}
}

func (d *Discovery) checkPeerConnections() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for peerID, peerInfo := range d.foundPeers {
		isConnected := d.host.Network().Connectedness(peerID) == 2

		if isConnected != peerInfo.IsConnected {
			peerInfo.IsConnected = isConnected
			if isConnected {
				d.logger.Infof("Peer %s connected", peerID)
			} else {
				d.logger.Infof("Peer %s disconnected", peerID)
			}
		}

		if isConnected {
			peerInfo.LastSeen = time.Now()
		}
	}

	for peerID, peerInfo := range d.foundPeers {
		if time.Since(peerInfo.LastSeen) > time.Minute*5 {
			d.logger.Infof("Removing stale peer: %s", peerID)
			delete(d.foundPeers, peerID)
		}
	}
}

func (d *Discovery) HandlePeerFound(pi peer.AddrInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if pi.ID == d.host.ID() {
		return
	}

	peerInfo, exists := d.foundPeers[pi.ID]
	if !exists {
		peerInfo = &PeerInfo{
			ID:       pi.ID,
			Addrs:    pi.Addrs,
			FoundAt:  time.Now(),
			LastSeen: time.Now(),
		}
		d.foundPeers[pi.ID] = peerInfo

		d.logger.Infof("Discovered new peer: %s", pi.ID)

		go d.connectToPeer(pi)
	} else {
		peerInfo.LastSeen = time.Now()
		peerInfo.Addrs = pi.Addrs
	}

	for _, handler := range d.peerHandlers {
		go handler(peerInfo)
	}
}

func (d *Discovery) connectToPeer(pi peer.AddrInfo) {
	ctx, cancel := context.WithTimeout(d.ctx, time.Second*30)
	defer cancel()

	if err := d.host.Connect(ctx, pi); err != nil {
		d.logger.Errorf("Failed to connect to peer %s: %v", pi.ID, err)
		return
	}

	d.logger.Infof("Successfully connected to peer: %s", pi.ID)

	d.mu.Lock()
	if peerInfo, exists := d.foundPeers[pi.ID]; exists {
		peerInfo.IsConnected = true
	}
	d.mu.Unlock()
	
	// Automatically exchange cards with the new peer
	if d.protocolHandler != nil {
		go func() {
			time.Sleep(1 * time.Second) // Small delay to ensure connection is stable
			card, err := d.protocolHandler.RequestCard(context.Background(), pi.ID)
			if err != nil {
				d.logger.Errorf("Failed to exchange cards with %s: %v", pi.ID, err)
			} else {
				d.logger.Infof("✅ Automatically exchanged cards with %s", pi.ID)
				// Update peer info with card
				d.mu.Lock()
				if peerInfo, exists := d.foundPeers[pi.ID]; exists {
					peerInfo.AgentCard = card
				}
				d.mu.Unlock()
			}
		}()
	}
}

// SetProtocolHandler sets the protocol handler for automatic card exchange
func (d *Discovery) SetProtocolHandler(handler *P2PProtocolHandler) {
	d.protocolHandler = handler
}

func (d *Discovery) RegisterPeerHandler(handler PeerHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peerHandlers = append(d.peerHandlers, handler)
}

func (d *Discovery) GetPeers() []*PeerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]*PeerInfo, 0, len(d.foundPeers))
	for _, peerInfo := range d.foundPeers {
		peers = append(peers, peerInfo)
	}

	return peers
}

func (d *Discovery) GetConnectedPeers() []*PeerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peers := make([]*PeerInfo, 0)
	for _, peerInfo := range d.foundPeers {
		if peerInfo.IsConnected {
			peers = append(peers, peerInfo)
		}
	}

	return peers
}

func (d *Discovery) GetPeerInfo(peerID peer.ID) (*PeerInfo, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	peerInfo, exists := d.foundPeers[peerID]
	return peerInfo, exists
}

func (d *Discovery) ConnectToBootstrapPeers(bootstrapPeers []string) error {
	for _, peerAddr := range bootstrapPeers {
		addr, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			d.logger.Errorf("Invalid bootstrap peer address %s: %v", peerAddr, err)
			continue
		}

		peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			d.logger.Errorf("Failed to parse peer info from %s: %v", peerAddr, err)
			continue
		}

		d.logger.Infof("Connecting to bootstrap peer: %s", peerInfo.ID)

		go d.connectToPeer(*peerInfo)
	}

	return nil
}

type discoveryNotifee struct {
	discovery *Discovery
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.discovery.HandlePeerFound(pi)
}