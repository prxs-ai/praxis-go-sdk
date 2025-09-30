package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/bus"
	"github.com/praxis/praxis-go-sdk/internal/dsl"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// newTestOrchestrator returns a WorkflowOrchestrator with a test logger and event bus
func newTestOrchestrator() (*WorkflowOrchestrator, *bus.EventBus) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	eventBus := bus.NewEventBus(logger) // ✅ pass logger here
	dslAnalyzer := &dsl.Analyzer{}      // dummy
	wo := NewWorkflowOrchestrator(eventBus, dslAnalyzer, logger)
	return wo, eventBus
}

func TestExecuteWorkflow_SimpleFlow(t *testing.T) {
	wo, _ := newTestOrchestrator()

	nodes := []interface{}{
		map[string]interface{}{
			"id":   "n1",
			"type": "tool",
			"data": map[string]interface{}{
				"name": "echo_tool",
				"args": map[string]interface{}{"msg": "hello"},
			},
		},
	}
	edges := []interface{}{}

	err := wo.ExecuteWorkflow(context.Background(), "wf1", nodes, edges)
	assert.NoError(t, err)

	status, err := wo.GetWorkflowStatus("wf1")
	assert.NoError(t, err)
	assert.Equal(t, "completed", status["status"])
	assert.Contains(t, status["nodeStatuses"], "n1")
	assert.Equal(t, "success", status["nodeStatuses"].(map[string]string)["n1"])
}

func TestExecuteWorkflowWithOptions_ParamsAndSecrets(t *testing.T) {
	wo, _ := newTestOrchestrator()

	nodes := []interface{}{
		map[string]interface{}{
			"id":   "n1",
			"type": "tool",
			"data": map[string]interface{}{
				"name": "telegram_poster",
				"args": map[string]interface{}{},
			},
		},
	}
	edges := []interface{}{}

	opts := &dsl.WorkflowOptions{
		Params: map[string]interface{}{
			"message": "Hello World",
			"channel": "@mychannel",
		},
		Secrets: map[string]string{
			"TELEGRAM_BOT_TOKEN": "super-secret",
		},
	}

	err := wo.ExecuteWorkflowWithOptions(context.Background(), "wf2", nodes, edges, opts)
	assert.NoError(t, err)

	status, err := wo.GetWorkflowStatus("wf2")
	assert.NoError(t, err)
	assert.Equal(t, "completed", status["status"])
}

func TestExecuteWorkflow_NoEntryNodes(t *testing.T) {
	wo, _ := newTestOrchestrator()

	// Node references itself → cycle, but orchestrator will auto-select it as entry
	nodes := []interface{}{
		map[string]interface{}{
			"id":   "n1",
			"type": "tool",
			"data": map[string]interface{}{"name": "cycle"},
		},
	}
	edges := []interface{}{
		map[string]interface{}{
			"id":     "e1",
			"source": "n1",
			"target": "n1",
			"type":   "custom",
		},
	}

	err := wo.ExecuteWorkflow(context.Background(), "wf3", nodes, edges)
	assert.NoError(t, err, "orchestrator should auto-select entry and complete workflow")

	status, err := wo.GetWorkflowStatus("wf3")
	assert.NoError(t, err)
	assert.Equal(t, "completed", status["status"])
	assert.Contains(t, status["nodeStatuses"], "n1")
}

func TestNodeStatusTransitions(t *testing.T) {
	wo, _ := newTestOrchestrator()

	nodes := []interface{}{
		map[string]interface{}{
			"id":   "start",
			"type": "tool",
			"data": map[string]interface{}{"name": "echo"},
		},
		map[string]interface{}{
			"id":   "next",
			"type": "tool",
			"data": map[string]interface{}{"name": "echo2"},
		},
	}
	edges := []interface{}{
		map[string]interface{}{
			"id":     "e1",
			"source": "start",
			"target": "next",
			"type":   "custom",
		},
	}

	err := wo.ExecuteWorkflow(context.Background(), "wf4", nodes, edges)
	assert.NoError(t, err)

	status, err := wo.GetWorkflowStatus("wf4")
	assert.NoError(t, err)

	nodeStatuses := status["nodeStatuses"].(map[string]string)
	assert.Equal(t, "success", nodeStatuses["start"])
	assert.Equal(t, "success", nodeStatuses["next"])
	assert.Equal(t, "completed", status["status"])
}

func TestMaskSecrets(t *testing.T) {
	secrets := map[string]string{
		"KEY1": "value1",
		"KEY2": "value2",
	}
	masked := maskSecrets(secrets)
	assert.Equal(t, "***", masked["KEY1"])
	assert.Equal(t, "***", masked["KEY2"])
	assert.Len(t, masked, 2)
}

func TestExecuteWorkflowWithOptions_DelegatesToExecuteWorkflow(t *testing.T) {
	wo, _ := newTestOrchestrator()

	nodes := []interface{}{
		map[string]interface{}{
			"id":   "n1",
			"type": "tool",
			"data": map[string]interface{}{"name": "noop"},
		},
	}
	edges := []interface{}{}

	opts := &dsl.WorkflowOptions{Params: map[string]interface{}{"foo": "bar"}}

	start := time.Now()
	err := wo.ExecuteWorkflowWithOptions(context.Background(), "wf5", nodes, edges, opts)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.Less(t, duration.Seconds(), 5.0, "workflow should finish quickly")
}
