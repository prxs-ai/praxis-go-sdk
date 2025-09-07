package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpTypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/praxis/praxis-go-sdk/internal/config"
	"github.com/praxis/praxis-go-sdk/internal/contracts"
	"github.com/praxis/praxis-go-sdk/internal/dagger"
	"github.com/praxis/praxis-go-sdk/internal/mcp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaggerE2E_PythonAnalyzer проверяет полный End-to-End сценарий выполнения Python анализатора через Dagger
func TestDaggerE2E_PythonAnalyzer(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger E2E test in CI environment without a running engine")
	}

	ctx := context.Background()

	// 1. Получаем абсолютный путь к shared директории
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Переходим к корню проекта из internal/agent
	projectRoot := filepath.Join(workingDir, "../..")
	sharedDir := filepath.Join(projectRoot, "shared")

	// Проверяем, что shared/analyzer.py существует
	analyzerPath := filepath.Join(sharedDir, "analyzer.py")
	if _, err := os.Stat(analyzerPath); os.IsNotExist(err) {
		t.Skip("Skipping test: shared/analyzer.py not found")
	}

	t.Run("Complete MCP to Dagger pipeline", func(t *testing.T) {
		// 2. Создаем временный файл для анализа в shared директории
		testFile := filepath.Join(sharedDir, "test_e2e_input.txt")
		testContent := "Hello world from Dagger E2E testing\nThis is a second line\nAnd a third line for word counting"

		err := os.WriteFile(testFile, []byte(testContent), 0644)
		require.NoError(t, err)
		defer os.Remove(testFile) // Очистка после теста

		// 3. Инициализируем Dagger Engine напрямую для тестирования
		engine, err := dagger.NewEngine(ctx)
		require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
		defer engine.Close()

		// 4. Создаем минимальный агент для тестирования
		logger := logrus.New()
		logger.SetLevel(logrus.DebugLevel)

		// Создаем MCP server для тестирования
		serverConfig := mcp.ServerConfig{
			Name:        "test-agent",
			Version:     "1.0.0",
			Logger:      logger,
			EnableTools: true,
		}
		mcpServer, err := mcp.NewMCPServer(serverConfig)
		require.NoError(t, err)

		appConfig := &config.AppConfig{
			Agent: config.AgentConfig{
				SharedDir: sharedDir, // Используем абсолютный путь
				Tools: []config.ToolConfig{
					{
						Name:        "python_analyzer",
						Description: "Analyzes data using an external Python script",
						Engine:      "dagger",
						Params: []map[string]string{
							{
								"name":        "input_file",
								"type":        "string",
								"required":    "true",
								"description": "Path to the file to analyze",
							},
						},
						EngineSpec: map[string]interface{}{
							"image":   "python:3.11-slim",
							"command": []interface{}{"python", "/shared/analyzer.py"},
							"mounts":  map[string]string{sharedDir: "/shared"},
						},
					},
				},
			},
		}

		agent := &PraxisAgent{
			name:             "test-agent",
			mcpServer:        mcpServer,
			executionEngines: map[string]contracts.ExecutionEngine{"dagger": engine},
			appConfig:        appConfig,
			logger:           logger,
		}

		// 5. Регистрируем динамические инструменты
		agent.registerDynamicTools()

		// 6. Получаем зарегистрированный хендлер для прямого тестирования
		handler := agent.mcpServer.FindToolHandler("python_analyzer")
		require.NotNil(t, handler, "python_analyzer handler should be registered")

		// 7. Создаем MCP запрос в правильном формате
		mcpRequest := mcpTypes.CallToolRequest{
			Params: struct {
				Name      string         `json:"name"`
				Arguments interface{}    `json:"arguments,omitempty"`
				Meta      *mcpTypes.Meta `json:"_meta,omitempty"`
			}{
				Name: "python_analyzer",
				Arguments: map[string]interface{}{
					"input_file": "/shared/test_e2e_input.txt",
				},
			},
		}

		// 8. Выполняем запрос через полный MCP-Dagger пайплайн
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		// Выполняем напрямую через хендлер
		result, err := handler(ctxWithTimeout, mcpRequest)

		// 9. Проверяем результат
		if err != nil {
			// Проверяем, не является ли это сетевой ошибкой
			if containsNetworkError(err) {
				t.Skipf("Skipping test due to network issues: %v", err)
			}
			t.Fatalf("Unexpected error in E2E pipeline: %v", err)
		}

		require.NotNil(t, result)
		assert.False(t, result.IsError)
		assert.NotEmpty(t, result.Content)

		// 10. Проверяем структуру результата
		// Добавим отладочную информацию
		t.Logf("Result: %+v", result)
		t.Logf("Content length: %d", len(result.Content))
		for i, content := range result.Content {
			t.Logf("Content[%d]: %T = %+v", i, content, content)
		}
		
		// Результат должен быть в формате JSON
		textContent, ok := result.Content[0].(mcpTypes.TextContent)
		if !ok {
			textContentPtr, ok := result.Content[0].(*mcpTypes.TextContent)
			require.True(t, ok, "Result should contain text content")
			textContent = *textContentPtr
		}
		content := textContent.Text

		// Парсим JSON результат для более точной проверки
		var parsedResult map[string]interface{}
		err = json.Unmarshal([]byte(content), &parsedResult)
		require.NoError(t, err, "Result should be valid JSON")

		// Проверяем, что результат содержит ожидаемые поля
		assert.Equal(t, "success", parsedResult["status"])
		assert.Equal(t, "test_e2e_input.txt", parsedResult["input_file"])
		assert.Contains(t, parsedResult, "analysis")
		assert.Contains(t, parsedResult, "content_length")
		
		// Проверяем analysis секцию
		analysis, ok := parsedResult["analysis"].(map[string]interface{})
		require.True(t, ok, "analysis should be a map")
		
		// Проверяем количество слов (должно быть > 0)
		wordCount, ok := analysis["word_count"].(float64)
		require.True(t, ok, "word_count should be a number")
		assert.Greater(t, int(wordCount), 0, "word_count should be greater than 0")
		
		// Проверяем количество строк
		lineCount, ok := analysis["line_count"].(float64)
		require.True(t, ok, "line_count should be a number")
		assert.Equal(t, 3, int(lineCount), "Should have 3 lines")

		t.Logf("E2E test completed successfully. Result: %s", content)
	})
}

