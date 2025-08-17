package main

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sirupsen/logrus"
)


type LLMConfig struct {
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	Provider  string        `yaml:"provider" json:"provider"`
	APIKey    string        `yaml:"api_key" json:"api_key"`
	Model     string        `yaml:"model" json:"model"`
	MaxTokens int           `yaml:"max_tokens" json:"max_tokens"`
	Temperature float32     `yaml:"temperature" json:"temperature"`
	Timeout   time.Duration `yaml:"timeout" json:"timeout"`
	
	FunctionCalling LLMFunctionConfig `yaml:"function_calling" json:"function_calling"`
	Caching         LLMCacheConfig    `yaml:"caching" json:"caching"`
	RateLimiting    LLMRateConfig     `yaml:"rate_limiting" json:"rate_limiting"`
}

type LLMFunctionConfig struct {
	StrictMode        bool          `yaml:"strict_mode" json:"strict_mode"`
	MaxParallelCalls  int           `yaml:"max_parallel_calls" json:"max_parallel_calls"`
	ToolTimeout       time.Duration `yaml:"tool_timeout" json:"tool_timeout"`
}

type LLMCacheConfig struct {
	Enabled bool          `yaml:"enabled" json:"enabled"`
	TTL     time.Duration `yaml:"ttl" json:"ttl"`
	MaxSize int           `yaml:"max_size" json:"max_size"`
}

type LLMRateConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute" json:"requests_per_minute"`
	TokensPerMinute   int `yaml:"tokens_per_minute" json:"tokens_per_minute"`
}


type OpenAITool struct {
	Type     string             `json:"type"`
	Function OpenAIFunctionDef `json:"function"`
}

type OpenAIFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Strict      bool                   `json:"strict,omitempty"`
}

type OpenAIMessage struct {
	Role         string                 `json:"role"`
	Content      string                 `json:"content,omitempty"`
	ToolCalls    []OpenAIToolCall      `json:"tool_calls,omitempty"`
	ToolCallID   string                 `json:"tool_call_id,omitempty"`
	FunctionCall *OpenAIFunctionCall   `json:"function_call,omitempty"`
}

type OpenAIToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float32         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

