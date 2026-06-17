package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// RedirectHandler serves the public short-link redirect endpoint.
type RedirectHandler struct {
	links     service.LinkService
	analytics service.AnalyticsService
}

// NewRedirectHandler wires a RedirectHandler to its services.
func NewRedirectHandler(links service.LinkService, analytics service.AnalyticsService) *RedirectHandler {
	return &RedirectHandler{links: links, analytics: analytics}
}

// Redirect handles GET /:code. It resolves the code to its original URL and
// issues a 302. The click is recorded asynchronously so analytics never delays
// the redirect; a lost click on crash is acceptable.
func (h *RedirectHandler) Redirect(c echo.Context) error {
	link, err := h.links.Resolve(c.Request().Context(), c.Param("code"))
	if err != nil {
		return response.Error(c, err) // 404 / 410 / 500
	}

	// Capture request data before the goroutine — the echo.Context must not be
	// touched once the handler returns.
	in := service.RecordInput{
		LinkID:    link.ID,
		Referrer:  c.Request().Referer(),
		IPAddress: c.RealIP(),
		UserAgent: c.Request().UserAgent(),
	}
	go func() {
		if err := h.analytics.Record(context.Background(), in); err != nil {
			slog.Error("record click failed", "link_id", in.LinkID, "error", err)
		}
	}()

	return c.Redirect(http.StatusFound, link.OriginalURL)
}
