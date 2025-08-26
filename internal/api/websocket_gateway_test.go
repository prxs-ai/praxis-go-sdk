package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/praxis/praxis-go-sdk/internal/dsl"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// MockOrchestrator for testing
type MockOrchestrator struct {
	called bool
}

func (m *MockOrchestrator) ExecuteWorkflow(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}) error {
	m.called = true
	return nil
}

// INT-WS-01: Test WebSocketGateway message parsing and routing
func TestWebSocketGateway_MessageRouting(t *testing.T) {
	// Setup
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	eventBus := bus.NewEventBus(logger)
	dslAnalyzer := dsl.NewAnalyzer(logger)

	gateway := NewWebSocketGateway(9001, eventBus, dslAnalyzer, logger)

	// Create mock orchestrator
	mockOrch := new(MockOrchestrator)
	gateway.SetOrchestrator(mockOrch)

	// Start gateway hub
	go gateway.hub.run()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(gateway.handleWebSocket))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/workflow"

	t.Run("DSL_COMMAND routing", func(t *testing.T) {
		// Connect to WebSocket
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err)
		defer ws.Close()

		// Send DSL_COMMAND
		message := map[string]interface{}{
			"type": "DSL_COMMAND",
			"payload": map[string]interface{}{
				"command":    "CALL test_tool arg1",
				"workflowId": "test-workflow-1",
			},
		}

		err = ws.WriteJSON(message)
		assert.NoError(t, err)

		// Read responses - we might get dslProgress and dslResult
		var response map[string]interface{}
		err = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		assert.NoError(t, err)

		err = ws.ReadJSON(&response)
		assert.NoError(t, err)

		// Should receive either dslProgress or dslResult event
		eventType := response["type"].(string)
		assert.True(t, eventType == "dslProgress" || eventType == "dslResult")
		assert.NotNil(t, response["payload"])
	})

	t.Run("EXECUTE_WORKFLOW routing", func(t *testing.T) {
		// Connect to WebSocket
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err)
		defer ws.Close()

		// Reset called flag
		mockOrch.called = false

		// Send EXECUTE_WORKFLOW
		message := map[string]interface{}{
			"type": "EXECUTE_WORKFLOW",
			"payload": map[string]interface{}{
				"workflowId": "test-workflow-2",
				"nodes": []interface{}{
					map[string]interface{}{
						"id":   "node1",
						"type": "test",
						"data": map[string]interface{}{"label": "Test Node"},
					},
				},
				"edges": []interface{}{},
			},
		}

		err = ws.WriteJSON(message)
		assert.NoError(t, err)

		// Read response - might need to read multiple messages to find workflowStart
		var response map[string]interface{}
		var foundWorkflowStart bool

		for i := 0; i < 3; i++ { // Try up to 3 messages
			err = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			assert.NoError(t, err)

			err = ws.ReadJSON(&response)
			if err != nil {
				break
			}

			if response["type"] == "workflowStart" {
				foundWorkflowStart = true
				break
			}
		}

		// Should eventually receive workflowStart event
		assert.True(t, foundWorkflowStart, "Expected to receive workflowStart event")

		// Verify mock was called
		assert.True(t, mockOrch.called)
	})

	t.Run("CHAT_MESSAGE routing", func(t *testing.T) {
		// Connect to WebSocket
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err)
		defer ws.Close()

		// Send CHAT_MESSAGE
		message := map[string]interface{}{
			"type": "CHAT_MESSAGE",
			"payload": map[string]interface{}{
				"content": "Test message",
				"sender":  "user",
			},
		}

		err = ws.WriteJSON(message)
		assert.NoError(t, err)

		// Read response - might need to read multiple messages to find chatMessage
		var response map[string]interface{}
		var foundChatMessage bool

		for i := 0; i < 3; i++ { // Try up to 3 messages
			err = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			assert.NoError(t, err)

			err = ws.ReadJSON(&response)
			if err != nil {
				break
			}

			if response["type"] == "chatMessage" {
				foundChatMessage = true
				break
			}
		}

		// Should eventually receive chatMessage event
		assert.True(t, foundChatMessage, "Expected to receive chatMessage event")
		assert.NotNil(t, response["payload"])

		payload := response["payload"]
		if payloadMap, ok := payload.(map[string]interface{}); ok {
			assert.Contains(t, payloadMap["content"], "Processing")
		}
	})
}

// INT-BUS-01: Test EventBus end-to-end event flow
func TestEventBus_EndToEnd(t *testing.T) {
	logger := logrus.New()
	eventBus := bus.NewEventBus(logger)

	// Channel to receive events
	receivedEvents := make(chan bus.Event, 10)

	// Subscribe to workflow events
	eventBus.Subscribe(bus.EventWorkflowStart, func(event bus.Event) {
		receivedEvents <- event
	})

	eventBus.Subscribe(bus.EventWorkflowComplete, func(event bus.Event) {
		receivedEvents <- event
	})

	t.Run("Event publication and subscription", func(t *testing.T) {
		// Publish workflow start event
		eventBus.Publish(bus.Event{
			Type: bus.EventWorkflowStart,
			Payload: map[string]interface{}{
				"workflowId": "test-123",
				"timestamp":  time.Now(),
			},
		})

		// Should receive the event
		select {
		case event := <-receivedEvents:
			assert.Equal(t, bus.EventWorkflowStart, event.Type)
			payload := event.Payload
			assert.Equal(t, "test-123", payload["workflowId"])
		case <-time.After(2 * time.Second):
			t.Fatal("Did not receive workflow start event")
		}
	})

	t.Run("Async event publication", func(t *testing.T) {
		// Publish multiple events asynchronously
		for i := 0; i < 5; i++ {
			eventBus.PublishAsync(bus.EventWorkflowLog, map[string]interface{}{
				"message": fmt.Sprintf("Log message %d", i),
				"level":   "info",
			})
		}

		// Give time for async processing
		time.Sleep(100 * time.Millisecond)

		// Events should be processed
		// (In real implementation, we'd check the WebSocket output)
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		counter := 0
		mutex := &sync.Mutex{}

		// Add multiple subscribers
		for i := 0; i < 3; i++ {
			eventBus.Subscribe(bus.EventWorkflowError, func(event bus.Event) {
				mutex.Lock()
				counter++
				mutex.Unlock()
			})
		}

		// Publish error event
		eventBus.PublishWorkflowError("test-workflow", "Test error", "node-1")

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		// All subscribers should receive the event
		mutex.Lock()
		assert.Equal(t, 3, counter)
		mutex.Unlock()
	})
}
