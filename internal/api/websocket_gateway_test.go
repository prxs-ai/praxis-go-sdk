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
	"github.com/praxis/praxis-go-sdk/internal/workflow"
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

// MockOrchestratorWithOpts captures WorkflowOptions when available
type MockOrchestratorWithOpts struct {
	calledLegacy   bool
	calledWithOpts bool
	lastOpts       *workflow.WorkflowOptions
	calledCh       chan struct{}
}

func (m *MockOrchestratorWithOpts) ExecuteWorkflow(ctx context.Context, id string, nodes, edges []interface{}) error {
	m.calledLegacy = true
	return nil
}

func (m *MockOrchestratorWithOpts) ExecuteWorkflowWithOptions(ctx context.Context, id string, nodes, edges []interface{}, opts *workflow.WorkflowOptions) error {
	m.calledWithOpts = true
	m.lastOpts = opts
	if m.calledCh != nil {
		select {
		case m.calledCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// wsReadUntilType reads messages until we see the given event type or timeout
func wsReadUntilType(ws *websocket.Conn, want string, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			// keep looping until deadline
			continue
		}
		if typ, _ := msg["type"].(string); typ == want {
			return msg, nil
		}
	}
	return nil, fmt.Errorf("did not receive %s before timeout", want)
}

// wsReadUntilAnyType reads until one of the desired event types appears (order-agnostic)
func wsReadUntilAnyType(ws *websocket.Conn, wants []string, timeout time.Duration) (map[string]interface{}, error) {
	wantSet := make(map[string]struct{}, len(wants))
	for _, w := range wants {
		wantSet[w] = struct{}{}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			continue
		}
		if typ, _ := msg["type"].(string); typ != "" {
			if _, ok := wantSet[typ]; ok {
				return msg, nil
			}
		}
	}
	return nil, fmt.Errorf("did not receive any of %v before timeout", wants)
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
		assert.NoError(t, ws.WriteJSON(message))

		// Accept either dslResult or dslProgress; ignore workflowLog etc.
		resp, err := wsReadUntilAnyType(ws, []string{"dslResult", "dslProgress"}, 5*time.Second)
		assert.NoError(t, err, "expected dslResult or dslProgress event")
		assert.NotNil(t, resp["payload"])
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

		assert.NoError(t, ws.WriteJSON(message))

		// Read response - might need to read multiple messages to find workflowStart
		var response map[string]interface{}
		var foundWorkflowStart bool

		for i := 0; i < 3; i++ { // Try up to 3 messages
			_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))

			if err := ws.ReadJSON(&response); err != nil {
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
		assert.NoError(t, ws.WriteJSON(message))

		// Read response - might need to read multiple messages to find chatMessage
		var response map[string]interface{}
		var foundChatMessage bool

		for i := 0; i < 3; i++ { // Try up to 3 messages
			_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))

			if err := ws.ReadJSON(&response); err != nil {
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

		// Payload.content can be the intermediate log ("Processing ...")
		// OR the final structured result ("✅ Result: ... status:completed ...")
		if payloadMap, ok := response["payload"].(map[string]interface{}); ok {
			content := fmt.Sprint(payloadMap["content"]) // safe stringify
			okProcessing := strings.Contains(content, "Processing")
			okFinal := strings.Contains(content, "status:completed") &&
				strings.Contains(content, "type:command") &&
				strings.Contains(content, "value:Test")

			assert.True(
				t,
				okProcessing || okFinal,
				"expected either an in-flight 'Processing' log or a final result with 'status:completed', 'type:command', and 'value:Test'; got: %q",
				content,
			)
		}
	})
}

