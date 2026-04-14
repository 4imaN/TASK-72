// cmd/api/main.go — Portal API server entrypoint.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"portal/internal/app/audit"
	"portal/internal/app/auth"
	"portal/internal/app/catalog"
	appconfig "portal/internal/app/config"
	"portal/internal/app/exports"
	"portal/internal/app/learning"
	"portal/internal/app/mfa"
	"portal/internal/app/permissions"
	"portal/internal/app/procurement"
	"portal/internal/app/reconciliation"
	"portal/internal/app/recommendations"
	"portal/internal/app/reviews"
	"portal/internal/app/search"
	"portal/internal/app/sessions"
	"portal/internal/app/taxonomy"
	"portal/internal/app/users"
	"portal/internal/app/webhooks"
	"portal/internal/platform/crypto"
	"portal/internal/platform/logging"
	"portal/internal/platform/postgres"
	"portal/internal/platform/storage"
)

const (
	defaultPort    = "8080"
	shutdownTimeout = 10 * time.Second
)

func main() {
	log := logging.New(os.Stdout, logging.INFO, "api")

	// ── Database connection ───────────────────────────────────────────────────
	dbCfg, err := postgres.ConfigFromEnv()
	if err != nil {
		log.Error("database config error", map[string]any{"err": err.Error()})
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := postgres.Open(ctx, dbCfg)
	if err != nil {
		log.Error("database connect error", map[string]any{"err": err.Error()})
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("database connected", map[string]any{"host": dbCfg.Host, "db": dbCfg.Database})

	// ── Crypto encryptor ──────────────────────────────────────────────────────
	encryptor, err := crypto.NewEncryptorFromEnv()
	if err != nil {
		log.Error("encryption key required but not found", map[string]any{"err": err.Error()})
		os.Exit(1)
	}

	// ── Echo setup ────────────────────────────────────────────────────────────
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Middleware stack
	e.Use(traceIDMiddleware())
	e.Use(structuredLogMiddleware(log))
	e.Use(middleware.Recover())
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		HSTSMaxAge:            0, // offline — no HTTPS HSTS
		ContentSecurityPolicy: "default-src 'self'",
	}))
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"http://localhost:3000", "http://web"},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderContentType, echo.HeaderAuthorization, "X-Request-ID", "X-Client-Version"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	// ── File storage ──────────────────────────────────────────────────────────
	storageDir := envOrDefault("STORAGE_DIR", "/app/storage")
	fileStore, err := storage.NewStore(storageDir)
	if err != nil {
		log.Warn("storage init failed, using /tmp fallback", map[string]any{"err": err.Error()})
		fileStore, _ = storage.NewStore(os.TempDir())
	}

	// ── Route registration ────────────────────────────────────────────────────
	registerRoutes(e, pool, encryptor, log, fileStore)

	// ── Start server ─────────────────────────────────────────────────────────
	port := envOrDefault("PORT", defaultPort)
	addr := fmt.Sprintf(":%s", port)

	// Graceful shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Info("API server starting", map[string]any{"addr": addr})
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Error("server error", map[string]any{"err": err.Error()})
			os.Exit(1)
		}
	}()

	<-shutdownCh
	log.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", map[string]any{"err": err.Error()})
	}
	log.Info("server stopped")
}

// configAuditAdapter bridges the audit.Store to the config.AuditRecorder
// interface so config handlers can record mutations without taking a direct
// dependency on the audit package (and the import cycle that would create).
type configAuditAdapter struct{ s *audit.Store }

func (a configAuditAdapter) Record(ctx context.Context, evt appconfig.AuditEvent) {
	a.s.RecordConfigChange(ctx,
		evt.ActorID, evt.Action, evt.TargetType, evt.TargetID,
		evt.OldValue, evt.NewValue, evt.IPAddress)
}

