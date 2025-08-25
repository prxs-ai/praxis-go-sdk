package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/multiformats/go-multiaddr"
	"github.com/praxis/praxis-go-sdk/internal/api"
	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/praxis/praxis-go-sdk/internal/config"
	"github.com/praxis/praxis-go-sdk/internal/dsl"
	applogger "github.com/praxis/praxis-go-sdk/internal/logger"
	"github.com/praxis/praxis-go-sdk/internal/mcp"
	"github.com/praxis/praxis-go-sdk/internal/p2p"
	"github.com/praxis/praxis-go-sdk/internal/workflow"
	"github.com/sirupsen/logrus"
)

type PraxisAgent struct {
	name                 string
	version              string
	host                 host.Host
	discovery            *p2p.Discovery
	mcpServer            *mcp.MCPServerWrapper
	p2pBridge            *p2p.P2PMCPBridge
	p2pProtocol          *p2p.P2PProtocolHandler
	dslAnalyzer          *dsl.Analyzer
	orchestratorAnalyzer *dsl.OrchestratorAnalyzer
	httpServer           *gin.Engine
	httpPort             int
	p2pPort              int
	ssePort              int
	websocketPort        int
	eventBus             *bus.EventBus
	websocketGateway     *api.WebSocketGateway
	orchestrator         *workflow.WorkflowOrchestrator
	logger               *logrus.Logger
	ctx                  context.Context
	cancel               context.CancelFunc
	wg                   sync.WaitGroup
	card                 *AgentCard
}

type Config struct {
	AgentName     string
	AgentVersion  string
	HTTPPort      int
	P2PPort       int
	SSEPort       int
	WebSocketPort int
	MCPEnabled    bool
	LogLevel      string
}

func NewPraxisAgent(config Config) (*PraxisAgent, error) {
	logger := logrus.New()
	
	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize EventBus
	eventBus := bus.NewEventBus(logger)

	// Add WebSocket log hook
	logHook := applogger.NewWebSocketLogHook(eventBus, config.AgentName)
	logger.AddHook(logHook)

	agent := &PraxisAgent{
		name:          config.AgentName,
		version:       config.AgentVersion,
		httpPort:      config.HTTPPort,
		p2pPort:       config.P2PPort,
		ssePort:       config.SSEPort,
		websocketPort: config.WebSocketPort,
		eventBus:      eventBus,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
	}

	if err := agent.initializeP2P(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize P2P: %w", err)
	}

	agent.initializeHTTP()
	agent.initializeDSL()
	agent.initializeAgentCard()

	if config.MCPEnabled {
		if err := agent.initializeMCP(); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize MCP: %w", err)
		}
	}

	// Initialize Workflow Orchestrator
	agent.orchestrator = workflow.NewWorkflowOrchestrator(agent.eventBus, agent.dslAnalyzer, logger)
	agent.orchestrator.SetAgentInterface(agent)

	// Initialize WebSocket Gateway
	agent.websocketGateway = api.NewWebSocketGateway(
		config.WebSocketPort,
		agent.eventBus,
		agent.dslAnalyzer,
		logger,
	)
	agent.websocketGateway.SetOrchestrator(agent.orchestrator)
	agent.websocketGateway.SetOrchestratorAnalyzer(agent.orchestratorAnalyzer)

	logger.Infof("Praxis Agent %s v%s initialized", config.AgentName, config.AgentVersion)

	return agent, nil
}

func (a *PraxisAgent) initializeP2P() error {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", a.p2pPort)
	sourceMultiAddr, err := multiaddr.NewMultiaddr(listenAddr)
	if err != nil {
		return fmt.Errorf("failed to create multiaddr: %w", err)
	}

	host, err := libp2p.New(
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(priv),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.DefaultPeerstore,
		libp2p.EnableRelay(),
	)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	a.host = host
	a.logger.Infof("P2P host created with ID: %s", host.ID())

	for _, addr := range host.Addrs() {
		a.logger.Infof("Listening on: %s/p2p/%s", addr, host.ID())
	}

	// Initialize P2P protocol handler for direct P2P communication
	a.p2pProtocol = p2p.NewP2PProtocolHandler(host, a.logger)

	// Initialize discovery
	discovery, err := p2p.NewDiscovery(host, a.logger)
	if err != nil {
		return fmt.Errorf("failed to create discovery: %w", err)
	}
	a.discovery = discovery
	
	// Connect discovery and protocol handler for automatic card exchange
	discovery.SetProtocolHandler(a.p2pProtocol)

	// Start discovery
	if err := a.discovery.Start(); err != nil {
		return fmt.Errorf("failed to start discovery: %w", err)
	}

	return nil
}

