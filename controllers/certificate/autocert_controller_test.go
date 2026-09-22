package certificate

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newTestAutocertController(t *testing.T) *AutocertController {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:      log,
		DataPath: t.TempDir(),
		Email:    "test@example.com",
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("failed to init autocert controller: %v", err)
	}
	return c
}

func testRouteMeta(id string, host string) (*ingress_v1alpha.HttpRoute, *entity.Meta) {
	route := &ingress_v1alpha.HttpRoute{
		ID:   entity.Id(id),
		Host: host,
	}
	ent := entity.New(entity.Ident, entity.Id(id), route.Encode)
	return route, &entity.Meta{Entity: ent, Revision: 1}
}

func TestAutocertController_Init(t *testing.T) {
	c := newTestAutocertController(t)
	if c.mgr == nil {
		t.Fatal("expected autocert.Manager to be initialized")
	}
}

func TestAutocertController_Reconcile_AddsAllowedHost(t *testing.T) {
	c := newTestAutocertController(t)
	c.SetReady()

	route, meta := testRouteMeta("test-route", "example.com")
	if err := c.Reconcile(context.Background(), route, meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := c.allowedHosts.Load("example.com"); !ok {
		t.Error("expected example.com to be in allowed hosts")
	}
}

func TestAutocertController_Reconcile_EmptyHost(t *testing.T) {
	c := newTestAutocertController(t)
	c.SetReady()

	route, meta := testRouteMeta("test-route", "")
	if err := c.Reconcile(context.Background(), route, meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	c.allowedHosts.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected no allowed hosts, got %d", count)
	}
}

func TestAutocertController_GetCertificate_FallbackForUnknownHost(t *testing.T) {
	c := newTestAutocertController(t)

	hello := &tls.ClientHelloInfo{ServerName: "unknown.example.com"}
	cert, err := c.GetCertificate(hello)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected a fallback certificate, got nil")
		return
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected fallback cert to have certificate data")
	}
}

func TestAutocertController_GetCertificate_FallbackForAllowedHostWithoutCert(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("example.com", struct{}{})

	hello := &tls.ClientHelloInfo{ServerName: "example.com"}
	cert, err := c.GetCertificate(hello)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected a fallback certificate, got nil")
	}
}

func TestAutocertController_HostPolicy(t *testing.T) {
	c := newTestAutocertController(t)

	err := c.mgr.HostPolicy(context.Background(), "unknown.example.com")
	if err == nil {
		t.Error("expected host policy to reject unknown host")
	}

	c.allowedHosts.Store("allowed.example.com", struct{}{})
	err = c.mgr.HostPolicy(context.Background(), "allowed.example.com")
	if err != nil {
		t.Errorf("expected host policy to accept allowed host, got: %v", err)
	}
}

func TestAutocertController_Reconcile_WildcardStoresPattern(t *testing.T) {
	c := newTestAutocertController(t)
	c.SetReady()

	route, meta := testRouteMeta("wildcard-route", "*.example.com")
	if err := c.Reconcile(context.Background(), route, meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := c.allowedHosts.Load("*.example.com"); !ok {
		t.Error("expected *.example.com to be in allowed hosts")
	}
}

// vouchFor installs a HostChecker that allows exactly the given names and
// counts how often it is asked.
func vouchFor(c *AutocertController, names ...string) *atomic.Int32 {
	var calls atomic.Int32
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		allowed[n] = true
	}
	c.SetHostChecker(func(_ context.Context, host string) (bool, error) {
		calls.Add(1)
		return allowed[host], nil
	})
	return &calls
}

// Before MIR-1919, any single label under a wildcard route got a certificate,
// so a scanner guessing names collected one per guess. Now the checker has to
// vouch for each name.
func TestAutocertController_IsAllowedHost_WildcardMatching(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})
	calls := vouchFor(c, "foo.example.com")

	tests := []struct {
		host    string
		allowed bool
	}{
		{"foo.example.com", true},       // vouched for
		{"bar.example.com", false},      // under the wildcard, but nobody vouches
		{"example.com", false},          // bare domain requires its own route
		{"other.com", false},            // unrelated domain
		{"deep.sub.example.com", false}, // only one level of wildcard
	}

	for _, tt := range tests {
		got := c.isAllowedHost(context.Background(), tt.host)
		if got != tt.allowed {
			t.Errorf("isAllowedHost(%q) = %v, want %v", tt.host, got, tt.allowed)
		}
	}

	// Only foo and bar sit under a route; the rest never reach the checker.
	if got := calls.Load(); got != 2 {
		t.Errorf("checker asked %d times, want 2", got)
	}
}

