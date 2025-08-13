package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/sirupsen/logrus"
)

type MCPProtocolHandler struct {
	host         host.Host
	bridge       *MCPBridge
	logger       *logrus.Logger
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewMCPProtocolHandler(host host.Host, bridge *MCPBridge, logger *logrus.Logger) *MCPProtocolHandler {
	ctx, cancel := context.WithCancel(context.Background())
	
	handler := &MCPProtocolHandler{
		host:   host,
		bridge: bridge,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
	
	host.SetStreamHandler(protocol.ID(MCPBridgeProtocol), handler.HandleStream)
	
	return handler
}

func (h *MCPProtocolHandler) HandleStream(stream network.Stream) {
	defer stream.Close()
	
	peerID := stream.Conn().RemotePeer()
	timestamp := time.Now().UTC().Format(time.RFC3339)
	protocol := stream.Protocol()
	
	h.logger.Infof("📨 [MCP] [%s] Received MCP request from peer %s via protocol %s", timestamp, peerID, protocol)
	
	if err := stream.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		h.logger.Errorf("❌ [MCP] [%s] Failed to set read deadline: %v", timestamp, err)
		return
	}
	
	requestData, err := io.ReadAll(stream)
	if err != nil {
		h.logger.Errorf("❌ [MCP] [%s] Failed to read request data: %v", timestamp, err)
		h.sendErrorResponse(stream, "", MCPErrorInternalError, "Failed to read request data", nil)
		return
	}
	
	h.logger.Infof("📥 [MCP] [%s] Received %d bytes from peer %s", timestamp, len(requestData), peerID)
	
	mcpRequest, err := UnmarshalMCPRequest(requestData)
	if err != nil {
		h.logger.Errorf("❌ [MCP] [%s] Failed to unmarshal MCP request: %v", timestamp, err)
		h.sendErrorResponse(stream, "", MCPErrorParseError, "Failed to parse request", err.Error())
		return
	}
	
	if err := mcpRequest.Validate(); err != nil {
		h.logger.Errorf("❌ [MCP] [%s] Invalid MCP request: %v", timestamp, err)
		if mcpErr, ok := err.(*MCPError); ok {
			h.sendErrorResponse(stream, mcpRequest.ID, mcpErr.Code, mcpErr.Message, mcpErr.Data)
		} else {
			h.sendErrorResponse(stream, mcpRequest.ID, MCPErrorInvalidRequest, err.Error(), nil)
		}
		return
	}
	
	h.logger.Infof("🔧 [MCP] [%s] Processing %s request for server '%s' (ID: %s)", 
		timestamp, mcpRequest.Method, mcpRequest.ServerName, mcpRequest.ID)
	
	ctx, cancel := context.WithTimeout(h.ctx, mcpRequest.Timeout)
	defer cancel()
	
	response, err := h.bridge.ProcessRequest(ctx, mcpRequest)
	if err != nil {
		h.logger.Errorf("❌ [MCP] [%s] Failed to process request: %v", timestamp, err)
		if mcpErr, ok := err.(*MCPError); ok {
			h.sendErrorResponse(stream, mcpRequest.ID, mcpErr.Code, mcpErr.Message, mcpErr.Data)
		} else {
			h.sendErrorResponse(stream, mcpRequest.ID, MCPErrorInternalError, err.Error(), nil)
		}
		return
	}
	
	if err := h.sendResponse(stream, response); err != nil {
		h.logger.Errorf("❌ [MCP] [%s] Failed to send response: %v", timestamp, err)
		return
	}
	
	h.logger.Infof("✅ [MCP] [%s] Successfully processed %s request (ID: %s)", 
		timestamp, mcpRequest.Method, mcpRequest.ID)
}

func (h *MCPProtocolHandler) SendMCPRequest(ctx context.Context, peerID peer.ID, req *MCPRequest) (*MCPResponse, error) {
	h.logger.Infof("📤 [MCP] Sending %s request to peer %s (ID: %s)", req.Method, peerID, req.ID)
	
	stream, err := h.host.NewStream(ctx, peerID, protocol.ID(MCPBridgeProtocol))
	if err != nil {
		return nil, fmt.Errorf("failed to open stream to peer %s: %w", peerID, err)
	}
	defer stream.Close()
	
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	
	if err := stream.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("failed to set stream deadline: %w", err)
	}
	
	requestData, err := req.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	if _, err := stream.Write(requestData); err != nil {
		return nil, fmt.Errorf("failed to write request data: %w", err)
	}
	
	if err := stream.CloseWrite(); err != nil {
		h.logger.Warnf("⚠️ [MCP] Failed to close write side of stream: %v", err)
	}
	
	h.logger.Infof("📡 [MCP] Sent %d bytes to peer %s", len(requestData), peerID)
	
	responseData, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	
	h.logger.Infof("📨 [MCP] Received %d bytes response from peer %s", len(responseData), peerID)
	
	response, err := UnmarshalMCPResponse(responseData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	
	if response.Error != nil {
		h.logger.Errorf("❌ [MCP] Remote error from peer %s: %s (code: %d)", 
			peerID, response.Error.Message, response.Error.Code)
		return response, &MCPError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
			Data:    response.Error.Data,
		}
	}
	
	h.logger.Infof("✅ [MCP] Successfully received response for request %s from peer %s", req.ID, peerID)
	return response, nil
}

func (h *MCPProtocolHandler) sendResponse(stream network.Stream, response *MCPResponse) error {
	if err := stream.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	
	responseData, err := response.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}
	
	if _, err := stream.Write(responseData); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}
	
	h.logger.Infof("📤 [MCP] Sent %d bytes response", len(responseData))
	return nil
}

func (h *MCPProtocolHandler) sendErrorResponse(stream network.Stream, requestID string, code int, message string, data interface{}) {
	errorResponse := NewMCPErrorResponse(requestID, code, message, data)
	if err := h.sendResponse(stream, errorResponse); err != nil {
		h.logger.Errorf("❌ [MCP] Failed to send error response: %v", err)
	}
}

func (h *MCPProtocolHandler) ListTools() []MCPTool {
	if h.bridge == nil {
		return []MCPTool{}
	}
	return h.bridge.ListAllTools()
}

func (h *MCPProtocolHandler) ListResources() []MCPResource {
	if h.bridge == nil {
		return []MCPResource{}
	}
	return h.bridge.ListAllResources()
}

func (h *MCPProtocolHandler) GetMCPCapabilities() []MCPCapability {
	if h.bridge == nil {
		return []MCPCapability{}
	}
	return h.bridge.GetCapabilities()
}

func (h *MCPProtocolHandler) Shutdown() error {
	h.logger.Info("🔄 [MCP] Shutting down MCP protocol handler...")
	
	h.cancel()
	
	h.host.RemoveStreamHandler(protocol.ID(MCPBridgeProtocol))
	
	h.logger.Info("✅ [MCP] MCP protocol handler shutdown complete")
	return nil
}

func (h *MCPProtocolHandler) IsHealthy() bool {
	return h.bridge != nil && h.bridge.IsHealthy()
}

func (h *MCPProtocolHandler) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"protocol_id": MCPBridgeProtocol,
		"peer_id":     h.host.ID().String(),
		"healthy":     h.IsHealthy(),
	}
	
	if h.bridge != nil {
		bridgeStats := h.bridge.GetStats()
		for k, v := range bridgeStats {
			stats[k] = v
		}
	}
	
	return stats
}