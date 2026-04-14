// Package config provides store and handler for the configuration center.
package config

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConfigFlag represents a feature flag row.
type ConfigFlag struct {
	Key               string    `json:"key"`
	Enabled           bool      `json:"enabled"`
	Description       string    `json:"description,omitempty"`
	RolloutPercentage int       `json:"rollout_percentage"`
	TargetRoles       []string  `json:"target_roles"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ConfigParam represents a configuration parameter row.
type ConfigParam struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ClientVersionRule represents a client version compatibility rule.
type ClientVersionRule struct {
	MinVersion string     `json:"min_version"`
	MaxVersion string     `json:"max_version,omitempty"`
	Action     string     `json:"action"` // block, warn, read_only
	Message    string     `json:"message,omitempty"`
	GraceUntil *time.Time `json:"grace_until,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// RolloutRule represents a per-role feature flag override.
type RolloutRule struct {
	FlagKey     string `json:"flag_key"`
	RoleName    string `json:"role_name"`
	EnabledForRole bool `json:"enabled_for_role"`
}

// Store provides database operations for configuration.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ListFlags returns all config flags with their rollout settings.
func (s *Store) ListFlags(ctx context.Context) ([]ConfigFlag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT flag_key, flag_value, coalesce(description,''),
		       coalesce(rollout_percentage, 100),
		       coalesce(target_roles, '{}'),
		       updated_at
		FROM config_flags
		ORDER BY flag_key`)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close()

	var flags []ConfigFlag
	for rows.Next() {
		var f ConfigFlag
		if err := rows.Scan(&f.Key, &f.Enabled, &f.Description,
			&f.RolloutPercentage, &f.TargetRoles, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if f.TargetRoles == nil {
			f.TargetRoles = []string{}
		}
		flags = append(flags, f)
	}
	if flags == nil {
		flags = []ConfigFlag{}
	}
	return flags, nil
}

// GetFlag returns a single config flag by key.
func (s *Store) GetFlag(ctx context.Context, key string) (*ConfigFlag, error) {
	var f ConfigFlag
	err := s.pool.QueryRow(ctx, `
		SELECT flag_key, flag_value, coalesce(description,''),
		       coalesce(rollout_percentage, 100),
		       coalesce(target_roles, '{}'),
		       updated_at
		FROM config_flags WHERE flag_key = $1`, key).
		Scan(&f.Key, &f.Enabled, &f.Description,
			&f.RolloutPercentage, &f.TargetRoles, &f.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("flag not found: %s", key)
		}
		return nil, err
	}
	if f.TargetRoles == nil {
		f.TargetRoles = []string{}
	}
	return &f, nil
}

// SetFlag upserts a config flag.
func (s *Store) SetFlag(ctx context.Context, key string, enabled bool, rolloutPct int, targetRoles []string) error {
	if targetRoles == nil {
		targetRoles = []string{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config_flags (flag_key, flag_value, rollout_percentage, target_roles, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (flag_key) DO UPDATE SET
			flag_value         = EXCLUDED.flag_value,
			rollout_percentage = EXCLUDED.rollout_percentage,
			target_roles       = EXCLUDED.target_roles,
			updated_at         = NOW()`,
		key, enabled, rolloutPct, targetRoles)
	return err
}

// GetParam returns a single config parameter value by key.
// Returns ("", pgx.ErrNoRows) if the key does not exist.
func (s *Store) GetParam(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		`SELECT param_value FROM config_parameters WHERE param_key = $1`, key).
		Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// ListParams returns all config parameters.
func (s *Store) ListParams(ctx context.Context) ([]ConfigParam, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT param_key, param_value, coalesce(description,''), updated_at
		FROM config_parameters
		ORDER BY param_key`)
	if err != nil {
		return nil, fmt.Errorf("list params: %w", err)
	}
	defer rows.Close()

	var params []ConfigParam
	for rows.Next() {
		var p ConfigParam
		if err := rows.Scan(&p.Key, &p.Value, &p.Description, &p.UpdatedAt); err != nil {
			return nil, err
		}
		params = append(params, p)
	}
	if params == nil {
		params = []ConfigParam{}
	}
	return params, nil
}

// SetParam upserts a config parameter.
func (s *Store) SetParam(ctx context.Context, key, value, description string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config_parameters (param_key, param_value, value_type, description, updated_at)
		VALUES ($1, $2, 'string', $3, NOW())
		ON CONFLICT (param_key) DO UPDATE SET
			param_value = EXCLUDED.param_value,
			description = CASE WHEN $3 = '' THEN config_parameters.description ELSE EXCLUDED.description END,
			updated_at  = NOW()`,
		key, value, description)
	return err
}

