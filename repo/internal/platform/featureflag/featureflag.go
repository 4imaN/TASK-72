// Package featureflag centralises the feature-flag evaluation pattern used by
// the admin Config Center. It exposes the minimal interfaces that handlers and
// middleware need to consult the flag store at request time without importing
// internal/app/config directly (which would cause import cycles for any
// package config.Store already depends on).
//
// Flags that gate capabilities are stored in the config_flags table, seeded in
// seeds/001_bootstrap.sql. Phased rollout uses rollout_percentage and
// target_roles columns (see migration 006).
package featureflag

import (
	"context"

	"github.com/labstack/echo/v4"
)

// Checker evaluates a flag for a caller's role set. Implemented by
// internal/app/config.Store.CheckFlag.
type Checker interface {
	CheckFlag(ctx context.Context, flagKey string, userRoles []string) (bool, error)
}

// RoleLookup resolves a user's roles by ID at request time. Implemented by
// internal/app/users.Store.RolesForUser.
type RoleLookup interface {
	RolesForUser(ctx context.Context, userID string) ([]string, error)
}

// Gate is a small holder that bundles a Checker + a RoleLookup so handlers can
// ask a single question ("is flag X enabled for the caller?") without repeating
// the boilerplate in every package.
type Gate struct {
	flags Checker
	roles RoleLookup
}

// New returns a Gate. Either dependency may be nil; when flags is nil the gate
// always reports "enabled=true" (open by default — appropriate for older tests
// and handlers wired without a real store).
func New(flags Checker, roles RoleLookup) *Gate {
	return &Gate{flags: flags, roles: roles}
}

// EnabledFor resolves the caller's roles from the Echo context (via user_id set
// by the auth middleware) and returns whether the flag is enabled for them.
// If the gate has no Checker, EnabledFor returns true so that a missing
// config-center wire-up does not silently disable features.
func (g *Gate) EnabledFor(c echo.Context, flagKey string) bool {
	if g == nil || g.flags == nil {
		return true
	}
	var userRoles []string
	if g.roles != nil {
		if userID, _ := c.Get("user_id").(string); userID != "" {
			if rs, err := g.roles.RolesForUser(c.Request().Context(), userID); err == nil {
				userRoles = rs
			}
		}
	}
	enabled, err := g.flags.CheckFlag(c.Request().Context(), flagKey, userRoles)
	if err != nil {
		return false
	}
	return enabled
}

// EnabledGlobally checks a flag without any role context. Appropriate for
// capabilities that are either on or off system-wide (for example, the
// client-version compatibility enforcement kill-switch).
func (g *Gate) EnabledGlobally(ctx context.Context, flagKey string) bool {
	if g == nil || g.flags == nil {
		return true
	}
	enabled, err := g.flags.CheckFlag(ctx, flagKey, nil)
	if err != nil {
		return false
	}
	return enabled
}
