// Package handler contains the HTTP transport layer built on Echo: it binds
// requests, invokes services, and writes responses. Handlers stay thin and
// contain no business logic.
package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/YOUR-REPO-NAME/pkg/response"
)

// HealthHandler serves liveness/readiness checks.
type HealthHandler struct{}

// NewHealthHandler returns a HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Health responds with a simple status payload, suitable for load-balancer
// and orchestrator health checks.
func (h *HealthHandler) Health(c echo.Context) error {
	return response.Success(c, http.StatusOK, map[string]string{"status": "ok"})
}