type OpenAIChoice struct {
	Index        int             `json:"index"`
	Message      OpenAIMessage   `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}


type LLMRequest struct {
	ID           string                 `json:"id"`
	UserInput    string                 `json:"user_input"`
	Context      map[string]interface{} `json:"context,omitempty"`
	Tools        []string               `json:"tools,omitempty"`
	MaxTokens    int                    `json:"max_tokens,omitempty"`
	Temperature  float32                `json:"temperature,omitempty"`
}

type LLMResponse struct {
	ID           string                 `json:"id"`
	Response     string                 `json:"response"`
	ToolCalls    []ToolExecution        `json:"tool_calls,omitempty"`
	TokensUsed   int                    `json:"tokens_used"`
	ProcessTime  time.Duration          `json:"process_time"`
	Success      bool                   `json:"success"`
	Error        string                 `json:"error,omitempty"`
}

type ToolExecution struct {
	ToolName   string                 `json:"tool_name"`
	PeerName   string                 `json:"peer_name,omitempty"`
	Parameters map[string]interface{} `json:"parameters"`
	Result     interface{}            `json:"result"`
	Error      string                 `json:"error,omitempty"`
	Duration   time.Duration          `json:"duration"`
}


type ToolRegistryImpl struct {
	localTools   map[string]*LocalTool
	remoteTools  map[string]*RemoteTool
	cache        *ToolCacheImpl
	discovery    *ToolDiscoveryImpl
	lastUpdated  time.Time
	logger       *logrus.Logger
	mcpBridge    *MCPBridge
	p2pAgent     *P2PAgent
}

type LocalTool struct {
	ServerName   string    `json:"server_name"`
	Tool         MCPTool   `json:"tool"`
	Status       ToolStatus `json:"status"`
	LastUsed     time.Time `json:"last_used"`
}

type RemoteTool struct {
	PeerName     string        `json:"peer_name"`
	PeerID       peer.ID       `json:"peer_id"`
	ServerName   string        `json:"server_name"`
	Tool         MCPTool       `json:"tool"`
	Latency      time.Duration `json:"latency"`
	LastSeen     time.Time     `json:"last_seen"`
	Status       ToolStatus    `json:"status"`
}

type ToolStatus string

const (
	ToolStatusActive      ToolStatus = "active"
	ToolStatusUnavailable ToolStatus = "unavailable"
	ToolStatusError       ToolStatus = "error"
	ToolStatusUnknown     ToolStatus = "unknown"
)

type ToolCacheImpl struct {
	entries    map[string]*ToolCacheEntry
	maxSize    int
	ttl        time.Duration
}

type ToolCacheEntry struct {
	Key        string
	Value      interface{}
	CreatedAt  time.Time
	AccessedAt time.Time
	AccessCount int
}

type ToolDiscoveryImpl struct {
	enabled         bool
	refreshInterval time.Duration
	timeout         time.Duration
	p2pAgent        *P2PAgent
	logger          *logrus.Logger
}


type LLMMetrics struct {
	RequestsTotal    int64         `json:"requests_total"`
	RequestsSuccess  int64         `json:"requests_success"`
	RequestsError    int64         `json:"requests_error"`
	AvgResponseTime  time.Duration `json:"avg_response_time"`
	TokensUsed       int64         `json:"tokens_used"`
	ToolCallsTotal   int64         `json:"tool_calls_total"`
	ToolCallsSuccess int64         `json:"tool_calls_success"`
	ToolCallsError   int64         `json:"tool_calls_error"`
}


type LLMCacheImpl struct {
	entries map[string]*LLMCacheEntry
	config  LLMCacheConfig
}

type LLMCacheEntry struct {
	Key       string
	Request   *LLMRequest
	Response  *LLMResponse
	CreatedAt time.Time
}


type RateLimiterImpl struct {
	requestsPerMinute int
	tokensPerMinute   int
	requestCount      int
	tokenCount        int
	lastReset         time.Time
}


type FunctionRegistryImpl struct {
	functions map[string]OpenAIFunctionDef
}


type LLMError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (e *LLMError) Error() string {
	return e.Message
}


const (
	LLMErrorInvalidRequest = 4000
	LLMErrorUnauthorized   = 4001
	LLMErrorRateLimit      = 4029
	LLMErrorAPIError       = 5000
	LLMErrorTimeout        = 5001
	LLMErrorToolExecution  = 5002
)


var DefaultLLMConfig = LLMConfig{
	Enabled:     true,
	Provider:    "openai",
	Model:       "gpt-4o-mini",
	MaxTokens:   4096,
	Temperature: 0.1,
	Timeout:     30 * time.Second,
	FunctionCalling: LLMFunctionConfig{
		StrictMode:       true,
		MaxParallelCalls: 5,
		ToolTimeout:      15 * time.Second,
	},
	Caching: LLMCacheConfig{
		Enabled: true,
		TTL:     300 * time.Second,
		MaxSize: 1000,
	},
	RateLimiting: LLMRateConfig{
		RequestsPerMinute: 60,
		TokensPerMinute:   100000,
	},
}


type LLMClient interface {
	ProcessRequest(ctx context.Context, req *LLMRequest) (*LLMResponse, error)
	RegisterTool(tool OpenAIFunctionDef) error
	GetAvailableTools() []OpenAIFunctionDef
	Health() error
}

type ToolExecutor interface {
	ExecuteTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error)
	ExecuteRemoteTool(ctx context.Context, peerName, toolName string, params map[string]interface{}) (interface{}, error)
	GetLocalTools() map[string]MCPTool
	GetRemoteTools(peerName string) (map[string]MCPTool, error)
}