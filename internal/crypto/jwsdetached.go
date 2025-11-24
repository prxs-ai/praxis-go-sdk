package crypto

import (
	"crypto/ed25519"
	"fmt"
	"strings"
)

// AgentCardSigner signs canonical agent cards with detached JWS (EdDSA/Ed25519).
type AgentCardSigner struct {
	did        string
	didDocURI  string
	keyID      string
	privateKey ed25519.PrivateKey
}

// NewAgentCardSigner constructs signer with DID metadata and Ed25519 private key.
func NewAgentCardSigner(did, didDocURI, keyID string, priv ed25519.PrivateKey) (*AgentCardSigner, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("agent card signer: invalid Ed25519 private key size")
	}
	if did == "" {
		return nil, fmt.Errorf("agent card signer: DID must be provided")
	}
	if keyID == "" {
		return nil, fmt.Errorf("agent card signer: key id must be provided")
	}
	return &AgentCardSigner{did: did, didDocURI: didDocURI, keyID: keyID, privateKey: priv}, nil
}

// KidForDID returns kid by appending fragment to DID, ensuring only one '#'.
func KidForDID(did, fragment string) (string, error) {
	if did == "" {
		return "", fmt.Errorf("kid: did empty")
	}
	if fragment == "" {
		return "", fmt.Errorf("kid: fragment empty")
	}
	if strings.Contains(fragment, "#") {
		return "", fmt.Errorf("kid: fragment must not contain '#'")
	}
	return did + "#" + fragment, nil
}
