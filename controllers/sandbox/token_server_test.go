package sandbox

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/dns"
	"miren.dev/runtime/pkg/workloadidentity"
)

const testSandboxIP = "10.0.0.5"
const testSandboxID = "sandbox/myapp-web-abc123"
const testSecret = "test-secret-token-value"

func newTestTokenController(t *testing.T) *SandboxController {
	t.Helper()

	dir := t.TempDir()
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:       dir,
		IssuerURL:      "https://test.miren.systems",
		OrganizationID: "org-test",
		ClusterID:      "cluster-test",
	})
	require.NoError(t, err)

	log := slog.Default()

	sm := network.NewServiceManager(log, nil)
	sm.AddTestDNSServer(t, func(s *dns.Server) {
		s.AddSandboxMapping(testSandboxID, testSandboxIP, "myapp", "web")
	})

	secrets := newTokenSecretRegistry()
	secrets.register(testSandboxID, testSecret)

	return &SandboxController{
		Log:            log,
		NetServ:        sm,
		WorkloadIssuer: issuer,
		tokenSecrets:   secrets,
	}
}

func authedRequest(method, url string) *http.Request {
	req := httptest.NewRequest(method, url, nil)
	req.RemoteAddr = testSandboxIP + ":12345"
	req.Header.Set("Authorization", "Bearer "+testSecret)
	return req
}

func TestTokenServer_DefaultToken(t *testing.T) {
	c := newTestTokenController(t)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, authedRequest("GET", "/v1/token"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp tokenResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Value)

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (interface{}, error) {
		assert.Equal(t, "RS256", tok.Method.Alg())
		return c.WorkloadIssuer.(*workloadidentity.Issuer).PublicKey(), nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*workloadidentity.WorkloadClaims)
	assert.Equal(t, "myapp", claims.App)
	assert.Equal(t, testSandboxID, claims.SandboxID)
	assert.Equal(t, "org-test", claims.OrganizationID)
	assert.Equal(t, jwt.ClaimStrings{"miren"}, claims.Audience)
}

func TestTokenServer_CustomAudience(t *testing.T) {
	c := newTestTokenController(t)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, authedRequest("GET", "/v1/token?audience=sts.amazonaws.com&audience=myapi.example.com"))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp tokenResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (interface{}, error) {
		assert.Equal(t, "RS256", tok.Method.Alg())
		return c.WorkloadIssuer.(*workloadidentity.Issuer).PublicKey(), nil
	}, jwt.WithAudience("sts.amazonaws.com"))
	require.NoError(t, err)

	claims := token.Claims.(*workloadidentity.WorkloadClaims)
	assert.Equal(t, jwt.ClaimStrings{"sts.amazonaws.com", "myapi.example.com"}, claims.Audience)
}

func TestTokenServer_CustomTTL(t *testing.T) {
	c := newTestTokenController(t)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, authedRequest("GET", "/v1/token?ttl=300"))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp tokenResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (interface{}, error) {
		assert.Equal(t, "RS256", tok.Method.Alg())
		return c.WorkloadIssuer.(*workloadidentity.Issuer).PublicKey(), nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*workloadidentity.WorkloadClaims)
	ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	assert.Equal(t, 300.0, ttl.Seconds())
}

