package agent

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	mcp "github.com/metoro-io/mcp-golang"
	mcphttp "github.com/metoro-io/mcp-golang/transport/http"
	"github.com/sirupsen/logrus"

	"praxis-go-sdk/internal/config"
	"praxis-go-sdk/internal/llm"
)

// APIServer provides HTTP API for the agent
type APIServer struct {
	agent      Agent
	config     *config.HTTPConfig
	httpServer *http.Server
	router     *gin.Engine
	logger     *logrus.Logger
}

// NewAPIServer creates a new API server
func NewAPIServer(agent Agent, cfg *config.HTTPConfig, logger *logrus.Logger) *APIServer {
	// Set Gin mode
	gin.SetMode(gin.ReleaseMode)

	// Create router
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	server := &APIServer{
		agent:  agent,
		config: cfg,
		router: router,
		logger: logger,
	}

	// Register routes
	server.registerRoutes()

	return server
}

// Start starts the API server
func (s *APIServer) Start() error {
	if !s.config.Enabled {
		s.logger.Info("HTTP server is disabled")
		return nil
	}

	s.logger.Infof("Starting HTTP server on %s:%d", s.config.Host, s.config.Port)

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler: s.router,
	}

	// Start server in a goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	return nil
}

// Shutdown stops the API server
func (s *APIServer) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}

	s.logger.Info("Shutting down HTTP server...")

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown the server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown error: %w", err)
	}

	s.logger.Info("HTTP server shutdown complete")
	return nil
}

// registerRoutes registers the API routes
func (s *APIServer) registerRoutes() {
	// Agent card routes
	s.router.GET("/card", s.getAgentCard)
	s.router.GET("/.well-known/agent-card.json", s.getAgentCard)
	s.router.GET("/a2a/agent-card", s.getAgentCard)

	// Health check
	s.router.GET("/health", s.getHealth)

	// P2P routes
	s.router.GET("/p2p/info", s.getP2PInfo)
	s.router.GET("/p2p/status", s.getP2PStatus)
	s.router.POST("/p2p/connect/:peer_name", s.connectToPeer)
	s.router.POST("/p2p/request-card/:peer_name", s.requestPeerCard)

	// MCP routes
	s.router.GET("/mcp/status", s.getMCPStatus)
	s.router.GET("/mcp/tools", s.getMCPTools)
	s.router.GET("/mcp/resources", s.getMCPResources)
	s.router.POST("/mcp/invoke/:peer_name/:server_name/:tool_name", s.invokeTool)

	// LLM routes
	s.router.POST("/llm/chat", s.processLLMRequest)
	s.router.GET("/llm/tools", s.getLLMTools)
	s.router.GET("/llm/status", s.getLLMStatus)
	s.router.GET("/llm/health", s.getLLMHealth)

	// Agent registry
	s.router.POST("/find_agent", s.findAgent)

	// Echo route for testing
	s.router.POST("/echo", s.echo)
}

// getAgentCard returns the agent card
func (s *APIServer) getAgentCard(c *gin.Context) {
	c.JSON(http.StatusOK, s.agent.GetCard())
}

// getHealth returns the agent health status
func (s *APIServer) getHealth(c *gin.Context) {
	p2pStatus := "disabled"
	if host := s.agent.GetP2PHost(); host != nil {
		p2pStatus = "active"
	}

	mcpStatus := "disabled"
	if bridge := s.agent.GetMCPBridge(); bridge != nil {
		mcpStatus = "active"
	}

	llmStatus := "disabled"
	if client := s.agent.GetLLMClient(); client != nil {
		llmStatus = "active"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"services": gin.H{
			"p2p": p2pStatus,
			"mcp": mcpStatus,
			"llm": llmStatus,
		},
	})
}

// getP2PInfo returns information about the P2P host
func (s *APIServer) getP2PInfo(c *gin.Context) {
	host := s.agent.GetP2PHost()
	if host == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "P2P not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"peer_id":   host.GetID().String(),
		"addresses": host.GetAddresses(),
	})
}

// getP2PStatus returns the P2P status
func (s *APIServer) getP2PStatus(c *gin.Context) {
	host := s.agent.GetP2PHost()
	if host == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "P2P not initialized"})
		return
	}

	// In a real implementation, this would return connection status
	c.JSON(http.StatusOK, gin.H{
		"peer_id":         host.GetID().String(),
		"connections":     0,
		"connected_peers": []string{},
	})
}

