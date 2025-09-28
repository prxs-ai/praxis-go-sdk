package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// ExternalMCPConfig describes configuration for an external MCP endpoint. Supports
// either a plain string (URL) or full object via custom YAML unmarshalling.
type ExternalMCPConfig struct {
	Name      string            `yaml:"name" json:"name"`
	URL       string            `yaml:"url" json:"url"`
	Transport string            `yaml:"transport" json:"transport"`
	Headers   map[string]string `yaml:"headers" json:"headers"`
}

// UnmarshalYAML allows ExternalMCPConfig to accept scalar (URL) or mapping values.
func (c *ExternalMCPConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*c = ExternalMCPConfig{URL: value.Value}
		return nil
	case yaml.MappingNode:
		type raw ExternalMCPConfig
		var r raw
		if err := value.Decode(&r); err != nil {
			return err
		}
		*c = ExternalMCPConfig(r)
		return nil
	default:
		return fmt.Errorf("invalid external MCP endpoint entry: kind %d", value.Kind)
	}
}

// AppConfig is the main configuration structure for the application
type AppConfig struct {
	Agent   AgentConfig     `yaml:"agent" json:"agent"`
	P2P     P2PConfig       `yaml:"p2p" json:"p2p"`
	HTTP    HTTPConfig      `yaml:"http" json:"http"`
	MCP     MCPBridgeConfig `yaml:"mcp" json:"mcp"`
	LLM     LLMConfig       `yaml:"llm" json:"llm"`
	Logging LogConfig       `yaml:"logging" json:"logging"`
}

// ToolConfig определяет конфигурацию одного инструмента в YAML.
type ToolConfig struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Engine      string                 `yaml:"engine"`
	Params      []map[string]string    `yaml:"params"`
	EngineSpec  map[string]interface{} `yaml:"engineSpec"`
}

// IdentityConfig configures DID identity and key management for the agent.
type IdentityConfig struct {
	DID       string            `yaml:"did" json:"did"`
	DIDDocURI string            `yaml:"did_doc_uri" json:"did_doc_uri"`
	Key       IdentityKeyConfig `yaml:"key" json:"key"`
}

// IdentityKeyConfig defines how the agent's signing key is sourced.
type IdentityKeyConfig struct {
	Type    string `yaml:"type" json:"type"`
	Source  string `yaml:"source" json:"source"`
	Path    string `yaml:"path" json:"path"`
	Service string `yaml:"service" json:"service"`
	Account string `yaml:"account" json:"account"`
	ID      string `yaml:"id" json:"id"`
}

// AgentSecurityConfig toggles signing/verification features.
type AgentSecurityConfig struct {
	SignCards       bool `yaml:"sign_cards" json:"sign_cards"`
	VerifyPeerCards bool `yaml:"verify_peer_cards" json:"verify_peer_cards"`
	SignA2A         bool `yaml:"sign_a2a" json:"sign_a2a"`
	VerifyA2A       bool `yaml:"verify_a2a" json:"verify_a2a"`
}

// ToolParamConfig описывает параметр инструмента
type ToolParamConfig struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    string `yaml:"required"`
}

// AgentConfig contains basic agent information
type AgentConfig struct {
	Name                 string              `yaml:"name" json:"name"`
	Version              string              `yaml:"version" json:"version"`
	Description          string              `yaml:"description" json:"description"`
	URL                  string              `yaml:"url" json:"url"`
	SharedDir            string              `yaml:"shared_dir" json:"shared_dir"`                         // Base directory for filesystem tools
	Tools                []ToolConfig        `yaml:"tools"`                                                // Список инструментов, доступных агенту
	ExternalMCPEndpoints []ExternalMCPConfig `yaml:"external_mcp_endpoints" json:"external_mcp_endpoints"` // Внешние MCP серверы для автообнаружения
	ExternalMCPServers   []ExternalMCPConfig `yaml:"external_mcp_servers" json:"external_mcp_servers"`     // Alias для ExternalMCPEndpoints
	Identity             IdentityConfig      `yaml:"identity" json:"identity"`
	Security             AgentSecurityConfig `yaml:"security" json:"security"`
	DIDCacheTTL          time.Duration       `yaml:"did_cache_ttl" json:"did_cache_ttl"`
}

// P2PConfig contains libp2p configuration
type P2PConfig struct {
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	Port           int      `yaml:"port" json:"port"`
	Secure         bool     `yaml:"secure" json:"secure"`
	Rendezvous     string   `yaml:"rendezvous" json:"rendezvous"`
	EnableMDNS     bool     `yaml:"enable_mdns" json:"enable_mdns"`
	EnableDHT      bool     `yaml:"enable_dht" json:"enable_dht"`
	BootstrapNodes []string `yaml:"bootstrap_nodes" json:"bootstrap_nodes"`
}

