package main

import (
	"encoding/json"
	"fmt"
	"time"
)

type MCPCapability struct {
	ServerName   string                 `json:"server_name"`
	Transport    string                 `json:"transport"`
	Tools        []MCPTool             `json:"tools"`
	Resources    []MCPResource         `json:"resources,omitempty"`
	Status       string                `json:"status"`
	LastSeen     time.Time             `json:"last_seen,omitempty"`
}

type MCPTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	InputSchema  map[string]interface{} `json:"input_schema"`
	OutputSchema map[string]interface{} `json:"output_schema,omitempty"`
}

type MCPResource struct {
	URI         string                 `json:"uri"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mime_type,omitempty"`
}

type ExtendedAgentCard struct {
	AgentCard
	MCPServers []MCPCapability `json:"mcp_servers,omitempty"`
}

type MCPRequest struct {
	ID         string                 `json:"id"`
	Method     string                 `json:"method"`
	ServerName string                 `json:"server_name"`
	ToolName   string                 `json:"tool_name,omitempty"`
	Params     map[string]interface{} `json:"params"`
	Timeout    time.Duration          `json:"timeout,omitempty"`
}

type MCPResponse struct {
	ID      string      `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *MCPError) Error() string {
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

type MCPServerConfig struct {
	Name      string            `yaml:"name" json:"name"`
	Transport string            `yaml:"transport" json:"transport"`
	Command   string            `yaml:"command" json:"command,omitempty"`
	Args      []string          `yaml:"args" json:"args,omitempty"`
	Env       map[string]string `yaml:"env" json:"env,omitempty"`
	URL       string            `yaml:"url" json:"url,omitempty"`
	WorkDir   string            `yaml:"workdir" json:"workdir,omitempty"`
	Timeout   time.Duration     `yaml:"timeout" json:"timeout,omitempty"`
	Enabled   bool              `yaml:"enabled" json:"enabled"`
}

type MCPBridgeConfig struct {
	Enabled   bool              `yaml:"enabled" json:"enabled"`
	Servers   []MCPServerConfig `yaml:"servers" json:"servers"`
	Limits    MCPLimits         `yaml:"limits" json:"limits"`
	LogLevel  string            `yaml:"log_level" json:"log_level"`
}

type MCPLimits struct {
	MaxConcurrentRequests  int           `yaml:"max_concurrent_requests" json:"max_concurrent_requests"`
	RequestTimeoutMs       int           `yaml:"request_timeout_ms" json:"request_timeout_ms"`
	MaxResponseSizeBytes   int64         `yaml:"max_response_size_bytes" json:"max_response_size_bytes"`
	MaxServersPerNode      int           `yaml:"max_servers_per_node" json:"max_servers_per_node"`
	ConnectionPoolSize     int           `yaml:"connection_pool_size" json:"connection_pool_size"`
	RetryAttempts          int           `yaml:"retry_attempts" json:"retry_attempts"`
	RetryBackoffMs         int           `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
}

var DefaultMCPLimits = MCPLimits{
	MaxConcurrentRequests:  100,
	RequestTimeoutMs:       30000,
	MaxResponseSizeBytes:   10485760,
	MaxServersPerNode:      10,
	ConnectionPoolSize:     5,
	RetryAttempts:          3,
	RetryBackoffMs:         1000,
}

const (
	MCPBridgeProtocol = "/mcp/bridge/1.0.0"
	
	MCPMethodToolCall      = "tools/call"
	MCPMethodListTools     = "tools/list"
	MCPMethodListResources = "resources/list"
	MCPMethodReadResource  = "resources/read"
	MCPMethodInitialize    = "initialize"
	MCPMethodPing          = "ping"
	
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

func NewMCPRequest(id, method, serverName string, params map[string]interface{}) *MCPRequest {
	return &MCPRequest{
		ID:         id,
		Method:     method,
		ServerName: serverName,
		Params:     params,
		Timeout:    30 * time.Second,
	}
}

func NewMCPToolCallRequest(id, serverName, toolName string, params map[string]interface{}) *MCPRequest {
	req := NewMCPRequest(id, MCPMethodToolCall, serverName, params)
	req.ToolName = toolName
	return req
}

func NewMCPResponse(id string, result interface{}) *MCPResponse {
	return &MCPResponse{
		ID:     id,
		Result: result,
	}
}

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

func (r *MCPRequest) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

func (r *MCPResponse) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

func UnmarshalMCPRequest(data []byte) (*MCPRequest, error) {
	var req MCPRequest
	err := json.Unmarshal(data, &req)
	return &req, err
}

func UnmarshalMCPResponse(data []byte) (*MCPResponse, error) {
	var resp MCPResponse
	err := json.Unmarshal(data, &resp)
	return &resp, err
}

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

func (c *MCPServerConfig) Validate() error {
	if c.Name == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "missing server name"}
	}
	if c.Transport != "stdio" && c.Transport != "sse" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "transport must be 'stdio' or 'sse'"}
	}
	if c.Transport == "stdio" && c.Command == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "command required for stdio transport"}
	}
	if c.Transport == "sse" && c.URL == "" {
		return &MCPError{Code: MCPErrorInvalidRequest, Message: "URL required for sse transport"}
	}
	return nil
}