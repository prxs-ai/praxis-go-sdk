package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/multiformats/go-multiaddr"
	"github.com/praxis/praxis-go-sdk/internal/a2a"
	"github.com/praxis/praxis-go-sdk/internal/api"
	"github.com/praxis/praxis-go-sdk/internal/bus"
	appconfig "github.com/praxis/praxis-go-sdk/internal/config"
	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/praxis/praxis-go-sdk/internal/dagger"
	"github.com/praxis/praxis-go-sdk/internal/did"
	didweb "github.com/praxis/praxis-go-sdk/internal/did/web"
	didwebvh "github.com/praxis/praxis-go-sdk/internal/did/webvh"
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
	a2aCard              *a2a.AgentCard // Canonical A2A Agent Card
	transportManager     *mcp.TransportManager
	executionEngines     map[string]contracts.ExecutionEngine
	appConfig            *appconfig.AppConfig
	taskManager          *a2a.TaskManager // A2A Task Manager
	identityManager      *IdentityManager
	didResolver          did.Resolver
	securityConfig       appconfig.AgentSecurityConfig
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
	// Pass loaded application config so agent doesn't reload a hardcoded path
	AppConfig *appconfig.AppConfig
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

	// ADDED: Инициализация менеджера транспортов и исполнительных движков
	agent.transportManager = mcp.NewTransportManager(logger)
	agent.executionEngines = make(map[string]contracts.ExecutionEngine)

	// Initialize A2A Task Manager
	agent.taskManager = a2a.NewTaskManager(eventBus, logger)

	// Инициализация Remote MCP Engine (всегда доступен)
	remoteMCPEngine := mcp.NewRemoteMCPEngine(agent.transportManager)
	agent.executionEngines["remote-mcp"] = remoteMCPEngine
	logger.Info("📡 Remote MCP Engine initialized successfully")

	// Dagger Engine будет инициализирован позже, при первом использовании
	// Это избегает ошибок запуска, когда Docker не доступен
	logger.Info("🚀 Dagger Engine will be initialized on first use")

	// Use provided application configuration (from main or env), fallback to defaults
	if config.AppConfig != nil {
		agent.appConfig = config.AppConfig
	} else {
		logger.Warn("AppConfig not provided to agent; using defaults")
		agent.appConfig = appconfig.DefaultConfig()
	}
	agent.securityConfig = agent.appConfig.Agent.Security

	if err := agent.initializeP2P(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize P2P: %w", err)
	}

	agent.initializeDSL()
	agent.initializeAgentCard()

	// Initialize A2A card after P2P host is available
	agent.initializeA2ACard()

	if err := agent.initializeIdentity(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize identity: %w", err)
	}

	// Initialize HTTP server AFTER A2A card is initialized
	agent.initializeHTTP()

	// Set up P2P protocol with A2A card provider
	if agent.p2pProtocol != nil {
		agent.p2pProtocol.SetA2ACardProvider(agent)
	}

	if config.MCPEnabled {
		if err := agent.initializeMCP(); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize MCP: %w", err)
		}

		// Auto-discover and register external MCP tools
		go agent.discoverAndRegisterExternalTools(ctx)
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

	// Set orchestrator analyzer for WebSocket gateway
	if agent.orchestratorAnalyzer != nil {
		logger.Info("🔗 Setting OrchestratorAnalyzer in WebSocket Gateway")
		agent.websocketGateway.SetOrchestratorAnalyzer(agent.orchestratorAnalyzer)
	} else {
		logger.Error("❌ OrchestratorAnalyzer is nil, cannot set in WebSocket Gateway!")
		// This should not happen if initializeDSL was called
	}

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
	a.p2pProtocol.ConfigureSecurity(p2p.SecurityOptions{
		VerifyPeerCards: a.securityConfig.VerifyPeerCards,
		SignA2A:         a.securityConfig.SignA2A,
		VerifyA2A:       a.securityConfig.VerifyA2A,
	})

	// Set agent interface for A2A protocol
	a.p2pProtocol.SetAgent(a)

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
	// Register system tools (always available)
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

	executeTool := mcpTypes.NewTool("execute_workflow",
		mcpTypes.WithDescription("Execute a workflow defined in DSL"),
		mcpTypes.WithString("dsl",
			mcpTypes.Required(),
			mcpTypes.Description("DSL definition of the workflow"),
		),
	)
	a.mcpServer.AddTool(executeTool, a.handleExecuteWorkflow)

	// Dynamic tool registration from configuration
	a.registerDynamicTools()
}

func (a *PraxisAgent) registerDynamicTools() {

	// Register tools from configuration
	if a.appConfig != nil && len(a.appConfig.Agent.Tools) > 0 {
		for _, toolCfg := range a.appConfig.Agent.Tools {
			a.logger.Infof("Registering tool from config: %s (engine: %s)", toolCfg.Name, toolCfg.Engine)

			// Build MCP tool parameters
			var mcpOptions []mcpTypes.ToolOption
			mcpOptions = append(mcpOptions, mcpTypes.WithDescription(toolCfg.Description))

			for _, param := range toolCfg.Params {
				var paramOpts []mcpTypes.PropertyOption

				// Add description if provided
				if description, ok := param["description"]; ok && description != "" {
					paramOpts = append(paramOpts, mcpTypes.Description(description))
				}

				// Add required constraint
				if required, ok := param["required"]; ok && required == "true" {
					paramOpts = append(paramOpts, mcpTypes.Required())
				}

				// Add parameter based on type
				paramType := param["type"]
				paramName := param["name"]
				switch paramType {
				case "string":
					mcpOptions = append(mcpOptions, mcpTypes.WithString(paramName, paramOpts...))
				case "number":
					mcpOptions = append(mcpOptions, mcpTypes.WithNumber(paramName, paramOpts...))
				case "boolean":
					mcpOptions = append(mcpOptions, mcpTypes.WithBoolean(paramName, paramOpts...))
				default:
					mcpOptions = append(mcpOptions, mcpTypes.WithString(paramName, paramOpts...))
				}
			}

			toolSpec := mcpTypes.NewTool(toolCfg.Name, mcpOptions...)

			// Choose handler based on engine
			switch toolCfg.Engine {
			case "dagger", "remote-mcp":
				handler := a.createGenericHandler(toolCfg)
				a.mcpServer.AddTool(toolSpec, handler)
				a.logger.Infof("Registered '%s' tool from config: %s", toolCfg.Engine, toolCfg.Name)
			default:
				a.logger.Warnf("Unknown engine '%s' for tool '%s'", toolCfg.Engine, toolCfg.Name)
			}
		}
	} else {
		a.logger.Warn("No tools found in configuration")
	}
}

