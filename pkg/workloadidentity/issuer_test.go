package workloadidentity

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIssuer_GeneratesKey(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)
	require.NotNil(t, iss)

	assert.Equal(t, "https://example.miren.cloud", iss.IssuerURL())
	assert.NotEmpty(t, iss.primary.kid)

	// Key file should exist
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	_, err = os.Stat(keyPath)
	assert.NoError(t, err)
}

func TestNewIssuer_LoadsExistingKey(t *testing.T) {
	dir := t.TempDir()

	iss1, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	iss2, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	// Same key should produce same kid
	assert.Equal(t, iss1.primary.kid, iss2.primary.kid)
	assert.Equal(t, iss1.primary.public, iss2.primary.public)
}

func TestIssueToken(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
		ClusterID:      "cluster-456",
	})
	require.NoError(t, err)

	tokenStr, err := iss.IssueToken("myapp", "sandbox-789")
	require.NoError(t, err)
	require.NotEmpty(t, tokenStr)

	// Parse and verify the token
	token, err := jwt.ParseWithClaims(tokenStr, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		assert.Equal(t, jwt.SigningMethodRS256, tok.Method)
		assert.Equal(t, iss.primary.kid, tok.Header["kid"])
		return iss.primary.public, nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims := token.Claims.(*WorkloadClaims)
	assert.Equal(t, "https://example.miren.cloud", claims.Issuer)
	assert.Equal(t, "org:org-123:app:myapp:sandbox:sandbox-789", claims.Subject)
	assert.Equal(t, jwt.ClaimStrings{"miren"}, claims.Audience)
	assert.Equal(t, "org-123", claims.OrganizationID)
	assert.Equal(t, "cluster-456", claims.ClusterID)
	assert.Equal(t, "myapp", claims.App)
	assert.Equal(t, "sandbox-789", claims.SandboxID)
	assert.NotNil(t, claims.IssuedAt)
	assert.NotNil(t, claims.ExpiresAt)
}

func TestIssueToken_RejectsAmbiguousSubject(t *testing.T) {
	iss, err := NewIssuer(IssuerConfig{
		DataPath:       t.TempDir(),
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
	})
	require.NoError(t, err)

	_, err = iss.IssueToken("prod-api:sandbox:x", "sandbox/attacker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `subject app value "prod-api:sandbox:x" contains reserved delimiter ":"`)
}

func TestIssueToken_NoOrg(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	tokenStr, err := iss.IssueToken("myapp", "sandbox-789")
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		return iss.primary.public, nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*WorkloadClaims)
	assert.Equal(t, "app:myapp:sandbox:sandbox-789", claims.Subject)
	assert.Empty(t, claims.OrganizationID)
}

func TestIssueToken_NoApp(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:       dir,
		IssuerURL:      "https://example.miren.cloud",
		OrganizationID: "org-123",
	})
	require.NoError(t, err)

	tokenStr, err := iss.IssueToken("", "sandbox-789")
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		return iss.primary.public, nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*WorkloadClaims)
	assert.Equal(t, "org:org-123:sandbox:sandbox-789", claims.Subject)
}

func TestDiscoveryDocument(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	doc := iss.DiscoveryDocument()

	var parsed map[string]any
	err = json.Unmarshal(doc, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "https://example.miren.cloud", parsed["issuer"])
	assert.Equal(t, "https://example.miren.cloud/.well-known/miren/jwks", parsed["jwks_uri"])
	assert.Contains(t, parsed["id_token_signing_alg_values_supported"], "RS256")
}

func TestJWKSDocument(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	data, err := iss.JWKSDocument()
	require.NoError(t, err)

	var jwks jose.JSONWebKeySet
	err = json.Unmarshal(data, &jwks)
	require.NoError(t, err)

	require.Len(t, jwks.Keys, 1)
	assert.Equal(t, iss.primary.kid, jwks.Keys[0].KeyID)
	assert.Equal(t, "RS256", jwks.Keys[0].Algorithm)
	assert.Equal(t, "sig", jwks.Keys[0].Use)

	// The key should be usable to verify tokens
	pubKey, ok := jwks.Keys[0].Key.(*rsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, iss.primary.public, pubKey)
}

