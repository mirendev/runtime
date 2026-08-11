package clientconfig

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/caauth"
)

func testAuthority(t *testing.T) *caauth.Authority {
	t.Helper()

	ca, err := caauth.New(caauth.Options{
		CommonName:   "miren-test-ca",
		Organization: "miren",
		ValidFor:     time.Hour,
	})
	require.NoError(t, err)

	return ca
}

func issuedCAPEM(t *testing.T) string {
	t.Helper()
	return string(testAuthority(t).GetCACertificate())
}

func issuedClientPair(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()

	cc, err := testAuthority(t).IssueCertificate(caauth.Options{
		CommonName:   "test-client",
		Organization: "miren",
		ValidFor:     time.Hour,
	})
	require.NoError(t, err)

	return string(cc.CertPEM), string(cc.KeyPEM)
}

// A cluster entry with no credentials on it is a legitimate shape: `miren
// cluster add --cluster X --address Y` writes one when no identity is
// configured, and so does MIREN_CLUSTER=addr;sha1:fp. It used to blow up in
// tls.X509KeyPair, because []byte("") is non-nil and so an empty ClientCert
// pair looked like a certificate that failed to parse. The resulting "tls:
// failed to find any PEM data in certificate input" blamed the certificate for
// a config that simply had none.
func TestCredentiallessClusterDials(t *testing.T) {
	// Otherwise this takes the OIDC branch when the suite itself runs in CI.
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")

	ctx := context.Background()
	config := NewConfig()

	t.Run("without a CA certificate", func(t *testing.T) {
		r := require.New(t)
		cluster := &ClusterConfig{Hostname: "homelab:8443"}

		opts, err := cluster.RPCOptionsWithName(ctx, config, "homelab")
		r.NoError(err)
		r.NotEmpty(opts)

		state, err := cluster.State(ctx, config)
		r.NoError(err)
		r.NotNil(state)
	})

	t.Run("with a pinned CA certificate", func(t *testing.T) {
		r := require.New(t)
		cluster := &ClusterConfig{
			Hostname: "homelab:8443",
			CACert:   issuedCAPEM(t),
		}

		state, err := cluster.State(ctx, config)
		r.NoError(err)
		r.NotNil(state)
	})

	t.Run("a real certificate pair is still honored", func(t *testing.T) {
		r := require.New(t)
		certPEM, keyPEM := issuedClientPair(t)

		cluster := &ClusterConfig{
			Hostname:   "homelab:8443",
			CACert:     issuedCAPEM(t),
			ClientCert: certPEM,
			ClientKey:  keyPEM,
		}

		state, err := cluster.State(ctx, config)
		r.NoError(err)
		r.NotNil(state)
	})

	// A half-configured pair is a genuine mistake rather than an absent
	// credential, so it must not be silently dropped.
	t.Run("a certificate without its key is rejected", func(t *testing.T) {
		r := require.New(t)
		certPEM, _ := issuedClientPair(t)

		cluster := &ClusterConfig{
			Hostname:   "homelab:8443",
			ClientCert: certPEM,
		}

		_, err := cluster.State(ctx, config)
		r.Error(err)
	})
}
