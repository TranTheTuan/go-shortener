// Package router wires HTTP routes to handlers and configures global
// middleware. It owns the Echo instance and keeps routing concerns out of the
// handler and main packages.
package router

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

	_ "github.com/TranTheTuan/go-shortener/docs/swagger" // generated OpenAPI spec
	"github.com/TranTheTuan/go-shortener/internal/handler"
	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/keycloak"
	"github.com/TranTheTuan/go-shortener/web"
)

// Deps groups cross-cutting dependencies the router needs to build middleware.
type Deps struct {
	Verifier keycloak.TokenVerifier
	Users    service.UserService
	Dedup    *service.DedupCache
	Quota    service.QuotaService
}

// Handlers groups the application's HTTP handlers for registration.
type Handlers struct {
	Health   *handler.HealthHandler
	User     *handler.UserHandler
	Link     *handler.LinkHandler
	Redirect *handler.RedirectHandler
	Auth     *handler.AuthHandler
	Frontend *handler.FrontendHandler
	BulkJob  *handler.BulkJobHandler // nil when R2 is not configured
}

// New builds a configured Echo instance with middleware and all routes. Deps
// carries the JWT issuer, API keys, and the dedup/quota collaborators used by
// the link-creation middleware chain.
func New(h Handlers, deps Deps) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestID())
	// e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	// Trace every request as a span keyed by route template. Skip the public
	// redirect hot path (GET /:code) — cache-hit 302s are low trace value and a
	// per-request span would erode the L1-cache throughput win — plus infra
	// endpoints. No-op when tracing is disabled (global tracer is a no-op).
	e.Use(otelecho.Middleware("go-shortener-server", otelecho.WithSkipper(func(c echo.Context) bool {
		return skipTrace(c.Path())
	})))
	// RED metrics (duration + in-flight) for every request, keyed by route template.
	e.Use(appmw.Metrics())
	// Adds ETag + Cache-Control to the embedded SPA so unchanged assets 304 and
	// new deploys are picked up immediately (embed.FS has no modtime of its own).
	e.Use(appmw.FrontendCache(web.Files))

	registerRoutes(e, h, deps)
	return e
}

// skipTrace reports whether a route template should NOT get a tracing span.
// The redirect catch-all (/:code) is excluded to protect the L1-cache hot path;
// health/metrics are infra noise.
func skipTrace(routePath string) bool {
	switch routePath {
	case "/:code", "/healthz", "/metrics":
		return true
	default:
		return false
	}
}

func registerRoutes(e *echo.Echo, h Handlers, deps Deps) {
	e.GET("/healthz", h.Health.Health)

	// Swagger UI (browse at /swagger/index.html).
	e.GET("/swagger/*", echoSwagger.WrapHandler)

	// Frontend SPA (embedded). Specific routes registered before the /:code
	// catch-all so they take precedence; index.html also receives the OIDC
	// callback query, which keycloak-js parses client-side.
	e.FileFS("/", "index.html", web.Files)
	e.StaticFS("/static", echo.MustSubFS(web.Files, "static"))
	e.GET("/app-config.json", h.Frontend.Config)

	// Authentication is owned by Keycloak; this service only validates tokens.
	keycloakMW := appmw.Keycloak(deps.Verifier, deps.Users)

	// Current authenticated user (Keycloak token required).
	auth := e.Group("/auth")
	auth.GET("/me", h.Auth.Me, keycloakMW)

	// User lookups require authentication so the roster (usernames + emails) is
	// not exposed anonymously.
	users := e.Group("/users", keycloakMW)
	users.GET("", h.User.List)
	users.GET("/:id", h.User.Get)

	// Link management — authenticated by a Keycloak access token; the token's
	// user owns the links and is subject to the daily quota.
	api := e.Group("/api", keycloakMW)
	links := api.Group("/links")
	// Create chain: dedup fast-path → quota check → handler.
	links.POST("", h.Link.Create, appmw.DuplicateURLCheck(deps.Dedup), appmw.QuotaCheck(deps.Quota))
	links.GET("", h.Link.List)
	links.GET("/:code/stats", h.Link.Stats)
	links.PUT("/:code", h.Link.Update)
	links.DELETE("/:code", h.Link.Delete)

	// Bulk URL upload — only registered when R2 is configured.
	if h.BulkJob != nil {
		bulk := api.Group("/bulk-jobs")
		bulk.GET("/template", h.BulkJob.DownloadTemplate)
		bulk.POST("/upload-url", h.BulkJob.GetUploadURL)
		bulk.POST("", h.BulkJob.ConfirmUpload)
		bulk.GET("", h.BulkJob.ListJobs)
		bulk.GET("/:id", h.BulkJob.GetJob)
	}

	// Public redirect. Registered last; Echo prioritizes the static /healthz,
	// /users and /api routes over this catch-all param route.
	e.GET("/:code", h.Redirect.Redirect)
}
