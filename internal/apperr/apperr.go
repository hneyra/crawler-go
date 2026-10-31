// Package apperr = typed app errors. domain returns these. http maps kind -> status.
package apperr

import (
	"errors"
	"fmt"
)

// Kind = error class. stable. maps to http status in httpapi.
type Kind int

const (
	KindInternal Kind = iota
	KindInvalid
	KindNotFound
	KindConflict
	KindUnauthorized
	KindForbidden
	KindRateLimited
	KindTooLarge
	KindUnavailable
	KindNotImplemented
)

func (k Kind) String() string {
	switch k {
	case KindInvalid:
		return "invalid"
	case KindNotFound:
		return "not_found"
	case KindConflict:
		return "conflict"
	case KindUnauthorized:
		return "unauthorized"
	case KindForbidden:
		return "forbidden"
	case KindRateLimited:
		return "rate_limited"
	case KindTooLarge:
		return "too_large"
	case KindUnavailable:
		return "unavailable"
	case KindNotImplemented:
		return "not_implemented"
	default:
		return "internal"
	}
}

// FieldError = one invalid input field. safe to show user.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error = app error. Message safe for client. cause kept for logs only.
type Error struct {
	Kind    Kind
	Code    string // stable machine code, e.g. "store_not_found"
	Message string
	Fields  []FieldError
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// Wrap attaches cause. cause never shown to client.
func (e *Error) Wrap(cause error) *Error {
	c := *e
	c.cause = cause
	return &c
}

// New makes error of kind.
func New(kind Kind, code, msg string) *Error { return &Error{Kind: kind, Code: code, Message: msg} }

func NotFound(code, msg string) *Error     { return New(KindNotFound, code, msg) }
func Conflict(code, msg string) *Error     { return New(KindConflict, code, msg) }
func Forbidden(code, msg string) *Error    { return New(KindForbidden, code, msg) }
func Unauthorized(code, msg string) *Error { return New(KindUnauthorized, code, msg) }
func RateLimited(msg string) *Error        { return New(KindRateLimited, "rate_limited", msg) }
func TooLarge(code, msg string) *Error     { return New(KindTooLarge, code, msg) }
func NotImplemented() *Error {
	return New(KindNotImplemented, "not_implemented", "not implemented yet")
}

// Invalid = validation error with field details.
func Invalid(code, msg string, fields ...FieldError) *Error {
	return &Error{Kind: KindInvalid, Code: code, Message: msg, Fields: fields}
}

// As unwraps to *Error. ok=false -> internal.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsKind reports err is app error of kind.
func IsKind(err error, k Kind) bool {
	e, ok := As(err)
	return ok && e.Kind == k
}

// Fields collects field errors. empty -> nil error.
type Fields []FieldError

// Add appends one field error.
func (f *Fields) Add(field, code, msg string) {
	*f = append(*f, FieldError{Field: field, Code: code, Message: msg})
}

// Err returns Invalid if any field error.
func (f Fields) Err(code, msg string) error {
	if len(f) == 0 {
		return nil
	}
	return Invalid(code, msg, f...)
}
