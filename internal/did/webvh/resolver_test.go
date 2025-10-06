package webvh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/praxis/praxis-go-sdk/internal/crypto"
	"github.com/praxis/praxis-go-sdk/internal/did"
	"github.com/praxis/praxis-go-sdk/internal/did/web"
)

func TestResolverResolveSHA256(t *testing.T) {
	var docJSON string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/did.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if docJSON == "" {
			t.Fatal("docJSON not initialised")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, docJSON)
	}))
	defer ts.Close()

	hostWithPort := strings.TrimPrefix(ts.URL, "http://")
	didHost := strings.ReplaceAll(hostWithPort, ":", "%3A")
	didWeb := fmt.Sprintf("did:web:%s", didHost)
	serviceEndpoint := fmt.Sprintf("http://%s/.well-known/agent-card.json", hostWithPort)
	docJSON = fmt.Sprintf(`{"@context":["https://www.w3.org/ns/did/v1"],"id":"%s","verificationMethod":[{"id":"%s#key-1","type":"Ed25519VerificationKey2020","controller":"%s","publicKeyMultibase":"z6MktW7mqPgNmPqV9B9CLf2w8XMBwR4GwhTzQH8Jw7v3uh2m"}],"authentication":["%s#key-1"],"assertionMethod":["%s#key-1"],"service":[{"id":"%s#agent-card","type":"PraxisAgentCard","serviceEndpoint":"%s"}],"alsoKnownAs":["https://praxis.example/agent"]}`, didWeb, didWeb, didWeb, didWeb, didWeb, didWeb, serviceEndpoint)

	canonical, err := crypto.CanonicalizeRawJSON([]byte(docJSON))
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}

	digest := sha256.Sum256(canonical)
	b32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:])
	t.Logf("b32=%s", b32)
	didWebvh := fmt.Sprintf("did:webvh:%s:sha256-%s", didHost, strings.ToLower(b32))
	if algo, expectedDigest, err := parseHashSpec("sha256-" + strings.ToLower(b32)); err != nil {
		t.Fatalf("parse hash spec: %v", err)
	} else if algo != "sha256" {
		t.Fatalf("expected sha256 algo, got %s", algo)
	} else if !bytes.Equal(expectedDigest, digest[:]) {
		t.Fatalf("expected digest %x, got %x", digest[:], expectedDigest)
	}

	resolver := &Resolver{WebResolver: &web.Resolver{Client: ts.Client(), AllowInsecure: true}}
	_, raw, err := resolver.WebResolver.ResolveRaw(context.Background(), didWeb)
	if err != nil {
		t.Fatalf("resolve raw: %v", err)
	}
	actualCanonical, err := crypto.CanonicalizeRawJSON(raw)
	if err != nil {
		t.Fatalf("canonicalize raw: %v", err)
	}
	actualDigest := sha256.Sum256(actualCanonical)
	actualB32 := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(actualDigest[:])
	if strings.ToLower(actualB32) != strings.ToLower(b32) {
		t.Fatalf("preflight digest mismatch: expected %s, got %s", strings.ToLower(b32), strings.ToLower(actualB32))
	}

	doc, err := resolver.Resolve(context.Background(), didWebvh)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if doc == nil {
		t.Fatalf("expected document, got nil")
	}
	if doc.ID != didWeb {
		t.Fatalf("unexpected document id: %s", doc.ID)
	}

	invalidEncoded := []byte(strings.ToLower(b32))
	if invalidEncoded[0] == 'a' {
		invalidEncoded[0] = 'b'
	} else {
		invalidEncoded[0] = 'a'
	}
	invalid := fmt.Sprintf("did:webvh:%s:sha256-%s", didHost, string(invalidEncoded))
	if _, err := resolver.Resolve(context.Background(), invalid); !errors.Is(err, did.ErrHashMismatch) {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}
}
