package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/praxis/praxis-go-sdk/pkg/utils"
)

// LoadConfig loads configuration from a YAML file
// If the file doesn't exist, it returns the default configuration
func LoadConfig(path string, logger *logrus.Logger) (*AppConfig, error) {
	// Start with default configuration
	config := DefaultConfig()

	// Check if the config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Warnf("Configuration file %s not found, using defaults", path)
		// Still apply environment overrides even with defaults
		applyEnvironmentOverrides(config)
		return config, nil
	}

	// Read the configuration file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in the configuration
	configString := utils.ExpandEnvVars(string(data))

	// Parse YAML
	if err := yaml.Unmarshal([]byte(configString), config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate the configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Override with environment variables
	applyEnvironmentOverrides(config)

	return config, nil
}

// SaveConfig saves the configuration to a YAML file
func SaveConfig(config *AppConfig, path string) error {
	// Create the directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validateConfig checks if the configuration is valid
func validateConfig(config *AppConfig) error {
	// Basic validation
	if config.Agent.Name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	// P2P validation
	if config.P2P.Enabled && config.P2P.Rendezvous == "" {
		return fmt.Errorf("rendezvous string cannot be empty when P2P is enabled")
	}

	if config.P2P.AutoTLS.Enabled {
		if config.P2P.Port <= 0 {
			return fmt.Errorf("p2p.port must be a fixed, non-zero value when AutoTLS is enabled")
		}
		if config.P2P.AutoTLS.IdentityKeyPath == "" {
			return fmt.Errorf("p2p.autotls.identity_key must be set when AutoTLS is enabled")
		}
		if config.P2P.AutoTLS.CertDir == "" {
			return fmt.Errorf("p2p.autotls.cert_dir must be set when AutoTLS is enabled")
		}
		switch strings.ToLower(config.P2P.AutoTLS.CA) {
		case "", "staging", "production":
			if config.P2P.AutoTLS.CA == "" {
				config.P2P.AutoTLS.CA = "staging"
			}
		default:
			if !strings.HasPrefix(config.P2P.AutoTLS.CA, "http://") && !strings.HasPrefix(config.P2P.AutoTLS.CA, "https://") {
				return fmt.Errorf("p2p.autotls.ca must be 'staging', 'production', or a custom HTTPS endpoint")
			}
		}
		if config.P2P.AutoTLS.RegistrationDelaySec < 0 {
			return fmt.Errorf("p2p.autotls.registration_delay_sec не может быть отрицательным")
		}
		if config.P2P.AutoTLS.ForgeDomain != "" && config.P2P.AutoTLS.RegistrationEndpoint == "" {
			return fmt.Errorf("p2p.autotls.registration_endpoint must be set when forge_domain is provided")
		}
		if config.P2P.AutoTLS.TrustedRootsFile != "" {
			if _, err := os.Stat(config.P2P.AutoTLS.TrustedRootsFile); err != nil {
				return fmt.Errorf("p2p.autotls.trusted_roots_file is not accessible: %w", err)
			}
		}
		if config.P2P.AutoTLS.ResolverNetwork == "" && config.P2P.AutoTLS.ResolverAddress != "" {
			config.P2P.AutoTLS.ResolverNetwork = "udp"
		}
	}

	// LLM validation
	if config.LLM.Enabled {
		if config.LLM.Provider == "" {
			return fmt.Errorf("LLM provider cannot be empty when LLM is enabled")
		}
		if config.LLM.Provider == "openai" && config.LLM.APIKey == "" {
			return fmt.Errorf("OpenAI API key cannot be empty when using OpenAI provider")
		}
	}

	// MCP validation
	if config.MCP.Enabled {
		for _, server := range config.MCP.Servers {
			if err := validateMCPServer(&server); err != nil {
				return err
			}
		}
	}

	if config.Agent.Security.SignCards || config.Agent.Security.VerifyPeerCards || config.Agent.Security.SignA2A || config.Agent.Security.VerifyA2A {
		if config.Agent.Identity.DID == "" {
			return fmt.Errorf("agent identity DID must be configured when security features are enabled")
		}
	}

	return nil
}

// validateMCPServer validates an MCP server configuration
func validateMCPServer(server *MCPServerConfig) error {
	if server.Name == "" {
		return fmt.Errorf("MCP server name cannot be empty")
	}

	if server.Transport != "stdio" && server.Transport != "sse" {
		return fmt.Errorf("MCP server transport must be 'stdio' or 'sse', got '%s'", server.Transport)
	}

	if server.Transport == "stdio" && server.Command == "" {
		return fmt.Errorf("command is required for stdio transport in MCP server '%s'", server.Name)
	}

	if server.Transport == "sse" && server.URL == "" {
		return fmt.Errorf("URL is required for sse transport in MCP server '%s'", server.Name)
	}

	return nil
}

// applyEnvironmentOverrides applies environment variable overrides to the configuration
func applyEnvironmentOverrides(config *AppConfig) {
	// Agent overrides
	if name := os.Getenv("AGENT_NAME"); name != "" {
		config.Agent.Name = name
	}
	if version := os.Getenv("AGENT_VERSION"); version != "" {
		config.Agent.Version = version
	}
	if desc := os.Getenv("AGENT_DESCRIPTION"); desc != "" {
		config.Agent.Description = desc
	}
	if url := os.Getenv("AGENT_URL"); url != "" {
		config.Agent.URL = url
	}

	// P2P overrides
	config.P2P.Enabled = utils.BoolFromEnv("P2P_ENABLED", config.P2P.Enabled)
	if portStr := os.Getenv("P2P_PORT"); portStr != "" {
		if _, err := fmt.Sscanf(portStr, "%d", &config.P2P.Port); err != nil {
			// Log error but don't fail
			logrus.Warnf("Invalid P2P_PORT: %s", portStr)
		}
	}
	config.P2P.Secure = !utils.BoolFromEnv("INSECURE_P2P", !config.P2P.Secure)
	config.P2P.EnableNATPortMap = utils.BoolFromEnv("NAT_PORTMAP_ENABLED", config.P2P.EnableNATPortMap)
	config.P2P.AutoTLS.Enabled = utils.BoolFromEnv("AUTOTLS_ENABLED", config.P2P.AutoTLS.Enabled)
	if advertise := os.Getenv("P2P_ADVERTISE_ADDRS"); advertise != "" {
		parts := strings.Split(advertise, ",")
		config.P2P.AdvertiseAddrs = config.P2P.AdvertiseAddrs[:0]
		for _, part := range parts {
			addr := strings.TrimSpace(part)
			if addr == "" {
				continue
			}
			config.P2P.AdvertiseAddrs = append(config.P2P.AdvertiseAddrs, addr)
		}
	}
	if ca := os.Getenv("AUTOTLS_CA"); ca != "" {
		config.P2P.AutoTLS.CA = strings.ToLower(ca)
	}
	if dir := os.Getenv("AUTOTLS_CERT_DIR"); dir != "" {
		config.P2P.AutoTLS.CertDir = dir
	}
	if key := os.Getenv("AUTOTLS_IDENTITY_KEY"); key != "" {
		config.P2P.AutoTLS.IdentityKeyPath = key
	}
	if domain := os.Getenv("AUTOTLS_FORGE_DOMAIN"); domain != "" {
		config.P2P.AutoTLS.ForgeDomain = domain
	}
	if endpoint := os.Getenv("AUTOTLS_REGISTRATION_ENDPOINT"); endpoint != "" {
		config.P2P.AutoTLS.RegistrationEndpoint = endpoint
	}
	if token := os.Getenv("AUTOTLS_FORGE_AUTH_TOKEN"); token != "" {
		config.P2P.AutoTLS.ForgeAuthToken = token
	}
	if roots := os.Getenv("AUTOTLS_TRUSTED_ROOTS_FILE"); roots != "" {
		config.P2P.AutoTLS.TrustedRootsFile = roots
	}
	if resolverAddr := os.Getenv("AUTOTLS_RESOLVER_ADDR"); resolverAddr != "" {
		config.P2P.AutoTLS.ResolverAddress = resolverAddr
	}
	if resolverNet := os.Getenv("AUTOTLS_RESOLVER_NET"); resolverNet != "" {
		config.P2P.AutoTLS.ResolverNetwork = resolverNet
	}
	if delay := os.Getenv("AUTOTLS_REGISTRATION_DELAY_SEC"); delay != "" {
		if v, err := strconv.Atoi(delay); err != nil {
			logrus.Warnf("Invalid AUTOTLS_REGISTRATION_DELAY_SEC: %s", delay)
		} else {
			config.P2P.AutoTLS.RegistrationDelaySec = v
		}
	}
	config.P2P.AutoTLS.AllowPrivateAddresses = utils.BoolFromEnv("AUTOTLS_ALLOW_PRIVATE_ADDRS", config.P2P.AutoTLS.AllowPrivateAddresses)
	config.P2P.AutoTLS.ProduceShortAddrs = utils.BoolFromEnv("AUTOTLS_SHORT_ADDRS", config.P2P.AutoTLS.ProduceShortAddrs)

	// HTTP overrides
	config.HTTP.Enabled = utils.BoolFromEnv("HTTP_ENABLED", config.HTTP.Enabled)
	if portStr := os.Getenv("HTTP_PORT"); portStr != "" {
		if _, err := fmt.Sscanf(portStr, "%d", &config.HTTP.Port); err != nil {
			logrus.Warnf("Invalid HTTP_PORT: %s", portStr)
		}
	}

	// MCP overrides
	config.MCP.Enabled = utils.BoolFromEnv("MCP_ENABLED", config.MCP.Enabled)

	// LLM overrides
	config.LLM.Enabled = utils.BoolFromEnv("LLM_ENABLED", config.LLM.Enabled)
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config.LLM.APIKey = apiKey
	}
	if model := os.Getenv("LLM_MODEL"); model != "" {
		config.LLM.Model = model
	}

	// Logging overrides
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Logging.Level = level
	}

	if config.P2P.AutoTLS.ResolverNetwork == "" && config.P2P.AutoTLS.ResolverAddress != "" {
		config.P2P.AutoTLS.ResolverNetwork = "udp"
	}
}
