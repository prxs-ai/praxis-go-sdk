package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/sirupsen/logrus"
)

type LLMAgentImpl struct {
	config       *LLMConfig
	toolRegistry *ToolRegistryImpl
	mcpBridge    *MCPBridge
	p2pAgent     *P2PAgent
	logger       *logrus.Logger
	metrics      *LLMMetrics
	cache        *LLMCacheImpl
	rateLimiter  *RateLimiterImpl
	openaiClient *openai.Client
	functions    *FunctionRegistryImpl
	mu           sync.RWMutex
}

func NewLLMAgent(config *LLMConfig, mcpBridge *MCPBridge, p2pAgent *P2PAgent, logger *logrus.Logger) (*LLMAgentImpl, error) {
	if config == nil {
		config = &DefaultLLMConfig
	}

	
	if config.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}

	
	logger.Infof("🔑 [LLM Agent] Creating OpenAI client with API key: %s...", config.APIKey[:min(10, len(config.APIKey))])
	logger.Infof("🔑 [LLM Agent] Full API key length: %d", len(config.APIKey))
	
	agent := &LLMAgentImpl{
		config:       config,
		mcpBridge:    mcpBridge,
		p2pAgent:     p2pAgent,
		logger:       logger,
		metrics:      &LLMMetrics{},
		openaiClient: openai.NewClient(config.APIKey),
		functions:    NewFunctionRegistry(),
	}

	
	if config.Caching.Enabled {
		agent.cache = NewLLMCache(config.Caching)
	}

	
	agent.rateLimiter = NewRateLimiter(config.RateLimiting)

	
	toolRegistry, err := NewToolRegistry(mcpBridge, p2pAgent, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create tool registry: %w", err)
	}
	agent.toolRegistry = toolRegistry

	
	if err := agent.registerDefaultFunctions(); err != nil {
		return nil, fmt.Errorf("failed to register default functions: %w", err)
	}

	logger.Info("🤖 [LLM Agent] Initialized successfully")
	return agent, nil
}

func (a *LLMAgentImpl) ProcessRequest(ctx context.Context, req *LLMRequest) (*LLMResponse, error) {
	startTime := time.Now()
	
	a.mu.Lock()
	a.metrics.RequestsTotal++
	a.mu.Unlock()

	
	if !a.rateLimiter.Allow() {
		a.mu.Lock()
		a.metrics.RequestsError++
		a.mu.Unlock()
		return nil, &LLMError{
			Code:    LLMErrorRateLimit,
			Message: "Rate limit exceeded",
			Type:    "rate_limit_error",
		}
	}

	
	if a.cache != nil {
		if cached := a.cache.Get(req); cached != nil {
			a.logger.Debugf("🎯 [LLM Agent] Cache hit for request: %s", req.ID)
			return cached, nil
		}
	}

	
	openAIReq, err := a.prepareOpenAIRequest(req)
	if err != nil {
		a.mu.Lock()
		a.metrics.RequestsError++
		a.mu.Unlock()
		return nil, fmt.Errorf("failed to prepare OpenAI request: %w", err)
	}

	
	openAIResp, err := a.callOpenAI(ctx, openAIReq)
	if err != nil {
		a.mu.Lock()
		a.metrics.RequestsError++
		a.mu.Unlock()
		return nil, fmt.Errorf("OpenAI API call failed: %w", err)
	}

	
	response, err := a.processOpenAIResponse(ctx, openAIResp, req.ID)
	if err != nil {
		a.mu.Lock()
		a.metrics.RequestsError++
		a.mu.Unlock()
		return nil, fmt.Errorf("failed to process OpenAI response: %w", err)
	}

	
	duration := time.Since(startTime)
	a.mu.Lock()
	a.metrics.RequestsSuccess++
	a.metrics.AvgResponseTime = (a.metrics.AvgResponseTime + duration) / 2
	a.metrics.TokensUsed += int64(openAIResp.Usage.TotalTokens)
	a.mu.Unlock()

	response.ProcessTime = duration
	response.TokensUsed = openAIResp.Usage.TotalTokens

	
	if a.cache != nil {
		a.cache.Set(req, response)
	}

	a.logger.Infof("✅ [LLM Agent] Processed request %s in %v", req.ID, duration)
	return response, nil
}

