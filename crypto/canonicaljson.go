package crypto

import (
	"encoding/json"

	canonicaljson "github.com/gibson042/canonicaljson-go"
)

// MarshalCanonical marshals the given value to canonical JSON bytes (RFC-style JCS).
func MarshalCanonical(v any) ([]byte, error) {
	return canonicaljson.Marshal(v)
}

// CanonicalizeRawJSON canonicalizes raw JSON bytes using the same JCS rules that
// MarshalCanonical applies to Go values. It preserves unknown fields that may not
// be represented in strongly-typed structs, which is important for hashing DID
// documents verbatim (e.g. for did:webvh verification).
func CanonicalizeRawJSON(data []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return canonicaljson.Marshal(v)
}
