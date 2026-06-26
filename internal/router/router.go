// Package router wires HTTP routes to handlers and configures global
// middleware. It owns the Echo instance and keeps routing concerns out of the
// handler and main packages.
package router

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"

	_ "github.com/TranTheTuan/go-shortener/docs/swagger" // generated OpenAPI spec
	"github.com/TranTheTuan/go-shortener/internal/handler"
	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// Deps groups cross-cutting dependencies the router needs to build middleware.
type Deps struct {
	Issuer  *token.Issuer
	APIKeys []string
	Dedup   *service.DedupCache
	Quota   service.QuotaService
}

// Handlers groups the application's HTTP handlers for registration.
type Handlers struct {
	Health   *handler.HealthHandler
	User     *handler.UserHandler
	Link     *handler.LinkHandler
	Redirect *handler.RedirectHandler
	Auth     *handler.AuthHandler
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

	registerRoutes(e, h, deps)
	return e
}

func registerRoutes(e *echo.Echo, h Handlers, deps Deps) {
	e.GET("/healthz", h.Health.Health)

	// Swagger UI (browse at /swagger/index.html).
	e.GET("/swagger/*", echoSwagger.WrapHandler)

	// Authentication. register/login/refresh/logout are public; /me requires a
	// valid access token via the JWT middleware.
	auth := e.Group("/auth")
	auth.POST("/register", h.Auth.Register)
	auth.POST("/login", h.Auth.Login)
	auth.POST("/refresh", h.Auth.Refresh)
	auth.POST("/logout", h.Auth.Logout)
	auth.GET("/me", h.Auth.Me, appmw.JWT(deps.Issuer))

	// User lookups require authentication so the roster (usernames + emails) is
	// not exposed anonymously.
	users := e.Group("/users", appmw.JWT(deps.Issuer))
	users.GET("", h.User.List)
	users.GET("/:id", h.User.Get)

	// Link management — authenticated by JWT or static API key. A JWT identifies
	// the owner (links get a user_id); an API key authenticates without a user.
	api := e.Group("/api")
	api.Use(appmw.Authn(deps.Issuer, deps.APIKeys))
	links := api.Group("/links")
	// Create chain: dedup fast-path → quota check → handler.
	links.POST("", h.Link.Create, appmw.DuplicateURLCheck(deps.Dedup), appmw.QuotaCheck(deps.Quota))
	links.GET("/:code/stats", h.Link.Stats)

	// Public redirect. Registered last; Echo prioritizes the static /healthz,
	// /users and /api routes over this catch-all param route.
	e.GET("/:code", h.Redirect.Redirect)
}