func TestJWKSDocument_WithPreviousKey(t *testing.T) {
	dir := t.TempDir()

	// Create initial issuer (generates key)
	iss1, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)
	oldKID := iss1.primary.kid

	// Simulate key rotation: move current key to .prev
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	err = os.Rename(keyPath, keyPath+".prev")
	require.NoError(t, err)

	// Create new issuer (generates new key, loads old as previous)
	iss2, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)
	assert.NotEqual(t, oldKID, iss2.primary.kid)
	require.NotEmpty(t, iss2.advertised)

	// JWKS should contain both keys
	data, err := iss2.JWKSDocument()
	require.NoError(t, err)

	var jwks jose.JSONWebKeySet
	err = json.Unmarshal(data, &jwks)
	require.NoError(t, err)

	require.Len(t, jwks.Keys, 2)
	assert.Equal(t, iss2.primary.kid, jwks.Keys[0].KeyID)
	assert.Equal(t, oldKID, jwks.Keys[1].KeyID)

	// Token signed by the new key should be verifiable via JWKS
	tokenStr, err := iss2.IssueToken("myapp", "sb-1")
	require.NoError(t, err)

	_, err = jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		kid := tok.Header["kid"].(string)
		keys := jwks.Key(kid)
		require.NotEmpty(t, keys)
		return keys[0].Key, nil
	})
	require.NoError(t, err)

	// Token signed by the OLD key (simulated via iss1) should also verify via JWKS
	oldTokenStr, err := iss1.IssueToken("myapp", "sb-2")
	require.NoError(t, err)

	_, err = jwt.Parse(oldTokenStr, func(tok *jwt.Token) (any, error) {
		kid := tok.Header["kid"].(string)
		keys := jwks.Key(kid)
		require.NotEmpty(t, keys)
		return keys[0].Key, nil
	})
	require.NoError(t, err)
}

func TestIssueTokenWithOptions_CustomAudience(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sb-1", TokenOptions{
		Audience: []string{"sts.amazonaws.com"},
	})
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		return iss.primary.public, nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*WorkloadClaims)
	assert.Equal(t, jwt.ClaimStrings{"sts.amazonaws.com"}, claims.Audience)
}

func TestIssueTokenWithOptions_CustomTTL(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sb-1", TokenOptions{
		TTL: 5 * time.Minute,
	})
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		return iss.primary.public, nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*WorkloadClaims)
	ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time)

	assert.Equal(t, 5*time.Minute, ttl)
}

func TestIssueTokenWithOptions_TTLClamping(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	// Max TTL clamped to 24h
	tokenStr, err := iss.IssueTokenWithOptions("myapp", "sb-1", TokenOptions{
		TTL: 48 * time.Hour,
	})
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(tokenStr, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		return iss.primary.public, nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*WorkloadClaims)
	ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time)

	assert.Equal(t, 24*time.Hour, ttl)

	// Min TTL clamped to 60s
	tokenStr2, err := iss.IssueTokenWithOptions("myapp", "sb-1", TokenOptions{
		TTL: 5 * time.Second,
	})
	require.NoError(t, err)

	token2, err := jwt.ParseWithClaims(tokenStr2, &WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
		return iss.primary.public, nil
	})
	require.NoError(t, err)

	claims2 := token2.Claims.(*WorkloadClaims)
	ttl2 := claims2.ExpiresAt.Sub(claims2.IssuedAt.Time)

	assert.Equal(t, 60*time.Second, ttl2)
}

// writeLegacyEdDSAKey writes a PKCS#8 PEM Ed25519 private key to keyPath,
// emulating a cluster provisioned before the RS256 default. Returns the public
// key for assertions.
func writeLegacyEdDSAKey(t *testing.T, keyPath string) ed25519.PublicKey {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	require.NoError(t, os.MkdirAll(filepath.Dir(keyPath), 0700))
	require.NoError(t, os.WriteFile(keyPath, pemData, 0600))

	return pub
}

