package coordinate

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/workloadidentity"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mustKeyPair(t *testing.T) *cloudauth.KeyPair {
	t.Helper()

	kp, err := cloudauth.GenerateKeyPair()
	require.NoError(t, err)
	return kp
}

// newTestIssuer builds an issuer anchored at issuerURL, with a freshly
// generated signing key on a temp path.
func newTestIssuer(t *testing.T, issuerURL string) *workloadidentity.Issuer {
	t.Helper()

	iss, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:       t.TempDir(),
		IssuerURL:      issuerURL,
		OrganizationID: "org-1",
		ClusterID:      "cluster-1",
	})
	require.NoError(t, err)
	return iss
}

// A cluster only publishes its keys when its tokens actually carry the
// cloud-assigned issuer. Publishing otherwise would have cloud serving a
// discovery document for an issuer no token uses.
func TestAnchoredAtCloud(t *testing.T) {
	const cloudAnchor = "https://api.miren.cloud/identity/cluster-1"

	authClient, err := cloudauth.NewAuthClient("https://api.miren.cloud", mustKeyPair(t))
	require.NoError(t, err)

	tests := []struct {
		name      string
		issuerURL string
		noIssuer  bool
		noClient  bool
		cloud     CloudAuthConfig
		want      bool
	}{
		{
			name:      "anchored at cloud",
			issuerURL: cloudAnchor,
			cloud:     CloudAuthConfig{Enabled: true, IdentityIssuerURL: cloudAnchor},
			want:      true,
		},
		{
			name:      "registered but left on the default anchor",
			issuerURL: "https://cluster-abc.miren.systems",
			cloud:     CloudAuthConfig{Enabled: true, IdentityIssuerURL: cloudAnchor},
			want:      false,
		},
		{
			name:      "registered but cloud assigned no anchor",
			issuerURL: "https://cluster-abc.miren.systems",
			cloud:     CloudAuthConfig{Enabled: true},
			want:      false,
		},
		{
			name:      "unregistered cluster with a hostname",
			issuerURL: "https://bare-metal.example",
			cloud:     CloudAuthConfig{},
			noClient:  true,
			want:      false,
		},
		{
			name:      "unregistered cluster with no hostname at all",
			issuerURL: workloadidentity.LocalIssuerURL,
			cloud:     CloudAuthConfig{},
			noClient:  true,
			want:      false,
		},
		{
			name:     "no issuer",
			noIssuer: true,
			cloud:    CloudAuthConfig{Enabled: true, IdentityIssuerURL: cloudAnchor},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Coordinator{}
			c.CloudAuth = tt.cloud
			if !tt.noIssuer {
				c.WorkloadIssuer = newTestIssuer(t, tt.issuerURL)
			}
			if !tt.noClient {
				c.authClient = authClient
			}

			require.Equal(t, tt.want, c.anchoredAtCloud())
		})
	}
}

// The publish paths must be inert for a cluster that isn't anchored at cloud —
// including one with no cloud at all, where authClient is nil and touching it
// would panic.
func TestPublishSigningKeysIsInertWithoutCloudAnchor(t *testing.T) {
	c := &Coordinator{Log: testLogger()}
	c.WorkloadIssuer = newTestIssuer(t, workloadidentity.LocalIssuerURL)

	published, err := c.publishSigningKeys(context.Background())
	require.NoError(t, err)
	require.False(t, published)

	require.NotPanics(t, func() {
		c.publishSigningKeysAtStartup(context.Background())
	})
}