func TestAutocertController_IsAllowedHost_ClosedWithoutChecker(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})
	c.allowedHosts.Store("app.example.com", struct{}{})

	for _, host := range []string{"foo.example.com", "pr-33.app.example.com"} {
		if c.isAllowedHost(context.Background(), host) {
			t.Errorf("isAllowedHost(%q) = true with no checker, want false", host)
		}
	}
	if !c.isAllowedHost(context.Background(), "app.example.com") {
		t.Error("exact route should be allowed without a checker")
	}
}

func TestAutocertController_GetCertificate_WildcardSubdomain(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})

	// Nobody vouches for the name, so it gets the fallback without touching ACME.
	hello := &tls.ClientHelloInfo{ServerName: "foo.example.com"}
	cert, err := c.GetCertificate(hello)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert != &c.fallbackCert {
		t.Fatal("expected the fallback certificate")
	}
	if c.inCooldown("foo.example.com") {
		t.Error("a refused name should never reach autocert, so no failure should be recorded")
	}
}

func TestAutocertController_HostPolicy_WildcardMatching(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})
	vouchFor(c, "foo.example.com")

	if err := c.mgr.HostPolicy(context.Background(), "foo.example.com"); err != nil {
		t.Errorf("expected vouched-for foo.example.com to be accepted, got: %v", err)
	}
	if err := c.mgr.HostPolicy(context.Background(), "bar.example.com"); err == nil {
		t.Error("expected host policy to reject bar.example.com")
	}
	if err := c.mgr.HostPolicy(context.Background(), "other.com"); err == nil {
		t.Error("expected host policy to reject other.com")
	}
}

// An ephemeral subdomain of a normal route gets a certificate only when the
// checker vouches for it (in production, because the ephemeral deploy exists).
// A scanner prepending "www." to a real route must not.
func TestAutocertController_IsAllowedHost_EphemeralSubdomain(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("app.example.com", struct{}{})
	vouchFor(c, "pr-33.app.example.com")

	tests := []struct {
		host    string
		allowed bool
	}{
		{"app.example.com", true},       // exact match (the registered route)
		{"pr-33.app.example.com", true}, // live ephemeral deploy
		{"www.app.example.com", false},  // scanner guess
		{"app.example.org", false},      // unrelated TLD
		{"example.com", false},          // parent of the route, not a subdomain
		{"other.com", false},            // unrelated domain
	}

	for _, tt := range tests {
		got := c.isAllowedHost(context.Background(), tt.host)
		if got != tt.allowed {
			t.Errorf("isAllowedHost(%q) = %v, want %v", tt.host, got, tt.allowed)
		}
	}
}

func TestAutocertController_HostCheck_CachesAnswers(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})
	calls := vouchFor(c, "foo.example.com")

	for range 3 {
		c.isAllowedHost(context.Background(), "foo.example.com")
		c.isAllowedHost(context.Background(), "junk.example.com")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("checker asked %d times, want 2 (one per name)", got)
	}

	c.SetReady()
	reconcile := func(tlsCheck string) {
		t.Helper()
		route, meta := testRouteMeta("wildcard-route", "*.example.com")
		route.TlsCheck = tlsCheck
		if err := c.Reconcile(context.Background(), route, meta); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// A route the controller hasn't seen could change any answer.
	reconcile("/tls-check")
	c.isAllowedHost(context.Background(), "foo.example.com")
	if got := calls.Load(); got != 3 {
		t.Errorf("checker asked %d times after a new route, want 3", got)
	}

	// The hourly resync of an unchanged route keeps what we know.
	reconcile("/tls-check")
	c.isAllowedHost(context.Background(), "foo.example.com")
	if got := calls.Load(); got != 3 {
		t.Errorf("checker asked %d times after a resync, want 3", got)
	}

	// A changed tls_check can change the answer.
	reconcile("/other-check")
	c.isAllowedHost(context.Background(), "foo.example.com")
	if got := calls.Load(); got != 4 {
		t.Errorf("checker asked %d times after tls_check changed, want 4", got)
	}
}

// A refusal is remembered only briefly, because a preview deploy can come up
// right after something probed its name. An approval lasts longer.
func TestAutocertController_HostCheck_RefusalsExpireSooner(t *testing.T) {
	if hostDenyTTL >= hostAllowTTL {
		t.Fatalf("hostDenyTTL (%v) should be shorter than hostAllowTTL (%v)", hostDenyTTL, hostAllowTTL)
	}

	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})
	vouchFor(c, "foo.example.com")

	c.isAllowedHost(context.Background(), "foo.example.com")
	c.isAllowedHost(context.Background(), "junk.example.com")

	if _, ok := c.hostAllowed.Get("foo.example.com"); !ok {
		t.Error("approval should be in the allow cache")
	}
	if _, ok := c.hostDenied.Get("junk.example.com"); !ok {
		t.Error("refusal should be in the deny cache")
	}
}

