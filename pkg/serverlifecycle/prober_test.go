package serverlifecycle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthProberReadsServerBlock(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 503 from an unhealthy dependency still identifies the process.
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unhealthy","checks":{},"server":{
			"version":"v9.0.1","commit":"abc","runtime_instance_id":"inst-7","ready":true,
			"install_kind":"systemd","components":{"containerd":"v2.0.4","runc":"1.2.2"}}}`))
	}))
	defer srv.Close()

	snap, err := NewHealthProber(srv.URL, "").Probe(context.Background())
	require.NoError(t, err)
	require.Equal(t, Snapshot{
		InstanceID: "inst-7", Version: "v9.0.1", Commit: "abc", Ready: true, InstallKind: "systemd",
		Components: map[string]string{"containerd": "v2.0.4", "runc": "1.2.2"},
	}, snap)
}

func TestHealthProberToleratesServerWithoutComponents(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","checks":{},"server":{"version":"v9.0.0","runtime_instance_id":"inst-1","ready":true}}`))
	}))
	defer srv.Close()

	snap, err := NewHealthProber(srv.URL, "").Probe(context.Background())
	require.NoError(t, err)
	require.Nil(t, snap.Components)
	require.Equal(t, "inst-1", snap.InstanceID)
}
