package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/dns"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
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
		Tempdir:        dir,
		WorkloadIssuer: issuer,
		tokenSecrets:   secrets,
	}
}

func persistTestTokenSecret(t *testing.T, c *SandboxController, sandboxID, secret string) {
	t.Helper()
	path := filepath.Join(c.Tempdir, "containerd", entity.Id(sandboxID).PathSafe(), tokenSecretFilename)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, writeTokenSecret(path, secret))
}

func forgetTestTokenSecret(r *tokenSecretRegistry, sandboxID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bySandbox, sandboxID)
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

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
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

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
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

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
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

func TestActivityServer_RequiresPostAndWorkloadAuthentication(t *testing.T) {
	c := newTestTokenController(t)
	post := func() *http.Request {
		r := httptest.NewRequest("POST", "/v1/activity", strings.NewReader(`{"state":"idle"}`))
		r.RemoteAddr = testSandboxIP + ":12345"
		return r
	}

	for _, tc := range []struct {
		name string
		req  *http.Request
		want int
	}{
		{name: "method", req: authedRequest("PUT", "/v1/activity"), want: http.StatusMethodNotAllowed},
		{name: "missing secret", req: func() *http.Request {
			return post()
		}(), want: http.StatusUnauthorized},
		{name: "wrong source", req: func() *http.Request {
			r := post()
			r.Header.Set("Authorization", "Bearer "+testSecret)
			r.RemoteAddr = "10.0.0.99:12345"
			return r
		}(), want: http.StatusForbidden},
		{name: "wrong secret", req: func() *http.Request {
			r := post()
			r.Header.Set("Authorization", "Bearer another-sandbox-secret")
			return r
		}(), want: http.StatusForbidden},
		{name: "invalid state", req: func() *http.Request {
			r := httptest.NewRequest("POST", "/v1/activity", strings.NewReader(`{"state":"stopped"}`))
			r.RemoteAddr = testSandboxIP + ":12345"
			r.Header.Set("Authorization", "Bearer "+testSecret)
			return r
		}(), want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.handleActivityRequest(w, tc.req)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestSelfReportedActivityExpiresConservatively(t *testing.T) {
	now := time.Now()
	sb := compute.Sandbox{Status: compute.RUNNING, Activity: compute.Activity{State: compute.IDLE, ReportedAt: now}}
	assert.Equal(t, compute.IDLE, sb.SelfReportedActivity(now.Add(time.Minute)))
	assert.Equal(t, compute.ActivityUnknown, sb.SelfReportedActivity(now.Add(compute.ActivityFreshFor)))
	assert.Equal(t, compute.ActivityUnknown, (compute.Sandbox{}).SelfReportedActivity(now))
	sb.Status = compute.STOPPED
	assert.Equal(t, compute.ActivityUnknown, sb.SelfReportedActivity(now))
}

func TestActivityServer_TransitionsAndTeardown(t *testing.T) {
	ctx := t.Context()
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	id := entity.Id(testSandboxID)
	var e entityserver_v1alpha.Entity
	e.SetId(id.String())
	e.SetAttrs(entity.New(entity.DBId, id, (&compute.Sandbox{Status: compute.RUNNING, HostNetwork: true}).Encode).Attrs())
	_, err := inm.EAC.Put(ctx, &e)
	require.NoError(t, err)

	report := func(state string) int {
		r := httptest.NewRequest("POST", "/v1/activity", strings.NewReader(`{"state":"`+state+`"}`))
		r.RemoteAddr = testSandboxIP + ":12345"
		r.Header.Set("Authorization", "Bearer "+testSecret)
		w := httptest.NewRecorder()
		c.handleActivityRequest(w, r)
		return w.Code
	}
	for _, state := range []string{"idle", "active", "idle"} {
		require.Equal(t, http.StatusNoContent, report(state))
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var sb compute.Sandbox
		sb.Decode(resp.Entity().Entity())
		want := compute.IDLE
		if state == "active" {
			want = compute.ACTIVE
		}
		assert.Equal(t, want, sb.SelfReportedActivity(time.Now()))
		assert.True(t, sb.HostNetwork, "activity reports must preserve unrelated sandbox fields")
	}
	// A delayed earlier request cannot overwrite a newer state.
	before, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var latest compute.Sandbox
	latest.Decode(before.Entity().Entity())
	require.NoError(t, c.recordSandboxActivity(ctx, id.String(), compute.ACTIVE, latest.Activity.ReportedAt.Add(-time.Second)))
	after, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var unchanged compute.Sandbox
	unchanged.Decode(after.Entity().Entity())
	assert.Equal(t, compute.IDLE, unchanged.Activity.State)
	// A runner restart loses the in-memory secret but not the report or the
	// host-side secret; repair permits subsequent transitions.
	persistTestTokenSecret(t, c, id.String(), testSecret)
	forgetTestTokenSecret(c.tokenSecrets, id.String())
	require.Equal(t, http.StatusNoContent, report("active"))
	after, err = inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	unchanged = compute.Sandbox{}
	unchanged.Decode(after.Entity().Entity())
	assert.Equal(t, compute.ACTIVE, unchanged.Activity.State)
	assert.True(t, unchanged.HostNetwork)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id, (&compute.Sandbox{Status: compute.STOPPED}).Encode).Attrs(), 0)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, report("idle"))
}