// HTTPConfig contains HTTP server configuration
type HTTPConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Port    int    `yaml:"port" json:"port"`
	Host    string `yaml:"host" json:"host"`
}

// MCPBridgeConfig contains MCP bridge configuration
type MCPBridgeConfig struct {
	Enabled   bool              `yaml:"enabled" json:"enabled"`
	Transport string            `yaml:"transport" json:"transport"`
	Servers   []MCPServerConfig `yaml:"servers" json:"servers"`
	Limits    MCPLimits         `yaml:"limits" json:"limits"`
	LogLevel  string            `yaml:"log_level" json:"log_level"`
}

// MCPServerConfig defines a single MCP server configuration
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

// MCPLimits defines resource limits for the MCP bridge
type MCPLimits struct {
	MaxConcurrentRequests int   `yaml:"max_concurrent_requests" json:"max_concurrent_requests"`
	RequestTimeoutMs      int   `yaml:"request_timeout_ms" json:"request_timeout_ms"`
	MaxResponseSizeBytes  int64 `yaml:"max_response_size_bytes" json:"max_response_size_bytes"`
	MaxServersPerNode     int   `yaml:"max_servers_per_node" json:"max_servers_per_node"`
	ConnectionPoolSize    int   `yaml:"connection_pool_size" json:"connection_pool_size"`
	RetryAttempts         int   `yaml:"retry_attempts" json:"retry_attempts"`
	RetryBackoffMs        int   `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
}

// LLMConfig contains LLM integration configuration
type LLMConfig struct {
	Enabled     bool          `yaml:"enabled" json:"enabled"`
	Provider    string        `yaml:"provider" json:"provider"`
	APIKey      string        `yaml:"api_key" json:"api_key"`
	Model       string        `yaml:"model" json:"model"`
	MaxTokens   int           `yaml:"max_tokens" json:"max_tokens"`
	Temperature float32       `yaml:"temperature" json:"temperature"`
	Timeout     time.Duration `yaml:"timeout" json:"timeout"`

	FunctionCalling LLMFunctionConfig `yaml:"function_calling" json:"function_calling"`
	Caching         LLMCacheConfig    `yaml:"caching" json:"caching"`
	RateLimiting    LLMRateConfig     `yaml:"rate_limiting" json:"rate_limiting"`
}

// LLMFunctionConfig contains function calling configuration
type LLMFunctionConfig struct {
	StrictMode       bool          `yaml:"strict_mode" json:"strict_mode"`
	MaxParallelCalls int           `yaml:"max_parallel_calls" json:"max_parallel_calls"`
	ToolTimeout      time.Duration `yaml:"tool_timeout" json:"tool_timeout"`
}

// LLMCacheConfig contains LLM caching configuration
type LLMCacheConfig struct {
	Enabled bool          `yaml:"enabled" json:"enabled"`
	TTL     time.Duration `yaml:"ttl" json:"ttl"`
	MaxSize int           `yaml:"max_size" json:"max_size"`
}

// LLMRateConfig contains LLM rate limiting configuration
type LLMRateConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute" json:"requests_per_minute"`
	TokensPerMinute   int `yaml:"tokens_per_minute" json:"tokens_per_minute"`
}

// LogConfig contains logging configuration
type LogConfig struct {
	Level  string `yaml:"level" json:"level"`
	Format string `yaml:"format" json:"format"`
	File   string `yaml:"file" json:"file"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *AppConfig {
	return &AppConfig{
		Agent: AgentConfig{
			Name:        "go-agent",
			Version:     "1.0.0",
			Description: "Go P2P Agent",
			URL:         "http://localhost:8000",
			Identity: IdentityConfig{
				Key: IdentityKeyConfig{
					Type:   "ed25519",
					Source: "file",
					Path:   "./configs/keys/ed25519.key",
					ID:     "key-1",
				},
			},
			Security:    AgentSecurityConfig{},
			DIDCacheTTL: time.Minute,
		},
		P2P: P2PConfig{
			Enabled:    true,
			Port:       0, // Random port
			Secure:     true,
			Rendezvous: "praxis-agents",
			EnableMDNS: true,
			EnableDHT:  true,
		},
		HTTP: HTTPConfig{
			Enabled: true,
			Port:    8000,
			Host:    "0.0.0.0",
		},
		MCP: MCPBridgeConfig{
			Enabled:   true,
			Transport: "sse",
			Limits: MCPLimits{
				MaxConcurrentRequests: 100,
				RequestTimeoutMs:      30000,
				MaxResponseSizeBytes:  10485760,
				MaxServersPerNode:     10,
				ConnectionPoolSize:    5,
				RetryAttempts:         3,
				RetryBackoffMs:        1000,
			},
			LogLevel: "info",
		},
		LLM: LLMConfig{
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
		},
		Logging: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}
