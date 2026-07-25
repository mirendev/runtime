package rpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestRESTGateway(t *testing.T) {
	newServer := func() *httptest.Server {
		mux := http.NewServeMux()
		rpc.RegisterREST(mux, example.AdaptMeter(&restMeter{}))
		return httptest.NewServer(mux)
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
	r := require.New(t)

	mux := http.NewServeMux()
	rpc.RegisterREST(mux, app_v1alpha.AdaptLogs(echoLogs{}))
	srv := httptest.NewServer(mux)
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
}
