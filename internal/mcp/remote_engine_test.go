// internal/mcp/remote_engine_test.go
package mcp

import (
	"context"
	"testing"

	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoteMCPEngine_ImplementsInterface проверяет, что RemoteMCPEngine реализует ExecutionEngine
func TestRemoteMCPEngine_ImplementsInterface(t *testing.T) {
	tm := NewTransportManager(nil)
	defer tm.Close()
	
	engine := NewRemoteMCPEngine(tm)
	
	// Проверяем, что RemoteMCPEngine реализует интерфейс ExecutionEngine
	var _ contracts.ExecutionEngine = engine
	assert.NotNil(t, engine)
	assert.NotNil(t, engine.transportManager)
}

// TestRemoteMCPEngine_Execute_MissingAddress проверяет обработку отсутствующего адреса
func TestRemoteMCPEngine_Execute_MissingAddress(t *testing.T) {
	tm := NewTransportManager(nil)
	defer tm.Close()
	
	engine := NewRemoteMCPEngine(tm)
	ctx := context.Background()
	
	// Контракт без адреса
	contract := contracts.ToolContract{
		Engine: "remote-mcp",
		Name:   "test_tool",
		EngineSpec: map[string]interface{}{
			// Address отсутствует намеренно
		},
	}
	
	args := map[string]interface{}{
		"tool_name": "test_tool",
		"param1":    "value1",
	}
	
	result, err := engine.Execute(ctx, contract, args)
	
	assert.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "missing 'address'")
}

// TestNewRemoteMCPEngine проверяет создание нового экземпляра движка
func TestNewRemoteMCPEngine(t *testing.T) {
	tm := NewTransportManager(nil)
	defer tm.Close()
	
	engine := NewRemoteMCPEngine(tm)
	
	require.NotNil(t, engine)
	assert.Equal(t, tm, engine.transportManager)
}