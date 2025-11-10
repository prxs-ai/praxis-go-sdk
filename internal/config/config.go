package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/sirupsen/logrus"
)

// LoadConfig loads configuration from a YAML file
// If the file doesn't exist, it returns the default configuration
func LoadConfig(path string, logger *logrus.Logger) (*AppConfig, error) {
	// Start with default configuration
	config := DefaultConfig()

	k := koanf.New(".")

	// Check if the config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Warnf("Configuration file %s not found, using defaults", path)
	} else {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	k = applyEnvs(k)

	k.Unmarshal("", &config)

	// Validate the configuration
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// SaveConfig saves the configuration to a YAML file
func SaveConfig(config *AppConfig, path string) error {
	// Create the directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	k := koanf.New(".")

	// Marshal to YAML
	data, err := k.Marshal(yaml.Parser())
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func applyEnvs(k *koanf.Koanf) *koanf.Koanf {
	convertor := envsConvertor()

	k.Load(env.Provider(".", env.Opt{
		TransformFunc: func(k, v string) (string, any) {
			if newK, exists := convertor[k]; exists {
				k = newK
			}

			return k, v
		},
	}), nil)

	return k
}

func envsConvertor() map[string]string {
	return map[string]string{
		"HTTP_ENABLED": "http.enabled",
		"HTTP_PORT":    "http.port",

		"MCP_ENABLED": "mcp.enabled",

		"LOG_LEVEL": "logging.level",

		"AGENT_NAME":        "agent.name",
		"AGENT_VERSION":     "agent.version",
		"AGENT_DESCRIPTION": "agent.description",
		"AGENT_URL":         "agent.url",

		"OPENAI_API_KEY": "llm.api_key",
	}
}
