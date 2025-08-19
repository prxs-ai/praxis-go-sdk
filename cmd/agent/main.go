package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"go-p2p-agent/internal/agent"
	"go-p2p-agent/internal/config"
	"go-p2p-agent/pkg/utils"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config/config.yaml", "Path to configuration file")
	logLevel := flag.String("log-level", "", "Log level (debug, info, warn, error)")
	flag.Parse()

	// Create logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Set log level from flag or environment variable
	level := *logLevel
	if level == "" {
		level = utils.GetEnv("LOG_LEVEL", "info")
	}

	logLevelValue, err := logrus.ParseLevel(level)
	if err != nil {
		logger.Warnf("Invalid log level: %s, using 'info'", level)
		logLevelValue = logrus.InfoLevel
	}
	logger.SetLevel(logLevelValue)

	logger.Info("Starting Go P2P Agent...")

	// Load configuration
	logger.Infof("Loading configuration from %s", *configPath)
	appConfig, err := config.LoadConfig(*configPath, logger)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Create agent configuration
	agentConfig := agent.DefaultConfig()
	agentConfig.AppConfig = appConfig

	// Override name from environment variable
	if name := utils.GetEnv("AGENT_NAME", ""); name != "" {
		agentConfig.AppConfig.Agent.Name = name
		agentConfig.Card.Name = name
	}

	// Create agent
	logger.Info("Creating agent...")
	agentInstance, err := agent.NewAgent(agentConfig, logger)
	if err != nil {
		logger.Fatalf("Failed to create agent: %v", err)
	}

	// Start agent
	logger.Info("Starting agent...")
	if err := agentInstance.Start(); err != nil {
		logger.Fatalf("Failed to start agent: %v", err)
	}

	// Create API server
	logger.Info("Creating API server...")
	apiServer := agent.NewAPIServer(agentInstance, &appConfig.HTTP, logger)

	// Start API server
	logger.Info("Starting API server...")
	if err := apiServer.Start(); err != nil {
		logger.Fatalf("Failed to start API server: %v", err)
	}

	// Wait for interrupt signal
	logger.Info("Agent running. Press Ctrl+C to stop.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// Shutdown API server
	logger.Info("Shutting down API server...")
	if err := apiServer.Shutdown(); err != nil {
		logger.Errorf("API server shutdown error: %v", err)
	}

	// Shutdown agent
	logger.Info("Shutting down agent...")
	if err := agentInstance.Shutdown(); err != nil {
		logger.Errorf("Agent shutdown error: %v", err)
	}

	logger.Info("Agent stopped")
}
