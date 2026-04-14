// Package integration_test provides a real-DB harness for integration tests.
//
// The existing tests/api and tests/security packages use in-memory fakes —
// fast, hermetic, but they cannot catch SQL/schema/wiring defects. This package
// runs the same handlers against a real PostgreSQL instance with the real
// migrations + seeds applied, so RBAC, object authorization, state machines,
// and SQL constraints are all exercised end-to-end.
//
// Tests in this package SKIP when INTEGRATION_DATABASE_URL is not set, so
// developers without a local DB can still run `go test ./...` cleanly. CI
// (or anyone running `make test-integration`) sets the env var to a clean
// throwaway database.
//
// Usage in a test:
//
//	func TestSomething(t *testing.T) {
//	    h := integration.Setup(t)
//	    defer h.Cleanup()
//	    // … h.Pool, h.Echo, h.NewUser(...) etc.
//	}
package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// Harness bundles the moving parts a test needs: a connection pool against a
// freshly-prepared schema and a partially-built Echo router that registers the
// handlers the test wants to exercise.
type Harness struct {
	t    *testing.T
	Pool *pgxpool.Pool
	Echo *echo.Echo
	// schemaName is the per-test PostgreSQL schema we install everything into.
	// Cleanup drops the schema so tests run in parallel without bleeding state.
	schemaName string
}

// Setup connects to INTEGRATION_DATABASE_URL, creates a fresh per-test schema,
// applies all migrations + seeds, and returns a ready-to-use Harness. The test
// is skipped when the env var is empty.
//
// Migrations and seeds run inside the per-test schema (search_path) so multiple
// integration tests can run concurrently against the same database without
// conflicting on table names.
func Setup(t *testing.T) *Harness {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set — skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to integration DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping integration DB: %v", err)
	}

	// Per-test schema isolation. uuid → 8-char prefix is enough — collisions
	// across concurrent tests within one minute are vanishingly unlikely.
	schemaName := "it_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schemaName)); err != nil {
		pool.Close()
		t.Fatalf("create schema: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`SET search_path TO %s, public`, schemaName)); err != nil {
		_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA %s CASCADE`, schemaName))
		pool.Close()
		t.Fatalf("set search_path: %v", err)
	}

	h := &Harness{
		t:          t,
		Pool:       pool,
		Echo:       echo.New(),
		schemaName: schemaName,
	}

	if err := h.applyMigrations(ctx); err != nil {
		h.Cleanup()
		t.Fatalf("apply migrations: %v", err)
	}
	if err := h.applySeeds(ctx); err != nil {
		h.Cleanup()
		t.Fatalf("apply seeds: %v", err)
	}
	return h
}

// Cleanup drops the per-test schema and closes the pool.
func (h *Harness) Cleanup() {
	if h.Pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = h.Pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, h.schemaName))
	h.Pool.Close()
	h.Pool = nil
}

// repoRoot resolves the repository root from the location of this file so the
// test can find migrations/ and seeds/ regardless of the test's working dir.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func (h *Harness) applyMigrations(ctx context.Context) error {
	dir := filepath.Join(repoRoot(), "migrations")
	return h.applySQLDir(ctx, dir, ".sql")
}

func (h *Harness) applySeeds(ctx context.Context) error {
	dir := filepath.Join(repoRoot(), "seeds")
	return h.applySQLDir(ctx, dir, ".sql")
}

func (h *Harness) applySQLDir(ctx context.Context, dir, suffix string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files) // 001_, 002_, … run in order
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := h.Pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
	}
	return nil
}

// MakeUser inserts a user with bcrypt-hashed password and the given role(s),
// returning the new user's ID. Useful to set up fixtures for RBAC tests.
func (h *Harness) MakeUser(ctx context.Context, username, password string, roleNames ...string) string {
	h.t.Helper()
	id := uuid.New().String()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		h.t.Fatalf("hash password: %v", err)
	}
	if _, err := h.Pool.Exec(ctx, `
		INSERT INTO users (id, username, email, display_name, password_hash, is_active, force_password_reset)
		VALUES ($1, $2, $3, $2, $4, TRUE, FALSE)`,
		id, username, username+"@example.test", string(hash),
	); err != nil {
		h.t.Fatalf("insert user %s: %v", username, err)
	}
	for _, r := range roleNames {
		if _, err := h.Pool.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1::uuid, id FROM roles WHERE name = $2
			ON CONFLICT DO NOTHING`, id, r); err != nil {
			h.t.Fatalf("assign role %s: %v", r, err)
		}
	}
	return id
}

// SeedSession inserts a session row directly and returns the opaque token the
// client would send. Skips the login flow so RBAC tests do not need to bring up
// the full auth stack.
func (h *Harness) SeedSession(ctx context.Context, userID string, mfaVerified bool) string {
	h.t.Helper()
	token := uuid.New().String() + uuid.New().String() // 64-char opaque token
	tokenHash := sha256Hex(token)
	now := time.Now().UTC()
	_, err := h.Pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, created_at, last_active_at,
		                      expires_at, idle_expires_at, mfa_verified)
		VALUES ($1, $2, $3, $4, $4, $4 + interval '8 hours', $4 + interval '15 minutes', $5)`,
		uuid.New().String(), userID, tokenHash, now, mfaVerified,
	)
	if err != nil {
		h.t.Fatalf("seed session: %v", err)
	}
	return token
}

// AuditCount returns the number of audit_logs rows matching action prefix +
// optional target_id. Tests use this to confirm a privileged write actually
// recorded an audit row, not just succeeded.
func (h *Harness) AuditCount(ctx context.Context, actionPrefix, targetID string) int {
	h.t.Helper()
	q := `SELECT COUNT(*) FROM audit_logs WHERE action LIKE $1`
	args := []any{actionPrefix + "%"}
	if targetID != "" {
		q += ` AND target_id = $2`
		args = append(args, targetID)
	}
	var n int
	if err := h.Pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		h.t.Fatalf("audit count: %v", err)
	}
	return n
}
