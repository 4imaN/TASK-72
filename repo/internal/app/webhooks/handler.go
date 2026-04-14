// Package webhooks — HTTP handlers for webhook endpoint management.
package webhooks

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"

	"portal/internal/app/common"
	"portal/internal/platform/featureflag"
)

// ValidateLANURLForTest exposes validateLANURL for unit tests so the
// LAN-only validator can be exercised in isolation. Production callers
// should keep using the unexported validateLANURL inside this package.
func ValidateLANURLForTest(raw string, hostnameAllowlist []string) error {
	return validateLANURL(raw, hostnameAllowlist)
}

// validateLANURL enforces the offline/LAN-only contract on webhook
// destinations: only http:// (HTTPS would require certificate management this
// portal does not provide) targeting localhost, an RFC1918 / link-local /
// unique-local IP, or a hostname in the optional allowlist. Returns a
// human-readable reason on rejection.
func validateLANURL(raw string, hostnameAllowlist []string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL must include a host")
	}

	// Hostname allowlist short-circuits the IP check (lets ops point at e.g.
	// "internal-erp.corp" without resolving DNS at write time).
	for _, allowed := range hostnameAllowlist {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return nil
		}
	}

	// Loopback names are always permitted.
	if strings.EqualFold(host, "localhost") {
		return nil
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname not in allowlist and not an IP literal — reject. We do not
		// resolve DNS here because (a) DNS rebinding could flip the answer
		// between validation and delivery, and (b) the offline deployment may
		// not have working DNS for arbitrary names.
		return fmt.Errorf("hostname %q is not on the allowlist and is not an IP literal — public destinations are not allowed", host)
	}
	if !isPrivateIP(ip) {
		return fmt.Errorf("IP %s is not in a private/loopback/link-local range — public destinations are not allowed", host)
	}
	return nil
}

// isPrivateIP returns true for loopback, RFC1918 (10/8, 172.16/12, 192.168/16),
// link-local (169.254/16, fe80::/10), unique-local IPv6 (fc00::/7), and the
// IPv4 carrier-grade NAT range (100.64/10, often used for LAN segments).
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 is RFC6598 shared address space.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// Handler exposes webhook management HTTP endpoints.
type Handler struct {
	store        *Store
	httpClient   *http.Client
	gate         *featureflag.Gate
	urlAllowlist []string // optional hostname allowlist for non-IP destinations
}

// NewHandler constructs a Handler.
func NewHandler(store *Store) *Handler {
	return &Handler{
		store:      store,
		httpClient: &http.Client{Timeout: 10 * 1e9}, // 10 second timeout
	}
}

// NewHandlerWithFlags returns a Handler that gates webhook creation and
// delivery behind the exports.webhook_enabled flag. When disabled, the
// creation endpoint returns 409 with code "feature.disabled" and the process
// endpoint becomes a no-op so no outbound traffic is generated.
//
// hostnameAllowlist may be nil. When non-nil, listed hostnames bypass the
// IP-literal RFC1918/loopback check and are accepted as LAN destinations
// (useful for internal hostnames whose DNS resolves only on the operator's
// network).
func NewHandlerWithFlags(store *Store, flags featureflag.Checker, roles featureflag.RoleLookup, hostnameAllowlist ...string) *Handler {
	return &Handler{
		store:        store,
		httpClient:   &http.Client{Timeout: 10 * 1e9},
		gate:         featureflag.New(flags, roles),
		urlAllowlist: hostnameAllowlist,
	}
}

// ListEndpoints returns all webhook endpoints.
// GET /api/v1/admin/webhooks
func (h *Handler) ListEndpoints(c echo.Context) error {
	endpoints, err := h.store.ListEndpoints(c.Request().Context())
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"endpoints": endpoints})
}

// CreateEndpoint creates a new webhook endpoint.
// POST /api/v1/admin/webhooks
func (h *Handler) CreateEndpoint(c echo.Context) error {
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return common.Unauthorized(c, "Not authenticated")
	}

	// Phased-rollout gate: exports.webhook_enabled.
	// When webhooks are globally off, reject endpoint creation before we
	// persist a secret that would never be used.
	if h.gate != nil && !h.gate.EnabledGlobally(c.Request().Context(), "exports.webhook_enabled") {
		return common.ErrorResponse(c, http.StatusConflict, "feature.disabled",
			"LAN webhook exports are currently disabled. Ask an admin to enable exports.webhook_enabled.")
	}

	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
	}
	if err := c.Bind(&req); err != nil {
		return common.BadRequest(c, "validation.invalid_body", "Invalid request body")
	}
	if req.URL == "" {
		return common.BadRequest(c, "validation.required", "url is required")
	}

	// LAN-only contract: reject public-internet destinations before we
	// persist a secret or schedule a delivery to one.
	if err := validateLANURL(req.URL, h.urlAllowlist); err != nil {
		return common.BadRequest(c, "webhooks.url_not_lan", err.Error())
	}

	// Validate or generate the webhook secret.
	secretRevealed := ""
	if req.Secret == "" {
		// No secret provided — generate a secure 32-byte random secret server-side.
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return common.Internal(c)
		}
		req.Secret = hex.EncodeToString(raw)
		secretRevealed = req.Secret
	} else {
		// Secret provided — reject weak values.
		if req.Secret == "default-secret" || len(req.Secret) < 16 {
			return common.BadRequest(c, "webhooks.secret_too_weak",
				"Secret must be at least 16 characters and must not be 'default-secret'")
		}
	}

	endpoint, err := h.store.CreateEndpoint(c.Request().Context(),
		req.URL, req.Events, userID, req.Secret)
	if err != nil {
		return common.Internal(c)
	}

	// Return the generated secret once (only when it was server-generated).
	// Caller must save it — it will not be shown again after creation.
	if secretRevealed != "" {
		type endpointWithSecret struct {
			*WebhookEndpoint
			Secret string `json:"secret"`
		}
		return c.JSON(http.StatusCreated, endpointWithSecret{
			WebhookEndpoint: endpoint,
			Secret:          secretRevealed,
		})
	}
	return c.JSON(http.StatusCreated, endpoint)
}

// ListDeliveries returns recent webhook delivery records.
// GET /api/v1/admin/webhooks/deliveries
func (h *Handler) ListDeliveries(c echo.Context) error {
	deliveries, err := h.store.ListDeliveries(c.Request().Context(), 50)
	if err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]any{"deliveries": deliveries})
}

// ProcessDeliveries manually triggers delivery processing.
// POST /api/v1/admin/webhooks/process
func (h *Handler) ProcessDeliveries(c echo.Context) error {
	// Phased-rollout gate: honor exports.webhook_enabled here too so the
	// manual trigger does not leak outbound traffic when the feature is off.
	if h.gate != nil && !h.gate.EnabledGlobally(c.Request().Context(), "exports.webhook_enabled") {
		return c.JSON(http.StatusOK, map[string]any{"status": "skipped", "reason": "feature.disabled"})
	}
	if err := h.store.ProcessPendingDeliveries(c.Request().Context(), h.httpClient); err != nil {
		return common.Internal(c)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
