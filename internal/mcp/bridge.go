package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	transport "github.com/mark3labs/mcp-go/client/transport"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"praxis-go-sdk/internal/config"
)

// NewMCPBridge creates a new bridge using the provided configuration file
func NewMCPBridge(_ interface{}, configPath string, logger *logrus.Logger) (Bridge, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}
	b := &mcpBridge{
		cfg:       cfg,
		clients:   make(map[string]*mcpclient.Client),
		processes: make(map[string]*exec.Cmd),
		logger:    logger,
	}
	return b, nil
}

// Reads the MCP configuration from the given path.
func loadConfig(path string) (*config.MCPBridgeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config.MCPBridgeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Start launches configured MCP servers and initializes clients.
func (b *mcpBridge) Start() error {
	if b.cfg == nil || !b.cfg.Enabled {
		return nil
	}

	for _, srv := range b.cfg.Servers {
		if !srv.Enabled {
			continue
		}
		cmd := exec.Command(srv.Command, srv.Args...)
		if srv.WorkDir != "" {
			cmd.Dir = srv.WorkDir
		}
		if len(srv.Env) > 0 {
			cmd.Env = append(os.Environ(), envMapToSlice(srv.Env)...)

		}

		stdin, err := cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdin pipe for %s: %w", srv.Name, err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe for %s: %w", srv.Name, err)
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start server %s: %w", srv.Name, err)
		}

		transport := transport.NewIO(stdout, stdin, nil)
		if err := transport.Start(context.Background()); err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("failed to start transport for %s: %w", srv.Name, err)
		}

		client := mcpclient.NewClient(transport)
		initReq := mcp.InitializeRequest{Params: mcp.InitializeParams{ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION}}
		if _, err := client.Initialize(context.Background(), initReq); err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("failed to initialize server %s: %w", srv.Name, err)
		}

		b.clients[srv.Name] = client
		b.processes[srv.Name] = cmd
	}

	//
	b.updateCapabilities()
	return nil
}

// Shutdown terminates all started MCP servers.
func (b *mcpBridge) Shutdown() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for name, proc := range b.processes {
		if proc.Process != nil {
			_ = proc.Process.Kill()
			b.logger.Infof("Stopped MCP server: %s", name)
		}
	}

	b.clients = make(map[string]*mcpclient.Client)
	b.processes = make(map[string]*exec.Cmd)
	b.capabilities = nil
	return nil
}

// GetClient returns a client wrapper for interacting with servers.
func (b *mcpBridge) GetClient() Client {
	return &mcpClient{bridge: b}
}

// GetStats returns basic statistics. Currently not implemented.
func (b *mcpBridge) GetStats() map[string]interface{} {
	return map[string]interface{}{}
}

// GetCapabilities returns the collected capabilities for all servers.
func (b *mcpBridge) GetCapabilities() []MCPCapability {
	b.mu.RLock()
	defer b.mu.RUnlock()
	caps := make([]MCPCapability, len(b.capabilities))
	copy(caps, b.capabilities)
	return caps
}

// ListAllTools lists tools from all registered servers.
func (b *mcpBridge) ListAllTools() map[string][]MCPTool {
	res := make(map[string][]MCPTool)
	for name, client := range b.clients {
		toolsResp, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})

		if err != nil {
			b.logger.Warnf("failed to list tools for %s: %v", name, err)
			continue
		}

		res[name] = toolsResp.Tools
	}

	return res
}

// ListAllResources lists resources from all registered servers.
func (b *mcpBridge) ListAllResources() map[string][]MCPResource {
	res := make(map[string][]MCPResource)
	for name, client := range b.clients {
		resResp, err := client.ListResources(context.Background(), mcp.ListResourcesRequest{})

		if err != nil {
			b.logger.Warnf("failed to list resources for %s: %v", name, err)
			continue
		}
		resources := make([]MCPResource, 0, len(resResp.Resources))
		// for _, r := range resResp.Resources {
		// 	if r != nil {
		// 		resources = append(resources, *r)
		// 	}
		// }
		res[name] = resources
	}

	return res
}

// updateCapabilities refreshes the cached capabilities for all servers.
func (b *mcpBridge) updateCapabilities() {
	b.mu.Lock()
	defer b.mu.Unlock()

	var caps []MCPCapability
	for name, client := range b.clients {
		toolsResp, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})
		if err != nil {
			b.logger.Warnf("failed to list tools for %s: %v", name, err)
			continue
		}

		resResp, err := client.ListResources(context.Background(), mcp.ListResourcesRequest{})
		var resources []MCPResource
		if err != nil {
			b.logger.Warnf("failed to list resources for %s: %v", name, err)
		} else {
			resources = resResp.Resources
		}
		caps = append(caps, MCPCapability{
			ServerName: name,
			Transport:  "stdio",
			Tools:      toolsResp.Tools,
			Resources:  resources,
			Status:     "active",
			LastSeen:   time.Now(),
		})
	}

	b.capabilities = caps
}

// envMapToSlice converts an environment map to a slice of "key=value" strings.
func envMapToSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

// InvokeTool invokes a tool on a server via the bridge's client wrapper.
func (c *mcpClient) InvokeTool(ctx interface{}, _ interface{}, serverName, toolName string, params map[string]interface{}) (interface{}, error) {
	client, ok := c.bridge.clients[serverName]
	if !ok {
		return nil, fmt.Errorf("unknown MCP server: %s", serverName)
	}

	var realCtx context.Context
	switch t := ctx.(type) {
	case context.Context:
		realCtx = t
	default:
		realCtx = context.Background()
	}

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: params,
		},
	}
	resp, err := client.CallTool(realCtx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ListRemoteTools is currently not implemented for the simplified bridge.
func (c *mcpClient) ListRemoteTools(ctx interface{}, peerID interface{}) (map[string][]MCPTool, error) {
	return nil, fmt.Errorf("ListRemoteTools not supported in this MCP implementation")
}

// ListRemoteResources is currently not implemented for the simplified bridge.
func (c *mcpClient) ListRemoteResources(ctx interface{}, peerID interface{}) (map[string][]MCPResource, error) {
	return nil, fmt.Errorf("ListRemoteResources not supported in this MCP implementation")
}

// ReadRemoteResource is currently not implemented for the simplified bridge.
func (c *mcpClient) ReadRemoteResource(ctx interface{}, peerID interface{}, serverName, resourceURI string) ([]byte, error) {
	return nil, fmt.Errorf("ReadRemoteResource not supported in this MCP implementation")
}
