package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/etcdtest"
)

func TestDNSControllerCachesUnknownSNI(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	shared, err := clusterstate.New(client, prefix, []byte("authority"))
	require.NoError(t, err)
	controller := &Controller{certCache: make(map[string]*cachedCert), shared: shared}
	for attempt := range 2 {
		if attempt == 1 {
			offline, err := clientv3.New(clientv3.Config{Endpoints: []string{"http://127.0.0.1:1"}, DialTimeout: time.Second})
			require.NoError(t, err)
			defer offline.Close()
			shared, err := clusterstate.New(offline, prefix, []byte("authority"))
			require.NoError(t, err)
			controller.SetSharedStore(shared)
		}
		serverConn, clientConn := net.Pipe()
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		done := make(chan error, 1)
		go func() {
			done <- tls.Server(serverConn, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: controller.GetCertificate}).HandshakeContext(ctx)
		}()
		err := tls.Client(clientConn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "scanner.example", InsecureSkipVerify: true}).HandshakeContext(ctx)
		require.Error(t, err)
		serverErr := <-done
		cancel()
		serverConn.Close()
		clientConn.Close()
		require.ErrorContains(t, serverErr, "no certificate available for scanner.example")
		require.NotErrorIs(t, serverErr, context.DeadlineExceeded)
	}
}

func TestDNSControllerCachedHandshakeDoesNotNeedEtcd(t *testing.T) {
	client, err := clientv3.New(clientv3.Config{Endpoints: []string{"http://127.0.0.1:1"}, DialTimeout: time.Second})
	require.NoError(t, err)
	defer client.Close()
	shared, err := clusterstate.New(client, "/test", []byte("authority"))
	require.NoError(t, err)
	ca, err := caauth.New(caauth.Options{CommonName: "test", ValidFor: time.Hour})
	require.NoError(t, err)
	cert, err := ca.IssueCertificate(caauth.Options{CommonName: "example.com", DNSNames: []string{"example.com"}, ValidFor: time.Hour})
	require.NoError(t, err)
	controller := &Controller{certCache: make(map[string]*cachedCert), shared: shared}
	_, err = controller.loadCertIntoCache("example.com", cert.CertPEM, cert.KeyPEM)
	require.NoError(t, err)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- tls.Server(serverConn, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: controller.GetCertificate}).HandshakeContext(ctx)
	}()
	err = tls.Client(clientConn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "example.com", InsecureSkipVerify: true}).HandshakeContext(ctx)
	require.NoError(t, err)
	require.NoError(t, <-done)
}

func TestDNSControllerPacesProvisioningFailures(t *testing.T) {
	calls := 0
	controller := &Controller{
		Log: slog.Default(),
	}
	domain := "example.com"
	provision := func(context.Context, string) error {
		calls++
		return &acmeObtainError{err: errors.New("provider unavailable")}
	}
	log := controller.Log.With("domain", domain)

	require.NoError(t, controller.provisionRouteCertificate(t.Context(), log, domain, provision))
	require.Equal(t, 1, calls)

	// A framework retry or noisy route update during the cooldown is cheap and
	// does not call the external DNS/ACME provider again.
	require.NoError(t, controller.provisionRouteCertificate(t.Context(), log, domain, provision))
	require.Equal(t, 1, calls)

	controller.failures.Store(domain, time.Now().Add(-dnsFailureCooldown))
	require.NoError(t, controller.provisionRouteCertificate(t.Context(), log, domain, provision))
	require.Equal(t, 2, calls)
}

func TestDNSControllerRetriesLocalProvisioningFailures(t *testing.T) {
	controller := &Controller{Log: slog.Default()}
	domain := "example.com"
	wantErr := errors.New("writing certificate")
	provision := func(context.Context, string) error {
		return wantErr
	}

	err := controller.provisionRouteCertificate(
		t.Context(), controller.Log.With("domain", domain), domain, provision,
	)
	require.ErrorIs(t, err, wantErr)
	require.False(t, controller.inFailureCooldown(domain))
}