// createGenericHandler создает универсальный обработчик для любого движка.
func (a *PraxisAgent) createGenericHandler(toolCfg appconfig.ToolConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
		args := req.GetArguments()
		engineName := toolCfg.Engine

		a.logger.Infof("Executing tool '%s' via generic handler with engine '%s'", toolCfg.Name, engineName)

		engine, ok := a.executionEngines[engineName]
		if !ok {
			if engineName == "dagger" {
				a.logger.Info("Initializing Dagger Engine on first use...")
				daggerEngine, err := dagger.NewEngine(ctx)
				if err != nil {
					a.logger.Errorf("Failed to initialize Dagger Engine: %v", err)
					return mcpTypes.NewToolResultError(fmt.Sprintf("Dagger Engine initialization failed: %v", err)), nil
				}
				a.executionEngines["dagger"] = daggerEngine
				engine = daggerEngine
				a.logger.Info("🚀 Dagger Engine initialized successfully")
			} else {
				err := fmt.Errorf("execution engine '%s' not found or not initialized", engineName)
				a.logger.Error(err)
				return mcpTypes.NewToolResultError(err.Error()), nil
			}
		}

		// 2. Создаем контракт из конфигурации
		contract := contracts.ToolContract{
			Engine:     engineName,
			Name:       toolCfg.Name,
			EngineSpec: toolCfg.EngineSpec,
		}

		// Валидация аргументов
		for _, param := range toolCfg.Params {
			paramName := param["name"]
			isRequired := param["required"] == "true"
			if isRequired {
				if _, exists := args[paramName]; !exists {
					return mcpTypes.NewToolResultError(fmt.Sprintf("required parameter '%s' missing", paramName)), nil
				}
			}
		}

		// 3. Выполняем контракт через найденный движок
		result, err := engine.Execute(ctx, contract, args)
		if err != nil {
			a.logger.Errorf("Tool '%s' execution with engine '%s' failed: %v", toolCfg.Name, engineName, err)
			return mcpTypes.NewToolResultError(err.Error()), nil
		}

		return mcpTypes.NewToolResultText(result), nil
	}
}

// handleDaggerTool обратная совместимость для старых вызовов
func (a *PraxisAgent) handleDaggerTool(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
	// This is kept for backward compatibility
	args := req.GetArguments()

	contract := contracts.ToolContract{
		Engine: "dagger",
		Name:   "python_analyzer",
		EngineSpec: map[string]interface{}{
			"image":   "python:3.11-slim",
			"command": []string{"python", "/shared/analyzer.py"},
			"mounts":  map[string]string{a.appConfig.Agent.SharedDir: "/shared"},
		},
	}

	// Get or initialize Dagger Engine
	engine, ok := a.executionEngines["dagger"]
	if !ok {
		a.logger.Info("Initializing Dagger Engine on first use...")
		daggerEngine, err := dagger.NewEngine(ctx)
		if err != nil {
			a.logger.Errorf("Failed to initialize Dagger Engine: %v", err)
			return mcpTypes.NewToolResultError(fmt.Sprintf("Dagger Engine initialization failed: %v", err)), nil
		}
		a.executionEngines["dagger"] = daggerEngine
		engine = daggerEngine
		a.logger.Info("🚀 Dagger Engine initialized successfully")
	}

	result, err := engine.Execute(ctx, contract, args)
	if err != nil {
		a.logger.Errorf("Dagger tool execution failed: %v", err)
		return mcpTypes.NewToolResultError(err.Error()), nil
	}

	return mcpTypes.NewToolResultText(result), nil
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

	// Serve generated artifacts (reports) as static files
	// Files are saved by tools to /app/shared/reports (mounted from ./shared)
	a.httpServer.Static("/reports", "/app/shared/reports")

	a.httpServer.GET("/health", a.handleHealth)
	a.httpServer.GET("/agent/card", a.handleGetCard)
	a.httpServer.GET("/peers", a.handleListPeers)
	a.httpServer.POST("/execute", a.handleExecuteDSL)
	a.httpServer.GET("/p2p/cards", a.handleGetP2PCards)
	a.httpServer.POST("/p2p/tool", a.handleInvokeP2PTool)
	if a.identityManager != nil {
		a.httpServer.GET("/.well-known/did.json", a.handleGetDIDDocument)
	}

	// A2A endpoints
	a.httpServer.POST("/a2a/message/send", a.handleA2AMessageSend)
	a.httpServer.POST("/a2a/tasks/get", a.handleA2ATasksGet)
	a.httpServer.GET("/a2a/tasks", a.handleA2ATasksList)

	// A2A JSON-RPC endpoints for v0.2.9
	a.httpServer.POST("/a2a/v1", a.handleA2AJSONRPC) // Main A2A JSON-RPC endpoint
	a.httpServer.POST("/", a.handleA2AJSONRPC)       // Optional compatibility endpoint

	// A2A card endpoints
	a.httpServer.GET("/.well-known/agent-card.json", a.handleGetA2ACard)
	a.httpServer.GET("/v1/card", a.handleGetAuthenticatedExtendedCardHTTP)
	// ERC-8004 offchain data endpoints
	a.httpServer.GET("/.well-known/feedback.json", a.handleFeedbackData)
	a.httpServer.GET("/.well-known/validation-requests.json", a.handleValidationRequests)
	a.httpServer.GET("/.well-known/validation-responses.json", a.handleValidationResponses)
	// Admin: update registration entry after on-chain tx
	a.httpServer.POST("/admin/erc8004/register", a.handleAdminSetRegistration)

	a.logger.Info("✅ A2A well-known endpoint registered: /.well-known/agent-card.json")
	a.logger.Info("✅ A2A JSON-RPC endpoint registered: /a2a/v1")

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
	if a.orchestratorAnalyzer != nil {
		a.logger.Info("✅ Orchestrator analyzer initialized successfully with event bus integration")
	} else {
		a.logger.Error("❌ Failed to initialize orchestrator analyzer - it's nil!")
	}
}

