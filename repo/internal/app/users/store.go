// Package users provides user lookup and mutation operations.
package users

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents the core user record.
type User struct {
	ID                 string
	Username           string
	Email              string
	DisplayName        string
	PasswordHash       string
	ForcePasswordReset bool
	IsActive           bool
	JobFamilyID        *int64
	LastLoginAt        *time.Time
}

// UserWithRoles extends User with resolved role and permission slices.
type UserWithRoles struct {
	User
	Roles       []string
	Permissions []string
}

// Store provides database operations on the users table.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// GetByUsername retrieves a user record by username.
// Returns an error (including pgx.ErrNoRows) if not found.
func (s *Store) GetByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, email, display_name, password_hash, force_password_reset, is_active
		FROM users WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.ForcePasswordReset, &u.IsActive)
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return &u, nil
}

// GetWithRoles retrieves a user by ID and eagerly loads their roles and permissions.
func (s *Store) GetWithRoles(ctx context.Context, userID string) (*UserWithRoles, error) {
	var u UserWithRoles
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, email, display_name, password_hash, force_password_reset, is_active, last_login_at
		FROM users WHERE id = $1`, userID).
		Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.ForcePasswordReset, &u.IsActive, &u.LastLoginAt)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT r.name FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("get roles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		u.Roles = append(u.Roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan roles: %w", err)
	}

	rows2, err := s.pool.Query(ctx, `
		SELECT DISTINCT p.code FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("get permissions: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var perm string
		if err := rows2.Scan(&perm); err != nil {
			return nil, err
		}
		u.Permissions = append(u.Permissions, perm)
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("scan permissions: %w", err)
	}

	return &u, nil
}

// RolesForUser returns just the role-name slice for a given user.
// Lighter-weight than GetWithRoles when permissions are not needed
// (used, for example, by feature-flag evaluation).
func (s *Store) RolesForUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.name FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("roles for user: %w", err)
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// UpdatePasswordHash sets a new bcrypt hash and clears force_password_reset.
func (s *Store) UpdatePasswordHash(ctx context.Context, userID, newHash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET password_hash = $1, force_password_reset = FALSE, updated_at = NOW()
		WHERE id = $2`, newHash, userID)
	return err
}

// UpdateLastLogin records the current time as last_login_at.
func (s *Store) UpdateLastLogin(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, userID)
	return err
}

// ListUsers returns all users with their roles, paginated. Also returns total count.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]UserWithRoles, int, error) {
	if limit <= 0 {
		limit = 50
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, username, email, display_name, password_hash, force_password_reset, is_active, last_login_at
		FROM users
		ORDER BY username
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var result []UserWithRoles
	for rows.Next() {
		var u UserWithRoles
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName,
			&u.PasswordHash, &u.ForcePasswordReset, &u.IsActive, &u.LastLoginAt); err != nil {
			return nil, 0, err
		}
		result = append(result, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan users: %w", err)
	}

	// Load roles for each user.
	for i := range result {
		uw, err := s.GetWithRoles(ctx, result[i].ID)
		if err != nil {
			continue
		}
		result[i].Roles = uw.Roles
		result[i].Permissions = uw.Permissions
	}

	if result == nil {
		result = []UserWithRoles{}
	}
	return result, total, nil
}

// UpdateUserRoles replaces all roles for the given user atomically.
func (s *Store) UpdateUserRoles(ctx context.Context, userID string, roles []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete existing roles.
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete user roles: %w", err)
	}

	// Insert new roles by name.
	for _, roleName := range roles {
		var roleID string
		err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, roleName).Scan(&roleID)
		if err != nil {
			return fmt.Errorf("role not found: %s", roleName)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
			return fmt.Errorf("insert user role: %w", err)
		}
	}

	return tx.Commit(ctx)
}