func (a *PraxisAgent) initializeMCP() error {
	serverConfig := mcp.ServerConfig{
		Name:            a.name,
		Version:         a.version,
		Transport:       mcp.TransportSSE,
		Port:            fmt.Sprintf(":%d", a.ssePort),
		Logger:          a.logger,
		EnableTools:     true,
		EnableResources: true,
		EnablePrompts:   true,
	}

	mcpServer, err := mcp.NewMCPServer(serverConfig)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}

	a.mcpServer = mcpServer

	a.p2pBridge = p2p.NewP2PMCPBridge(a.host, a.mcpServer, a.logger)
	
	// Connect the MCP bridge to the P2P protocol handler for tool execution
	if a.p2pProtocol != nil {
		a.p2pProtocol.SetMCPBridge(a.p2pBridge)
	}

	a.registerMCPHandlers()
	
	// Update P2P card with full tool specifications after all tools are registered
	a.updateP2PCardWithTools()

	if err := a.mcpServer.StartSSE(fmt.Sprintf(":%d", a.ssePort)); err != nil {
		return fmt.Errorf("failed to start SSE server: %w", err)
	}

	a.logger.Infof("MCP SSE server started on port %d", a.ssePort)

	return nil
}

func (a *PraxisAgent) registerMCPHandlers() {
	if a.dslAnalyzer != nil {
		dslTool := mcp.NewDSLTool(a.dslAnalyzer, a.logger)
		a.mcpServer.AddTool(dslTool.GetTool(), dslTool.Handler)
	}

	if a.card != nil {
		cardResource := mcp.NewAgentCardResource(a.card, a.logger)
		a.mcpServer.AddResource(cardResource.GetResource(), cardResource.Handler)
	}

	p2pTool := mcp.NewP2PTool(a.p2pBridge, a.logger)
	a.mcpServer.AddTool(p2pTool.GetListPeersTool(), p2pTool.ListPeersHandler)
	a.mcpServer.AddTool(p2pTool.GetSendMessageTool(), p2pTool.SendMessageHandler)

	// Add filesystem tools for agent-2 (filesystem provider)
	if a.name == "praxis-agent-2" {
		filesystemTools := mcp.NewFilesystemTools(a.logger, "/shared")
		a.mcpServer.AddTool(filesystemTools.GetWriteFileTool(), filesystemTools.WriteFileHandler)
		a.mcpServer.AddTool(filesystemTools.GetReadFileTool(), filesystemTools.ReadFileHandler)
		a.mcpServer.AddTool(filesystemTools.GetListFilesTool(), filesystemTools.ListFilesHandler)
		a.mcpServer.AddTool(filesystemTools.GetDeleteFileTool(), filesystemTools.DeleteFileHandler)
		a.logger.Info("Registered filesystem tools (write_file, read_file, list_files, delete_file)")
		
		// Update agent card to include filesystem capabilities
		a.card.Skills = append(a.card.Skills, AgentSkill{
			ID:          "filesystem-operations", 
			Name:        "Filesystem Operations",
			Description: "Create, read, and manage files in shared storage",
			Tags:        []string{"filesystem", "file_operations", "storage"},
		})
		
		// Update will be done after all tools are registered
	}

	executeTool := mcpTypes.NewTool("execute_workflow",
		mcpTypes.WithDescription("Execute a workflow defined in DSL"),
		mcpTypes.WithString("dsl", 
			mcpTypes.Required(),
			mcpTypes.Description("DSL definition of the workflow"),
		),
	)
	a.mcpServer.AddTool(executeTool, a.handleExecuteWorkflow)
}

