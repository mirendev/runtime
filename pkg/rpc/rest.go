package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// RESTGateway mounts an RPC interface's HTTP-annotated methods onto a
// http.ServeMux as a plain REST/JSON API. It reuses the exact handler
// implementations registered via AdaptXxx — a REST call is dispatched through
// the same rpc.Method.Handler that a CBOR-over-QUIC call would be, backed by a
// restCall that sources arguments from the HTTP request and captures results as
// JSON.
//
// Only methods carrying an http: annotation in the IDL (i.e. with a non-nil
// Method.HTTP) are exposed; the generator rejects an annotation on a
// capability-returning method, which cannot be expressed over REST.
//
// Auth matches the RPC transport: a non-public method requires an authenticated
// identity in the request context. Populating that identity is the job of the
// http.Handler stack in front of the mux, the same way handleCalls does it for
// the QUIC transport.

// RegisterREST mounts every HTTP-annotated method of iface onto mux. Route
// patterns use Go 1.22 method+wildcard syntax ("GET /api/v1/apps/{app}"), so a
// single mux can host multiple interfaces as long as their paths do not
// conflict.
func RegisterREST(mux *http.ServeMux, iface *Interface) {
	for _, m := range iface.Methods() {
		if m.HTTP == nil {
			continue
		}
		mux.HandleFunc(m.HTTP.Verb+" "+m.HTTP.Path, restHandler(m))
	}
}

// maxRESTBodySize bounds how much of a request body the gateway will read
// before rejecting it, so an oversized payload cannot be buffered into memory
// wholesale on its way to the field map.
const maxRESTBodySize = 4 << 20

func restHandler(m Method) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		// A non-public response is identity-specific. Keep it out of browser and
		// intermediary caches so it cannot be replayed to a different caller.
		// Set before the auth check so it covers every response path.
		if !m.Public {
			w.Header().Set("Cache-Control", "no-store")
		}

		// Mirror the auth enforcement handleCalls does for the RPC transport: a
		// non-public method requires an authenticated identity in the context,
		// which the surrounding http.Handler stack is responsible for putting
		// there. Without this a method the QUIC transport would refuse could be
		// called unauthenticated over REST.
		if !m.Public && IdentityFromContext(r.Context()) == nil {
			writeRESTStatus(w, http.StatusUnauthorized, restErrorBody{
				Error: "authentication required",
				Code:  "unauthorized",
			})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxRESTBodySize)

		fields, err := restFields(r, m.HTTP)
		if err != nil {
			status := http.StatusBadRequest

			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				status = http.StatusRequestEntityTooLarge
			}

			writeRESTStatus(w, status, restErrorBody{
				Error: err.Error(),
				Code:  "validation-failure",
			})
			return
		}

		call := &restCall{r: r, binding: m.HTTP, fields: fields}

		if err := m.Handler(r.Context(), call); err != nil {
			writeRESTError(w, err)
			return
		}

		// A field whose JSON type does not match the generated args struct is
		// only discovered when the handler asks for its arguments, by which
		// point the handler has already run. Reporting the 400 anyway beats
		// returning a 200 built from silently-empty args.
		if call.argErr != nil {
			writeRESTStatus(w, http.StatusBadRequest, restErrorBody{
				Error: "invalid arguments: " + call.argErr.Error(),
				Code:  "validation-failure",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if call.results == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		body, err := json.Marshal(call.results)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// restFields assembles the JSON object backing a call's arguments from the
// request body, path wildcards, and query string. Precedence, lowest to
// highest: request body, query string, path parameters.
//
// It runs before the handler is dispatched so that a bad request can be
// rejected with a 400 — rpc.Call.Args has no error return, so nothing after
// dispatch can report a parse failure to the caller cleanly.
func restFields(r *http.Request, binding *HTTPBinding) (map[string]json.RawMessage, error) {
	fields := map[string]json.RawMessage{}

	if binding.Body == "*" && r.Body != nil {
		// An empty or absent body just leaves fields unset, matching the "all
		// optional" shape of generated args. Malformed JSON is different: the
		// caller sent something, so they should hear that it was rejected
		// rather than get a confusing empty-args invocation.
		dec := json.NewDecoder(r.Body)

		err := dec.Decode(&fields)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("invalid request body: %w", err)
		}

		// A literal null decodes into a map without error but leaves it nil,
		// which the binding below would panic on. Every other non-object body
		// already fails the decode, so reject it for the same reason.
		if fields == nil {
			return nil, fmt.Errorf("invalid request body: expected a JSON object")
		}

		// The body is one document, not a stream. Anything after the object is
		// a malformed request rather than something to quietly discard.
		if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("invalid request body: %w", err)
			}
			return nil, fmt.Errorf("invalid request body: expected a single JSON object")
		}
	}

	// Query params carry the non-path inputs of a bodyless verb (GET/DELETE).
	// Each is coerced from its raw string into typed JSON per its declared kind,
	// so bools, numbers, and timestamps land in their typed fields rather than
	// as strings. A parameter that is present but does not parse for its kind is
	// an error: silently dropping it would run the handler with a default value
	// the caller never asked for.
	query := r.URL.Query()
	for _, p := range binding.Query {
		if !query.Has(p.Name) {
			continue
		}

		raw := query.Get(p.Name)

		val, ok := coerceQueryValue(p.Kind, raw)
		if !ok {
			return nil, fmt.Errorf("query parameter %q: %q is not a valid %s", p.Name, raw, p.Kind)
		}
		fields[p.Name] = val
	}

	// Path parameters win: they are the canonical addressing of the resource.
	// Path segments are always strings.
	for _, name := range binding.PathParams {
		if val := r.PathValue(name); val != "" {
			fields[name] = jsonString(val)
		}
	}

	return fields, nil
}

