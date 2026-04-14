// tests/api/route_registration_test.go — guards the real route-permission
// wiring in cmd/api/main.go against silent regressions.
//
// The handler-level tests in this package use in-memory fakes, which miss bugs
// in the registered middleware (for example, a write route guarded by a read
// permission). This file parses cmd/api/main.go as source and asserts that
// each security-critical route still pairs with the correct permission code.
package api_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// routeExpectation describes a single security-critical route registration we
// want to lock in: HTTP method, path suffix (matched anywhere in the line), and
// the permission code that must appear on the same line.
type routeExpectation struct {
	method       string
	pathSuffix   string
	wantContains string // substring that must appear on the registration line
	note         string
}

func TestMainRoutePermissionWiring(t *testing.T) {
	// Locate cmd/api/main.go relative to this test file.
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "api", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/api/main.go: %v", err)
	}
	text := string(src)

	// Expectations derived from the prompt's role matrix and the 26-permission
	// seed model. Each entry asserts the permission code present on the same
	// registration line; this catches regressions like guarding a write route
	// with a read permission.
	expectations := []routeExpectation{
		{
			method:       "POST",
			pathSuffix:   `"/reconciliation/runs"`,
			wantContains: `RequirePermission("reconciliation:write")`,
			note:         "creating a reconciliation run is a write operation",
		},
		{
			method:       "POST",
			pathSuffix:   `"/reconciliation/runs/:id/process"`,
			wantContains: `RequirePermission("reconciliation:write")`,
			note:         "processing a run mutates variances; must require write",
		},
		{
			method:       "POST",
			pathSuffix:   `"/reconciliation/variances/:id/approve"`,
			wantContains: `RequirePermission("writeoffs:approve")`,
			note:         "variance approval is a finance-only gate",
		},
		{
			method:       "POST",
			pathSuffix:   `"/reconciliation/batches/:id/approve"`,
			wantContains: `RequirePermission("settlements:write")`,
			note:         "batch approval mutates AR/AP and must be finance-scoped",
		},
		{
			method:       "POST",
			pathSuffix:   `"/procurement/orders"`,
			wantContains: `RequirePermission("orders:write")`,
			note:         "creating a vendor order is a write operation",
		},
		{
			method:       "POST",
			pathSuffix:   `"/procurement/orders/:id/approve"`,
			wantContains: `RequirePermission("orders:approve")`,
			note:         "segregation of duties: approve must require orders:approve, NOT orders:write",
		},
		{
			method:       "POST",
			pathSuffix:   `"/procurement/orders/:id/reject"`,
			wantContains: `RequirePermission("orders:approve")`,
			note:         "segregation of duties: reject must require orders:approve, NOT orders:write",
		},
		{
			method:       "POST",
			pathSuffix:   `"/reviews"`,
			wantContains: `RequirePermission("reviews:write")`,
			note:         "submitting a review is a write operation",
		},
		{
			method:       "POST",
			pathSuffix:   `"/catalog/resources"`,
			wantContains: `RequirePermission("catalog:write")`,
			note:         "creating a resource must require catalog:write, not catalog:read",
		},
		{
			method:       "PUT",
			pathSuffix:   `"/catalog/resources/:id"`,
			wantContains: `RequirePermission("catalog:write")`,
			note:         "editing a resource must require catalog:write",
		},
		{
			method:       "POST",
			pathSuffix:   `"/catalog/resources/:id/archive"`,
			wantContains: `RequirePermission("catalog:publish")`,
			note:         "archiving a resource is a lifecycle action gated by catalog:publish",
		},
	}

	for _, exp := range expectations {
		line, ok := findRouteLine(text, exp.method, exp.pathSuffix)
		if !ok {
			t.Errorf("no %s registration found for %s", exp.method, exp.pathSuffix)
			continue
		}
		if !strings.Contains(line, exp.wantContains) {
			t.Errorf("route %s %s: expected %q on the registration line (%s)\n  got: %s",
				exp.method, exp.pathSuffix, exp.wantContains, exp.note, strings.TrimSpace(line))
		}
	}
}

// findRouteLine returns the first line that looks like a registration of the
// given method + path. We match on `.METHOD("...path..."`, which is robust to
// formatting variations of the Echo route call.
func findRouteLine(src, method, pathLiteral string) (string, bool) {
	// e.g. \.POST\(\s*"path"
	pattern := regexp.MustCompile(`\.` + regexp.QuoteMeta(method) + `\(\s*` + regexp.QuoteMeta(pathLiteral))
	for _, line := range strings.Split(src, "\n") {
		if pattern.MatchString(line) {
			return line, true
		}
	}
	return "", false
}