// INT-WS-02: Ensure ExecuteWorkflow uses options when supported and passes params/secrets
func TestWebSocketGateway_ExecuteWorkflow_PassesOptions(t *testing.T) {
	logger := logrus.New()
	eventBus := bus.NewEventBus(logger)
	dslAnalyzer := dsl.NewAnalyzer(logger)
	gateway := NewWebSocketGateway(9001, eventBus, dslAnalyzer, logger)

	mockOrch := &MockOrchestratorWithOpts{calledCh: make(chan struct{}, 1)}
	gateway.SetOrchestrator(mockOrch)
	go gateway.hub.run()

	server := httptest.NewServer(http.HandlerFunc(gateway.handleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/workflow"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.NoError(t, err)
	defer ws.Close()

	msg := map[string]interface{}{
		"type": "EXECUTE_WORKFLOW",
		"payload": map[string]interface{}{
			"workflowId": "wf-opts-1",
			"nodes": []interface{}{
				map[string]interface{}{
					"id":   "n1",
					"type": "tool",
					"data": map[string]interface{}{
						"label": "Tool",
						"type":  "tool",
						"args": map[string]interface{}{
							"username": "{{params.username}}",
							"token":    "{{secrets.apify_key}}",
						},
					},
				},
			},
			"edges":   []interface{}{},
			"params":  map[string]interface{}{"username": "elonmusk"},
			"secrets": map[string]interface{}{"apify_key": "SECRET123"},
		},
	}
	assert.NoError(t, ws.WriteJSON(msg))

	// Expect workflowStart event
	_, err = wsReadUntilType(ws, "workflowStart", 3*time.Second)
	assert.NoError(t, err)

	// Wait until orchestrator receives options
	select {
	case <-mockOrch.calledCh:
	case <-time.After(2 * time.Second):
		t.Fatal("orchestrator was not called with options")
	}
	assert.True(t, mockOrch.calledWithOpts, "ExecuteWorkflowWithOptions should be used")
	if assert.NotNil(t, mockOrch.lastOpts) {
		assert.Equal(t, "elonmusk", mockOrch.lastOpts.Params["username"])
		assert.Equal(t, "SECRET123", mockOrch.lastOpts.Secrets["apify_key"])
	}
}

// INT-WS-03: Ensure DSL_COMMAND interpolates params into analyzer execution
func TestWebSocketGateway_DSLCommand_ParamsInterpolation(t *testing.T) {
	logger := logrus.New()
	eventBus := bus.NewEventBus(logger)
	dslAnalyzer := dsl.NewAnalyzer(logger)
	gateway := NewWebSocketGateway(9001, eventBus, dslAnalyzer, logger)
	go gateway.hub.run()

	server := httptest.NewServer(http.HandlerFunc(gateway.handleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/workflow"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.NoError(t, err)
	defer ws.Close()

	// This uses analyzer's CALL path; with our analyzer extensions, args are resolved.
	msg := map[string]interface{}{
		"type": "DSL_COMMAND",
		"payload": map[string]interface{}{
			"command": `CALL write_file --filename "out.txt" --content "Hello {{params.name}}"`,
			"params":  map[string]interface{}{"name": "Praxis"},
		},
	}
	assert.NoError(t, ws.WriteJSON(msg))

	// Read until dslResult
	resp, err := wsReadUntilType(ws, "dslResult", 5*time.Second)
	assert.NoError(t, err)
	pl := resp["payload"].(map[string]interface{})
	res := pl["result"].(map[string]interface{})
	results := res["results"].([]interface{})
	assert.NotEmpty(t, results)
	first := results[0].(map[string]interface{})
	args := first["args"].(map[string]interface{})
	assert.Equal(t, "out.txt", args["filename"])
	assert.Equal(t, "Hello Praxis", args["content"]) // <- interpolation worked
}

// INT-WS-04: Ensure DSL_COMMAND supports {{env.*}} interpolation
func TestWebSocketGateway_DSLCommand_EnvInterpolation(t *testing.T) {
	t.Setenv("PRACTICE_VAR", "FromEnv")

	logger := logrus.New()
	eventBus := bus.NewEventBus(logger)
	dslAnalyzer := dsl.NewAnalyzer(logger)
	gateway := NewWebSocketGateway(9001, eventBus, dslAnalyzer, logger)
	go gateway.hub.run()

	server := httptest.NewServer(http.HandlerFunc(gateway.handleWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/workflow"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.NoError(t, err)
	defer ws.Close()

	msg := map[string]interface{}{
		"type": "DSL_COMMAND",
		"payload": map[string]interface{}{
			"command": `CALL write_file --filename "out.txt" --content "Env={{env.PRACTICE_VAR}}"`,
		},
	}
	assert.NoError(t, ws.WriteJSON(msg))

	resp, err := wsReadUntilType(ws, "dslResult", 5*time.Second)
	assert.NoError(t, err)

	pl := resp["payload"].(map[string]interface{})
	res := pl["result"].(map[string]interface{})
	results := res["results"].([]interface{})
	args := results[0].(map[string]interface{})["args"].(map[string]interface{})

	assert.Equal(t, "Env=FromEnv", args["content"])
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
