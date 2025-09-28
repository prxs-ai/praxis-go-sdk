package did

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/multiformats/go-multibase"
)

// FindVerificationMethod locates verification method with matching id.
func FindVerificationMethod(doc *Document, id string) (*VerificationMethod, error) {
	if doc == nil {
		return nil, fmt.Errorf("did: document is nil")
	}
	for i := range doc.VerificationMethod {
		vm := &doc.VerificationMethod[i]
		if vm.ID == id {
			return vm, nil
		}
	}
	return nil, ErrVerificationMethod
}

// ExtractEd25519PublicKey returns Ed25519 public key bytes from a verification method.
func ExtractEd25519PublicKey(vm *VerificationMethod) (ed25519.PublicKey, error) {
	if vm == nil {
		return nil, fmt.Errorf("did: verification method is nil")
	}

	if len(vm.PublicKeyJWK) > 0 {
		raw, err := json.Marshal(vm.PublicKeyJWK)
		if err != nil {
			return nil, err
		}
		key, err := jwk.ParseKey(raw)
		if err != nil {
			return nil, err
		}
		if key.KeyType() != "OKP" {
			return nil, ErrKeyFormatUnsupported
		}
		var pub ed25519.PublicKey
		if err := key.Raw(&pub); err != nil {
			return nil, err
		}
		return pub, nil
	}

	if vm.PublicKeyMultibase != "" {
		prefix, decoded, err := multibase.Decode(vm.PublicKeyMultibase)
		if err != nil {
			return nil, err
		}
		// If the decoder already returns raw bytes (without multicodec prefix)
		// we need to strip possible multicodec header 0xed 0x01 for Ed25519.
		if len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
		if len(decoded) == ed25519.PublicKeySize+2 && decoded[0] == 0xed && decoded[1] == 0x01 {
			return ed25519.PublicKey(decoded[2:]), nil
		}
		return nil, fmt.Errorf("did: unexpected multibase (prefix %v) key length %d", prefix, len(decoded))
	}

	return nil, ErrKeyFormatUnsupported
}

// DIDFromKID extracts base DID portion from a key id like "did:web:example#key-1".
func DIDFromKID(kid string) (string, error) {
	if kid == "" {
		return "", fmt.Errorf("did: kid is empty")
	}
	if !strings.Contains(kid, "#") {
		// Already a DID without fragment
		if strings.HasPrefix(kid, "did:") {
			return kid, nil
		}
		return "", fmt.Errorf("did: kid lacks fragment: %s", kid)
	}
	base := kid[:strings.Index(kid, "#")]
	if base == "" {
		return "", fmt.Errorf("did: malformed kid: %s", kid)
	}
	return base, nil
}

// DecodeDetachedPayload decodes base64url payload (used in tests/helpers).
func DecodeDetachedPayload(encoded string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(encoded)
}
