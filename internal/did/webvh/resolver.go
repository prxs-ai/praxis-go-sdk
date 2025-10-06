package webvh

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/multiformats/go-multibase"
	"github.com/praxis/praxis-go-sdk/internal/crypto"
	"github.com/praxis/praxis-go-sdk/internal/did"
	"github.com/praxis/praxis-go-sdk/internal/did/web"
	"golang.org/x/crypto/sha3"
)

// Resolver resolves did:webvh identifiers (web DID with verification hash).
type Resolver struct {
	WebResolver *web.Resolver
}

// Resolve implements did.Resolver for did:webvh identifiers.
func (r *Resolver) Resolve(ctx context.Context, identifier string) (*did.Document, error) {
	method, specific, err := did.BaseIdentifier(identifier)
	if err != nil {
		return nil, err
	}
	if method != "webvh" {
		return nil, did.ErrUnsupportedMethod
	}

	parts := strings.Split(specific, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("did:webvh: missing verification hash segment")
	}

	hashSpec := parts[len(parts)-1]
	baseSpecific := strings.Join(parts[:len(parts)-1], ":")
	baseDID := "did:web:" + baseSpecific

	if r.WebResolver == nil {
		return nil, fmt.Errorf("did:webvh resolver requires web resolver")
	}

	doc, rawDocument, err := r.WebResolver.ResolveRaw(ctx, baseDID)
	if err != nil {
		return nil, err
	}

	canonicalDoc, err := crypto.CanonicalizeRawJSON(rawDocument)
	if err != nil {
		return nil, fmt.Errorf("did:webvh canonicalization failed: %w", err)
	}

	algo, expectedDigest, err := parseHashSpec(hashSpec)
	if err != nil {
		return nil, err
	}

	var actual []byte
	switch algo {
	case "sha256":
		digest := sha256.Sum256(canonicalDoc)
		actual = digest[:]
	case "sha3-256":
		digest := sha3.Sum256(canonicalDoc)
		actual = digest[:]
	default:
		return nil, fmt.Errorf("did:webvh: unsupported hash algorithm %s", algo)
	}

	if subtle.ConstantTimeCompare(actual, expectedDigest) != 1 {
		return nil, did.ErrHashMismatch
	}

	if doc.ID == "" {
		doc.ID = identifier
	}
	return doc, nil
}

func parseHashSpec(spec string) (string, []byte, error) {
	if spec == "" {
		return "", nil, fmt.Errorf("did:webvh: empty hash spec")
	}
	pieces := strings.SplitN(spec, "-", 2)
	if len(pieces) != 2 {
		return "", nil, fmt.Errorf("did:webvh: invalid hash spec %s", spec)
	}
	algo := strings.ToLower(pieces[0])
	encoded := pieces[1]

	decoded, err := decodeHash(encoded)
	if err != nil {
		return "", nil, err
	}
	return algo, decoded, nil
}

func decodeHash(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, fmt.Errorf("did:webvh: empty hash value")
	}

	if prefix, _ := utf8.DecodeRuneInString(encoded); prefix != utf8.RuneError {
		if _, ok := multibase.EncodingToStr[multibase.Encoding(prefix)]; ok {
			if _, data, err := multibase.Decode(encoded); err == nil {
				return data, nil
			}
		}
	}

	base32Decoder := base32.StdEncoding.WithPadding(base32.NoPadding)
	if decoded, err := base32Decoder.DecodeString(strings.ToUpper(encoded)); err == nil {
		return decoded, nil
	}

	if decoded, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return decoded, nil
	}

	return nil, fmt.Errorf("did:webvh: unable to decode hash value")
}