func TestTokenServer_MissingAuth(t *testing.T) {
	c := newTestTokenController(t)

	req := httptest.NewRequest("GET", "/v1/token", nil)
	req.RemoteAddr = testSandboxIP + ":12345"
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestTokenServer_WrongSecret(t *testing.T) {
	c := newTestTokenController(t)

	req := httptest.NewRequest("GET", "/v1/token", nil)
	req.RemoteAddr = testSandboxIP + ":12345"
	req.Header.Set("Authorization", "Bearer wrong-secret")
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTokenServer_UnknownIP(t *testing.T) {
	c := newTestTokenController(t)

	req := httptest.NewRequest("GET", "/v1/token", nil)
	req.RemoteAddr = "10.0.0.99:12345"
	req.Header.Set("Authorization", "Bearer "+testSecret)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestTokenServer_RejectsPost(t *testing.T) {
	c := newTestTokenController(t)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, authedRequest("POST", "/v1/token"))

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestTokenServer_InvalidTTL(t *testing.T) {
	c := newTestTokenController(t)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, authedRequest("GET", "/v1/token?ttl=notanumber"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestTokenSecretRegistry_KeyedBySandboxIdentity pins the property behind keying the
// registry by sandbox identity rather than raw IP: a secret is bound to one sandbox and
// cannot authenticate a different sandbox (e.g. one that later reused a recycled pod IP).
func TestTokenSecretRegistry_KeyedBySandboxIdentity(t *testing.T) {
	r := newTokenSecretRegistry()
	r.register("sandbox/old", "secret-old")

	assert.True(t, r.verify("sandbox/old", "secret-old"))
	assert.False(t, r.verify("sandbox/new", "secret-old"))

	r.unregister("sandbox/old")
	assert.False(t, r.verify("sandbox/old", "secret-old"))
}

// TestTokenServer_RecycledIPResolvesToCurrentSandbox reproduces MIR-1511: a sandbox that
// lands on a recently-recycled address gets 403 "invalid token" forever, because the
// address still resolves to the sandbox that held it before and the presented secret is
// checked against that one. The identity the server would have issued is the *previous*
// sandbox's app, which is why this is a security bug and not only an availability one.
func TestTokenServer_RecycledIPResolvesToCurrentSandbox(t *testing.T) {
	const (
		recycledIP = "10.8.64.17"
		newSandbox = "sandbox/reviewagent-web-NEW"
		oldSandbox = "sandbox/db-app-web-OLD"
		newSecret  = "the-new-sandbox-secret"
	)

	c := newTestTokenController(t)

	sm := network.NewServiceManager(slog.Default(), nil)
	sm.AddTestDNSServer(t, func(s *dns.Server) {
		// The new sandbox takes the address, then a late event for the outgoing one
		// re-registers it — the ordering that left the mapping naming the old sandbox.
		s.AddSandboxMapping(newSandbox, recycledIP, "reviewagent", "web")
		s.AddSandboxMapping(oldSandbox, recycledIP, "db-app", "web")
		s.RemoveSandboxMapping(oldSandbox)
	})
	c.NetServ = sm
	c.tokenSecrets.register(newSandbox, newSecret)

	req := httptest.NewRequest("GET", "/v1/token", nil)
	req.RemoteAddr = recycledIP + ":12345"
	req.Header.Set("Authorization", "Bearer "+newSecret)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, req)
	require.Equal(t, http.StatusOK, w.Code, "the sandbox currently holding the address should get a token")

	var resp tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (interface{}, error) {
		return c.WorkloadIssuer.(*workloadidentity.Issuer).PublicKey(), nil
	})
	require.NoError(t, err)

	claims := token.Claims.(*workloadidentity.WorkloadClaims)
	assert.Equal(t, newSandbox, claims.SandboxID)
	assert.Equal(t, "reviewagent", claims.App,
		"the token must carry the current occupant's identity, never the previous one's")
}

// TestTokenServer_CorrectedMappingUnblocksSandbox covers the recovery path from the
// handler's side: a sandbox locked out by a stale mapping starts working the moment the
// mapping is fixed, with no restart. Re-deriving the mapping needs an entity store, so
// the re-resolution itself is covered in pkg/dns; here the correction is applied directly.
func TestTokenServer_CorrectedMappingUnblocksSandbox(t *testing.T) {
	const (
		liveSandbox = "sandbox/reviewagent-web-LIVE"
		liveSecret  = "the-live-sandbox-secret"
	)

	c := newTestTokenController(t)
	c.tokenSecrets.register(liveSandbox, liveSecret)

	// The address resolves to a sandbox that no longer holds it, so the live sandbox's
	// secret cannot verify against the identity the lookup returns.
	req := httptest.NewRequest("GET", "/v1/token", nil)
	req.RemoteAddr = testSandboxIP + ":12345"
	req.Header.Set("Authorization", "Bearer "+liveSecret)
	w := httptest.NewRecorder()

	c.handleTokenRequest(w, req)
	require.Equal(t, http.StatusForbidden, w.Code,
		"with no entity store to re-derive from, a stale mapping still rejects")

	// Once the mapping is corrected — by the watcher, by the controller registering the
	// sandbox, or by a re-resolution — the same request succeeds without the sandbox
	// having restarted.
	c.NetServ.AddSandboxMapping(liveSandbox, testSandboxIP, "reviewagent", "web")

	w = httptest.NewRecorder()
	c.handleTokenRequest(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRefreshLimiter_BoundsRescansPerAddress(t *testing.T) {
	var l refreshLimiter

	assert.True(t, l.allow("10.8.64.17"), "the first attempt for an address is allowed")
	assert.False(t, l.allow("10.8.64.17"), "a retry within the cooldown is not")
	assert.True(t, l.allow("10.8.64.18"), "the cooldown is per address")
}

func TestRefreshLimiter_SweepsExpiredEntries(t *testing.T) {
	l := refreshLimiter{last: make(map[string]time.Time)}

	// Fill past the sweep threshold with entries old enough to be expired.
	stale := time.Now().Add(-2 * refreshCooldown)
	for i := range refreshLimiterSweepAt {
		l.last[fmt.Sprintf("10.8.%d.%d", i/256, i%256)] = stale
	}

	require.True(t, l.allow("10.8.64.17"))
	assert.Equal(t, 1, len(l.last), "expired entries should be dropped, leaving only the new one")
}

func TestWriteLoadTokenSecret_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), tokenSecretFilename)

	secret, err := generateTokenSecret()
	require.NoError(t, err)

	require.NoError(t, writeTokenSecret(path, secret))

	got, ok, err := loadTokenSecret(path)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, secret, got)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestLoadTokenSecret_TrimsTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), tokenSecretFilename)
	require.NoError(t, os.WriteFile(path, []byte("deadbeef\n"), 0600))

	got, ok, err := loadTokenSecret(path)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "deadbeef", got)
}

