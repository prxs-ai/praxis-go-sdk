package crypto

import (
	"context"
	"crypto/ed25519"
	crand "crypto/rand"
	"testing"
	"time"

	"github.com/multiformats/go-multibase"
	"github.com/praxis/praxis-go-sdk/internal/a2a"
	"github.com/praxis/praxis-go-sdk/internal/did"
)

type staticResolver struct {
	doc *did.Document
}

func (s *staticResolver) Resolve(ctx context.Context, didID string) (*did.Document, error) {
	return s.doc, nil
}

func TestSignAndVerifyAgentCard(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	didID := "did:web:example.com"
	kid := didID + "#key-1"

	mb, err := multibase.Encode(multibase.Base58BTC, append([]byte{0xed, 0x01}, pub...))
	if err != nil {
		t.Fatalf("encode multibase: %v", err)
	}

	doc := &did.Document{
		ID: didID,
		VerificationMethod: []did.VerificationMethod{{
			ID:                 kid,
			Type:               "Ed25519VerificationKey2020",
			Controller:         didID,
			PublicKeyMultibase: mb,
		}},
	}

	card := &a2a.AgentCard{
		ProtocolVersion:    "0.2.9",
		Name:               "test-agent",
		Description:        "unit test",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"application/json"},
	}

	signer, err := NewAgentCardSigner(didID, "https://example.com/.well-known/did.json", kid, priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	if err := signer.Sign(card, time.Unix(0, 0)); err != nil {
		t.Fatalf("sign card: %v", err)
	}

	resolver := &staticResolver{doc: doc}
	if err := VerifyAgentCard(context.Background(), card, resolver); err != nil {
		t.Fatalf("verify card: %v", err)
	}

	// Tamper with card and expect verification failure.
	cardCopy := *card
	cardCopy.Description = "tampered"
	if err := VerifyAgentCard(context.Background(), &cardCopy, resolver); err == nil {
		t.Fatalf("expected verification failure after tamper")
	}
}
