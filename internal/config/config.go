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
