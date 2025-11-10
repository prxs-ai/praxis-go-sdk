package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/pkg/utils"
	"github.com/sirupsen/logrus"
)

// applyEnvironmentOverrides applies environment variable overrides to the configuration
func applyEnvironmentOverrides(config *AppConfig) {
	// Agent overrides
	applyAgentOverrides(config)

	// P2P overrides
	applyP2POverrides(config)

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
	applyLLMOverrides(config)

	// Logging overrides
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Logging.Level = level
	}

	// Prometheus overrides
	applyPrometheusOverrides(config)
}

func applyAgentOverrides(config *AppConfig) {
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
}

func applyP2POverrides(config *AppConfig) {
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
	config.P2P.AutoTLS.AllowPrivateAddresses = utils.BoolFromEnv(
		"AUTOTLS_ALLOW_PRIVATE_ADDRS",
		config.P2P.AutoTLS.AllowPrivateAddresses,
	)
	config.P2P.AutoTLS.ProduceShortAddrs = utils.BoolFromEnv(
		"AUTOTLS_SHORT_ADDRS",
		config.P2P.AutoTLS.ProduceShortAddrs,
	)

	if config.P2P.AutoTLS.ResolverNetwork == "" && config.P2P.AutoTLS.ResolverAddress != "" {
		config.P2P.AutoTLS.ResolverNetwork = "udp"
	}
}

func applyLLMOverrides(config *AppConfig) {
	config.LLM.Enabled = utils.BoolFromEnv("LLM_ENABLED", config.LLM.Enabled)
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config.LLM.APIKey = apiKey
	}
	if model := os.Getenv("LLM_MODEL"); model != "" {
		config.LLM.Model = model
	}
}

func applyPrometheusOverrides(config *AppConfig) {
	config.Prometheus.Enabled = utils.BoolFromEnv("PROMETHEUS_ENABLED", config.Prometheus.Enabled)
	if remoteWriteURL := os.Getenv("PROMETHEUS_REMOTE_WRITE_URL"); remoteWriteURL != "" {
		config.Prometheus.RemoteWriteURL = remoteWriteURL
	}
	if pushIntervalStr := os.Getenv("PROMETHEUS_PUSH_INTERVAL"); pushIntervalStr != "" {
		if duration, err := time.ParseDuration(pushIntervalStr); err == nil {
			config.Prometheus.PushInterval = duration
		} else {
			logrus.Warnf("Invalid PROMETHEUS_PUSH_INTERVAL: %s", pushIntervalStr)
		}
	}
	if username := os.Getenv("PROMETHEUS_USERNAME"); username != "" {
		config.Prometheus.Username = username
	}
	if password := os.Getenv("PROMETHEUS_PASSWORD"); password != "" {
		config.Prometheus.Password = password
	}
}