// The checker drives the ingress proxy path, which can panic. A panic must
// come back as an undecided answer, not crash the process from the
// singleflight goroutine.
func TestAutocertController_HostCheck_RecoversCheckerPanic(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})

	var calls atomic.Int32
	c.SetHostChecker(func(context.Context, string) (bool, error) {
		calls.Add(1)
		panic(http.ErrAbortHandler)
	})

	for range 2 {
		if c.isAllowedHost(context.Background(), "foo.example.com") {
			t.Error("a panicking check must not allow the name")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("checker asked %d times, want 2 (a panic is undecided, not cached)", got)
	}
}

// A scan of random names must not fan out into unbounded asks of an app.
func TestAutocertController_HostCheck_CapsConcurrentChecks(t *testing.T) {
	c := newTestAutocertController(t)
	c.hostCheckSlots = make(chan struct{}, 1)
	c.allowedHosts.Store("*.example.com", struct{}{})

	var calls atomic.Int32
	release := make(chan struct{})
	c.SetHostChecker(func(_ context.Context, host string) (bool, error) {
		calls.Add(1)
		if host == "slow.example.com" {
			<-release
		}
		return true, nil
	})

	go c.isAllowedHost(context.Background(), "slow.example.com")
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond)

	if c.isAllowedHost(context.Background(), "other.example.com") {
		t.Error("a name over the cap should be refused")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("checker asked %d times, want 1 (over the cap never asks)", got)
	}

	close(release)
	require.Eventually(t, func() bool {
		_, ok := c.hostAllowed.Get("slow.example.com")
		return ok
	}, time.Second, time.Millisecond)

	// The slot frees after the answer is cached, so wait for it.
	require.Eventually(t, func() bool { return len(c.hostCheckSlots) == 0 }, time.Second, time.Millisecond)

	// The refusal wasn't remembered, so the name gets a real answer next time.
	if !c.isAllowedHost(context.Background(), "other.example.com") {
		t.Error("expected other.example.com to be allowed once a slot frees up")
	}
}

func TestAutocertController_HostCheck_ErrorsAreNotCached(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})

	var calls atomic.Int32
	c.SetHostChecker(func(context.Context, string) (bool, error) {
		calls.Add(1)
		return true, errors.New("entity store unavailable")
	})

	for range 2 {
		if c.isAllowedHost(context.Background(), "foo.example.com") {
			t.Error("an undecided name must not be allowed")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("checker asked %d times, want 2 (errors are retried)", got)
	}
}

func TestAutocertController_HostCheck_CollapsesConcurrentAsks(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})

	var calls atomic.Int32
	release := make(chan struct{})
	c.SetHostChecker(func(context.Context, string) (bool, error) {
		calls.Add(1)
		<-release
		return true, nil
	})

	const handshakes = 10
	var wg sync.WaitGroup
	for range handshakes {
		wg.Go(func() {
			if !c.isAllowedHost(context.Background(), "foo.example.com") {
				t.Error("expected foo.example.com to be allowed")
			}
		})
	}
	// Let every goroutine join the in-flight ask before it resolves.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("checker asked %d times, want 1", got)
	}
}

// Asking a cold app can block on a sandbox boot far longer than a handshake
// should wait, and the checker doesn't necessarily honor its deadline. The
// handshake gives up on its own; the late answer still helps the next one.
func TestAutocertController_HostCheck_HandshakeDoesNotWaitOnSlowChecker(t *testing.T) {
	c := newTestAutocertController(t)
	c.hostCheckTimeout = 50 * time.Millisecond
	c.allowedHosts.Store("*.example.com", struct{}{})

	var calls atomic.Int32
	release := make(chan struct{})
	c.SetHostChecker(func(context.Context, string) (bool, error) {
		calls.Add(1)
		<-release // ignores its context, like a lease acquisition would
		return true, nil
	})

	start := time.Now()
	if c.isAllowedHost(context.Background(), "foo.example.com") {
		t.Error("expected the handshake to give up and refuse")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("handshake waited %v on the checker", waited)
	}

	close(release)
	require.Eventually(t, func() bool {
		_, ok := c.hostAllowed.Get("foo.example.com")
		return ok
	}, time.Second, time.Millisecond, "the late answer should land in the cache")
	if !c.isAllowedHost(context.Background(), "foo.example.com") {
		t.Error("the late answer should be remembered for the next handshake")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("checker asked %d times, want 1", got)
	}
}

