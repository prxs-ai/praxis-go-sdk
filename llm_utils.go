package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/sirupsen/logrus"
)


func NewFunctionRegistry() *FunctionRegistryImpl {
	return &FunctionRegistryImpl{
		functions: make(map[string]OpenAIFunctionDef),
	}
}

func (r *FunctionRegistryImpl) Register(fn OpenAIFunctionDef) error {
	if fn.Name == "" {
		return fmt.Errorf("function name cannot be empty")
	}
	
	r.functions[fn.Name] = fn
	return nil
}

func (r *FunctionRegistryImpl) Get(name string) (OpenAIFunctionDef, bool) {
	fn, exists := r.functions[name]
	return fn, exists
}

func (r *FunctionRegistryImpl) GetAll() []openai.Tool {
	tools := make([]openai.Tool, 0, len(r.functions))
	for _, fn := range r.functions {
		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        fn.Name,
				Description: fn.Description,
				Parameters:  fn.Parameters,
				Strict:      fn.Strict,
			},
		})
	}
	
	return tools
}

func (r *FunctionRegistryImpl) List() []string {
	names := make([]string, 0, len(r.functions))
	for name := range r.functions {
		names = append(names, name)
	}
	
	return names
}

func (r *FunctionRegistryImpl) Remove(name string) bool {
	_, exists := r.functions[name]
	if exists {
		delete(r.functions, name)
	}
	
	return exists
}

func (r *FunctionRegistryImpl) Count() int {
	return len(r.functions)
}


func NewLLMCache(config LLMCacheConfig) *LLMCacheImpl {
	cache := &LLMCacheImpl{
		entries: make(map[string]*LLMCacheEntry),
		config:  config,
	}
	
	
	go cache.cleanup()
	
	return cache
}

func (c *LLMCacheImpl) generateKey(req *LLMRequest) string {
	
	
	return fmt.Sprintf("%s_%d", req.UserInput, len(req.UserInput))
}

func (c *LLMCacheImpl) Get(req *LLMRequest) *LLMResponse {
	if !c.config.Enabled {
		return nil
	}
	
	key := c.generateKey(req)
	
	entry, exists := c.entries[key]
	if !exists {
		return nil
	}
	
	
	if time.Since(entry.CreatedAt) > c.config.TTL {
		return nil
	}
	
	return entry.Response
}

func (c *LLMCacheImpl) Set(req *LLMRequest, resp *LLMResponse) {
	if !c.config.Enabled {
		return
	}
	
	key := c.generateKey(req)
	
	
	if len(c.entries) >= c.config.MaxSize {
		
		c.evictOldest()
	}
	
	c.entries[key] = &LLMCacheEntry{
		Key:       key,
		Request:   req,
		Response:  resp,
		CreatedAt: time.Now(),
	}
}

func (c *LLMCacheImpl) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	
	for key, entry := range c.entries {
		if oldestKey == "" || entry.CreatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.CreatedAt
		}
	}
	
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (c *LLMCacheImpl) cleanup() {
	ticker := time.NewTicker(c.config.TTL / 2)
	defer ticker.Stop()
	
	for range ticker.C {
		now := time.Now()
		for key, entry := range c.entries {
			if now.Sub(entry.CreatedAt) > c.config.TTL {
				delete(c.entries, key)
			}
		}
	}
}

func (c *LLMCacheImpl) Clear() {
	c.entries = make(map[string]*LLMCacheEntry)
}

func (c *LLMCacheImpl) Size() int {
	return len(c.entries)
}


func NewRateLimiter(config LLMRateConfig) *RateLimiterImpl {
	limiter := &RateLimiterImpl{
		requestsPerMinute: config.RequestsPerMinute,
		tokensPerMinute:   config.TokensPerMinute,
		lastReset:         time.Now(),
	}
	
	
	go limiter.resetCounters()
	
	return limiter
}

func (r *RateLimiterImpl) Allow() bool {
	if r.requestCount >= r.requestsPerMinute {
		return false
	}
	
	r.requestCount++
	return true
}

func (r *RateLimiterImpl) AllowTokens(tokens int) bool {
	if r.tokenCount+tokens > r.tokensPerMinute {
		return false
	}
	
	r.tokenCount += tokens
	return true
}

func (r *RateLimiterImpl) resetCounters() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		r.requestCount = 0
		r.tokenCount = 0
		r.lastReset = time.Now()
	}
}

func (r *RateLimiterImpl) GetStatus() (int, int) {
	return r.requestCount, r.tokenCount
}


func NewToolRegistry(mcpBridge *MCPBridge, p2pAgent *P2PAgent, logger *logrus.Logger) (*ToolRegistryImpl, error) {
	registry := &ToolRegistryImpl{
		localTools:  make(map[string]*LocalTool),
		remoteTools: make(map[string]*RemoteTool),
		mcpBridge:   mcpBridge,
		p2pAgent:    p2pAgent,
		logger:      logger,
		lastUpdated: time.Now(),
	}
	
	
	registry.cache = &ToolCacheImpl{
		entries: make(map[string]*ToolCacheEntry),
		maxSize: 1000,
		ttl:     5 * time.Minute,
	}
	
	
	registry.discovery = &ToolDiscoveryImpl{
		enabled:         true,
		refreshInterval: 60 * time.Second,
		timeout:         10 * time.Second,
		p2pAgent:        p2pAgent,
		logger:          logger,
	}
	
	
	if registry.discovery.enabled {
		go registry.startDiscovery()
	}
	
	
	if err := registry.loadLocalTools(); err != nil {
		return nil, fmt.Errorf("failed to load local tools: %w", err)
	}
	
	return registry, nil
}

