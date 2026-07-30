package rpc

import (
	"fmt"
	"time"
)

type ErrorCategory interface {
	ErrorCategory() string
}

type ErrorCode interface {
	ErrorCode() string
}

type ErrorMessage interface {
	ErrorMessage() string
}

// ResolveErrorKind represents different kinds of capability resolution errors.
//
// The three transport kinds (Unreachable, WentSilent, NoAnswer) exist because
// they have genuinely different causes and different fixes, even though the
// underlying transport reports two of them with the same string. quic-go raises
// its idle-timeout error both for a connection that never completed a handshake
// and for one that completed and later went quiet, so the error value alone
// can't tell them apart — see NetworkClient.classifyTransportError.
type ResolveErrorKind int

const (
	// ResolveHTTPError is a transport failure we couldn't classify further.
	ResolveHTTPError ResolveErrorKind = iota
	// ResolveStatusError is a non-200 response to the lookup.
	ResolveStatusError
	// ResolveDecodeError is a response body we couldn't parse.
	ResolveDecodeError
	// ResolveLookupError is the server telling us it doesn't have the
	// capability. The server answered, so it is reachable and healthy.
	ResolveLookupError
	// ResolveUnreachableError means nothing ever answered: we gave up before
	// any connection completed its handshake. The server is down, the address
	// is wrong, or something is dropping the traffic.
	ResolveUnreachableError
	// ResolveWentSilentError means we had an established connection and it
	// stopped responding mid-request: a crash, a hang, or a lost network path.
	ResolveWentSilentError
	// ResolveNoAnswerError means the connection stayed healthy the whole time
	// but the server never produced a response. The server is running and
	// reachable but isn't replying — wedged, or too old to serve this lookup.
	ResolveNoAnswerError
)

func (k ResolveErrorKind) String() string {
	switch k {
	case ResolveHTTPError:
		return "http"
	case ResolveStatusError:
		return "status"
	case ResolveDecodeError:
		return "decode"
	case ResolveLookupError:
		return "lookup"
	case ResolveUnreachableError:
		return "unreachable"
	case ResolveWentSilentError:
		return "went-silent"
	case ResolveNoAnswerError:
		return "no-answer"
	default:
		return "unknown"
	}
}

// ResolveError represents an error that occurred during capability resolution.
//
// The exported fields carry the structured facts about the failure; turning
// those into user-facing prose is the CLI's job (see wrapRPCError), which is
// also where the cluster name and the command the user typed are known.
type ResolveError struct {
	Kind       ResolveErrorKind
	Err        error
	Msg        string
	StatusCode int // HTTP status code for ResolveStatusError

	// Name is the capability being resolved, e.g. "entities".
	Name string
	// Remote is the address we were talking to, e.g. "localhost:8443".
	Remote string
	// Elapsed is how long we waited before giving up. Zero when the failure
	// wasn't a timeout.
	Elapsed time.Duration
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

// Is matches another *ResolveError of the same kind. Comparing kinds (rather
// than matching any *ResolveError, as this used to) is what makes the exported
// sentinels below meaningful — otherwise errors.Is(err, ErrResolveLookup) was
// true for every resolve failure, including transport ones.
func (e *ResolveError) Is(target error) bool {
	t, ok := target.(*ResolveError)
	return ok && t.Kind == e.Kind
}

// Exported sentinel errors for common resolve error kinds
var (
	ErrResolveHTTP        = &ResolveError{Kind: ResolveHTTPError, Msg: "http request error"}
	ErrResolveStatus      = &ResolveError{Kind: ResolveStatusError, Msg: "unexpected status code"}
	ErrResolveDecode      = &ResolveError{Kind: ResolveDecodeError, Msg: "decode error"}
	ErrResolveLookup      = &ResolveError{Kind: ResolveLookupError, Msg: "lookup error"}
	ErrResolveUnreachable = &ResolveError{Kind: ResolveUnreachableError, Msg: "unreachable"}
	ErrResolveWentSilent  = &ResolveError{Kind: ResolveWentSilentError, Msg: "connection went silent"}
	ErrResolveNoAnswer    = &ResolveError{Kind: ResolveNoAnswerError, Msg: "no answer"}
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
func NewResolveStatusError(name, remote string, statusCode int) error {
	return &ResolveError{
		Kind:       ResolveStatusError,
		StatusCode: statusCode,
		Name:       name,
		Remote:     remote,
		Msg:        fmt.Sprintf("unexpected status code %d from %s while looking up %q", statusCode, remote, name),
	}
}

// NewResolveDecodeError creates a decode error
func NewResolveDecodeError(name, remote string, err error) error {
	return &ResolveError{
		Kind:   ResolveDecodeError,
		Err:    err,
		Name:   name,
		Remote: remote,
		Msg:    fmt.Sprintf("unable to decode the response from %s for %q: %v", remote, name, err),
	}
}

// NewResolveLookupError creates a lookup error: the server answered and told us
// it doesn't have the capability.
func NewResolveLookupError(name, remote, msg string) error {
	return &ResolveError{
		Kind:   ResolveLookupError,
		Name:   name,
		Remote: remote,
		Msg:    msg,
	}
}

// NewResolveUnreachableError reports that nothing ever answered at remote.
func NewResolveUnreachableError(name, remote string, elapsed time.Duration, err error) error {
	return &ResolveError{
		Kind:    ResolveUnreachableError,
		Err:     err,
		Name:    name,
		Remote:  remote,
		Elapsed: elapsed,
		Msg: withUnderlying(err, fmt.Sprintf("no response from %s after %s while looking up %q",
			remote, elapsed.Round(time.Second), name)),
	}
}

// NewResolveWentSilentError reports that an established connection to remote
// stopped responding partway through the lookup.
func NewResolveWentSilentError(name, remote string, elapsed time.Duration, err error) error {
	return &ResolveError{
		Kind:    ResolveWentSilentError,
		Err:     err,
		Name:    name,
		Remote:  remote,
		Elapsed: elapsed,
		Msg: withUnderlying(err, fmt.Sprintf("%s stopped responding after %s while looking up %q",
			remote, elapsed.Round(time.Second), name)),
	}
}

// NewResolveNoAnswerError reports that remote held a healthy connection open
// but never answered the lookup.
func NewResolveNoAnswerError(name, remote string, elapsed time.Duration, err error) error {
	return &ResolveError{
		Kind:    ResolveNoAnswerError,
		Err:     err,
		Name:    name,
		Remote:  remote,
		Elapsed: elapsed,
		Msg: withUnderlying(err, fmt.Sprintf("%s never answered the lookup for %q after %s",
			remote, name, elapsed.Round(time.Second))),
	}
}

// withUnderlying keeps the transport's own wording on the end of our message.
// Our summary is what a person needs; the raw error ("timeout: no recent
// network activity") is what someone debugging the transport needs, and it
// should survive into logs and -v rather than being replaced.
func withUnderlying(err error, msg string) string {
	if err == nil {
		return msg
	}
	return msg + ": " + err.Error()
}
