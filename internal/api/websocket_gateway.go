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
	"github.com/praxis/praxis-go-sdk/internal/workflow"
	"github.com/sirupsen/logrus"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512000
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from any origin for development
		// TODO: Restrict in production
		return true
	},
}

// MessageType represents the type of WebSocket message
type MessageType string

const (
	// Client -> Server messages
	MessageDSLCommand      MessageType = "DSL_COMMAND"
	MessageExecuteWorkflow MessageType = "EXECUTE_WORKFLOW"
	MessageChatMessage     MessageType = "CHAT_MESSAGE"

	// Server -> Client messages (handled by EventBus)
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
	broadcastMu          sync.Mutex // Prevent concurrent message sending
}

// WorkflowOrchestrator interface for workflow execution
type WorkflowOrchestrator interface {
	ExecuteWorkflow(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}) error
}

type workflowExecWithOpts interface {
	ExecuteWorkflowWithOptions(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}, opts *workflow.WorkflowOptions) error
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

	// Subscribe to all events from EventBus
	eventBus.SubscribeAll(gateway.handleEvent)

	return gateway
}

// SetOrchestrator sets the workflow orchestrator
func (gw *WebSocketGateway) SetOrchestrator(orchestrator WorkflowOrchestrator) {
	gw.orchestrator = orchestrator
}

// SetOrchestratorAnalyzer sets the orchestrator analyzer for complex DSL handling
func (gw *WebSocketGateway) SetOrchestratorAnalyzer(analyzer *dsl.OrchestratorAnalyzer) {
	gw.orchestratorAnalyzer = analyzer
	if analyzer != nil {
		gw.logger.Info("✅ OrchestratorAnalyzer set successfully in WebSocket Gateway")
	} else {
		gw.logger.Warn("⚠️ OrchestratorAnalyzer is nil in WebSocket Gateway")
	}
}

// Run starts the WebSocket gateway
func (gw *WebSocketGateway) Run() error {
	// Start hub
	go gw.hub.run()

	// Setup HTTP server
	http.HandleFunc("/ws/workflow", gw.handleWebSocket)

	addr := fmt.Sprintf(":%d", gw.port)
	gw.logger.Infof("WebSocket Gateway starting on %s", addr)

	return http.ListenAndServe(addr, nil)
}

// handleWebSocket handles WebSocket upgrade and client connection
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

	// Start goroutines for reading and writing
	go client.writePump()
	go gw.readPump(client)
}

