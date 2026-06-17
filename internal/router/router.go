// Package router wires HTTP routes to handlers and configures global
// middleware. It owns the Echo instance and keeps routing concerns out of the
// handler and main packages.
package router

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/TranTheTuan/YOUR-REPO-NAME/internal/handler"
)

// Handlers groups the application's HTTP handlers for registration.
type Handlers struct {
	Health *handler.HealthHandler
	User   *handler.UserHandler
}

// New builds a configured Echo instance with middleware and all routes.
func New(h Handlers) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestID())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	registerRoutes(e, h)
	return e
}

func registerRoutes(e *echo.Echo, h Handlers) {
	e.GET("/healthz", h.Health.Health)

	users := e.Group("/users")
	users.POST("", h.User.Create)
	users.GET("", h.User.List)
	users.GET("/:id", h.User.Get)
}
