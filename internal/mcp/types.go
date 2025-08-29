package mcp

import (
	"os/exec"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"

	"praxis-go-sdk/internal/config"
)

// MCPTool describes a tool provided by an MCP server
type MCPTool = mcp.Tool

// MCPResource describes a resource provided by an MCP server
type MCPResource = mcp.Resource

// MCPCapability contains information about a server's tools and resources
type MCPCapability struct {
	ServerName string
	Transport  string
	Tools      []MCPTool
	Resources  []MCPResource
	Status     string
	LastSeen   time.Time
}

// Bridge is the interface for the MCP bridge
type Bridge interface {
	Start() error
	Shutdown() error
	GetClient() Client
	GetStats() map[string]interface{}
	GetCapabilities() []MCPCapability
	ListAllTools() map[string][]MCPTool
	ListAllResources() map[string][]MCPResource
}

// The interface for interacting with MCP servers through the bridge
type Client interface {
	InvokeTool(ctx interface{}, peerID interface{}, serverName, toolName string, params map[string]interface{}) (interface{}, error)
	ListRemoteTools(ctx interface{}, peerID interface{}) (map[string][]MCPTool, error)
	ListRemoteResources(ctx interface{}, peerID interface{}) (map[string][]MCPResource, error)
	ReadRemoteResource(ctx interface{}, peerID interface{}, serverName, resourceURI string) ([]byte, error)
}

type mcpBridge struct {
	cfg          *config.MCPBridgeConfig
	clients      map[string]*mcpclient.Client
	processes    map[string]*exec.Cmd
	capabilities []MCPCapability
	logger       *logrus.Logger
	mu           sync.RWMutex
}

type mcpClient struct {
	bridge *mcpBridge
}
