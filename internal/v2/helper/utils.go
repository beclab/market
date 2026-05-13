package helper

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateRandomHash returns a random 64-character lowercase hex
// string. The output shape matches a SHA-256 digest (32 bytes
// hex-encoded), making it suitable as a drop-in fixture for the
// `hash` fields surfaced by /market/hash and the embedded
// Others.Hash on market_sources.data.
//
// Backed by crypto/rand for unbiased, non-predictable test data.
// Returns empty string only if crypto/rand fails (it does not in
// practice on supported platforms; the defensive branch keeps the
// signature simple for callers that just want a non-empty random
// string).
func GenerateRandomHash() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
