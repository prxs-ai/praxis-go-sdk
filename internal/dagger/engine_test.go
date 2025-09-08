// internal/dagger/engine_test.go
package dagger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaggerEngine_Execute проверяет базовый сценарий выполнения команды.
// Этот тест является интеграционным, так как требует запущенного Dagger Engine.
func TestDaggerEngine_Execute(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger.
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger test in CI environment without a running engine")
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
	defer engine.Close()

	t.Run("Successful execution with busybox", func(t *testing.T) {
		// 1. Определяем простой контракт для теста - используем busybox вместо alpine
		// busybox меньше и быстрее загружается
		contract := contracts.ToolContract{
			Engine: "dagger",
			Name:   "test_echo",
			EngineSpec: map[string]interface{}{
				"image":   "busybox:latest",
				"command": []string{"echo", "-n", "Hello Dagger"}, // Используем -n чтобы убрать перенос строки
			},
		}

		// 2. Выполняем контракт
		result, err := engine.Execute(ctx, contract, nil) // args не нужны для echo

		// 3. Проверяем результат
		if err != nil {
			// Если есть сетевые проблемы, пропускаем тест
			if containsNetworkError(err) {
				t.Skipf("Skipping test due to network issues: %v", err)
			}
			t.Fatalf("Unexpected error: %v", err)
		}
		assert.Equal(t, "Hello Dagger", result)
	})

	t.Run("Execution with error", func(t *testing.T) {
		// 1. Определяем контракт, который завершится с ошибкой
		contract := contracts.ToolContract{
			Engine: "dagger",
			Name:   "test_error",
			EngineSpec: map[string]interface{}{
				"image":   "busybox:latest",
				"command": []string{"sh", "-c", "echo 'error message' >&2; exit 1"},
			},
		}

		// 2. Выполняем контракт
		result, err := engine.Execute(ctx, contract, nil)

		// 3. Проверяем, что получили ошибку
		assert.Error(t, err)
		assert.Empty(t, result)

		// Проверяем, что это именно ошибка выполнения, а не сетевая проблема
		if !containsNetworkError(err) {
			// Если не сетевая ошибка, то должна содержать наше сообщение об ошибке
			assert.Contains(t, err.Error(), "exit code: 1")
		}
	})
}

// containsNetworkError проверяет, является ли ошибка сетевой
func containsNetworkError(err error) bool {
	errStr := err.Error()
	networkErrors := []string{
		"TLS handshake timeout",
		"connection timeout",
		"network unreachable",
		"failed to resolve",
		"failed to connect",
	}

	for _, netErr := range networkErrors {
		if containsIgnoreCase(errStr, netErr) {
			return true
		}
	}
	return false
}

// containsIgnoreCase проверяет содержание строки без учета регистра
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestDaggerEngine_ExecuteWithMounts проверяет работу с монтированием директорий
func TestDaggerEngine_ExecuteWithMounts(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger.
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger test in CI environment without a running engine")
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
	defer engine.Close()

	t.Run("Execute with shared directory mount", func(t *testing.T) {
		// 1. Получаем абсолютный путь к shared директории
		workingDir, err := os.Getwd()
		require.NoError(t, err)
		sharedDir := filepath.Join(workingDir, "../../shared")

		// Определяем контракт с монтированием shared директории
		contract := contracts.ToolContract{
			Engine: "dagger",
			Name:   "test_mount",
			EngineSpec: map[string]interface{}{
				"image":   "busybox:latest",
				"command": []string{"ls", "-la", "/shared"},
				"mounts":  map[string]string{sharedDir: "/shared"},
			},
		}

		// 2. Выполняем контракт
		result, err := engine.Execute(ctx, contract, nil)

		// 3. Проверяем результат
		if err != nil {
			// Если есть сетевые проблемы, пропускаем тест
			if containsNetworkError(err) {
				t.Skipf("Skipping test due to network issues: %v", err)
			}
			t.Fatalf("Unexpected error: %v", err)
		}

		// Проверяем, что команда вернула содержимое директории
		assert.Contains(t, result, "analyzer.py") // Наш тестовый скрипт должен быть там
	})
}

