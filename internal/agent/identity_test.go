package agent

import (
	"crypto/ed25519"
	crand "crypto/rand"
	"testing"

	"github.com/praxis/praxis-go-sdk/internal/config"
)

func TestBuildDIDDocumentAddsEd25519Context(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	doc, _, err := buildDIDDocument("did:web:example", "did:web:example#key-1", pub, config.AgentConfig{URL: "https://example"}, "")
	if err != nil {
		t.Fatalf("build doc: %v", err)
	}

	if len(doc.Context) < 2 {
		t.Fatalf("expected at least two context entries, got %#v", doc.Context)
	}

	found := false
	for _, ctx := range doc.Context {
		if s, ok := ctx.(string); ok && s == ed25519VerificationContext {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected context %s, got %#v", ed25519VerificationContext, doc.Context)
	}
}
