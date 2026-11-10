package certrevocation

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/certrevocation/certrevocation_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

const (
	leaked    = "4f1a9c3e7b2d8a6f0c5e1d3b9a7f2e64"
	apiSerial = "8d3e6a1f4c7b9e20d5f1a3c6b8e2470f"
)

func newTestServer(t *testing.T) (*certrevocation_v1alpha.CertRevocationsClient, *rpc.CertRevocations) {
	t.Helper()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	set := rpc.NewCertRevocations()
	set.Load(nil)
	server := NewServer(testutils.TestLogger(t), inmem.Client, set, apiSerial)
	return certrevocation_v1alpha.NewCertRevocationsClient(rpc.LocalClient(certrevocation_v1alpha.AdaptCertRevocations(server))), set
}

func operator() context.Context {
	return rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "operator"})
}

func TestRevoke(t *testing.T) {
	c, set := newTestServer(t)
	ctx := operator()

	first, err := c.Revoke(ctx, "4F1A9C3E7B2D8A6F0C5E1D3B9A7F2E64", "leaked into a transcript")
	require.NoError(t, err)
	require.False(t, first.AlreadyRevoked())
	require.Equal(t, leaked, first.Revocation().Serial())
	require.Equal(t, "operator", first.Revocation().RevokedBy())
	require.True(t, set.Contains(leaked), "enforced by the time Revoke returns")

	again, err := c.Revoke(ctx, leaked, "a different reason")
	require.NoError(t, err)
	require.True(t, again.AlreadyRevoked())
	require.Equal(t, "leaked into a transcript", again.Revocation().Reason(), "the original record stands")

	listed, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed.Revocations(), 1)
	require.Equal(t, leaked, listed.Revocations()[0].Serial())
}

func TestRevokeRejectsMalformedSerial(t *testing.T) {
	c, set := newTestServer(t)

	_, err := c.Revoke(operator(), "not-a-serial", "")
	require.ErrorContains(t, err, "invalid certificate serial")

	listed, err := c.List(operator())
	require.NoError(t, err)
	require.Empty(t, listed.Revocations())
	require.False(t, set.Contains("not-a-serial"))
}

func TestRevokeRefusesCallersOwnCertificate(t *testing.T) {
	c, set := newTestServer(t)
	ctx := rpc.ContextWithCallerCertSerial(operator(), leaked)

	_, err := c.Revoke(ctx, leaked, "")
	require.ErrorContains(t, err, "refusing to revoke")
	require.False(t, set.Contains(leaked))

	_, err = c.Revoke(ctx, "abc", "")
	require.NoError(t, err, "a different certificate is fine")
}

func TestRevokeRefusesCoordinatorAPICertificate(t *testing.T) {
	c, set := newTestServer(t)

	_, err := c.Revoke(operator(), strings.ToUpper(apiSerial), "")
	require.ErrorContains(t, err, "coordinator's own API certificate")
	require.False(t, set.Contains(apiSerial))

	listed, err := c.List(operator())
	require.NoError(t, err)
	require.Empty(t, listed.Revocations(), "nothing stored for a restart to load")
}

func TestOperatorsOnly(t *testing.T) {
	denied := []struct {
		name     string
		identity *rpc.Identity
	}{
		{"no identity", nil},
		{"runner certificate", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "runner-abc"}},
		{"shared runner certificate", &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "miren-runner"}},
		{"workload", &rpc.Identity{Method: rpc.AuthMethodWorkload, Subject: "w", Metadata: map[string]any{"app": "a"}}},
		{"oidc", &rpc.Identity{Method: rpc.AuthMethodOIDC, Subject: "ci", Metadata: map[string]any{"bound_app": "a"}}},
		{"system", &rpc.Identity{Method: rpc.AuthMethodSystem, Subject: "s"}},
	}
	for _, tt := range denied {
		t.Run(tt.name, func(t *testing.T) {
			c, set := newTestServer(t)
			ctx := context.Background()
			if tt.identity != nil {
				ctx = rpc.ContextWithIdentity(ctx, tt.identity)
			}

			_, err := c.Revoke(ctx, leaked, "")
			require.ErrorContains(t, err, "requires a cluster operator")
			require.False(t, set.Contains(leaked))

			_, err = c.List(ctx)
			require.ErrorContains(t, err, "requires a cluster operator")
		})
	}

	t.Run("cloud operator", func(t *testing.T) {
		c, set := newTestServer(t)
		ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: rpc.AuthMethodJWT, Subject: "person"})
		_, err := c.Revoke(ctx, leaked, "")
		require.NoError(t, err)
		require.True(t, set.Contains(leaked))
	})
}
