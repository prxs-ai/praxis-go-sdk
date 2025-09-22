package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/praxis/praxis-go-sdk/internal/agent"
	"github.com/praxis/praxis-go-sdk/internal/config"
	"github.com/sirupsen/logrus"
)

func main() {
	// Parse command line flags
	configFile := flag.String("config", "configs/agent.yaml", "Path to configuration file")
	flag.Parse()

	// Initialize basic logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	logger.Info("Starting Praxis Agent")

	// Load configuration from file and environment
	appConfig, err := config.LoadConfig(*configFile, logger)
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	// Convert AppConfig to agent.Config
	agentConfig := agent.AdaptAppConfigToAgentConfig(appConfig)

	logger.Infof("Configuration loaded from %s: %+v", *configFile, agentConfig)

	praxisAgent, err := agent.NewPraxisAgent(agentConfig)
	if err != nil {
		logger.Fatalf("Failed to create agent: %v", err)
	}

	if err := praxisAgent.Start(); err != nil {
		logger.Fatalf("Failed to start agent: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("Received shutdown signal")

	if err := praxisAgent.Stop(); err != nil {
		logger.Errorf("Error stopping agent: %v", err)
	}

	logger.Info("Agent shutdown complete")
}

func init() {
	printBanner()
}

func printBanner() {
	banner := `
╔══════════════════════════════════════════╗
║       PRAXIS P2P AGENT                   ║
║                                          ║
╚══════════════════════════════════════════╝
`
	fmt.Println(banner)
}
