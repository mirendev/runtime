package rpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
	"miren.dev/runtime/pkg/rpc/standard"
)

// restMeter is a Meter implementation for exercising the REST gateway. It knows
// a single meter ("room1") and returns a cond.NotFound for anything else.
type restMeter struct{}

func (m *restMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	name := call.Args().Name()
	if name != "room1" {
		return cond.NotFound("meter", name)
	}

	reading := new(example.Reading)
	reading.SetMeter(name)
	reading.SetTemperature(21.5)
	call.Results().SetReading(reading)
	return nil
}

func (m *restMeter) GetSetter(ctx context.Context, call *example.MeterGetSetter) error {
	panic("not used in REST tests")
}

// authenticated wraps h the way a real deployment's auth middleware would,
// putting an identity in the request context so non-public methods dispatch.
func authenticated(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := &rpc.Identity{Subject: "tester", Method: rpc.AuthMethodCert}
		h.ServeHTTP(w, r.WithContext(rpc.ContextWithIdentity(r.Context(), id)))
	})
}

func TestRESTGateway(t *testing.T) {
	newServer := func() *httptest.Server {
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, example.AdaptMeter(&restMeter{}))
		return httptest.NewServer(authenticated(mux))
	}

	t.Run("serves an annotated method as REST/JSON", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/meters/room1/temperature")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)
		r.Equal("application/json", resp.Header.Get("Content-Type"))

		body, err := io.ReadAll(resp.Body)
		r.NoError(err)

		var out struct {
			Reading struct {
				Meter       string  `json:"meter"`
				Temperature float32 `json:"temperature"`
			} `json:"reading"`
		}
		r.NoError(json.Unmarshal(body, &out))
		r.Equal("room1", out.Reading.Meter)
		r.Equal(float32(21.5), out.Reading.Temperature)
	})

	t.Run("maps a not-found error onto HTTP 404 with a code", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/meters/basement/temperature")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusNotFound, resp.StatusCode)

		var out struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Equal("not-found", out.Code)
		r.Contains(out.Error, "basement")
	})

	t.Run("un-annotated and unknown routes are not mounted", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		// getSetter has no http: annotation, so nothing is mounted for it.
		resp, err := http.Get(srv.URL + "/api/v1/meters/room1/setter")
		r.NoError(err)
		resp.Body.Close()
		r.Equal(http.StatusNotFound, resp.StatusCode)

		// POST to a GET-only route: the mux rejects the method mismatch.
		resp2, err := http.Post(srv.URL+"/api/v1/meters/room1/temperature", "application/json", nil)
		r.NoError(err)
		resp2.Body.Close()
		r.Equal(http.StatusMethodNotAllowed, resp2.StatusCode)
	})

	t.Run("rejects an unauthenticated call to a non-public method", func(t *testing.T) {
		r := require.New(t)

		// Mounted without the auth middleware, so nothing populates an identity.
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, example.AdaptMeter(&restMeter{}))
		srv := httptest.NewServer(mux)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/meters/room1/temperature")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusUnauthorized, resp.StatusCode)

		var out struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Equal("unauthorized", out.Code)
	})

	t.Run("maps a wrapped error onto its status", func(t *testing.T) {
		r := require.New(t)

		mux := http.NewServeMux()
		rpc.RegisterREST(mux, example.AdaptMeter(&wrappingMeter{}))
		srv := httptest.NewServer(authenticated(mux))
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/meters/basement/temperature")
		r.NoError(err)
		defer resp.Body.Close()

		// The handler wraps cond.NotFound with %w. A bare type assertion would
		// miss it and fall through to a 500.
		r.Equal(http.StatusNotFound, resp.StatusCode)

		var out struct {
			Code string `json:"code"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Equal("not-found", out.Code)
	})
}

// wrappingMeter returns a cond error wrapped in additional context, the common
// Go pattern that a direct type assertion cannot see through.
type wrappingMeter struct{ restMeter }

func (m *wrappingMeter) ReadTemperature(ctx context.Context, call *example.MeterReadTemperature) error {
	name := call.Args().Name()
	return fmt.Errorf("reading meter: %w", cond.NotFound("meter", name))
}

// restReadings echoes back what it was handed so a test can prove the request
// body bound onto the args alongside the path parameter.
type restReadings struct{}

func (restReadings) Record(ctx context.Context, call *example.ReadingsRecord) error {
	reading := new(example.Reading)
	reading.SetMeter(call.Args().Name())
	reading.SetTemperature(call.Args().Temperature())
	call.Results().SetReading(reading)
	return nil
}

func (restReadings) Ping(ctx context.Context, call *example.ReadingsPing) error {
	call.Results().SetOk(true)
	return nil
}

func TestRESTGatewayRequestBody(t *testing.T) {
	newServer := func() *httptest.Server {
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, example.AdaptReadings(restReadings{}))
		return httptest.NewServer(authenticated(mux))
	}

	post := func(t *testing.T, srv *httptest.Server, body string) *http.Response {
		t.Helper()
		resp, err := http.Post(srv.URL+"/api/v1/meters/room1/readings", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		return resp
	}

	t.Run("binds the body alongside path params", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, `{"temperature": 21.5}`)
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)

		var out struct {
			Reading struct {
				Meter       string  `json:"meter"`
				Temperature float32 `json:"temperature"`
			} `json:"reading"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Equal("room1", out.Reading.Meter)
		r.Equal(float32(21.5), out.Reading.Temperature)
	})

	t.Run("an empty body leaves the args unset", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, "")
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)
	})

	t.Run("rejects a malformed body with 400", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		// Previously this ran the handler with empty args and returned 200.
		resp := post(t, srv, `{"temperature": }`)
		defer resp.Body.Close()

		r.Equal(http.StatusBadRequest, resp.StatusCode)

		var out struct {
			Error string `json:"error"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Contains(out.Error, "invalid request body")
	})

	t.Run("rejects a body field of the wrong type with 400", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, `{"temperature": "warm"}`)
		defer resp.Body.Close()

		r.Equal(http.StatusBadRequest, resp.StatusCode)
	})
}

// echoLogs is a Logs implementation that echoes the parsed query parameters
// back so a test can prove typed query binding (timestamp + bool) worked. Only
// the non-streaming AppLogs/SandboxLogs methods are REST-annotated; the
// streaming methods are never reached over the gateway.
type echoLogs struct{}

func (echoLogs) AppLogs(ctx context.Context, state *app_v1alpha.LogsAppLogs) error {
	args := state.Args()

	line := fmt.Sprintf("app=%s follow=%v hasFrom=%v", args.Application(), args.Follow(), args.HasFrom())

	entry := new(app_v1alpha.LogEntry)
	entry.SetLine(line)
	if args.HasFrom() {
		entry.SetTimestamp(args.From())
	}
	state.Results().SetLogs([]*app_v1alpha.LogEntry{entry})
	return nil
}

func (echoLogs) SandboxLogs(ctx context.Context, state *app_v1alpha.LogsSandboxLogs) error {
	return cond.NotFound("sandbox", state.Args().Sandbox())
}

func (echoLogs) StreamLogs(ctx context.Context, state *app_v1alpha.LogsStreamLogs) error {
	panic("streaming is not exposed over REST")
}

func (echoLogs) StreamLogChunks(ctx context.Context, state *app_v1alpha.LogsStreamLogChunks) error {
	panic("streaming is not exposed over REST")
}

func TestRESTGatewayTypedQueryParams(t *testing.T) {
	newServer := func() *httptest.Server {
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, app_v1alpha.AdaptLogs(echoLogs{}))
		return httptest.NewServer(authenticated(mux))
	}

	t.Run("coerces declared params into their typed fields", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		from := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

		resp, err := http.Get(srv.URL + "/api/v1/apps/myapp/logs?from=" + from.Format(time.RFC3339) + "&follow=true")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)

		var out struct {
			Logs []struct {
				Line      string              `json:"line"`
				Timestamp *standard.Timestamp `json:"timestamp"`
			} `json:"logs"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Len(out.Logs, 1)

		// follow=true parsed as a bool, from parsed as a real timestamp (not "").
		r.Equal("app=myapp follow=true hasFrom=true", out.Logs[0].Line)
		r.NotNil(out.Logs[0].Timestamp)
		r.True(from.Equal(standard.FromTimestamp(out.Logs[0].Timestamp)))
	})

	t.Run("rejects a param that does not parse for its kind", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		// "tru" is not a bool. Dropping it would silently run the handler with
		// follow=false, which is not what the caller asked for.
		resp, err := http.Get(srv.URL + "/api/v1/apps/myapp/logs?follow=tru")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusBadRequest, resp.StatusCode)

		var out struct {
			Error string `json:"error"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Contains(out.Error, "follow")
	})

	t.Run("an absent param is left unset", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/api/v1/apps/myapp/logs")
		r.NoError(err)
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)

		var out struct {
			Logs []struct {
				Line string `json:"line"`
			} `json:"logs"`
		}
		r.NoError(json.NewDecoder(resp.Body).Decode(&out))
		r.Len(out.Logs, 1)
		r.Equal("app=myapp follow=false hasFrom=false", out.Logs[0].Line)
	})
}

func TestRESTGatewayBodyLimit(t *testing.T) {
	r := require.New(t)

	mux := http.NewServeMux()
	rpc.RegisterREST(mux, example.AdaptReadings(restReadings{}))
	srv := httptest.NewServer(authenticated(mux))
	defer srv.Close()

	// A body past the limit is refused rather than buffered into memory.
	body := `{"temperature": 1, "pad": "` + strings.Repeat("x", 5<<20) + `"}`

	resp, err := http.Post(srv.URL+"/api/v1/meters/room1/readings", "application/json", strings.NewReader(body))
	r.NoError(err)
	defer resp.Body.Close()

	r.Equal(http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestRESTGatewayBodyShape(t *testing.T) {
	newServer := func() *httptest.Server {
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, example.AdaptReadings(restReadings{}))
		return httptest.NewServer(authenticated(mux))
	}

	post := func(t *testing.T, srv *httptest.Server, body string) *http.Response {
		t.Helper()
		resp, err := http.Post(srv.URL+"/api/v1/meters/room1/readings", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		return resp
	}

	// A literal null decodes into the field map without error but leaves it
	// nil, which the path binding would panic on.
	t.Run("rejects a null body", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, "null")
		defer resp.Body.Close()

		r.Equal(http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("rejects trailing content after the object", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, `{"temperature": 1} {}`)
		defer resp.Body.Close()

		r.Equal(http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("rejects a non-object body", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, `[1, 2, 3]`)
		defer resp.Body.Close()

		r.Equal(http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("marks a non-public response no-store", func(t *testing.T) {
		r := require.New(t)

		srv := newServer()
		defer srv.Close()

		resp := post(t, srv, `{"temperature": 21.5}`)
		defer resp.Body.Close()

		r.Equal(http.StatusOK, resp.StatusCode)
		r.Equal("no-store", resp.Header.Get("Cache-Control"))
	})
}