func registerRoutes(e *echo.Echo, pool *pgxpool.Pool, encryptor *crypto.Encryptor, log *logging.Logger, fileStore *storage.Store) {
	// ── Health check ─────────────────────────────────────────────────────────
	e.GET("/api/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "portal-api",
		})
	})

	// ── Version ───────────────────────────────────────────────────────────────
	e.GET("/api/version", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"version": appVersion(),
			"service": "portal-api",
		})
	})

	// ── Stores and handlers ───────────────────────────────────────────────────
	userStore   := users.NewStore(pool)
	configStore := appconfig.NewStore(pool)

	// auditStore is constructed up front so every privileged-mutation handler
	// below — taxonomy, reviews, reconciliation, procurement, config, users —
	// shares one writer into audit_logs. Keeping it global means we can audit
	// any new admin/approval/state-transition route by passing this same handle.
	auditStore := audit.NewStore(pool)

	// Load session timeouts from config_parameters (seeded keys: session.idle_timeout_seconds,
	// session.max_timeout_seconds). Falls back to 15min idle / 8hr absolute if not set.
	sessionIdle, sessionAbs, _ := sessions.LoadTimeouts(context.Background(), configStore)
	sessionStore := sessions.NewStoreWithTimeouts(pool, sessionIdle, sessionAbs)
	mfaStore     := mfa.NewStore(pool, encryptor)
	authHandler  := auth.NewHandler(userStore, sessionStore, mfaStore, configStore, log)
	permMW       := permissions.NewMiddleware(sessionStore, userStore, mfaStore, configStore)

	// ── API v1 group ──────────────────────────────────────────────────────────
	v1 := e.Group("/api/v1")

	// Public auth routes — no session required.
	v1.POST("/auth/login",  authHandler.Login)
	v1.POST("/auth/logout", authHandler.Logout)

	// Protected routes — session required.
	protected := v1.Group("", permMW.RequireAuth)
	protected.GET("/session",              authHandler.GetSession)
	protected.POST("/auth/password/change", authHandler.ChangePassword)
	protected.GET("/ping", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"message": "pong"})
	})

	// ── MFA routes ────────────────────────────────────────────────────────────
	mfaHandler := mfa.NewHandler(mfaStore, sessionStore, userStore, log)

	protected.POST("/mfa/enroll/start",   mfaHandler.StartEnrollment)
	protected.POST("/mfa/enroll/confirm", mfaHandler.ConfirmEnrollment)
	protected.POST("/mfa/verify",         mfaHandler.Verify)
	protected.POST("/mfa/recovery",       mfaHandler.VerifyRecovery)
	// Frontend alias routes (Issue 5)
	protected.POST("/auth/mfa/verify",    mfaHandler.Verify)
	protected.POST("/auth/mfa/recovery",  mfaHandler.VerifyRecovery)

	// ── Catalog routes ────────────────────────────────────────────────────────
	catalogStore   := catalog.NewStore(pool)
	catalogHandler := catalog.NewHandler(catalogStore)

	protected.GET("/catalog/resources",     catalogHandler.ListResources)
	protected.GET("/catalog/resources/:id", catalogHandler.GetResource)

	// Catalog mutations — content authoring (catalog:write) and lifecycle
	// (catalog:publish, held by content moderators per the seed role matrix).
	protected.POST("/catalog/resources",                catalogHandler.CreateResource,  permMW.RequirePermission("catalog:write"))
	protected.PUT("/catalog/resources/:id",             catalogHandler.UpdateResource,  permMW.RequirePermission("catalog:write"))
	protected.POST("/catalog/resources/:id/archive",    catalogHandler.ArchiveResource, permMW.RequirePermission("catalog:publish"))
	protected.POST("/catalog/resources/:id/restore",    catalogHandler.RestoreResource, permMW.RequirePermission("catalog:publish"))

	// ── Search routes ─────────────────────────────────────────────────────────
	searchStore   := search.NewStore(pool)
	// Pass the configStore as the FlagChecker so search.pinyin_expansion rollout
	// is enforced per-role at request time; userStore supplies role lookups.
	searchHandler := search.NewHandlerWithFlags(searchStore, configStore, userStore)

	protected.GET("/search",                                  searchHandler.Search)
	protected.GET("/archive/buckets",                         searchHandler.GetArchiveBuckets)
	protected.GET("/archive/buckets/:type/:key/resources",    searchHandler.GetBucketResources)
	protected.POST("/search/rebuild",                         searchHandler.RebuildIndex, permMW.RequireRole("admin"))

	// ── Taxonomy routes ───────────────────────────────────────────────────────
	taxonomyStore   := taxonomy.NewStore(pool)
	taxonomyHandler := taxonomy.NewHandlerWithAudit(taxonomyStore, auditStore)

	protected.GET("/taxonomy/tags",                   taxonomyHandler.ListTags)
	protected.GET("/taxonomy/tags/:id",               taxonomyHandler.GetTag)
	protected.POST("/taxonomy/tags/:id/synonyms",     taxonomyHandler.AddSynonym, permMW.RequirePermission("taxonomy:write"))
	protected.GET("/taxonomy/conflicts",              taxonomyHandler.ListConflicts,    permMW.RequirePermission("taxonomy:write"))
	protected.POST("/taxonomy/conflicts/:id/resolve", taxonomyHandler.ResolveConflict,  permMW.RequirePermission("taxonomy:write"))

	// ── Learning routes ───────────────────────────────────────────────────────
	learningStore   := learning.NewStore(pool)
	learningHandler := learning.NewHandler(learningStore, log)

	// Learning paths (all authenticated users)
	protected.GET("/paths",                     learningHandler.ListPaths)
	protected.GET("/paths/:id",                 learningHandler.GetPath)
	protected.POST("/paths/:id/enroll",         learningHandler.Enroll)
	protected.GET("/paths/:id/progress",        learningHandler.GetPathProgress)

	// Learner personal progress (owns only their own data)
	protected.GET("/me/enrollments",             learningHandler.ListEnrollments)
	protected.GET("/me/progress",               learningHandler.GetResumeState)
	protected.POST("/me/progress/:resource_id", learningHandler.RecordProgress)
	protected.GET("/me/exports/csv",            learningHandler.ExportCSV)

	// ── Recommendations routes ────────────────────────────────────────────────
	recStore   := recommendations.NewStore(pool)
	// Gate delivery behind recommendations.enabled so the flag is authoritative.
	recHandler := recommendations.NewHandlerWithFlags(recStore, log, configStore, userStore)

	protected.GET("/recommendations",        recHandler.GetRecommendations)
	protected.POST("/recommendations/events", recHandler.RecordEvent)

	// ── Reviews, appeals, and moderation routes ───────────────────────────────
	reviewStore   := reviews.NewStoreWithEncryptor(pool, fileStore, encryptor)
	reviewHandler := reviews.NewHandlerWithAudit(reviewStore, userStore, fileStore, auditStore)

	// Reviews
	protected.POST("/reviews",                     reviewHandler.CreateReview,          permMW.RequirePermission("reviews:write"))
	protected.GET("/reviews/:id",                  reviewHandler.GetReview)
	protected.GET("/orders/:order_id/reviews",     reviewHandler.ListOrderReviews)
	protected.POST("/reviews/:id/reply",           reviewHandler.AddMerchantReply,      permMW.RequirePermission("merchant_replies:write"))
	protected.POST("/reviews/:id/flag",            reviewHandler.FlagReview,            permMW.RequirePermission("reviews:write"))
	protected.GET("/reviews/attachments/:id",      reviewHandler.DownloadAttachment)

	// Appeals
	protected.POST("/appeals",                     reviewHandler.CreateAppeal,          permMW.RequirePermission("appeals:write"))
	protected.GET("/appeals/:id",                  reviewHandler.GetAppeal)
	protected.GET("/appeals",                      reviewHandler.ListAppeals)
	protected.POST("/appeals/:id/arbitrate",       reviewHandler.Arbitrate,             permMW.RequirePermission("appeals:decide"))
	protected.GET("/appeals/evidence/:id",         reviewHandler.DownloadEvidence)

	// Moderation queue
	protected.GET("/moderation/queue",             reviewHandler.ListModerationQueue,   permMW.RequirePermission("moderation:write"))
	protected.POST("/moderation/queue/:id/decide", reviewHandler.DecideModerationItem,  permMW.RequirePermission("moderation:write"))

	// ── Webhook store (built early so reconciliation + exports can fan out events) ──
	webhookStore := webhooks.NewStore(pool, encryptor)

	// ── Reconciliation & Settlement routes ────────────────────────────────────
	// Wire the webhook emitter so settlement transitions (approved/exported/
	// settled) enqueue deliveries to subscribers of those event types.
	reconStore   := reconciliation.NewStore(pool).WithWebhooks(webhookStore)
	reconHandler := reconciliation.NewHandlerWithAudit(reconStore, auditStore)

	// Statement imports — required BEFORE running reconciliation
	protected.POST("/reconciliation/statements",  reconHandler.ImportStatements,  permMW.RequirePermission("reconciliation:write"))
	protected.GET("/reconciliation/statements",   reconHandler.ListImportBatches, permMW.RequirePermission("reconciliation:read"))

	// Billing rules
	protected.GET("/reconciliation/rules", reconHandler.ListRules, permMW.RequirePermission("reconciliation:read"))

	// Reconciliation runs
	protected.GET("/reconciliation/runs",              reconHandler.ListRuns,    permMW.RequirePermission("reconciliation:read"))
	protected.POST("/reconciliation/runs",             reconHandler.CreateRun,   permMW.RequirePermission("reconciliation:write"))
	protected.GET("/reconciliation/runs/:id",          reconHandler.GetRun,      permMW.RequirePermission("reconciliation:read"))
	protected.POST("/reconciliation/runs/:id/process", reconHandler.ProcessRun,  permMW.RequirePermission("reconciliation:write"))

	// Variances
	protected.GET("/reconciliation/runs/:id/variances",   reconHandler.ListVariances,    permMW.RequirePermission("reconciliation:read"))
	protected.POST("/reconciliation/variances/:id/submit-approval", reconHandler.SubmitVarianceForApproval, permMW.RequirePermission("reconciliation:write"))
	protected.POST("/reconciliation/variances/:id/approve",         reconHandler.ApproveVariance,           permMW.RequirePermission("writeoffs:approve"))
	protected.POST("/reconciliation/variances/:id/apply",           reconHandler.ApplySuggestion,           permMW.RequirePermission("reconciliation:write"))

	// Settlement batches
	protected.GET("/reconciliation/batches",              reconHandler.ListBatches,  permMW.RequirePermission("reconciliation:read"))
	protected.POST("/reconciliation/batches",             reconHandler.CreateBatch,  permMW.RequirePermission("settlements:write"))
	protected.GET("/reconciliation/batches/:id",          reconHandler.GetBatch,     permMW.RequirePermission("reconciliation:read"))
	protected.POST("/reconciliation/batches/:id/submit",  reconHandler.SubmitBatch,  permMW.RequirePermission("settlements:write"))
	protected.POST("/reconciliation/batches/:id/approve", reconHandler.ApproveBatch, permMW.RequirePermission("settlements:write"))
	protected.POST("/reconciliation/batches/:id/export",  reconHandler.ExportBatch,  permMW.RequirePermission("settlements:write"))
	protected.POST("/reconciliation/batches/:id/settle",  reconHandler.SettleBatch,  permMW.RequirePermission("settlements:write"))
	protected.POST("/reconciliation/batches/:id/void",    reconHandler.VoidBatch,    permMW.RequirePermission("settlements:write"))

	// ── Export Jobs routes ────────────────────────────────────────────────────
	// exportStore.UpdateJobStatus emits export.completed / export.failed events
	// to webhook subscribers when wired with the webhook emitter.
	exportStore   := exports.NewStore(pool).WithWebhooks(webhookStore)
	exportHandler := exports.NewHandler(exportStore, pool, userStore)

	// CreateJob performs per-type permission checks in the handler (learner
	// export uses learning:export_own; reconciliation_export requires exports:write).
	protected.POST("/exports/jobs",              exportHandler.CreateJob)
	protected.GET("/exports/jobs",               exportHandler.ListJobs)
	protected.GET("/exports/jobs/:id",           exportHandler.GetJob)
	protected.GET("/exports/jobs/:id/download",  exportHandler.DownloadJob)

	// ── Config Center routes (admin-only) ──────────────────────────────────────
	// auditStore is the same instance constructed near the reconciliation block.
	configHandler := appconfig.NewHandlerWithAudit(configStore, configAuditAdapter{auditStore})

	protected.GET("/admin/config/flags",           configHandler.ListFlags,        permMW.RequireRole("admin"))
	protected.PUT("/admin/config/flags/:key",      configHandler.SetFlag,          permMW.RequireRole("admin"))
	protected.GET("/admin/config/params",          configHandler.ListParams,       permMW.RequirePermission("config:read"))
	protected.PUT("/admin/config/params/:key",     configHandler.SetParam,         permMW.RequireRole("admin"))
	protected.GET("/admin/config/version-rules",   configHandler.ListVersionRules, permMW.RequirePermission("config:read"))
	protected.PUT("/admin/config/version-rules",   configHandler.SetVersionRule,   permMW.RequireRole("admin"))

	// ── Webhook routes (admin-only) ────────────────────────────────────────────
	// webhookStore was constructed above so reconciliation + exports could
	// share the emitter; only the handler is created here.
	// Gate endpoint creation + processing behind exports.webhook_enabled so
	// the flag acts as a kill-switch for outbound LAN webhook traffic.
	webhookHandler := webhooks.NewHandlerWithFlags(webhookStore, configStore, userStore)

	protected.GET("/admin/webhooks",            webhookHandler.ListEndpoints,    permMW.RequireRole("admin"))
	protected.POST("/admin/webhooks",           webhookHandler.CreateEndpoint,   permMW.RequireRole("admin"))
	protected.GET("/admin/webhooks/deliveries", webhookHandler.ListDeliveries,   permMW.RequireRole("admin"))
	protected.POST("/admin/webhooks/process",   webhookHandler.ProcessDeliveries, permMW.RequireRole("admin"))

	// ── Admin user management routes ──────────────────────────────────────────
	// auditStore was constructed above for the config handler; reuse it here.
	userAdminHandler := users.NewAdminHandlerWithAudit(userStore, auditStore)

	protected.GET("/admin/users",                       userAdminHandler.ListUsers,       permMW.RequireRole("admin"))
	protected.GET("/admin/users/:id",                   userAdminHandler.GetUser,         permMW.RequireRole("admin"))
	protected.PUT("/admin/users/:id/roles",             userAdminHandler.UpdateUserRoles, permMW.RequireRole("admin"))
	protected.GET("/admin/users/:id/reveal-email",      userAdminHandler.RevealEmail,     permMW.RequirePermission("sensitive_data:reveal"))

	// ── Admin audit log routes ────────────────────────────────────────────────
	auditHandler := audit.NewHandler(auditStore)

	protected.GET("/admin/audit", auditHandler.ListEvents, permMW.RequireRole("admin"))

	// ── Procurement routes ────────────────────────────────────────────────────
	procurementStore   := procurement.NewStore(pool)
	procurementHandler := procurement.NewHandlerWithAudit(procurementStore, auditStore)

	protected.GET("/procurement/orders",              procurementHandler.ListOrders,   permMW.RequirePermission("orders:read"))
	protected.POST("/procurement/orders",             procurementHandler.CreateOrder,  permMW.RequirePermission("orders:write"))
	protected.GET("/procurement/orders/:id",          procurementHandler.GetOrder,     permMW.RequirePermission("orders:read"))
	protected.POST("/procurement/orders/:id/approve", procurementHandler.ApproveOrder, permMW.RequirePermission("orders:approve"))
	protected.POST("/procurement/orders/:id/reject",  procurementHandler.RejectOrder,  permMW.RequirePermission("orders:approve"))
}

// traceIDMiddleware injects a unique trace ID into every request context.
func traceIDMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			traceID := c.Request().Header.Get("X-Request-ID")
			if traceID == "" {
				traceID = uuid.New().String()
			}
			c.Request().Header.Set("X-Request-ID", traceID)
			c.Response().Header().Set("X-Request-ID", traceID)
			c.Set("trace_id", traceID)
			return next(c)
		}
	}
}

// structuredLogMiddleware logs every request as a structured JSON line.
func structuredLogMiddleware(log *logging.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			duration := time.Since(start)

			status := c.Response().Status
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				} else {
					status = http.StatusInternalServerError
				}
			}

			fields := map[string]any{
				"method":     c.Request().Method,
				"path":       c.Request().URL.Path,
				"status":     status,
				"duration_ms": duration.Milliseconds(),
				"trace_id":   c.Get("trace_id"),
				"remote_ip":  c.RealIP(),
			}

			if status >= 500 {
				log.Error("request", fields)
			} else {
				log.Info("request", fields)
			}

			return err
		}
	}
}

func appVersion() string {
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return "0.1.0"
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