func (a *PraxisAgent) initializeAgentCard() {
	// Build dynamic skills from config (engines + tools)
	dynamicSkills, engineNames := a.buildSkillsFromConfig()

	a.card = &AgentCard{
		Name:            a.name,
		Version:         a.version,
		ProtocolVersion: "0.2.9", // A2A Protocol Version
		URL:             fmt.Sprintf("http://localhost:%d/a2a/v1", a.httpPort),
		Description:     "Praxis P2P Agent with A2A and MCP support",
		Provider: &AgentProvider{
			Name:        "Praxis",
			Version:     a.version,
			Description: "Praxis Agent Framework",
		},
		Capabilities: AgentCapabilities{
			Streaming:              boolPtr(false), // No message/stream implementation
			PushNotifications:      boolPtr(false),
			StateTransitionHistory: boolPtr(true),
		},
		PreferredTransport: "JSONRPC",
		AdditionalInterfaces: []AgentInterface{
			{
				URL:       fmt.Sprintf("http://localhost:%d/a2a/v1", a.httpPort),
				Transport: "JSONRPC",
			},
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"application/json"},
		SecuritySchemes: map[string]interface{}{
			"none": map[string]interface{}{
				"type": "none",
			},
		},
		// Dynamic skills first (engines + declared tools), then core capabilities
		Skills: append(dynamicSkills, []AgentSkill{
			{
				ID:          "p2p-communication",
				Name:        "P2P Communication",
				Description: "Communicate with other agents via P2P network using A2A protocol",
				Tags:        []string{"p2p", "networking", "agent-to-agent", "a2a"},
			},
			{
				ID:          "task-management",
				Name:        "Task Management",
				Description: "Asynchronous task lifecycle management with A2A protocol",
				Tags:        []string{"a2a", "tasks", "async", "stateful"},
			},
			{
				ID:          "mcp-integration",
				Name:        "MCP Integration",
				Description: "Model Context Protocol support for tool invocation and discovery",
				Tags:        []string{"mcp", "tools", "resources", "discovery"},
			},
		}...),
		Metadata: map[string]interface{}{
			"implementation": "praxis-go-sdk",
			"runtime":        "go",
			"engines":        engineNames,
		},
	}

	// Update P2P protocol handler with our card
	if a.p2pProtocol != nil {
		// Convert to P2P card format
		p2pCard := &p2p.AgentCard{
			Name:         a.card.Name,
			Version:      a.card.Version,
			PeerID:       a.host.ID().String(),
			Capabilities: []string{"mcp", "dsl", "workflow", "p2p"},
			Tools:        []p2p.ToolSpec{}, // Will be populated based on registered MCP tools
			Timestamp:    time.Now().Unix(),
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

func (a *PraxisAgent) initializeA2ACard() {
	// Initialize canonical A2A card according to specification
	// Derive dynamic skills (engines + tools) for canonical A2A card
	dynamicSkills := a.buildA2ASkillsFromConfig()

	a.a2aCard = &a2a.AgentCard{
		ProtocolVersion: "0.2.9",
		Name:            a.name,
		Description:     "Praxis P2P Agent with A2A and MCP support",
		Capabilities: a2a.AgentCapabilities{
			Streaming:              false,
			PushNotifications:      false,
			StateTransitionHistory: true,
		},
		// Dynamic skills first, then core capabilities for completeness
		Skills: append(dynamicSkills, []a2a.AgentSkill{
			{
				ID:          "p2p-communication",
				Name:        "P2P Communication",
				Description: "Communicate with other agents via A2A protocol",
				Tags:        []string{"p2p", "a2a"},
			},
			{
				ID:          "task-management",
				Name:        "Task Management",
				Description: "Asynchronous task lifecycle management",
				Tags:        []string{"a2a", "tasks"},
			},
			{
				ID:          "mcp-integration",
				Name:        "MCP Integration",
				Description: "Model Context Protocol support for tool invocation and discovery",
				Tags:        []string{"mcp", "tools", "discovery"},
			},
		}...),
		DefaultInputModes:                 []string{"text/plain", "application/json"},
		DefaultOutputModes:                []string{"application/json"},
		SupportsAuthenticatedExtendedCard: true,
		// ERC-8004 top-level fields
		TrustModels: []string{"feedback", "inference-validation"},
		SecuritySchemes: map[string]any{
			"none": map[string]interface{}{
				"type": "none",
			},
		},
	}

	a.logger.Infof("✅ Canonical A2A Agent Card initialized for agent '%s' on port %d", a.name, a.httpPort)
	a.signA2ACard()
}

func (a *PraxisAgent) initializeIdentity() error {
	cfg := a.appConfig.Agent.Identity
	if cfg.DID == "" {
		a.logger.Warn("DID identity not configured; skipping DID endpoints")
		return nil
	}

	manager, err := NewIdentityManager(a.appConfig.Agent, a.logger)
	if err != nil {
		return fmt.Errorf("initialize identity manager: %w", err)
	}
	a.identityManager = manager

	if a.card != nil {
		a.card.DID = manager.DID()
		a.card.DIDDocURI = manager.DIDDocumentURI()
	}

	a.signA2ACard()

	allowInsecure := strings.HasPrefix(strings.ToLower(manager.DIDDocumentURI()), "http://") || strings.HasPrefix(strings.ToLower(a.appConfig.Agent.URL), "http://")
	webResolver := &didweb.Resolver{AllowInsecure: allowInsecure}
	webvhResolver := &didwebvh.Resolver{WebResolver: webResolver}

	options := []did.MultiResolverOption{
		did.WithWebResolver(webResolver),
		did.WithWebVHResolver(webvhResolver),
	}
	if ttl := a.appConfig.Agent.DIDCacheTTL; ttl > 0 {
		options = append(options, did.WithCacheTTL(ttl))
	}

	a.didResolver = did.NewMultiResolver(options...)

	if a.p2pProtocol != nil {
		a.p2pProtocol.SetDIDResolver(a.didResolver)
	}

	return nil
}

func (a *PraxisAgent) signA2ACard() {
	if a.identityManager == nil || a.a2aCard == nil {
		return
	}
	if err := a.identityManager.SignAgentCard(a.a2aCard); err != nil {
		a.logger.Errorf("Failed to sign A2A card: %v", err)
	}
}

// buildSkillsFromConfig constructs internal AgentCard skills from the loaded configuration.
// Returns the skills slice and the list of engine names discovered.
func (a *PraxisAgent) buildSkillsFromConfig() ([]AgentSkill, []string) {
	if a.appConfig == nil {
		return nil, []string{}
	}

	enginesSet := map[string]struct{}{}
	skills := make([]AgentSkill, 0, 8)

	// Collect engines present in tools
	for _, t := range a.appConfig.Agent.Tools {
		if t.Engine != "" {
			enginesSet[strings.ToLower(t.Engine)] = struct{}{}
		}
	}

	// Engine skills first
	if _, ok := enginesSet["dagger"]; ok {
		skills = append(skills, AgentSkill{
			ID:          "engine-dagger",
			Name:        "Dagger Engine",
			Description: "Executes containerized tools via Dagger engine",
			Tags:        []string{"engine", "dagger", "containers"},
		})
	}
	if _, ok := enginesSet["local-go"]; ok {
		skills = append(skills, AgentSkill{
			ID:          "engine-local",
			Name:        "Local Tools",
			Description: "Executes built-in tools on local runtime",
			Tags:        []string{"engine", "local-go", "filesystem"},
		})
	}

	// Represent each declared tool as a skill for discoverability
	for _, t := range a.appConfig.Agent.Tools {
		skills = append(skills, AgentSkill{
			ID:          strings.ToLower(t.Name),
			Name:        humanizeName(t.Name),
			Description: t.Description,
			Tags:        []string{"tool", strings.ToLower(t.Engine)},
		})
	}

	// Build engines list for metadata
	engineNames := make([]string, 0, len(enginesSet))
	for e := range enginesSet {
		engineNames = append(engineNames, e)
	}

	// Ensure deterministic order
	sort.Strings(engineNames)

	return skills, engineNames
}

// buildA2ASkillsFromConfig constructs A2A canonical skills from config.
func (a *PraxisAgent) buildA2ASkillsFromConfig() []a2a.AgentSkill {
	if a.appConfig == nil {
		return nil
	}

	enginesSet := map[string]struct{}{}
	skills := make([]a2a.AgentSkill, 0, 8)

	for _, t := range a.appConfig.Agent.Tools {
		if t.Engine != "" {
			enginesSet[strings.ToLower(t.Engine)] = struct{}{}
		}
	}

	// Engine skills
	if _, ok := enginesSet["dagger"]; ok {
		skills = append(skills, a2a.AgentSkill{
			ID:          "engine-dagger",
			Name:        "Dagger Engine",
			Description: "Executes containerized tools via Dagger engine",
			Tags:        []string{"engine", "dagger"},
		})
	}
	if _, ok := enginesSet["local-go"]; ok {
		skills = append(skills, a2a.AgentSkill{
			ID:          "engine-local",
			Name:        "Local Tools",
			Description: "Executes built-in tools on local runtime",
			Tags:        []string{"engine", "local-go"},
		})
	}

	// Tool skills
	for _, t := range a.appConfig.Agent.Tools {
		skills = append(skills, a2a.AgentSkill{
			ID:          strings.ToLower(t.Name),
			Name:        humanizeName(t.Name),
			Description: t.Description,
			Tags:        []string{"tool", strings.ToLower(t.Engine)},
		})
	}

	return skills
}

// humanizeName converts identifiers like "twitter_scraper" or "tg-poster" to "Twitter Scraper" or "Tg Poster".
func humanizeName(s string) string {
	if s == "" {
		return s
	}
	// Replace separators with spaces
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	// Collapse multiple spaces
	s = strings.Join(strings.Fields(s), " ")
	// Title case
	parts := strings.Split(s, " ")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
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

	// Stop execution engines
	for engineName, engine := range a.executionEngines {
		if engineName == "dagger" {
			if daggerEngine, ok := engine.(*dagger.DaggerEngine); ok {
				daggerEngine.Close()
				a.logger.Infof("Execution engine '%s' stopped", engineName)
			}
		}
	}

	// Stop transport manager
	if a.transportManager != nil {
		a.transportManager.Close()
		a.logger.Info("Transport manager stopped")
	}

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
		"status":  "healthy",
		"agent":   a.name,
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
			"id":        peerInfo.ID.String(),
			"connected": peerInfo.IsConnected,
			"foundAt":   peerInfo.FoundAt,
			"lastSeen":  peerInfo.LastSeen,
		})
	}

	c.JSON(200, gin.H{"peers": peers})
}

