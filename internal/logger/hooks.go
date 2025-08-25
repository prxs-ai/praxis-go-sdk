package logger

import (
	"fmt"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/sirupsen/logrus"
)

// WebSocketLogHook sends log entries to the EventBus for WebSocket clients
type WebSocketLogHook struct {
	eventBus   *bus.EventBus
	agentName  string
	workflowID string
}

// NewWebSocketLogHook creates a new WebSocket log hook
func NewWebSocketLogHook(eventBus *bus.EventBus, agentName string) *WebSocketLogHook {
	return &WebSocketLogHook{
		eventBus:  eventBus,
		agentName: agentName,
	}
}

// SetWorkflowID sets the current workflow ID for log context
func (h *WebSocketLogHook) SetWorkflowID(workflowID string) {
	h.workflowID = workflowID
}

// Levels returns the log levels this hook is interested in
func (h *WebSocketLogHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
		logrus.DebugLevel,
		logrus.TraceLevel,
	}
}

// Fire is called when a log event occurs
func (h *WebSocketLogHook) Fire(entry *logrus.Entry) error {
	// Skip if no EventBus
	if h.eventBus == nil {
		return nil
	}

	// Extract workflow ID from entry fields if available
	workflowID := h.workflowID
	if wfID, ok := entry.Data["workflowId"].(string); ok {
		workflowID = wfID
	}

	// Extract node ID from entry fields if available
	nodeID := ""
	if nID, ok := entry.Data["nodeId"].(string); ok {
		nodeID = nID
	}

	// Format message
	message := entry.Message
	
	// Add field data to message if present
	var fieldParts []string
	for key, value := range entry.Data {
		if key != "workflowId" && key != "nodeId" {
			fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", key, value))
		}
	}
	if len(fieldParts) > 0 {
		message = fmt.Sprintf("%s [%s]", message, strings.Join(fieldParts, ", "))
	}

	// Don't send empty workflow logs
	if workflowID == "" {
		// Check if this is a workflow-related message
		if !strings.Contains(strings.ToLower(message), "workflow") &&
			!strings.Contains(strings.ToLower(message), "node") &&
			!strings.Contains(strings.ToLower(message), "executing") {
			return nil
		}
	}

	// Publish log event
	h.eventBus.PublishAsync(bus.EventWorkflowLog, map[string]interface{}{
		"workflowId": workflowID,
		"level":      entry.Level.String(),
		"message":    message,
		"source":     h.agentName,
		"nodeId":     nodeID,
		"timestamp":  entry.Time.Format(time.RFC3339),
	})

	return nil
}

// ContextualLogger wraps a logger with workflow context
type ContextualLogger struct {
	*logrus.Logger
	workflowID string
	nodeID     string
}

// NewContextualLogger creates a new contextual logger
func NewContextualLogger(logger *logrus.Logger, workflowID, nodeID string) *ContextualLogger {
	return &ContextualLogger{
		Logger:     logger,
		workflowID: workflowID,
		nodeID:     nodeID,
	}
}

// WithWorkflow adds workflow context to log entries
func (l *ContextualLogger) WithWorkflow(workflowID string) *ContextualLogger {
	return &ContextualLogger{
		Logger:     l.Logger,
		workflowID: workflowID,
		nodeID:     l.nodeID,
	}
}

// WithNode adds node context to log entries
func (l *ContextualLogger) WithNode(nodeID string) *ContextualLogger {
	return &ContextualLogger{
		Logger:     l.Logger,
		workflowID: l.workflowID,
		nodeID:     nodeID,
	}
}

// addContext adds workflow and node context to fields
func (l *ContextualLogger) addContext(fields logrus.Fields) logrus.Fields {
	if fields == nil {
		fields = logrus.Fields{}
	}
	if l.workflowID != "" {
		fields["workflowId"] = l.workflowID
	}
	if l.nodeID != "" {
		fields["nodeId"] = l.nodeID
	}
	return fields
}

// Info logs at info level with context
func (l *ContextualLogger) Info(args ...interface{}) {
	l.WithFields(l.addContext(nil)).Info(args...)
}

// Infof logs at info level with format and context
func (l *ContextualLogger) Infof(format string, args ...interface{}) {
	l.WithFields(l.addContext(nil)).Infof(format, args...)
}

// Debug logs at debug level with context
func (l *ContextualLogger) Debug(args ...interface{}) {
	l.WithFields(l.addContext(nil)).Debug(args...)
}

// Debugf logs at debug level with format and context
func (l *ContextualLogger) Debugf(format string, args ...interface{}) {
	l.WithFields(l.addContext(nil)).Debugf(format, args...)
}

// Error logs at error level with context
func (l *ContextualLogger) Error(args ...interface{}) {
	l.WithFields(l.addContext(nil)).Error(args...)
}

// Errorf logs at error level with format and context
func (l *ContextualLogger) Errorf(format string, args ...interface{}) {
	l.WithFields(l.addContext(nil)).Errorf(format, args...)
}

// Warn logs at warn level with context
func (l *ContextualLogger) Warn(args ...interface{}) {
	l.WithFields(l.addContext(nil)).Warn(args...)
}

// Warnf logs at warn level with format and context
func (l *ContextualLogger) Warnf(format string, args ...interface{}) {
	l.WithFields(l.addContext(nil)).Warnf(format, args...)
}