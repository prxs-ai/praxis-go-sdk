package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type SSEConnection struct {
	w      http.ResponseWriter
	r      *http.Request
	done   chan struct{}
	mu     sync.Mutex
}

type MCPSSEServer struct {
	config      MCPServerConfig
	logger      *logrus.Logger
	connections map[string]*SSEConnection
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	httpServer  *http.Server
	tools       []MCPTool
	resources   []MCPResource
	initialized bool
}

func NewMCPSSEServer(config MCPServerConfig, logger *logrus.Logger) *MCPSSEServer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &MCPSSEServer{
		config:      config,
		logger:      logger,
		connections: make(map[string]*SSEConnection),
		ctx:         ctx,
		cancel:      cancel,
		tools:       []MCPTool{},
		resources:   []MCPResource{},
	}
}

func (s *MCPSSEServer) Start() error {
	port := s.extractPortFromURL()
	if port == 0 {
		port = 8080
	}
	
	s.logger.Infof("🌐 [MCP SSE] Starting SSE server %s on port %d", s.config.Name, port)
	
	router := gin.New()
	router.Use(gin.Recovery())
	
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})
	
	router.GET("/sse", s.handleSSE)
	router.POST("/message", s.handleMessage)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "server": s.config.Name})
	})
	
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: router,
	}
	
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("❌ [MCP SSE] Server %s failed: %v", s.config.Name, err)
		}
	}()
	
	if err := s.initializeCapabilities(); err != nil {
		s.logger.Warnf("⚠️ [MCP SSE] Failed to initialize capabilities for %s: %v", s.config.Name, err)
	}
	
	s.logger.Infof("✅ [MCP SSE] Server %s started successfully", s.config.Name)
	return nil
}

func (s *MCPSSEServer) Stop() error {
	s.logger.Infof("🛑 [MCP SSE] Stopping SSE server %s", s.config.Name)
	
	s.cancel()
	
	s.mu.Lock()
	for id, conn := range s.connections {
		close(conn.done)
		delete(s.connections, id)
	}
	s.mu.Unlock()
	
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.logger.Errorf("❌ [MCP SSE] Failed to shutdown server %s: %v", s.config.Name, err)
			return err
		}
	}
	
	s.logger.Infof("✅ [MCP SSE] Server %s stopped", s.config.Name)
	return nil
}

func (s *MCPSSEServer) handleSSE(c *gin.Context) {
	clientID := c.Query("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("client-%d", time.Now().UnixNano())
	}
	
	s.logger.Infof("🔗 [MCP SSE] New SSE connection from client %s", clientID)
	
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		s.logger.Error("❌ [MCP SSE] Response writer does not support flushing")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	
	conn := &SSEConnection{
		w:    c.Writer,
		r:    c.Request,
		done: make(chan struct{}),
	}
	
	s.mu.Lock()
	s.connections[clientID] = conn
	s.mu.Unlock()
	
	defer func() {
		s.mu.Lock()
		delete(s.connections, clientID)
		s.mu.Unlock()
		s.logger.Infof("🔌 [MCP SSE] Client %s disconnected", clientID)
	}()
	
	s.sendInitialData(conn)
	flusher.Flush()
	
	select {
	case <-conn.done:
		return
	case <-s.ctx.Done():
		return
	case <-c.Request.Context().Done():
		return
	}
}

func (s *MCPSSEServer) handleMessage(c *gin.Context) {
	var request map[string]interface{}
	if err := c.ShouldBindJSON(&request); err != nil {
		s.logger.Errorf("❌ [MCP SSE] Invalid JSON in request: %v", err)
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}
	
	response := s.processRequest(request)
	c.JSON(200, response)
}

func (s *MCPSSEServer) sendInitialData(conn *SSEConnection) {
	initMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params": map[string]interface{}{
			"server_name": s.config.Name,
			"tools":       s.tools,
			"resources":   s.resources,
		},
	}
	
	s.sendSSEMessage(conn, "initialized", initMessage)
}