func (a *PraxisAgent) handleExecuteDSL(c *gin.Context) {
	// Read the request body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to read request body"})
		return
	}

	// Try to parse as JSON-RPC first
	var rpcRequest a2a.JSONRPCRequest
	if err := json.Unmarshal(bodyBytes, &rpcRequest); err == nil && rpcRequest.JSONRPC == "2.0" {
		// Handle as A2A JSON-RPC request
		a.logger.Infof("Received A2A JSON-RPC request. Method: %s, RequestID: %v", rpcRequest.Method, rpcRequest.ID)
		response := a.DispatchA2ARequest(rpcRequest)
		c.Header("Content-Type", "application/json")
		c.JSON(200, response)
		return
	}

	// Fallback: try legacy DSL format
	var legacyRequest struct {
		DSL string `json:"dsl"`
	}
	if err := json.Unmarshal(bodyBytes, &legacyRequest); err != nil || legacyRequest.DSL == "" {
		c.JSON(400, gin.H{"error": "Invalid request format - expected A2A JSON-RPC or legacy DSL"})
		return
	}

	a.logger.Infof("Received legacy DSL request, converting to A2A format")

	// Convert legacy DSL to A2A Message and create JSON-RPC request
	msg := a2a.Message{
		Role:      "user",
		Parts:     []a2a.Part{a2a.NewTextPart(legacyRequest.DSL)},
		MessageID: uuid.New().String(),
		Kind:      "message",
	}

	rpcRequest = a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "message/send",
		Params: map[string]interface{}{
			"message": map[string]interface{}{
				"role":      msg.Role,
				"parts":     msg.Parts,
				"messageId": msg.MessageID,
				"kind":      msg.Kind,
			},
		},
	}

	// Process through A2A dispatcher
	response := a.DispatchA2ARequest(rpcRequest)
	c.Header("Content-Type", "application/json")
	c.JSON(200, response)
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

