//go:build linux

package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/serverconfig"
)

func TestWaitForVictoriaHealthWaitsForHealthyResponse(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	require.NoError(t, waitForVictoriaHealth(t.Context(), "victoria", server.URL))
	require.GreaterOrEqual(t, requests.Load(), int32(2))
}

func TestWaitForVictoriaHealthHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.ErrorContains(t, waitForVictoriaHealth(ctx, "victoria", server.URL), "did not become healthy")
}

func TestVictoriaLogsBootUsesExternalServerWithoutEmbeddedComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			_, _ = io.WriteString(w, `vm_app_version{version="victoria-logs-tags-v1.52.0", short_version="v1.52.0"} 1`+"\n")
			return
		}
		require.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	config := serverconfig.VictoriaLogsConfig{}
	config.SetStartEmbedded(false)
	config.SetAddress(server.URL)
	b := &victoriaLogsBoot{inputs: victoriaLogsBootInputs{
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		config: config,
	}}

	output, err := b.startExternal(t.Context())
	require.NoError(t, err)
	require.Equal(t, server.URL, output.address)
	require.Nil(t, b.server, "external configuration must not create or manage an embedded server")
	require.NoError(t, b.stop(t.Context()))
}

func TestExternalVictoriaLogsVersionWarning(t *testing.T) {
	for _, tt := range []struct {
		name, metrics, wantWarning string
		status                     int
	}{
		{name: "old", metrics: `vm_app_version{version="victoria-logs-tags-v1.0.0", short_version="v1.0.0"} 1`, wantWarning: "v1.0"},
		{name: "minimum supported", metrics: `vm_app_version{version="victoria-logs-tags-v1.50.0", short_version="v1.50.0"} 1`},
		{name: "newer", metrics: `vm_app_version{version="victoria-logs-tags-v1.52.0", short_version="v1.52.0"} 1`},
		{name: "metrics unavailable", status: http.StatusNotFound, wantWarning: "metrics endpoint returned"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/metrics" {
					if tt.status != 0 {
						w.WriteHeader(tt.status)
					}
					_, _ = io.WriteString(w, tt.metrics+"\n")
				}
			}))
			defer server.Close()
			var logs bytes.Buffer
			config := serverconfig.VictoriaLogsConfig{}
			config.SetStartEmbedded(false)
			config.SetAddress(server.URL)
			b := &victoriaLogsBoot{inputs: victoriaLogsBootInputs{
				log: slog.New(slog.NewTextHandler(&logs, nil)), config: config,
			}}
			_, err := b.startExternal(t.Context())
			require.NoError(t, err, "external deployments stay usable")
			if tt.wantWarning == "" {
				require.NotContains(t, logs.String(), "level=WARN")
			} else {
				require.Contains(t, logs.String(), "level=WARN")
				require.Contains(t, logs.String(), tt.wantWarning)
			}
		})
	}
}
