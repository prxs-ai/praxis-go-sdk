package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"go-p2p-agent/internal/config"
	"go-p2p-agent/internal/mcp"
)

// OpenAIClient implements the Client interface for OpenAI
type OpenAIClient struct {
	config       *config.LLMConfig
	httpClient   *http.Client
	toolRegistry *ToolRegistry
	functions    map[string]FunctionDef
	metrics      Metrics
	cache        *Cache
	rateLimiter  *RateLimiter
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// NewClient creates a new LLM client
func NewClient(cfg *config.LLMConfig, mcpBridge mcp.Bridge, logger *logrus.Logger) (Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("LLM is disabled in configuration")
	}

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}

	// Initialize tool registry
	toolRegistry := NewToolRegistry(mcpBridge, logger)

	// Initialize cache if enabled
	var cache *Cache
	if cfg.Caching.Enabled {
		cache = NewCache(cfg.Caching.MaxSize, cfg.Caching.TTL)
	}

	// Initialize rate limiter
	rateLimiter := NewRateLimiter(cfg.RateLimiting.RequestsPerMinute, cfg.RateLimiting.TokensPerMinute)

	client := &OpenAIClient{
		config:       cfg,
		httpClient:   httpClient,
		toolRegistry: toolRegistry,
		functions:    make(map[string]FunctionDef),
		cache:        cache,
		rateLimiter:  rateLimiter,
		logger:       logger,
	}

	// Register built-in tools
	client.registerBuiltInTools()

	return client, nil
}

// ProcessRequest processes an LLM request
func (c *OpenAIClient) ProcessRequest(ctx context.Context, req *Request) (*Response, error) {
	startTime := time.Now()

	// Generate request ID if not provided
	if req.ID == "" {
		req.ID = fmt.Sprintf("llm_%d", time.Now().UnixNano())
	}

	// Check cache if enabled
	if c.cache != nil {
		if cachedResp := c.cache.Get(req.UserInput); cachedResp != nil {
			c.logger.Infof("[LLM] Cache hit for request: %s", req.ID)
			cachedResp.ID = req.ID // Update ID to match request
			cachedResp.ProcessTime = time.Since(startTime)
			return cachedResp, nil
		}
	}

	// Check rate limits
	if !c.rateLimiter.Allow() {
		return nil, &LLMError{
			Code:    ErrorRateLimit,
			Message: "rate limit exceeded",
			Type:    "rate_limit_error",
		}
	}

	// Apply configuration defaults if not specified in request
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.config.MaxTokens
	}

	temperature := req.Temperature
	if temperature == 0 {
		temperature = c.config.Temperature
	}

	// Prepare tools
	tools := c.prepareTools(req.Tools)

	// Prepare messages
	messages := []Message{
		{
			Role:    "user",
			Content: req.UserInput,
		},
	}

	// Create OpenAI request
	openAIReq := struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Tools       []Tool    `json:"tools,omitempty"`
		ToolChoice  string    `json:"tool_choice,omitempty"`
		MaxTokens   int       `json:"max_tokens,omitempty"`
		Temperature float32   `json:"temperature,omitempty"`
	}{
		Model:       c.config.Model,
		Messages:    messages,
		Tools:       tools,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	// Set tool_choice if tools are available
	if len(tools) > 0 {
		openAIReq.ToolChoice = "auto"
	}

	// Marshal request
	reqBody, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	// Send request
	c.logger.Infof("[LLM] Sending request to OpenAI: %s", req.ID)
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.metrics.RequestsError++
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer httpResp.Body.Close()

	// Check status code
	if httpResp.StatusCode != http.StatusOK {
		c.metrics.RequestsError++
		var errResp struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(httpResp.Body).Decode(&errResp); err != nil {
			return nil, fmt.Errorf("HTTP error %d", httpResp.StatusCode)
		}
		return nil, &LLMError{
			Code:    httpResp.StatusCode,
			Message: errResp.Error.Message,
			Type:    errResp.Error.Type,
		}
	}

	// Parse response
	var openAIResp struct {
		ID      string `json:"id"`
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&openAIResp); err != nil {
		c.metrics.RequestsError++
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Update metrics
	c.mu.Lock()
	c.metrics.RequestsTotal++
	c.metrics.RequestsSuccess++
	c.metrics.TokensUsed += int64(openAIResp.Usage.TotalTokens)
	c.metrics.LastRequest = time.Now()
	processTime := time.Since(startTime)
	if c.metrics.AvgResponseTime == 0 {
		c.metrics.AvgResponseTime = processTime
	} else {
		c.metrics.AvgResponseTime = (c.metrics.AvgResponseTime + processTime) / 2
	}
	c.mu.Unlock()

	// Check if we have a response
	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	// Get the first choice
	choice := openAIResp.Choices[0]

	// Create response
	resp := &Response{
		ID:          req.ID,
		Response:    choice.Message.Content,
		TokensUsed:  openAIResp.Usage.TotalTokens,
		ProcessTime: processTime,
		Success:     true,
	}

	// Handle tool calls
	if len(choice.Message.ToolCalls) > 0 {
		resp.ToolCalls = make([]ToolExecution, 0, len(choice.Message.ToolCalls))
		c.mu.Lock()
		c.metrics.ToolCallsTotal += int64(len(choice.Message.ToolCalls))
		c.mu.Unlock()

		for _, toolCall := range choice.Message.ToolCalls {
			execution, err := c.executeToolCall(ctx, toolCall)
			if err != nil {
				c.mu.Lock()
				c.metrics.ToolCallsError++
				c.mu.Unlock()
				execution.Error = err.Error()
			} else {
				c.mu.Lock()
				c.metrics.ToolCallsSuccess++
				c.mu.Unlock()
			}
			resp.ToolCalls = append(resp.ToolCalls, execution)
		}
	}

	// Cache response if enabled
	if c.cache != nil {
		c.cache.Set(req.UserInput, resp)
	}

	return resp, nil
}

// RegisterTool registers a tool with the LLM
func (c *OpenAIClient) RegisterTool(tool FunctionDef) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.functions[tool.Name]; exists {
		return fmt.Errorf("tool already registered: %s", tool.Name)
	}

	c.functions[tool.Name] = tool
	c.logger.Infof("[LLM] Registered tool: %s", tool.Name)
	return nil
}

