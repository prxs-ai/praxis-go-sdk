package utils

import (
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

// LogConfig stores logging configuration
type LogConfig struct {
	Level      string `json:"level" yaml:"level"`
	Format     string `json:"format" yaml:"format"`
	OutputPath string `json:"output_path" yaml:"output_path"`
}

// DefaultLogConfig returns the default logging configuration
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Level:      "info",
		Format:     "text",
		OutputPath: "",
	}
}

// ConfigureLogger sets up a logrus logger based on configuration
func ConfigureLogger(config LogConfig) *logrus.Logger {
	logger := logrus.New()
	
	// Configure log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)
	
	// Configure output format
	if config.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}
	
	// Configure output destination
	if config.OutputPath != "" {
		file, err := os.OpenFile(config.OutputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			// Write to both file and stdout
			mw := io.MultiWriter(os.Stdout, file)
			logger.SetOutput(mw)
		}
	}
	
	return logger
}