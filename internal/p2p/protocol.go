package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/praxis/praxis-go-sdk/internal/a2a"
	"github.com/sirupsen/logrus"
)

const (
	// P2P Protocol IDs
	ProtocolMCP  = protocol.ID("/praxis/mcp/1.0.0")
	ProtocolCard = protocol.ID("/praxis/card/1.0.0")
	ProtocolTool = protocol.ID("/praxis/tool/1.0.0")
	ProtocolA2A  = protocol.ID("/praxis/a2a/1.0.0") // A2A Protocol
)

// P2PProtocolHandler handles P2P protocol messages
type P2PProtocolHandler struct {
	host      host.Host
	logger    *logrus.Logger
	handlers  map[protocol.ID]StreamHandler
	peerCards map[peer.ID]*AgentCard
	ourCard   *AgentCard    // Our own agent card
	mcpBridge *P2PMCPBridge // Reference to MCP bridge for tool execution
	agent     A2AAgent      // Interface to agent for A2A protocol
	mu        sync.RWMutex
}

// A2AAgent interface for A2A protocol operations
type A2AAgent interface {
	DispatchA2ARequest(req a2a.JSONRPCRequest) a2a.JSONRPCResponse
}

// StreamHandler handles incoming streams
type StreamHandler func(network.Stream)

// ToolParameter описывает один параметр инструмента
type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "string", "boolean", "number", "object", "array"
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ToolSpec описывает полную спецификацию инструмента
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  []ToolParameter `json:"parameters"`
}

// AgentCard represents agent capabilities with full tool specifications
type AgentCard struct {
	Name         string     `json:"name"`
	Version      string     `json:"version"`
	PeerID       string     `json:"peerId"`
	Capabilities []string   `json:"capabilities"`
	Tools        []ToolSpec `json:"tools"` // Changed from []string to []ToolSpec
	Timestamp    int64      `json:"timestamp"`
}

// P2PMessage represents a P2P message
type P2PMessage struct {
	Type   string      `json:"type"`
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
	Result interface{} `json:"result,omitempty"`
	Error  *P2PError   `json:"error,omitempty"`
}

// P2PError represents an error in P2P communication
type P2PError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewP2PProtocolHandler creates a new protocol handler
func NewP2PProtocolHandler(host host.Host, logger *logrus.Logger) *P2PProtocolHandler {
	if logger == nil {
		logger = logrus.New()
	}

	handler := &P2PProtocolHandler{
		host:      host,
		logger:    logger,
		handlers:  make(map[protocol.ID]StreamHandler),
		peerCards: make(map[peer.ID]*AgentCard),
	}

	// Register protocol handlers
	host.SetStreamHandler(ProtocolMCP, handler.handleMCPStream)
	host.SetStreamHandler(ProtocolCard, handler.handleCardStream)
	host.SetStreamHandler(ProtocolTool, handler.handleToolStream)
	host.SetStreamHandler(ProtocolA2A, handler.handleA2AStream)

	logger.Info("P2P protocol handlers registered")

	return handler
}

// SetMCPBridge sets the MCP bridge for tool execution
func (h *P2PProtocolHandler) SetMCPBridge(bridge *P2PMCPBridge) {
	h.mcpBridge = bridge
	h.logger.Debug("MCP bridge set for P2P protocol handler")
}

// SetAgent sets the agent for A2A protocol operations
func (h *P2PProtocolHandler) SetAgent(agent A2AAgent) {
	h.agent = agent
	h.logger.Debug("A2A agent interface set for P2P protocol handler")
}

// handleMCPStream handles MCP protocol streams
func (h *P2PProtocolHandler) handleMCPStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	h.logger.Infof("📡 Handling MCP stream from peer: %s", peerID.ShortString())

	decoder := json.NewDecoder(stream)
	encoder := json.NewEncoder(stream)

	for {
		var msg P2PMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				h.logger.Errorf("Failed to decode message: %v", err)
			}
			break
		}

		h.logger.Debugf("Received P2P message: type=%s, method=%s", msg.Type, msg.Method)

		// Process message and send response
		response := h.processMCPMessage(msg)
		if err := encoder.Encode(response); err != nil {
			h.logger.Errorf("Failed to send response: %v", err)
			break
		}
	}
}

// handleCardStream handles agent card exchange
func (h *P2PProtocolHandler) handleCardStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	h.logger.Infof("🎴 Exchanging cards with peer: %s", peerID.ShortString())

	// Send our card
	ourCard := h.getOurCard()
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(ourCard); err != nil {
		h.logger.Errorf("Failed to send our card: %v", err)
		return
	}

	// Receive peer's card
	decoder := json.NewDecoder(stream)
	var peerCard AgentCard
	if err := decoder.Decode(&peerCard); err != nil {
		h.logger.Errorf("Failed to receive peer card: %v", err)
		return
	}

	// Store peer's card
	h.mu.Lock()
	h.peerCards[peerID] = &peerCard
	h.mu.Unlock()

	h.logger.Infof("✅ Card exchange complete with %s: %s v%s",
		peerID.ShortString(), peerCard.Name, peerCard.Version)
}

