package config

import (
	"time"
)

// AppConfig is the main configuration structure for the application
type AppConfig struct {
	Agent      AgentConfig      `koanf:"agent" yaml:"agent" json:"agent"`
	P2P        P2PConfig        `koanf:"p2p" yaml:"p2p" json:"p2p"`
	HTTP       HTTPConfig       `koanf:"http" yaml:"http" json:"http"`
	MCP        MCPBridgeConfig  `koanf:"mcp" yaml:"mcp" json:"mcp"`
	LLM        LLMConfig        `koanf:"llm" yaml:"llm" json:"llm"`
	Logging    LogConfig        `koanf:"logging" yaml:"logging" json:"logging"`
	Prometheus PrometheusConfig `koanf:"prometheus" yaml:"prometheus" json:"prometheus"`
}

// ToolConfig определяет конфигурацию одного инструмента в YAML.
type ToolConfig struct {
	Name        string                 `koanf:"name" yaml:"name"`
	Description string                 `koanf:"description" yaml:"description"`
	Engine      string                 `koanf:"engine" yaml:"engine"`
	Params      []map[string]string    `koanf:"params" yaml:"params"`
	EngineSpec  map[string]interface{} `koanf:"engineSpec" yaml:"engineSpec"`
}

// IdentityConfig configures DID identity and key management for the agent.
type IdentityConfig struct {
	DID       string            `koanf:"did" yaml:"did" json:"did"`
	DIDDocURI string            `koanf:"did_doc_uri" yaml:"did_doc_uri" json:"did_doc_uri"`
	Key       IdentityKeyConfig `koanf:"key" yaml:"key" json:"key"`
}

// IdentityKeyConfig defines how the agent's signing key is sourced.
type IdentityKeyConfig struct {
	Type    string `koanf:"type" yaml:"type" json:"type"`
	Source  string `koanf:"source" yaml:"source" json:"source"`
	Path    string `koanf:"path" yaml:"path" json:"path"`
	Service string `koanf:"service" yaml:"service" json:"service"`
	Account string `koanf:"account" yaml:"account" json:"account"`
	ID      string `koanf:"id" yaml:"id" json:"id"`
}

// AgentSecurityConfig toggles signing/verification features.
type AgentSecurityConfig struct {
	SignCards       bool `koanf:"sign_cards" yaml:"sign_cards" json:"sign_cards"`
	VerifyPeerCards bool `koanf:"verify_peer_cards" yaml:"verify_peer_cards" json:"verify_peer_cards"`
	SignA2A         bool `koanf:"sign_a2a" yaml:"sign_a2a" json:"sign_a2a"`
	VerifyA2A       bool `koanf:"verify_a2a" yaml:"verify_a2a" json:"verify_a2a"`
}

// ToolParamConfig описывает параметр инструмента
type ToolParamConfig struct {
	Name        string `koanf:"name" yaml:"name"`
	Type        string `koanf:"type" yaml:"type"`
	Description string `koanf:"description" yaml:"description"`
	Required    string `koanf:"required" yaml:"required"`
}

type ExternalMCPConfig struct {
	Name      string            `koanf:"name" yaml:"name" json:"name"`
	URL       string            `koanf:"url" yaml:"url" json:"url"`
	Transport string            `koanf:"transport" yaml:"transport" json:"transport"`
	Headers   map[string]string `koanf:"headers" yaml:"headers" json:"headers"`
}

// AgentConfig contains basic agent information
type AgentConfig struct {
	Name                 string              `koanf:"name" yaml:"name" json:"name"`
	Version              string              `koanf:"version" yaml:"version" json:"version"`
	Description          string              `koanf:"description" yaml:"description" json:"description"`
	URL                  string              `koanf:"url" yaml:"url" json:"url"`
	SharedDir            string              `koanf:"shared_dir" yaml:"shared_dir" json:"shared_dir"`                                     // Base directory for filesystem tools
	Tools                []ToolConfig        `koanf:"tools" yaml:"tools"`                                                                 // Список инструментов, доступных агенту
	ExternalMCPEndpoints []ExternalMCPConfig `koanf:"external_mcp_endpoints" yaml:"external_mcp_endpoints" json:"external_mcp_endpoints"` // Внешние MCP серверы для автообнаружения
	ExternalMCPServers   []ExternalMCPConfig `koanf:"external_mcp_servers" yaml:"external_mcp_servers" json:"external_mcp_servers"`       // Alias для ExternalMCPEndpoints
	Identity             IdentityConfig      `koanf:"identity" yaml:"identity" json:"identity"`
	Security             AgentSecurityConfig `koanf:"security" yaml:"security" json:"security"`
	DIDCacheTTL          time.Duration       `koanf:"did_cache_ttl" yaml:"did_cache_ttl" json:"did_cache_ttl"`
}

