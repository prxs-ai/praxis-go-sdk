package dagger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"dagger.io/dagger"
	"github.com/praxis/praxis-go-sdk/internal/contracts"
)

type DaggerEngine struct {
	client *dagger.Client
}

func NewEngine(ctx context.Context) (*DaggerEngine, error) {
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stdout))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to dagger: %w", err)
	}
	return &DaggerEngine{client: client}, nil
}

// Close закрывает соединение с Dagger Engine.
func (e *DaggerEngine) Close() {
	e.client.Close()
}

func (e *DaggerEngine) Execute(ctx context.Context, contract contracts.ToolContract, args map[string]interface{}) (string, error) {
	spec := contract.EngineSpec

	image, ok := spec["image"].(string)
	if !ok || image == "" {
		return "", fmt.Errorf("dagger spec missing or invalid 'image' field")
	}

	command, err := toStringSlice(spec["command"])
	if err != nil {
		return "", fmt.Errorf("dagger spec invalid 'command' field: %w", err)
	}

	mounts, err := toStringMap(spec["mounts"])
	if err != nil {
		return "", fmt.Errorf("dagger spec invalid 'mounts' field: %w", err)
	}

	envMap, _ := toStringMap(spec["env"])
	envPassthrough, _ := toStringSlice(spec["env_passthrough"])

	fmt.Printf("⚙️ [Dagger] Preparing container\n")
	fmt.Printf("   image=%s command=%v\n", image, command)
	fmt.Printf("   mounts=%v\n", mounts)

	finalCommand := make([]string, len(command))
	copy(finalCommand, command)

	container := e.client.Container().From(image)

	// Apply mounts
	for hostPath, containerPath := range mounts {
		absPath, err := filepath.Abs(hostPath)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path for %s: %w", hostPath, err)
		}
		dir := e.client.Host().Directory(absPath)
		container = container.WithDirectory(containerPath, dir)
	}

	// Apply fixed env variables
	if len(envMap) > 0 {
		fmt.Printf("   fixed env=%v\n", envMap)
	}
	for k, v := range envMap {
		container = container.WithEnvVariable(k, v)
	}

	// Split args into params vs secrets (simple heuristic: secrets often UPPERCASE or from dsl.Secrets)
	params := map[string]string{}
	secrets := map[string]string{}
	for k, v := range args {
		strVal := fmt.Sprintf("%v", v)
		if isSecretKey(k) {
			secrets[k] = strVal
		} else {
			params[k] = strVal
		}
	}

	if len(params) > 0 {
		fmt.Printf("   injecting params=%v\n", params)
	}
	for k, v := range params {
		container = container.WithEnvVariable(k, v)
	}

	if len(secrets) > 0 {
		fmt.Printf("   injecting secrets(keys)=%v\n", redactSecretKeys(secrets))
	}
	for k, v := range secrets {
		container = container.WithEnvVariable(k, v)
	}

	container = container.WithEnvVariable("CACHE_BUST", fmt.Sprintf("%d", time.Now().UnixNano()))

	// Passthrough env
	if len(envPassthrough) > 0 {
		fmt.Printf("   passthrough env=%v\n", envPassthrough)
	}
	for _, name := range envPassthrough {
		if val := os.Getenv(name); val != "" {
			container = container.WithEnvVariable(name, val)
		}
	}

	fmt.Printf("🚀 [Dagger] Executing %v\n", finalCommand)
	execContainer := container.WithExec(finalCommand)

	result, err := execContainer.Stdout(ctx)
	if err != nil {
		stderr, _ := execContainer.Stderr(ctx)
		return "", fmt.Errorf("dagger execution failed: %s", stderr)
	}

	return result, nil
}

// toStringSlice safely converts interface{} to []string
func toStringSlice(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}

	switch arr := v.(type) {
	case []string:
		return arr, nil
	case []interface{}:
		result := make([]string, len(arr))
		for i, item := range arr {
			if str, ok := item.(string); ok {
				result[i] = str
			} else {
				return nil, fmt.Errorf("element at index %d is not a string", i)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("expected []string or []interface{}, got %T", v)
	}
}

// toStringMap safely converts interface{} to map[string]string
func toStringMap(v interface{}) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}

	switch m := v.(type) {
	case map[string]string:
		return m, nil
	case map[string]interface{}:
		result := make(map[string]string)
		for k, val := range m {
			if str, ok := val.(string); ok {
				result[k] = str
			} else {
				return nil, fmt.Errorf("value for key %s is not a string", k)
			}
		}
		return result, nil
	case map[interface{}]interface{}:
		result := make(map[string]string)
		for k, val := range m {
			key, keyOk := k.(string)
			value, valOk := val.(string)
			if !keyOk {
				return nil, fmt.Errorf("key %v is not a string", k)
			}
			if !valOk {
				return nil, fmt.Errorf("value for key %s is not a string", key)
			}
			result[key] = value
		}
		return result, nil
	default:
		return nil, fmt.Errorf("expected map[string]string, got %T", v)
	}
}

func redactSecretKeys(m map[string]string) map[string]string {
	redacted := make(map[string]string, len(m))
	for k := range m {
		redacted[k] = "***"
	}
	return redacted
}

func isSecretKey(key string) bool {
	return key == "api_key" || key == "API_KEY" || key == "token" || key == "TOKEN"
}
