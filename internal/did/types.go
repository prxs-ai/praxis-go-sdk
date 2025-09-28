package did

import (
	"context"
	"errors"
	"fmt"
)

// Common errors returned by DID helpers.
var (
	ErrUnsupportedMethod    = errors.New("did: unsupported method")
	ErrDocumentNotFound     = errors.New("did: document not found")
	ErrVerificationMethod   = errors.New("did: verification method not found")
	ErrKeyFormatUnsupported = errors.New("did: unsupported verification method key format")
	ErrHashMismatch         = errors.New("did: content hash mismatch")
)

// Document represents a DID Document with the subset of fields required by Praxis.
// It intentionally keeps fields flexible (using interface{}) so future extensions
// do not require breaking changes.
type Document struct {
	Context            []any                `json:"@context"`
	ID                 string               `json:"id"`
	VerificationMethod []VerificationMethod `json:"verificationMethod,omitempty"`
	Authentication     []any                `json:"authentication,omitempty"`
	AssertionMethod    []any                `json:"assertionMethod,omitempty"`
	Service            []Service            `json:"service,omitempty"`
}

// VerificationMethod describes a verification method entry inside a DID document.
type VerificationMethod struct {
	ID                 string         `json:"id"`
	Type               string         `json:"type"`
	Controller         string         `json:"controller,omitempty"`
	PublicKeyJWK       map[string]any `json:"publicKeyJwk,omitempty"`
	PublicKeyMultibase string         `json:"publicKeyMultibase,omitempty"`
}

// Service is a DID service descriptor.
type Service struct {
	ID              string      `json:"id"`
	Type            string      `json:"type"`
	ServiceEndpoint interface{} `json:"serviceEndpoint"`
}

// Resolver resolves DID documents for a given DID identifier.
type Resolver interface {
	Resolve(ctx context.Context, did string) (*Document, error)
}

// MethodForDID extracts method name from DID string (e.g. "did:web:example" -> "web").
func MethodForDID(did string) (string, error) {
	if len(did) < 4 || did[:4] != "did:" {
		return "", fmt.Errorf("did: invalid identifier: %s", did)
	}
	rest := did[4:]
	idx := 0
	for idx < len(rest) && rest[idx] != ':' {
		idx++
	}
	if idx == 0 || idx == len(rest) {
		return "", fmt.Errorf("did: malformed identifier: %s", did)
	}
	return rest[:idx], nil
}

// BaseIdentifier splits DID into method and method-specific ID.
func BaseIdentifier(did string) (method string, methodSpecific string, err error) {
	if len(did) < 4 || did[:4] != "did:" {
		return "", "", fmt.Errorf("did: invalid identifier: %s", did)
	}
	rest := did[4:]
	idx := 0
	for idx < len(rest) && rest[idx] != ':' {
		idx++
	}
	if idx == 0 || idx == len(rest) {
		return "", "", fmt.Errorf("did: malformed identifier: %s", did)
	}
	method = rest[:idx]
	methodSpecific = rest[idx+1:]
	if methodSpecific == "" {
		return "", "", fmt.Errorf("did: missing method specific identifier: %s", did)
	}
	return method, methodSpecific, nil
}
