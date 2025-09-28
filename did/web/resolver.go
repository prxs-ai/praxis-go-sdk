package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/praxis/praxis-go-sdk/internal/did"
)

// Resolver resolves did:web identifiers over HTTPS (optionally HTTP for dev).
type Resolver struct {
	Client        *http.Client
	AllowInsecure bool
}

const defaultTimeout = 10 * time.Second

// Resolve implements did.Resolver for did:web identifiers.
func (r *Resolver) Resolve(ctx context.Context, identifier string) (*did.Document, error) {
	doc, _, err := r.ResolveRaw(ctx, identifier)
	return doc, err
}

// ResolveRaw behaves like Resolve but also returns the raw bytes fetched from the
// remote endpoint. It is primarily used by did:webvh verification to compute a
// canonical hash over the original JSON document.
func (r *Resolver) ResolveRaw(ctx context.Context, identifier string) (*did.Document, []byte, error) {
	method, specific, err := did.BaseIdentifier(identifier)
	if err != nil {
		return nil, nil, err
	}
	if method != "web" {
		return nil, nil, did.ErrUnsupportedMethod
	}

	httpClient := r.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	reqURL, err := r.buildURL(specific)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("did:web resolver: unexpected status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("did:web resolver: read failed: %w", err)
	}

	var doc did.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("did:web resolver: decode failed: %w", err)
	}

	if doc.ID == "" {
		doc.ID = identifier
	}

	return &doc, raw, nil
}

func (r *Resolver) buildURL(methodSpecific string) (string, error) {
	parts := strings.Split(methodSpecific, ":")
	if len(parts) == 0 {
		return "", fmt.Errorf("did:web resolver: empty method specific id")
	}

	host := strings.ReplaceAll(parts[0], "%3A", ":")
	if host == "" {
		return "", fmt.Errorf("did:web resolver: empty host")
	}

	var base url.URL
	if r.AllowInsecure {
		base.Scheme = "http"
	} else {
		base.Scheme = "https"
	}
	base.Host = host

	if len(parts) == 1 {
		base.Path = "/.well-known/did.json"
		return base.String(), nil
	}

	// Additional path segments map colon-separated tokens to slash-separated path.
	// According to the DID web method spec, they should be percent-decoded before usage.
	segments := make([]string, len(parts)-1)
	for i, seg := range parts[1:] {
		segments[i] = strings.ReplaceAll(seg, "%3A", ":")
	}
	base.Path = path.Join(append([]string{""}, append(segments, "did.json")...)...)
	return base.String(), nil
}
