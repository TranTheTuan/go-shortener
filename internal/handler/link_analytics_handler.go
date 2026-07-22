package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// Analytics handles GET /api/links/:code/analytics.
//
// @Summary      Advanced link analytics
// @Description  Returns time-series, referrer, and device breakdown for a short link. Requires Pro or Business plan.
// @Tags         links
// @Produce      json
// @Security     BearerAuth
// @Param        code   path      string  true   "Short link code"
// @Param        range  query     string  false  "Date range: 7d, 30d, 90d (default 30d)"
// @Success      200    {object}  service.AdvancedStats
// @Failure      401    {object}  response.Envelope  "not authenticated"
// @Failure      403    {object}  response.Envelope  "feature locked (basic plan)"
// @Failure      404    {object}  response.Envelope  "link not found or not owned by caller"
// @Router       /api/links/{code}/analytics [get]
func (h *LinkHandler) Analytics(c echo.Context) error {
	owner, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}

	code := c.Param("code")
	rng := c.QueryParam("range")

	stats, err := h.analytics.Advanced(c.Request().Context(), code, owner, rng)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, stats)
}
