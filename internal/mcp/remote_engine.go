// internal/mcp/remote_engine.go
package mcp

import (
	"context"
	"fmt"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/praxis/praxis-go-sdk/internal/contracts"
)

// RemoteMCPEngine реализует ExecutionEngine для проксирования вызовов
// на внешние MCP-серверы по SSE.
type RemoteMCPEngine struct {
	transportManager *TransportManager
}

// NewRemoteMCPEngine создает новый экземпляр движка.
func NewRemoteMCPEngine(tm *TransportManager) *RemoteMCPEngine {
	return &RemoteMCPEngine{
		transportManager: tm,
	}
}

// Execute выполняет контракт удаленного инструмента.
func (e *RemoteMCPEngine) Execute(ctx context.Context, contract contracts.ToolContract, args map[string]interface{}) (string, error) {
	spec := contract.EngineSpec
	address, ok := spec["address"].(string)
	if !ok || address == "" {
		return "", fmt.Errorf("remote-mcp spec missing 'address' for tool '%s'", contract.Name)
	}

	// Регистрируем эндпоинт в TransportManager, если его еще нет.
	clientName := address
	e.transportManager.RegisterSSEEndpoint(clientName, address, nil)

	// Используем имя инструмента из контракта
	toolName := contract.Name

	result, err := e.transportManager.CallRemoteTool(ctx, clientName, toolName, args)
	if err != nil {
		return "", fmt.Errorf("failed to call remote tool '%s' at '%s': %w", toolName, address, err)
	}

	// Извлекаем текстовый результат из ответа MCP.
	if result != nil && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(*mcpTypes.TextContent); ok {
			return textContent.Text, nil
		}
	}

	return fmt.Sprintf("Tool '%s' executed successfully with no text output.", toolName), nil
}