// AdaptAppConfigToAgentConfig converts appconfig.AppConfig to agent.Config
func AdaptAppConfigToAgentConfig(appConfig *appconfig.AppConfig) Config {
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
		AppConfig:     appConfig,
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
			Name      string         `json:"name"`
			Arguments interface{}    `json:"arguments,omitempty"`
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

// GetLocalTools returns a list of all local tool names registered with the MCP server
func (a *PraxisAgent) GetLocalTools() []string {
	if a.mcpServer == nil {
		return []string{}
	}

	registeredTools := a.mcpServer.GetRegisteredTools()
	toolNames := make([]string, len(registeredTools))

	for i, tool := range registeredTools {
		toolNames[i] = tool.Name
	}

	return toolNames
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

// discoverAndRegisterExternalTools automatically discovers and registers tools from external MCP servers
func (a *PraxisAgent) discoverAndRegisterExternalTools(ctx context.Context) {
	// Support both field names for backward compatibility
	endpoints := a.appConfig.Agent.ExternalMCPEndpoints
	if len(endpoints) == 0 {
		endpoints = a.appConfig.Agent.ExternalMCPServers
	}
	if len(endpoints) == 0 {
		a.logger.Debug("No external MCP endpoints/servers configured for auto-discovery")
		return
	}

	a.logger.Infof("🔍 Starting discovery of external MCP tools from %d endpoints...", len(endpoints))

	// Get the remote MCP engine
	remoteEngine, exists := a.executionEngines["remote-mcp"]
	if !exists {
		a.logger.Error("Remote MCP engine not found, cannot discover external tools")
		return
	}

	// Create discovery service
	discoveryService := mcp.NewToolDiscoveryService(a.logger)

	for _, endpoint := range endpoints {
		addr := strings.TrimSpace(endpoint.URL)
		if addr == "" {
			a.logger.Warn("External MCP endpoint missing URL, skipping")
			continue
		}

		name := endpoint.Name
		if name == "" {
			name = addr
		}

		a.logger.Infof("🔗 Discovering tools from external MCP server at %s", addr)

		switch strings.ToLower(endpoint.Transport) {
		case "", "sse":
			a.transportManager.RegisterSSEEndpoint(name, addr, endpoint.Headers)
		case "http", "stream", "streamable_http":
			a.transportManager.RegisterHTTPEndpoint(name, addr, endpoint.Headers)
		default:
			a.logger.Warnf("Unsupported transport '%s' for %s, defaulting to SSE", endpoint.Transport, addr)
			a.transportManager.RegisterSSEEndpoint(name, addr, endpoint.Headers)
		}

		// Discover tools using the discovery service
		discoveredTools, err := discoveryService.DiscoverToolsFromServer(ctx, addr)
		if err != nil {
			a.logger.Errorf("Failed to discover tools from %s: %v", addr, err)
			// Fallback to hardcoded tools for backward compatibility
			a.registerFallbackTools(addr, remoteEngine)
			continue
		}

		// Register each discovered tool
		for _, tool := range discoveredTools {
			externalName := fmt.Sprintf("%s_external", tool.Name)

			// Create tool specification dynamically with proper input schema
			toolOptions := []mcpTypes.ToolOption{
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.Description, tool.ServerName)),
			}

			// Parse input schema and add parameters
			if tool.InputSchema.Properties != nil {
				for propName, propSchema := range tool.InputSchema.Properties {
					// Extract property details
					propMap, ok := propSchema.(map[string]interface{})
					if !ok {
						continue
					}

					propType, _ := propMap["type"].(string)
					propDesc, _ := propMap["description"].(string)

					// Add parameter based on type
					switch propType {
					case "string":
						toolOptions = append(toolOptions, mcpTypes.WithString(propName, mcpTypes.Description(propDesc)))
					case "integer", "number":
						toolOptions = append(toolOptions, mcpTypes.WithNumber(propName, mcpTypes.Description(propDesc)))
					case "boolean":
						toolOptions = append(toolOptions, mcpTypes.WithBoolean(propName, mcpTypes.Description(propDesc)))
					default:
						// For complex types, add as string for now
						toolOptions = append(toolOptions, mcpTypes.WithString(propName, mcpTypes.Description(propDesc)))
					}
				}
			}

			toolSpec := mcpTypes.NewTool(externalName, toolOptions...)

			// Create a handler that proxies to the external server
			toolNameCopy := tool.Name // Capture for closure
			addrCopy := addr          // Capture for closure

			handler := func(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
				a.logger.Debugf("Executing external tool %s via %s", toolNameCopy, addrCopy)

				// Prepare arguments for the remote call
				args := req.GetArguments()

				// Add tool_name to help the remote engine
				args["tool_name"] = toolNameCopy

				// Create contract for remote execution
				contract := contracts.ToolContract{
					Engine: "remote-mcp",
					Name:   toolNameCopy,
					EngineSpec: map[string]interface{}{
						"address": addrCopy,
					},
				}

				// Execute via remote engine
				result, err := remoteEngine.Execute(ctx, contract, args)
				if err != nil {
					a.logger.Errorf("Failed to execute external tool %s: %v", toolNameCopy, err)
					// Return error result
					return &mcpTypes.CallToolResult{
						IsError: true,
						Content: []mcpTypes.Content{
							&mcpTypes.TextContent{
								Type: "text",
								Text: fmt.Sprintf("Error: %v", err),
							},
						},
					}, nil
				}

				return &mcpTypes.CallToolResult{
					Content: []mcpTypes.Content{
						&mcpTypes.TextContent{
							Type: "text",
							Text: result,
						},
					},
				}, nil
			}

			// Register the tool with MCP server
			a.mcpServer.AddTool(toolSpec, handler)
			a.logger.Infof("✅ Registered external tool '%s' from %s", externalName, addr)
		}
	}

	// Update P2P card with new tools after discovery
	a.updateP2PCardWithTools()

	a.logger.Info("✨ External MCP tool discovery completed")
}

