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

// Error translates an error into a JSON response. Application errors are
// rendered with their status, code, and message; any other error is logged
// and reported as a generic 500 so internal details never leak.
func Error(c echo.Context, err error) error {
	if appErr, ok := apperror.As(err); ok {
		if appErr.Status >= http.StatusInternalServerError {
			slog.Error("request failed", "code", appErr.Code, "error", appErr)
		}
		return c.JSON(appErr.Status, Envelope{Error: &ErrorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
		}})
	}

	slog.Error("unhandled error", "error", err)
	return c.JSON(http.StatusInternalServerError, Envelope{Error: &ErrorBody{
		Code:    "INTERNAL",
		Message: "internal server error",
	}})
}