func (a *LLMAgentImpl) prepareOpenAIRequest(req *LLMRequest) (*openai.ChatCompletionRequest, error) {
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "You are a helpful AI assistant. You have access to various tools that can help you accomplish tasks. Use tools when appropriate to provide accurate and helpful responses.",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: req.UserInput,
		},
	}

	request := &openai.ChatCompletionRequest{
		Model:       a.config.Model,
		Messages:    messages,
		MaxTokens:   a.config.MaxTokens,
		Temperature: a.config.Temperature,
	}

	
	tools := a.getAvailableTools()
	if len(tools) > 0 {
		request.Tools = tools
		request.ToolChoice = "auto"
	}

	return request, nil
}

func (a *LLMAgentImpl) callOpenAI(ctx context.Context, req *openai.ChatCompletionRequest) (*openai.ChatCompletionResponse, error) {
	a.logger.Debugf("🔄 [LLM Agent] Calling OpenAI API with %d tools available", len(req.Tools))
	
	resp, err := a.openaiClient.CreateChatCompletion(ctx, *req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	a.logger.Debugf("✅ [LLM Agent] OpenAI API call successful, tokens used: %d", resp.Usage.TotalTokens)
	return &resp, nil
}

func (a *LLMAgentImpl) processOpenAIResponse(ctx context.Context, resp *openai.ChatCompletionResponse, requestID string) (*LLMResponse, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in OpenAI response")
	}

	choice := resp.Choices[0]
	response := &LLMResponse{
		ID:        requestID,
		Response:  choice.Message.Content,
		Success:   true,
		ToolCalls: []ToolExecution{},
	}

	
	if len(choice.Message.ToolCalls) > 0 {
		a.logger.Infof("🔧 [LLM Agent] Processing %d tool calls", len(choice.Message.ToolCalls))
		
		for _, toolCall := range choice.Message.ToolCalls {
			execution, err := a.executeTool(ctx, toolCall.Function.Name, toolCall.Function.Arguments)
			if err != nil {
				a.logger.Errorf("❌ [LLM Agent] Tool execution failed: %v", err)
				execution = ToolExecution{
					ToolName: toolCall.Function.Name,
					Error:    err.Error(),
					Duration: 0,
				}
			}
			response.ToolCalls = append(response.ToolCalls, execution)
		}

		
		if len(response.ToolCalls) > 0 {
			finalResponse, err := a.generateFinalResponse(ctx, resp, response.ToolCalls)
			if err != nil {
				a.logger.Warnf("⚠️ [LLM Agent] Failed to generate final response: %v", err)
			} else {
				response.Response = finalResponse
			}
		}
	}

	return response, nil
}

func (a *LLMAgentImpl) executeTool(ctx context.Context, toolName string, arguments string) (ToolExecution, error) {
	startTime := time.Now()
	
	a.mu.Lock()
	a.metrics.ToolCallsTotal++
	a.mu.Unlock()

	
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &params); err != nil {
		return ToolExecution{}, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	
	localTools := a.toolRegistry.GetLocalTools()
	
	
	if result, err := a.executeNativeFunction(toolName, params); err == nil {
		execution := ToolExecution{
			ToolName:   toolName,
			Parameters: params,
			Duration:   time.Since(startTime),
			Result:     result,
		}
		
		a.mu.Lock()
		a.metrics.ToolCallsSuccess++
		a.mu.Unlock()
		
		return execution, nil
	}

	
	for key, tool := range localTools {
		if tool.Name == toolName || strings.HasSuffix(key, ":"+toolName) {
			result, err := a.executeLocalTool(ctx, key, params)
			
			execution := ToolExecution{
				ToolName:   toolName,
				Parameters: params,
				Duration:   time.Since(startTime),
			}
			
			if err != nil {
				execution.Error = err.Error()
				a.mu.Lock()
				a.metrics.ToolCallsError++
				a.mu.Unlock()
			} else {
				execution.Result = result
				a.mu.Lock()
				a.metrics.ToolCallsSuccess++
				a.mu.Unlock()
			}
			
			return execution, err
		}
	}

	
	
	return ToolExecution{}, fmt.Errorf("tool '%s' not found", toolName)
}