// P2PConfig contains libp2p configuration
type P2PConfig struct {
	Enabled          bool          `koanf:"enabled" yaml:"enabled" json:"enabled"`
	Port             int           `koanf:"port" yaml:"port" json:"port"`
	Secure           bool          `koanf:"secure" yaml:"secure" json:"secure"`
	Rendezvous       string        `koanf:"rendezvous" yaml:"rendezvous" json:"rendezvous"`
	EnableMDNS       bool          `koanf:"enable_mdns" yaml:"enable_mdns" json:"enable_mdns"`
	EnableDHT        bool          `koanf:"enable_dht" yaml:"enable_dht" json:"enable_dht"`
	EnableNATPortMap bool          `koanf:"enable_nat_portmap" yaml:"enable_nat_portmap" json:"enable_nat_portmap"`
	AdvertiseAddrs   []string      `koanf:"advertise_addrs" yaml:"advertise_addrs" json:"advertise_addrs"`
	BootstrapNodes   []string      `koanf:"bootstrap_nodes" yaml:"bootstrap_nodes" json:"bootstrap_nodes"`
	AutoTLS          AutoTLSConfig `koanf:"autotls" yaml:"autotls" json:"autotls"`
}

// AutoTLSConfig controls integration with libp2p AutoTLS (libp2p.direct).
type AutoTLSConfig struct {
	Enabled               bool   `koanf:"enabled" yaml:"enabled" json:"enabled"`
	CA                    string `koanf:"ca" yaml:"ca" json:"ca"`
	CertDir               string `koanf:"cert_dir" yaml:"cert_dir" json:"cert_dir"`
	IdentityKeyPath       string `koanf:"identity_key" yaml:"identity_key" json:"identity_key"`
	RegistrationDelaySec  int    `koanf:"registration_delay_sec" yaml:"registration_delay_sec" json:"registration_delay_sec"`
	AllowPrivateAddresses bool   `koanf:"allow_private_addresses" yaml:"allow_private_addresses" json:"allow_private_addresses"`
	ProduceShortAddrs     bool   `koanf:"produce_short_addrs" yaml:"produce_short_addrs" json:"produce_short_addrs"`
	ForgeDomain           string `koanf:"forge_domain" yaml:"forge_domain" json:"forge_domain"`
	RegistrationEndpoint  string `koanf:"registration_endpoint" yaml:"registration_endpoint" json:"registration_endpoint"`
	ForgeAuthToken        string `koanf:"forge_auth_token" yaml:"forge_auth_token" json:"forge_auth_token"`
	TrustedRootsFile      string `koanf:"trusted_roots_file" yaml:"trusted_roots_file" json:"trusted_roots_file"`
	ResolverAddress       string `koanf:"resolver_address" yaml:"resolver_address" json:"resolver_address"`
	ResolverNetwork       string `koanf:"resolver_network" yaml:"resolver_network" json:"resolver_network"`
}

// HTTPConfig contains HTTP server configuration
type HTTPConfig struct {
	Enabled bool   `koanf:"enabled" yaml:"enabled" json:"enabled"`
	Port    int    `koanf:"port" yaml:"port" json:"port"`
	Host    string `koanf:"host" yaml:"host" json:"host"`
}

// MCPBridgeConfig contains MCP bridge configuration
type MCPBridgeConfig struct {
	Enabled  bool              `koanf:"enabled" yaml:"enabled" json:"enabled"`
	Servers  []MCPServerConfig `koanf:"servers" yaml:"servers" json:"servers"`
	Limits   MCPLimits         `koanf:"limits" yaml:"limits" json:"limits"`
	LogLevel string            `koanf:"log_level" yaml:"log_level" json:"log_level"`
}

// MCPServerConfig defines a single MCP server configuration
type MCPServerConfig struct {
	Name      string            `koanf:"name" yaml:"name" json:"name"`
	Transport string            `koanf:"transport" yaml:"transport" json:"transport"`
	Command   string            `koanf:"command" yaml:"command" json:"command,omitempty"`
	Args      []string          `koanf:"args" yaml:"args" json:"args,omitempty"`
	Env       map[string]string `koanf:"env" yaml:"env" json:"env,omitempty"`
	URL       string            `koanf:"url" yaml:"url" json:"url,omitempty"`
	WorkDir   string            `koanf:"workdir" yaml:"workdir" json:"workdir,omitempty"`
	Timeout   time.Duration     `koanf:"timeout" yaml:"timeout" json:"timeout,omitempty"`
	Enabled   bool              `koanf:"enabled" yaml:"enabled" json:"enabled"`
}

