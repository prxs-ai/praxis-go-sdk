package dsl

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/llm"
	"github.com/sirupsen/logrus"
)

// AgentInterface provides access to agent capabilities for DSL execution
type AgentInterface interface {
	HasLocalTool(toolName string) bool
	ExecuteLocalTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error)
	FindAgentWithTool(toolName string) (string, error) // Returns peer ID
	ExecuteRemoteTool(ctx context.Context, peerID string, toolName string, args map[string]interface{}) (interface{}, error)
}

type Analyzer struct {
	logger *logrus.Logger
	agent  AgentInterface
	cache  *llm.ToolCache
	params *ParamStore
}

type ParamStore struct {
	Params  map[string]interface{}
	Secrets map[string]string
	Env     map[string]string
}

func (a *Analyzer) SetParams(ps *ParamStore) {
	a.params = ps
	if a.params == nil {
		a.params = &ParamStore{Params: map[string]interface{}{}, Secrets: map[string]string{}}
	}
}

func NewAnalyzer(logger *logrus.Logger) *Analyzer {
	if logger == nil {
		logger = logrus.New()
	}

	return &Analyzer{
		logger: logger,
		cache:  llm.NewToolCache(1000, 5*time.Minute), // 1000 entries, 5 minute TTL
		params: &ParamStore{Params: map[string]interface{}{}, Secrets: map[string]string{}},
	}
}

// NewAnalyzerWithAgent creates an analyzer with agent integration for real execution
func NewAnalyzerWithAgent(logger *logrus.Logger, agent AgentInterface) *Analyzer {
	if logger == nil {
		logger = logrus.New()
	}

	return &Analyzer{
		logger: logger,
		agent:  agent,
		cache:  llm.NewToolCache(1000, 5*time.Minute), // 1000 entries, 5 minute TTL
	}
}

func (a *Analyzer) AnalyzeDSL(ctx context.Context, dsl string) (interface{}, error) {
	a.logger.Debugf("Analyzing DSL: %s", dsl)

	dsl = strings.TrimSpace(dsl)
	if dsl == "" {
		return nil, fmt.Errorf("empty DSL query")
	}

	tokens := a.tokenize(dsl)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("failed to tokenize DSL")
	}

	ast, err := a.parse(tokens)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSL: %w", err)
	}

	result, err := a.execute(ctx, ast)
	if err != nil {
		return nil, fmt.Errorf("failed to execute DSL: %w", err)
	}

	return result, nil
}

func (a *Analyzer) tokenize(dsl string) []Token {
	var tokens []Token

	lines := strings.Split(dsl, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := a.parseQuotedFields(line)
		if len(parts) > 0 {
			tokens = append(tokens, Token{
				Type:  TokenTypeCommand,
				Value: parts[0],
				Args:  parts[1:],
			})
		}
	}

	return tokens
}

// parseQuotedFields parses a line respecting quoted strings
func (a *Analyzer) parseQuotedFields(line string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false
	escaped := false

	for i, r := range line {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		if r == '"' {
			if inQuotes {
				// End of quoted string
				fields = append(fields, current.String())
				current.Reset()
				inQuotes = false
				// Skip whitespace after quote
				for j := i + 1; j < len(line) && line[j] == ' '; j++ {
					i = j
				}
			} else {
				// Start of quoted string - add any current content first
				if current.Len() > 0 {
					fields = append(fields, current.String())
					current.Reset()
				}
				inQuotes = true
			}
			continue
		}

		if !inQuotes && r == ' ' {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			// Skip consecutive spaces
			for i+1 < len(line) && line[i+1] == ' ' {
				i++
			}
		} else {
			current.WriteRune(r)
		}
	}

	// Add any remaining content
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}

	return fields
}