// readPump pumps messages from the WebSocket connection to the hub
func (gw *WebSocketGateway) readPump(client *Client) {
	defer func() {
		client.hub.unregister <- client
		_ = client.conn.Close()
		gw.logger.Infof("WebSocket client disconnected: %s", client.clientID)
	}()

	client.conn.SetReadLimit(maxMessageSize)
	_ = client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(string) error {
		_ = client.conn.SetReadDeadline(time.Now().Add(pongWait))
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

		// Parse and handle message
		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			gw.logger.Errorf("Failed to parse WebSocket message: %v", err)
			continue
		}

		gw.handleClientMessage(client, msg)
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send the current message
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			// Send any additional queued messages separately
			n := len(c.send)
			for i := 0; i < n; i++ {
				if err := c.conn.WriteMessage(websocket.TextMessage, <-c.send); err != nil {
					return
				}
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// hub.run handles client registration, unregistration and message broadcasting
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

// handleClientMessage routes incoming messages to appropriate handlers
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

// handleDSLCommand processes DSL commands
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
	// Pull optional params/secrets from payload and inject into analyzers
	ps := &dsl.ParamStore{
		Params:  toStringAnyMap(payload["params"]),
		Secrets: toStringStringMap(payload["secrets"]),
	}
	// Safe even if empty; avoids secret leakage in logs
	if gw.orchestratorAnalyzer != nil {
		gw.orchestratorAnalyzer.SetParams(ps)
	}
	if gw.dslAnalyzer != nil {
		// Analyzer has SetParams in our DSL extension
		if setter, ok := interface{}(gw.dslAnalyzer).(interface{ SetParams(*dsl.ParamStore) }); ok {
			setter.SetParams(ps)
		}
	}

	// Use OrchestratorAnalyzer if available for better agent selection and UI feedback
	ctx := context.Background()
	var result interface{}
	var err error

	if gw.orchestratorAnalyzer != nil {
		// Use orchestrator analyzer for complex workflows and agent selection
		gw.logger.Info("Using OrchestratorAnalyzer for DSL command")
		result, err = gw.orchestratorAnalyzer.AnalyzeWithOrchestration(ctx, command)
	} else {
		// Fall back to basic analyzer
		gw.logger.Info("Using basic Analyzer for DSL command")
		// Publish progress event
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

	// Create workflow suggestion based on DSL analysis
	workflowSuggestion := gw.createWorkflowFromDSL(result)

	// Publish result
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

// handleExecuteWorkflow processes workflow execution requests
func (gw *WebSocketGateway) handleExecuteWorkflow(client *Client, payload map[string]interface{}) {
	params := toStringAnyMap(payload["params"])
	secrets := toStringStringMap(payload["secrets"])

	// First check if we have the full workflow object with an ID
	if workflow, ok := payload["workflow"].(map[string]interface{}); ok {
		if workflowID, ok := workflow["id"].(string); ok {
			// This is a stored workflow - execute it
			if gw.orchestratorAnalyzer != nil {
				gw.logger.Infof("▶️ Executing stored workflow: %s", workflowID)

				// Publish workflow start event
				gw.eventBus.Publish(bus.Event{
					Type: bus.EventWorkflowStart,
					Payload: map[string]interface{}{
						"workflowId": workflowID,
					},
				})

				ctx := context.Background()
				go func() {
					gw.orchestratorAnalyzer.SetParams(&dsl.ParamStore{
						Params:  params,
						Secrets: secrets,
					})
					result, err := gw.orchestratorAnalyzer.ExecuteStoredWorkflow(ctx, workflowID)
					if err != nil {
						gw.logger.Errorf("Workflow execution failed: %v", err)
						gw.eventBus.PublishWorkflowError(workflowID, err.Error(), "")
					} else {
						gw.logger.Infof("✅ Workflow %s executed successfully", workflowID)

						// Extract tool execution results for better frontend display
						var message string
						var toolResults []interface{}

						if resultMap, ok := result.(map[string]interface{}); ok {
							if results, ok := resultMap["results"].([]interface{}); ok && len(results) > 0 {
								toolResults = results
								// Extract the first tool result for the message
								if firstResult, ok := results[0].(map[string]interface{}); ok {
									if toolName, ok := firstResult["tool"].(string); ok {
										if toolResult, ok := firstResult["result"].(string); ok {
											// Try to parse tool result as JSON
											var parsedResult map[string]interface{}
											if err := json.Unmarshal([]byte(toolResult), &parsedResult); err == nil {
												if msg, ok := parsedResult["message"].(string); ok {
													message = msg
												}
											} else {
												message = fmt.Sprintf("%s tool executed successfully", toolName)
											}
										} else {
											message = fmt.Sprintf("%s tool executed successfully", toolName)
										}
									}
								}
							}
						}

						if message == "" {
							message = "Workflow execution completed"
						}

						gw.eventBus.Publish(bus.Event{
							Type: bus.EventWorkflowComplete,
							Payload: map[string]interface{}{
								"workflowId":  workflowID,
								"result":      result,
								"message":     message,
								"toolResults": toolResults,
							},
						})
					}
				}()
				return
			}
		}
	}

	// Fallback to old method with nodes and edges
	workflowID, _ := payload["workflowId"].(string)
	if workflowID == "" {
		workflowID = fmt.Sprintf("workflow-%d", time.Now().UnixNano())
	}

	nodes, _ := payload["nodes"].([]interface{})
	edges, _ := payload["edges"].([]interface{})

	if len(nodes) == 0 {
		gw.sendError(client, "No nodes in workflow")
		return
	}

	// Publish workflow start event
	gw.eventBus.Publish(bus.Event{
		Type: bus.EventWorkflowStart,
		Payload: map[string]interface{}{
			"workflowId": workflowID,
		},
	})

	// Execute workflow if orchestrator is available
	if gw.orchestrator != nil {
		ctx := context.Background()
		go func() {
			// Prefer parameterized execution if supported by orchestrator
			if withOpts, ok := gw.orchestrator.(workflowExecWithOpts); ok {
				err := withOpts.ExecuteWorkflowWithOptions(ctx, workflowID, nodes, edges, &workflow.WorkflowOptions{
					Params:  params,
					Secrets: secrets,
				})
				if err != nil {
					gw.eventBus.PublishWorkflowError(workflowID, err.Error(), "")
				}
				return
			}
			// Legacy path (no params support)
			err := gw.orchestrator.ExecuteWorkflow(ctx, workflowID, nodes, edges)
			if err != nil {
				gw.eventBus.PublishWorkflowError(workflowID, err.Error(), "")
			}
		}()
	} else {
		// Simulate workflow execution for testing
		gw.simulateWorkflowExecution(workflowID, nodes, edges)
	}
}

// handleChatMessage processes chat messages
func (gw *WebSocketGateway) handleChatMessage(client *Client, payload map[string]interface{}) {
	content, _ := payload["content"].(string)
	gw.logger.Infof("📨 Received chat message: %s", content)

	// Log orchestrator status
	if gw.orchestratorAnalyzer == nil {
		gw.logger.Warn("⚠️ OrchestratorAnalyzer is nil - cannot process message!")
	} else {
		gw.logger.Info("✅ OrchestratorAnalyzer is available")
	}

	// Echo back as assistant message
	gw.eventBus.Publish(bus.Event{
		Type: bus.EventChatMessage,
		Payload: map[string]interface{}{
			"content": fmt.Sprintf("Processing: %s", content),
			"sender":  "assistant",
		},
	})

	// IMPORTANT: Process ALL messages as DSL commands with orchestrator
	// Every chat message should go through the orchestrator for intelligent processing
	if content != "" {
		if gw.orchestratorAnalyzer != nil {
			go func() {
				ctx := context.Background()
				gw.logger.Infof("🚀 Processing chat message with OrchestratorAnalyzer: %s", content)

				// Use orchestrator analyzer for intelligent processing
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

				// Send success message
				gw.eventBus.Publish(bus.Event{
					Type: bus.EventChatMessage,
					Payload: map[string]interface{}{
						"content": "✅ Command executed successfully",
						"sender":  "system",
					},
				})

				// Log the result
				gw.logger.Infof("✅ DSL command result: %v", result)
			}()
		} else if gw.dslAnalyzer != nil {
			// Fallback to regular DSL analyzer if orchestrator not available
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

				// Send success message
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

// handleEvent handles events from EventBus and broadcasts to clients
func (gw *WebSocketGateway) handleEvent(event bus.Event) {
	// Serialize access to prevent message batching
	gw.broadcastMu.Lock()
	defer gw.broadcastMu.Unlock()

	// Convert event to WebSocket message format
	wsMessage := map[string]interface{}{
		"type":    string(event.Type),
		"payload": event.Payload,
	}

	messageBytes, err := json.Marshal(wsMessage)
	if err != nil {
		gw.logger.Errorf("Failed to marshal event: %v", err)
		return
	}

	// Broadcast to all connected clients
	gw.hub.broadcast <- messageBytes

	// Add small delay to ensure message is processed before next one
	time.Sleep(20 * time.Millisecond)
}

// sendError sends an error message to a specific client
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

// createWorkflowFromDSL creates a workflow suggestion from DSL analysis
func (gw *WebSocketGateway) createWorkflowFromDSL(result interface{}) map[string]interface{} {
	// This is a simplified implementation
	// In reality, this would parse the DSL result and create appropriate nodes/edges

	nodes := []map[string]interface{}{
		{
			"id":   "orchestrator",
			"type": "orchestrator",
			"position": map[string]int{
				"x": 100,
				"y": 100,
			},
			"data": map[string]interface{}{
				"label": "Workflow Orchestrator",
				"type":  "orchestrator",
			},
		},
		{
			"id":   "executor",
			"type": "executor",
			"position": map[string]int{
				"x": 400,
				"y": 100,
			},
			"data": map[string]interface{}{
				"label": "P2P Executor",
				"type":  "executor",
			},
		},
	}

	edges := []map[string]interface{}{
		{
			"id":     "e1",
			"source": "orchestrator",
			"target": "executor",
			"type":   "custom",
		},
	}

	return map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	}
}

// simulateWorkflowExecution simulates workflow execution for testing
func (gw *WebSocketGateway) simulateWorkflowExecution(workflowID string, nodes []interface{}, edges []interface{}) {
	go func() {
		for i, node := range nodes {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				continue
			}

			nodeID, _ := nodeMap["id"].(string)

			// Update status to running
			gw.eventBus.PublishNodeStatusUpdate(workflowID, nodeID, "running")

			// Simulate processing
			time.Sleep(2 * time.Second)

			// Send log
			gw.eventBus.PublishWorkflowLog(
				workflowID,
				"info",
				fmt.Sprintf("Processing node %d of %d", i+1, len(nodes)),
				"simulator",
				nodeID,
			)

			// Update status to success
			gw.eventBus.PublishNodeStatusUpdate(workflowID, nodeID, "success")

			time.Sleep(1 * time.Second)
		}

		// Complete workflow
		gw.eventBus.PublishWorkflowComplete(workflowID, map[string]interface{}{
			"message": "Workflow completed successfully",
		})
	}()
}

func toStringAnyMap(v interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	if v == nil {
		return out
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	// tolerate JSON maps that came in as map[string]any already
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return out
}

func toStringStringMap(v interface{}) map[string]string {
	out := map[string]string{}
	if v == nil {
		return out
	}
	if m, ok := v.(map[string]string); ok {
		return m
	}
	if m, ok := v.(map[string]interface{}); ok {
		for k, vv := range m {
			out[k] = fmt.Sprintf("%v", vv)
		}
	}
	return out
}
