package middleware

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// ctxUserID is the echo.Context key under which the authenticated user ID is
// stored by the JWT middleware.
const ctxUserID = "user_id"

// JWT returns middleware that requires a valid Bearer access token on the
// Authorization header. On success it stores the user ID in the context; it is
// fail-closed and returns 401 for any missing or invalid token.
func JWT(issuer *token.Issuer) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			raw, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
			if !ok || strings.TrimSpace(raw) == "" {
				return response.Error(c, apperror.New(
					http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token",
				))
			}

			claims, err := issuer.Parse(raw)
			if err != nil {
				return response.Error(c, apperror.New(
					http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired token",
				))
			}

			c.Set(ctxUserID, claims.UserID)
			return next(c)
		}
	}
}

// UserIDFrom returns the authenticated user ID stored by the JWT middleware.
// The bool is false when no authenticated user is present on the context.
func UserIDFrom(c echo.Context) (int64, bool) {
	id, ok := c.Get(ctxUserID).(int64)
	return id, ok
}
