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
//
// @Summary      Create a short link
// @Description  Generates a random short code for the given URL. Optionally expires at a future time.
// @Tags         links
// @Accept       json
// @Produce      json
// @Security     ApiKeyAuth
// @Param        X-API-Key  header  string              true  "API key"
// @Param        request  body      createLinkRequest   true  "URL to shorten (expires_at optional, RFC 3339)"
// @Success      201      {object}  createLinkResponse
// @Failure      400      {object}  response.Envelope   "invalid url or expiry"
// @Failure      401      {object}  response.Envelope   "missing or invalid API key"
// @Router       /api/links [post]
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
//
// @Summary      Get click statistics
// @Description  Returns the total click count and the most recent clicks for a short link.
// @Tags         links
// @Produce      json
// @Security     ApiKeyAuth
// @Param        X-API-Key  header  string          true  "API key"
// @Param        code  path      string             true  "Short code"
// @Success      200   {object}  service.LinkStats
// @Failure      401   {object}  response.Envelope  "missing or invalid API key"
// @Failure      404   {object}  response.Envelope  "short link not found"
// @Router       /api/links/{code}/stats [get]
func (h *LinkHandler) Stats(c echo.Context) error {
	stats, err := h.analytics.Stats(c.Request().Context(), c.Param("code"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, stats)
}
