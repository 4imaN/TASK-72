// tests/unit/featureflag_test.go — covers the generalized phased-rollout gate
// used by search, recommendations, webhooks, and the compatibility middleware.
package unit_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"portal/internal/platform/featureflag"
)

// stubChecker is a trivial in-memory Checker that looks the flag up in a map.
// Role targeting: when the flag's value is nil the flag is disabled; when it's
// an empty slice the flag is on for everyone; when it's a non-empty slice the
// flag is on only for callers with at least one listed role.
type stubChecker struct {
	flags map[string][]string // flagKey → allowed role list; nil means disabled
}

func (s stubChecker) CheckFlag(_ context.Context, key string, userRoles []string) (bool, error) {
	target, ok := s.flags[key]
	if !ok || target == nil {
		return false, nil
	}
	if len(target) == 0 {
		return true, nil
	}
	for _, r := range userRoles {
		for _, t := range target {
			if r == t {
				return true, nil
			}
		}
	}
	return false, nil
}

// stubRoles is an in-memory RoleLookup.
type stubRoles map[string][]string

func (s stubRoles) RolesForUser(_ context.Context, userID string) ([]string, error) {
	return s[userID], nil
}

// newCtxWithUser returns an Echo context that carries a user_id (what the
// RequireAuth middleware would normally inject).
func newCtxWithUser(userID string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if userID != "" {
		c.Set("user_id", userID)
	}
	return c
}

func TestGate_OpenByDefaultWhenCheckerMissing(t *testing.T) {
	g := featureflag.New(nil, nil)
	c := newCtxWithUser("u1")
	if !g.EnabledFor(c, "any.flag") {
		t.Error("nil checker should be open by default (backward compatibility)")
	}
	if !g.EnabledGlobally(context.Background(), "any.flag") {
		t.Error("nil checker should be open globally by default")
	}
}

func TestGate_DisabledFlagBlocks(t *testing.T) {
	chk := stubChecker{flags: map[string][]string{"search.pinyin_expansion": nil}}
	g := featureflag.New(chk, stubRoles{})
	c := newCtxWithUser("u1")
	if g.EnabledFor(c, "search.pinyin_expansion") {
		t.Error("disabled flag must return false")
	}
}

func TestGate_EnabledForEveryone(t *testing.T) {
	chk := stubChecker{flags: map[string][]string{"recommendations.enabled": {}}}
	g := featureflag.New(chk, stubRoles{})
	c := newCtxWithUser("u1")
	if !g.EnabledFor(c, "recommendations.enabled") {
		t.Error("fully-enabled flag must return true even with no roles")
	}
}

func TestGate_RoleTargetingHonorsRollout(t *testing.T) {
	chk := stubChecker{flags: map[string][]string{
		"search.pinyin_expansion": {"admin", "finance"},
	}}
	roles := stubRoles{
		"admin-user":   {"admin"},
		"learner-user": {"learner"},
	}
	g := featureflag.New(chk, roles)

	if !g.EnabledFor(newCtxWithUser("admin-user"), "search.pinyin_expansion") {
		t.Error("admin user should see the targeted feature")
	}
	if g.EnabledFor(newCtxWithUser("learner-user"), "search.pinyin_expansion") {
		t.Error("learner user should NOT see the targeted feature")
	}
}

func TestGate_EnabledGloballyIgnoresRoles(t *testing.T) {
	// compatibility.check_enabled must be evaluable even before user_id is known
	// (it runs inside the auth middleware against the overall on/off state).
	chk := stubChecker{flags: map[string][]string{"compatibility.check_enabled": {}}}
	g := featureflag.New(chk, stubRoles{})
	if !g.EnabledGlobally(context.Background(), "compatibility.check_enabled") {
		t.Error("globally-enabled flag should report true without a user")
	}

	chkOff := stubChecker{flags: map[string][]string{"compatibility.check_enabled": nil}}
	gOff := featureflag.New(chkOff, stubRoles{})
	if gOff.EnabledGlobally(context.Background(), "compatibility.check_enabled") {
		t.Error("disabled flag should report false globally — kill-switch must work")
	}
}

func TestGate_MissingRolesDoesNotPanic(t *testing.T) {
	// When user_id is missing (anonymous / pre-auth), a fully-enabled flag
	// should still be true; a role-gated flag should fall through to false.
	chk := stubChecker{flags: map[string][]string{
		"everyone.flag":  {},
		"admin.only":     {"admin"},
	}}
	g := featureflag.New(chk, stubRoles{})
	c := newCtxWithUser("") // no user_id set

	if !g.EnabledFor(c, "everyone.flag") {
		t.Error("fully-enabled flag should apply to anonymous callers")
	}
	if g.EnabledFor(c, "admin.only") {
		t.Error("role-gated flag should not apply without a user")
	}
}
