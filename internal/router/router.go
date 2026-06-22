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
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// Handlers groups the application's HTTP handlers for registration.
type Handlers struct {
	Health   *handler.HealthHandler
	User     *handler.UserHandler
	Link     *handler.LinkHandler
	Redirect *handler.RedirectHandler
	Auth     *handler.AuthHandler
}

// New builds a configured Echo instance with middleware and all routes.
// apiKeys protect the link-management endpoints under /api; issuer backs the
// JWT middleware on authenticated routes.
func New(h Handlers, apiKeys []string, issuer *token.Issuer) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestID())
	// e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())

	registerRoutes(e, h, apiKeys, issuer)
	return e
}

func registerRoutes(e *echo.Echo, h Handlers, apiKeys []string, issuer *token.Issuer) {
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
	auth.GET("/me", h.Auth.Me, appmw.JWT(issuer))

	users := e.Group("/users")
	users.GET("", h.User.List)
	users.GET("/:id", h.User.Get)

	// Link management — protected by API key.
	api := e.Group("/api")
	api.Use(appmw.APIKey(apiKeys))
	links := api.Group("/links")
	links.POST("", h.Link.Create)
	links.GET("/:code/stats", h.Link.Stats)

	// Public redirect. Registered last; Echo prioritizes the static /healthz,
	// /users and /api routes over this catch-all param route.
	e.GET("/:code", h.Redirect.Redirect)
}
