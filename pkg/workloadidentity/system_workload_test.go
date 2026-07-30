package workloadidentity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testIssuer(t *testing.T) *Issuer {
	t.Helper()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:       t.TempDir(),
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)
	return iss
}

func TestIssueSystemWorkloadToken_Claims(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	claims, err := iss.VerifyToken(token, "miren-registry")
	require.NoError(t, err)

	assert.Equal(t, IdentityTypeSystem, claims.IdentityType)
	assert.Equal(t, SystemWorkloadSandboxController, claims.SystemWorkload)
	assert.Equal(t, "org:org-123:cluster:cluster-456:system:sandboxcontroller", claims.Subject)
	assert.Equal(t, "org-123", claims.OrganizationID)
	assert.Equal(t, "cluster-456", claims.ClusterID)

	// A system workload identity is not scoped to any app or sandbox.
	assert.Empty(t, claims.App)
	assert.Empty(t, claims.SandboxID)
}

func TestIssueSystemWorkloadToken_SubjectOmitsUnsetClusterMetadata(t *testing.T) {
	// A bare-metal cluster with no registration has neither an org nor a
	// cluster id, and the subject should degrade to just the workload rather
	// than emitting empty segments.
	iss, err := NewIssuer(IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://baremetal.example.com",
	})
	require.NoError(t, err)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	claims, err := iss.VerifyToken(token, "miren-registry")
	require.NoError(t, err)
	assert.Equal(t, "system:sandboxcontroller", claims.Subject)
}

func TestParseSystemWorkload(t *testing.T) {
	for _, workload := range []SystemWorkload{SystemWorkloadSandboxController, SystemWorkloadTelemetryWriter} {
		parsed, err := ParseSystemWorkload(string(workload))
		require.NoError(t, err)
		assert.Equal(t, workload, parsed)
	}

	_, err := ParseSystemWorkload("notathing")
	require.Error(t, err)
}

func TestIssueSystemWorkloadToken_RejectsUnknownWorkload(t *testing.T) {
	_, err := testIssuer(t).IssueSystemWorkloadToken(SystemWorkload("notathing"), TokenOptions{})
	require.Error(t, err)
}

// TestVerifySystemWorkloadToken_RejectsSandboxToken is the check the whole scheme
// rests on. Sandboxes can reach cluster-internal services on the bridge, and
// their tokens are signed by the same key as a system workload's, so the
// identity_type claim is the only thing keeping them out.
func TestVerifySystemWorkloadToken_RejectsSandboxToken(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueTokenWithOptions("myapp", "sb-123", TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	// The token itself is perfectly valid...
	claims, err := iss.VerifyToken(token, "miren-registry")
	require.NoError(t, err)
	assert.Equal(t, IdentityTypeSandbox, claims.IdentityType)

	// ...but it is not a system workload identity.
	_, err = iss.VerifySystemWorkloadToken(token, "miren-registry", SystemWorkloadSandboxController)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a system workload identity")
}

func TestVerifySystemWorkloadToken_AcceptsSystemWorkloadToken(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	claims, err := iss.VerifySystemWorkloadToken(token, "miren-registry", SystemWorkloadSandboxController)
	require.NoError(t, err)
	assert.Equal(t, SystemWorkloadSandboxController, claims.SystemWorkload)
}

func TestVerifySystemWorkloadToken_RejectsDifferentSystemWorkload(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadTelemetryWriter, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	_, err = iss.VerifySystemWorkloadToken(token, "miren-registry", SystemWorkloadSandboxController)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `system workload "telemetrywriter", expected "sandboxcontroller"`)
}

func TestVerifySystemWorkloadToken_RejectsUnknownExpectedWorkload(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	_, err = iss.VerifySystemWorkloadToken(token, "miren-registry", SystemWorkload("notathing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown expected system workload "notathing"`)
}

// TestVerifyToken_RejectsWrongAudience covers replay across services: a token
// minted so a system workload can reach one service must not open another.
func TestVerifyToken_RejectsWrongAudience(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-metrics"},
	})
	require.NoError(t, err)

	_, err = iss.VerifyToken(token, "miren-registry")
	require.Error(t, err)

	// Same token, at the service it was actually minted for.
	_, err = iss.VerifyToken(token, "miren-metrics")
	assert.NoError(t, err)
}