// MCPLimits defines resource limits for the MCP bridge
type MCPLimits struct {
	MaxConcurrentRequests int   `koanf:"max_concurrent_requests" yaml:"max_concurrent_requests" json:"max_concurrent_requests"`
	RequestTimeoutMs      int   `koanf:"request_timeout_ms" yaml:"request_timeout_ms" json:"request_timeout_ms"`
	MaxResponseSizeBytes  int64 `koanf:"max_response_size_bytes" yaml:"max_response_size_bytes" json:"max_response_size_bytes"`
	MaxServersPerNode     int   `koanf:"max_servers_per_node" yaml:"max_servers_per_node" json:"max_servers_per_node"`
	ConnectionPoolSize    int   `koanf:"connection_pool_size" yaml:"connection_pool_size" json:"connection_pool_size"`
	RetryAttempts         int   `koanf:"retry_attempts" yaml:"retry_attempts" json:"retry_attempts"`
	RetryBackoffMs        int   `koanf:"retry_backoff_ms" yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
}

// LLMConfig contains LLM integration configuration
type LLMConfig struct {
	Enabled     bool          `koanf:"enabled" yaml:"enabled" json:"enabled"`
	Provider    string        `koanf:"provider" yaml:"provider" json:"provider"`
	APIKey      string        `koanf:"api_key" yaml:"api_key" json:"api_key"`
	Model       string        `koanf:"model" yaml:"model" json:"model"`
	MaxTokens   int           `koanf:"max_tokens" yaml:"max_tokens" json:"max_tokens"`
	Temperature float32       `koanf:"temperature" yaml:"temperature" json:"temperature"`
	Timeout     time.Duration `koanf:"timeout" yaml:"timeout" json:"timeout"`

	FunctionCalling LLMFunctionConfig `koanf:"function_calling" yaml:"function_calling" json:"function_calling"`
	Caching         LLMCacheConfig    `koanf:"caching" yaml:"caching" json:"caching"`
	RateLimiting    LLMRateConfig     `koanf:"rate_limiting" yaml:"rate_limiting" json:"rate_limiting"`
}

// LLMFunctionConfig contains function calling configuration
type LLMFunctionConfig struct {
	StrictMode       bool          `koanf:"strict_mode" yaml:"strict_mode" json:"strict_mode"`
	MaxParallelCalls int           `koanf:"max_parallel_calls" yaml:"max_parallel_calls" json:"max_parallel_calls"`
	ToolTimeout      time.Duration `koanf:"tool_timeout" yaml:"tool_timeout" json:"tool_timeout"`
}

// LLMCacheConfig contains LLM caching configuration
type LLMCacheConfig struct {
	Enabled bool          `koanf:"enabled" yaml:"enabled" json:"enabled"`
	TTL     time.Duration `koanf:"ttl" yaml:"ttl" json:"ttl"`
	MaxSize int           `koanf:"max_size" yaml:"max_size" json:"max_size"`
}

// LLMRateConfig contains LLM rate limiting configuration
type LLMRateConfig struct {
	RequestsPerMinute int `koanf:"requests_per_minute" yaml:"requests_per_minute" json:"requests_per_minute"`
	TokensPerMinute   int `koanf:"tokens_per_minute" yaml:"tokens_per_minute" json:"tokens_per_minute"`
}

// LogConfig contains logging configuration
type LogConfig struct {
	Level  string `koanf:"level" yaml:"level" json:"level"`
	Format string `koanf:"format" yaml:"format" json:"format"`
	File   string `koanf:"file" yaml:"file" json:"file"`
}

// PrometheusConfig contains Prometheus metrics configuration
type PrometheusConfig struct {
	Enabled        bool          `koanf:"enabled" yaml:"enabled" json:"enabled"`
	RemoteWriteURL string        `koanf:"remote_write_url" yaml:"remote_write_url" json:"remote_write_url"`
	PushInterval   time.Duration `koanf:"push_interval" yaml:"push_interval" json:"push_interval"`
	Username       string        `koanf:"username" yaml:"username" json:"username"`
	Password       string        `koanf:"password" yaml:"password" json:"password"`
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
			AutoTLS: AutoTLSConfig{
				Enabled:              false,
				CA:                   "staging",
				CertDir:              "./data/p2p-forge-certs",
				IdentityKeyPath:      "./data/identity.key",
				RegistrationDelaySec: 10,
			},
		},
		HTTP: HTTPConfig{
			Enabled: true,
			Port:    8000,
			Host:    "0.0.0.0",
		},
		MCP: MCPBridgeConfig{
			Enabled: true,
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
		Prometheus: PrometheusConfig{
			Enabled:        false,
			RemoteWriteURL: "",
			PushInterval:   30 * time.Second,
			Username:       "",
			Password:       "",
		},
	}
}