func (a *Analyzer) parse(tokens []Token) (*AST, error) {
	ast := &AST{
		Nodes: make([]ASTNode, 0),
	}

	for _, token := range tokens {
		// Convert []string args to map[string]interface{} for traditional DSL
		argsMap := make(map[string]interface{})
		toolName := ""

		// Support PARAM / INPUT / SECRET definitions
		if (token.Value == "PARAM" || token.Value == "INPUT" || token.Value == "SECRET") && len(token.Args) > 0 {
			for _, pair := range token.Args {
				kv := strings.SplitN(pair, "=", 2)
				key := strings.TrimSpace(kv[0])
				val := ""
				if len(kv) == 2 {
					val = strings.TrimSpace(kv[1])
				}
				val = strings.Trim(val, `"`)
				argsMap[key] = val
			}
		}

		if token.Value == "CALL" && len(token.Args) > 0 {
			toolName = token.Args[0]
			// Debug logging
			a.logger.Infof("🔍 Debug tokenization: toolName=%s, args=%v", toolName, token.Args)

			// НОВАЯ, ИСПРАВЛЕННАЯ ЛОГИКА
			args := token.Args[1:] // Все, кроме имени инструмента
			for i := 0; i < len(args); i++ {
				arg := args[i]
				if strings.HasPrefix(arg, "--") {
					key := strings.TrimPrefix(arg, "--")
					if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
						// Это пара ключ-значение: --key value
						argsMap[key] = args[i+1]
						i++ // Пропускаем следующий токен, так как он является значением
					} else {
						// Это флаг без значения: --verbose
						argsMap[key] = true
					}
				} else {
					// Это позиционный аргумент
					// Для обратной совместимости с простыми вызовами (например, CALL read_file test.txt)
					// мы можем присвоить его стандартному имени параметра.
					if len(argsMap) == 0 { // Первый позиционный аргумент
						switch toolName {
						case "read_file", "delete_file":
							argsMap["filename"] = arg
						case "list_files":
							argsMap["directory"] = arg
						case "write_file":
							argsMap["filename"] = arg
						default:
							// Generic conversion for unknown tools
							argsMap[fmt.Sprintf("arg%d", len(argsMap))] = arg
						}
					} else {
						// Для write_file второй позиционный аргумент - content
						if toolName == "write_file" && len(argsMap) == 1 {
							// Join remaining args as content
							content := strings.Join(args[i:], " ")
							content = strings.Trim(content, "\"")
							argsMap["content"] = content
							break // Выходим из цикла
						} else {
							// Generic conversion for unknown tools
							argsMap[fmt.Sprintf("arg%d", len(argsMap))] = arg
						}
					}
				}
			}
		}

		node := ASTNode{
			Type:     NodeTypeCommand,
			Value:    token.Value,
			ToolName: toolName,
			Args:     argsMap,
		}

		switch token.Value {
		case "WORKFLOW":
			node.Type = NodeTypeWorkflow
		case "TASK":
			node.Type = NodeTypeTask
		case "AGENT":
			node.Type = NodeTypeAgent
		case "CALL":
			node.Type = NodeTypeCall
		case "PARALLEL":
			node.Type = NodeTypeParallel
		case "SEQUENCE":
			node.Type = NodeTypeSequence
		case "PARAM", "INPUT", "SECRET":
			node.Type = NodeTypeParam
		}

		ast.Nodes = append(ast.Nodes, node)
	}

	return ast, nil
}

func (a *Analyzer) execute(ctx context.Context, ast *AST) (interface{}, error) {
	results := make([]interface{}, 0)

	for _, node := range ast.Nodes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := a.executeNode(ctx, node)
		if err != nil {
			return nil, fmt.Errorf("failed to execute node %s: %w", node.Type, err)
		}

		results = append(results, result)
	}

	return map[string]interface{}{
		"status":  "completed",
		"results": results,
	}, nil
}

func (a *Analyzer) executeNode(ctx context.Context, node ASTNode) (interface{}, error) {
	a.logger.Debugf("Executing node: %s with args: %v", node.Type, node.Args)

	switch node.Type {
	case NodeTypeParam:
		return a.executeParam(ctx, node)
	case NodeTypeWorkflow:
		return a.executeWorkflow(ctx, node)
	case NodeTypeTask:
		return a.executeTask(ctx, node)
	case NodeTypeAgent:
		return a.executeAgent(ctx, node)
	case NodeTypeCall:
		return a.executeCall(ctx, node)
	case NodeTypeParallel:
		return a.executeParallel(ctx, node)
	case NodeTypeSequence:
		return a.executeSequence(ctx, node)
	default:
		return map[string]interface{}{
			"type":   node.Type,
			"value":  node.Value,
			"args":   node.Args,
			"status": "executed",
		}, nil
	}
}

func (a *Analyzer) executeWorkflow(ctx context.Context, node ASTNode) (interface{}, error) {
	workflowName := ""
	if name, exists := node.Args["name"]; exists {
		if nameStr, ok := name.(string); ok {
			workflowName = nameStr
		}
	}

	a.logger.Infof("Executing workflow: %s", workflowName)

	return map[string]interface{}{
		"type":     "workflow",
		"name":     workflowName,
		"status":   "started",
		"children": node.Children,
	}, nil
}