// An answer computed before a route changed must not be remembered after it.
func TestAutocertController_HostCheck_InvalidationDropsInFlightAnswer(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("*.example.com", struct{}{})

	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	c.SetHostChecker(func(context.Context, string) (bool, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return true, nil // the old tls_check said yes
		}
		return false, nil // the new one says no
	})

	first := make(chan bool)
	go func() { first <- c.isAllowedHost(context.Background(), "foo.example.com") }()
	<-started

	c.forgetHostDecisions()
	close(release)
	if !<-first {
		t.Error("the in-flight handshake should still get its answer")
	}

	if c.isAllowedHost(context.Background(), "foo.example.com") {
		t.Error("an answer from before the invalidation must not be reused")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("checker asked %d times, want 2", got)
	}
}

func TestAutocertController_Init_PrePopulatesAllowedHosts(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create http_route entities before Init
	routes := []struct {
		id   string
		host string
	}{
		{"route-1", "example.com"},
		{"route-2", "api.example.com"},
		{"route-3", "*.staging.example.com"},
	}
	for _, r := range routes {
		route := &ingress_v1alpha.HttpRoute{Host: r.host}
		if _, err := server.Client.Create(ctx, r.id, route); err != nil {
			t.Fatalf("failed to create route %s: %v", r.id, err)
		}
	}

	// Create controller with real EAC and init
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:      log,
		EAC:      server.EAC,
		DataPath: t.TempDir(),
		Email:    "test@example.com",
	})
	if err := c.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// Verify all hosts were pre-populated
	for _, r := range routes {
		if _, ok := c.allowedHosts.Load(r.host); !ok {
			t.Errorf("expected %q to be in allowed hosts after Init", r.host)
		}
	}

	// Verify unknown hosts are NOT in allowedHosts
	if _, ok := c.allowedHosts.Load("unknown.com"); ok {
		t.Error("unexpected host in allowed hosts")
	}
}

func TestAutocertController_Reconcile_SkipsDuringFailureCooldown(t *testing.T) {
	c := newTestAutocertController(t)
	c.SetReady()

	// Simulate a recent failure for this domain
	c.failures.Store("cooldown.example.com", acmeFailure{when: time.Now(), cooldown: acmeFailureCooldown})

	route, meta := testRouteMeta("cooldown-route", "cooldown.example.com")
	if err := c.Reconcile(context.Background(), route, meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Domain should still be in allowedHosts (Reconcile always adds it)
	if _, ok := c.allowedHosts.Load("cooldown.example.com"); !ok {
		t.Error("expected domain to be in allowed hosts")
	}

	// Failure entry should still be present (not cleared by a successful provision)
	if _, ok := c.failures.Load("cooldown.example.com"); !ok {
		t.Error("expected failure entry to remain during cooldown")
	}
}

func TestAutocertController_Reconcile_ClearsExpiredCooldown(t *testing.T) {
	c := newTestAutocertController(t)
	c.SetReady()

	// Simulate an old failure (well past cooldown)
	c.failures.Store("expired.example.com", acmeFailure{when: time.Now().Add(-10 * time.Minute), cooldown: acmeFailureCooldown})

	route, meta := testRouteMeta("expired-route", "expired.example.com")
	// This will attempt ACME (and fail since there's no real ACME server),
	// but the important thing is it doesn't skip due to cooldown.
	_ = c.Reconcile(context.Background(), route, meta)

	// The old failure entry should have been deleted before the attempt.
	// A new one may have been stored if the ACME attempt itself failed,
	// but the original stale timestamp should be gone.
	if v, ok := c.failures.Load("expired.example.com"); ok {
		f := v.(acmeFailure)
		if time.Since(f.when) > time.Minute {
			t.Error("expected stale failure entry to be replaced, but old timestamp remains")
		}
	}
}

func TestAutocertController_GetCertificate_RecordsFailureOnTimeout(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("timeout.example.com", struct{}{})

	// GetCertificate will attempt autocert, which will fail/timeout since
	// there's no real ACME server. It should record a failure.
	hello := &tls.ClientHelloInfo{ServerName: "timeout.example.com"}
	cert, err := c.GetCertificate(hello)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected fallback cert, got nil")
	}

	// After the call completes (via timeout or error), the domain should be in failures
	if _, ok := c.failures.Load("timeout.example.com"); !ok {
		t.Error("expected failure to be recorded after GetCertificate timeout/error")
	}
}