// restCall adapts an HTTP request to the rpc.Call interface. It supports the
// data-plane of a call (arguments in, results out); the capability operations
// panic because capabilities cannot be expressed over plain REST.
type restCall struct {
	r       *http.Request
	binding *HTTPBinding
	fields  map[string]json.RawMessage
	argErr  error
	results any
}

var _ Call = (*restCall)(nil)

// Args unmarshals the request fields assembled by restFields into v (a
// generated *XxxArgs, which implements json.Unmarshaler over its json-tagged
// data struct). The Call interface gives Args no error return, so a failure is
// recorded on argErr for restHandler to turn into a 400.
func (c *restCall) Args(v any) {
	data, err := json.Marshal(c.fields)
	if err != nil {
		c.argErr = err
		return
	}

	if err := json.Unmarshal(data, v); err != nil {
		c.argErr = err
	}
}

// coerceQueryValue turns a raw query-string value into typed JSON according to
// the parameter's kind. It returns ok=false when the value does not parse for
// the kind, so the caller can leave the field unset rather than injecting a
// malformed value.
func coerceQueryValue(kind, raw string) (json.RawMessage, bool) {
	switch kind {
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, false
		}
		return jsonMarshal(b)
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, false
		}
		return jsonMarshal(n)
	case "uint":
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, false
		}
		return jsonMarshal(n)
	case "float":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, false
		}
		return jsonMarshal(f)
	case "timestamp":
		return coerceTimestamp(raw)
	default:
		return jsonString(raw), true
	}
}

// coerceTimestamp accepts an RFC3339 timestamp (with or without sub-second
// precision) or a bare unix-seconds integer and renders it as the JSON object
// shape of standard.Timestamp ({"seconds":N,"nanoseconds":M}), which the
// generated Timestamp.UnmarshalJSON consumes.
func coerceTimestamp(raw string) (json.RawMessage, bool) {
	var t time.Time
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		t = parsed
	} else if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
		t = time.Unix(secs, 0).UTC()
	} else {
		return nil, false
	}

	return jsonMarshal(struct {
		Seconds     int64 `json:"seconds"`
		Nanoseconds int32 `json:"nanoseconds"`
	}{
		Seconds:     t.Unix(),
		Nanoseconds: int32(t.Nanosecond()),
	})
}

func jsonMarshal(v any) (json.RawMessage, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return b, true
}

func (c *restCall) Results(v any) {
	c.results = v
}

// IsAuthenticated reports whether the request carried an authenticated
// identity. The REST gateway relies on the surrounding http.Handler stack (or a
// future auth middleware) to populate the identity in the request context, the
// same way handleCalls does for the RPC transport.
func (c *restCall) IsAuthenticated() bool {
	return IdentityFromContext(c.r.Context()) != nil
}

func (c *restCall) NewCapability(i *Interface) *Capability {
	panic("rpc: capabilities are not supported over the REST gateway")
}

func (c *restCall) NewClient(capa *Capability) Client {
	panic("rpc: capabilities are not supported over the REST gateway")
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// restErrorBody is the JSON shape returned for a failed REST call.
type restErrorBody struct {
	Error    string `json:"error"`
	Code     string `json:"code,omitempty"`
	Category string `json:"category,omitempty"`
}

func writeRESTError(w http.ResponseWriter, err error) {
	body := restErrorBody{Error: err.Error()}

	// errors.As rather than a bare type assertion: handlers routinely wrap a
	// cond.* error with fmt.Errorf("...: %w", err), and an assertion on the
	// outer error would leave the code empty and fall through to a 500.
	var em ErrorMessage
	if errors.As(err, &em) {
		body.Error = em.ErrorMessage()
	}
	var ec ErrorCode
	if errors.As(err, &ec) {
		body.Code = ec.ErrorCode()
	}
	var ecat ErrorCategory
	if errors.As(err, &ecat) {
		body.Category = ecat.ErrorCategory()
	}

	writeRESTStatus(w, restStatusFor(err, body.Code), body)
}

func writeRESTStatus(w http.ResponseWriter, status int, body restErrorBody) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	data, err := json.Marshal(body)
	if err != nil {
		return
	}
	_, _ = w.Write(data)
}

// restStatusFor maps an error's code onto an HTTP status. The codes mirror
// pkg/cond's error taxonomy (not-found, conflict, ...).
func restStatusFor(err error, code string) int {
	switch code {
	case "not-found":
		return http.StatusNotFound
	case "conflict":
		return http.StatusConflict
	case "validation-failure":
		return http.StatusBadRequest
	}

	if errors.Is(err, ErrUnauthorized) {
		return http.StatusForbidden
	}

	return http.StatusInternalServerError
}