// registerFallbackTools registers hardcoded tools as fallback when discovery fails
func (a *PraxisAgent) registerFallbackTools(addr string, remoteEngine contracts.ExecutionEngine) {
	a.logger.Warn("Using fallback tool registration for backward compatibility")

	// Hardcoded common tools
	commonTools := []struct {
		name   string
		desc   string
		params []string
	}{
		{"read_file", "Read a file from external filesystem", []string{"path"}},
		{"write_file", "Write a file to external filesystem", []string{"path", "content"}},
		{"list_directory", "List directory contents from external filesystem", []string{"path"}},
		{"create_directory", "Create a directory in external filesystem", []string{"path"}},
	}

	for _, tool := range commonTools {
		externalName := fmt.Sprintf("%s_external", tool.name)

		// Create tool specification
		var toolSpec mcpTypes.Tool
		if tool.name == "write_file" {
			toolSpec = mcpTypes.NewTool(
				externalName,
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.desc, addr)),
				mcpTypes.WithString("path", mcpTypes.Description("Path parameter")),
				mcpTypes.WithString("content", mcpTypes.Description("Content to write")),
			)
		} else if len(tool.params) > 0 && tool.params[0] == "path" {
			toolSpec = mcpTypes.NewTool(
				externalName,
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.desc, addr)),
				mcpTypes.WithString("path", mcpTypes.Description("Path parameter")),
			)
		} else {
			toolSpec = mcpTypes.NewTool(
				externalName,
				mcpTypes.WithDescription(fmt.Sprintf("%s (via %s)", tool.desc, addr)),
			)
		}

		// Create handler
		toolNameCopy := tool.name
		addrCopy := addr

		handler := func(ctx context.Context, req mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
			args := req.GetArguments()
			args["tool_name"] = toolNameCopy

			contract := contracts.ToolContract{
				Engine: "remote-mcp",
				Name:   toolNameCopy,
				EngineSpec: map[string]interface{}{
					"address": addrCopy,
				},
			}

			result, err := remoteEngine.Execute(ctx, contract, args)
			if err != nil {
				return &mcpTypes.CallToolResult{
					IsError: true,
					Content: []mcpTypes.Content{
						&mcpTypes.TextContent{
							Type: "text",
							Text: fmt.Sprintf("Error: %v", err),
						},
					},
				}, nil
			}

			return &mcpTypes.CallToolResult{
				Content: []mcpTypes.Content{
					&mcpTypes.TextContent{
						Type: "text",
						Text: result,
					},
				},
			}, nil
		}

		a.mcpServer.AddTool(toolSpec, handler)
		a.logger.Infof("✅ Registered fallback tool '%s' from %s", externalName, addr)
	}
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

// ============= A2A Protocol Implementation =============

// DispatchA2ARequest handles JSON-RPC requests for A2A protocol
func (a *PraxisAgent) DispatchA2ARequest(req a2a.JSONRPCRequest) a2a.JSONRPCResponse {
	a.logger.Infof("A2A Request received. Method: %s, RequestID: %v", req.Method, req.ID)

	var result interface{}
	var rpcErr *a2a.RPCError

	params, ok := req.Params.(map[string]interface{})
	if !ok && req.Params != nil {
		rpcErr = a2a.NewRPCError(a2a.ErrorCodeInvalidParams, "Invalid params format")
		return a2a.NewJSONRPCErrorResponse(req.ID, rpcErr)
	}

	switch req.Method {
	case "message/send":
		result, rpcErr = a.handleMessageSend(params)
	case "tasks/get":
		result, rpcErr = a.handleTasksGet(params)
	case "tasks/cancel":
		result, rpcErr = a.handleTasksCancel(params)
	case "agent/getAuthenticatedExtendedCard":
		result, rpcErr = a.handleGetAuthenticatedExtendedCard(params)
	default:
		rpcErr = a2a.NewRPCError(a2a.ErrorCodeMethodNotFound, "Method not found")
	}

	if rpcErr != nil {
		return a2a.NewJSONRPCErrorResponse(req.ID, rpcErr)
	}
	return a2a.NewJSONRPCResponse(req.ID, result)
}

// handleMessageSend handles message/send JSON-RPC method
func (a *PraxisAgent) handleMessageSend(params map[string]interface{}) (interface{}, *a2a.RPCError) {
	// Parse message from params
	msgData, ok := params["message"].(map[string]interface{})
	if !ok {
		return nil, a2a.NewRPCError(a2a.ErrorCodeInvalidParams, "Missing message parameter")
	}

	// Convert to A2A Message
	msg, err := a.parseMessageFromParams(msgData)
	if err != nil {
		return nil, a2a.NewRPCError(a2a.ErrorCodeInvalidParams, fmt.Sprintf("Invalid message format: %v", err))
	}

	// Create task
	task := a.taskManager.CreateTask(*msg)
	a.logger.Infof("A2A Task %s created in 'submitted' state for message: %s", task.ID, msg.MessageID)

	// Start async processing
	go a.processTask(context.Background(), task)

	return task, nil
}

// handleTasksGet handles tasks/get JSON-RPC method
func (a *PraxisAgent) handleTasksGet(params map[string]interface{}) (interface{}, *a2a.RPCError) {
	taskID, ok := params["id"].(string)
	if !ok {
		return nil, a2a.NewRPCError(a2a.ErrorCodeInvalidParams, "Missing or invalid task id")
	}

	task, exists := a.taskManager.GetTask(taskID)
	if !exists {
		return nil, a2a.NewRPCError(a2a.ErrorCodeTaskNotFound, "Task not found")
	}

	return task, nil
}

// handleTasksCancel handles tasks/cancel JSON-RPC method
func (a *PraxisAgent) handleTasksCancel(params map[string]interface{}) (interface{}, *a2a.RPCError) {
	taskID, ok := params["id"].(string)
	if !ok {
		return nil, a2a.NewRPCError(a2a.ErrorCodeInvalidParams, "Missing or invalid task id")
	}

	task, err := a.taskManager.CancelTask(taskID)
	if err != nil {
		// Map task manager errors to A2A RPC errors
		if rpcErr, ok := err.(*a2a.RPCError); ok {
			return nil, rpcErr
		}
		// Fallback for unexpected errors
		return nil, a2a.NewRPCError(a2a.ErrorCodeInternalError, fmt.Sprintf("Failed to cancel task: %v", err))
	}

	return task, nil
}

// handleGetAuthenticatedExtendedCard handles agent/getAuthenticatedExtendedCard JSON-RPC method
func (a *PraxisAgent) handleGetAuthenticatedExtendedCard(params map[string]interface{}) (interface{}, *a2a.RPCError) {
	// For now, return the same canonical A2A card
	// In a full implementation, this would include extended information for authenticated clients
	if a.a2aCard == nil {
		return nil, a2a.NewRPCError(a2a.ErrorCodeInternalError, "A2A card not initialized")
	}

	return a.a2aCard, nil
}

