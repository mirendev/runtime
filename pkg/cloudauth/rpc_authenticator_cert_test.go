package cloudauth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/rpc"
)

// TestRPCAuthenticator_CertRequiresVerifiedChain is the cloudauth mirror of the
// rpc-package regression test for the client-cert auth bypass. The cert branch
// of Authenticate must only trust a client cert that the TLS layer verified
// against the cluster CA (VerifiedChains non-empty). A presented-but-unverified
// cert must yield no identity -- otherwise a self-signed forgery would be
// granted the RBAC-bypassing "cert" method in Authorize.
func TestRPCAuthenticator_CertRequiresVerifiedChain(t *testing.T) {
	auth, err := NewRPCAuthenticator(t.Context(), Config{Logger: slog.Default()})
	require.NoError(t, err)
	defer auth.Stop()

	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "test-client"}}

	newReq := func(t *testing.T, tlsState *tls.ConnectionState) *http.Request {
		t.Helper()
		req, err := http.NewRequest("POST", "/_rpc/call/test/method", nil)
		require.NoError(t, err)
		req.TLS = tlsState
		return req
	}

	t.Run("unverified cert yields no identity", func(t *testing.T) {
		// PeerCertificates present but VerifiedChains empty: exactly the state a
		// forged client cert produces under a correctly configured listener, and
		// the state EVERY cert produces if the listener regresses to
		// tls.RequestClientCert. Must not be trusted.
		req := newReq(t, &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
		})
		id, err := auth.Authenticate(t.Context(), rpc.CredentialsFromRequest(req))
		require.NoError(t, err)
		require.Nil(t, id, "unverified client cert must not yield an identity")
	})

	t.Run("verified cert yields cert identity", func(t *testing.T) {
		req := newReq(t, &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
			VerifiedChains:   [][]*x509.Certificate{{cert}},
		})
		id, err := auth.Authenticate(t.Context(), rpc.CredentialsFromRequest(req))
		require.NoError(t, err)
		require.NotNil(t, id)
		require.Equal(t, "test-client", id.Subject)
		require.Equal(t, rpc.AuthMethodCert, id.Method)
	})
}
