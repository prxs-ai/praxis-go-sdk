package bus

import (
	"sync"
	"github.com/sirupsen/logrus"
)

type EventType string

const (
	EventDSLProgress EventType = "dslProgress"
	EventDSLResult   EventType = "dslResult"
	
	EventWorkflowStart    EventType = "workflowStart"
	EventNodeStatusUpdate EventType = "nodeStatusUpdate"
	EventWorkflowLog      EventType = "workflowLog"
	EventWorkflowComplete EventType = "workflowComplete"
	EventWorkflowError    EventType = "workflowError"
	
	EventChatMessage EventType = "chatMessage"
)

type Event struct {
	Type    EventType              `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

type EventHandler func(event Event)

type EventBus struct {
	mu        sync.RWMutex
	handlers  map[EventType][]EventHandler
	logger    *logrus.Logger
	eventChan chan Event
	stopChan  chan struct{}
}

func NewEventBus(logger *logrus.Logger) *EventBus {
	eb := &EventBus{
		handlers:  make(map[EventType][]EventHandler),
		logger:    logger,
		eventChan: make(chan Event, 100),
		stopChan:  make(chan struct{}),
	}
	
	go eb.processEvents()
	
	return eb
}

func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
	eb.logger.Debugf("Handler subscribed to event type: %s", eventType)
}

func (eb *EventBus) SubscribeAll(handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	
	eventTypes := []EventType{
		EventDSLProgress,
		EventDSLResult,
		EventWorkflowStart,
		EventNodeStatusUpdate,
		EventWorkflowLog,
		EventWorkflowComplete,
		EventWorkflowError,
		EventChatMessage,
	}
	
	for _, eventType := range eventTypes {
		eb.handlers[eventType] = append(eb.handlers[eventType], handler)
	}
	
	eb.logger.Debug("Handler subscribed to all event types")
}

func (eb *EventBus) Publish(event Event) {
	select {
	case eb.eventChan <- event:
		eb.logger.Debugf("Event published: %s", event.Type)
	default:
		eb.logger.Warnf("Event channel full, dropping event: %s", event.Type)
	}
}

func (eb *EventBus) PublishAsync(eventType EventType, payload map[string]interface{}) {
	go func() {
		eb.Publish(Event{
			Type:    eventType,
			Payload: payload,
		})
	}()
}

func (eb *EventBus) processEvents() {
	for {
		select {
		case event := <-eb.eventChan:
			eb.handleEvent(event)
		case <-eb.stopChan:
			eb.logger.Info("EventBus stopped")
			return
		}
	}
}

func (eb *EventBus) handleEvent(event Event) {
	eb.mu.RLock()
	handlers := eb.handlers[event.Type]
	eb.mu.RUnlock()
	
	for _, handler := range handlers {
		// Run each handler in a goroutine to prevent blocking
		go func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					eb.logger.Errorf("Panic in event handler for %s: %v", event.Type, r)
				}
			}()
			h(event)
		}(handler)
	}
}

func (eb *EventBus) Stop() {
	close(eb.stopChan)
	close(eb.eventChan)
}


// PublishDSLProgress publishes a DSL progress event
func (eb *EventBus) PublishDSLProgress(stage, message string, details map[string]interface{}) {
	eb.PublishAsync(EventDSLProgress, map[string]interface{}{
		"stage":   stage,
		"message": message,
		"details": details,
	})
}

// PublishNodeStatusUpdate publishes a node status update event
func (eb *EventBus) PublishNodeStatusUpdate(workflowID, nodeID, status string) {
	eb.PublishAsync(EventNodeStatusUpdate, map[string]interface{}{
		"workflowId": workflowID,
		"nodeId":     nodeID,
		"status":     status,
	})
}

// PublishWorkflowLog publishes a workflow log event
func (eb *EventBus) PublishWorkflowLog(workflowID, level, message, source, nodeID string) {
	eb.PublishAsync(EventWorkflowLog, map[string]interface{}{
		"workflowId": workflowID,
		"level":      level,
		"message":    message,
		"source":     source,
		"nodeId":     nodeID,
	})
}

// PublishWorkflowComplete publishes a workflow completion event
func (eb *EventBus) PublishWorkflowComplete(workflowID string, result map[string]interface{}) {
	eb.PublishAsync(EventWorkflowComplete, map[string]interface{}{
		"workflowId": workflowID,
		"status":     "success",
		"result":     result,
	})
}

// PublishWorkflowError publishes a workflow error event
func (eb *EventBus) PublishWorkflowError(workflowID, message, nodeID string) {
	eb.PublishAsync(EventWorkflowError, map[string]interface{}{
		"workflowId": workflowID,
		"message":    message,
		"nodeId":     nodeID,
	})
}