func (a *PraxisAgent) handleExecuteWorkflow(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	args := req.GetArguments()
	dslQuery, _ := args["dsl"].(string)
	if dslQuery == "" {
		return mcpTypes.NewToolResultError("DSL query is required"), nil
	}

	result, err := a.dslAnalyzer.AnalyzeDSL(ctx, dslQuery)
	if err != nil {
		return mcpTypes.NewToolResultError(fmt.Sprintf("Failed to execute workflow: %v", err)), nil
	}

	return mcpTypes.NewToolResultText(fmt.Sprintf("Workflow executed: %v", result)), nil
}

// updateP2PCardWithTools updates the P2P card with full tool specifications
func (a *PraxisAgent) updateP2PCardWithTools() {
	if a.p2pProtocol == nil || a.mcpServer == nil {
		return
	}

	// Get all registered tools from MCP server
	registeredTools := a.mcpServer.GetRegisteredTools()
	
	// Convert MCP tools to P2P ToolSpecs
	var toolSpecs []p2p.ToolSpec
	for _, tool := range registeredTools {
		spec := p2p.ToolSpec{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  []p2p.ToolParameter{}, // Will be filled if we can extract them
		}
		
		// For now, we'll use simplified parameter extraction
		// since the MCP tool structure might vary
		// This can be enhanced later when we have a clearer schema structure
		
		toolSpecs = append(toolSpecs, spec)
	}
	
	// Create updated P2P card with full tool specifications
	p2pCard := &p2p.AgentCard{
		Name:         a.card.Name,
		Version:      a.card.Version,
		PeerID:       a.host.ID().String(),
		Capabilities: []string{"mcp", "dsl", "workflow", "p2p"},
		Tools:        toolSpecs,
		Timestamp:    time.Now().Unix(),
	}
	
	// Add filesystem capabilities for agent-2
	if a.name == "praxis-agent-2" {
		p2pCard.Capabilities = append(p2pCard.Capabilities, "filesystem", "file_operations")
	}
	
	a.p2pProtocol.SetAgentCard(p2pCard)
	a.logger.Infof("Updated P2P card with %d tool specifications", len(toolSpecs))
}

func (a *PraxisAgent) initializeHTTP() {
	gin.SetMode(gin.ReleaseMode)
	a.httpServer = gin.New()
	a.httpServer.Use(gin.Recovery())

	a.httpServer.GET("/health", a.handleHealth)
	a.httpServer.GET("/agent/card", a.handleGetCard)
	a.httpServer.GET("/peers", a.handleListPeers)
	a.httpServer.POST("/execute", a.handleExecuteDSL)
	a.httpServer.GET("/p2p/cards", a.handleGetP2PCards)
	a.httpServer.POST("/p2p/tool", a.handleInvokeP2PTool)
	
	// Diagnostic endpoints
	a.httpServer.GET("/p2p/info", a.handleGetP2PInfo)
	a.httpServer.GET("/mcp/tools", a.handleGetMCPTools)
	a.httpServer.GET("/cache/stats", a.handleGetCacheStats)
	a.httpServer.DELETE("/cache", a.handleClearCache)

	a.logger.Info("HTTP server initialized")
}

func (a *PraxisAgent) initializeDSL() {
	// Create analyzer with agent integration for real execution
	a.dslAnalyzer = dsl.NewAnalyzerWithAgent(a.logger, a)
	a.logger.Info("DSL analyzer initialized with agent integration")
	
	// Create orchestrator analyzer for complex workflows
	a.orchestratorAnalyzer = dsl.NewOrchestratorAnalyzer(a.logger, a, a.eventBus)
	a.logger.Info("Orchestrator analyzer initialized with event bus integration")
}


