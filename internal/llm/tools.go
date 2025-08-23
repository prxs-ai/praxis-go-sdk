package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sirupsen/logrus"

	"go-p2p-agent/internal/mcp"
)

// ToolRegistry manages the registration and execution of tools
type ToolRegistry struct {
	localTools  map[string]*LocalTool
	remoteTools map[string]*RemoteTool
	cache       *ToolCache
	mcpBridge   mcp.Bridge
	logger      *logrus.Logger
	mu          sync.RWMutex
	lastUpdated time.Time
}

// LocalTool represents a tool available locally
type LocalTool struct {
	ServerName string
	Tool       mcp.MCPTool
	Status     string
	LastUsed   time.Time
}

// RemoteTool represents a tool available on a remote peer
type RemoteTool struct {
	PeerName   string
	PeerID     peer.ID
	ServerName string
	Tool       mcp.MCPTool
	Latency    time.Duration
	LastSeen   time.Time
	Status     string
}

// ToolCache provides caching for tool executions
type ToolCache struct {
	entries map[string]*ToolCacheEntry
	maxSize int
	ttl     time.Duration
	mu      sync.RWMutex
}

// ToolCacheEntry represents a cached tool execution
type ToolCacheEntry struct {
	Key         string
	Value       interface{}
	CreatedAt   time.Time
	AccessedAt  time.Time
	AccessCount int
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry(mcpBridge mcp.Bridge, logger *logrus.Logger) *ToolRegistry {
	registry := &ToolRegistry{
		localTools:  make(map[string]*LocalTool),
		remoteTools: make(map[string]*RemoteTool),
		cache:       NewToolCache(1000, 5*time.Minute),
		mcpBridge:   mcpBridge,
		logger:      logger,
		lastUpdated: time.Now(),
	}

	// Initialize local tools
	registry.refreshLocalTools()

	return registry
}

// ExecuteTool executes a tool
func (r *ToolRegistry) ExecuteTool(ctx context.Context, toolName string, params map[string]interface{}) (interface{}, error) {
	// Check if this is a remote tool (format: peer_name/tool_name)
	if strings.Contains(toolName, "/") {
		parts := strings.SplitN(toolName, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid remote tool format: %s", toolName)
		}
		peerName := parts[0]
		remoteTool := parts[1]
		return r.ExecuteRemoteTool(ctx, peerName, remoteTool, params)
	}

	// Check cache
	cacheKey := r.getCacheKey(toolName, params)
	if cachedResult := r.cache.Get(cacheKey); cachedResult != nil {
		r.logger.Infof("Cache hit for tool execution: %s", toolName)
		return cachedResult, nil
	}

	// Look for local tool
	r.mu.RLock()
	localTool, exists := r.localTools[toolName]
	r.mu.RUnlock()

	if !exists {
		// Refresh local tools and try again
		r.refreshLocalTools()
		r.mu.RLock()
		localTool, exists = r.localTools[toolName]
		r.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("tool not found: %s", toolName)
		}
	}

	// Execute the tool
	r.logger.Infof("Executing local tool: %s on server: %s", toolName, localTool.ServerName)

	// Update last used timestamp
	r.mu.Lock()
	localTool.LastUsed = time.Now()
	r.mu.Unlock()

	// Execute tool through MCP bridge
	result, err := r.mcpBridge.GetClient().InvokeTool(ctx, peer.ID(""), localTool.ServerName, toolName, params)
	if err != nil {
		return nil, fmt.Errorf("tool execution failed: %w", err)
	}

	// Cache result
	r.cache.Set(cacheKey, result)

	return result, nil
}