func (s *MCPSSEServer) sendSSEMessage(conn *SSEConnection, eventType string, data interface{}) {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	
	jsonData, err := json.Marshal(data)
	if err != nil {
		s.logger.Errorf("❌ [MCP SSE] Failed to marshal message: %v", err)
		return
	}
	
	fmt.Fprintf(conn.w, "event: %s\n", eventType)
	fmt.Fprintf(conn.w, "data: %s\n\n", string(jsonData))
	
	if flusher, ok := conn.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *MCPSSEServer) processRequest(request map[string]interface{}) map[string]interface{} {
	method, ok := request["method"].(string)
	if !ok {
		return s.createErrorResponse("", "Missing or invalid method")
	}
	
	id, _ := request["id"].(string)
	
	switch method {
	case "tools/list":
		return s.createSuccessResponse(id, map[string]interface{}{
			"tools": s.tools,
		})
	case "resources/list":
		return s.createSuccessResponse(id, map[string]interface{}{
			"resources": s.resources,
		})
	case "tools/call":
		return s.handleToolCall(id, request)
	case "initialize":
		s.initialized = true
		return s.createSuccessResponse(id, map[string]interface{}{
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{"listChanged": true},
				"resources": map[string]interface{}{"listChanged": true},
			},
			"serverInfo": map[string]interface{}{
				"name":    s.config.Name,
				"version": "1.0.0",
			},
		})
	default:
		return s.createErrorResponse(id, fmt.Sprintf("Unknown method: %s", method))
	}
}

func (s *MCPSSEServer) handleToolCall(id string, request map[string]interface{}) map[string]interface{} {
	params, ok := request["params"].(map[string]interface{})
	if !ok {
		return s.createErrorResponse(id, "Missing or invalid params")
	}
	
	toolName, ok := params["name"].(string)
	if !ok {
		return s.createErrorResponse(id, "Missing tool name")
	}
	
	args, _ := params["arguments"].(map[string]interface{})
	
	result := s.executeFilesystemTool(toolName, args)
	
	return s.createSuccessResponse(id, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": result,
			},
		},
	})
}

func (s *MCPSSEServer) executeFilesystemTool(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "read_file":
		if path, ok := args["path"].(string); ok {
			return fmt.Sprintf("Reading file: %s (SSE filesystem tool)", path)
		}
		return "Error: missing path parameter"
	
	case "write_file":
		if path, ok := args["path"].(string); ok {
			content, _ := args["content"].(string)
			return fmt.Sprintf("Writing to file: %s with content length: %d (SSE filesystem tool)", path, len(content))
		}
		return "Error: missing path parameter"
	
	case "list_directory":
		if path, ok := args["path"].(string); ok {
			return fmt.Sprintf("Listing directory: %s (SSE filesystem tool)", path)
		}
		return "Error: missing path parameter"
	
	case "create_directory":
		if path, ok := args["path"].(string); ok {
			return fmt.Sprintf("Creating directory: %s (SSE filesystem tool)", path)
		}
		return "Error: missing path parameter"
	
	default:
		return fmt.Sprintf("Unknown tool: %s", toolName)
	}
}

func (s *MCPSSEServer) createSuccessResponse(id string, result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}

func (s *MCPSSEServer) createErrorResponse(id, message string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    -1,
			"message": message,
		},
	}
}

func (s *MCPSSEServer) initializeCapabilities() error {
	s.tools = []MCPTool{
		{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to read",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to write",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "list_directory",
			Description: "List the contents of a directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the directory to list",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "create_directory",
			Description: "Create a new directory",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the directory to create",
					},
				},
				"required": []string{"path"},
			},
		},
	}
	
	s.resources = []MCPResource{
		{
			URI:         "file://filesystem",
			Name:        "Filesystem Access",
			Description: "Access to filesystem operations via SSE",
			MimeType:    "application/json",
		},
	}
	
	return nil
}

func (s *MCPSSEServer) extractPortFromURL() int {
	if s.config.URL == "" {
		return 0
	}
	
	parts := strings.Split(s.config.URL, ":")
	if len(parts) < 2 {
		return 0
	}
	
	portStr := parts[len(parts)-1]
	if strings.Contains(portStr, "/") {
		portStr = strings.Split(portStr, "/")[0]
	}
	
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	
	return port
}

func (s *MCPSSEServer) GetTools() []MCPTool {
	return s.tools
}

func (s *MCPSSEServer) GetResources() []MCPResource {
	return s.resources
}

func (s *MCPSSEServer) IsReady() bool {
	return s.initialized
}