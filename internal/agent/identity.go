package agent

import (
	"bytes"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multiformats/go-multibase"
	"github.com/praxis/praxis-go-sdk/internal/a2a"
	"github.com/praxis/praxis-go-sdk/internal/config"
	internalcrypto "github.com/praxis/praxis-go-sdk/internal/crypto"
	"github.com/praxis/praxis-go-sdk/internal/did"
	"github.com/sirupsen/logrus"
)

const (
	didCoreContext             = "https://www.w3.org/ns/did/v1"
	ed25519VerificationContext = "https://w3id.org/security/suites/ed25519-2020/v1"
)

// IdentityManager manages the agent's DID, signing key, and DID document.
type IdentityManager struct {
	did        string
	didDocURI  string
	keyID      string
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	didDoc     *did.Document
	signer     *internalcrypto.AgentCardSigner
	logger     *logrus.Logger
}

// NewIdentityManager loads/creates signing key material and builds DID doc metadata.
func NewIdentityManager(agentCfg config.AgentConfig, logger *logrus.Logger) (*IdentityManager, error) {
	if logger == nil {
		return nil, errors.New("identity manager requires logger")
	}

	cfg := agentCfg.Identity
	if cfg.DID == "" {
		return nil, fmt.Errorf("identity: DID must be configured")
	}
	if cfg.Key.Type != "ed25519" && cfg.Key.Type != "" {
		return nil, fmt.Errorf("identity: unsupported key type %s", cfg.Key.Type)
	}

	fragment := cfg.Key.ID
	if fragment == "" {
		fragment = "key-1"
	}

	priv, pub, err := loadOrCreateEd25519Key(cfg.Key)
	if err != nil {
		return nil, err
	}

	keyID, err := internalcrypto.KidForDID(cfg.DID, fragment)
	if err != nil {
		return nil, err
	}

	doc, didDocURI, err := buildDIDDocument(cfg.DID, keyID, pub, agentCfg, cfg.DIDDocURI)
	if err != nil {
		return nil, err
	}

	signer, err := internalcrypto.NewAgentCardSigner(cfg.DID, didDocURI, keyID, priv)
	if err != nil {
		return nil, err
	}

	return &IdentityManager{
		did:        cfg.DID,
		didDocURI:  didDocURI,
		keyID:      keyID,
		privateKey: priv,
		publicKey:  pub,
		didDoc:     doc,
		signer:     signer,
		logger:     logger,
	}, nil
}

func (m *IdentityManager) DID() string {
	return m.did
}

func (m *IdentityManager) DIDDocument() *did.Document {
	return m.didDoc
}

func (m *IdentityManager) DIDDocumentURI() string {
	return m.didDocURI
}

func (m *IdentityManager) KeyID() string {
	return m.keyID
}

func (m *IdentityManager) SignAgentCard(card *a2a.AgentCard) error {
	if m.signer == nil {
		return fmt.Errorf("identity: signer not initialized")
	}
	return m.signer.Sign(card, time.Now().UTC())
}

func (m *IdentityManager) PublicKey() ed25519.PublicKey {
	return m.publicKey
}

func (m *IdentityManager) PrivateKey() ed25519.PrivateKey {
	return m.privateKey
}

type keyConfig = config.IdentityKeyConfig

func loadOrCreateEd25519Key(cfg keyConfig) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	switch cfg.Source {
	case "file", "":
		return loadFromFile(cfg)
	default:
		return nil, nil, fmt.Errorf("identity: unsupported key source %s", cfg.Source)
	}
}

func loadFromFile(cfg keyConfig) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if cfg.Path == "" {
		return nil, nil, fmt.Errorf("identity: key path required for file source")
	}

	path := cfg.Path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("identity: create key dir: %w", err)
		}
		pub, priv, err := ed25519.GenerateKey(crand.Reader)
		if err != nil {
			return nil, nil, fmt.Errorf("identity: generate key: %w", err)
		}
		if err := writeKeyFile(path, priv); err != nil {
			return nil, nil, err
		}
		return priv, pub, nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("identity: read key file: %w", err)
	}

	privBytes, err := decodeKey(data)
	if err != nil {
		return nil, nil, err
	}
	priv := ed25519.PrivateKey(privBytes)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, nil
}

func decodeKey(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == ed25519.PrivateKeySize {
		key := make([]byte, ed25519.PrivateKeySize)
		copy(key, trimmed)
		return key, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil {
		return nil, fmt.Errorf("identity: decode key: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("identity: expected %d byte key, got %d", ed25519.PrivateKeySize, len(decoded))
	}
	return decoded, nil
}

func writeKeyFile(path string, priv ed25519.PrivateKey) error {
	encoded := base64.StdEncoding.EncodeToString(priv)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(encoded+"\n"), 0o600); err != nil {
		return fmt.Errorf("identity: write key temp: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("identity: move key file: %w", err)
	}
	return nil
}

func buildDIDDocument(didID, keyID string, pub ed25519.PublicKey, agentCfg config.AgentConfig, overrideURI string) (*did.Document, string, error) {
	multibaseKey, err := multibase.Encode(multibase.Base58BTC, append([]byte{0xed, 0x01}, pub...))
	if err != nil {
		return nil, "", fmt.Errorf("identity: encode multibase key: %w", err)
	}

	baseURL := strings.TrimSuffix(agentCfg.URL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}

	cardEndpoint := baseURL + "/.well-known/agent-card.json"
	didEndpoint := overrideURI
	if didEndpoint == "" {
		didEndpoint = baseURL + "/.well-known/did.json"
	}

	doc := &did.Document{
		Context: []any{
			didCoreContext,
			ed25519VerificationContext,
		},
		ID: didID,
		VerificationMethod: []did.VerificationMethod{{
			ID:                 keyID,
			Type:               "Ed25519VerificationKey2020",
			Controller:         didID,
			PublicKeyMultibase: multibaseKey,
		}},
		Authentication:  []any{keyID},
		AssertionMethod: []any{keyID},
		Service: []did.Service{{
			ID:              didID + "#agent-card",
			Type:            "PraxisAgentCard",
			ServiceEndpoint: cardEndpoint,
		}},
	}

	doc.Context = normalizeContexts(doc.Context)

	return doc, didEndpoint, nil
}

func normalizeContexts(ctx []any) []any {
	if ctx == nil {
		ctx = []any{}
	}
	seen := map[string]bool{}
	result := make([]any, 0, len(ctx)+2)

	addString := func(s string) {
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}

	addString(didCoreContext)

	for _, entry := range ctx {
		switch v := entry.(type) {
		case string:
			addString(v)
		default:
			result = append(result, v)
		}
	}

	addString(ed25519VerificationContext)
	return result
}