func TestLoadTokenSecret_Missing(t *testing.T) {
	got, ok, err := loadTokenSecret(filepath.Join(t.TempDir(), tokenSecretFilename))

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, got)
}

// TestTokenServer_RecoversSecretAfterRestart reproduces MIR-1235: a still-running sandbox
// 403s after the controller/token-server restarts and the in-memory registry is lost, then
// recovers once the persisted secret is reloaded and re-registered for the sandbox —
// without restarting the sandbox.
func TestTokenServer_RecoversSecretAfterRestart(t *testing.T) {
	c := newTestTokenController(t)

	// Simulate a controller/token-server restart: the registry is recreated empty.
	c.tokenSecrets = newTokenSecretRegistry()

	w := httptest.NewRecorder()
	c.handleTokenRequest(w, authedRequest("GET", "/v1/token"))
	require.Equal(t, http.StatusForbidden, w.Code)

	// On start the secret was persisted host-side; boot reconcile reloads it and
	// re-registers it under the sandbox identity. We use a plain t.TempDir() rather
	// than c.sandboxPath(&sb, tokenSecretFilename) because this test exercises the
	// load+register handoff in isolation; sandboxPath construction is covered elsewhere.
	path := filepath.Join(t.TempDir(), tokenSecretFilename)
	require.NoError(t, writeTokenSecret(path, testSecret))

	secret, ok, err := loadTokenSecret(path)
	require.NoError(t, err)
	require.True(t, ok)
	c.tokenSecrets.register(testSandboxID, secret)

	w = httptest.NewRecorder()
	c.handleTokenRequest(w, authedRequest("GET", "/v1/token"))
	assert.Equal(t, http.StatusOK, w.Code)
}
