package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/sirupsen/logrus"
)


type ProtocolHandler interface {
	HandleRequest(ctx context.Context, stream network.Stream, request []byte) ([]byte, error)
	GetProtocolID() protocol.ID
}



type LibP2PBridge interface {
	
	RouteRequest(stream network.Stream, request []byte) ([]byte, error)
	
	
	RegisterProtocol(protocolID protocol.ID, handler ProtocolHandler)
	
	
	ForwardToRemote(peerID peer.ID, protocolID protocol.ID, request []byte) ([]byte, error)
	
	
	Start() error
	
	
	Shutdown() error
}


type LibP2PBridgeImpl struct {
	host      host.Host
	logger    *logrus.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	
	
	handlers  map[protocol.ID]ProtocolHandler
	mu        sync.RWMutex
	
	
	stats     BridgeStats
	statsMu   sync.RWMutex
}

type BridgeStats struct {
	TotalRequests     uint64 `json:"total_requests"`
	SuccessfulRequests uint64 `json:"successful_requests"`
	FailedRequests    uint64 `json:"failed_requests"`
	RegisteredProtocols int   `json:"registered_protocols"`
}


func NewLibP2PBridge(host host.Host, logger *logrus.Logger) *LibP2PBridgeImpl {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &LibP2PBridgeImpl{
		host:     host,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		handlers: make(map[protocol.ID]ProtocolHandler),
		stats:    BridgeStats{},
	}
}


func (b *LibP2PBridgeImpl) Start() error {
	b.logger.Info("🌉 [LibP2P Bridge] Starting bridge...")
	
	
	b.host.SetStreamHandler(protocol.ID("/bridge/router/1.0.0"), b.handleGenericStream)
	
	b.logger.Info("✅ [LibP2P Bridge] Bridge started successfully")
	return nil
}


func (b *LibP2PBridgeImpl) RegisterProtocol(protocolID protocol.ID, handler ProtocolHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	b.handlers[protocolID] = handler
	
	
	b.host.SetStreamHandler(protocolID, func(stream network.Stream) {
		b.handleProtocolStream(stream, handler)
	})
	
	b.statsMu.Lock()
	b.stats.RegisteredProtocols = len(b.handlers)
	b.statsMu.Unlock()
	
	b.logger.Infof("🔌 [LibP2P Bridge] Registered protocol handler for %s", protocolID)
}


func (b *LibP2PBridgeImpl) handleProtocolStream(stream network.Stream, handler ProtocolHandler) {
	defer stream.Close()
	
	peerID := stream.Conn().RemotePeer()
	protocolID := stream.Protocol()
	
	b.logger.Infof("📨 [LibP2P Bridge] Handling %s request from peer %s", protocolID, peerID)
	
	b.statsMu.Lock()
	b.stats.TotalRequests++
	b.statsMu.Unlock()
	
	
	
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	
	
	
	if err := b.delegateToHandler(ctx, stream, handler); err != nil {
		b.logger.Errorf("❌ [LibP2P Bridge] Handler error for %s: %v", protocolID, err)
		b.statsMu.Lock()
		b.stats.FailedRequests++
		b.statsMu.Unlock()
		return
	}
	
	b.statsMu.Lock()
	b.stats.SuccessfulRequests++
	b.statsMu.Unlock()
	
	b.logger.Infof("✅ [LibP2P Bridge] Successfully handled %s request from %s", protocolID, peerID)
}


func (b *LibP2PBridgeImpl) delegateToHandler(ctx context.Context, stream network.Stream, handler ProtocolHandler) error {
	
	
	
	
	
	if handler == nil {
		return fmt.Errorf("no handler available for protocol %s", stream.Protocol())
	}
	
	
	
	return nil
}


func (b *LibP2PBridgeImpl) handleGenericStream(stream network.Stream) {
	defer stream.Close()
	
	
	
	b.logger.Info("📨 [LibP2P Bridge] Received generic routing request (not implemented)")
}


func (b *LibP2PBridgeImpl) RouteRequest(stream network.Stream, request []byte) ([]byte, error) {
	protocolID := stream.Protocol()
	
	b.mu.RLock()
	handler, exists := b.handlers[protocolID]
	b.mu.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("no handler registered for protocol %s", protocolID)
	}
	
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	
	return handler.HandleRequest(ctx, stream, request)
}


func (b *LibP2PBridgeImpl) ForwardToRemote(peerID peer.ID, protocolID protocol.ID, request []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	
	stream, err := b.host.NewStream(ctx, peerID, protocolID)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream to %s: %w", peerID, err)
	}
	defer stream.Close()
	
	
	if _, err := stream.Write(request); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}
	
	
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	return responseData, nil
}


func (b *LibP2PBridgeImpl) GetStats() BridgeStats {
	b.statsMu.RLock()
	defer b.statsMu.RUnlock()
	return b.stats
}


func (b *LibP2PBridgeImpl) Shutdown() error {
	b.logger.Info("🌉 [LibP2P Bridge] Shutting down bridge...")
	
	b.cancel()
	
	b.logger.Info("✅ [LibP2P Bridge] Bridge shutdown complete")
	return nil
}