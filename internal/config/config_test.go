package config

import (
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigOverrideRules(t *testing.T) {
	// Clean environment before tests
	cleanEnvVars()
	defer cleanEnvVars()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests

	t.Run("defaults only - no config file, no env vars", func(t *testing.T) {
		cleanEnvVars()

		// Set required API key to pass validation
		os.Setenv("OPENAI_API_KEY", "open-api-key")

		// Use a non-existent config file path
		config, err := LoadConfig("/non/existent/config.yaml", logger)
		require.NoError(t, err)

		// Verify against default config
		defaultConfig := DefaultConfig()
		assert.Equal(t, defaultConfig.Agent.Name, config.Agent.Name)
		assert.Equal(t, defaultConfig.Agent.Version, config.Agent.Version)
		assert.Equal(t, defaultConfig.Agent.URL, config.Agent.URL)
		assert.Equal(t, defaultConfig.HTTP.Port, config.HTTP.Port)
		assert.Equal(t, defaultConfig.HTTP.Host, config.HTTP.Host)
		assert.Equal(t, defaultConfig.P2P.Enabled, config.P2P.Enabled)
		assert.Equal(t, defaultConfig.P2P.Port, config.P2P.Port)
		assert.Equal(t, defaultConfig.P2P.Rendezvous, config.P2P.Rendezvous)
		assert.Equal(t, defaultConfig.Logging.Level, config.Logging.Level)
		assert.Equal(t, defaultConfig.Logging.Format, config.Logging.Format)
		assert.Equal(t, "open-api-key", config.LLM.APIKey)
	})

	t.Run("config file overrides defaults", func(t *testing.T) {
		cleanEnvVars()

		// Set required API key to pass validation
		os.Setenv("OPENAI_API_KEY", "open-api-key")

		config, err := LoadConfig("test-config.yaml", logger)
		require.NoError(t, err)
		defaultConfig := DefaultConfig()

		// Verify config file values override defaults
		assert.Equal(t, "test-agent-from-config", config.Agent.Name)
		assert.NotEqual(t, defaultConfig.Agent.Name, config.Agent.Name)

		assert.Equal(t, "1.0.1", config.Agent.Version)
		assert.NotEqual(t, defaultConfig.Agent.Version, config.Agent.Version)

		assert.Equal(t, "http://praxis-agent-1:9000", config.Agent.URL)
		assert.NotEqual(t, defaultConfig.Agent.URL, config.Agent.URL)

		assert.Equal(t, 9000, config.HTTP.Port)
		assert.NotEqual(t, defaultConfig.HTTP.Port, config.HTTP.Port)

		assert.Equal(t, "127.0.0.1", config.HTTP.Host)
		assert.NotEqual(t, defaultConfig.HTTP.Host, config.HTTP.Host)

		assert.Equal(t, 6003, config.P2P.Port)
		assert.NotEqual(t, defaultConfig.P2P.Port, config.P2P.Port)

		assert.Equal(t, "test-praxis-agents", config.P2P.Rendezvous)
		assert.NotEqual(t, defaultConfig.P2P.Rendezvous, config.P2P.Rendezvous)

		assert.Equal(t, "debug", config.Logging.Level)
		assert.NotEqual(t, defaultConfig.Logging.Level, config.Logging.Level)

		assert.Equal(t, "json", config.Logging.Format)
		assert.NotEqual(t, defaultConfig.Logging.Format, config.Logging.Format)

		assert.Equal(t, "open-api-key", config.LLM.APIKey)

		// Verify unchanged defaults
		assert.Equal(t, defaultConfig.P2P.Enabled, config.P2P.Enabled) // Not overridden in config
	})

	t.Run("koanf environment variables override config file and defaults", func(t *testing.T) {
		cleanEnvVars()

		// Set required API key to pass validation
		os.Setenv("OPENAI_API_KEY", "open-api-key")

		// Set koanf-style environment variables (dot notation)
		os.Setenv("HTTP_ENABLED", "false")
		os.Setenv("HTTP_PORT", "7000")

		os.Setenv("MCP_ENABLED", "true")

		os.Setenv("LOG_LEVEL", "error")

		os.Setenv("AGENT_NAME", "koanf-env-agent")
		os.Setenv("AGENT_VERSION", "3.0.0")
		os.Setenv("AGENT_URL", "http://koanf-env-agent:7000")
		os.Setenv("AGENT_DESCRIPTION", "Some description")

		os.Setenv("P2P_ENABLED", "false")
		os.Setenv("P2P_PORT", "7003")
		os.Setenv("INSECURE_P2P", "false")
		os.Setenv("NAT_PORTMAP_ENABLED", "false")
		os.Setenv("P2P_ADVERTISE_ADDRS", "/ip4/124.68.105.199/tcp/6003")

		os.Setenv("AUTOTLS_ENABLED", "false")
		os.Setenv("AUTOTLS_CA", "https://acme-staging-v02.api.letsencrypt.org/sub-directory")
		os.Setenv("AUTOTLS_CERT_DIR", "./data/test-agent1/p2p-forge-certs")
		os.Setenv("AUTOTLS_IDENTITY_KEY", "./data/test-agent1/identity.key")
		os.Setenv("AUTOTLS_FORGE_DOMAIN", "test.libp2p.direct")
		os.Setenv("AUTOTLS_REGISTRATION_ENDPOINT", "https://test.registration.libp2p.direct")
		os.Setenv("AUTOTLS_FORGE_AUTH_TOKEN", "123")
		os.Setenv("AUTOTLS_TRUSTED_ROOTS_FILE", "file")
		os.Setenv("AUTOTLS_RESOLVER_ADDR", "127.0.0.1:53")
		os.Setenv("AUTOTLS_RESOLVER_NET", "tcp")
		os.Setenv("AUTOTLS_REGISTRATION_DELAY_SEC", "15")
		os.Setenv("AUTOTLS_ALLOW_PRIVATE_ADDRS", "false")

		os.Setenv("LLM_ENABLED", "true")
		os.Setenv("LLM_MODEL", "llama3.1")

		os.Setenv("PROMETHEUS_ENABLED", "true")
		os.Setenv("PROMETHEUS_REMOTE_WRITE_URL", "127.0.0.1")
		os.Setenv("PROMETHEUS_PUSH_INTERVAL", "10s")
		os.Setenv("PROMETHEUS_USERNAME", "user")
		os.Setenv("PROMETHEUS_PASSWORD", "password")

		config, err := LoadConfig("test-config.yaml", logger)
		require.NoError(t, err)

		// Verify koanf environment variables override both config and defaults
		assert.Equal(t, false, config.HTTP.Enabled)
		assert.Equal(t, 7000, config.HTTP.Port)

		assert.Equal(t, true, config.MCP.Enabled)

		assert.Equal(t, "error", config.Logging.Level)

		assert.Equal(t, "koanf-env-agent", config.Agent.Name)
		assert.Equal(t, "3.0.0", config.Agent.Version)
		assert.Equal(t, "http://koanf-env-agent:7000", config.Agent.URL)
		assert.Equal(t, "Some description", config.Agent.Description)

		assert.Equal(t, 7003, config.P2P.Port)
		assert.Equal(t, false, config.P2P.Enabled)
		assert.Equal(t, false, config.P2P.Secure)
		assert.Equal(t, false, config.P2P.EnableNATPortMap)
		assert.Equal(t, []string{"/ip4/124.68.105.199/tcp/6003"}, config.P2P.AdvertiseAddrs)

		assert.Equal(t, false, config.P2P.AutoTLS.Enabled)
		assert.Equal(t, "https://acme-staging-v02.api.letsencrypt.org/sub-directory", config.P2P.AutoTLS.CA)
		assert.Equal(t, "./data/test-agent1/p2p-forge-certs", config.P2P.AutoTLS.CertDir)
		assert.Equal(t, "./data/test-agent1/identity.key", config.P2P.AutoTLS.IdentityKeyPath)
		assert.Equal(t, "test.libp2p.direct", config.P2P.AutoTLS.ForgeDomain)
		assert.Equal(t, "https://test.registration.libp2p.direct", config.P2P.AutoTLS.RegistrationEndpoint)
		assert.Equal(t, "123", config.P2P.AutoTLS.ForgeAuthToken)
		assert.Equal(t, "file", config.P2P.AutoTLS.TrustedRootsFile)
		assert.Equal(t, "tcp", config.P2P.AutoTLS.ResolverNetwork)
		assert.Equal(t, 15, config.P2P.AutoTLS.RegistrationDelaySec)
		assert.Equal(t, false, config.P2P.AutoTLS.AllowPrivateAddresses)

		assert.Equal(t, true, config.LLM.Enabled)
		assert.Equal(t, "llama3.1", config.LLM.Model)

		assert.Equal(t, true, config.Prometheus.Enabled)
		assert.Equal(t, "127.0.0.1", config.Prometheus.RemoteWriteURL)
		assert.Equal(t, 10*time.Second, config.Prometheus.PushInterval)
		assert.Equal(t, "user", config.Prometheus.Username)
		assert.Equal(t, "password", config.Prometheus.Password)
	})
}

// Helper functions

func cleanEnvVars() {
	for k, v := range envsConvertor() {
		os.Unsetenv(k)
		os.Unsetenv(v)
	}
}
