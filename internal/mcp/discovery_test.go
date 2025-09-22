package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDiscoveryService_DiscoverToolsFromServer_HTTP(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	httpServer, endpoint := newStreamableHTTPTestServer(t, "Test MCP Server", []mcpTypes.Tool{
		mcpTypes.NewTool("test_tool_1",
			mcpTypes.WithDescription("Test tool 1 description"),
			mcpTypes.WithString("param1",
				mcpTypes.Description("Parameter 1"),
				mcpTypes.Required(),
			),
		),
		mcpTypes.NewTool("test_tool_2",
			mcpTypes.WithDescription("Test tool 2 description"),
			mcpTypes.WithNumber("param2",
				mcpTypes.Description("Parameter 2"),
			),
		),
	})
	defer httpServer.Close()

	service := NewToolDiscoveryService(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := service.DiscoverTools(ctx, DiscoveryParams{
		URL:       endpoint,
		Transport: TransportHTTP,
	})
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	assert.Equal(t, "test_tool_1", tools[0].Name)
	assert.Equal(t, "Test tool 1 description", tools[0].Description)
	assert.Equal(t, "Test MCP Server", tools[0].ServerName)
	assert.Equal(t, endpoint, tools[0].ServerURL)

	assert.Equal(t, "test_tool_2", tools[1].Name)
	assert.Equal(t, "Test tool 2 description", tools[1].Description)
}

func TestToolDiscoveryService_DiscoverToolsFromMultipleServers_HTTP(t *testing.T) {
	logger := logrus.New()
	service := NewToolDiscoveryService(logger)

	server1, endpoint1 := newStreamableHTTPTestServer(t, "Server1", []mcpTypes.Tool{
		mcpTypes.NewTool("tool1", mcpTypes.WithDescription("tool1 description")),
		mcpTypes.NewTool("tool2", mcpTypes.WithDescription("tool2 description")),
	})
	defer server1.Close()

	server2, endpoint2 := newStreamableHTTPTestServer(t, "Server2", []mcpTypes.Tool{
		mcpTypes.NewTool("tool3", mcpTypes.WithDescription("tool3 description")),
		mcpTypes.NewTool("tool4", mcpTypes.WithDescription("tool4 description")),
	})
	defer server2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := service.DiscoverToolsFromMultipleServers(ctx, []string{endpoint1, endpoint2})

	assert.Len(t, results, 2)
	assert.Len(t, results[endpoint1], 2)
	assert.Len(t, results[endpoint2], 2)
}

func newStreamableHTTPTestServer(t *testing.T, name string, tools []mcpTypes.Tool) (*httptest.Server, string) {
	mcpServer := server.NewMCPServer(name, "1.0.0",
		server.WithToolCapabilities(true),
	)

	for _, tool := range tools {
		toolCopy := tool
		mcpServer.AddTool(toolCopy, func(ctx context.Context, request mcpTypes.CallToolRequest) (*mcpTypes.CallToolResult, error) {
			return &mcpTypes.CallToolResult{}, nil
		})
	}

	handler := server.NewStreamableHTTPServer(mcpServer)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	testServer := httptest.NewServer(mux)
	t.Cleanup(testServer.Close)

	return testServer, fmt.Sprintf("%s/mcp", testServer.URL)
}
