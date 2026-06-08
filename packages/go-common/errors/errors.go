// Package errors defines the canonical error envelope shared across all Go services.
// Aligns with gRPC status codes so transport translation is trivial.
package errors

import "fmt"

// Code is a stable string identifier; do not reuse values once published.
type Code string

const (
	CodeUnknown        Code = "UNKNOWN"
	CodeInvalidArg     Code = "INVALID_ARGUMENT"
	CodeNotFound       Code = "NOT_FOUND"
	CodePermissionDenied Code = "PERMISSION_DENIED"
	CodeUnavailable    Code = "UNAVAILABLE"
	CodeInternal       Code = "INTERNAL"
)

// Error is the standard application error.
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// New constructs an Error.
func New(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }

// Wrap attaches a cause.
func Wrap(code Code, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}