// ListVersionRules returns all client version rules.
func (s *Store) ListVersionRules(ctx context.Context) ([]ClientVersionRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT min_version,
		       coalesce(max_version,''),
		       is_blocked,
		       coalesce(action, CASE WHEN is_blocked THEN 'block' ELSE 'warn' END),
		       grace_until,
		       coalesce(message, description, ''),
		       created_at
		FROM client_version_rules
		ORDER BY min_version`)
	if err != nil {
		return nil, fmt.Errorf("list version rules: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	var rules []ClientVersionRule
	for rows.Next() {
		var minVersion, maxVersion, action, message string
		var isBlocked bool
		var graceUntil *time.Time
		var createdAt time.Time
		if err := rows.Scan(&minVersion, &maxVersion, &isBlocked, &action, &graceUntil, &message, &createdAt); err != nil {
			return nil, err
		}
		// Apply the same grace-window downgrade shown by EvaluateClientVersion
		// so list output matches the effective action clients would see.
		if action == "block" && graceUntil != nil && graceUntil.After(now) {
			action = "read_only"
		}
		rules = append(rules, ClientVersionRule{
			MinVersion: minVersion,
			MaxVersion: maxVersion,
			Action:     action,
			Message:    message,
			GraceUntil: graceUntil,
			CreatedAt:  createdAt,
		})
	}
	if rules == nil {
		rules = []ClientVersionRule{}
	}
	return rules, nil
}

// SetVersionRule upserts a version rule, persisting max_version, action,
// message, and grace_until (columns added by migration 006 plus the original
// grace_until from migration 001). is_blocked is kept in sync with action for
// legacy readers. Pass a zero graceUntil to clear the grace window.
func (s *Store) SetVersionRule(ctx context.Context, minVersion, maxVersion, action, message string, graceUntil time.Time) error {
	if action == "" {
		action = "block"
	}
	switch action {
	case "block", "warn", "read_only":
	default:
		return fmt.Errorf("invalid action %q (expected block, warn, or read_only)", action)
	}
	isBlocked := action == "block" || action == "read_only"

	// Convert a zero time to NULL so callers can clear the grace window.
	var graceArg any
	if graceUntil.IsZero() {
		graceArg = nil
	} else {
		graceArg = graceUntil.UTC()
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO client_version_rules (min_version, max_version, is_blocked, action, message, description, grace_until)
		VALUES ($1, NULLIF($2,''), $3, $4, NULLIF($5,''), NULLIF($5,''), $6)
		ON CONFLICT (min_version) DO UPDATE SET
			max_version = EXCLUDED.max_version,
			is_blocked  = EXCLUDED.is_blocked,
			action      = EXCLUDED.action,
			message     = EXCLUDED.message,
			description = EXCLUDED.description,
			grace_until = EXCLUDED.grace_until`,
		minVersion, maxVersion, isBlocked, action, message, graceArg)
	return err
}

// EvaluateClientVersion returns the compatibility rule that applies to the
// caller's client version, or nil if the client is current.
//
// Semantics (matches the "minimum supported version" model in the README):
//   - min_version is the smallest version that is still supported.
//   - Clients BELOW min_version are unsupported and receive the rule's action
//     (typically block or read_only).
//   - When grace_until > NOW() and the action is block, it is downgraded to
//     read_only so the UI shows the grace banner.
//   - If max_version is set, the rule only applies when clientVersion < max_version
//     (lets admins retire old rules without deleting them).
func (s *Store) EvaluateClientVersion(ctx context.Context, clientVersion string) (*ClientVersionRule, error) {
	if clientVersion == "" {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT min_version,
		       coalesce(max_version,''),
		       is_blocked,
		       coalesce(action, CASE WHEN is_blocked THEN 'block' ELSE 'warn' END),
		       grace_until,
		       coalesce(message, description, ''),
		       created_at
		FROM client_version_rules
		ORDER BY min_version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	for rows.Next() {
		var minVersion, maxVersion, action, message string
		var isBlocked bool
		var graceUntil *time.Time
		var createdAt time.Time

		if err := rows.Scan(&minVersion, &maxVersion, &isBlocked, &action, &graceUntil, &message, &createdAt); err != nil {
			continue
		}

		// Rule applies when clientVersion < min_version (i.e. the client is
		// BELOW the minimum supported version).
		if semverGTE(clientVersion, minVersion) {
			continue
		}
		// Optional upper bound: skip rule if clientVersion >= max_version.
		if maxVersion != "" && semverGTE(clientVersion, maxVersion) {
			continue
		}

		// Within grace period, downgrade a hard block to read_only.
		if action == "block" && graceUntil != nil && graceUntil.After(now) {
			action = "read_only"
		}

		return &ClientVersionRule{
			MinVersion: minVersion,
			MaxVersion: maxVersion,
			Action:     action,
			Message:    message,
			GraceUntil: graceUntil,
			CreatedAt:  createdAt,
		}, nil
	}
	return nil, nil
}

// CheckFlag returns true if the flag is enabled and the user's role is covered.
func (s *Store) CheckFlag(ctx context.Context, flagKey string, userRoles []string) (bool, error) {
	flag, err := s.GetFlag(ctx, flagKey)
	if err != nil {
		return false, err
	}
	if !flag.Enabled {
		return false, nil
	}
	if flag.RolloutPercentage >= 100 {
		return true, nil
	}
	for _, role := range userRoles {
		for _, target := range flag.TargetRoles {
			if strings.EqualFold(role, target) {
				return true, nil
			}
		}
	}
	return false, nil
}

// parseSemver splits "major.minor.patch" into [3]int.
func parseSemver(v string) [3]int {
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		result[i], _ = strconv.Atoi(p)
	}
	return result
}

// semverGTE returns true if version v >= minV (numeric component-wise).
func semverGTE(v, minV string) bool {
	va := parseSemver(v)
	lo := parseSemver(minV)
	for i := 0; i < 3; i++ {
		if va[i] > lo[i] {
			return true
		}
		if va[i] < lo[i] {
			return false
		}
	}
	return true // equal
}

// semverInRange returns true if minV <= v <= maxV (numeric component-wise).
// Empty maxV means no upper bound.
func semverInRange(version, minV, maxV string) bool {
	if !semverGTE(version, minV) {
		return false
	}
	if maxV != "" && !semverGTE(maxV, version) {
		return false
	}
	return true
}
