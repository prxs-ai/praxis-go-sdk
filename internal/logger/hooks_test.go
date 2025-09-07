package logger

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// INT-LOG-01: Test Logrus Hook integration with EventBus
func TestWebSocketLogHook_EventBusIntegration(t *testing.T) {
	// Setup
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	eventBus := bus.NewEventBus(logger)

	// Track received events
	receivedEvents := make([]bus.Event, 0)
	var mutex sync.Mutex

	// Subscribe to workflow log events
	eventBus.Subscribe(bus.EventWorkflowLog, func(event bus.Event) {
		mutex.Lock()
		receivedEvents = append(receivedEvents, event)
		mutex.Unlock()
	})

	// Create and add the hook
	hook := NewWebSocketLogHook(eventBus, "test-agent")
	logger.AddHook(hook)

	t.Run("Log message triggers EventBus event", func(t *testing.T) {
		// Clear received events
		mutex.Lock()
		receivedEvents = receivedEvents[:0]
		mutex.Unlock()

		// Write a workflow-related log message
		logger.Info("Workflow execution starting")

		// Give time for async processing
		time.Sleep(100 * time.Millisecond)

		// Check that event was published
		mutex.Lock()
		defer mutex.Unlock()

		assert.Len(t, receivedEvents, 1)
		if len(receivedEvents) > 0 {
			event := receivedEvents[0]
			assert.Equal(t, bus.EventWorkflowLog, event.Type)

			payload := event.Payload
			assert.Equal(t, "info", payload["level"])
			assert.Equal(t, "Workflow execution starting", payload["message"])
			assert.Equal(t, "test-agent", payload["source"])
		}
	})

	t.Run("Log with workflow context", func(t *testing.T) {
		// Clear received events
		mutex.Lock()
		receivedEvents = receivedEvents[:0]
		mutex.Unlock()

		// Set workflow ID
		hook.SetWorkflowID("workflow-123")

		// Log with workflow context
		logger.WithFields(logrus.Fields{
			"nodeId": "node-456",
		}).Info("Node execution started")

		// Give time for async processing
		time.Sleep(100 * time.Millisecond)

		// Check event contains workflow and node IDs
		mutex.Lock()
		defer mutex.Unlock()

		assert.Len(t, receivedEvents, 1)
		if len(receivedEvents) > 0 {
			payload := receivedEvents[0].Payload
			assert.Equal(t, "workflow-123", payload["workflowId"])
			assert.Equal(t, "node-456", payload["nodeId"])
			assert.Contains(t, payload["message"], "Node execution started")
		}
	})

	t.Run("Different log levels", func(t *testing.T) {
		// Clear received events
		mutex.Lock()
		receivedEvents = receivedEvents[:0]
		mutex.Unlock()

		// Log at different levels
		logger.Debug("Debug message")
		logger.Info("Info message")
		logger.Warn("Warning message")
		logger.Error("Error message")

		// Give time for async processing
		time.Sleep(200 * time.Millisecond)

		// Check all levels are captured
		mutex.Lock()
		defer mutex.Unlock()

		assert.Len(t, receivedEvents, 4)

		levels := make(map[string]bool)
		for _, event := range receivedEvents {
			payload := event.Payload
			levels[payload["level"].(string)] = true
		}

		assert.True(t, levels["debug"])
		assert.True(t, levels["info"])
		assert.True(t, levels["warning"])
		assert.True(t, levels["error"])
	})
}

// Test ContextualLogger
func TestContextualLogger(t *testing.T) {
	baseLogger := logrus.New()
	baseLogger.SetLevel(logrus.DebugLevel)

	// Create output buffer to capture logs
	output := &strings.Builder{}
	baseLogger.SetOutput(output)
	baseLogger.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
		DisableColors:    true,
	})

	t.Run("Context is added to log entries", func(t *testing.T) {
		output.Reset()

		contextLogger := NewContextualLogger(baseLogger, "workflow-789", "node-123")

		contextLogger.Info("Test message with context")

		logOutput := output.String()
		assert.Contains(t, logOutput, "workflowId=workflow-789")
		assert.Contains(t, logOutput, "nodeId=node-123")
		assert.Contains(t, logOutput, "Test message with context")
	})

	t.Run("WithWorkflow creates new context", func(t *testing.T) {
		output.Reset()

		contextLogger := NewContextualLogger(baseLogger, "", "")
		newLogger := contextLogger.WithWorkflow("new-workflow")

		newLogger.Info("Message with new workflow")

		logOutput := output.String()
		assert.Contains(t, logOutput, "workflowId=new-workflow")
	})

	t.Run("WithNode creates new context", func(t *testing.T) {
		output.Reset()

		contextLogger := NewContextualLogger(baseLogger, "workflow-1", "")
		newLogger := contextLogger.WithNode("new-node")

		newLogger.Info("Message with new node")

		logOutput := output.String()
		assert.Contains(t, logOutput, "workflowId=workflow-1")
		assert.Contains(t, logOutput, "nodeId=new-node")
	})
}
