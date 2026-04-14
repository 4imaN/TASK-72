// Package sessions manages server-side session lifecycle stored in PostgreSQL.
package sessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// ConfigParamReader is a minimal interface for reading config_parameters values.
type ConfigParamReader interface {
	GetParam(ctx context.Context, key string) (string, error)
}

// LoadTimeouts reads session timeout configuration from config_parameters.
// Reads the seeded keys session.idle_timeout_seconds and session.max_timeout_seconds
// (900 and 28800 by default). Falls back to IdleTimeout / AbsoluteTimeout constants.
func LoadTimeouts(ctx context.Context, configStore ConfigParamReader) (idleTimeout, absoluteTimeout time.Duration, err error) {
	idleTimeout = IdleTimeout
	absoluteTimeout = AbsoluteTimeout

	if v, e := configStore.GetParam(ctx, "session.idle_timeout_seconds"); e == nil {
		if secs, e2 := strconv.Atoi(v); e2 == nil && secs > 0 {
			idleTimeout = time.Duration(secs) * time.Second
		}
	}

	if v, e := configStore.GetParam(ctx, "session.max_timeout_seconds"); e == nil {
		if secs, e2 := strconv.Atoi(v); e2 == nil && secs > 0 {
			absoluteTimeout = time.Duration(secs) * time.Second
		}
	}

	return idleTimeout, absoluteTimeout, nil
}

const (
	// CookieName is the HttpOnly session cookie name.
	CookieName = "portal_session"
	// IdleTimeout is the inactivity window before a session is invalidated.
	IdleTimeout = 15 * time.Minute
	// AbsoluteTimeout is the maximum session lifetime regardless of activity.
	AbsoluteTimeout = 8 * time.Hour
)

// Session holds the decoded session record from the database.
type Session struct {
	ID            string
	UserID        string
	CreatedAt     time.Time
	LastActiveAt  time.Time
	ExpiresAt     time.Time
	IdleExpiresAt time.Time
	IsInvalidated bool
	ClientVersion string
	MFAVerified   bool
}

// Store manages session persistence in PostgreSQL.
type Store struct {
	pool            *pgxpool.Pool
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
}

// NewStore constructs a Store backed by the given connection pool using default timeouts.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, idleTimeout: IdleTimeout, absoluteTimeout: AbsoluteTimeout}
}

// NewStoreWithTimeouts constructs a Store with config-driven timeout values.
func NewStoreWithTimeouts(pool *pgxpool.Pool, idleTimeout, absoluteTimeout time.Duration) *Store {
	return &Store{pool: pool, idleTimeout: idleTimeout, absoluteTimeout: absoluteTimeout}
}

// Create generates a new session, persists it, and returns the opaque token.
// The token is never stored; only its SHA-256 hash is kept in the database.
// mfaVerified should be false for newly-created sessions; it is set to true
// after the user completes the MFA challenge via SetMFAVerified.
func (s *Store) Create(ctx context.Context, userID, clientVersion, ipAddress, userAgent string, mfaVerified bool) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	tokenHash := hashToken(token)
	now := time.Now().UTC()
	expiresAt := now.Add(s.absoluteTimeout)
	idleExpiresAt := now.Add(s.idleTimeout)

	_, err = s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, created_at, last_active_at, expires_at, idle_expires_at, client_version, ip_address, user_agent, mfa_verified)
		VALUES ($1, $2, $3, $4, $4, $5, $6, $7, $8, $9, $10)`,
		uuid.New().String(), userID, tokenHash, now, expiresAt, idleExpiresAt,
		clientVersion, ipAddress, userAgent, mfaVerified,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return token, nil
}

// SetMFAVerified marks the given session as having passed the MFA challenge.
func (s *Store) SetMFAVerified(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET mfa_verified = TRUE WHERE id = $1`, sessionID)
	return err
}

// Validate looks up a session by token, enforces all timeout conditions, and
// extends the idle timer on success. Returns (nil, nil) when not found or expired.
func (s *Store) Validate(ctx context.Context, token string) (*Session, error) {
	tokenHash := hashToken(token)
	now := time.Now().UTC()

	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, created_at, last_active_at, expires_at, idle_expires_at, is_invalidated, coalesce(client_version, ''), coalesce(mfa_verified, FALSE)
		FROM sessions
		WHERE token_hash = $1`, tokenHash)

	var sess Session
	err := row.Scan(
		&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.LastActiveAt,
		&sess.ExpiresAt, &sess.IdleExpiresAt, &sess.IsInvalidated, &sess.ClientVersion, &sess.MFAVerified,
	)
	if err != nil {
		// Not found or scan error — treat as invalid.
		return nil, nil
	}

	if sess.IsInvalidated {
		return nil, nil
	}
	if now.After(sess.ExpiresAt) {
		return nil, nil
	}
	if now.After(sess.IdleExpiresAt) {
		return nil, nil
	}

	// Extend idle timeout on activity.
	newIdleExpiry := now.Add(s.idleTimeout)
	_, _ = s.pool.Exec(ctx, `
		UPDATE sessions SET last_active_at = $1, idle_expires_at = $2 WHERE id = $3`,
		now, newIdleExpiry, sess.ID,
	)

	return &sess, nil
}

// Invalidate marks a session as invalidated (used on logout).
func (s *Store) Invalidate(ctx context.Context, token string) error {
	tokenHash := hashToken(token)
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET is_invalidated = TRUE WHERE token_hash = $1`, tokenHash)
	return err
}

// SetCookie writes the session token as an HttpOnly, SameSite=Strict cookie
// using the package-level default timeout. Prefer (*Store).WriteCookie when
// the absolute timeout has been configured at runtime — that path mirrors
// the server-side expiry the Store uses, instead of the hard-coded constant.
func SetCookie(c echo.Context, token string) {
	writeCookie(c, token, AbsoluteTimeout)
}

// WriteCookie is the Store-bound cookie writer that honours the absolute
// timeout the Store was constructed with (via NewStoreWithTimeouts /
// LoadTimeouts). Use this anywhere Login/MFA verify hands the user a token.
func (s *Store) WriteCookie(c echo.Context, token string) {
	writeCookie(c, token, s.absoluteTimeout)
}

// AbsoluteLifetime returns the configured absolute timeout — exposed so
// callers (cookie writers, other handlers) can stay aligned with the store.
func (s *Store) AbsoluteLifetime() time.Duration {
	return s.absoluteTimeout
}

func writeCookie(c echo.Context, token string, lifetime time.Duration) {
	if lifetime <= 0 {
		lifetime = AbsoluteTimeout
	}
	cookie := new(http.Cookie)
	cookie.Name     = CookieName
	cookie.Value    = token
	cookie.Path     = "/"
	cookie.HttpOnly = true
	cookie.Secure   = false // offline deployment — no HTTPS
	cookie.SameSite = http.SameSiteStrictMode
	cookie.MaxAge   = int(lifetime.Seconds())
	c.SetCookie(cookie)
}

// ClearCookie removes the session cookie from the client.
func ClearCookie(c echo.Context) {
	cookie := new(http.Cookie)
	cookie.Name     = CookieName
	cookie.Value    = ""
	cookie.Path     = "/"
	cookie.HttpOnly = true
	cookie.MaxAge   = -1
	c.SetCookie(cookie)
}

// TokenFromRequest extracts the session token from the request cookie.
// Returns an empty string when no cookie is present.
func TokenFromRequest(c echo.Context) string {
	cookie, err := c.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return ""
	}
	return cookie.Value
}

// generateToken creates a cryptographically random 32-byte token encoded as hex.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA-256 hash of the token.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
