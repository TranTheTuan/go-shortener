// Package apperror defines a structured application error type that carries an
// HTTP status, a machine-readable code, and a human-readable message. It lets
// the service layer report failures without importing the HTTP framework, and
// lets the transport layer translate them into responses uniformly.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is a structured application error.
type Error struct {
	Status  int    // HTTP status code to return.
	Code    string // Stable, machine-readable error code.
	Message string // Human-readable, client-safe message.
	Err     error  // Optional wrapped cause (not exposed to clients).
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause for errors.Is/As.
func (e *Error) Unwrap() error { return e.Err }

// Wrap attaches an underlying cause to the error, returning a copy.
func (e *Error) Wrap(err error) *Error {
	clone := *e
	clone.Err = err
	return &clone
}

// New constructs an Error.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// As extracts an *Error from err if present in its chain.
func As(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// Constructors for the common cases.

// BadRequest reports invalid client input (HTTP 400).
func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, "BAD_REQUEST", message)
}

// NotFound reports a missing resource (HTTP 404).
func NotFound(message string) *Error {
	return New(http.StatusNotFound, "NOT_FOUND", message)
}

// Conflict reports a state conflict, e.g. a duplicate (HTTP 409).
func Conflict(message string) *Error {
	return New(http.StatusConflict, "CONFLICT", message)
}

// Gone reports a resource that existed but is no longer available (HTTP 410),
// e.g. an expired short link.
func Gone(message string) *Error {
	return New(http.StatusGone, "GONE", message)
}

// Internal reports an unexpected server-side failure (HTTP 500). The cause is
// wrapped for logging but never shown to clients.
func Internal(err error) *Error {
	return (&Error{
		Status:  http.StatusInternalServerError,
		Code:    "INTERNAL",
		Message: "internal server error",
	}).Wrap(err)
}
