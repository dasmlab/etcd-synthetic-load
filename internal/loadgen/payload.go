package loadgen

import (
	"crypto/sha256"
	"encoding/base64"
)

// GeneratePayload deterministically generates n bytes of base64-alphabet
// filler data derived from seed. It is not cryptographically random and is
// NOT suitable as a real secret value - it exists purely to occupy etcd
// storage space in a realistic-looking (base64-ish) shape for load testing.
// Determinism (same seed -> same bytes) makes `load` runs reproducible and
// idempotent-friendly.
func GeneratePayload(n int, seed string) []byte {
	if n <= 0 {
		return []byte{}
	}
	h := sha256.Sum256([]byte(seed))
	raw := make([]byte, 0, n+sha256.Size)
	for len(raw)*4/3 < n { // base64 expands by ~4/3
		raw = append(raw, h[:]...)
		h = sha256.Sum256(h[:])
	}
	encoded := base64.RawStdEncoding.EncodeToString(raw)
	if len(encoded) > n {
		encoded = encoded[:n]
	}
	for len(encoded) < n {
		encoded += "="
	}
	return []byte(encoded)
}