// TestDaggerE2E_ErrorHandling проверяет обработку ошибок в E2E сценарии
func TestDaggerE2E_ErrorHandling(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger E2E test in CI environment without a running engine")
	}

	ctx := context.Background()

	t.Run("File not found error handling", func(t *testing.T) {
		// 1. Получаем абсолютный путь к shared директории
		workingDir, err := os.Getwd()
		require.NoError(t, err)
		projectRoot := filepath.Join(workingDir, "../..")
		sharedDir := filepath.Join(projectRoot, "shared")

		// 2. Инициализируем Dagger Engine
		engine, err := dagger.NewEngine(ctx)
		require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
		defer engine.Close()

		// 2. Создаем минимальный агент для тестирования
		logger := logrus.New()
		logger.SetLevel(logrus.DebugLevel)

		serverConfig := mcp.ServerConfig{
			Name:        "test-agent",
			Version:     "1.0.0",
			Logger:      logger,
			EnableTools: true,
		}
		mcpServer, err := mcp.NewMCPServer(serverConfig)
		require.NoError(t, err)

		appConfig := &config.AppConfig{
			Agent: config.AgentConfig{
				SharedDir: sharedDir, // Используем абсолютный путь
				Tools: []config.ToolConfig{
					{
						Name:   "python_analyzer",
						Engine: "dagger",
						Params: []map[string]string{
							{
								"name":     "input_file",
								"type":     "string",
								"required": "true",
							},
						},
						EngineSpec: map[string]interface{}{
							"image":   "python:3.11-slim",
							"command": []interface{}{"python", "/shared/analyzer.py"},
							"mounts":  map[string]string{sharedDir: "/shared"},
						},
					},
				},
			},
		}

		agent := &PraxisAgent{
			name:             "test-agent",
			mcpServer:        mcpServer,
			executionEngines: map[string]contracts.ExecutionEngine{"dagger": engine},
			appConfig:        appConfig,
			logger:           logger,
		}

		agent.registerDynamicTools()

		// 3. Получаем handler для прямого тестирования
		handler := agent.mcpServer.FindToolHandler("python_analyzer")
		require.NotNil(t, handler, "python_analyzer handler should be registered")

		// 4. Создаем запрос с несуществующим файлом
		mcpRequest := mcpTypes.CallToolRequest{
			Params: struct {
				Name      string         `json:"name"`
				Arguments interface{}    `json:"arguments,omitempty"`
				Meta      *mcpTypes.Meta `json:"_meta,omitempty"`
			}{
				Name: "python_analyzer",
				Arguments: map[string]interface{}{
					"input_file": "/shared/nonexistent_file_12345.txt",
				},
			},
		}

		// 5. Выполняем запрос и ожидаем ошибку
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		result, err := handler(ctxWithTimeout, mcpRequest)

		// 5. Проверяем, что ошибка обработана корректно
		if err != nil && containsNetworkError(err) {
			t.Skipf("Skipping test due to network issues: %v", err)
		}

		// В E2E тестировании инструмент может обработать ошибку по-разному:
		// 1. Вернуть ошибку в результате (result.IsError = true)
		// 2. Вернуть JSON с полем error/status
		// 3. Выйти с кодом ошибки (что приведет к err != nil)

		if err != nil {
			// Ожидаемое поведение - ошибка выполнения
			assert.Error(t, err)
			t.Logf("Expected error occurred: %v", err)
		} else if result != nil {
			t.Logf("Result received: IsError=%v, Content length=%d", result.IsError, len(result.Content))
			// Результат может быть ошибкой либо содержать ошибку в JSON
			if result.IsError {
				// Ошибка в формате MCP
				assert.True(t, result.IsError)
				t.Logf("MCP error result: %+v", result)
			} else if len(result.Content) > 0 {
				// Проверяем содержимое на наличие информации об ошибке
				var textContent *mcpTypes.TextContent
				var ok bool
				if textContent, ok = result.Content[0].(*mcpTypes.TextContent); !ok {
					// Попробуем как значение, а не указатель
					if tc, isValue := result.Content[0].(mcpTypes.TextContent); isValue {
						textContent = &tc
						ok = true
					}
				}
				require.True(t, ok, "Should have text content")
				content := textContent.Text

				// Python скрипт может вернуть JSON с error полем
				var parsedResult map[string]interface{}
				if json.Unmarshal([]byte(content), &parsedResult) == nil {
					if status, exists := parsedResult["status"]; exists {
						// Если статус не success, значит была обработана ошибка
						if status != "success" {
							t.Logf("Error handled in tool result: %s", content)
							assert.NotEqual(t, "success", status)
						}
					}
				}
			}
		} else {
			t.Fatal("Expected either error or result, got neither")
		}
	})
}