func (a *PraxisAgent) initializeAgentCard() {
	a.card = &AgentCard{
		Name:            a.name,
		Version:         a.version,
		ProtocolVersion: "0.2.5",
		URL:             fmt.Sprintf("http://localhost:%d", a.httpPort),
		Description:     "Praxis P2P Agent with MCP support",
		Capabilities: AgentCapabilities{
			Streaming:         boolPtr(true),
			PushNotifications: boolPtr(false),
			StateTransition:   boolPtr(true),
		},
		Skills: []AgentSkill{
			{
				ID:          "dsl-analysis",
				Name:        "DSL Analysis",
				Description: "Analyze and execute DSL workflows",
				Tags:        []string{"dsl", "workflow", "orchestration"},
			},
			{
				ID:          "p2p-communication",
				Name:        "P2P Communication",
				Description: "Communicate with other agents via P2P network",
				Tags:        []string{"p2p", "networking", "agent-to-agent"},
			},
			{
				ID:          "mcp-integration",
				Name:        "MCP Integration",
				Description: "Model Context Protocol support for tool invocation",
				Tags:        []string{"mcp", "tools", "resources"},
			},
		},
	}
	
	// Update P2P protocol handler with our card
	if a.p2pProtocol != nil {
		// Convert to P2P card format
		p2pCard := &p2p.AgentCard{
			Name:    a.card.Name,
			Version: a.card.Version,
			PeerID:  a.host.ID().String(),
			Capabilities: []string{"mcp", "dsl", "workflow", "p2p"},
			Tools: []p2p.ToolSpec{}, // Will be populated based on registered MCP tools
			Timestamp: time.Now().Unix(),
		}
		
		// Add capabilities from skills
		for _, skill := range a.card.Skills {
			for _, tag := range skill.Tags {
				p2pCard.Capabilities = append(p2pCard.Capabilities, tag)
			}
		}
		
		a.p2pProtocol.SetAgentCard(p2pCard)
	}
}

func (a *PraxisAgent) Start() error {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.httpServer.Run(fmt.Sprintf(":%d", a.httpPort)); err != nil {
			a.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	// Start WebSocket Gateway
	if a.websocketGateway != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			if err := a.websocketGateway.Run(); err != nil {
				a.logger.Errorf("WebSocket gateway error: %v", err)
			}
		}()
		a.logger.Infof("WebSocket gateway started on port %d", a.websocketPort)
	}

	a.logger.Infof("Agent %s started on HTTP port %d, P2P port %d, SSE port %d", 
		a.name, a.httpPort, a.p2pPort, a.ssePort)

	return nil
}

func (a *PraxisAgent) Stop() error {
	a.logger.Infof("Stopping agent %s", a.name)

	a.cancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if a.mcpServer != nil {
		if err := a.mcpServer.Shutdown(shutdownCtx); err != nil {
			a.logger.Errorf("Failed to shutdown MCP server: %v", err)
		}
	}

	if a.p2pBridge != nil {
		if err := a.p2pBridge.Close(); err != nil {
			a.logger.Errorf("Failed to close P2P bridge: %v", err)
		}
	}

	if err := a.host.Close(); err != nil {
		a.logger.Errorf("Failed to close P2P host: %v", err)
	}

	a.wg.Wait()

	a.logger.Info("Agent stopped")
	return nil
}

func (a *PraxisAgent) handleHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status": "healthy",
		"agent":  a.name,
		"version": a.version,
	})
}

func (a *PraxisAgent) handleGetCard(c *gin.Context) {
	c.JSON(200, a.card)
}

func (a *PraxisAgent) handleListPeers(c *gin.Context) {
	// Get peers from discovery
	discoveredPeers := a.discovery.GetConnectedPeers()
	
	peers := make([]gin.H, 0, len(discoveredPeers))
	for _, peerInfo := range discoveredPeers {
		peers = append(peers, gin.H{
			"id":          peerInfo.ID.String(),
			"connected":   peerInfo.IsConnected,
			"foundAt":     peerInfo.FoundAt,
			"lastSeen":    peerInfo.LastSeen,
		})
	}
	
	c.JSON(200, gin.H{"peers": peers})
}

