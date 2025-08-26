package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

type MCPServerWrapper struct {
	server          *server.MCPServer
	sseServer       *server.SSEServer
	httpServer      *server.StreamableHTTPServer
	logger          *logrus.Logger
	agentName       string
	agentVersion    string
	toolHandlers    map[string]server.ToolHandlerFunc // Track registered tool handlers
	registeredTools []mcpTypes.Tool                   // Track all registered tools with their specs
}

type ServerConfig struct {
	Name            string
	Version         string
	Transport       TransportType
	Port            string
	Logger          *logrus.Logger
	EnableTools     bool
	EnableResources bool
	EnablePrompts   bool
}

type TransportType string

const (
	TransportSTDIO TransportType = "stdio"
	TransportSSE   TransportType = "sse"
	TransportHTTP  TransportType = "http"
)

func NewMCPServer(config ServerConfig) (*MCPServerWrapper, error) {
	if config.Logger == nil {
		config.Logger = logrus.New()
	}

	var opts []server.ServerOption

	if config.EnableTools {
		opts = append(opts, server.WithToolCapabilities(true))
	}
	if config.EnableResources {
		opts = append(opts, server.WithResourceCapabilities(true, true))
	}
	if config.EnablePrompts {
		opts = append(opts, server.WithPromptCapabilities(true))
	}

	s := server.NewMCPServer(config.Name, config.Version, opts...)

	wrapper := &MCPServerWrapper{
		server:          s,
		logger:          config.Logger,
		agentName:       config.Name,
		agentVersion:    config.Version,
		toolHandlers:    make(map[string]server.ToolHandlerFunc),
		registeredTools: []mcpTypes.Tool{},
	}

	config.Logger.Infof("Created MCP server: %s v%s", config.Name, config.Version)

	return wrapper, nil
}

func (w *MCPServerWrapper) AddTool(tool mcpTypes.Tool, handler server.ToolHandlerFunc) {
	w.server.AddTool(tool, handler)
	w.toolHandlers[tool.Name] = handler                 // Store the handler for later access
	w.registeredTools = append(w.registeredTools, tool) // Store the tool specification
	w.logger.Debugf("Added tool: %s", tool.Name)
}

// FindToolHandler returns the handler for a specific tool if it exists
func (w *MCPServerWrapper) FindToolHandler(toolName string) server.ToolHandlerFunc {
	return w.toolHandlers[toolName]
}

// HasTool checks if a tool is registered
func (w *MCPServerWrapper) HasTool(toolName string) bool {
	_, exists := w.toolHandlers[toolName]
	return exists
}

// GetRegisteredTools returns all registered tools with their specifications
func (w *MCPServerWrapper) GetRegisteredTools() []mcpTypes.Tool {
	return w.registeredTools
}

func (w *MCPServerWrapper) AddResource(resource mcpTypes.Resource, handler server.ResourceHandlerFunc) {
	w.server.AddResource(resource, handler)
	w.logger.Debugf("Added resource: %s", resource.URI)
}

func (w *MCPServerWrapper) AddPrompt(prompt mcpTypes.Prompt, handler server.PromptHandlerFunc) {
	w.server.AddPrompt(prompt, handler)
	w.logger.Debugf("Added prompt: %s", prompt.Name)
}

func (w *MCPServerWrapper) StartSSE(port string) error {
	w.sseServer = server.NewSSEServer(w.server)

	w.logger.Infof("Starting SSE server on port %s", port)

	go func() {
		if err := w.sseServer.Start(port); err != nil && err != http.ErrServerClosed {
			w.logger.Errorf("SSE server error: %v", err)
		}
	}()

	return nil
}

