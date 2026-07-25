package rpc

import (
	"encoding/json"
	"errors"
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
// Method.HTTP) are exposed; capability-returning and streaming methods must be
// left un-annotated.

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

func restHandler(m Method) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		call := &restCall{r: r, binding: m.HTTP}

		if err := m.Handler(r.Context(), call); err != nil {
			writeRESTError(w, err)
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

// restCall adapts an HTTP request to the rpc.Call interface. It supports the
// data-plane of a call (arguments in, results out); the capability operations
// panic because capabilities cannot be expressed over plain REST.
type restCall struct {
	r       *http.Request
	binding *HTTPBinding
	results any
}

var _ Call = (*restCall)(nil)

// Args assembles a JSON object from the request body, path wildcards, and query
// string, then unmarshals it into v (a generated *XxxArgs, which implements
// json.Unmarshaler over its json-tagged data struct). Precedence, lowest to
// highest: request body, query string, path parameters.
func (c *restCall) Args(v any) {
	fields := map[string]json.RawMessage{}

	if c.binding.Body == "*" && c.r.Body != nil {
		// Ignore decode errors: an empty or absent body just leaves fields
		// unset, matching the "all optional" shape of generated args.
		_ = json.NewDecoder(c.r.Body).Decode(&fields)
	}

	// Query params carry the non-path inputs of a bodyless verb (GET/DELETE).
	// Each is coerced from its raw string into typed JSON per its declared kind,
	// so bools, numbers, and timestamps land in their typed fields rather than
	// as strings. A value that fails to parse for its kind is simply omitted.
	query := c.r.URL.Query()
	for _, p := range c.binding.Query {
		raw := query.Get(p.Name)
		if raw == "" {
			continue
		}
		if val, ok := coerceQueryValue(p.Kind, raw); ok {
			fields[p.Name] = val
		}
	}

	// Path parameters win: they are the canonical addressing of the resource.
	// Path segments are always strings.
	for _, name := range c.binding.PathParams {
		if val := c.r.PathValue(name); val != "" {
			fields[name] = jsonString(val)
		}
	}

	data, err := json.Marshal(fields)
	if err != nil {
		return
	}

	_ = json.Unmarshal(data, v)
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

	if em, ok := err.(ErrorMessage); ok {
		body.Error = em.ErrorMessage()
	}
	if ec, ok := err.(ErrorCode); ok {
		body.Code = ec.ErrorCode()
	}
	if ec, ok := err.(ErrorCategory); ok {
		body.Category = ec.ErrorCategory()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(restStatusFor(err, body.Code))

	data, mErr := json.Marshal(body)
	if mErr != nil {
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
	case "validation":
		return http.StatusBadRequest
	}

	if errors.Is(err, ErrUnauthorized) {
		return http.StatusForbidden
	}

	return http.StatusInternalServerError
}