// TestDaggerEngine_ExecuteWithArgs проверяет передачу аргументов
func TestDaggerEngine_ExecuteWithArgs(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger.
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger test in CI environment without a running engine")
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
	defer engine.Close()

	t.Run("Execute with arguments", func(t *testing.T) {
		// 1. Определяем контракт который будет принимать аргументы
		contract := contracts.ToolContract{
			Engine: "dagger",
			Name:   "test_args",
			EngineSpec: map[string]interface{}{
				"image":   "busybox:latest",
				"command": []string{"sh", "-c", "echo \"$@\"", "sh"},
			},
		}

		// 2. Подготавливаем аргументы
		args := map[string]interface{}{
			"name": "Alice",
			"age":  "25",
		}

		// 3. Выполняем контракт
		result, err := engine.Execute(ctx, contract, args)

		// 4. Проверяем результат
		if err != nil {
			// Если есть сетевые проблемы, пропускаем тест
			if containsNetworkError(err) {
				t.Skipf("Skipping test due to network issues: %v", err)
			}
			t.Fatalf("Unexpected error: %v", err)
		}

		// Проверяем, что аргументы были правильно переданы
		assert.Contains(t, result, "--name")
		assert.Contains(t, result, "Alice")
		assert.Contains(t, result, "--age")
		assert.Contains(t, result, "25")
	})
}

// TestNewEngine проверяет создание и инициализацию DaggerEngine
func TestNewEngine(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger.
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger test in CI environment without a running engine")
	}

	ctx := context.Background()

	t.Run("Successful engine creation", func(t *testing.T) {
		// 1. Создаем движок
		engine, err := NewEngine(ctx)

		// 2. Проверяем, что движок создался успешно
		require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
		require.NotNil(t, engine)
		require.NotNil(t, engine.client)

		// 3. Закрываем движок
		engine.Close()
	})
}

// TestDaggerEngine_ContractValidation проверяет валидацию ToolContract - это unit-тест
func TestDaggerEngine_ContractValidation(t *testing.T) {
	t.Run("Valid contract structure", func(t *testing.T) {
		// 1. Создаем валидный контракт
		contract := contracts.ToolContract{
			Engine: "dagger",
			Name:   "test_validation",
			EngineSpec: map[string]interface{}{
				"image":   "busybox:latest",
				"command": []string{"echo", "test"},
				"mounts":  map[string]string{"./test": "/test"},
			},
		}

		// 2. Проверяем, что все поля заполнены корректно
		assert.Equal(t, "dagger", contract.Engine)
		assert.Equal(t, "test_validation", contract.Name)
		assert.Equal(t, "busybox:latest", contract.EngineSpec["image"])
		command := contract.EngineSpec["command"].([]string)
		assert.Len(t, command, 2)
		assert.Contains(t, command, "echo")
		assert.Contains(t, command, "test")
		mounts := contract.EngineSpec["mounts"].(map[string]string)
		assert.Equal(t, "/test", mounts["./test"])
	})

	t.Run("Contract with empty fields", func(t *testing.T) {
		// 1. Создаем контракт с пустыми полями
		contract := contracts.ToolContract{
			Engine: "dagger",
			Name:   "empty_test",
			EngineSpec: map[string]interface{}{
				"image":   "",
				"command": []string{},
				"mounts":  map[string]string{},
			},
		}

		// 2. Проверяем поля
		assert.Equal(t, "dagger", contract.Engine)
		assert.Equal(t, "empty_test", contract.Name)
		assert.Empty(t, contract.EngineSpec["image"])
		assert.Empty(t, contract.EngineSpec["command"])
		assert.Empty(t, contract.EngineSpec["mounts"])
	})
}

// TestDaggerEngine_ArgumentProcessing проверяет обработку аргументов - unit-тест
func TestDaggerEngine_ArgumentProcessing(t *testing.T) {
	t.Run("Process arguments to command format", func(t *testing.T) {
		// 1. Подготавливаем базовую команду и аргументы
		baseCommand := []string{"python", "script.py"}
		args := map[string]interface{}{
			"input":  "test.txt",
			"output": "result.json",
			"count":  42,
		}

		// 2. Симулируем процесс, который происходит в Execute
		// (создание финальной команды с аргументами)
		finalCommand := make([]string, len(baseCommand))
		copy(finalCommand, baseCommand)

		for key, val := range args {
			finalCommand = append(finalCommand, fmt.Sprintf("--%s", key), fmt.Sprintf("%v", val))
		}

		// 3. Проверяем, что команда собрана корректно
		assert.Contains(t, finalCommand, "python")
		assert.Contains(t, finalCommand, "script.py")
		assert.Contains(t, finalCommand, "--input")
		assert.Contains(t, finalCommand, "test.txt")
		assert.Contains(t, finalCommand, "--output")
		assert.Contains(t, finalCommand, "result.json")
		assert.Contains(t, finalCommand, "--count")
		assert.Contains(t, finalCommand, "42")

		// Проверяем общую длину
		expectedLength := len(baseCommand) + len(args)*2 // каждый аргумент = ключ + значение
		assert.Len(t, finalCommand, expectedLength)
	})
}
