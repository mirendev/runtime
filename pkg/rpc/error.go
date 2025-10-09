package rpc

import "fmt"

type ErrorCategory interface {
	ErrorCategory() string
}

type ErrorCode interface {
	ErrorCode() string
}

type ErrorMessage interface {
	ErrorMessage() string
}

// ResolveErrorKind represents different kinds of capability resolution errors
type ResolveErrorKind int

const (
	ResolveHTTPError ResolveErrorKind = iota
	ResolveStatusError
	ResolveDecodeError
	ResolveLookupError
)

// ResolveError represents an error that occurred during capability resolution
type ResolveError struct {
	Kind       ResolveErrorKind
	Err        error
	Msg        string
	StatusCode int // HTTP status code for ResolveStatusError
}

func (e *ResolveError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "resolve error"
}

func (e *ResolveError) Unwrap() error {
	return e.Err
}

func (e *ResolveError) Is(target error) bool {
	_, ok := target.(*ResolveError)
	return ok
}

// Exported sentinel errors for common resolve error kinds
var (
	ErrResolveHTTP   = &ResolveError{Kind: ResolveHTTPError, Msg: "http request error"}
	ErrResolveStatus = &ResolveError{Kind: ResolveStatusError, Msg: "unexpected status code"}
	ErrResolveDecode = &ResolveError{Kind: ResolveDecodeError, Msg: "decode error"}
	ErrResolveLookup = &ResolveError{Kind: ResolveLookupError, Msg: "lookup error"}
)

// NewResolveError creates a new ResolveError with the specified kind and underlying error
func NewResolveError(kind ResolveErrorKind, err error, msg string) error {
	return &ResolveError{
		Kind: kind,
		Err:  err,
		Msg:  msg,
	}
}

// NewResolveHTTPError creates an HTTP request error
func NewResolveHTTPError(err error, format string, args ...interface{}) error {
	return &ResolveError{
		Kind: ResolveHTTPError,
		Err:  err,
		Msg:  fmt.Sprintf(format, args...),
	}
}

// NewResolveStatusError creates a status code error
func NewResolveStatusError(statusCode int) error {
	return &ResolveError{
		Kind:       ResolveStatusError,
		StatusCode: statusCode,
		Msg:        fmt.Sprintf("unexpected status code: %d", statusCode),
	}
}

// NewResolveDecodeError creates a decode error
func NewResolveDecodeError(err error) error {
	return &ResolveError{
		Kind: ResolveDecodeError,
		Err:  err,
		Msg:  fmt.Sprintf("unable to decode response body: %v", err),
	}
}

// NewResolveLookupError creates a lookup error
func NewResolveLookupError(msg string) error {
	return &ResolveError{
		Kind: ResolveLookupError,
		Msg:  msg,
	}
}