// HandoffA2AOverP2P sends A2A message/send request over P2P JSON-RPC
func (a *PraxisAgent) HandoffA2AOverP2P(ctx context.Context, peerIDStr string, message a2a.Message) (*a2a.Task, error) {
	if a.p2pProtocol == nil {
		return nil, fmt.Errorf("P2P protocol handler not available")
	}

	// Parse peer ID string
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer ID %s: %w", peerIDStr, err)
	}

	// Create JSON-RPC request for message/send
	rpcRequest := a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  "message/send",
		Params: map[string]interface{}{
			"message": map[string]interface{}{
				"role":      message.Role,
				"parts":     message.Parts,
				"messageId": message.MessageID,
				"contextId": message.ContextID,
				"kind":      message.Kind,
			},
		},
	}

	// Send request over P2P
	response, err := a.p2pProtocol.SendA2ARequest(ctx, peerID, rpcRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send A2A request over P2P: %w", err)
	}

	// Parse response as Task
	if response.Error != nil {
		return nil, fmt.Errorf("A2A request failed: %s", response.Error.Message)
	}

	// Convert response result to Task
	taskData, ok := response.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response format from peer")
	}

	// Parse task from response
	taskBytes, err := json.Marshal(taskData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task data: %w", err)
	}

	var task a2a.Task
	if err := json.Unmarshal(taskBytes, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}

// processTask processes a task asynchronously
func (a *PraxisAgent) processTask(ctx context.Context, task *a2a.Task) {
	a.taskManager.UpdateTaskStatus(task.ID, "working", nil)
	a.logger.Infof("[TaskID: %s] Starting processing for user input: \"%s\"",
		task.ID, a.getTextFromMessage(task.History[0]))

	userText := a.getTextFromMessage(task.History[0])

	// Validate userText is not empty before processing
	if strings.TrimSpace(userText) == "" {
		a.logger.Errorf("[TaskID: %s] Empty user message received", task.ID)
		a.taskManager.UpdateTaskStatus(task.ID, "failed", &a2a.Message{
			Role:      "agent",
			Parts:     []a2a.Part{a2a.NewTextPart("Cannot process empty message. Please provide a valid request.")},
			MessageID: uuid.New().String(),
		})
		return
	}

	a.logger.Infof("[TaskID: %s] Processing user request: \"%s\" (length: %d)", task.ID, userText, len(userText))

	// Get execution plan (separated from execution)
	executionPlan, err := a.orchestratorAnalyzer.AnalyzeWithOrchestration(ctx, userText)
	if err != nil {
		a.logger.Errorf("[TaskID: %s] Failed to create execution plan: %v", task.ID, err)
		a.taskManager.UpdateTaskStatus(task.ID, "failed", &a2a.Message{
			Role:      "agent",
			Parts:     []a2a.Part{a2a.NewTextPart(fmt.Sprintf("Failed to analyze request: %v", err))},
			MessageID: uuid.New().String(),
		})
		return
	}

	a.logger.Infof("[TaskID: %s] Execution plan received", task.ID)

	// Create artifact with execution result
	artifactParts := []a2a.Part{a2a.NewDataPart(executionPlan)}

	artifact := a2a.NewArtifact(
		uuid.New().String(),
		"Execution Result",
		artifactParts,
	)

	a.taskManager.AddArtifactToTask(task.ID, *artifact)
	a.taskManager.UpdateTaskStatus(task.ID, "completed", &a2a.Message{
		Role:      "agent",
		Parts:     []a2a.Part{a2a.NewTextPart("Task completed successfully")},
		MessageID: uuid.New().String(),
	})

	a.logger.Infof("[TaskID: %s] Status updated to 'completed'", task.ID)
}

// parseMessageFromParams converts params map to A2A Message
func (a *PraxisAgent) parseMessageFromParams(msgData map[string]interface{}) (*a2a.Message, error) {
	role, _ := msgData["role"].(string)
	messageID, _ := msgData["messageId"].(string)
	contextID, _ := msgData["contextId"].(string)

	if role == "" {
		role = "user" // default
	}
	if messageID == "" {
		messageID = uuid.New().String()
	}

	// Parse parts
	var parts []a2a.Part
	if partsData, ok := msgData["parts"].([]interface{}); ok {
		for _, partInterface := range partsData {
			if partMap, ok := partInterface.(map[string]interface{}); ok {
				part := a2a.Part{}
				part.Kind, _ = partMap["kind"].(string)
				part.Text, _ = partMap["text"].(string)
				part.Data = partMap["data"]

				parts = append(parts, part)
			}
		}
	}

	// Validate that we have at least one non-empty text part
	hasValidText := false
	for _, part := range parts {
		if part.Kind == "text" && strings.TrimSpace(part.Text) != "" {
			hasValidText = true
			break
		}
	}

	if !hasValidText {
		return nil, fmt.Errorf("message must contain at least one non-empty text part")
	}

	msg := &a2a.Message{
		Role:      role,
		Parts:     parts,
		MessageID: messageID,
		ContextID: contextID,
		Kind:      "message",
	}

	return msg, nil
}

// getTextFromMessage extracts text content from a message
func (a *PraxisAgent) getTextFromMessage(msg a2a.Message) string {
	for _, part := range msg.Parts {
		if part.Kind == "text" && part.Text != "" {
			return part.Text
		}
	}
	return ""
}

// ============= A2A HTTP Handlers =============

// handleA2AMessageSend handles direct A2A message/send requests
func (a *PraxisAgent) handleA2AMessageSend(c *gin.Context) {
	var params a2a.MessageSendParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(400, a2a.NewJSONRPCErrorResponse(nil,
			a2a.NewRPCError(a2a.ErrorCodeInvalidParams, err.Error())))
		return
	}

	// Create JSON-RPC request
	rpcRequest := a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "message/send",
		Params: map[string]interface{}{
			"message": params.Message,
		},
	}

	response := a.DispatchA2ARequest(rpcRequest)
	c.JSON(200, response)
}

