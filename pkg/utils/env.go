package utils

import (
	"log"
	"os"
	"strings"
)

// GetEnv retrieves an environment variable or returns a default value if not set
func GetEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// ExpandEnvVars expands environment variables in a string
// Similar to os.ExpandEnv but with additional logging for sensitive values
func ExpandEnvVars(s string) string {
	expanded := os.ExpandEnv(s)
	
	// Special case for API keys to avoid logging full key
	if strings.Contains(s, "${OPENAI_API_KEY}") {
		originalKey := "${OPENAI_API_KEY}"
		envKey := os.Getenv("OPENAI_API_KEY")
		if len(envKey) > 0 {
			log.Printf("🔑 [DEBUG] API Key substitution: %s -> %s (first 20 chars)", 
				originalKey, envKey[:Min(20, len(envKey))])
		}
	}
	return expanded
}

// Min returns the smaller of two integers
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BoolFromEnv converts an environment variable to a boolean
// "true", "yes", "1", "on" are considered true (case-insensitive)
// Any other value is considered false
func BoolFromEnv(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	
	val = strings.ToLower(val)
	return val == "true" || val == "yes" || val == "1" || val == "on"
}