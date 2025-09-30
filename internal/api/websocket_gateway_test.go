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

type MockOrchestrator struct {
	called      bool
	lastParams  map[string]interface{}
	lastSecrets map[string]string
}

func (m *MockOrchestrator) ExecuteWorkflow(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}) error {
	m.called = true
	return nil
}

func (m *MockOrchestrator) ExecuteWorkflowWithOptions(ctx context.Context, workflowID string, nodes []interface{}, edges []interface{}, opts *dsl.WorkflowOptions) error {
	m.called = true
	m.lastParams = opts.Params
	m.lastSecrets = opts.Secrets
	return nil
}

func TestWebSocketGateway_MessageRouting(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	eventBus := bus.NewEventBus(logger)
	dslAnalyzer := dsl.NewAnalyzer(logger)

	gateway := NewWebSocketGateway(9001, eventBus, dslAnalyzer, logger)

	mockOrch := new(MockOrchestrator)
	gateway.SetOrchestrator(mockOrch)

	go gateway.hub.run()

	server := httptest.NewServer(http.HandlerFunc(gateway.handleWebSocket))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/workflow"

	t.Run("DSL_COMMAND routing", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err)
		defer ws.Close()

		message := map[string]interface{}{
			"type": "DSL_COMMAND",
			"payload": map[string]interface{}{
				"command":    "CALL test_tool arg1",
				"workflowId": "test-workflow-1",
			},
		}
		err = ws.WriteJSON(message)
		assert.NoError(t, err)

		var response map[string]interface{}
		err = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		assert.NoError(t, err)
		err = ws.ReadJSON(&response)
		assert.NoError(t, err)

		eventType := response["type"].(string)
		assert.True(t, eventType == "dslProgress" || eventType == "dslResult")
		assert.NotNil(t, response["payload"])
	})

	t.Run("EXECUTE_WORKFLOW routing with params/secrets", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err)
		defer ws.Close()

		mockOrch.called = false

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
				"edges":   []interface{}{},
				"params":  map[string]interface{}{"message": "hello"},
				"secrets": map[string]interface{}{"token": "secret-token"},
			},
		}

		err = ws.WriteJSON(message)
		assert.NoError(t, err)

		var response map[string]interface{}
		var foundWorkflowStart bool
		for i := 0; i < 3; i++ {
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

		assert.True(t, foundWorkflowStart, "Expected workflowStart")
		assert.True(t, mockOrch.called)
		assert.Equal(t, "hello", mockOrch.lastParams["message"])
		assert.Equal(t, "secret-token", mockOrch.lastSecrets["token"])
	})

	t.Run("CHAT_MESSAGE routing", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		assert.NoError(t, err)
		defer ws.Close()

		message := map[string]interface{}{
			"type": "CHAT_MESSAGE",
			"payload": map[string]interface{}{
				"content": "Test message",
				"sender":  "user",
			},
		}
		err = ws.WriteJSON(message)
		assert.NoError(t, err)

		var response map[string]interface{}
		var foundChatMessage bool
		for i := 0; i < 3; i++ {
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
		assert.True(t, foundChatMessage)
		assert.NotNil(t, response["payload"])
		if payloadMap, ok := response["payload"].(map[string]interface{}); ok {
			assert.Contains(t, payloadMap["content"], "Processing")
		}
	})
}

func TestEventBus_EndToEnd(t *testing.T) {
	logger := logrus.New()
	eventBus := bus.NewEventBus(logger)

	receivedEvents := make(chan bus.Event, 10)

	eventBus.Subscribe(bus.EventWorkflowStart, func(event bus.Event) {
		receivedEvents <- event
	})
	eventBus.Subscribe(bus.EventWorkflowComplete, func(event bus.Event) {
		receivedEvents <- event
	})

	t.Run("Event publication and subscription", func(t *testing.T) {
		eventBus.Publish(bus.Event{
			Type: bus.EventWorkflowStart,
			Payload: map[string]interface{}{
				"workflowId": "test-123",
				"timestamp":  time.Now(),
			},
		})
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
		for i := 0; i < 5; i++ {
			eventBus.PublishAsync(bus.EventWorkflowLog, map[string]interface{}{
				"message": fmt.Sprintf("Log message %d", i),
				"level":   "info",
			})
		}
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("Multiple subscribers", func(t *testing.T) {
		counter := 0
		mutex := &sync.Mutex{}
		for i := 0; i < 3; i++ {
			eventBus.Subscribe(bus.EventWorkflowError, func(event bus.Event) {
				mutex.Lock()
				counter++
				mutex.Unlock()
			})
		}
		eventBus.PublishWorkflowError("test-workflow", "Test error", "node-1")
		time.Sleep(100 * time.Millisecond)
		mutex.Lock()
		assert.Equal(t, 3, counter)
		mutex.Unlock()
	})
}
