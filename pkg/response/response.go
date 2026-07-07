// Package response provides helpers for writing consistent JSON responses
// from Echo handlers. Every response is wrapped in an envelope with either a
// "data" or an "error" field.
package response

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// Envelope is the uniform response body.
type Envelope struct {
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is the client-facing error payload.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Success writes a successful JSON response with the given status and data.
func Success(c echo.Context, status int, data any) error {
	return c.JSON(status, Envelope{Data: data})
}

// Error translates an error into a JSON response and logs it ONCE here — the
// single choke point every handler funnels errors through. Services attach
// operation context by wrapping (fmt.Errorf("svc.Method: %w", err)); the wrapped
// cause travels inside the apperror and surfaces in this one line, so there is
// no need to log inside each method. 5xx log at ERROR (with the cause), 4xx at
// DEBUG (client errors — opt-in visibility). Each line carries request_id +
// route so it correlates with the request's other logs in Loki.
func Error(c echo.Context, err error) error {
	appErr, ok := apperror.As(err)
	if !ok {
		appErr = apperror.Internal(err) // unknown error → generic 500, cause kept for the log
	}

	attrs := []any{
		"code", appErr.Code,
		"status", appErr.Status,
		"method", c.Request().Method,
		"route", c.Path(), // route template (e.g. /api/links/:code) — low cardinality
		"request_id", c.Response().Header().Get(echo.HeaderXRequestID),
		"error", appErr, // renders the full wrapped chain: code: message: cause
	}
	ctx := c.Request().Context()
	if appErr.Status >= http.StatusInternalServerError {
		slog.ErrorContext(ctx, "request failed", attrs...)
	} else {
		slog.DebugContext(ctx, "request rejected", attrs...)
	}

	return c.JSON(appErr.Status, Envelope{Error: &ErrorBody{
		Code:    appErr.Code,
		Message: appErr.Message,
	}})
}