// handleToolStream handles tool invocation requests
func (h *P2PProtocolHandler) handleToolStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	h.logger.Infof("🔧 Handling tool request from peer: %s", peerID.ShortString())

	decoder := json.NewDecoder(stream)
	encoder := json.NewEncoder(stream)

	var request ToolRequest
	if err := decoder.Decode(&request); err != nil {
		h.logger.Errorf("Failed to decode tool request: %v", err)
		return
	}

	h.logger.Infof("📥 Tool request: %s with args: %v", request.Name, request.Arguments)

	// Process tool request
	result := h.processTool(request)

	// Send response
	response := ToolResponse{
		ID:     request.ID,
		Result: result,
	}

	if err := encoder.Encode(response); err != nil {
		h.logger.Errorf("Failed to send tool response: %v", err)
		return
	}

	h.logger.Infof("📤 Tool response sent for: %s", request.Name)
}

// RequestCard requests an agent card from a peer
func (h *P2PProtocolHandler) RequestCard(ctx context.Context, peerID peer.ID) (*AgentCard, error) {
	// Check cache first
	h.mu.RLock()
	if card, exists := h.peerCards[peerID]; exists {
		h.mu.RUnlock()
		return card, nil
	}
	h.mu.RUnlock()

	h.logger.Infof("🎴 Requesting card from peer: %s", peerID.ShortString())

	stream, err := h.host.NewStream(ctx, peerID, ProtocolCard)
	if err != nil {
		return nil, fmt.Errorf("failed to open card stream: %w", err)
	}
	defer stream.Close()

	// Send our card first
	ourCard := h.getOurCard()
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(ourCard); err != nil {
		return nil, fmt.Errorf("failed to send our card: %w", err)
	}

	// Receive peer's card
	decoder := json.NewDecoder(stream)
	var peerCard AgentCard
	if err := decoder.Decode(&peerCard); err != nil {
		return nil, fmt.Errorf("failed to receive peer card: %w", err)
	}

	// Cache the card
	h.mu.Lock()
	h.peerCards[peerID] = &peerCard
	h.mu.Unlock()

	h.logger.Infof("✅ Received card from %s: %s v%s",
		peerID.ShortString(), peerCard.Name, peerCard.Version)

	return &peerCard, nil
}

// InvokeTool invokes a tool on a remote peer
func (h *P2PProtocolHandler) InvokeTool(ctx context.Context, peerID peer.ID, toolName string, args map[string]interface{}) (*ToolResponse, error) {
	h.logger.Infof("🔧 Invoking tool '%s' on peer: %s", toolName, peerID.ShortString())

	stream, err := h.host.NewStream(ctx, peerID, ProtocolTool)
	if err != nil {
		return nil, fmt.Errorf("failed to open tool stream: %w", err)
	}
	defer stream.Close()

	// Send tool request
	request := ToolRequest{
		ID:        generateID(),
		Name:      toolName,
		Arguments: args,
		Timestamp: time.Now().Unix(),
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(request); err != nil {
		return nil, fmt.Errorf("failed to send tool request: %w", err)
	}

	h.logger.Debugf("📤 Sent tool request: %s", request.ID)

	// Receive response
	decoder := json.NewDecoder(stream)
	var response ToolResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to receive tool response: %w", err)
	}

	h.logger.Infof("✅ Tool '%s' executed successfully on peer %s",
		toolName, peerID.ShortString())

	return &response, nil
}

// SendMCPRequest sends an MCP request to a peer
func (h *P2PProtocolHandler) SendMCPRequest(ctx context.Context, peerID peer.ID, request interface{}) (*P2PMessage, error) {
	h.logger.Infof("📨 Sending MCP request to peer: %s", peerID.ShortString())

	stream, err := h.host.NewStream(ctx, peerID, ProtocolMCP)
	if err != nil {
		return nil, fmt.Errorf("failed to open MCP stream: %w", err)
	}
	defer stream.Close()

	msg := P2PMessage{
		Type:   "request",
		ID:     generateID(),
		Method: "mcp.execute",
		Params: request,
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send MCP request: %w", err)
	}

	decoder := json.NewDecoder(stream)
	var response P2PMessage
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to receive MCP response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", response.Error.Message)
	}

	return &response, nil
}

// GetPeerCards returns all cached peer cards
func (h *P2PProtocolHandler) GetPeerCards() map[peer.ID]*AgentCard {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cards := make(map[peer.ID]*AgentCard)
	for id, card := range h.peerCards {
		cards[id] = card
	}
	return cards
}

// processMCPMessage processes incoming MCP messages
func (h *P2PProtocolHandler) processMCPMessage(msg P2PMessage) P2PMessage {
	response := P2PMessage{
		Type: "response",
		ID:   msg.ID,
	}

	switch msg.Method {
	case "tools.list":
		response.Result = h.listTools()
	case "tool.invoke":
		if params, ok := msg.Params.(map[string]interface{}); ok {
			response.Result = h.invokeTool(params)
		} else {
			response.Error = &P2PError{Code: -32602, Message: "Invalid params"}
		}
	default:
		response.Error = &P2PError{Code: -32601, Message: "Method not found"}
	}

	return response
}

