// Package router wires HTTP routes to handlers and configures global
// middleware. It owns the Echo instance and keeps routing concerns out of the
// handler and main packages.
package router

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/TranTheTuan/go-shortener/internal/handler"
	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
)

// Handlers groups the application's HTTP handlers for registration.
type Handlers struct {
	Health   *handler.HealthHandler
	User     *handler.UserHandler
	Link     *handler.LinkHandler
	Redirect *handler.RedirectHandler
}

// New builds a configured Echo instance with middleware and all routes.
// apiKeys protect the link-management endpoints under /api.
func New(h Handlers, apiKeys []string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestID())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	registerRoutes(e, h, apiKeys)
	return e
}

func registerRoutes(e *echo.Echo, h Handlers, apiKeys []string) {
	e.GET("/healthz", h.Health.Health)

	users := e.Group("/users")
	users.POST("", h.User.Create)
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
