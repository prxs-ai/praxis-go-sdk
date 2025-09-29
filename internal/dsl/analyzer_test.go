package dsl

import (
	"context"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
)

type mockAgent struct {
	localTools   map[string]bool
	localResults map[string]interface{}
}

func (m *mockAgent) HasLocalTool(toolName string) bool {
	return m.localTools[toolName]
}

func (m *mockAgent) ExecuteLocalTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return m.localResults[toolName], nil
}

func (m *mockAgent) FindAgentWithTool(toolName string) (string, error) {
	return "peer-123", nil
}

func (m *mockAgent) ExecuteRemoteTool(ctx context.Context, peerID string, toolName string, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{"remote": true}, nil
}

func TestTokenize_BasicAndQuotes(t *testing.T) {
	a := NewAnalyzer(logrus.New())

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"basic", `CALL read_file test.txt`, []string{"CALL", "read_file", "test.txt"}},
		{"quoted args", `CALL write_file "hello world.txt" "some content"`, []string{"CALL", "write_file", "hello world.txt", "some content"}},
		{"flags", `CALL tool --flag --key value`, []string{"CALL", "tool", "--flag", "--key", "value"}},
		{"with comment", "# comment\n\nCALL test arg1", []string{"CALL", "test", "arg1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := a.tokenize(tt.input)
			if len(tokens) == 0 {
				t.Fatalf("expected tokens for input %q, got empty slice", tt.input)
			}

			var got []string
			for _, tok := range tokens {
				got = append(got, tok.Value)
				got = append(got, tok.Args...)
			}

			assert.Equal(t, tt.want, got, "input: %s", tt.input)
		})
	}
}

func TestAnalyzeDSL_EmptyOrInvalid(t *testing.T) {
	a := NewAnalyzer(logrus.New())
	ctx := context.Background()

	_, err := a.AnalyzeDSL(ctx, "")
	assert.Error(t, err)

	_, err = a.AnalyzeDSL(ctx, "    ")
	assert.Error(t, err)
}

func TestAnalyzeDSL_SimpleCall_NoAgent(t *testing.T) {
	a := NewAnalyzer(logrus.New())
	ctx := context.Background()

	res, err := a.AnalyzeDSL(ctx, "CALL read_file test.txt")
	assert.NoError(t, err)

	resultMap := res.(map[string]interface{})
	assert.Equal(t, "completed", resultMap["status"])
	results := resultMap["results"].([]interface{})
	assert.Len(t, results, 1)

	call := results[0].(map[string]interface{})
	assert.Equal(t, "read_file", call["tool"])
	assert.Equal(t, "simulated", call["status"])
}

func TestAnalyzeDSL_WriteFile_Content(t *testing.T) {
	a := NewAnalyzer(logrus.New())
	ctx := context.Background()

	res, err := a.AnalyzeDSL(ctx, `CALL write_file my.txt "Hello World"`)
	assert.NoError(t, err)

	resultMap := res.(map[string]interface{})
	call := resultMap["results"].([]interface{})[0].(map[string]interface{})
	payload := call["payload"].(map[string]interface{})
	args := payload["args"].(map[string]interface{})

	assert.Equal(t, "my.txt", args["filename"])
	assert.Equal(t, "Hello World", args["content"])
}

func TestExecuteCall_LocalAgent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	mock := &mockAgent{
		localTools:   map[string]bool{"echo": true},
		localResults: map[string]interface{}{"echo": "ok"},
	}
	a := NewAnalyzerWithAgent(logger, mock)

	node := ASTNode{
		Type:     NodeTypeCall,
		Value:    "CALL",
		ToolName: "echo",
		Args:     map[string]interface{}{"msg": "hi"},
		Params:   map[string]interface{}{"param1": "foo"},
		Secrets:  map[string]interface{}{"secret1": "bar"},
	}

	res, err := a.executeCall(context.Background(), node)
	assert.NoError(t, err)

	result := res.(map[string]interface{})
	assert.Equal(t, "executed", result["status"])
	assert.Equal(t, "ok", result["result"])
}

func TestCache_Behavior(t *testing.T) {
	a := NewAnalyzer(nil)
	ctx := context.Background()

	// First run populates cache
	_, err := a.AnalyzeDSL(ctx, "CALL test_tool arg1")
	assert.NoError(t, err)

	stats := a.GetCacheStats()
	assert.True(t, stats["size"].(int) >= 1)

	// Clear cache
	a.ClearCache()
	stats = a.GetCacheStats()
	assert.Equal(t, 0, stats["size"])
}

func TestMaskSecrets(t *testing.T) {
	secrets := map[string]interface{}{
		"token": "abc123",
		"key":   "xyz",
	}
	masked := maskSecrets(secrets)

	assert.Equal(t, "***", masked["token"])
	assert.Equal(t, "***", masked["key"])
}