// handleA2ATasksGet handles direct A2A tasks/get requests
func (a *PraxisAgent) handleA2ATasksGet(c *gin.Context) {
	var params a2a.TasksGetParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(400, a2a.NewJSONRPCErrorResponse(nil,
			a2a.NewRPCError(a2a.ErrorCodeInvalidParams, err.Error())))
		return
	}

	// Create JSON-RPC request
	rpcRequest := a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tasks/get",
		Params: map[string]interface{}{
			"id": params.ID,
		},
	}

	response := a.DispatchA2ARequest(rpcRequest)
	c.JSON(200, response)
}

// handleA2ATasksList lists all tasks for debugging
func (a *PraxisAgent) handleA2ATasksList(c *gin.Context) {
	tasks := a.taskManager.ListTasks()
	counts := a.taskManager.GetTaskCount()

	c.JSON(200, gin.H{
		"tasks":  tasks,
		"counts": counts,
		"agent":  a.name,
	})
}

// handleGetA2ACard handles GET /.well-known/agent-card.json requests
func (a *PraxisAgent) handleGetA2ACard(c *gin.Context) {
	a.signA2ACard()
	a.logger.Infof("📋 A2A card requested via /.well-known/agent-card.json endpoint")

	if a.a2aCard == nil {
		a.logger.Error("❌ A2A card is nil when requested!")
		c.JSON(503, gin.H{"error": "A2A card not initialized"})
		return
	}

	a.logger.Infof("✅ Serving A2A card for agent '%s' (protocol version: %s)",
		a.a2aCard.Name, a.a2aCard.ProtocolVersion)

	c.Header("Content-Type", "application/json")
	c.JSON(200, a.a2aCard)
}

func (a *PraxisAgent) handleGetDIDDocument(c *gin.Context) {
	if a.identityManager == nil {
		c.JSON(404, gin.H{"error": "DID identity not configured"})
		return
	}

	doc := a.identityManager.DIDDocument()
	if doc == nil {
		c.JSON(503, gin.H{"error": "DID document not available"})
		return
	}

	docCopy := *doc
	docCopy.Context = normalizeContexts(docCopy.Context)

	a.logger.Infof("📄 Serving DID document for %s", docCopy.ID)
	c.Header("Content-Type", "application/json")
	c.JSON(200, &docCopy)
}

// handleGetAuthenticatedExtendedCardHTTP handles GET /v1/card requests
func (a *PraxisAgent) handleGetAuthenticatedExtendedCardHTTP(c *gin.Context) {
	if a.a2aCard == nil {
		c.JSON(503, gin.H{"error": "A2A card not initialized"})
		return
	}

	// For authenticated extended card, we could add additional information
	// For now, return the canonical card
	c.Header("Content-Type", "application/json")
	c.JSON(200, a.a2aCard)
}

// GetA2ACard returns the canonical A2A Agent Card (implements A2ACardProvider interface)
func (a *PraxisAgent) GetA2ACard() *a2a.AgentCard {
	return a.a2aCard
}

// handleA2AJSONRPC handles A2A JSON-RPC 2.0 requests
func (a *PraxisAgent) handleA2AJSONRPC(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, a2a.NewJSONRPCErrorResponse(nil, a2a.NewRPCError(a2a.ErrorCodeParseError, "Failed to read body")))
		return
	}
	var rpc a2a.JSONRPCRequest
	if err := json.Unmarshal(body, &rpc); err != nil || rpc.JSONRPC != "2.0" {
		c.JSON(400, a2a.NewJSONRPCErrorResponse(nil, a2a.NewRPCError(a2a.ErrorCodeParseError, "Invalid JSON-RPC 2.0 payload")))
		return
	}
	resp := a.DispatchA2ARequest(rpc)
	c.Header("Content-Type", "application/json")
	c.JSON(200, resp)
}

// --- ERC-8004 Offchain Data Handlers ---
// handleFeedbackData serves the offchain feedback list for this agent (client role).
func (a *PraxisAgent) handleFeedbackData(c *gin.Context) {
	// TODO: replace with real storage of feedback entries; minimal valid shape is an array
	data := []map[string]any{}
	c.Header("Content-Type", "application/json")
	c.JSON(200, data)
}

// handleValidationRequests serves mapping DataHash=>DataURI for validation requests (server role).
func (a *PraxisAgent) handleValidationRequests(c *gin.Context) {
	// TODO: back this by your task manager or validation storage
	data := map[string]string{}
	c.Header("Content-Type", "application/json")
	c.JSON(200, data)
}

// handleValidationResponses serves mapping DataHash=>DataURI for validators.
func (a *PraxisAgent) handleValidationResponses(c *gin.Context) {
	data := map[string]string{}
	c.Header("Content-Type", "application/json")
	c.JSON(200, data)
}

// SetERC8004Registration updates the A2A card with an on-chain registration record.
// Use this after successful IdentityRegistry.NewAgent/UpdateAgent calls.
func (a *PraxisAgent) SetERC8004Registration(chainID uint64, agentID uint64, agentAddress string, signature string) {
	if a.a2aCard == nil {
		return
	}
	caip10 := fmt.Sprintf("eip155:%d:%s", chainID, strings.ToLower(agentAddress))
	reg := a2a.ERC8004Registration{
		AgentID:      agentID,
		AgentAddress: caip10,
		Signature:    signature,
	}
	a.a2aCard.Registrations = append(a.a2aCard.Registrations, reg)
}

// handleAdminSetRegistration allows adding a registration entry via HTTP (for testing/admin flows).
// Body: {"chainId":11155111, "agentId":1, "agentAddress":"0x...", "signature":"0x..."}
func (a *PraxisAgent) handleAdminSetRegistration(c *gin.Context) {
	var req struct {
		ChainID       uint64 `json:"chainId"`
		AgentID       uint64 `json:"agentId"`
		AgentAddress  string `json:"agentAddress"`  // EOA 0x...
		AddressCAIP10 string `json:"addressCaip10"` // optional CAIP-10 (back-compat)
		Signature     string `json:"signature"`
		RegistryAddr  string `json:"registry,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	// Support either EOA (agentAddress) or CAIP-10 (addressCaip10)
	eoa := req.AgentAddress
	if eoa == "" && req.AddressCAIP10 != "" {
		parts := strings.Split(req.AddressCAIP10, ":")
		if len(parts) >= 3 {
			eoa = parts[len(parts)-1]
		}
	}
	a.SetERC8004Registration(req.ChainID, req.AgentID, eoa, req.Signature)
	c.JSON(200, gin.H{"status": "ok"})
}