func (a *Analyzer) executeTask(ctx context.Context, node ASTNode) (interface{}, error) {
	taskName := ""
	if name, exists := node.Args["name"]; exists {
		if nameStr, ok := name.(string); ok {
			taskName = nameStr
		}
	}

	a.logger.Infof("Executing task: %s", taskName)

	return map[string]interface{}{
		"type":   "task",
		"name":   taskName,
		"status": "completed",
		"args":   node.Args,
	}, nil
}

func (a *Analyzer) executeAgent(ctx context.Context, node ASTNode) (interface{}, error) {
	agentID := ""
	if id, exists := node.Args["agent_id"]; exists {
		if idStr, ok := id.(string); ok {
			agentID = idStr
		}
	}

	a.logger.Infof("Selecting agent: %s", agentID)

	return map[string]interface{}{
		"type":    "agent",
		"agentID": agentID,
		"status":  "selected",
	}, nil
}

// --- PARAM handling & arg resolution ----------------------------------------
func (a *Analyzer) executeParam(ctx context.Context, node ASTNode) (interface{}, error) {
	if a.params == nil {
		a.params = &ParamStore{Params: map[string]interface{}{}, Secrets: map[string]string{}}
	}
	// The node.Value determines whether it's SECRET or PARAM/INPUT
	switch strings.ToUpper(node.Value) {
	case "SECRET":
		for k, v := range node.Args {
			a.params.Secrets[k] = fmt.Sprintf("%v", v)
		}
	default: // PARAM / INPUT
		for k, v := range node.Args {
			a.params.Params[k] = v
		}
	}
	return map[string]interface{}{
		"type":   "param",
		"status": "applied",
		"kind":   strings.ToLower(node.Value),
		"count":  len(node.Args),
	}, nil
}

var reInterp = regexp.MustCompile(`\{\{\s*(params|secrets|env)\.([a-zA-Z0-9_\-\.]+)\s*\}\}`)

func (a *Analyzer) resolveArgs(raw map[string]interface{}) map[string]interface{} {
	if raw == nil {
		return nil
	}
	out := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		out[k] = a.resolveValue(v)
	}
	return out
}

func (a *Analyzer) resolveValue(v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return a.interpolateString(t)
	case map[string]interface{}:
		return a.resolveArgs(t)
	case []interface{}:
		arr := make([]interface{}, len(t))
		for i, item := range t {
			arr[i] = a.resolveValue(item)
		}
		return arr
	default:
		return v
	}
}

func (a *Analyzer) interpolateString(s string) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	return reInterp.ReplaceAllStringFunc(s, func(m string) string {
		sub := reInterp.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		scope, path := sub[1], sub[2]
		switch scope {
		case "params":
			if val, ok := getNested(a.paramsSafe().Params, path); ok {
				return fmt.Sprintf("%v", val)
			}
		case "secrets":
			if v, ok := a.paramsSafe().Secrets[path]; ok {
				return v
			}
		case "env":
			if a.params != nil && a.params.Env != nil {
				if v, ok := a.params.Env[path]; ok {
					return v
				}
			}
			return os.Getenv(path)
		}
		return ""
	})
}

