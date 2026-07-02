package handler

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// RedirectHandler serves the public short-link redirect endpoint.
type RedirectHandler struct {
	links  service.LinkService
	clicks events.ClickProducer
}

// NewRedirectHandler wires a RedirectHandler to its services.
func NewRedirectHandler(links service.LinkService, clicks events.ClickProducer) *RedirectHandler {
	return &RedirectHandler{links: links, clicks: clicks}
}

// Redirect handles GET /:code. It resolves the code to its original URL and
// issues a 302. The click is recorded asynchronously so analytics never delays
// the redirect; a lost click on crash is acceptable.
//
// @Summary      Redirect to the original URL
// @Description  Resolves a short code and issues a 302 redirect. Records the click asynchronously.
// @Tags         redirect
// @Produce      json
// @Param        code  path  string  true  "Short code"
// @Success      302   "redirect to the original URL"
// @Failure      404   {object}  response.Envelope  "short link not found"
// @Failure      410   {object}  response.Envelope  "short link has expired"
// @Router       /{code} [get]
func (h *RedirectHandler) Redirect(c echo.Context) error {
	link, err := h.links.Resolve(c.Request().Context(), c.Param("code"))
	if err != nil {
		return response.Error(c, err) // 404 / 410 / 500
	}

	h.clicks.Publish(events.ClickEvent{
		LinkID:    link.ID,
		ClickedAt: time.Now().UTC(),
		Referrer:  c.Request().Referer(),
		IPAddress: c.RealIP(),
		UserAgent: c.Request().UserAgent(),
	})

	return c.Redirect(http.StatusFound, link.OriginalURL)
}
