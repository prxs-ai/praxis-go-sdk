package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/sirupsen/logrus"
)

type PeerDiscovery struct {
	host        host.Host
	logger      *logrus.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	
	discoveredPeers map[string]peer.AddrInfo
	mu              sync.RWMutex
	
	peerChan        chan peer.AddrInfo
	service         mdns.Service
	
	rendezvous      string
}

type discoveryNotifee struct {
	PeerChan chan peer.AddrInfo
	logger   *logrus.Logger
}

func NewPeerDiscovery(host host.Host, logger *logrus.Logger, rendezvous string) *PeerDiscovery {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &PeerDiscovery{
		host:            host,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		discoveredPeers: make(map[string]peer.AddrInfo),
		peerChan:        make(chan peer.AddrInfo, 100),
		rendezvous:      rendezvous,
	}
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.logger.Infof("🔍 [mDNS Discovery] Found peer: %s with %d addresses", pi.ID, len(pi.Addrs))
	select {
	case n.PeerChan <- pi:
	default:
		n.logger.Warnf("⚠️ [mDNS Discovery] Peer channel full, dropping peer: %s", pi.ID)
	}
}

func (pd *PeerDiscovery) Start() error {
	pd.logger.Info("🚀 [P2P Discovery] Starting mDNS peer discovery...")
	
	notifee := &discoveryNotifee{
		PeerChan: pd.peerChan,
		logger:   pd.logger,
	}
	
	service := mdns.NewMdnsService(pd.host, pd.rendezvous, notifee)
	if err := service.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS service: %w", err)
	}
	
	pd.service = service
	
	go pd.handleDiscoveredPeers()
	
	pd.logger.Infof("✅ [P2P Discovery] mDNS discovery started with rendezvous: %s", pd.rendezvous)
	return nil
}

func (pd *PeerDiscovery) handleDiscoveredPeers() {
	for {
		select {
		case peerInfo := <-pd.peerChan:
			pd.addDiscoveredPeer(peerInfo)
		case <-pd.ctx.Done():
			pd.logger.Info("🛑 [P2P Discovery] Stopping peer discovery handler")
			return
		}
	}
}

func (pd *PeerDiscovery) addDiscoveredPeer(peerInfo peer.AddrInfo) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	
	if peerInfo.ID == pd.host.ID() {
		return
	}
	
	pd.discoveredPeers[peerInfo.ID.String()] = peerInfo
	
	pd.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, time.Hour)
	
	pd.logger.Infof("➕ [P2P Discovery] Added peer %s with addresses: %v", 
		peerInfo.ID, peerInfo.Addrs)
}

func (pd *PeerDiscovery) FindPeerByName(name string) (peer.AddrInfo, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	
	for _, peerInfo := range pd.discoveredPeers {
		if peerInfo.ID.String() == name {
			return peerInfo, nil
		}
	}
	
	return peer.AddrInfo{}, fmt.Errorf("peer not found: %s", name)
}

func (pd *PeerDiscovery) FindPeerByID(peerID peer.ID) (peer.AddrInfo, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	
	if peerInfo, exists := pd.discoveredPeers[peerID.String()]; exists {
		return peerInfo, nil
	}
	
	return peer.AddrInfo{}, fmt.Errorf("peer not found: %s", peerID)
}

func (pd *PeerDiscovery) WaitForPeer(name string, timeout time.Duration) (peer.AddrInfo, error) {
	pd.logger.Infof("⏳ [P2P Discovery] Waiting for peer %s (timeout: %v)", name, timeout)
	
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	timeoutChan := time.After(timeout)
	
	for {
		select {
		case <-ticker.C:
			if peerInfo, err := pd.FindPeerByName(name); err == nil {
				pd.logger.Infof("✅ [P2P Discovery] Found peer %s", name)
				return peerInfo, nil
			}
		case <-timeoutChan:
			return peer.AddrInfo{}, fmt.Errorf("timeout waiting for peer %s", name)
		case <-pd.ctx.Done():
			return peer.AddrInfo{}, fmt.Errorf("discovery cancelled")
		}
	}
}

func (pd *PeerDiscovery) GetDiscoveredPeers() []peer.AddrInfo {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	
	peers := make([]peer.AddrInfo, 0, len(pd.discoveredPeers))
	for _, peerInfo := range pd.discoveredPeers {
		peers = append(peers, peerInfo)
	}
	
	return peers
}

func (pd *PeerDiscovery) GetPeerCount() int {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return len(pd.discoveredPeers)
}

func (pd *PeerDiscovery) ConnectToPeerByName(name string) error {
	peerInfo, err := pd.FindPeerByName(name)
	if err != nil {
		peerInfo, err = pd.WaitForPeer(name, 10*time.Second)
		if err != nil {
			return fmt.Errorf("failed to discover peer %s: %w", name, err)
		}
	}
	
	ctx, cancel := context.WithTimeout(pd.ctx, 10*time.Second)
	defer cancel()
	
	if pd.host.Network().Connectedness(peerInfo.ID) == 0 {
		pd.logger.Infof("🔗 [P2P Discovery] Connecting to peer %s (%s)", name, peerInfo.ID)
		
		if err := pd.host.Connect(ctx, peerInfo); err != nil {
			return fmt.Errorf("failed to connect to peer %s: %w", name, err)
		}
		
		pd.logger.Infof("✅ [P2P Discovery] Successfully connected to peer %s", name)
	} else {
		pd.logger.Infof("🔗 [P2P Discovery] Already connected to peer %s", name)
	}
	
	return nil
}

func (pd *PeerDiscovery) ResolvePeerName(name string) (peer.ID, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	
	for peerIDStr, peerInfo := range pd.discoveredPeers {
		if peerIDStr == name || peerInfo.ID.String() == name {
			return peerInfo.ID, nil
		}
	}
	
	for _, peerInfo := range pd.discoveredPeers {
		if len(peerInfo.Addrs) > 0 {
			addrStr := peerInfo.Addrs[0].String()
			if addrStr == name {
				return peerInfo.ID, nil
			}
		}
	}
	
	return "", fmt.Errorf("peer name not resolved: %s", name)
}

func (pd *PeerDiscovery) Shutdown() error {
	pd.logger.Info("🛑 [P2P Discovery] Shutting down peer discovery...")
	
	if pd.service != nil {
		if err := pd.service.Close(); err != nil {
			pd.logger.Errorf("❌ [P2P Discovery] Failed to close mDNS service: %v", err)
		}
	}
	
	pd.cancel()
	
	pd.logger.Info("✅ [P2P Discovery] Peer discovery shutdown complete")
	return nil
}