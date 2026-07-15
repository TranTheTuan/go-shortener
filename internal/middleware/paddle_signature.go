package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v5"
)

// PaddleSignature verifies the Paddle-Signature header on incoming webhook
// requests using the SDK's HMAC verifier. Rejects with 401 on invalid or
// missing signatures, matching the Keycloak middleware pattern.
func PaddleSignature(verifier *paddlesdk.WebhookVerifier) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ok, err := verifier.Verify(c.Request())
			if err != nil || !ok {
				return c.NoContent(http.StatusUnauthorized)
			}
			return next(c)
		}
	}
}
