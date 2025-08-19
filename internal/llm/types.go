package llm

import (
	"context"
	"time"

	"go-p2p-agent/internal/mcp"
)

// Client defines the interface for an LLM client
type Client interface {
	// ProcessRequest processes an LLM request
	ProcessRequest(ctx context.Context, req *Request) (*Response, error)
	
	// RegisterTool registers a tool with the LLM
	RegisterTool(tool FunctionDef) error
	
	// GetAvailableTools returns the list of available tools
	GetAvailableTools() []FunctionDef
	
	// Health checks the health of the LLM connection
	Health() error
	
	// GetMetrics returns metrics about the LLM client
	GetMetrics() Metrics
}

// ToolExecutor defines the interface for executing tools
type ToolExecutor interface {
	// ExecuteTool executes a local tool
	ExecuteTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error)
	
	// ExecuteRemoteTool executes a tool on a remote peer
	ExecuteRemoteTool(ctx context.Context, peerName, toolName string, params map[string]interface{}) (interface{}, error)
	
	// GetLocalTools returns the list of local tools
	GetLocalTools() map[string]mcp.MCPTool
	
	// GetRemoteTools returns the list of tools available on a remote peer
	GetRemoteTools(peerName string) (map[string]mcp.MCPTool, error)
}

// Request represents a request to the LLM
type Request struct {
	ID           string                 `json:"id"`
	UserInput    string                 `json:"user_input"`
	Context      map[string]interface{} `json:"context,omitempty"`
	Tools        []string               `json:"tools,omitempty"`
	MaxTokens    int                    `json:"max_tokens,omitempty"`
	Temperature  float32                `json:"temperature,omitempty"`
}

// Response represents a response from the LLM
type Response struct {
	ID           string          `json:"id"`
	Response     string          `json:"response"`
	ToolCalls    []ToolExecution `json:"tool_calls,omitempty"`
	TokensUsed   int             `json:"tokens_used"`
	ProcessTime  time.Duration   `json:"process_time"`
	Success      bool            `json:"success"`
	Error        string          `json:"error,omitempty"`
}

// ToolExecution represents the execution of a tool
type ToolExecution struct {
	ToolName   string                 `json:"tool_name"`
	PeerName   string                 `json:"peer_name,omitempty"`
	Parameters map[string]interface{} `json:"parameters"`
	Result     interface{}            `json:"result"`
	Error      string                 `json:"error,omitempty"`
	Duration   time.Duration          `json:"duration"`
}

// FunctionDef defines a function that can be called by the LLM
type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Strict      bool                   `json:"strict,omitempty"`
}

// Tool represents a tool for the LLM
type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// Message represents a message in the LLM conversation
type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	FunctionCall *FunctionCall `json:"function_call,omitempty"`
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents a function call from the LLM
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Metrics contains LLM usage metrics
type Metrics struct {
	RequestsTotal    int64         `json:"requests_total"`
	RequestsSuccess  int64         `json:"requests_success"`
	RequestsError    int64         `json:"requests_error"`
	AvgResponseTime  time.Duration `json:"avg_response_time"`
	TokensUsed       int64         `json:"tokens_used"`
	ToolCallsTotal   int64         `json:"tool_calls_total"`
	ToolCallsSuccess int64         `json:"tool_calls_success"`
	ToolCallsError   int64         `json:"tool_calls_error"`
	LastRequest      time.Time     `json:"last_request"`
}

// LLMError represents an error from the LLM
type LLMError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Error implements the error interface
func (e *LLMError) Error() string {
	return e.Message
}

// LLM error codes
const (
	ErrorInvalidRequest = 4000
	ErrorUnauthorized   = 4001
	ErrorRateLimit      = 4029
	ErrorAPIError       = 5000
	ErrorTimeout        = 5001
	ErrorToolExecution  = 5002
)