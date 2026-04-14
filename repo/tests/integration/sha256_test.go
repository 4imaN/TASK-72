package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256Hex matches the hash format sessions.Store uses internally so seeded
// session tokens validate cleanly through the real Validate path.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
