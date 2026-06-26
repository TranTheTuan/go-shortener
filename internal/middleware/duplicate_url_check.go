package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// dedupResponse is the body returned when a create request is short-circuited
// because the owner already has a link for this URL.
type dedupResponse struct {
	ShortURL string `json:"short_url"`
	Reused   bool   `json:"reused"`
}

// DuplicateURLCheck short-circuits a create request when the authenticated owner
// already has a link for the same URL (per-owner Redis fast-path), returning it
// without touching the quota or the database. It runs after Authn and before
// QuotaCheck. Unauthenticated (API-key) requests skip it — their dedup is
// handled by the service layer. A Redis miss/outage falls through to next.
func DuplicateURLCheck(dedup *service.DedupCache) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, ok := UserIDFrom(c)
			if !ok {
				return next(c)
			}

			// Read+restore the body so the downstream handler can still bind it.
			body, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return next(c)
			}
			c.Request().Body = io.NopCloser(bytes.NewReader(body))

			var req struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(body, &req) != nil || req.URL == "" {
				return next(c)
			}

			if short, found := dedup.Lookup(c.Request().Context(), userID, req.URL); found {
				return response.Success(c, http.StatusOK, dedupResponse{ShortURL: short, Reused: true})
			}
			return next(c)
		}
	}
}
