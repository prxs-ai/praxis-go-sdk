package a2a

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/sirupsen/logrus"
)

// TaskManager manages the lifecycle of A2A tasks
type TaskManager struct {
	tasks    map[string]*Task
	mu       sync.RWMutex
	eventBus *bus.EventBus
	logger   *logrus.Logger
}

// NewTaskManager creates a new task manager
func NewTaskManager(eb *bus.EventBus, logger *logrus.Logger) *TaskManager {
	if logger == nil {
		logger = logrus.New()
	}
	
	tm := &TaskManager{
		tasks:    make(map[string]*Task),
		eventBus: eb,
		logger:   logger,
	}
	
	logger.Info("A2A TaskManager initialized successfully")
	
	return tm
}

// CreateTask creates a new task from a message
func (tm *TaskManager) CreateTask(msg Message) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	taskID := uuid.New().String()
	contextID := msg.ContextID
	if contextID == "" {
		contextID = uuid.New().String()
	}

	// Update message with task and context IDs
	msg.TaskID = taskID
	msg.ContextID = contextID

	task := &Task{
		ID:        taskID,
		ContextID: contextID,
		Status: TaskStatus{
			State:     "submitted",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		History:   []Message{msg},
		Artifacts: []Artifact{},
		Kind:      "task",
	}

	tm.tasks[taskID] = task
	tm.logger.Infof("[TaskID: %s] Task created in 'submitted' state", taskID)

	// Publish task creation event
	if tm.eventBus != nil {
		tm.eventBus.Publish(bus.Event{
			Type: bus.EventTaskCreated,
			Payload: map[string]interface{}{
				"taskId": taskID,
				"task":   task,
			},
		})
	}

	return task
}

// GetTask retrieves a task by ID
func (tm *TaskManager) GetTask(id string) (*Task, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	
	task, exists := tm.tasks[id]
	return task, exists
}

// UpdateTaskStatus updates the status of a task
func (tm *TaskManager) UpdateTaskStatus(id, state string, agentMessage *Message) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task, exists := tm.tasks[id]
	if !exists {
		tm.logger.Warnf("[TaskID: %s] Attempted to update non-existent task", id)
		return
	}

	oldState := task.Status.State
	task.Status.State = state
	task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)

	if agentMessage != nil {
		task.Status.Message = agentMessage
		task.History = append(task.History, *agentMessage)
	}

	tm.logger.Infof("[TaskID: %s] Status updated from '%s' to '%s'", id, oldState, state)

	// Publish status update event
	if tm.eventBus != nil {
		tm.eventBus.Publish(bus.Event{
			Type: bus.EventTaskStatusUpdate,
			Payload: map[string]interface{}{
				"taskId":   id,
				"oldState": oldState,
				"newState": state,
				"status":   task.Status,
			},
		})
	}
}

// AddArtifactToTask adds an artifact to a task
func (tm *TaskManager) AddArtifactToTask(id string, artifact Artifact) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task, exists := tm.tasks[id]
	if !exists {
		tm.logger.Warnf("[TaskID: %s] Attempted to add artifact to non-existent task", id)
		return
	}

	task.Artifacts = append(task.Artifacts, artifact)
	tm.logger.Infof("[TaskID: %s] Artifact '%s' added", id, artifact.Name)

	// Publish artifact creation event
	if tm.eventBus != nil {
		tm.eventBus.Publish(bus.Event{
			Type: bus.EventArtifactAdded,
			Payload: map[string]interface{}{
				"taskId":   id,
				"artifact": artifact,
			},
		})
	}
}

// AddMessageToHistory adds a message to task history
func (tm *TaskManager) AddMessageToHistory(id string, message Message) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task, exists := tm.tasks[id]
	if !exists {
		tm.logger.Warnf("[TaskID: %s] Attempted to add message to non-existent task", id)
		return
	}

	// Ensure message has correct task and context IDs
	message.TaskID = id
	message.ContextID = task.ContextID

	task.History = append(task.History, message)
	tm.logger.Debugf("[TaskID: %s] Message '%s' added to history", id, message.MessageID)
}

// ListTasks returns all tasks (for debugging/monitoring)
func (tm *TaskManager) ListTasks() map[string]*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make(map[string]*Task)
	for id, task := range tm.tasks {
		tasks[id] = task
	}
	return tasks
}

// GetTasksByState returns tasks in a specific state
func (tm *TaskManager) GetTasksByState(state string) []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var tasks []*Task
	for _, task := range tm.tasks {
		if task.Status.State == state {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// CleanupCompletedTasks removes completed tasks older than the specified duration
func (tm *TaskManager) CleanupCompletedTasks(olderThan time.Duration) int {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	cleaned := 0

	for id, task := range tm.tasks {
		if task.Status.State == "completed" || task.Status.State == "failed" || task.Status.State == "canceled" || task.Status.State == "rejected" {
			if timestamp, err := time.Parse(time.RFC3339, task.Status.Timestamp); err == nil {
				if timestamp.Before(cutoff) {
					delete(tm.tasks, id)
					cleaned++
				}
			}
		}
	}

	if cleaned > 0 {
		tm.logger.Infof("Cleaned up %d completed tasks older than %v", cleaned, olderThan)
	}

	return cleaned
}

// CancelTask cancels a task if it's in a cancelable state
func (tm *TaskManager) CancelTask(id string) (*Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if task exists
	task, exists := tm.tasks[id]
	if !exists {
		return nil, ErrTaskNotFound
	}

	// Check if task is in a cancelable state
	// Non-cancelable states: completed, failed, canceled, rejected
	nonCancelableStates := map[string]bool{
		"completed": true,
		"failed":    true,
		"canceled":  true,
		"rejected":  true,
	}

	if nonCancelableStates[task.Status.State] {
		tm.logger.Warnf("[TaskID: %s] Attempted to cancel task in non-cancelable state: %s", id, task.Status.State)
		return nil, ErrTaskNotCancelable
	}

	// Update task status to canceled
	oldState := task.Status.State
	task.Status.State = "canceled"
	task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)

	tm.logger.Infof("[TaskID: %s] Status updated from '%s' to 'canceled'", id, oldState)

	// Publish task cancellation event
	if tm.eventBus != nil {
		tm.eventBus.Publish(bus.Event{
			Type: bus.EventTaskStatusUpdate,
			Payload: map[string]interface{}{
				"taskId":   id,
				"oldState": oldState,
				"newState": "canceled",
				"status":   task.Status,
			},
		})
	}

	return task, nil
}

// GetTaskCount returns the number of tasks by state
func (tm *TaskManager) GetTaskCount() map[string]int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	counts := map[string]int{
		"submitted":      0,
		"working":        0,
		"completed":      0,
		"failed":         0,
		"input-required": 0,
		"canceled":       0,
		"rejected":       0,
		"auth-required":  0,
	}

	for _, task := range tm.tasks {
		if count, exists := counts[task.Status.State]; exists {
			counts[task.Status.State] = count + 1
		} else {
			counts["unknown"] = counts["unknown"] + 1
		}
	}

	return counts
}