func TestActivityServer_StartingSandboxIsRetryable(t *testing.T) {
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	id := entity.Id(testSandboxID)
	_, err := inm.EAC.Create(t.Context(), entity.New(entity.DBId, id,
		(&compute.Sandbox{Status: compute.PENDING}).Encode).Attrs())
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/activity", strings.NewReader(`{"state":"idle"}`))
	req.RemoteAddr = testSandboxIP + ":12345"
	req.Header.Set("Authorization", "Bearer "+testSecret)
	w := httptest.NewRecorder()
	c.handleActivityRequest(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	resp, err := inm.EAC.Get(t.Context(), id.String())
	require.NoError(t, err)
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	require.True(t, sb.Activity.ReportedAt.IsZero())
}

func TestActivityServer_CoalescedNewerReportDoesNotExposeOlderIdle(t *testing.T) {
	ctx := t.Context()
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	id := entity.Id(testSandboxID)
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{Status: compute.RUNNING, Activity: compute.Activity{State: compute.ACTIVE, ReportedAt: time.Now()}}).Encode).Attrs())
	require.NoError(t, err)
	order := &activityOrder{issued: 1, latest: 1}
	c.activity = map[string]*activityOrder{id.String(): order}
	order.mu.Lock()
	report := func(state string) <-chan int {
		result := make(chan int, 1)
		go func() {
			r := httptest.NewRequest("POST", "/v1/activity", strings.NewReader(`{"state":"`+state+`"}`))
			r.RemoteAddr = testSandboxIP + ":12345"
			r.Header.Set("Authorization", "Bearer "+testSecret)
			w := httptest.NewRecorder()
			c.handleActivityRequest(w, r)
			result <- w.Code
		}()
		return result
	}
	waitIssued := func(n uint64) {
		t.Helper()
		require.Eventually(t, func() bool {
			c.activityMu.Lock()
			defer c.activityMu.Unlock()
			return order.issued == n
		}, time.Second, time.Millisecond)
	}
	older := report("idle")
	waitIssued(2)
	newer := report("active")
	waitIssued(3)
	order.mu.Unlock()
	require.Equal(t, http.StatusNoContent, <-older)
	require.Equal(t, http.StatusNoContent, <-newer)
	resp, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	require.Equal(t, compute.ACTIVE, sb.Activity.State)
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

	forgetTestTokenSecret(r, "sandbox/old")
	assert.False(t, r.verify("sandbox/old", "secret-old"))
}

func TestTokenSecretRegistry_RetireWinsAgainstInFlightRepair(t *testing.T) {
	r := newTokenSecretRegistry()
	loadStarted := make(chan struct{})
	continueLoad := make(chan struct{})
	repairDone := make(chan struct{})

	go func() {
		defer close(repairDone)
		_, err := r.repair(testSandboxID, func() (string, bool, error) {
			close(loadStarted)
			<-continueLoad
			return testSecret, true, nil
		})
		assert.NoError(t, err)
	}()

	<-loadStarted
	retireDone := make(chan struct{})
	go func() {
		defer close(retireDone)
		assert.NoError(t, r.retire(testSandboxID, func() error { return nil }))
	}()

	close(continueLoad)
	<-repairDone
	<-retireDone
	assert.False(t, r.verify(testSandboxID, testSecret),
		"retirement must revoke a secret loaded by an already-running repair")
}

func TestTokenSecretRegistry_FailedSecretRemovalPreventsRepair(t *testing.T) {
	r := newTokenSecretRegistry()
	r.register(testSandboxID, testSecret)

	err := r.retire(testSandboxID, func() error { return errors.New("disk unavailable") })
	require.EqualError(t, err, "disk unavailable")

	loaded := false
	repaired, err := r.repair(testSandboxID, func() (string, bool, error) {
		loaded = true
		return testSecret, true, nil
	})
	require.NoError(t, err)
	assert.False(t, repaired)
	assert.False(t, loaded, "a retained file must not be read after explicit retirement")
	assert.False(t, r.verify(testSandboxID, testSecret))
}