func (a *LLMAgentImpl) executeNativeFunction(toolName string, params map[string]interface{}) (interface{}, error) {
	switch toolName {
	case "echo":
		message, ok := params["message"].(string)
		if !ok {
			return nil, fmt.Errorf("echo: missing or invalid 'message' parameter")
		}
		return map[string]interface{}{
			"echo": message,
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil

	case "calculate":
		expression, ok := params["expression"].(string)
		if !ok {
			return nil, fmt.Errorf("calculate: missing or invalid 'expression' parameter")
		}
		
		
		result, err := a.evaluateExpression(expression)
		if err != nil {
			return nil, fmt.Errorf("calculate: %w", err)
		}
		
		return map[string]interface{}{
			"expression": expression,
			"result": result,
			"type": "number",
		}, nil

	case "get_current_time":
		format := "human"
		if f, ok := params["format"].(string); ok {
			format = f
		}
		
		now := time.Now()
		var timeStr string
		
		switch format {
		case "rfc3339":
			timeStr = now.Format(time.RFC3339)
		case "unix":
			timeStr = fmt.Sprintf("%d", now.Unix())
		default: 
			timeStr = now.Format("2006-01-02 15:04:05 MST")
		}
		
		return map[string]interface{}{
			"current_time": timeStr,
			"format": format,
			"timezone": now.Location().String(),
		}, nil

	case "generate_uuid":
		uuid := make([]byte, 16)
		_, err := rand.Read(uuid)
		if err != nil {
			return nil, fmt.Errorf("generate_uuid: failed to generate random bytes: %w", err)
		}
		
		
		uuid[6] = (uuid[6] & 0x0f) | 0x40
		uuid[8] = (uuid[8] & 0x3f) | 0x80
		
		uuidStr := fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
		
		return map[string]interface{}{
			"uuid": uuidStr,
			"version": 4,
			"generated_at": time.Now().Format(time.RFC3339),
		}, nil

	case "hash_text":
		text, ok := params["text"].(string)
		if !ok {
			return nil, fmt.Errorf("hash_text: missing or invalid 'text' parameter")
		}
		
		algorithm := "sha256"
		if alg, ok := params["algorithm"].(string); ok {
			algorithm = alg
		}
		
		var hashStr string
		var err error
		
		switch algorithm {
		case "sha256":
			hashStr, err = a.hashSHA256(text)
		case "md5":
			hashStr, err = a.hashMD5(text)
		default:
			return nil, fmt.Errorf("hash_text: unsupported algorithm '%s'", algorithm)
		}
		
		if err != nil {
			return nil, fmt.Errorf("hash_text: %w", err)
		}
		
		return map[string]interface{}{
			"text": text,
			"algorithm": algorithm,
			"hash": hashStr,
			"length": len(hashStr),
		}, nil

	case "manipulate_text":
		text, ok := params["text"].(string)
		if !ok {
			return nil, fmt.Errorf("manipulate_text: missing or invalid 'text' parameter")
		}
		
		operation, ok := params["operation"].(string)
		if !ok {
			return nil, fmt.Errorf("manipulate_text: missing or invalid 'operation' parameter")
		}
		
		var result interface{}
		
		switch operation {
		case "uppercase":
			result = strings.ToUpper(text)
		case "lowercase":
			result = strings.ToLower(text)
		case "reverse":
			runes := []rune(text)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			result = string(runes)
		case "length":
			result = len(text)
		case "wordcount":
			result = len(strings.Fields(text))
		default:
			return nil, fmt.Errorf("manipulate_text: unsupported operation '%s'", operation)
		}
		
		return map[string]interface{}{
			"original": text,
			"operation": operation,
			"result": result,
		}, nil

	case "get_system_info":
		infoType := "basic"
		if it, ok := params["info_type"].(string); ok {
			infoType = it
		}
		
		result := make(map[string]interface{})
		
		switch infoType {
		case "basic", "memory", "peer_id", "uptime", "version":
			result["agent_type"] = "praxis-go-agent"
			result["version"] = "1.0.0"
			result["protocol_version"] = "1.0.0"
			
			if infoType == "peer_id" || infoType == "basic" {
				if a.p2pAgent != nil && a.p2pAgent.host != nil {
					result["peer_id"] = a.p2pAgent.host.ID().String()
				}
			}
			
			if infoType == "uptime" || infoType == "basic" {
				result["uptime"] = time.Since(time.Now().Add(-time.Hour)).String() 
			}
			
			if infoType == "memory" || infoType == "basic" {
				result["functions_count"] = a.functions.Count()
				if a.toolRegistry != nil {
					result["local_tools_count"] = len(a.toolRegistry.GetLocalTools())
				}
			}
			
		default:
			return nil, fmt.Errorf("get_system_info: unsupported info_type '%s'", infoType)
		}
		
		result["info_type"] = infoType
		result["timestamp"] = time.Now().Format(time.RFC3339)
		
		return result, nil

	case "execute_remote_tool":
		
		peerName, ok := params["peer_name"].(string)
		if !ok {
			return nil, fmt.Errorf("execute_remote_tool: missing or invalid 'peer_name' parameter")
		}
		
		serverName, ok := params["server_name"].(string)
		if !ok {
			return nil, fmt.Errorf("execute_remote_tool: missing or invalid 'server_name' parameter")
		}
		
		toolName, ok := params["tool_name"].(string)
		if !ok {
			return nil, fmt.Errorf("execute_remote_tool: missing or invalid 'tool_name' parameter")
		}
		
		
		toolParams := make(map[string]interface{})
		
		
		if p, ok := params["params"].(map[string]interface{}); ok {
			toolParams = p
			a.logger.Debugf("[Remote Tool] Using direct params object: %+v", toolParams)
		} else if paramsJSON, ok := params["tool_params_json"].(string); ok {
			
			if paramsJSON != "" {
				if err := json.Unmarshal([]byte(paramsJSON), &toolParams); err != nil {
					return nil, fmt.Errorf("execute_remote_tool: failed to parse tool_params_json: %w", err)
				}
				a.logger.Debugf("[Remote Tool] Using JSON string params: %+v", toolParams)
			}
		} else {
			
			for key, value := range params {
				if key != "peer_name" && key != "server_name" && key != "tool_name" && key != "params" && key != "tool_params_json" {
					toolParams[key] = value
				}
			}
			a.logger.Debugf("[Remote Tool] Using extracted individual params: %+v", toolParams)
		}
		
		
		result, err := a.executeRemoteTool(peerName, serverName, toolName, toolParams)
		if err != nil {
			return nil, fmt.Errorf("execute_remote_tool: %w", err)
		}
		
		return map[string]interface{}{
			"peer_name":    peerName,
			"server_name":  serverName,
			"tool_name":    toolName,
			"params":       toolParams,
			"result":       result,
			"timestamp":    time.Now().Format(time.RFC3339),
		}, nil

	default:
		return nil, fmt.Errorf("native function '%s' not implemented", toolName)
	}
}

func (a *LLMAgentImpl) evaluateExpression(expr string) (float64, error) {
	
	
	
	expr = strings.ReplaceAll(expr, " ", "")
	
	
	if strings.Contains(expr, "+") {
		parts := strings.Split(expr, "+")
		if len(parts) == 2 {
			a, err1 := strconv.ParseFloat(parts[0], 64)
			b, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil {
				return a + b, nil
			}
		}
	}
	
	
	if strings.Contains(expr, "-") && !strings.HasPrefix(expr, "-") {
		parts := strings.Split(expr, "-")
		if len(parts) == 2 {
			a, err1 := strconv.ParseFloat(parts[0], 64)
			b, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil {
				return a - b, nil
			}
		}
	}
	
	
	if strings.Contains(expr, "*") {
		parts := strings.Split(expr, "*")
		if len(parts) == 2 {
			a, err1 := strconv.ParseFloat(parts[0], 64)
			b, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil {
				return a * b, nil
			}
		}
	}
	
	
	if strings.Contains(expr, "/") {
		parts := strings.Split(expr, "/")
		if len(parts) == 2 {
			a, err1 := strconv.ParseFloat(parts[0], 64)
			b, err2 := strconv.ParseFloat(parts[1], 64)
			if err1 == nil && err2 == nil && b != 0 {
				return a / b, nil
			}
		}
	}
	
	
	if result, err := strconv.ParseFloat(expr, 64); err == nil {
		return result, nil
	}
	
	return 0, fmt.Errorf("unsupported expression: %s", expr)
}

func (a *LLMAgentImpl) hashSHA256(text string) (string, error) {
	hash := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", hash), nil
}

func (a *LLMAgentImpl) hashMD5(text string) (string, error) {
	hash := md5.Sum([]byte(text))
	return fmt.Sprintf("%x", hash), nil
}

func (a *LLMAgentImpl) executeLocalTool(ctx context.Context, toolKey string, params map[string]interface{}) (interface{}, error) {
	if a.mcpBridge == nil {
		return nil, fmt.Errorf("MCP bridge not available")
	}

	
	parts := strings.Split(toolKey, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid tool key format: %s", toolKey)
	}
	
	serverName := parts[0]
	toolName := parts[1]

	
	mcpReq := &MCPRequest{
		ID:         fmt.Sprintf("tool_%d", time.Now().UnixNano()),
		Method:     MCPMethodToolCall,
		ServerName: serverName,
		ToolName:   toolName,
		Params:     params,
		Timeout:    15 * time.Second,
	}

	
	mcpResp, err := a.mcpBridge.ProcessRequest(ctx, mcpReq)
	if err != nil {
		return nil, fmt.Errorf("MCP tool execution failed: %w", err)
	}

	if mcpResp.Error != nil {
		return nil, fmt.Errorf("MCP tool error: %s", mcpResp.Error.Message)
	}

	return mcpResp.Result, nil
}

func (a *LLMAgentImpl) executeRemoteTool(peerName, serverName, toolName string, params map[string]interface{}) (interface{}, error) {
	
	if a.p2pAgent == nil {
		return nil, fmt.Errorf("P2P agent not available")
	}

	
	if a.mcpBridge == nil {
		return nil, fmt.Errorf("MCP bridge not available")
	}

	
	peerID, err := a.p2pAgent.GetPeerByName(peerName)
	if err != nil {
		return nil, fmt.Errorf("failed to find peer '%s': %w", peerName, err)
	}

	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	
	mcpResp, err := a.mcpBridge.client.InvokeTool(ctx, peerID, serverName, toolName, params)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke remote tool '%s' on peer '%s': %w", toolName, peerName, err)
	}

	
	if mcpResp.Error != nil {
		return nil, fmt.Errorf("remote tool error: %s", mcpResp.Error.Message)
	}

	a.logger.Infof("✅ [Remote Tool] Successfully executed '%s' on peer '%s' server '%s'", toolName, peerName, serverName)
	return mcpResp.Result, nil
}

