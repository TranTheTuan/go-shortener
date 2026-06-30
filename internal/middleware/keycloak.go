package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/keycloak"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// ctxUserID is the echo.Context key under which the authenticated local user ID
// is stored. Downstream layers (handlers, quota, dedup) read it via UserIDFrom.
const ctxUserID = "user_id"

// userSyncer maps a verified Keycloak identity to a local user (JIT provisioning).
// Satisfied by service.UserService.
type userSyncer interface {
	SyncFromKeycloak(ctx context.Context, in service.SyncInput) (*repository.User, error)
}

// Keycloak authenticates a request by validating a Keycloak access token, then
// JIT-provisions/looks up the matching local user and stores its int64 ID in the
// context (so ownership/quota keep working on the local id). Fail-closed: any
// missing/invalid token is rejected with 401.
func Keycloak(verifier keycloak.TokenVerifier, users userSyncer) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			raw, ok := strings.CutPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
			if !ok || strings.TrimSpace(raw) == "" {
				return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token"))
			}

			id, err := verifier.Verify(c.Request().Context(), raw)
			if err != nil {
				log.Println("Keycloak token verification failed:", err)
				return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "invalid or expired token"))
			}

			user, err := users.SyncFromKeycloak(c.Request().Context(), service.SyncInput{
				Sub:        id.Sub,
				Email:      id.Email,
				Username:   id.Username,
				GivenName:  id.GivenName,
				FamilyName: id.FamilyName,
			})
			if err != nil {
				return response.Error(c, err)
			}

			c.Set(ctxUserID, user.ID)
			return next(c)
		}
	}
}

// UserIDFrom returns the authenticated local user ID stored by the auth
// middleware. The bool is false when no authenticated user is present.
func UserIDFrom(c echo.Context) (int64, bool) {
	id, ok := c.Get(ctxUserID).(int64)
	return id, ok
}
