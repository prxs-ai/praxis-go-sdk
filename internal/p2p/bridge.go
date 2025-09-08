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
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/praxis/praxis-go-sdk/internal/mcp"
	"github.com/sirupsen/logrus"
)

const (
	MCPProtocolID  = protocol.ID("/mcp/1.0.0")
	CardProtocolID = protocol.ID("/ai-agent/card/1.0.0")
)

type P2PMCPBridge struct {
	host         host.Host
	mcpServer    *mcp.MCPServerWrapper
	transportMgr *mcp.TransportManager
	peerClients  map[peer.ID]*mcp.MCPClientWrapper
	logger       *logrus.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
}

func NewP2PMCPBridge(host host.Host, mcpServer *mcp.MCPServerWrapper, logger *logrus.Logger) *P2PMCPBridge {
	ctx, cancel := context.WithCancel(context.Background())

	if logger == nil {
		logger = logrus.New()
	}

	bridge := &P2PMCPBridge{
		host:         host,
		mcpServer:    mcpServer,
		transportMgr: mcp.NewTransportManager(logger),
		peerClients:  make(map[peer.ID]*mcp.MCPClientWrapper),
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
	}

	host.SetStreamHandler(MCPProtocolID, bridge.handleMCPStream)
	host.SetStreamHandler(CardProtocolID, bridge.handleCardStream)

	logger.Info("P2P MCP Bridge initialized")

	return bridge
}

func (b *P2PMCPBridge) handleMCPStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	b.logger.Infof("Handling MCP stream from peer: %s", peerID)

	decoder := json.NewDecoder(stream)
	encoder := json.NewEncoder(stream)

	for {
		var request MCPRequest
		if err := decoder.Decode(&request); err != nil {
			if err != io.EOF {
				b.logger.Errorf("Failed to decode MCP request: %v", err)
			}
			break
		}

		response := b.ProcessMCPRequest(request)

		if err := encoder.Encode(response); err != nil {
			b.logger.Errorf("Failed to encode MCP response: %v", err)
			break
		}
	}
}

func (b *P2PMCPBridge) handleCardStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	b.logger.Infof("Handling card stream from peer: %s", peerID)

	encoder := json.NewEncoder(stream)

	card := b.getAgentCard()
	if err := encoder.Encode(card); err != nil {
		b.logger.Errorf("Failed to send agent card: %v", err)
	}
}

// ProcessMCPRequest processes an MCP request and returns the response
func (b *P2PMCPBridge) ProcessMCPRequest(request MCPRequest) MCPResponse {
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()

	switch request.Method {
	case "tools/list":
		return b.handleListTools(ctx)
	case "tools/call":
		return b.handleCallTool(ctx, request)
	case "resources/list":
		return b.handleListResources(ctx)
	case "resources/read":
		return b.handleReadResource(ctx, request)
	default:
		return MCPResponse{
			ID:    request.ID,
			Error: &MCPError{Code: -32601, Message: "Method not found"},
		}
	}
}

func (b *P2PMCPBridge) handleListTools(ctx context.Context) MCPResponse {
	b.logger.Debug("Listing tools for remote peer")

	tools := []mcpTypes.Tool{}

	return MCPResponse{
		ID: 0,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func (b *P2PMCPBridge) handleCallTool(ctx context.Context, request MCPRequest) MCPResponse {
	toolName, ok := request.Params["name"].(string)
	if !ok {
		return MCPResponse{
			ID:    request.ID,
			Error: &MCPError{Code: -32602, Message: "Invalid params: missing tool name"},
		}
	}

	args, _ := request.Params["arguments"].(map[string]interface{})

	b.logger.Infof("P2P Bridge: Executing tool %s with args: %v", toolName, args)

	// Check if we have the MCP server
	if b.mcpServer == nil {
		b.logger.Error("MCP server is not available")
		return MCPResponse{
			ID:    request.ID,
			Error: &MCPError{Code: -32603, Message: "MCP server not available"},
		}
	}

	// Find and execute the tool through the MCP server
	toolHandler := b.mcpServer.FindToolHandler(toolName)
	if toolHandler == nil {
		b.logger.Errorf("Tool %s not found", toolName)
		return MCPResponse{
			ID:    request.ID,
			Error: &MCPError{Code: -32601, Message: fmt.Sprintf("Tool %s not found", toolName)},
		}
	}

	// Create MCP request for the tool
	mcpReq := mcpTypes.CallToolRequest{
		Params: struct {
			Name      string         `json:"name"`
			Arguments interface{}    `json:"arguments,omitempty"`
			Meta      *mcpTypes.Meta `json:"_meta,omitempty"`
		}{
			Name:      toolName,
			Arguments: args,
		},
	}

	// Execute the tool handler
	result, err := toolHandler(ctx, mcpReq)
	if err != nil {
		b.logger.Errorf("Tool execution failed: %v", err)
		return MCPResponse{
			ID:    request.ID,
			Error: &MCPError{Code: -32603, Message: fmt.Sprintf("Tool execution failed: %v", err)},
		}
	}

	// Convert result to response format
	var responseContent []interface{}
	if result != nil && len(result.Content) > 0 {
		// Extract content from the result
		if textContent, ok := result.Content[0].(*mcpTypes.TextContent); ok {
			responseContent = []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": textContent.Text,
				},
			}
		}
	}

	// Fallback if content extraction fails
	if len(responseContent) == 0 {
		responseContent = []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("Tool %s executed successfully", toolName),
			},
		}
	}

	b.logger.Infof("Tool %s executed successfully via P2P", toolName)

	return MCPResponse{
		ID: request.ID,
		Result: map[string]interface{}{
			"content": responseContent,
			"isError": false,
		},
	}
}