func (a *LLMAgentImpl) generateFinalResponse(ctx context.Context, originalResp *openai.ChatCompletionResponse, toolCalls []ToolExecution) (string, error) {
	
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: "You are a helpful AI assistant. Please provide a natural response based on the tool results.",
		},
		originalResp.Choices[0].Message,
	}

	
	for _, execution := range toolCalls {
		if execution.Error != "" {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleTool,
				Content: fmt.Sprintf("Tool '%s' failed: %s", execution.ToolName, execution.Error),
			})
		} else {
			resultJSON, _ := json.Marshal(execution.Result)
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleTool,
				Content: string(resultJSON),
			})
		}
	}

	
	req := openai.ChatCompletionRequest{
		Model:       a.config.Model,
		Messages:    messages,
		MaxTokens:   a.config.MaxTokens / 2, 
		Temperature: a.config.Temperature,
	}

	resp, err := a.openaiClient.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in follow-up response")
	}

	return resp.Choices[0].Message.Content, nil
}

func (a *LLMAgentImpl) registerDefaultFunctions() error {
	
	
	
	echoFunc := OpenAIFunctionDef{
		Name:        "echo",
		Description: "Echo back the provided message",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message to echo back",
				},
			},
			"required": []string{"message"},
		},
	}

	
	calcFunc := OpenAIFunctionDef{
		Name:        "calculate",
		Description: "Perform basic mathematical calculations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "Mathematical expression to evaluate (e.g., '2+2', '10*3')",
				},
			},
			"required": []string{"expression"},
		},
	}

	
	timeFunc := OpenAIFunctionDef{
		Name:        "get_current_time",
		Description: "Get the current date and time",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Time format (rfc3339, unix, human)",
					"default":     "human",
				},
			},
		},
	}

	
	uuidFunc := OpenAIFunctionDef{
		Name:        "generate_uuid",
		Description: "Generate a random UUID",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		},
	}

	
	hashFunc := OpenAIFunctionDef{
		Name:        "hash_text",
		Description: "Generate SHA256 hash of text (local utility function)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text to hash",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Hash algorithm (sha256, md5)",
					"default":     "sha256",
				},
			},
			"required": []string{"text"},
		},
	}

	
	textFunc := OpenAIFunctionDef{
		Name:        "manipulate_text",
		Description: "Perform text operations (local string utility)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Input text",
				},
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "Operation: uppercase, lowercase, reverse, length, wordcount",
				},
			},
			"required": []string{"text", "operation"},
		},
	}

	
	sysInfoFunc := OpenAIFunctionDef{
		Name:        "get_system_info",
		Description: "Get local system information (local agent capability)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"info_type": map[string]interface{}{
					"type":        "string",
					"description": "Type of info: memory, peer_id, uptime, version",
					"default":     "basic",
				},
			},
		},
	}

	
	
	remoteToolFunc := OpenAIFunctionDef{
		Name:        "execute_remote_tool",
		Description: "Execute a tool on a remote peer via P2P connection. For tool parameters, you can use 'params' object, 'tool_params_json' string, or include parameters directly.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"peer_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the peer to connect to (e.g., 'go-agent-2')",
				},
				"server_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the MCP server on the remote peer (e.g., 'filesystem-sse-server-2')",
				},
				"tool_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the tool to execute (e.g., 'list_directory')",
				},
				"params": map[string]interface{}{
					"type":        "object",
					"description": "Parameters to pass to the remote tool as an object. Optional if using other methods.",
				},
				"tool_params_json": map[string]interface{}{
					"type":        "string",
					"description": "Alternative: Parameters as JSON string (e.g., '{\"path\": \"/tmp\"}' for directory listing). Use if params object fails.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "For filesystem tools: Directory or file path to operate on (alternative to params object)",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "For creation tools: Name of file/directory to create (alternative to params object)",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "For file tools: Content to write to file (alternative to params object)",
				},
			},
			"required": []string{"peer_name", "server_name", "tool_name"},
		},
	}

	
	if err := a.functions.Register(echoFunc); err != nil {
		return err
	}
	if err := a.functions.Register(calcFunc); err != nil {
		return err
	}
	if err := a.functions.Register(timeFunc); err != nil {
		return err
	}
	if err := a.functions.Register(uuidFunc); err != nil {
		return err
	}
	if err := a.functions.Register(hashFunc); err != nil {
		return err
	}
	if err := a.functions.Register(textFunc); err != nil {
		return err
	}
	if err := a.functions.Register(sysInfoFunc); err != nil {
		return err
	}
	if err := a.functions.Register(remoteToolFunc); err != nil {
		return err
	}

	return nil
}

func (a *LLMAgentImpl) getAvailableTools() []openai.Tool {
	tools := a.functions.GetAll()
	
	
	if a.toolRegistry != nil {
		localTools := a.toolRegistry.GetLocalTools()
		for _, tool := range localTools {
			openaiTool := openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			}
			tools = append(tools, openaiTool)
		}
	}
	
	return tools
}

func (a *LLMAgentImpl) Health() error {
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := openai.ChatCompletionRequest{
		Model: a.config.Model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: "Hello",
			},
		},
		MaxTokens: 1,
	}

	_, err := a.openaiClient.CreateChatCompletion(ctx, req)
	return err
}

func (a *LLMAgentImpl) GetMetrics() LLMMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return *a.metrics
}