func (a *PraxisAgent) handleExecuteDSL(c *gin.Context) {
	var request struct {
		DSL string `json:"dsl" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	result, err := a.dslAnalyzer.AnalyzeDSL(c.Request.Context(), request.DSL)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"result": result})
}

func (a *PraxisAgent) handleGetP2PCards(c *gin.Context) {
	cards := a.p2pProtocol.GetPeerCards()
	c.JSON(200, gin.H{"cards": cards})
}

func (a *PraxisAgent) handleInvokeP2PTool(c *gin.Context) {
	var request struct {
		PeerID   string                 `json:"peer_id" binding:"required"`
		ToolName string                 `json:"tool_name" binding:"required"`
		Args     map[string]interface{} `json:"args"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Parse peer ID
	peerID, err := peer.Decode(request.PeerID)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid peer ID"})
		return
	}

	// Invoke tool via P2P
	response, err := a.p2pProtocol.InvokeTool(c.Request.Context(), peerID, request.ToolName, request.Args)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"result": response})
}

func boolPtr(b bool) *bool {
	return &b
}

func GetConfigFromEnv() Config {
	config := Config{
		AgentName:     getEnv("AGENT_NAME", "praxis-agent"),
		AgentVersion:  getEnv("AGENT_VERSION", "1.0.0"),
		HTTPPort:      getEnvInt("HTTP_PORT", 8000),
		P2PPort:       getEnvInt("P2P_PORT", 4001),
		SSEPort:       getEnvInt("SSE_PORT", 8090),
		WebSocketPort: getEnvInt("WEBSOCKET_PORT", 9000),
		MCPEnabled:    getEnvBool("MCP_ENABLED", true),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
	}
	return config
}

