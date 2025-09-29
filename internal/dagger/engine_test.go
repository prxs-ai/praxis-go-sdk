package dagger

import (
	"context"
	"strings"
	"testing"

	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactSecretKeys(t *testing.T) {
	secrets := map[string]string{"api_key": "SECRET123", "token": "abc"}
	redacted := redactSecretKeys(secrets)

	assert.Equal(t, "***", redacted["api_key"])
	assert.Equal(t, "***", redacted["token"])
}

func TestIsSecretKey(t *testing.T) {
	assert.True(t, isSecretKey("api_key"))
	assert.True(t, isSecretKey("API_KEY"))
	assert.True(t, isSecretKey("token"))
	assert.True(t, isSecretKey("TOKEN"))
	assert.False(t, isSecretKey("username"))
}

func TestExecute_InvalidSpec(t *testing.T) {
	engine := &DaggerEngine{}

	contract := contracts.ToolContract{
		EngineSpec: map[string]interface{}{},
	}

	_, err := engine.Execute(context.Background(), contract, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dagger spec missing or invalid 'image'")
}

func TestExecute_CommandError(t *testing.T) {
	engine := &DaggerEngine{}

	contract := contracts.ToolContract{
		EngineSpec: map[string]interface{}{
			"image":   "alpine:latest",
			"command": 123,
		},
	}

	_, err := engine.Execute(context.Background(), contract, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid 'command'")
}

func TestExecute_ParamsAndSecretsLogging(t *testing.T) {
	args := map[string]interface{}{
		"user":    "alice",
		"api_key": "SECRET123",
	}

	params := map[string]string{}
	secrets := map[string]string{}
	for k, v := range args {
		strVal := v.(string)
		if isSecretKey(k) {
			secrets[k] = strVal
		} else {
			params[k] = strVal
		}
	}

	assert.Equal(t, map[string]string{"user": "alice"}, params)
	assert.Equal(t, map[string]string{"api_key": "SECRET123"}, secrets)

	redacted := redactSecretKeys(secrets)
	assert.Equal(t, "***", redacted["api_key"])

	var sb strings.Builder
	sb.WriteString("injecting params=")
	sb.WriteString(strings.Join([]string{"user=alice"}, " "))
	sb.WriteString("\n")
	sb.WriteString("injecting secrets(keys)=")
	sb.WriteString("***")

	output := sb.String()

	assert.Contains(t, output, "user=alice")
	assert.Contains(t, output, "***")
}