func (w *MCPServerWrapper) StartHTTP(port string) error {
	w.httpServer = server.NewStreamableHTTPServer(w.server)

	w.logger.Infof("Starting HTTP server on port %s", port)

	go func() {
		if err := w.httpServer.Start(port); err != nil && err != http.ErrServerClosed {
			w.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	return nil
}

func (w *MCPServerWrapper) StartSTDIO() error {
	w.logger.Info("Starting STDIO server")
	return server.ServeStdio(w.server)
}

func (w *MCPServerWrapper) Shutdown(ctx context.Context) error {
	w.logger.Info("Shutting down MCP server")

	if w.sseServer != nil {
		if err := w.sseServer.Shutdown(ctx); err != nil {
			w.logger.Errorf("Failed to shutdown SSE server: %v", err)
		}
	}

	if w.httpServer != nil {
		if err := w.httpServer.Shutdown(ctx); err != nil {
			w.logger.Errorf("Failed to shutdown HTTP server: %v", err)
		}
	}

	return nil
}

type DSLTool struct {
	analyzer DSLAnalyzer
	logger   *logrus.Logger
}

type DSLAnalyzer interface {
	AnalyzeDSL(ctx context.Context, dsl string) (interface{}, error)
}

func NewDSLTool(analyzer DSLAnalyzer, logger *logrus.Logger) *DSLTool {
	return &DSLTool{
		analyzer: analyzer,
		logger:   logger,
	}
}

func (t *DSLTool) GetTool() mcpTypes.Tool {
	return mcpTypes.NewTool("analyze_dsl",
		mcpTypes.WithDescription("Analyze DSL query and generate execution plan"),
		mcpTypes.WithString("query",
			mcpTypes.Required(),
			mcpTypes.Description("DSL query to analyze"),
		),
		mcpTypes.WithBoolean("validate_only",
			mcpTypes.DefaultBool(false),
			mcpTypes.Description("Only validate the DSL without execution"),
		),
	)
}

func (t *DSLTool) Handler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	query, _ := args["query"].(string)
	validateOnly, _ := args["validate_only"].(bool)

	if query == "" {
		return mcpTypes.NewToolResultError("DSL query is required"), nil
	}

	t.logger.Debugf("Analyzing DSL: %s", query)

	result, err := t.analyzer.AnalyzeDSL(ctx, query)
	if err != nil {
		return mcpTypes.NewToolResultError(fmt.Sprintf("DSL analysis failed: %v", err)), nil
	}

	if validateOnly {
		return mcpTypes.NewToolResultText("DSL query is valid"), nil
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to serialize result: %v", err)), nil
	}

	return mcpTypes.NewToolResultText(string(resultJSON)), nil
}

type AgentCardResource struct {
	card   interface{}
	logger *logrus.Logger
}

func NewAgentCardResource(card interface{}, logger *logrus.Logger) *AgentCardResource {
	return &AgentCardResource{
		card:   card,
		logger: logger,
	}
}

func (r *AgentCardResource) GetResource() mcpTypes.Resource {
	return mcpTypes.NewResource(
		"agent://card",
		"Agent Card",
		mcpTypes.WithResourceDescription("Agent capabilities and metadata"),
		mcpTypes.WithMIMEType("application/json"),
	)
}

func (r *AgentCardResource) Handler(ctx context.Context, req mcpTypes.ReadResourceRequest) ([]mcpTypes.ResourceContents, error) {
	r.logger.Debug("Reading agent card resource")

	cardJSON, err := json.MarshalIndent(r.card, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize agent card: %w", err)
	}

	textContents := mcpTypes.TextResourceContents{
		URI:      req.Params.URI,
		MIMEType: "application/json",
		Text:     string(cardJSON),
	}

	return []mcpTypes.ResourceContents{textContents}, nil
}

type P2PTool struct {
	p2pBridge P2PBridge
	logger    *logrus.Logger
}

type P2PBridge interface {
	SendMessage(ctx context.Context, peerID string, message interface{}) error
	ListPeers(ctx context.Context) ([]string, error)
}

func NewP2PTool(bridge P2PBridge, logger *logrus.Logger) *P2PTool {
	return &P2PTool{
		p2pBridge: bridge,
		logger:    logger,
	}
}

func (t *P2PTool) GetListPeersTool() mcpTypes.Tool {
	return mcpTypes.NewTool("list_p2p_peers",
		mcpTypes.WithDescription("List all connected P2P peers"),
	)
}

func (t *P2PTool) ListPeersHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	peers, err := t.p2pBridge.ListPeers(ctx)
	if err != nil {
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to list peers: %v", err)), nil
	}

	peersJSON, err := json.Marshal(peers)
	if err != nil {
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to serialize peers: %v", err)), nil
	}

	return mcpTypes.NewToolResultText(string(peersJSON)), nil
}

func (t *P2PTool) GetSendMessageTool() mcpTypes.Tool {
	return mcpTypes.NewTool("send_p2p_message",
		mcpTypes.WithDescription("Send message to a P2P peer"),
		mcpTypes.WithString("peer_id",
			mcpTypes.Required(),
			mcpTypes.Description("Target peer ID"),
		),
		mcpTypes.WithObject("message",
			mcpTypes.Required(),
			mcpTypes.Description("Message to send"),
		),
	)
}

func (t *P2PTool) SendMessageHandler(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	peerID, _ := args["peer_id"].(string)
	message := args["message"]

	if peerID == "" {
		return mcpTypes.NewToolResultError("peer_id is required"), nil
	}

	if message == nil {
		return mcpTypes.NewToolResultError("message is required"), nil
	}

	if err := t.p2pBridge.SendMessage(ctx, peerID, message); err != nil {
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to send message: %v", err)), nil
	}

	return mcpTypes.NewToolResultText("Message sent successfully"), nil
}
