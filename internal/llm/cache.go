package llm

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

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

// Size returns the current cache size
func (c *ToolCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all entries from the cache
func (c *ToolCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*ToolCacheEntry)
}

// generateCacheKey generates a cache key for a tool execution
func GenerateCacheKey(toolName string, args map[string]interface{}) string {
	// Serialize parameters to JSON
	argsJSON, err := json.Marshal(args)
	if err != nil {
		// If serialization fails, just use the tool name
		return toolName
	}

	return fmt.Sprintf("%s:%s", toolName, string(argsJSON))
}