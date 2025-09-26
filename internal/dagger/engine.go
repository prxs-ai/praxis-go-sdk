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

	// Optional fixed env map
	envMap, _ := toStringMap(spec["env"]) // ignore error; treat non-map as empty

	// Optional passthrough env list (names to forward from host env)
	envPassthrough, _ := toStringSlice(spec["env_passthrough"]) // ignore error; treat non-slice as empty

	// Don't append args to command since we're passing them as env variables
	// This allows shell substitution like $username to work properly
	finalCommand := make([]string, len(command))
	copy(finalCommand, command)

	container := e.client.Container().From(image)
	for hostPath, containerPath := range mounts {
		absPath, err := filepath.Abs(hostPath)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path for %s: %w", hostPath, err)
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return "", fmt.Errorf("host directory does not exist: %s", absPath)
		}

		dir := e.client.Host().Directory(absPath)
		container = container.WithDirectory(containerPath, dir)
	}

	// Apply fixed env variables
	for k, v := range envMap {
		if k == "" {
			continue
		}
		container = container.WithEnvVariable(k, v)
	}

	// Apply args as environment variables (for shell substitution)
	for key, val := range args {
		container = container.WithEnvVariable(key, fmt.Sprintf("%v", val))
	}

	// Add timestamp to prevent Dagger caching
	container = container.WithEnvVariable("CACHE_BUST", fmt.Sprintf("%d", time.Now().UnixNano()))

	// Apply passthrough env variables from the host process environment
	for _, name := range envPassthrough {
		if name == "" {
			continue
		}
		if val := os.Getenv(name); val != "" {
			container = container.WithEnvVariable(name, val)
		}
	}

	execContainer := container.WithExec(finalCommand)
	result, err := execContainer.Stdout(ctx)
	if err != nil {
		stderr, stderrErr := execContainer.Stderr(ctx)
		if stderrErr == nil && stderr != "" {
			return "", fmt.Errorf("dagger execution failed: %s", stderr)
		}
		return "", fmt.Errorf("dagger execution failed: %w", err)
	}

	// Export modified directories back to host
	for hostPath, containerPath := range mounts {
		absPath, _ := filepath.Abs(hostPath)
		// Export the directory from container back to host
		if _, err := execContainer.Directory(containerPath).Export(ctx, absPath); err != nil {
			// Log warning but don't fail - the directory might not have changed
			fmt.Printf("Warning: Could not export %s back to host: %v\n", containerPath, err)
		}
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