// ExecuteRemoteTool executes a tool on a remote peer
func (r *ToolRegistry) ExecuteRemoteTool(ctx context.Context, peerName, toolName string, params map[string]interface{}) (interface{}, error) {
	// Check cache
	cacheKey := r.getCacheKey(fmt.Sprintf("%s/%s", peerName, toolName), params)
	if cachedResult := r.cache.Get(cacheKey); cachedResult != nil {
		r.logger.Infof("Cache hit for remote tool execution: %s on peer: %s", toolName, peerName)
		return cachedResult, nil
	}

	// Find remote tool
	var serverName string
	var peerID peer.ID

	r.mu.RLock()
	for _, remoteTool := range r.remoteTools {
		if remoteTool.PeerName == peerName && remoteTool.Tool.Name == toolName {
			serverName = remoteTool.ServerName
			peerID = remoteTool.PeerID
			break
		}
	}
	r.mu.RUnlock()

	if serverName == "" {
		// Try to discover the tool
		if err := r.discoverRemoteTools(peerName); err != nil {
			return nil, fmt.Errorf("failed to discover tools for peer %s: %w", peerName, err)
		}

		// Check again
		r.mu.RLock()
		for _, remoteTool := range r.remoteTools {
			if remoteTool.PeerName == peerName && remoteTool.Tool.Name == toolName {
				serverName = remoteTool.ServerName
				peerID = remoteTool.PeerID
				break
			}
		}
		r.mu.RUnlock()

		if serverName == "" {
			return nil, fmt.Errorf("tool %s not found on peer %s", toolName, peerName)
		}
	}

	// Execute tool
	r.logger.Infof("Executing remote tool: %s on server: %s of peer: %s", toolName, serverName, peerName)

	// Get MCP client
	client := r.mcpBridge.GetClient()

	// Execute the tool
	result, err := client.InvokeTool(ctx, peerID, serverName, toolName, params)
	if err != nil {
		return nil, fmt.Errorf("remote tool execution failed: %w", err)
	}

	// Cache result
	r.cache.Set(cacheKey, result)

	return result, nil
}

// GetLocalTools returns the list of local tools
func (r *ToolRegistry) GetLocalTools() map[string]mcp.MCPTool {
	r.refreshLocalTools()

	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make(map[string]mcp.MCPTool)
	for name, tool := range r.localTools {
		tools[name] = tool.Tool
	}

	return tools
}

// GetRemoteTools returns the list of tools available on a remote peer
func (r *ToolRegistry) GetRemoteTools(peerName string) (map[string]mcp.MCPTool, error) {
	// Try to discover remote tools if not already cached
	if err := r.discoverRemoteTools(peerName); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make(map[string]mcp.MCPTool)
	for _, tool := range r.remoteTools {
		if tool.PeerName == peerName {
			tools[tool.Tool.Name] = tool.Tool
		}
	}

	return tools, nil
}

// refreshLocalTools refreshes the list of local tools
func (r *ToolRegistry) refreshLocalTools() {
	// Skip if last update was recent
	if time.Since(r.lastUpdated) < 5*time.Second {
		return
	}

	// Get all tools from MCP bridge
	allTools := r.mcpBridge.ListAllTools()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Update local tools
	for serverName, tools := range allTools {
		for _, tool := range tools {
			toolKey := tool.Name
			r.localTools[toolKey] = &LocalTool{
				ServerName: serverName,
				Tool:       tool,
				Status:     "active",
			}
		}
	}

	r.lastUpdated = time.Now()
}

// discoverRemoteTools discovers tools available on a remote peer
func (r *ToolRegistry) discoverRemoteTools(peerName string) error {
	// Skip if we've discovered tools for this peer recently
	r.mu.RLock()
	recentDiscovery := false
	for _, tool := range r.remoteTools {
		if tool.PeerName == peerName && time.Since(tool.LastSeen) < 1*time.Minute {
			recentDiscovery = true
			break
		}
	}
	r.mu.RUnlock()

	if recentDiscovery {
		return nil
	}

	// Get MCP client
	client := r.mcpBridge.GetClient()

	// Get peer ID
	// This would normally come from p2p.Host.GetPeerByName
	// For now, we'll just assume it's available
	var peerID peer.ID

	// List remote tools
	remoteTools, err := client.ListRemoteTools(context.Background(), peerID)
	if err != nil {
		return fmt.Errorf("failed to list remote tools: %w", err)
	}

	// Update remote tools
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	for serverName, tools := range remoteTools {
		for _, tool := range tools {
			toolKey := fmt.Sprintf("%s/%s", peerName, tool.Name)
			r.remoteTools[toolKey] = &RemoteTool{
				PeerName:   peerName,
				PeerID:     peerID,
				ServerName: serverName,
				Tool:       tool,
				Status:     "active",
				LastSeen:   now,
			}
		}
	}

	return nil
}

