package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	clientTransport "github.com/mark3labs/mcp-go/client/transport"
	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
)

// ToolDiscoveryService handles dynamic discovery of MCP tools
type ToolDiscoveryService struct {
	logger *logrus.Logger
}

// NewToolDiscoveryService creates a new discovery service
func NewToolDiscoveryService(logger *logrus.Logger) *ToolDiscoveryService {
	return &ToolDiscoveryService{
		logger: logger,
	}
}

// DiscoveredTool represents a discovered MCP tool
type DiscoveredTool struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	InputSchema mcpTypes.ToolInputSchema `json:"input_schema"`
	ServerURL   string                   `json:"server_url"`
	ServerName  string                   `json:"server_name"`
}

// DiscoveryParams defines connection parameters for probing an MCP server.
type DiscoveryParams struct {
	URL       string
	Headers   map[string]string
	Transport TransportType
}

// DiscoverTools connects to an MCP server using the specified transport and
// headers, listing available tools.
func (s *ToolDiscoveryService) DiscoverTools(ctx context.Context, params DiscoveryParams) ([]DiscoveredTool, error) {
	if params.URL == "" {
		return nil, fmt.Errorf("server URL cannot be empty")
	}

	transportType := params.Transport
	if transportType == "" {
		transportType = TransportHTTP
	}

	s.logger.Infof("🔍 Discovering tools from MCP server: %s (transport=%s)", params.URL, strings.ToUpper(string(transportType)))

	var (
		cli *client.Client
		err error
	)

	switch transportType {
	case TransportHTTP:
		opts := []clientTransport.StreamableHTTPCOption{clientTransport.WithHTTPTimeout(30 * time.Second)}
		if len(params.Headers) > 0 {
			opts = append(opts, clientTransport.WithHTTPHeaders(params.Headers))
		}
		cli, err = client.NewStreamableHttpClient(params.URL, opts...)
	case TransportSSE:
		opts := []clientTransport.ClientOption{}
		if len(params.Headers) > 0 {
			opts = append(opts, client.WithHeaders(params.Headers))
		}
		cli, err = client.NewSSEMCPClient(params.URL, opts...)
	default:
		return nil, fmt.Errorf("unsupported transport %q for discovery", transportType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s client: %w", transportType, err)
	}

	defer func() {
		if closeErr := cli.Close(); closeErr != nil {
			s.logger.Debugf("Failed to close MCP client: %v", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	initRequest := mcpTypes.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcpTypes.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcpTypes.Implementation{
		Name:    "Praxis MCP Discovery",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcpTypes.ClientCapabilities{}

	initResponse, err := cli.Initialize(ctx, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	serverName := initResponse.ServerInfo.Name
	if serverName == "" {
		serverName = params.URL
	}
	serverVersion := initResponse.ServerInfo.Version
	s.logger.Infof("✅ Connected to MCP server: %s (version %s)", serverName, serverVersion)

	toolsResponse, err := cli.ListTools(ctx, mcpTypes.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	discovered := make([]DiscoveredTool, 0, len(toolsResponse.Tools))
	for _, tool := range toolsResponse.Tools {
		discovered = append(discovered, DiscoveredTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			ServerURL:   params.URL,
			ServerName:  serverName,
		})
		s.logger.Debugf("  📦 Found tool: %s - %s", tool.Name, tool.Description)
	}

	s.logger.Infof("📋 Discovered %d tools from %s", len(discovered), serverName)
	return discovered, nil
}

// DiscoverToolsFromServer attempts streamable HTTP discovery first and falls back to SSE.
func (s *ToolDiscoveryService) DiscoverToolsFromServer(ctx context.Context, serverURL string) ([]DiscoveredTool, error) {
	tools, err := s.DiscoverTools(ctx, DiscoveryParams{URL: serverURL, Transport: TransportHTTP})
	if err == nil {
		return tools, nil
	}

	s.logger.Warnf("HTTP discovery failed for %s (%v), trying SSE", serverURL, err)
	return s.DiscoverTools(ctx, DiscoveryParams{URL: serverURL, Transport: TransportSSE})
}

// DiscoverToolsFromMultipleServers discovers tools from multiple MCP servers using HTTP by default.
func (s *ToolDiscoveryService) DiscoverToolsFromMultipleServers(ctx context.Context, serverURLs []string) map[string][]DiscoveredTool {
	result := make(map[string][]DiscoveredTool)

	for _, url := range serverURLs {
		tools, err := s.DiscoverTools(ctx, DiscoveryParams{URL: url, Transport: TransportHTTP})
		if err != nil {
			s.logger.Errorf("Failed to discover tools from %s: %v", url, err)
			continue
		}
		result[url] = tools
	}

	return result
}