func getNested(m map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var cur interface{} = m
	for _, p := range parts {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = asMap[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func (a *Analyzer) paramsSafe() *ParamStore {
	if a.params == nil {
		a.params = &ParamStore{Params: map[string]interface{}{}, Secrets: map[string]string{}}
	}
	return a.params
}

func (a *Analyzer) executeCall(ctx context.Context, node ASTNode) (interface{}, error) {
	toolName := node.ToolName

	argsMap := a.resolveArgs(node.Args)
	a.logger.Infof("Calling tool: %s with args: %v", toolName, argsMap)

	// Check cache first
	cacheKey := llm.GenerateCacheKey(toolName, argsMap)
	if cachedResult := a.cache.Get(cacheKey); cachedResult != nil {
		a.logger.Infof("Cache hit for tool %s", toolName)
		return cachedResult, nil
	}

	// If no agent integration, fall back to simulation
	if a.agent == nil {
		a.logger.Debug("No agent integration, simulating execution")
		result := map[string]interface{}{
			"type":   "call",
			"tool":   toolName,
			"args":   argsMap,
			"status": "simulated",
		}
		// Cache simulated result too
		a.cache.Set(cacheKey, result)
		return result, nil
	}

	// Step 1: Check if tool is available locally
	if a.agent.HasLocalTool(toolName) {
		a.logger.Infof("Executing tool %s locally", toolName)
		result, err := a.agent.ExecuteLocalTool(ctx, toolName, argsMap)
		if err != nil {
			errorResult := map[string]interface{}{
				"type":   "call",
				"tool":   toolName,
				"args":   argsMap,
				"status": "failed",
				"error":  err.Error(),
			}
			// Don't cache error results
			return errorResult, nil
		}

		successResult := map[string]interface{}{
			"type":   "call",
			"tool":   toolName,
			"args":   argsMap,
			"status": "executed",
			"result": result,
		}

		// Cache successful result
		a.cache.Set(cacheKey, successResult)
		return successResult, nil
	}

	// Step 2: Find agent with the tool
	peerID, err := a.agent.FindAgentWithTool(toolName)
	if err != nil {
		a.logger.Errorf("Tool %s not found: %v", toolName, err)
		errorResult := map[string]interface{}{
			"type":   "call",
			"tool":   toolName,
			"args":   argsMap,
			"status": "failed",
			"error":  fmt.Sprintf("Tool not found: %v", err),
		}
		// Don't cache error results
		return errorResult, nil
	}

	// Step 3: Execute remotely via P2P
	a.logger.Infof("Executing tool %s remotely on peer %s", toolName, peerID)
	result, err := a.agent.ExecuteRemoteTool(ctx, peerID, toolName, argsMap)
	if err != nil {
		errorResult := map[string]interface{}{
			"type":   "call",
			"tool":   toolName,
			"args":   argsMap,
			"status": "failed",
			"error":  fmt.Sprintf("Remote execution failed: %v", err),
		}
		// Don't cache error results
		return errorResult, nil
	}

	successResult := map[string]interface{}{
		"type":        "call",
		"tool":        toolName,
		"args":        argsMap,
		"status":      "executed",
		"result":      result,
		"executed_by": peerID,
	}

	// Cache successful result
	a.cache.Set(cacheKey, successResult)
	return successResult, nil
}

// GetCacheStats returns cache statistics
func (a *Analyzer) GetCacheStats() map[string]interface{} {
	return map[string]interface{}{
		"size":    a.cache.Size(),
		"enabled": true,
	}
}

// ClearCache clears the tool execution cache
func (a *Analyzer) ClearCache() {
	a.cache.Clear()
	a.logger.Info("Tool execution cache cleared")
}

func (a *Analyzer) executeParallel(ctx context.Context, node ASTNode) (interface{}, error) {
	a.logger.Info("Executing parallel tasks")

	results := make([]interface{}, 0)
	for _, child := range node.Children {
		result, err := a.executeNode(ctx, child)
		if err != nil {
			a.logger.Errorf("Failed to execute parallel task: %v", err)
			continue
		}
		results = append(results, result)
	}

	return map[string]interface{}{
		"type":    "parallel",
		"results": results,
		"status":  "completed",
	}, nil
}

func (a *Analyzer) executeSequence(ctx context.Context, node ASTNode) (interface{}, error) {
	a.logger.Info("Executing sequence tasks")

	results := make([]interface{}, 0)
	for _, child := range node.Children {
		result, err := a.executeNode(ctx, child)
		if err != nil {
			return nil, fmt.Errorf("sequence execution failed at step %d: %w", len(results), err)
		}
		results = append(results, result)
	}

	return map[string]interface{}{
		"type":    "sequence",
		"results": results,
		"status":  "completed",
	}, nil
}

type Token struct {
	Type  TokenType
	Value string
	Args  []string
}

type TokenType string

const (
	TokenTypeCommand  TokenType = "command"
	TokenTypeOperator TokenType = "operator"
	TokenTypeValue    TokenType = "value"
	NodeTypeParam     NodeType  = "param"
)

type AST struct {
	Nodes []ASTNode
}

type ASTNode struct {
	Type     NodeType
	Value    string
	ToolName string                 // Separate field for tool name
	Args     map[string]interface{} // Named arguments from LLM plan
	Children []ASTNode
}

type NodeType string

const (
	NodeTypeCommand  NodeType = "command"
	NodeTypeWorkflow NodeType = "workflow"
	NodeTypeTask     NodeType = "task"
	NodeTypeAgent    NodeType = "agent"
	NodeTypeCall     NodeType = "call"
	NodeTypeParallel NodeType = "parallel"
	NodeTypeSequence NodeType = "sequence"
)
