// Package middleware holds custom Echo middleware for the application.
package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// apiKeyHeader is the request header carrying the caller's API key.
const apiKeyHeader = "X-API-Key"

// APIKey returns middleware that requires a matching key on the X-API-Key
// header. It is fail-closed: an empty key set rejects every request.
func APIKey(keys []string) echo.MiddlewareFunc {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" {
			set[k] = struct{}{}
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := c.Request().Header.Get(apiKeyHeader)
			if _, ok := set[key]; key == "" || !ok {
				return response.Error(c, apperror.New(
					http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid API key",
				))
			}
			return next(c)
		}
	}
}
