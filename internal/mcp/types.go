package mcp

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// Protocol constants
const (
	// MCP protocol IDs
	MCPToolsCallProtocol     = protocol.ID("/mcp/tools/call/1.0.0")
	MCPToolsListProtocol     = protocol.ID("/mcp/tools/list/1.0.0")
	MCPResourcesListProtocol = protocol.ID("/mcp/resources/list/1.0.0")
	MCPResourcesReadProtocol = protocol.ID("/mcp/resources/read/1.0.0")
	MCPBridgeProtocol        = protocol.ID("/mcp/bridge/1.0.0")
	
	// MCP method names
	MCPMethodToolCall      = "tools/call"
	MCPMethodListTools     = "tools/list"
	MCPMethodListResources = "resources/list"
	MCPMethodReadResource  = "resources/read"
	MCPMethodInitialize    = "initialize"
	MCPMethodPing          = "ping"
	
	// MCP error codes
	MCPErrorParseError     = -32700
	MCPErrorInvalidRequest = -32600
	MCPErrorMethodNotFound = -32601
	MCPErrorInvalidParams  = -32602
	MCPErrorInternalError  = -32603
	MCPErrorServerError    = -32000
	MCPErrorTimeout        = -32001
	MCPErrorNotFound       = -32002
	MCPErrorPermission     = -32003
)

// Bridge is the interface for the MCP bridge
type Bridge interface {
	// Start initializes and starts the MCP bridge
	Start() error
	
	// Shutdown stops the MCP bridge and cleans up resources
	Shutdown() error
	
	// GetClient returns an MCP client for making requests
	GetClient() Client
	
	// GetStats returns statistics about the MCP bridge
	GetStats() map[string]interface{}
	
	// GetCapabilities returns the MCP capabilities
	GetCapabilities() []MCPCapability
	
	// ListAllTools returns all available tools across all MCP servers
	ListAllTools() map[string][]MCPTool
	
	// ListAllResources returns all available resources across all MCP servers
	ListAllResources() map[string][]MCPResource
}

// Client is the interface for an MCP client
type Client interface {
	// InvokeTool invokes a tool on an MCP server
	InvokeTool(ctx interface{}, peerID peer.ID, serverName, toolName string, params map[string]interface{}) (interface{}, error)
	
	// ListRemoteTools lists tools available on a remote peer
	ListRemoteTools(ctx interface{}, peerID peer.ID) (map[string][]MCPTool, error)
	
	// ListRemoteResources lists resources available on a remote peer
	ListRemoteResources(ctx interface{}, peerID peer.ID) (map[string][]MCPResource, error)
	
	// ReadRemoteResource reads a resource from a remote peer
	ReadRemoteResource(ctx interface{}, peerID peer.ID, serverName, resourceURI string) ([]byte, error)
}

// MCPCapability describes an MCP server's capabilities
type MCPCapability struct {
	ServerName   string        `json:"server_name"`
	Transport    string        `json:"transport"`
	Tools        []MCPTool     `json:"tools"`
	Resources    []MCPResource `json:"resources,omitempty"`
	Status       string        `json:"status"`
	LastSeen     time.Time     `json:"last_seen,omitempty"`
}

// MCPTool describes a tool provided by an MCP server
type MCPTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	OutputSchema map[string]interface{} `json:"output_schema,omitempty"`
}

// MCPResource describes a resource provided by an MCP server
type MCPResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

// MCPRequest represents a request to an MCP server
type MCPRequest struct {
	ID         string                 `json:"id"`
	Method     string                 `json:"method"`
	ServerName string                 `json:"server_name"`
	ToolName   string                 `json:"tool_name,omitempty"`
	Params     map[string]interface{} `json:"params"`
	Timeout    time.Duration          `json:"timeout,omitempty"`
}

// MCPResponse represents a response from an MCP server
type MCPResponse struct {
	ID      string      `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an error from an MCP server
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error implements the error interface
func (e *MCPError) Error() string {
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

// NewMCPRequest creates a new MCP request
func NewMCPRequest(id, method, serverName string, params map[string]interface{}) *MCPRequest {
	return &MCPRequest{
		ID:         id,
		Method:     method,
		ServerName: serverName,
		Params:     params,
		Timeout:    30 * time.Second,
	}
}

// NewMCPToolCallRequest creates a new tool call request
func NewMCPToolCallRequest(id, serverName, toolName string, params map[string]interface{}) *MCPRequest {
	req := NewMCPRequest(id, MCPMethodToolCall, serverName, params)
	req.ToolName = toolName
	return req
}

// NewMCPResponse creates a new MCP response
func NewMCPResponse(id string, result interface{}) *MCPResponse {
	return &MCPResponse{
		ID:     id,
		Result: result,
	}
}

// NewMCPErrorResponse creates a new MCP error response
func NewMCPErrorResponse(id string, code int, message string, data interface{}) *MCPResponse {
	return &MCPResponse{
		ID: id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// Marshal serializes an MCP request to JSON
func (r *MCPRequest) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// Marshal serializes an MCP response to JSON
func (r *MCPResponse) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// UnmarshalMCPRequest deserializes an MCP request from JSON
func UnmarshalMCPRequest(data []byte) (*MCPRequest, error) {
	var req MCPRequest
	err := json.Unmarshal(data, &req)
	return &req, err
}

// UnmarshalMCPResponse deserializes an MCP response from JSON
func UnmarshalMCPResponse(data []byte) (*MCPResponse, error) {
	var resp MCPResponse
	err := json.Unmarshal(data, &resp)
	return &resp, err
}

// Validate validates an MCP request
func (r *MCPRequest) Validate() error {
	if r.ID == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "missing request ID"}
	}
	if r.Method == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "missing method"}
	}
	if r.ServerName == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "missing server name"}
	}
	if r.Method == MCPMethodToolCall && r.ToolName == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "missing tool name for tool call"}
	}
	return nil
}