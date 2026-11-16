package certificate

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// The upgrade ready check probes the ingress by IP, so it sends no SNI. A
// DNS-01 ingress that refused those handshakes failed every upgrade.
func TestDNSControllerAnswersSNILessReadinessProbe(t *testing.T) {
	health := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","server":{"version":"v1.2.3","runtime_instance_id":"inst-1","ready":true}}`))
	})
	probe := func(t *testing.T, controller *Controller) (serverlifecycle.Snapshot, error) {
		// Not StartTLS: it adds a static certificate, and crypto/tls serves
		// that to SNI-less clients without consulting GetCertificate.
		srv := httptest.NewUnstartedServer(health)
		srv.Listener = tls.NewListener(srv.Listener, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: controller.GetCertificate})
		srv.Start()
		t.Cleanup(srv.Close)
		return serverlifecycle.NewHealthProber("https://"+srv.Listener.Addr().String()+"/.well-known/miren/health", "").Probe(t.Context())
	}

	t.Run("without a fallback certificate the probe cannot connect", func(t *testing.T) {
		_, err := probe(t, &Controller{certCache: make(map[string]*cachedCert)})
		require.Error(t, err)
	})

	t.Run("the fallback certificate lets the probe read health", func(t *testing.T) {
		fallback, err := autotls.LoadOrGenerateFallbackCert(t.TempDir())
		require.NoError(t, err)
		snap, err := probe(t, &Controller{certCache: make(map[string]*cachedCert), fallbackCert: &fallback})
		require.NoError(t, err)
		require.True(t, snap.Ready)
		require.Equal(t, "v1.2.3", snap.Version)
		require.Equal(t, "inst-1", snap.InstanceID)
	})
}