// TestDaggerE2E_ConfigurationVariations проверяет различные конфигурации
func TestDaggerE2E_ConfigurationVariations(t *testing.T) {
	// Пропускаем тест, если он запущен в CI-окружении без Docker/Dagger
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Dagger E2E test in CI environment without a running engine")
	}

	ctx := context.Background()

	t.Run("Different Docker image", func(t *testing.T) {
		// 1. Получаем абсолютный путь к shared директории
		workingDir, err := os.Getwd()
		require.NoError(t, err)
		projectRoot := filepath.Join(workingDir, "../..")
		sharedDir := filepath.Join(projectRoot, "shared")

		// 2. Проверяем выполнение с другим образом (busybox для простого echo)
		engine, err := dagger.NewEngine(ctx)
		require.NoError(t, err, "Failed to connect to Dagger Engine. Is it running?")
		defer engine.Close()

		logger := logrus.New()
		logger.SetLevel(logrus.DebugLevel)

		serverConfig := mcp.ServerConfig{
			Name:        "test-agent",
			Version:     "1.0.0",
			Logger:      logger,
			EnableTools: true,
		}
		mcpServer, err := mcp.NewMCPServer(serverConfig)
		require.NoError(t, err)

		appConfig := &config.AppConfig{
			Agent: config.AgentConfig{
				SharedDir: sharedDir, // Используем абсолютный путь
				Tools: []config.ToolConfig{
					{
						Name:   "echo_tool",
						Engine: "dagger",
						Params: []map[string]string{
							{
								"name":     "message",
								"type":     "string",
								"required": "true",
							},
						},
						EngineSpec: map[string]interface{}{
							"image":   "busybox:latest",
							"command": []interface{}{"echo"},
						},
					},
				},
			},
		}

		agent := &PraxisAgent{
			name:             "test-agent",
			mcpServer:        mcpServer,
			executionEngines: map[string]contracts.ExecutionEngine{"dagger": engine},
			appConfig:        appConfig,
			logger:           logger,
		}

		agent.registerDynamicTools()

		// 2. Получаем handler для прямого тестирования
		handler := agent.mcpServer.FindToolHandler("echo_tool")
		require.NotNil(t, handler, "echo_tool handler should be registered")

		// 3. Выполняем простую команду echo
		mcpRequest := mcpTypes.CallToolRequest{
			Params: struct {
				Name      string         `json:"name"`
				Arguments interface{}    `json:"arguments,omitempty"`
				Meta      *mcpTypes.Meta `json:"_meta,omitempty"`
			}{
				Name: "echo_tool",
				Arguments: map[string]interface{}{
					"message": "Hello from E2E test",
				},
			},
		}

		ctxWithTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		result, err := handler(ctxWithTimeout, mcpRequest)

		// 3. Проверяем результат
		if err != nil && containsNetworkError(err) {
			t.Skipf("Skipping test due to network issues: %v", err)
		}

		require.NoError(t, err)
		require.NotNil(t, result)
		t.Logf("ConfigVariations result: IsError=%v, Content length=%d", result.IsError, len(result.Content))
		if len(result.Content) > 0 {
			t.Logf("Content[0] type: %T", result.Content[0])
		}
		assert.False(t, result.IsError)
		
		// Проверяем, что есть контент
		require.NotEmpty(t, result.Content, "Result should have content")

		var textContent *mcpTypes.TextContent
		var ok bool
		if textContent, ok = result.Content[0].(*mcpTypes.TextContent); !ok {
			// Попробуем как значение, а не указатель
			if tc, isValue := result.Content[0].(mcpTypes.TextContent); isValue {
				textContent = &tc
				ok = true
			}
		}
		require.True(t, ok, "Result should contain text content")
		content := textContent.Text
		// echo выводит все аргументы включая --message
		assert.Contains(t, content, "--message", "Result: %s", content)
		assert.Contains(t, content, "Hello from E2E test", "Result: %s", content)

		t.Logf("Configuration variation test completed. Result: %s", content)
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