// connectToPeer connects to a peer
func (s *APIServer) connectToPeer(c *gin.Context) {
	peerName := c.Param("peer_name")

	if err := s.agent.ConnectToPeer(peerName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "connected",
		"peer":    peerName,
		"message": fmt.Sprintf("Successfully connected to %s", peerName),
	})
}

// requestPeerCard requests a card from a peer
func (s *APIServer) requestPeerCard(c *gin.Context) {
	peerName := c.Param("peer_name")

	card, err := s.agent.RequestCard(peerName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"peer":   peerName,
		"card":   card,
	})
}

// getMCPStatus returns the MCP status
func (s *APIServer) getMCPStatus(c *gin.Context) {
	bridge := s.agent.GetMCPBridge()
	if bridge == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "active",
		"stats":  bridge.GetStats(),
	})
}

// getMCPTools returns the MCP tools
func (s *APIServer) getMCPTools(c *gin.Context) {
	bridge := s.agent.GetMCPBridge()
	if bridge == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tools": bridge.ListAllTools(),
	})
}

// getMCPResources returns the MCP resources
func (s *APIServer) getMCPResources(c *gin.Context) {
	bridge := s.agent.GetMCPBridge()
	if bridge == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"resources": bridge.ListAllResources(),
	})
}

// invokeTool invokes a tool
func (s *APIServer) invokeTool(c *gin.Context) {
	bridge := s.agent.GetMCPBridge()
	if bridge == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP bridge not initialized"})
		return
	}

	peerName := c.Param("peer_name")
	serverName := c.Param("server_name")
	toolName := c.Param("tool_name")

	var params map[string]interface{}
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// For now, we'll just return a mock response
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"response": gin.H{
			"result": fmt.Sprintf("Invoked %s on %s for peer %s", toolName, serverName, peerName),
		},
	})
}

// processLLMRequest processes an LLM request
func (s *APIServer) processLLMRequest(c *gin.Context) {
	client := s.agent.GetLLMClient()
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM client not available"})
		return
	}

	var req llm.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate request
	if req.UserInput == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_input is required"})
		return
	}

	// Generate request ID if not provided
	if req.ID == "" {
		req.ID = fmt.Sprintf("llm_%d", time.Now().UnixNano())
	}

	// Process request
	resp, err := s.agent.ProcessLLMRequest(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// getLLMTools returns the LLM tools
func (s *APIServer) getLLMTools(c *gin.Context) {
	client := s.agent.GetLLMClient()
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM client not available"})
		return
	}

	tools := client.GetAvailableTools()
	c.JSON(http.StatusOK, gin.H{
		"tools": tools,
		"count": len(tools),
	})
}

// getLLMStatus returns the LLM status
func (s *APIServer) getLLMStatus(c *gin.Context) {
	client := s.agent.GetLLMClient()
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unavailable",
			"enabled": false,
		})
		return
	}

	metrics := client.GetMetrics()
	c.JSON(http.StatusOK, gin.H{
		"status":  "active",
		"enabled": true,
		"metrics": metrics,
	})
}

// getLLMHealth checks the LLM health
func (s *APIServer) getLLMHealth(c *gin.Context) {
	client := s.agent.GetLLMClient()
	if client == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM client not available"})
		return
	}

	if err := client.Health(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

// findAgent calls the AI registry MCP server to locate agents by goal
func (s *APIServer) findAgent(c *gin.Context) {
	var input struct {
		Goal string `json:"goal"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Goal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "goal is required"})
		return
	}

	transport := mcphttp.NewHTTPClientTransport("/llm/mcp/")
	transport.WithBaseURL("http://ai-registry.prxs.ai:8000")

	client := mcp.NewClient(transport)
	ctx := context.Background()
	if _, err := client.Initialize(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp, err := client.CallTool(ctx, "find_agent", map[string]interface{}{"goal": input.Goal})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// echo echoes the input message
func (s *APIServer) echo(c *gin.Context) {
	var input struct {
		Text string `json:"text"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"result": fmt.Sprintf("Echo: %s", input.Text),
	})
}
