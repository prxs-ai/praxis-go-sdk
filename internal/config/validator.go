package config

import (
	"fmt"
	"os"
	"strings"
)

// validateConfig checks if the configuration is valid
func validateConfig(config *AppConfig) error {
	// Agent config validation
	if err := validateAgentConfig(config); err != nil {
		return err
	}

	// P2P config validation
	if err := validateP2PConfig(config); err != nil {
		return err
	}

	// LLM config validation
	if err := validateLLMConfig(config); err != nil {
		return err
	}

	// MCP validation
	if err := validateMCPConfig(config); err != nil {
		return err
	}

	return nil
}

func validateAgentConfig(config *AppConfig) error {
	if config.Agent.Name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	if config.Agent.Security.SignCards ||
		config.Agent.Security.VerifyPeerCards ||
		config.Agent.Security.SignA2A ||
		config.Agent.Security.VerifyA2A {
		if config.Agent.Identity.DID == "" {
			return fmt.Errorf("agent identity DID must be configured when security features are enabled")
		}
	}

	return nil
}

func validateP2PConfig(config *AppConfig) error {
	if !config.P2P.Enabled {
		return nil
	}

	if config.P2P.Rendezvous == "" {
		return fmt.Errorf("rendezvous string cannot be empty when P2P is enabled")
	}

	if !config.P2P.AutoTLS.Enabled {
		return nil
	}

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
		if !strings.HasPrefix(config.P2P.AutoTLS.CA, "http://") &&
			!strings.HasPrefix(config.P2P.AutoTLS.CA, "https://") {
			return fmt.Errorf("p2p.autotls.ca must be 'staging', 'production', or a custom HTTPS endpoint")
		}
	}
	if config.P2P.AutoTLS.RegistrationDelaySec < 0 {
		return fmt.Errorf("p2p.autotls.registration_delay_sec can not be negative")
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

	return nil
}

func validateLLMConfig(config *AppConfig) error {
	if !config.LLM.Enabled {
		return nil
	}

	if config.LLM.Provider == "" {
		return fmt.Errorf("LLM provider cannot be empty when LLM is enabled")
	}
	if config.LLM.Provider == "openai" && config.LLM.APIKey == "" {
		return fmt.Errorf("OpenAI API key cannot be empty when using OpenAI provider")
	}

	return nil
}

func validateMCPConfig(config *AppConfig) error {
	if !config.MCP.Enabled {
		return nil
	}

	for _, server := range config.MCP.Servers {
		if err := validateMCPServer(&server); err != nil {
			return err
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
