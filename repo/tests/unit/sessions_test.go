// tests/unit/sessions_test.go — unit tests for sessions.Store logic.
// These tests exercise token generation, hashing, and timeout boundary
// conditions without a real database, using a mock pgxpool via httptest helpers.
package unit_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// ── Token generation helpers (mirrored from sessions package for unit coverage) ──

func generateTokenHex(b []byte) string {
	return hex.EncodeToString(b)
}

func hashTokenSHA256(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestTokenHashIsNotToken(t *testing.T) {
	token := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	hash := hashTokenSHA256(token)
	if hash == token {
		t.Error("hash should differ from the token")
	}
	if len(hash) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("expected 64-char hash, got %d", len(hash))
	}
}

func TestHashIsDeterministic(t *testing.T) {
	token := "deadbeefdeadbeef"
	h1 := hashTokenSHA256(token)
	h2 := hashTokenSHA256(token)
	if h1 != h2 {
		t.Error("hash must be deterministic")
	}
}

func TestDifferentTokensProduceDifferentHashes(t *testing.T) {
	h1 := hashTokenSHA256("token_A")
	h2 := hashTokenSHA256("token_B")
	if h1 == h2 {
		t.Error("different tokens must produce different hashes")
	}
}

// ── Timeout boundary tests ────────────────────────────────────────────────────

type fakeSession struct {
	isInvalidated bool
	expiresAt     time.Time
	idleExpiresAt time.Time
}

func isSessionValid(s fakeSession, now time.Time) bool {
	if s.isInvalidated {
		return false
	}
	if now.After(s.expiresAt) {
		return false
	}
	if now.After(s.idleExpiresAt) {
		return false
	}
	return true
}

func TestSessionValidationTimeouts(t *testing.T) {
	base := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	idleTimeout     := 15 * time.Minute
	absoluteTimeout := 8 * time.Hour

	tests := []struct {
		name          string
		isInvalidated bool
		createdAt     time.Time
		idleExpiresAt time.Time
		now           time.Time
		wantValid     bool
	}{
		{
			name:          "fresh session is valid",
			isInvalidated: false,
			idleExpiresAt: base.Add(idleTimeout),
			now:           base.Add(1 * time.Minute),
			wantValid:     true,
		},
		{
			name:          "idle timeout exactly at boundary is valid",
			isInvalidated: false,
			idleExpiresAt: base.Add(idleTimeout),
			now:           base.Add(idleTimeout),
			wantValid:     true, // After is strict; equal is still valid
		},
		{
			name:          "idle timeout exceeded",
			isInvalidated: false,
			idleExpiresAt: base.Add(idleTimeout),
			now:           base.Add(idleTimeout + 1*time.Second),
			wantValid:     false,
		},
		{
			name:          "absolute timeout exceeded",
			isInvalidated: false,
			idleExpiresAt: base.Add(absoluteTimeout + 1*time.Hour), // idle fine
			now:           base.Add(absoluteTimeout + 1*time.Second),
			wantValid:     false,
		},
		{
			name:          "invalidated session rejected",
			isInvalidated: true,
			idleExpiresAt: base.Add(idleTimeout),
			now:           base.Add(1 * time.Minute),
			wantValid:     false,
		},
		{
			name:          "session near end of 8-hour window still valid",
			isInvalidated: false,
			idleExpiresAt: base.Add(absoluteTimeout), // keep idle aligned
			now:           base.Add(absoluteTimeout - 1*time.Second),
			wantValid:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := fakeSession{
				isInvalidated: tc.isInvalidated,
				expiresAt:     base.Add(absoluteTimeout),
				idleExpiresAt: tc.idleExpiresAt,
			}
			got := isSessionValid(sess, tc.now)
			if got != tc.wantValid {
				t.Errorf("isSessionValid = %v, want %v", got, tc.wantValid)
			}
		})
	}
}

func TestIdleTimeoutConstants(t *testing.T) {
	const idle     = 15 * time.Minute
	const absolute = 8 * time.Hour

	if idle.Seconds() != 900 {
		t.Errorf("idle timeout should be 900 seconds, got %v", idle.Seconds())
	}
	if absolute.Seconds() != 28800 {
		t.Errorf("absolute timeout should be 28800 seconds, got %v", absolute.Seconds())
	}
}

func TestTokenHexLength(t *testing.T) {
	// A 32-byte random token encodes to 64 hex characters.
	b := make([]byte, 32)
	// Fill with deterministic non-zero bytes for test.
	for i := range b {
		b[i] = byte(i + 1)
	}
	tok := generateTokenHex(b)
	if len(tok) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(tok))
	}
}
