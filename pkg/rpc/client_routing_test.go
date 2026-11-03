package rpc

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/caauth"
)

func TestClientRoutingIsolatesAttempts(t *testing.T) {
	r := require.New(t)
	ca, err := caauth.New(caauth.Options{CommonName: "routing-ca", ValidFor: time.Hour})
	r.NoError(err)
	cert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "routing-server", DNSNames: []string{"routing.test"}, ValidFor: time.Hour,
	})
	r.NoError(err)
	server, err := NewState(t.Context(), WithCertPEMs(cert.CertPEM, cert.KeyPEM))
	r.NoError(err)
	t.Cleanup(func() { _ = server.Close() })
	emptyServer, err := NewState(t.Context(), WithCertPEMs(cert.CertPEM, cert.KeyPEM))
	r.NoError(err)
	t.Cleanup(func() { _ = emptyServer.Close() })
	var calls atomic.Int32
	server.Server().ExposeValue("test", NewInterface([]Method{{
		Name: "fail", InterfaceName: "test", Public: true,
		Handler: func(context.Context, Call) error {
			calls.Add(1)
			return fmt.Errorf("application failure")
		},
	}}, nil))

	routes := 0
	owner, err := NewState(t.Context(), WithSkipVerify,
		WithClientRouting(func(_ context.Context, attempt func(...StateOption) (*NetworkClient, error)) (*NetworkClient, error) {
			routes++
			_, err := attempt(WithEndpoint(server.ListenAddr()),
				WithCertificateVerification(ca.GetCACertificate()), WithTLSServerName("wrong.test"))
			r.Error(err)
			_, err = attempt(WithEndpoint(emptyServer.ListenAddr()),
				WithCertificateVerification(ca.GetCACertificate()), WithTLSServerName("routing.test"))
			r.Error(err, "a missing capability fails after the TLS handshake")
			_, err = attempt()
			r.ErrorContains(err, "no remote address specified", "attempt endpoints must not become defaults")
			return attempt(WithEndpoint(server.ListenAddr()),
				WithCertificateVerification(ca.GetCACertificate()), WithTLSServerName("routing.test"))
		}))
	r.NoError(err)
	t.Cleanup(func() { _ = owner.Close() })

	client, err := owner.Client("test")
	r.NoError(err)
	r.Equal(int32(0), calls.Load(), "routing must not make application calls")

	r.Error(client.Call(t.Context(), "fail", nil, nil))
	r.Equal(int32(1), calls.Load())
	r.Equal(1, routes, "application failure must not reenter routing")

	_, err = owner.Connect(server.ListenAddr(), "test")
	r.NoError(err)
	r.Equal(1, routes, "explicit Connect must bypass routing")
}
