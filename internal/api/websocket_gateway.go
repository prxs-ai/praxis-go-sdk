package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/praxis/praxis-go-sdk/internal/dsl"
	"github.com/sirupsen/logrus"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512000
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for dev
	},
}

// MessageType represents the type of WebSocket message
type MessageType string

const (
	MessageDSLCommand      MessageType = "DSL_COMMAND"
	MessageExecuteWorkflow MessageType = "EXECUTE_WORKFLOW"
	MessageChatMessage     MessageType = "CHAT_MESSAGE"
)

// ClientMessage represents a message from the client
type ClientMessage struct {
	Type    MessageType            `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// Client represents a WebSocket client connection
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	clientID string
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

// WebSocketGateway manages WebSocket connections and message routing
type WebSocketGateway struct {
	hub                  *Hub
	eventBus             *bus.EventBus
	dslAnalyzer          *dsl.Analyzer
	orchestratorAnalyzer *dsl.OrchestratorAnalyzer
	orchestrator         WorkflowOrchestrator
	logger               *logrus.Logger
	port                 int
	broadcastMu          sync.Mutex
}

// WorkflowOrchestrator interface for workflow execution
type WorkflowOrchestrator interface {
	ExecuteWorkflow(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}) error
	ExecuteWorkflowWithOptions(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}, opts *dsl.WorkflowOptions) error
}

// NewWebSocketGateway creates a new WebSocket gateway
func NewWebSocketGateway(port int, eventBus *bus.EventBus, dslAnalyzer *dsl.Analyzer, logger *logrus.Logger) *WebSocketGateway {
	hub := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}

	gateway := &WebSocketGateway{
		hub:         hub,
		eventBus:    eventBus,
		dslAnalyzer: dslAnalyzer,
		logger:      logger,
		port:        port,
	}

	eventBus.SubscribeAll(gateway.handleEvent)

	return gateway
}

func (gw *WebSocketGateway) SetOrchestrator(orchestrator WorkflowOrchestrator) {
	gw.orchestrator = orchestrator
}

func (gw *WebSocketGateway) SetOrchestratorAnalyzer(analyzer *dsl.OrchestratorAnalyzer) {
	gw.orchestratorAnalyzer = analyzer
	if analyzer != nil {
		gw.logger.Info("✅ OrchestratorAnalyzer set successfully in WebSocket Gateway")
	} else {
		gw.logger.Warn("⚠️ OrchestratorAnalyzer is nil in WebSocket Gateway")
	}
}

func (gw *WebSocketGateway) Run() error {
	go gw.hub.run()
	http.HandleFunc("/ws/workflow", gw.handleWebSocket)
	addr := fmt.Sprintf(":%d", gw.port)
	gw.logger.Infof("WebSocket Gateway starting on %s", addr)
	return http.ListenAndServe(addr, nil)
}

func (gw *WebSocketGateway) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		gw.logger.Errorf("WebSocket upgrade failed: %v", err)
		return
	}

	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())
	client := &Client{
		hub:      gw.hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		clientID: clientID,
	}

	gw.hub.register <- client
	gw.logger.Infof("New WebSocket client connected: %s", clientID)

	go client.writePump()
	go gw.readPump(client)
}

func (gw *WebSocketGateway) readPump(client *Client) {
	defer func() {
		client.hub.unregister <- client
		client.conn.Close()
		gw.logger.Infof("WebSocket client disconnected: %s", client.clientID)
	}()

	client.conn.SetReadLimit(maxMessageSize)
	client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				gw.logger.Errorf("WebSocket error: %v", err)
			}
			break
		}

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			gw.logger.Errorf("Failed to parse WebSocket message: %v", err)
			continue
		}

		gw.handleClientMessage(client, msg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
			n := len(c.send)
			for i := 0; i < n; i++ {
				if err := c.conn.WriteMessage(websocket.TextMessage, <-c.send); err != nil {
					return
				}
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (gw *WebSocketGateway) handleClientMessage(client *Client, msg ClientMessage) {
	gw.logger.Debugf("Received message type %s from client %s", msg.Type, client.clientID)

	switch msg.Type {
	case MessageDSLCommand:
		gw.handleDSLCommand(client, msg.Payload)
	case MessageExecuteWorkflow:
		gw.handleExecuteWorkflow(client, msg.Payload)
	case MessageChatMessage:
		gw.handleChatMessage(client, msg.Payload)
	default:
		gw.logger.Warnf("Unknown message type: %s", msg.Type)
	}
}

func (gw *WebSocketGateway) handleDSLCommand(client *Client, payload map[string]interface{}) {
	command, ok := payload["command"].(string)
	if !ok {
		gw.sendError(client, "Invalid DSL command format")
		return
	}

	workflowID, _ := payload["workflowId"].(string)
	if workflowID == "" {
		workflowID = fmt.Sprintf("workflow-%d", time.Now().UnixNano())
	}

	ctx := context.Background()
	var result interface{}
	var err error

	if gw.orchestratorAnalyzer != nil {
		gw.logger.Info("Using OrchestratorAnalyzer for DSL command")
		result, err = gw.orchestratorAnalyzer.AnalyzeWithOrchestration(ctx, command)
	} else {
		gw.logger.Info("Using basic Analyzer for DSL command")
		gw.eventBus.PublishDSLProgress("parsing", "Parsing DSL command...", map[string]interface{}{
			"command": command,
		})
		result, err = gw.dslAnalyzer.AnalyzeDSL(ctx, command)
	}

	if err != nil {
		gw.eventBus.Publish(bus.Event{
			Type: bus.EventDSLResult,
			Payload: map[string]interface{}{
				"success": false,
				"command": command,
				"error":   err.Error(),
			},
		})
		return
	}

	workflowSuggestion := gw.createWorkflowFromDSL(result)

	gw.eventBus.Publish(bus.Event{
		Type: bus.EventDSLResult,
		Payload: map[string]interface{}{
			"success":            true,
			"command":            command,
			"result":             result,
			"workflowSuggestion": workflowSuggestion,
		},
	})
}

func (gw *WebSocketGateway) handleExecuteWorkflow(client *Client, payload map[string]interface{}) {
	workflowID, _ := payload["workflowId"].(string)
	if workflowID == "" {
		workflowID = fmt.Sprintf("workflow-%d", time.Now().UnixNano())
	}

	nodes, _ := payload["nodes"].([]interface{})
	edges, _ := payload["edges"].([]interface{})

	params := map[string]interface{}{}
	if p, ok := payload["params"].(map[string]interface{}); ok {
		params = p
	}

	secrets := map[string]string{}
	if s, ok := payload["secrets"].(map[string]interface{}); ok {
		for k, v := range s {
			if str, ok := v.(string); ok {
				secrets[k] = str
			}
		}
	}

	if len(nodes) == 0 {
		gw.sendError(client, "No nodes in workflow")
		return
	}

	gw.eventBus.Publish(bus.Event{
		Type: bus.EventWorkflowStart,
		Payload: map[string]interface{}{
			"workflowId": workflowID,
			"params":     params,
			"secrets":    maskSecrets(secrets),
		},
	})

	if gw.orchestrator != nil {
		ctx := context.Background()
		go func() {
			opts := &dsl.WorkflowOptions{Params: params, Secrets: secrets}
			err := gw.orchestrator.ExecuteWorkflowWithOptions(ctx, workflowID, nodes, edges, opts)
			if err != nil {
				gw.eventBus.PublishWorkflowError(workflowID, err.Error(), "")
			}
		}()
	} else {
		gw.simulateWorkflowExecution(workflowID, nodes, edges)
	}
}

func (gw *WebSocketGateway) handleChatMessage(client *Client, payload map[string]interface{}) {
	content, _ := payload["content"].(string)
	gw.logger.Infof("📨 Received chat message: %s", content)

	if gw.orchestratorAnalyzer == nil {
		gw.logger.Warn("⚠️ OrchestratorAnalyzer is nil - cannot process message!")
	} else {
		gw.logger.Info("✅ OrchestratorAnalyzer is available")
	}

	gw.eventBus.Publish(bus.Event{
		Type: bus.EventChatMessage,
		Payload: map[string]interface{}{
			"content": fmt.Sprintf("Processing: %s", content),
			"sender":  "assistant",
		},
	})

	if content != "" {
		if gw.orchestratorAnalyzer != nil {
			go func() {
				ctx := context.Background()
				gw.logger.Infof("🚀 Processing chat message with OrchestratorAnalyzer: %s", content)
				result, err := gw.orchestratorAnalyzer.AnalyzeWithOrchestration(ctx, content)
				if err != nil {
					gw.logger.Errorf("❌ Failed to analyze DSL command: %v", err)
					gw.eventBus.Publish(bus.Event{
						Type: bus.EventChatMessage,
						Payload: map[string]interface{}{
							"content": fmt.Sprintf("❌ Error: %v", err),
							"sender":  "system",
						},
					})
					return
				}
				gw.eventBus.Publish(bus.Event{
					Type: bus.EventChatMessage,
					Payload: map[string]interface{}{
						"content": "✅ Command executed successfully",
						"sender":  "system",
					},
				})
				gw.logger.Infof("✅ DSL command result: %v", result)
			}()
		} else if gw.dslAnalyzer != nil {
			gw.logger.Warn("⚠️ Using fallback DSL analyzer (orchestrator not available)")
			go func() {
				ctx := context.Background()
				gw.logger.Infof("Processing chat message with DSL analyzer: %s", content)
				result, err := gw.dslAnalyzer.AnalyzeDSL(ctx, content)
				if err != nil {
					gw.logger.Errorf("Failed to analyze DSL: %v", err)
					gw.eventBus.Publish(bus.Event{
						Type: bus.EventChatMessage,
						Payload: map[string]interface{}{
							"content": fmt.Sprintf("❌ Error: %v", err),
							"sender":  "system",
						},
					})
					return
				}
				gw.eventBus.Publish(bus.Event{
					Type: bus.EventChatMessage,
					Payload: map[string]interface{}{
						"content": fmt.Sprintf("✅ Result: %v", result),
						"sender":  "system",
					},
				})
			}()
		}
	}
}

func (gw *WebSocketGateway) handleEvent(event bus.Event) {
	gw.broadcastMu.Lock()
	defer gw.broadcastMu.Unlock()

	wsMessage := map[string]interface{}{
		"type":    string(event.Type),
		"payload": event.Payload,
	}

	messageBytes, err := json.Marshal(wsMessage)
	if err != nil {
		gw.logger.Errorf("Failed to marshal event: %v", err)
		return
	}

	gw.hub.broadcast <- messageBytes
	time.Sleep(20 * time.Millisecond)
}

func (gw *WebSocketGateway) sendError(client *Client, message string) {
	errorMsg := map[string]interface{}{
		"type": "error",
		"payload": map[string]interface{}{
			"message": message,
		},
	}
	msgBytes, _ := json.Marshal(errorMsg)
	client.send <- msgBytes
}

func (gw *WebSocketGateway) createWorkflowFromDSL(result interface{}) map[string]interface{} {
	nodes := []map[string]interface{}{
		{
			"id":       "orchestrator",
			"type":     "orchestrator",
			"position": map[string]int{"x": 100, "y": 100},
			"data": map[string]interface{}{
				"label": "Workflow Orchestrator",
				"type":  "orchestrator",
			},
		},
		{
			"id":       "executor",
			"type":     "executor",
			"position": map[string]int{"x": 400, "y": 100},
			"data": map[string]interface{}{
				"label": "P2P Executor",
				"type":  "executor",
			},
		},
	}
	edges := []map[string]interface{}{
		{"id": "e1", "source": "orchestrator", "target": "executor", "type": "custom"},
	}
	return map[string]interface{}{"nodes": nodes, "edges": edges}
}

func (gw *WebSocketGateway) simulateWorkflowExecution(workflowID string, nodes []interface{}, edges []interface{}) {
	go func() {
		for i, node := range nodes {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				continue
			}
			nodeID, _ := nodeMap["id"].(string)
			gw.eventBus.PublishNodeStatusUpdate(workflowID, nodeID, "running")
			time.Sleep(2 * time.Second)
			gw.eventBus.PublishWorkflowLog(workflowID, "info",
				fmt.Sprintf("Processing node %d of %d", i+1, len(nodes)),
				"simulator", nodeID)
			gw.eventBus.PublishNodeStatusUpdate(workflowID, nodeID, "success")
			time.Sleep(1 * time.Second)
		}
		gw.eventBus.PublishWorkflowComplete(workflowID, map[string]interface{}{
			"message": "Workflow completed successfully",
		})
	}()
}

func maskSecrets(secrets map[string]string) map[string]string {
	masked := make(map[string]string, len(secrets))
	for k := range secrets {
		masked[k] = "***"
	}
	return masked
}
