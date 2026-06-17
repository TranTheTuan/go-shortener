package handler

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// LinkHandler exposes the management endpoints for short links (create + stats).
type LinkHandler struct {
	links     service.LinkService
	analytics service.AnalyticsService
	baseURL   string
}

// NewLinkHandler wires a LinkHandler to its services and the public base URL
// used to build short URLs.
func NewLinkHandler(links service.LinkService, analytics service.AnalyticsService, baseURL string) *LinkHandler {
	return &LinkHandler{links: links, analytics: analytics, baseURL: baseURL}
}

// createLinkRequest is the expected body for POST /api/links.
type createLinkRequest struct {
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// createLinkResponse is the payload returned after creating a short link.
type createLinkResponse struct {
	ShortCode   string     `json:"short_code"`
	ShortURL    string     `json:"short_url"`
	OriginalURL string     `json:"original_url"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// Create handles POST /api/links.
func (h *LinkHandler) Create(c echo.Context) error {
	var req createLinkRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, apperror.BadRequest("invalid request body"))
	}

	link, err := h.links.Create(c.Request().Context(), service.CreateLinkInput{
		URL:       req.URL,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, http.StatusCreated, createLinkResponse{
		ShortCode:   link.ShortCode,
		ShortURL:    h.baseURL + "/" + link.ShortCode,
		OriginalURL: link.OriginalURL,
		ExpiresAt:   link.ExpiresAt,
	})
}

// Stats handles GET /api/links/:code/stats.
func (h *LinkHandler) Stats(c echo.Context) error {
	stats, err := h.analytics.Stats(c.Request().Context(), c.Param("code"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, stats)
}