// getCacheKey generates a cache key for a tool execution
func (r *ToolRegistry) getCacheKey(toolName string, params map[string]interface{}) string {
	// Serialize parameters to JSON
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		// If serialization fails, just use the tool name
		return toolName
	}

	return fmt.Sprintf("%s:%s", toolName, string(paramsJSON))
}

// NewToolCache creates a new tool cache
func NewToolCache(maxSize int, ttl time.Duration) *ToolCache {
	return &ToolCache{
		entries: make(map[string]*ToolCacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get retrieves a value from the cache
func (c *ToolCache) Get(key string) interface{} {
	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check if entry has expired
	if time.Since(entry.CreatedAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil
	}

	// Update access time and count
	c.mu.Lock()
	entry.AccessedAt = time.Now()
	entry.AccessCount++
	c.mu.Unlock()

	return entry.Value
}

// Set adds a value to the cache
func (c *ToolCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict entries
	if len(c.entries) >= c.maxSize {
		c.evict()
	}

	// Add new entry
	c.entries[key] = &ToolCacheEntry{
		Key:         key,
		Value:       value,
		CreatedAt:   time.Now(),
		AccessedAt:  time.Now(),
		AccessCount: 1,
	}
}

// evict removes the least recently used entries
func (c *ToolCache) evict() {
	// Simple strategy: remove oldest entries
	var oldestKey string
	var oldestTime time.Time

	// Find oldest entry
	for key, entry := range c.entries {
		if oldestKey == "" || entry.AccessedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.AccessedAt
		}
	}

	// Remove oldest entry
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// RateLimiter implements rate limiting for API requests
type RateLimiter struct {
	requestsPerMinute int
	tokensPerMinute   int
	requestCount      int
	tokenCount        int
	lastReset         time.Time
	mu                sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requestsPerMinute, tokensPerMinute int) *RateLimiter {
	return &RateLimiter{
		requestsPerMinute: requestsPerMinute,
		tokensPerMinute:   tokensPerMinute,
		lastReset:         time.Now(),
	}
}

// Allow checks if a request is allowed under the rate limits
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset counters if a minute has passed
	if time.Since(r.lastReset) > time.Minute {
		r.requestCount = 0
		r.tokenCount = 0
		r.lastReset = time.Now()
	}

	// Check if we're over the request limit
	if r.requestCount >= r.requestsPerMinute {
		return false
	}

	// Increment request count
	r.requestCount++
	return true
}

// AddTokens adds tokens to the count
func (r *RateLimiter) AddTokens(tokens int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset counters if a minute has passed
	if time.Since(r.lastReset) > time.Minute {
		r.requestCount = 0
		r.tokenCount = 0
		r.lastReset = time.Now()
	}

	// Check if we're over the token limit
	if r.tokenCount+tokens > r.tokensPerMinute {
		return false
	}

	// Add tokens
	r.tokenCount += tokens
	return true
}

// Cache implements caching for LLM responses
type Cache struct {
	entries map[string]*Response
	maxSize int
	ttl     time.Duration
	mu      sync.RWMutex
}

// NewCache creates a new cache
func NewCache(maxSize int, ttl time.Duration) *Cache {
	return &Cache{
		entries: make(map[string]*Response),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Get retrieves a response from the cache
func (c *Cache) Get(key string) *Response {
	c.mu.RLock()
	response, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check if entry has expired
	if time.Since(response.ProcessTime) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil
	}

	// Create a copy to avoid modifying the cached version
	copy := *response
	return &copy
}

// Set adds a response to the cache
func (c *Cache) Set(key string, response *Response) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict entries
	if len(c.entries) >= c.maxSize {
		// Simple strategy: remove random entry
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}

	// Create a copy to avoid modifying the original
	copy := *response
	c.entries[key] = &copy
}