func (b *P2PMCPBridge) handleListResources(ctx context.Context) MCPResponse {
	b.logger.Debug("Listing resources for remote peer")

	resources := []mcpTypes.Resource{
		{
			URI:         "agent://card",
			Name:        "Agent Card",
			Description: "Agent capabilities and metadata",
			MIMEType:    "application/json",
		},
	}

	return MCPResponse{
		ID: 0,
		Result: map[string]interface{}{
			"resources": resources,
		},
	}
}

func (b *P2PMCPBridge) handleReadResource(ctx context.Context, request MCPRequest) MCPResponse {
	uri, ok := request.Params["uri"].(string)
	if !ok {
		return MCPResponse{
			ID:    request.ID,
			Error: &MCPError{Code: -32602, Message: "Invalid params: missing URI"},
		}
	}

	if uri == "agent://card" {
		card := b.getAgentCard()
		cardJSON, _ := json.Marshal(card)

		return MCPResponse{
			ID: request.ID,
			Result: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"uri":      uri,
						"mimeType": "application/json",
						"text":     string(cardJSON),
					},
				},
			},
		}
	}

	return MCPResponse{
		ID:    request.ID,
		Error: &MCPError{Code: -32603, Message: "Resource not found"},
	}
}

func (b *P2PMCPBridge) ConnectToPeer(peerID peer.ID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.peerClients[peerID]; exists {
		return nil
	}

	b.logger.Infof("Connecting to peer %s via MCP", peerID)

	stream, err := b.host.NewStream(b.ctx, peerID, MCPProtocolID)
	if err != nil {
		return fmt.Errorf("failed to open MCP stream: %w", err)
	}

	// For future use: implement P2P stream transport
	_ = &P2PStreamTransport{
		stream: stream,
		logger: b.logger,
	}

	clientConfig := mcp.ClientConfig{
		Type:   mcp.ClientTypeInProcess,
		Logger: b.logger,
	}

	client, err := mcp.NewMCPClient(clientConfig)
	if err != nil {
		stream.Close()
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		client.Close()
		stream.Close()
		return fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	b.peerClients[peerID] = client
	b.logger.Infof("Successfully connected to peer %s", peerID)

	return nil
}

func (b *P2PMCPBridge) CallPeerTool(ctx context.Context, peerID peer.ID, toolName string, args map[string]interface{}) (*mcpTypes.CallToolResult, error) {
	b.mu.RLock()
	client, exists := b.peerClients[peerID]
	b.mu.RUnlock()

	if !exists {
		if err := b.ConnectToPeer(peerID); err != nil {
			return nil, fmt.Errorf("failed to connect to peer: %w", err)
		}

		b.mu.RLock()
		client = b.peerClients[peerID]
		b.mu.RUnlock()
	}

	return client.CallTool(ctx, toolName, args)
}

func (b *P2PMCPBridge) ListPeers(ctx context.Context) ([]string, error) {
	peers := b.host.Network().Peers()
	peerList := make([]string, len(peers))

	for i, p := range peers {
		peerList[i] = p.String()
	}

	return peerList, nil
}

func (b *P2PMCPBridge) SendMessage(ctx context.Context, peerIDStr string, message interface{}) error {
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		return fmt.Errorf("invalid peer ID: %w", err)
	}

	messageJSON, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = b.CallPeerTool(ctx, peerID, "receive_message", map[string]interface{}{
		"message": string(messageJSON),
	})

	return err
}

func (b *P2PMCPBridge) GetAgentCard(peerID peer.ID) (interface{}, error) {
	stream, err := b.host.NewStream(b.ctx, peerID, CardProtocolID)
	if err != nil {
		return nil, fmt.Errorf("failed to open card stream: %w", err)
	}
	defer stream.Close()

	decoder := json.NewDecoder(stream)

	var card interface{}
	if err := decoder.Decode(&card); err != nil {
		return nil, fmt.Errorf("failed to decode agent card: %w", err)
	}

	return card, nil
}

func (b *P2PMCPBridge) getAgentCard() interface{} {
	return map[string]interface{}{
		"name":            "Praxis Agent",
		"version":         "1.0.0",
		"protocolVersion": "0.2.5",
		"capabilities": map[string]bool{
			"tools":     true,
			"resources": true,
			"prompts":   true,
		},
		"skills": []map[string]interface{}{
			{
				"id":          "dsl-analysis",
				"name":        "DSL Analysis",
				"description": "Analyze and execute DSL queries",
			},
			{
				"id":          "p2p-communication",
				"name":        "P2P Communication",
				"description": "Communicate with other agents via P2P",
			},
		},
	}
}

func (b *P2PMCPBridge) Close() error {
	b.cancel()

	b.mu.Lock()
	defer b.mu.Unlock()

	for peerID, client := range b.peerClients {
		if err := client.Close(); err != nil {
			b.logger.Errorf("Failed to close client for peer %s: %v", peerID, err)
		}
	}

	b.transportMgr.Close()

	return nil
}

type P2PStreamTransport struct {
	stream network.Stream
	logger *logrus.Logger
}

func (t *P2PStreamTransport) Send(data []byte) error {
	_, err := t.stream.Write(data)
	return err
}

func (t *P2PStreamTransport) Receive() ([]byte, error) {
	buf := make([]byte, 4096)
	n, err := t.stream.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (t *P2PStreamTransport) Close() error {
	return t.stream.Close()
}

type MCPRequest struct {
	ID     int                    `json:"id"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type MCPResponse struct {
	ID     int         `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