func TestNewIssuer_MigratesLegacyEdDSAKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	edPub := writeLegacyEdDSAKey(t, keyPath)
	edKID := computeKID(edPub)

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	// Active signing key is now RS256, not the legacy EdDSA key.
	assert.Equal(t, "RS256", iss.primary.alg)
	_, ok := iss.primary.public.(*rsa.PublicKey)
	require.True(t, ok)
	assert.NotEqual(t, edKID, iss.primary.kid)

	// The legacy EdDSA key was demoted to the .prev slot.
	_, err = os.Stat(keyPath + ".prev")
	require.NoError(t, err)

	// Newly minted tokens are signed with RS256.
	tokenStr, err := iss.IssueToken("myapp", "sandbox-1")
	require.NoError(t, err)
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenStr, &WorkloadClaims{})
	require.NoError(t, err)
	assert.Equal(t, "RS256", parsed.Header["alg"])
	assert.Equal(t, iss.primary.kid, parsed.Header["kid"])

	// JWKS advertises both the active RS256 key and the legacy EdDSA key, the
	// latter keeping its original kid so already-issued tokens still verify.
	data, err := iss.JWKSDocument()
	require.NoError(t, err)
	var jwks jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(data, &jwks))
	require.Len(t, jwks.Keys, 2)

	assert.Equal(t, iss.primary.kid, jwks.Keys[0].KeyID)
	assert.Equal(t, "RS256", jwks.Keys[0].Algorithm)

	assert.Equal(t, edKID, jwks.Keys[1].KeyID)
	assert.Equal(t, "EdDSA", jwks.Keys[1].Algorithm)
	advPub, ok := jwks.Keys[1].Key.(ed25519.PublicKey)
	require.True(t, ok)
	assert.Equal(t, edPub, advPub)

	// Discovery advertises both algorithms, RS256 first.
	var doc map[string]any
	require.NoError(t, json.Unmarshal(iss.DiscoveryDocument(), &doc))
	assert.Equal(t, []any{"RS256", "EdDSA"}, doc["id_token_signing_alg_values_supported"])

	// The freshly minted token verifies under the active RS256 JWK.
	_, err = jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		return jwks.Key(tok.Header["kid"].(string))[0].Key, nil
	})
	require.NoError(t, err)
}

// writeKeyFile generates a signing key of the given algorithm ("RS256" or
// "EdDSA"), writes it as PKCS#8 PEM to path, and returns its kid.
func writeKeyFile(t *testing.T, path, alg string) string {
	t.Helper()

	var priv crypto.Signer
	switch alg {
	case "RS256":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		priv = k
	case "EdDSA":
		_, k, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		priv = k
	default:
		t.Fatalf("unsupported alg %q", alg)
	}

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, pemData, 0600))

	return computeKID(priv.Public())
}

func TestNewIssuer_LoadsDirectoryKeys(t *testing.T) {
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "server", "workload-identity.d")
	edKID := writeKeyFile(t, filepath.Join(keysDir, "extra-eddsa.key"), "EdDSA")
	rsaKID := writeKeyFile(t, filepath.Join(keysDir, "extra-rsa.key"), "RS256")

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	// Primary was auto-generated as RS256; the two directory keys are live.
	assert.Equal(t, "RS256", iss.primary.alg)
	require.Len(t, iss.keys, 3)

	// JWKS publishes all three live keys, keyed by kid.
	data, err := iss.JWKSDocument()
	require.NoError(t, err)
	var jwks jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(data, &jwks))
	require.Len(t, jwks.Keys, 3)
	require.NotEmpty(t, jwks.Key(iss.primary.kid))
	require.NotEmpty(t, jwks.Key(edKID))
	require.NotEmpty(t, jwks.Key(rsaKID))

	// Discovery lists both algorithms.
	var doc map[string]any
	require.NoError(t, json.Unmarshal(iss.DiscoveryDocument(), &doc))
	assert.ElementsMatch(t, []any{"RS256", "EdDSA"}, doc["id_token_signing_alg_values_supported"])

	// Signing still uses the primary key only.
	tokenStr, err := iss.IssueToken("myapp", "sb-1")
	require.NoError(t, err)
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenStr, &WorkloadClaims{})
	require.NoError(t, err)
	assert.Equal(t, "RS256", parsed.Header["alg"])
	assert.Equal(t, iss.primary.kid, parsed.Header["kid"])

	// The token verifies under the primary JWK from JWKS.
	_, err = jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		return jwks.Key(tok.Header["kid"].(string))[0].Key, nil
	})
	require.NoError(t, err)
}

