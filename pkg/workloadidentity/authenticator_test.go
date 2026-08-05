package workloadidentity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
)

func testAuthenticator(t *testing.T) (*Authenticator, *Issuer) {
	t.Helper()

	iss := testIssuer(t)
	return NewAuthenticator(iss, slog.New(slog.DiscardHandler)), iss
}

func authenticateBearer(t *testing.T, a *Authenticator, token string) (*rpc.Identity, error) {
	t.Helper()

	req := httptest.NewRequest("POST", "/_rpc/call/test/method", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return a.Authenticate(context.Background(), req)
}

func TestAuthenticate_ValidToken(t *testing.T) {
	a, iss := testAuthenticator(t)

	tokenStr, err := iss.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	identity, err := authenticateBearer(t, a, tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)

	assert.Equal(t, rpc.AuthMethodWorkload, identity.Method)
	assert.Equal(t, "org:org-123:app:myapp:sandbox:sandbox-1", identity.Subject)
	assert.Equal(t, "myapp", identity.Metadata["app"])
	assert.Equal(t, "sandbox-1", identity.Metadata["sandbox_id"])
	assert.Equal(t, "org-123", identity.Metadata["organization_id"])
	assert.Equal(t, "cluster-456", identity.Metadata["cluster_id"])
	assert.Empty(t, identity.Groups, "workloads carry no group membership")
}

// The identity must expose its app under the key rpc.BoundApp reads, or the
// handlers' AllowApp guards let it reach every app.
func TestAuthenticate_IdentityIsAppScoped(t *testing.T) {
	a, iss := testAuthenticator(t)

	tokenStr, err := iss.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	identity, err := authenticateBearer(t, a, tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)

	ctx := rpc.ContextWithIdentity(context.Background(), identity)
	assert.Equal(t, "myapp", rpc.BoundApp(ctx))
	assert.True(t, rpc.AllowApp(ctx, "myapp"))
	assert.False(t, rpc.AllowApp(ctx, "otherapp"))
}

// An app-scoped role's identity carries the role and stays confined to its app.
func TestAuthenticate_AppScopedRole(t *testing.T) {
	a, iss := testAuthenticator(t)

	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sandbox-1", TokenOptions{
		Role: workloadroles.RoleAppAdmin,
	})
	require.NoError(t, err)

	identity, err := authenticateBearer(t, a, tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)

	assert.Equal(t, workloadroles.RoleAppAdmin, identity.Metadata["role"])
	assert.Equal(t, "myapp", identity.Metadata["app"])
	assert.Equal(t, "myapp", identity.Metadata["workload_app"])

	ctx := rpc.ContextWithIdentity(context.Background(), identity)
	assert.Equal(t, "myapp", rpc.BoundApp(ctx), "app-scoped role must stay confined")
	assert.False(t, rpc.AllowApp(ctx, "otherapp"))
}

// A cluster-scoped role's identity is not confined to one app: BoundApp is empty
// so AllowApp permits every app. The origin app is preserved for audit only.
func TestAuthenticate_ClusterScopedRoleUnconfines(t *testing.T) {
	a, iss := testAuthenticator(t)

	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sandbox-1", TokenOptions{
		Role: workloadroles.RoleClusterReadonly,
	})
	require.NoError(t, err)

	identity, err := authenticateBearer(t, a, tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)

	assert.Equal(t, workloadroles.RoleClusterReadonly, identity.Metadata["role"])
	assert.Empty(t, identity.Metadata["app"], "cluster role must not bind an app")
	assert.Equal(t, "myapp", identity.Metadata["workload_app"], "origin app kept for audit")

	ctx := rpc.ContextWithIdentity(context.Background(), identity)
	assert.Empty(t, rpc.BoundApp(ctx))
	assert.True(t, rpc.AllowApp(ctx, "myapp"))
	assert.True(t, rpc.AllowApp(ctx, "any-other-app"), "cluster role reaches beyond its own app")
}

// Default role (no role requested) is app-scoped, preserving pre-role behavior.
func TestAuthenticate_DefaultRole(t *testing.T) {
	a, iss := testAuthenticator(t)

	tokenStr, err := iss.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	identity, err := authenticateBearer(t, a, tokenStr)
	require.NoError(t, err)

	assert.Equal(t, workloadroles.Default, identity.Metadata["role"])
	assert.Equal(t, "myapp", identity.Metadata["app"])
}

// An unknown role fails closed: still authenticates (it's a valid token) but
// stays app-confined and — as the authorizer test covers — is denied every
// method.
func TestAuthenticate_UnknownRoleStaysConfined(t *testing.T) {
	a, iss := testAuthenticator(t)

	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sandbox-1", TokenOptions{
		Role: "no-such-role",
	})
	require.NoError(t, err)

	identity, err := authenticateBearer(t, a, tokenStr)
	require.NoError(t, err)

	assert.Equal(t, "no-such-role", identity.Metadata["role"])
	ctx := rpc.ContextWithIdentity(context.Background(), identity)
	assert.Equal(t, "myapp", rpc.BoundApp(ctx), "unknown role must not be treated as cluster-scoped")
}

// Every rejection is (nil, nil), never an error: the RPC server treats an
// authenticator error as terminal, so erroring here would reject requests
// carrying credentials meant for another authenticator in the chain.
func TestAuthenticate_DeclinesQuietly(t *testing.T) {
	a, iss := testAuthenticator(t)

	foreignIssuer, err := NewIssuer(IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://other-cluster.example.com",
	})
	require.NoError(t, err)
	foreignToken, err := foreignIssuer.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	federationToken, err := iss.IssueTokenWithOptions("myapp", "sandbox-1", TokenOptions{
		Audience: []string{"sts.amazonaws.com"},
	})
	require.NoError(t, err)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	forgedKey, err := newSigningKey(priv)
	require.NoError(t, err)
	forgedKey.kid = iss.primary.kid
	forgedToken := signAs(t, forgedKey, validClaims())

	tests := []struct {
		name  string
		token string
	}{
		{"another cluster's issuer", foreignToken},
		{"token minted for an external relying party", federationToken},
		{"forged signature", forgedToken},
		{"not a JWT", "not-a-jwt"},
		{"empty bearer", "   "},
		{"opaque bearer token", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity, err := authenticateBearer(t, a, tt.token)
			assert.NoError(t, err, "must not error: it would abort the chain")
			assert.Nil(t, identity)
		})
	}
}

func TestAuthenticate_NoBearerToken(t *testing.T) {
	a, _ := testAuthenticator(t)

	t.Run("no header", func(t *testing.T) {
		identity, err := authenticateBearer(t, a, "")
		require.NoError(t, err)
		assert.Nil(t, identity)
	})

	t.Run("non-bearer scheme", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/_rpc/call/test/method", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

		identity, err := a.Authenticate(context.Background(), req)
		require.NoError(t, err)
		assert.Nil(t, identity)
	})
}

// An expired token is rejected rather than silently accepted, which is what
// makes the client's token refresh load-bearing.
func TestAuthenticate_ExpiredToken(t *testing.T) {
	a, iss := testAuthenticator(t)

	claims := validClaims()
	claims.ExpiresAt = jwt.NewNumericDate(claims.IssuedAt.Add(-time.Hour))

	identity, err := authenticateBearer(t, a, signAs(t, iss.primary, claims))
	require.NoError(t, err)
	assert.Nil(t, identity)
}