func TestAutocertController_GetCertificate_SkipsDuringCooldown(t *testing.T) {
	c := newTestAutocertController(t)
	c.allowedHosts.Store("cooldown.example.com", struct{}{})
	c.failures.Store("cooldown.example.com", acmeFailure{when: time.Now(), cooldown: acmeFailureCooldown})

	hello := &tls.ClientHelloInfo{ServerName: "cooldown.example.com"}
	cert, err := c.GetCertificate(hello)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected fallback cert, got nil")
	}
}

func TestAutocertController_SetReady_Idempotent(t *testing.T) {
	c := newTestAutocertController(t)
	c.SetReady()
	c.SetReady() // should not panic
}

func TestAutocertController_Reconcile_BlocksUntilReady(t *testing.T) {
	c := newTestAutocertController(t)

	route, meta := testRouteMeta("test-route", "example.com")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Reconcile(ctx, route, meta)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestAutocertController_ClusterHostnames_AddedDuringInit(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:              log,
		DataPath:         t.TempDir(),
		Email:            "test@example.com",
		ClusterHostnames: []string{"cluster-abc.miren.systems", "  Cluster-DEF.miren.systems  "},
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	if _, ok := c.allowedHosts.Load("cluster-abc.miren.systems"); !ok {
		t.Error("expected cluster-abc.miren.systems in allowed hosts")
	}
	if _, ok := c.allowedHosts.Load("cluster-def.miren.systems"); !ok {
		t.Error("expected cluster-def.miren.systems in allowed hosts (lowercased/trimmed)")
	}
}

func TestAutocertController_ClusterHostnames_SurviveDelete(t *testing.T) {
	ctx := context.Background()

	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:              log,
		EAC:              server.EAC,
		DataPath:         t.TempDir(),
		Email:            "test@example.com",
		ClusterHostnames: []string{"cluster.miren.systems"},
	})
	if err := c.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// Also add a regular route-derived domain
	c.allowedHosts.Store("app.example.com", struct{}{})

	// Simulate deleting all routes — Delete scans allowedHosts and removes
	// any domain with zero remaining routes.
	if err := c.Delete(ctx, "some-route-id"); err != nil {
		t.Fatalf("unexpected error from Delete: %v", err)
	}

	// Cluster hostname must survive
	if _, ok := c.allowedHosts.Load("cluster.miren.systems"); !ok {
		t.Error("expected cluster hostname to remain in allowed hosts after Delete")
	}

	// Route-derived domain with no remaining routes should be removed
	if _, ok := c.allowedHosts.Load("app.example.com"); ok {
		t.Error("expected route-derived domain to be removed after Delete")
	}
}

func TestAutocertController_ClusterHostnames_IsAllowedHost(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:              log,
		DataPath:         t.TempDir(),
		Email:            "test@example.com",
		ClusterHostnames: []string{"cluster.miren.systems"},
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	tests := []struct {
		host    string
		allowed bool
	}{
		{"cluster.miren.systems", true},
		{"sub.cluster.miren.systems", false}, // under it, but nobody vouches
		{"other.miren.systems", false},
		{"example.com", false},
	}

	for _, tt := range tests {
		got := c.isAllowedHost(context.Background(), tt.host)
		if got != tt.allowed {
			t.Errorf("isAllowedHost(%q) = %v, want %v", tt.host, got, tt.allowed)
		}
	}
}

func TestAutocertController_ClusterHostnames_GetCertificateAttempts(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:              log,
		DataPath:         t.TempDir(),
		Email:            "test@example.com",
		ClusterHostnames: []string{"cluster.miren.systems"},
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// GetCertificate should attempt autocert (and fall back) rather than
	// immediately returning the fallback as it would for an unknown host.
	hello := &tls.ClientHelloInfo{ServerName: "cluster.miren.systems"}
	cert, err := c.GetCertificate(hello)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected a certificate, got nil")
	}

	// After the attempt fails (no real ACME), a failure should be recorded
	// proving the host was treated as allowed (not immediately rejected).
	if _, ok := c.failures.Load("cluster.miren.systems"); !ok {
		t.Error("expected failure recorded, proving autocert was attempted for cluster hostname")
	}
}

func TestAutocertController_ClusterHostnames_EmptyIgnored(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	c := NewAutocertController(AutocertControllerOpts{
		Log:              log,
		DataPath:         t.TempDir(),
		Email:            "test@example.com",
		ClusterHostnames: []string{"", "  "},
	})
	if err := c.Init(context.Background()); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	count := 0
	c.allowedHosts.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected no allowed hosts from empty cluster hostnames, got %d", count)
	}
}