// AdaptAppConfigToAgentConfig converts config.AppConfig to agent.Config
func AdaptAppConfigToAgentConfig(appConfig *config.AppConfig) Config {
	// Default WebSocket port
	websocketPort := 9000
	if wsPortStr := os.Getenv("WEBSOCKET_PORT"); wsPortStr != "" {
		if wsPort, err := strconv.Atoi(wsPortStr); err == nil {
			websocketPort = wsPort
		}
	}

	// Default SSE port
	ssePort := 8090
	if ssePortStr := os.Getenv("SSE_PORT"); ssePortStr != "" {
		if sseP, err := strconv.Atoi(ssePortStr); err == nil {
			ssePort = sseP
		}
	}

	return Config{
		AgentName:     appConfig.Agent.Name,
		AgentVersion:  appConfig.Agent.Version,
		HTTPPort:      appConfig.HTTP.Port,
		P2PPort:       appConfig.P2P.Port,
		SSEPort:       ssePort,
		WebSocketPort: websocketPort,
		MCPEnabled:    appConfig.MCP.Enabled,
		LogLevel:      appConfig.Logging.Level,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// ============= DSL AgentInterface Implementation =============

// HasLocalTool checks if the agent has a specific tool locally
func (a *PraxisAgent) HasLocalTool(toolName string) bool {
	if a.mcpServer == nil {
		return false
	}
	return a.mcpServer.HasTool(toolName)
}

// ExecuteLocalTool executes a local MCP tool
func (a *PraxisAgent) ExecuteLocalTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	if a.mcpServer == nil {
		return nil, fmt.Errorf("MCP server not available")
	}

	handler := a.mcpServer.FindToolHandler(toolName)
	if handler == nil {
		return nil, fmt.Errorf("tool %s not found locally", toolName)
	}

	// Create MCP request
	req := mcpTypes.CallToolRequest{
		Params: struct {
			Name      string      `json:"name"`
			Arguments interface{} `json:"arguments,omitempty"`
			Meta      *mcpTypes.Meta `json:"_meta,omitempty"`
		}{
			Name:      toolName,
			Arguments: args,
		},
	}

	// Execute the tool
	result, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// FindAgentWithTool finds an agent that has a specific tool
func (a *PraxisAgent) FindAgentWithTool(toolName string) (string, error) {
	ctx := context.Background()
	// First check the P2P protocol handler's peer cards
	if a.p2pProtocol != nil {
		peerCards := a.p2pProtocol.GetPeerCards()
		for peerID, card := range peerCards {
			// Check if this peer has the tool in their Tools list
			for _, toolSpec := range card.Tools {
				if toolSpec.Name == toolName {
					a.logger.Infof("Found peer %s with tool %s", peerID, toolName)
					return peerID.String(), nil
				}
			}
		}
	}

	// Check connected peers directly
	if a.discovery != nil {
		connectedPeers := a.discovery.GetConnectedPeers()
		for _, peerInfo := range connectedPeers {
			// Request card if we don't have it
			if a.p2pProtocol != nil {
				card, err := a.p2pProtocol.RequestCard(ctx, peerInfo.ID)
				if err == nil && card != nil {
					for _, toolSpec := range card.Tools {
						if toolSpec.Name == toolName {
							a.logger.Infof("Found peer %s with tool %s", peerInfo.ID, toolName)
							return peerInfo.ID.String(), nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no agent found with tool %s", toolName)
}

// ExecuteRemoteTool executes a tool on a remote agent via P2P
func (a *PraxisAgent) ExecuteRemoteTool(ctx context.Context, peerIDStr string, toolName string, args map[string]interface{}) (interface{}, error) {
	if a.p2pProtocol == nil {
		return nil, fmt.Errorf("P2P protocol handler not available")
	}

	// Parse peer ID string
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer ID %s: %w", peerIDStr, err)
	}

	// Use P2P protocol to invoke the tool
	result, err := a.p2pProtocol.InvokeTool(ctx, peerID, toolName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke remote tool: %w", err)
	}

	return result, nil
}

// GetPeerCards returns all known peer agent cards
func (a *PraxisAgent) GetPeerCards() map[string]*p2p.AgentCard {
	if a.p2pProtocol == nil {
		return make(map[string]*p2p.AgentCard)
	}
	
	peerCards := a.p2pProtocol.GetPeerCards()
	result := make(map[string]*p2p.AgentCard)
	
	for peerID, card := range peerCards {
		result[peerID.String()] = card
	}
	
	return result
}

// GetAgentNameByPeerID returns the agent name for a given peer ID
func (a *PraxisAgent) GetAgentNameByPeerID(peerIDStr string) string {
	peerCards := a.GetPeerCards()
	if card, exists := peerCards[peerIDStr]; exists {
		return card.Name
	}
	
	// Try short peer ID
	shortID := peerIDStr
	if len(peerIDStr) > 8 {
		shortID = peerIDStr[:8]
	}
	for pid, card := range peerCards {
		if strings.Contains(pid, shortID) {
			return card.Name
		}
	}
	
	return fmt.Sprintf("Agent %s", shortID)
}

// handleGetP2PInfo returns P2P host information
func (a *PraxisAgent) handleGetP2PInfo(c *gin.Context) {
	if a.host == nil {
		c.JSON(503, gin.H{"error": "P2P host not initialized"})
		return
	}
	
	addrs := []string{}
	for _, addr := range a.host.Addrs() {
		addrs = append(addrs, addr.String())
	}
	
	c.JSON(200, gin.H{
		"peer_id":   a.host.ID().String(),
		"addresses": addrs,
		"protocol":  "libp2p",
		"agent":     a.name,
	})
}

// handleGetMCPTools returns list of MCP tools
func (a *PraxisAgent) handleGetMCPTools(c *gin.Context) {
	if a.mcpServer == nil {
		c.JSON(503, gin.H{"error": "MCP server not initialized"})
		return
	}
	
	tools := a.mcpServer.GetRegisteredTools()
	
	c.JSON(200, gin.H{
		"tools": tools,
		"count": len(tools),
		"agent": a.name,
	})
}

// handleGetCacheStats returns cache statistics
func (a *PraxisAgent) handleGetCacheStats(c *gin.Context) {
	stats := a.dslAnalyzer.GetCacheStats()
	
	c.JSON(200, gin.H{
		"cache": stats,
		"agent": a.name,
	})
}

// handleClearCache clears the tool execution cache
func (a *PraxisAgent) handleClearCache(c *gin.Context) {
	a.dslAnalyzer.ClearCache()
	
	c.JSON(200, gin.H{
		"status": "cache cleared",
		"agent":  a.name,
	})
}