func TestNewIssuer_SkipsUnparseableDirectoryKey(t *testing.T) {
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "server", "workload-identity.d")
	require.NoError(t, os.MkdirAll(keysDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(keysDir, "garbage.key"), []byte("not a pem key"), 0600))
	goodKID := writeKeyFile(t, filepath.Join(keysDir, "good.key"), "EdDSA")

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.NoError(t, err)

	// Primary + the one good directory key; the garbage file is skipped.
	require.Len(t, iss.keys, 2)

	data, err := iss.JWKSDocument()
	require.NoError(t, err)
	var jwks jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(data, &jwks))
	require.Len(t, jwks.Keys, 2)
	require.NotEmpty(t, jwks.Key(iss.primary.kid))
	require.NotEmpty(t, jwks.Key(goodKID))
}

func TestNewIssuer_MigrationRefusesToClobberPrev(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	writeLegacyEdDSAKey(t, keyPath)
	// A pre-existing .prev (e.g. from a prior manual rotation) must not be
	// silently overwritten by the EdDSA→RS256 migration.
	require.NoError(t, os.WriteFile(keyPath+".prev", []byte("existing prev key"), 0600))

	_, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// The legacy primary and the existing .prev are left untouched.
	assert.FileExists(t, keyPath)
	prevData, readErr := os.ReadFile(keyPath + ".prev")
	require.NoError(t, readErr)
	assert.Equal(t, "existing prev key", string(prevData))
}

func TestNewIssuer_FailsOnBrokenPrev(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "server", "workload-identity.key")
	require.NoError(t, os.MkdirAll(filepath.Dir(keyPath), 0700))
	// A present-but-unparseable .prev must fail startup, not be silently dropped.
	require.NoError(t, os.WriteFile(keyPath+".prev", []byte("not a pem key"), 0600))

	_, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://example.miren.cloud",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous signing key")
}

// A cluster with no hostname to advertise still gets a working issuer. This is
// the --without-cloud install with no --dns-names: it has no way to be
// federated against from outside, but its own services still have to
// authenticate to each other, and they verify in-process against these keys.
func TestNewIssuer_AnchorsLocallyWithoutAHostname(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		ClusterID: "cluster-456",
	})
	require.NoError(t, err)

	assert.Equal(t, LocalIssuerURL, iss.IssuerURL())
	assert.Equal(t, "cluster.local", iss.Hostname())
}

// The whole point of issuing on a hostname-less cluster is that the tokens are
// usable, so mint and verify a real one end to end.
func TestLocalAnchoredIssuerRoundTrips(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		ClusterID: "cluster-456",
	})
	require.NoError(t, err)

	token, err := iss.IssueSystemWorkloadToken(
		SystemWorkloadTelemetryWriter,
		TokenOptions{Audience: []string{"miren-telemetry"}},
	)
	require.NoError(t, err)

	claims, err := iss.VerifySystemWorkloadToken(token, "miren-telemetry", SystemWorkloadTelemetryWriter)
	require.NoError(t, err)
	assert.Equal(t, LocalIssuerURL, claims.Issuer)
	assert.Equal(t, IdentityTypeSystem, claims.IdentityType)
	assert.Equal(t, "cluster:cluster-456:system:telemetrywriter", claims.Subject)
}

// An explicit hostname still wins; the local anchor is only a floor.
func TestNewIssuer_PrefersAnExplicitHostname(t *testing.T) {
	dir := t.TempDir()

	iss, err := NewIssuer(IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://cluster-abc.miren.systems",
	})
	require.NoError(t, err)

	assert.Equal(t, "https://cluster-abc.miren.systems", iss.IssuerURL())
}
