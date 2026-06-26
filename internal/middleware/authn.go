package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// Authn authenticates a request via EITHER a JWT bearer token OR a static API
// key. A valid JWT sets the user ID in the context (so handlers can stamp link
// ownership); a valid API key authenticates without a user (unowned). It is
// fail-closed: a request with neither valid credential is rejected.
func Authn(issuer *token.Issuer, apiKeys []string) echo.MiddlewareFunc {
	keys := keySet(apiKeys)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Prefer a bearer token when present.
			if raw, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer "); ok && strings.TrimSpace(raw) != "" {
				claims, err := issuer.Parse(raw)
				if err != nil {
					return unauthorized(c, "invalid or expired token")
				}
				c.Set(ctxUserID, claims.UserID)
				return next(c)
			}

			// Fall back to a static API key (unowned access).
			if key := c.Request().Header.Get(apiKeyHeader); key != "" {
				if _, ok := keys[key]; ok {
					return next(c)
				}
			}

			return unauthorized(c, "missing or invalid credentials")
		}
	}
}

// unauthorized writes a uniform 401 envelope.
func unauthorized(c echo.Context, message string) error {
	return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", message))
}