func TestTokenSecretRegistry_FailedReloadDoesNotConsumeCooldown(t *testing.T) {
	for _, tc := range []struct {
		name string
		load func() (string, bool, error)
	}{
		{name: "read error", load: func() (string, bool, error) {
			return "", false, errors.New("disk unavailable")
		}},
		{name: "missing file", load: func() (string, bool, error) {
			return "", false, nil
		}},
		{name: "empty secret", load: func() (string, bool, error) {
			return "", true, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTokenSecretRegistry()
			repaired, _ := r.repair(testSandboxID, tc.load)
			assert.False(t, repaired)

			repaired, err := r.repair(testSandboxID, func() (string, bool, error) {
				return testSecret, true, nil
			})
			require.NoError(t, err)
			assert.True(t, repaired, "a transient failure must not delay the next usable reload")
			assert.True(t, r.verify(testSandboxID, testSecret))
		})
	}
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

	token, err := jwt.ParseWithClaims(resp.Value, &workloadidentity.WorkloadClaims{}, func(tok *jwt.Token) (any, error) {
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

func TestTokenServer_RepairsLiveSecretRegistryFromPersistedSecret(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*tokenSecretRegistry)
	}{
		{
			name: "missing entry",
			setup: func(secrets *tokenSecretRegistry) {
				forgetTestTokenSecret(secrets, testSandboxID)
			},
		},
		{
			name: "diverged entry",
			setup: func(secrets *tokenSecretRegistry) {
				secrets.register(testSandboxID, "stale-registry-secret")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestTokenController(t)
			persistTestTokenSecret(t, c, testSandboxID, testSecret)
			tc.setup(c.tokenSecrets)

			w := httptest.NewRecorder()
			c.handleTokenRequest(w, authedRequest("GET", "/v1/token"))

			assert.Equal(t, http.StatusOK, w.Code)
			assert.True(t, c.tokenSecrets.verify(testSandboxID, testSecret))
		})
	}
}

func TestTokenServer_PersistedSecretDoesNotAuthorizeWrongBearer(t *testing.T) {
	c := newTestTokenController(t)
	persistTestTokenSecret(t, c, testSandboxID, testSecret)
	forgetTestTokenSecret(c.tokenSecrets, testSandboxID)

	req := authedRequest("GET", "/v1/token")
	req.Header.Set("Authorization", "Bearer wrong-secret")
	w := httptest.NewRecorder()
	c.handleTokenRequest(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.True(t, c.tokenSecrets.verify(testSandboxID, testSecret),
		"trusted host state should be repaired even though this request stays forbidden")

	w = httptest.NewRecorder()
	c.handleTokenRequest(w, authedRequest("GET", "/v1/token"))
	assert.Equal(t, http.StatusOK, w.Code,
		"a wrong request must not consume the legitimate request's recovery opportunity")
}

func TestTokenServer_ConcurrentRequestsShareSecretRepair(t *testing.T) {
	c := newTestTokenController(t)
	persistTestTokenSecret(t, c, testSandboxID, testSecret)
	forgetTestTokenSecret(c.tokenSecrets, testSandboxID)

	const requests = 8
	start := make(chan struct{})
	statuses := make([]int, requests)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			c.handleTokenRequest(w, authedRequest("GET", "/v1/token"))
			statuses[i] = w.Code
		}()
	}

	close(start)
	wg.Wait()
	assert.Equal(t, []int{
		http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK,
		http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK,
	}, statuses)
}

func TestOTLPMetricsEnv(t *testing.T) {
	base := "http://10.8.0.1:7123/v1/metrics"

	env := otlpMetricsEnv([]string{"PORT=3000", "OTEL_SERVICE_NAME=worker"}, base, "abc")
	require.Equal(t, []string{
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://10.8.0.1:7123/v1/metrics/sandbox/otlp/v1/metrics",
		"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL=http/protobuf",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS=Authorization=Bearer%20abc",
	}, env)

	// Any OTLP exporter setting of the app's own means it has a destination in
	// mind, including one that only configures traces, and including one baked
	// into the image rather than set in app.toml. The caller passes both.
	for _, own := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://collector:4318",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://collector:4318/v1/traces",
		"OTEL_EXPORTER_OTLP_HEADERS=x-api-key=k",
	} {
		require.Nil(t, otlpMetricsEnv([]string{"PORT=3000", own}, base, "abc"), own)
	}
}

// An image that sets its own OTLP endpoint with ENV must be left alone even
// though app.toml says nothing, since the image env is applied alongside ours.
func TestOTLPMetricsEnvSeesImageEnv(t *testing.T) {
	imageEnv := []string{"PATH=/usr/bin", "OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example:4318"}
	appEnv := []string{"PORT=3000"}
	require.Nil(t, otlpMetricsEnv(append(imageEnv, appEnv...), "http://10.8.0.1:7123/v1/metrics", "abc"))
}
