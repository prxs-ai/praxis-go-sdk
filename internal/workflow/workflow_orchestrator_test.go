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

type captureAgent struct {
	lastTool string
	lastArgs map[string]interface{}
}

func (c *captureAgent) HasLocalTool(name string) bool { return true }
func (c *captureAgent) ExecuteLocalTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	c.lastTool = toolName
	c.lastArgs = args
	return map[string]interface{}{"ok": true}, nil
}
func (c *captureAgent) FindAgentWithTool(string) (string, error) { return "", nil }
func (c *captureAgent) ExecuteRemoteTool(context.Context, string, string, map[string]interface{}) (interface{}, error) {
	return nil, nil
}

func TestWorkflow_ParamResolution(t *testing.T) {
	logger := logrus.New()
	eb := bus.NewEventBus(logger)
	an := dsl.NewAnalyzer(logger)
	wo := NewWorkflowOrchestrator(eb, an, logger)

	cap := &captureAgent{}
	wo.SetAgentInterface(cap)

	nodes := []interface{}{
		map[string]interface{}{
			"id":   "n1",
			"type": "tool",
			"data": map[string]interface{}{
				"name": "mytool",
				"args": map[string]interface{}{
					"user":   "{{params.username}}",
					"apiKey": "{{secrets.k}}",
					"note":   "hi",
				},
			},
		},
	}
	edges := []interface{}{}

	opts := &WorkflowOptions{
		Params:  map[string]interface{}{"username": "alice"},
		Secrets: map[string]string{"k": "K123"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := wo.ExecuteWorkflowWithOptions(ctx, "wf-1", nodes, edges, opts)
	assert.NoError(t, err)

	assert.Equal(t, "mytool", cap.lastTool)
	assert.Equal(t, "alice", cap.lastArgs["user"])
	assert.Equal(t, "K123", cap.lastArgs["apiKey"])
	assert.Equal(t, "hi", cap.lastArgs["note"])
}
