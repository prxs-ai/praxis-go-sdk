package crypto

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"

	"github.com/praxis/praxis-go-sdk/internal/a2a"
	"github.com/praxis/praxis-go-sdk/internal/did"
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

// Sign mutates the provided card by injecting DID metadata and JWS signature.
func (s *AgentCardSigner) Sign(card *a2a.AgentCard, now time.Time) error {
	if card == nil {
		return fmt.Errorf("agent card signer: card is nil")
	}

	// Prepare payload without signatures for deterministic signing.
	payloadCard := *card
	payloadCard.Signatures = nil
	payloadCard.DID = s.did
	payloadCard.DIDDocURI = s.didDocURI

	payload, err := MarshalCanonical(&payloadCard)
	if err != nil {
		return fmt.Errorf("agent card signer: canonicalize payload: %w", err)
	}

	hdr := jws.NewHeaders()
	if err := hdr.Set(jws.AlgorithmKey, jwa.EdDSA); err != nil {
		return err
	}
	if err := hdr.Set(jws.KeyIDKey, s.keyID); err != nil {
		return err
	}
	if err := hdr.Set(jws.TypeKey, "application/prxs-agent-card+jws"); err != nil {
		return err
	}
	if err := hdr.Set("ts", now.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if card.Version != "" {
		if err := hdr.Set("cardVersion", card.Version); err != nil {
			return err
		}
	} else if card.ProtocolVersion != "" {
		if err := hdr.Set("cardVersion", card.ProtocolVersion); err != nil {
			return err
		}
	}

	signed, err := jws.Sign(nil,
		jws.WithKey(jwa.EdDSA, s.privateKey, jws.WithProtectedHeaders(hdr)),
		jws.WithDetachedPayload(payload),
		jws.WithJSON(),
	)
	if err != nil {
		return fmt.Errorf("agent card signer: sign: %w", err)
	}

	var envelope struct {
		Payload    string `json:"payload"`
		Signatures []struct {
			Protected string                 `json:"protected"`
			Signature string                 `json:"signature"`
			Header    map[string]interface{} `json:"header,omitempty"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(signed, &envelope); err != nil {
		return fmt.Errorf("agent card signer: parse signed envelope: %w", err)
	}

	var sigProtected string
	var sigValue string
	var sigHeader map[string]interface{}
	if len(envelope.Signatures) > 0 {
		sigProtected = envelope.Signatures[0].Protected
		sigValue = envelope.Signatures[0].Signature
		sigHeader = envelope.Signatures[0].Header
	} else {
		var flattened struct {
			Payload   string                 `json:"payload"`
			Protected string                 `json:"protected"`
			Signature string                 `json:"signature"`
			Header    map[string]interface{} `json:"header,omitempty"`
		}
		if err := json.Unmarshal(signed, &flattened); err != nil {
			return fmt.Errorf("agent card signer: envelope missing signatures")
		}
		sigProtected = flattened.Protected
		sigValue = flattened.Signature
		sigHeader = flattened.Header
	}

	if sigProtected == "" || sigValue == "" {
		return fmt.Errorf("agent card signer: envelope missing signatures")
	}

	card.DID = s.did
	card.DIDDocURI = s.didDocURI
	card.Signatures = []a2a.AgentCardSignature{
		{
			Protected: sigProtected,
			Signature: sigValue,
			Header:    sigHeader,
		},
	}

	return nil
}

// VerifyAgentCard verifies all signatures on the provided card using DID resolution.
func VerifyAgentCard(ctx context.Context, card *a2a.AgentCard, resolver did.Resolver) error {
	if card == nil {
		return fmt.Errorf("agent card verify: card is nil")
	}
	if resolver == nil {
		return fmt.Errorf("agent card verify: resolver is nil")
	}
	if len(card.Signatures) == 0 {
		return fmt.Errorf("agent card verify: no signatures present")
	}

	payloadCard := *card
	payloadCard.Signatures = nil

	payload, err := MarshalCanonical(&payloadCard)
	if err != nil {
		return fmt.Errorf("agent card verify: canonicalize payload: %w", err)
	}

	for idx, sig := range card.Signatures {
		headerJSON, err := base64.RawURLEncoding.DecodeString(sig.Protected)
		if err != nil {
			return fmt.Errorf("agent card verify: signature %d invalid protected header: %w", idx, err)
		}

		var header map[string]interface{}
		if err := json.Unmarshal(headerJSON, &header); err != nil {
			return fmt.Errorf("agent card verify: signature %d header decode: %w", idx, err)
		}

		alg, _ := header[jws.AlgorithmKey].(string)
		if !strings.EqualFold(alg, string(jwa.EdDSA)) {
			return fmt.Errorf("agent card verify: signature %d unsupported alg %v", idx, alg)
		}

		kid, _ := header[jws.KeyIDKey].(string)
		if kid == "" {
			return fmt.Errorf("agent card verify: signature %d missing kid", idx)
		}
		baseDID, err := did.DIDFromKID(kid)
		if err != nil {
			return fmt.Errorf("agent card verify: signature %d kid: %w", idx, err)
		}

		doc, err := resolver.Resolve(ctx, baseDID)
		if err != nil {
			return fmt.Errorf("agent card verify: resolve DID %s: %w", baseDID, err)
		}

		vm, err := did.FindVerificationMethod(doc, kid)
		if err != nil {
			return fmt.Errorf("agent card verify: lookup vm %s: %w", kid, err)
		}

		pubKey, err := did.ExtractEd25519PublicKey(vm)
		if err != nil {
			return fmt.Errorf("agent card verify: extract key %s: %w", vm.ID, err)
		}

		jwkKey, err := jwk.FromRaw(pubKey)
		if err != nil {
			return fmt.Errorf("agent card verify: create jwk: %w", err)
		}
		if err := jwkKey.Set(jwk.AlgorithmKey, jwa.EdDSA); err != nil {
			return fmt.Errorf("agent card verify: set alg: %w", err)
		}

		envelope := map[string]interface{}{
			"payload": "",
			"signatures": []map[string]interface{}{{
				"protected": sig.Protected,
				"signature": sig.Signature,
			}},
		}

		envelopeBytes, err := json.Marshal(envelope)
		if err != nil {
			return fmt.Errorf("agent card verify: marshal envelope: %w", err)
		}

		if _, err := jws.Verify(
			envelopeBytes,
			jws.WithKey(jwa.EdDSA, jwkKey),
			jws.WithDetachedPayload(payload),
		); err != nil {
			return fmt.Errorf("agent card verify: signature %d invalid: %w", idx, err)
		}
	}
	return nil
}

// ExtractSignatureTimestamp returns time from protected header, if present.
func ExtractSignatureTimestamp(sig a2a.AgentCardSignature) (time.Time, error) {
	headerJSON, err := base64.RawURLEncoding.DecodeString(sig.Protected)
	if err != nil {
		return time.Time{}, err
	}
	var header map[string]interface{}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return time.Time{}, err
	}
	tsValue, ok := header["ts"].(string)
	if !ok || tsValue == "" {
		return time.Time{}, fmt.Errorf("ts not present")
	}
	t, err := time.Parse(time.RFC3339, tsValue)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
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