// listTools returns available tools
func (h *P2PProtocolHandler) listTools() []string {
	return []string{
		"analyze_dsl",
		"execute_workflow",
		"get_agent_info",
	}
}

// invokeTool invokes a local tool
func (h *P2PProtocolHandler) invokeTool(params map[string]interface{}) interface{} {
	toolName, _ := params["name"].(string)
	args, _ := params["arguments"].(map[string]interface{})

	h.logger.Infof("🔨 Invoking local tool: %s", toolName)

	// Simulate tool execution
	result := map[string]interface{}{
		"tool":   toolName,
		"status": "executed",
		"result": fmt.Sprintf("Tool %s executed with args: %v", toolName, args),
	}

	return result
}

// processTool processes a tool request
func (h *P2PProtocolHandler) processTool(request ToolRequest) interface{} {
	h.logger.Infof("⚙️ Processing tool: %s", request.Name)

	// If we have an MCP bridge, use it to execute the tool
	if h.mcpBridge != nil {
		// Create MCP request format
		mcpRequest := MCPRequest{
			ID:     0, // Convert string ID to int
			Method: "tools/call",
			Params: map[string]interface{}{
				"name":      request.Name,
				"arguments": request.Arguments,
			},
		}

		// Process through MCP bridge
		response := h.mcpBridge.ProcessMCPRequest(mcpRequest)

		// Check for errors
		if response.Error != nil {
			return map[string]interface{}{
				"status": "error",
				"error":  response.Error.Message,
			}
		}

		// Return the result
		return response.Result
	}

	// Fallback to default handling if no MCP bridge
	switch request.Name {
	case "analyze_dsl":
		return map[string]interface{}{
			"status": "analyzed",
			"dsl":    request.Arguments["dsl"],
		}
	case "get_peer_info":
		return h.getOurCard()
	default:
		return map[string]interface{}{
			"status": "unknown_tool",
			"error":  fmt.Sprintf("Tool %s not found", request.Name),
		}
	}
}

// SetAgentCard sets the agent card to use
func (h *P2PProtocolHandler) SetAgentCard(card interface{}) {
	if agentCard, ok := card.(*AgentCard); ok {
		h.mu.Lock()
		h.ourCard = agentCard
		h.mu.Unlock()
	}
}

// getOurCard returns our agent card
func (h *P2PProtocolHandler) getOurCard() AgentCard {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.ourCard != nil {
		return *h.ourCard
	}

	// Default card if not set
	return AgentCard{
		Name:    "praxis-agent",
		Version: "1.0.0",
		PeerID:  h.host.ID().String(),
		Capabilities: []string{
			"mcp", "dsl", "workflow", "p2p",
		},
		Tools:     []ToolSpec{}, // Empty by default, will be filled by agent
		Timestamp: time.Now().Unix(),
	}
}

// ToolRequest represents a tool invocation request
type ToolRequest struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Timestamp int64                  `json:"timestamp"`
}

// ToolResponse represents a tool invocation response
type ToolResponse struct {
	ID     string      `json:"id"`
	Result interface{} `json:"result"`
	Error  *P2PError   `json:"error,omitempty"`
}

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// handleA2AStream handles A2A protocol streams with JSON-RPC 2.0
func (h *P2PProtocolHandler) handleA2AStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	h.logger.Infof("🔗 Handling A2A stream from peer: %s", peerID.ShortString())

	decoder := json.NewDecoder(stream)
	encoder := json.NewEncoder(stream)

	for {
		var rpcRequest a2a.JSONRPCRequest
		if err := decoder.Decode(&rpcRequest); err != nil {
			if err != io.EOF {
				h.logger.Errorf("[PeerID: %s] Failed to decode JSON-RPC request: %v", peerID.ShortString(), err)
			}
			break
		}

		h.logger.Debugf("[PeerID: %s] Received JSON-RPC request. Method: %s, ID: %v", 
			peerID.ShortString(), rpcRequest.Method, rpcRequest.ID)

		// Route to agent if available
		var response a2a.JSONRPCResponse
		if h.agent != nil {
			response = h.agent.DispatchA2ARequest(rpcRequest)
		} else {
			response = a2a.NewJSONRPCErrorResponse(rpcRequest.ID, 
				a2a.NewRPCError(a2a.ErrorCodeInternalError, "Agent not available"))
		}

		if err := encoder.Encode(response); err != nil {
			h.logger.Errorf("[PeerID: %s] Failed to send JSON-RPC response: %v", peerID.ShortString(), err)
			break
		}

		h.logger.Debugf("[PeerID: %s] Sent JSON-RPC response. ID: %v", peerID.ShortString(), response.ID)
	}
}
