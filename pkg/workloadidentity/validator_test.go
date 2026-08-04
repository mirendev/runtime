package workloadidentity

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testIssuerURL must match the IssuerURL that the shared testIssuer helper
// (pkg/workloadidentity, system_workload_test.go) builds its issuer with, so
// tokens minted here with this issuer claim verify against it.
const testIssuerURL = "https://example.miren.cloud"

// signAs mints a token with arbitrary claims and headers, bypassing the issuer's
// own minting path. Tests that need a token the issuer would never produce (a
// forged alg, a foreign key, an expired exp) build it here.
func signAs(t *testing.T, key *signingKey, claims WorkloadClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(key.method, claims)
	token.Header["kid"] = key.kid
	signed, err := token.SignedString(key.private)
	require.NoError(t, err)
	return signed
}

func validClaims() WorkloadClaims {
	now := time.Now()
	return WorkloadClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuerURL,
			Subject:   "org:org-123:app:myapp:sandbox:sandbox-1",
			Audience:  jwt.ClaimStrings{APIAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
		App:            "myapp",
		SandboxID:      "sandbox-1",
	}
}

func TestValidate_MintedToken(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	tokenStr, err := iss.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	claims, err := v.Validate(tokenStr)
	require.NoError(t, err)

	assert.Equal(t, "myapp", claims.App)
	assert.Equal(t, "sandbox-1", claims.SandboxID)
	assert.Equal(t, "org-123", claims.OrganizationID)
	assert.Equal(t, "cluster-456", claims.ClusterID)
	assert.Equal(t, "org:org-123:app:myapp:sandbox:sandbox-1", claims.Subject)
}

func TestValidate_Expired(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	claims := validClaims()
	past := time.Now().Add(-2 * time.Hour)
	claims.IssuedAt = jwt.NewNumericDate(past)
	claims.NotBefore = jwt.NewNumericDate(past)
	claims.ExpiresAt = jwt.NewNumericDate(past.Add(time.Hour))

	_, err := v.Validate(signAs(t, iss.primary, claims))
	require.ErrorIs(t, err, jwt.ErrTokenExpired)
}

func TestValidate_MissingExpiry(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	claims := validClaims()
	claims.ExpiresAt = nil

	_, err := v.Validate(signAs(t, iss.primary, claims))
	require.Error(t, err)
}

func TestValidate_WrongIssuer(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	claims := validClaims()
	claims.Issuer = "https://evil.example.com"

	_, err := v.Validate(signAs(t, iss.primary, claims))
	require.ErrorIs(t, err, jwt.ErrTokenInvalidIssuer)
}

// A token minted for an external relying party must not be replayable against
// our own API. This is the reason the validator pins the audience at all.
func TestValidate_FederationTokenRejected(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sandbox-1", TokenOptions{
		Audience: []string{"sts.amazonaws.com"},
	})
	require.NoError(t, err)

	_, err = v.Validate(tokenStr)
	require.ErrorIs(t, err, jwt.ErrTokenInvalidAudience)
}

func TestValidate_ForeignKey(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	// A well-formed token from an attacker's key, but carrying a kid we do
	// publish — so it gets past key lookup and dies on the signature.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	foreign, err := newSigningKey(priv)
	require.NoError(t, err)
	foreign.kid = iss.primary.kid

	_, err = v.Validate(signAs(t, foreign, validClaims()))
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
}

