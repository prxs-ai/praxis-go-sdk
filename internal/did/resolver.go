package did

import (
	"context"
	"sync"
	"time"
)

// MultiResolver routes resolution requests to concrete method-specific resolvers
// (did:web, did:webvh, etc.) while maintaining an in-memory TTL cache.
type MultiResolver struct {
	webResolver   Resolver
	webvhResolver Resolver

	ttl   time.Duration
	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	doc       *Document
	expiresAt time.Time
}

// MultiResolverOption configures MultiResolver.
type MultiResolverOption func(*MultiResolver)

// WithCacheTTL overrides cache TTL duration.
func WithCacheTTL(ttl time.Duration) MultiResolverOption {
	return func(m *MultiResolver) {
		if ttl > 0 {
			m.ttl = ttl
		}
	}
}

// WithWebResolver sets a custom did:web resolver.
func WithWebResolver(r Resolver) MultiResolverOption {
	return func(m *MultiResolver) {
		m.webResolver = r
	}
}

// WithWebVHResolver sets a custom did:webvh resolver.
func WithWebVHResolver(r Resolver) MultiResolverOption {
	return func(m *MultiResolver) {
		m.webvhResolver = r
	}
}

// NewMultiResolver constructs a MultiResolver with optional overrides.
func NewMultiResolver(opts ...MultiResolverOption) *MultiResolver {
	m := &MultiResolver{
		ttl:   time.Minute,
		cache: make(map[string]cacheEntry),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Resolve resolves DID documents with caching and per-method routing.
func (m *MultiResolver) Resolve(ctx context.Context, did string) (*Document, error) {
	if doc := m.getFromCache(did); doc != nil {
		return doc, nil
	}

	method, _, err := BaseIdentifier(did)
	if err != nil {
		return nil, err
	}

	var resolver Resolver
	switch method {
	case "web":
		resolver = m.webResolver
	case "webvh":
		resolver = m.webvhResolver
	default:
		return nil, ErrUnsupportedMethod
	}

	if resolver == nil {
		return nil, ErrUnsupportedMethod
	}

	doc, err := resolver.Resolve(ctx, did)
	if err != nil {
		return nil, err
	}

	m.setCache(did, doc)
	return doc, nil
}

func (m *MultiResolver) ttlDuration() time.Duration {
	if m.ttl <= 0 {
		return time.Minute
	}
	return m.ttl
}

func (m *MultiResolver) getFromCache(did string) *Document {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if entry, ok := m.cache[did]; ok {
		if time.Now().Before(entry.expiresAt) {
			return entry.doc
		}
	}
	return nil
}

func (m *MultiResolver) setCache(did string, doc *Document) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[did] = cacheEntry{doc: doc, expiresAt: time.Now().Add(m.ttlDuration())}
}

// Invalidate removes cached entry for DID (mostly used in tests / rotations).
func (m *MultiResolver) Invalidate(did string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cache, did)
}