// GetAvailableTools returns the list of available tools
func (c *OpenAIClient) GetAvailableTools() []FunctionDef {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tools := make([]FunctionDef, 0, len(c.functions))
	for _, tool := range c.functions {
		tools = append(tools, tool)
	}
	return tools
}

// Health checks the health of the LLM connection
func (c *OpenAIClient) Health() error {
	// Create a simple request to test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &Request{
		ID:        "health_check",
		UserInput: "ping",
	}

	_, err := c.ProcessRequest(ctx, req)
	return err
}

// GetMetrics returns metrics about the LLM client
func (c *OpenAIClient) GetMetrics() Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy to avoid race conditions
	metrics := Metrics{
		RequestsTotal:    c.metrics.RequestsTotal,
		RequestsSuccess:  c.metrics.RequestsSuccess,
		RequestsError:    c.metrics.RequestsError,
		AvgResponseTime:  c.metrics.AvgResponseTime,
		TokensUsed:       c.metrics.TokensUsed,
		ToolCallsTotal:   c.metrics.ToolCallsTotal,
		ToolCallsSuccess: c.metrics.ToolCallsSuccess,
		ToolCallsError:   c.metrics.ToolCallsError,
		LastRequest:      c.metrics.LastRequest,
	}

	return metrics
}

// prepareTools prepares the tools for the request
func (c *OpenAIClient) prepareTools(requestedTools []string) []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// If no tools requested, return empty list
	if len(requestedTools) == 0 {
		return nil
	}

	tools := make([]Tool, 0)

	// If "*" is requested, return all tools
	if len(requestedTools) == 1 && requestedTools[0] == "*" {
		for _, fn := range c.functions {
			tools = append(tools, Tool{
				Type:     "function",
				Function: fn,
			})
		}
		return tools
	}

	// Otherwise, return only requested tools
	for _, name := range requestedTools {
		if fn, exists := c.functions[name]; exists {
			tools = append(tools, Tool{
				Type:     "function",
				Function: fn,
			})
		}
	}

	return tools
}

// executeToolCall executes a tool call
func (c *OpenAIClient) executeToolCall(ctx context.Context, toolCall ToolCall) (ToolExecution, error) {
	execution := ToolExecution{
		ToolName: toolCall.Function.Name,
	}
	startTime := time.Now()

	// Parse arguments
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &params); err != nil {
		return execution, fmt.Errorf("failed to parse arguments: %w", err)
	}
	execution.Parameters = params

	// Execute tool
	result, err := c.toolRegistry.ExecuteTool(ctx, toolCall.Function.Name, params)
	execution.Duration = time.Since(startTime)

	if err != nil {
		return execution, err
	}

	execution.Result = result
	return execution, nil
}

// registerBuiltInTools registers built-in tools
func (c *OpenAIClient) registerBuiltInTools() {
	// Echo tool
	c.RegisterTool(FunctionDef{
		Name:        "echo",
		Description: "Echoes the input message",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "The message to echo",
				},
			},
			"required": []string{"message"},
		},
	})

	// Other built-in tools can be added here
}