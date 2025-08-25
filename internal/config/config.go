package config

import (
	"fmt"
	"os"
	"path/filepath"

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
}