func TestVerifyToken_RequiresExpectedAudience(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{})
	require.NoError(t, err)

	// Verifying without naming an audience would silently accept a token minted
	// for anything, so it is refused outright.
	_, err = iss.VerifyToken(token, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audience is required")
}

func TestVerifyToken_RejectsExpiredToken(t *testing.T) {
	iss := testIssuer(t)

	// Build an already-expired token directly; IssueSystemWorkloadToken clamps TTL
	// to MinTTL, so it cannot produce one.
	claims := iss.baseClaims("system:sandboxcontroller", TokenOptions{
		Audience: []string{"miren-registry"},
	})
	claims.IdentityType = IdentityTypeSystem
	claims.SystemWorkload = SystemWorkloadSandboxController
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))

	token, err := iss.sign(claims)
	require.NoError(t, err)

	_, err = iss.VerifyToken(token, "miren-registry")
	assert.Error(t, err)
}

func TestVerifyToken_RejectsTokenFromAnotherCluster(t *testing.T) {
	// Each cluster is its own issuer with its own key, so a neighbouring
	// cluster's token must not verify here even though it is well-formed and
	// names the same audience.
	other := testIssuer(t)
	iss := testIssuer(t)

	token, err := other.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	_, err = iss.VerifyToken(token, "miren-registry")
	assert.Error(t, err)
}

// TestVerifyToken_RejectsUnsignedToken covers the classic JWT confusion attack:
// a caller nominating "none" as the algorithm to skip signature verification.
func TestVerifyToken_RejectsUnsignedToken(t *testing.T) {
	iss := testIssuer(t)

	claims := iss.baseClaims("system:sandboxcontroller", TokenOptions{
		Audience: []string{"miren-registry"},
	})
	claims.IdentityType = IdentityTypeSystem
	claims.SystemWorkload = SystemWorkloadSandboxController

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	unsigned.Header["kid"] = iss.primary.kid
	token, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = iss.VerifyToken(token, "miren-registry")
	assert.Error(t, err)
}

func TestVerifyToken_RejectsTokenWithoutKID(t *testing.T) {
	iss := testIssuer(t)

	claims := iss.baseClaims("system:sandboxcontroller", TokenOptions{
		Audience: []string{"miren-registry"},
	})
	claims.IdentityType = IdentityTypeSystem
	claims.SystemWorkload = SystemWorkloadSandboxController

	// Sign with the real key but omit the kid header.
	token := jwt.NewWithClaims(iss.primary.method, claims)
	signed, err := token.SignedString(iss.primary.private)
	require.NoError(t, err)

	_, err = iss.VerifyToken(signed, "miren-registry")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kid")
}

// TestVerifyToken_AcceptsTokenFromRotatedKey confirms verification follows the
// rotation overlap the issuer already supports: a token signed before a
// rotation stays verifiable while the old key is still advertised.
func TestVerifyToken_AcceptsTokenFromRotatedKey(t *testing.T) {
	dir := t.TempDir()

	before, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)

	token, err := before.IssueSystemWorkloadToken(SystemWorkloadSandboxController, TokenOptions{
		Audience: []string{"miren-registry"},
	})
	require.NoError(t, err)

	// Rotate: the operator moves the current key aside and restarts, which
	// generates a fresh primary and advertises the old one for verification.
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	require.NoError(t, os.Rename(keyPath, keyPath+".prev"))

	after, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)
	require.NotEqual(t, before.primary.kid, after.primary.kid)

	claims, err := after.VerifySystemWorkloadToken(token, "miren-registry", SystemWorkloadSandboxController)
	require.NoError(t, err)
	assert.Equal(t, SystemWorkloadSandboxController, claims.SystemWorkload)
}
