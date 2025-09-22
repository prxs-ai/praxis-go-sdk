// internal/mcp/remote_engine.go
package mcp

import (
	"context"
	"fmt"
	"strings"

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
	transport := strings.ToLower(strings.TrimSpace(getStringFromSpec(spec, "transport")))
	if transport == "" {
		transport = "http"
	}
	headers := getHeadersFromSpec(spec)

	switch transport {
	case "sse":
		e.transportManager.RegisterSSEEndpoint(clientName, address, headers)
	case "http", "streamable_http", "streamable-http":
		e.transportManager.RegisterHTTPEndpoint(clientName, address, headers)
	default:
		e.transportManager.RegisterHTTPEndpoint(clientName, address, headers)
	}

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

func getStringFromSpec(spec map[string]interface{}, key string) string {
	if raw, ok := spec[key]; ok {
		if str, ok := raw.(string); ok {
			return str
		}
	}
	return ""
}

func getHeadersFromSpec(spec map[string]interface{}) map[string]string {
	raw, ok := spec["headers"]
	if !ok || raw == nil {
		return nil
	}

	headers := make(map[string]string)
	switch typed := raw.(type) {
	case map[string]string:
		for k, v := range typed {
			headers[k] = v
		}
	case map[string]interface{}:
		for k, v := range typed {
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
	}

	if len(headers) == 0 {
		return nil
	}
	return headers
}