func TestValidate_UnknownKID(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	foreign, err := newSigningKey(priv)
	require.NoError(t, err)

	_, err = v.Validate(signAs(t, foreign, validClaims()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key")
}

func TestValidate_MissingKID(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	token := jwt.NewWithClaims(iss.primary.method, validClaims())
	signed, err := token.SignedString(iss.primary.private)
	require.NoError(t, err)

	_, err = v.Validate(signed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no kid")
}

func TestValidate_AlgNone(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	token := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims())
	token.Header["kid"] = iss.primary.kid
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = v.Validate(signed)
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
}

// The classic confusion attack: sign with HMAC, keyed by the public key the
// verifier will look up by kid. WithValidMethods is what stops it.
func TestValidate_AlgHMACConfusion(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	jwks, err := iss.JWKSDocument()
	require.NoError(t, err)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
	token.Header["kid"] = iss.primary.kid
	signed, err := token.SignedString(jwks)
	require.NoError(t, err)

	_, err = v.Validate(signed)
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
}

// Clusters provisioned before the RS256 default still have tokens in flight
// signed by their EdDSA key, which stays advertised for verification.
func TestValidate_LegacyEdDSAKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	writeLegacyEdDSAKey(t, keyPath)

	iss, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      testIssuerURL,
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)
	require.Equal(t, "RS256", iss.primary.alg)

	// Mint under the demoted EdDSA key, as a token issued before the migration
	// would have been.
	prevPEM, err := os.ReadFile(keyPath + ".prev")
	require.NoError(t, err)
	edKey, err := loadSigningKeyFromPEM(string(prevPEM))
	require.NoError(t, err)
	require.Equal(t, "EdDSA", edKey.alg)

	claims, err := NewValidator(iss).Validate(signAs(t, edKey, validClaims()))
	require.NoError(t, err)
	assert.Equal(t, "myapp", claims.App)
}

// A token signed by the previous key keeps verifying through the rotation
// overlap — that overlap is the whole point of the .prev slot.
func TestValidate_RotationOverlap(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "server", "workload-identity.key")

	first, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      testIssuerURL,
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)

	oldToken, err := first.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	// Rotate: the operator moves the key aside and restarts.
	require.NoError(t, os.Rename(keyPath, keyPath+".prev"))

	second, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      testIssuerURL,
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.primary.kid, second.primary.kid)

	v := NewValidator(second)

	claims, err := v.Validate(oldToken)
	require.NoError(t, err, "token signed by the previous key must still verify")
	assert.Equal(t, "sandbox-1", claims.SandboxID)

	newToken, err := second.IssueToken("myapp", "sandbox-2")
	require.NoError(t, err)
	_, err = v.Validate(newToken)
	require.NoError(t, err)
}

// resolveAppName returns "" silently on lookup failure, so an app-less token is
// reachable in practice. rpc.AllowApp treats an unbound caller as unscoped, so
// authenticating one would hand it every app.
func TestValidate_MissingApp(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	claims := validClaims()
	claims.App = ""

	_, err := v.Validate(signAs(t, iss.primary, claims))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no app")
}

func TestValidate_MissingSandboxID(t *testing.T) {
	iss := testIssuer(t)
	v := NewValidator(iss)

	claims := validClaims()
	claims.SandboxID = ""

	_, err := v.Validate(signAs(t, iss.primary, claims))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sandbox_id")
}

func TestValidate_FailsClosed(t *testing.T) {
	iss := testIssuer(t)
	tokenStr, err := iss.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)

	for name, v := range map[string]*Validator{
		"nil validator": nil,
		"nil issuer":    NewValidator(nil),
		"empty issuer URL": NewValidator(&Issuer{
			primary: iss.primary,
			keys:    iss.keys,
		}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := v.Validate(tokenStr)
			require.ErrorIs(t, err, ErrNoIssuer)
		})
	}
}

func TestValidate_Garbage(t *testing.T) {
	v := NewValidator(testIssuer(t))

	for _, tokenStr := range []string{"", "not-a-jwt", "a.b.c", "....."} {
		_, err := v.Validate(tokenStr)
		require.Error(t, err, "expected %q to be rejected", tokenStr)
	}
}

func TestVerificationKeys_MatchesJWKS(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	edPub := writeLegacyEdDSAKey(t, keyPath)

	iss, err := NewIssuer(IssuerConfig{DataPath: dir, IssuerURL: testIssuerURL})
	require.NoError(t, err)

	keys := iss.VerificationKeys()
	require.Len(t, keys, 2)
	assert.Equal(t, iss.primary.kid, keys[0].KeyID, "primary key comes first")
	assert.Equal(t, "RS256", keys[0].Algorithm)
	assert.Equal(t, computeKID(edPub), keys[1].KeyID)
	assert.Equal(t, "EdDSA", keys[1].Algorithm)
}