func (r *ToolRegistryImpl) loadLocalTools() error {
	if r.mcpBridge == nil {
		return nil
	}
	
	capabilities := r.mcpBridge.GetCapabilities()
	
	for _, capability := range capabilities {
		for _, tool := range capability.Tools {
			key := fmt.Sprintf("%s:%s", capability.ServerName, tool.Name)
			r.localTools[key] = &LocalTool{
				ServerName: capability.ServerName,
				Tool:       tool,
				Status:     ToolStatusActive,
				LastUsed:   time.Now(),
			}
		}
	}
	
	r.logger.Infof("🔧 [Tool Registry] Loaded %d local tools", len(r.localTools))
	return nil
}

func (r *ToolRegistryImpl) GetLocalTools() map[string]MCPTool {
	tools := make(map[string]MCPTool)
	for key, localTool := range r.localTools {
		tools[key] = localTool.Tool
	}
	
	return tools
}

func (r *ToolRegistryImpl) GetRemoteTools(peerName string) (map[string]MCPTool, error) {
	tools := make(map[string]MCPTool)
	for key, remoteTool := range r.remoteTools {
		if remoteTool.PeerName == peerName {
			tools[key] = remoteTool.Tool
		}
	}
	
	return tools, nil
}

func (r *ToolRegistryImpl) DiscoverPeerCapabilities(ctx context.Context, peerName string, refresh bool) (interface{}, error) {
	if !refresh {
		
		cached := r.cache.Get(fmt.Sprintf("capabilities:%s", peerName))
		if cached != nil {
			return cached.Value, nil
		}
	}
	
	
	if r.p2pAgent == nil {
		return nil, fmt.Errorf("P2P agent not available")
	}
	
	
	card, err := r.p2pAgent.RequestCard(peerName)
	if err != nil {
		return nil, fmt.Errorf("failed to request card from peer %s: %w", peerName, err)
	}
	
	
	r.updateRemoteToolsFromCard(peerName, card)
	
	
	r.cache.Set(fmt.Sprintf("capabilities:%s", peerName), card, time.Now())
	
	return card, nil
}

func (r *ToolRegistryImpl) updateRemoteToolsFromCard(peerName string, card *ExtendedAgentCard) {
	
	for key, remoteTool := range r.remoteTools {
		if remoteTool.PeerName == peerName {
			delete(r.remoteTools, key)
		}
	}
	
	
	for _, capability := range card.MCPServers {
		for _, tool := range capability.Tools {
			key := fmt.Sprintf("%s:%s:%s", peerName, capability.ServerName, tool.Name)
			r.remoteTools[key] = &RemoteTool{
				PeerName:   peerName,
				ServerName: capability.ServerName,
				Tool:       tool,
				LastSeen:   time.Now(),
				Status:     ToolStatusActive,
			}
		}
	}
	
	r.logger.Infof("🔄 [Tool Registry] Updated remote tools for peer %s", peerName)
}

func (r *ToolRegistryImpl) startDiscovery() {
	ticker := time.NewTicker(r.discovery.refreshInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		r.discoverPeers()
	}
}

func (r *ToolRegistryImpl) discoverPeers() {
	if r.p2pAgent == nil {
		return
	}
	
	
	r.p2pAgent.mu.RLock()
	peers := make(map[string]bool)
	for peerName := range r.p2pAgent.connections {
		peers[peerName] = true
	}
	r.p2pAgent.mu.RUnlock()
	
	
	for peerName := range peers {
		ctx, cancel := context.WithTimeout(context.Background(), r.discovery.timeout)
		_, err := r.DiscoverPeerCapabilities(ctx, peerName, false)
		if err != nil {
			r.logger.Warnf("⚠️ [Tool Registry] Failed to discover capabilities for peer %s: %v", peerName, err)
		}
		cancel()
	}
}


func (c *ToolCacheImpl) Get(key string) *ToolCacheEntry {
	entry, exists := c.entries[key]
	if !exists {
		return nil
	}
	
	
	if time.Since(entry.CreatedAt) > c.ttl {
		delete(c.entries, key)
		return nil
	}
	
	entry.AccessedAt = time.Now()
	entry.AccessCount++
	
	return entry
}

func (c *ToolCacheImpl) Set(key string, value interface{}, createdAt time.Time) {
	
	if len(c.entries) >= c.maxSize {
		c.evictLRU()
	}
	
	c.entries[key] = &ToolCacheEntry{
		Key:         key,
		Value:       value,
		CreatedAt:   createdAt,
		AccessedAt:  createdAt,
		AccessCount: 1,
	}
}

func (c *ToolCacheImpl) evictLRU() {
	var lruKey string
	var lruTime time.Time
	
	for key, entry := range c.entries {
		if lruKey == "" || entry.AccessedAt.Before(lruTime) {
			lruKey = key
			lruTime = entry.AccessedAt
		}
	}
	
	if lruKey != "" {
		delete(c.entries, lruKey)
	}
}

func (c *ToolCacheImpl) Clear() {
	c.entries = make(map[string]*ToolCacheEntry)
}

func (c *ToolCacheImpl) Size() int {
	return len(c.entries)
}