package workloadidentity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/x/workloadid"
)

// Services outside this repository verify our tokens with x/workloadid, over
// HTTP, against the discovery document and key set this issuer serves. Renaming
// a claim or reshaping discovery here would break them without failing
// anything else in this repository, so this test plays their part.
func TestTokensVerifyWithWorkloadid(t *testing.T) {
	var iss *Issuer
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(iss.DiscoveryDocument())
	})
	mux.HandleFunc("/.well-known/miren/jwks", func(w http.ResponseWriter, _ *http.Request) {
		data, err := iss.JWKSDocument()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var err error
	iss, err = NewIssuer(IssuerConfig{
		DataPath:       t.TempDir(),
		IssuerURL:      srv.URL,
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)

	const audience = "https://gatehouse.example"
	v, err := workloadid.NewVerifier(workloadid.VerifierConfig{
		TrustedIssuers:      []string{srv.URL},
		Audience:            audience,
		RequireOrganization: "org-123",
	})
	require.NoError(t, err)

	t.Run("sandbox", func(t *testing.T) {
		tok, err := iss.IssueTokenWithOptions("myapp", "sandbox-1", TokenOptions{
			Audience: []string{audience},
			Role:     "deployer",
		})
		require.NoError(t, err)

		c, err := v.Verify(context.Background(), tok)
		require.NoError(t, err)
		require.Equal(t, workloadid.IdentityTypeSandbox, c.IdentityType)
		require.Equal(t, "myapp", c.App)
		require.Equal(t, "sandbox-1", c.SandboxID)
		require.Equal(t, "cluster-456", c.ClusterID)
		require.Equal(t, "org-123", c.OrganizationID)
		require.Equal(t, "deployer", c.Role)
		require.Empty(t, c.SystemWorkload)
	})

	t.Run("system workload", func(t *testing.T) {
		tok, err := iss.IssueSystemWorkloadToken(SystemWorkloadTelemetryWriter, TokenOptions{
			Audience: []string{audience},
		})
		require.NoError(t, err)

		c, err := v.Verify(context.Background(), tok)
		require.NoError(t, err)
		require.Equal(t, workloadid.IdentityTypeSystem, c.IdentityType)
		require.Equal(t, string(SystemWorkloadTelemetryWriter), c.SystemWorkload)
	})

	t.Run("default audience is refused", func(t *testing.T) {
		tok, err := iss.IssueToken("myapp", "sandbox-1")
		require.NoError(t, err)

		_, err = v.Verify(context.Background(), tok)
		require.Error(t, err, "a token for the cluster API must not satisfy another service")
	})
}
