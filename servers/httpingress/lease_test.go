package httpingress

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/pkg/entity"
)

// mockActivator implements activator.AppActivator for testing lease behavior.
type mockActivator struct {
	mu           sync.Mutex
	renewCount   int
	releaseCount int
	renewErr     error
	releasedURLs []string
	renewedURLs  []string
}

func (m *mockActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*activator.Lease, error) {
	return &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}, nil
}

func (m *mockActivator) ReleaseLease(ctx context.Context, lease *activator.Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCount++
	m.releasedURLs = append(m.releasedURLs, lease.URL)
	return nil
}

func (m *mockActivator) RenewLease(ctx context.Context, lease *activator.Lease) (*activator.Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renewErr != nil {
		return nil, m.renewErr
	}
	m.renewCount++
	m.renewedURLs = append(m.renewedURLs, lease.URL)
	return lease, nil
}

func (m *mockActivator) Invalidations() <-chan activator.SandboxInvalidation {
	return make(chan activator.SandboxInvalidation)
}

func (m *mockActivator) SetPoolCreator(pc activator.PoolCreator) {}

func newTestServer(aa *mockActivator) *Server {
	return &Server{
		Log:  slog.Default(),
		aa:   aa,
		apps: make(map[string]*appUsage),
	}
}

func TestLeaseKeptAliveWithinTTL(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain a lease (simulates a request acquiring one)
	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)

	// Simulate the request finishing — Uses goes to 0
	srv.releaseLease(ctx, ll)

	if ll.Uses != 0 {
		t.Fatalf("expected Uses=0 after release, got %d", ll.Uses)
	}

	// Run expireLeases — lease should be renewed, not evicted (within TTL)
	srv.expireLeases(ctx)

	aa.mu.Lock()
	defer aa.mu.Unlock()

	if aa.releaseCount != 0 {
		t.Errorf("expected 0 releases (lease within TTL), got %d", aa.releaseCount)
	}
	if aa.renewCount != 1 {
		t.Errorf("expected 1 renewal, got %d", aa.renewCount)
	}

	// Verify the lease is still in cache
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if _, ok := srv.apps["app/myapp"]; !ok {
		t.Error("expected app to still have cached leases")
	}
}

func TestLeaseEvictedAfterTTL(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain and release a lease
	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)
	srv.releaseLease(ctx, ll)

	// Backdate LastUsed to beyond the TTL
	srv.mu.Lock()
	ll.LastUsed = time.Now().Add(-(minLeaseTTL + time.Minute))
	srv.mu.Unlock()

	// Run expireLeases — lease should be evicted
	srv.expireLeases(ctx)

	aa.mu.Lock()
	defer aa.mu.Unlock()

	if aa.releaseCount != 1 {
		t.Errorf("expected 1 release (lease past TTL), got %d", aa.releaseCount)
	}
	if aa.renewCount != 0 {
		t.Errorf("expected 0 renewals, got %d", aa.renewCount)
	}

	// Verify the app is removed from cache
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if _, ok := srv.apps["app/myapp"]; ok {
		t.Error("expected app to be removed from cache after eviction")
	}
}

func TestActiveLeaseRenewed(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain a lease and keep it active (don't release — simulates in-flight request)
	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)

	if ll.Uses != 1 {
		t.Fatalf("expected Uses=1 after retain, got %d", ll.Uses)
	}

	// Run expireLeases — active lease should be renewed
	srv.expireLeases(ctx)

	aa.mu.Lock()
	defer aa.mu.Unlock()

	if aa.releaseCount != 0 {
		t.Errorf("expected 0 releases (lease is active), got %d", aa.releaseCount)
	}
	if aa.renewCount != 1 {
		t.Errorf("expected 1 renewal, got %d", aa.renewCount)
	}
}

func TestFailedRenewalEvictsLease(t *testing.T) {
	aa := &mockActivator{renewErr: errors.New("sandbox gone")}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain and release a lease (within TTL, so it would normally be renewed)
	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)
	srv.releaseLease(ctx, ll)

	// Run expireLeases — renewal fails, lease should be released and dropped
	srv.expireLeases(ctx)

	aa.mu.Lock()
	defer aa.mu.Unlock()

	// Verify ReleaseLease was called to clean up activator resources
	if aa.releaseCount != 1 {
		t.Errorf("expected 1 release on failed renewal, got %d", aa.releaseCount)
	}

	// Verify the app is removed from cache
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if _, ok := srv.apps["app/myapp"]; ok {
		t.Error("expected app to be removed after failed renewal")
	}
}

func TestUsagePreservedAfterRenewal(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain a lease — Uses starts at 1 (simulates in-flight request)
	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	srv.retainLease(ctx, "app/myapp", actLease)

	// Run expireLeases — should renew but NOT reset Uses, since an
	// in-flight request is still holding the lease. Resetting would
	// cause releaseLease to decrement below zero.
	srv.expireLeases(ctx)

	srv.mu.Lock()
	ar := srv.apps["app/myapp"]
	if ar == nil || len(ar.leases) == 0 {
		srv.mu.Unlock()
		t.Fatal("expected lease to still be cached")
	}
	uses := ar.leases[0].Uses
	srv.mu.Unlock()

	if uses != 1 {
		t.Errorf("expected Uses preserved at 1 after renewal, got %d", uses)
	}
}

func TestLastUsedUpdatedOnUse(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain a lease
	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)
	srv.releaseLease(ctx, ll)

	// Record the initial LastUsed
	srv.mu.Lock()
	initial := ll.LastUsed
	srv.mu.Unlock()

	// Small sleep to ensure time advances
	time.Sleep(time.Millisecond)

	// Use the lease again (simulates another request hitting the cache)
	got, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	switch {
	case got == nil:
		t.Fatal("expected to get a cached lease")
	case !got.LastUsed.After(initial):
		t.Errorf("expected LastUsed to advance on use, initial=%v updated=%v", initial, got.LastUsed)
	}
}

func TestMultipleAppsIndependentTTL(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// App A: retain and release, then backdate past TTL
	leaseA := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	llA := srv.retainLease(ctx, "app/a", leaseA)
	srv.releaseLease(ctx, llA)
	srv.mu.Lock()
	llA.LastUsed = time.Now().Add(-(minLeaseTTL + time.Minute))
	srv.mu.Unlock()

	// App B: retain and release, still within TTL
	leaseB := &activator.Lease{Size: 10, URL: "http://10.0.0.2:3000"}
	llB := srv.retainLease(ctx, "app/b", leaseB)
	srv.releaseLease(ctx, llB)

	// Run expireLeases
	srv.expireLeases(ctx)

	srv.mu.Lock()
	defer srv.mu.Unlock()

	// App A should be evicted
	if _, ok := srv.apps["app/a"]; ok {
		t.Error("expected app/a to be evicted (past TTL)")
	}

	// App B should be retained
	if _, ok := srv.apps["app/b"]; !ok {
		t.Error("expected app/b to still be cached (within TTL)")
	}
}

func TestInvalidateLeaseRemovesFromCache(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)
	srv.invalidateLease(ctx, "app/myapp", ll)

	aa.mu.Lock()
	releaseCount := aa.releaseCount
	aa.mu.Unlock()
	if releaseCount != 1 {
		t.Errorf("expected 1 release on invalidation, got %d", releaseCount)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if _, ok := srv.apps["app/myapp"]; ok {
		t.Error("expected app to be removed from cache after invalidation")
	}
}

func TestInvalidateAppLeasesRemovesAll(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	// Retain two leases for the same app (simulates multiple cached connections)
	lease1 := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	srv.retainLease(ctx, "app/myapp", lease1)
	lease2 := &activator.Lease{Size: 10, URL: "http://10.0.0.2:3000"}
	srv.retainLease(ctx, "app/myapp", lease2)

	srv.invalidateAppLeases(ctx, "app/myapp")

	aa.mu.Lock()
	releaseCount := aa.releaseCount
	releasedURLs := aa.releasedURLs
	aa.mu.Unlock()

	if releaseCount != 2 {
		t.Errorf("expected 2 releases on app invalidation, got %d", releaseCount)
	}
	if len(releasedURLs) != 2 {
		t.Errorf("expected 2 released URLs, got %v", releasedURLs)
	}

	// Cache should be fully cleared
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if _, ok := srv.apps["app/myapp"]; ok {
		t.Error("expected app to be removed from cache after invalidateAppLeases")
	}
}

func TestInvalidateAndReacquire(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	actLease := &activator.Lease{Size: 10, URL: "http://10.0.0.1:3000"}
	ll := srv.retainLease(ctx, "app/myapp", actLease)
	srv.invalidateLease(ctx, "app/myapp", ll)

	// After invalidation, cache is empty
	got, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil lease after invalidation, got non-nil")
	}

	// Retain a fresh lease (simulates the retry path acquiring a new one)
	freshLease := &activator.Lease{Size: 10, URL: "http://10.0.0.2:3000"}
	freshLL := srv.retainLease(ctx, "app/myapp", freshLease)

	got2, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got2 == nil {
		t.Fatal("expected to get fresh lease")
		return
	}
	if got2.Lease.URL != "http://10.0.0.2:3000" {
		t.Errorf("expected fresh lease URL, got %s", got2.Lease.URL)
	}

	srv.releaseLease(ctx, freshLL)
	srv.releaseLease(ctx, got2)
}

func TestSandboxDeathInvalidatesCachedLease(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	sandboxID := entity.Id("sandbox/sb-1234")

	// Cache a lease whose underlying sandbox is sb-1234
	actLease := activator.NewTestLease(
		&compute_v1alpha.Sandbox{ID: sandboxID, Status: compute_v1alpha.RUNNING},
		10, "http://10.0.0.1:3000",
	)
	srv.retainLease(ctx, "app/myapp", actLease)

	// Verify the lease is cached
	got, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected cached lease before sandbox death")
	}
	srv.releaseLease(ctx, got)

	// Simulate sandbox going STOPPED — invalidate its leases
	srv.invalidateSandboxLeases(ctx, sandboxID)

	// Lease should be evicted
	got, err = srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected lease to be evicted after sandbox death, but got non-nil")
	}

	// ReleaseLease should have been called on the activator
	aa.mu.Lock()
	defer aa.mu.Unlock()
	if aa.releaseCount != 1 {
		t.Errorf("expected 1 ReleaseLease call, got %d", aa.releaseCount)
	}
}

func TestSandboxDeathOnlyInvalidatesMatchingLeases(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	sandbox1 := entity.Id("sandbox/sb-1111")
	sandbox2 := entity.Id("sandbox/sb-2222")

	// Cache leases for two different sandboxes under the same app
	lease1 := activator.NewTestLease(
		&compute_v1alpha.Sandbox{ID: sandbox1, Status: compute_v1alpha.RUNNING},
		10, "http://10.0.0.1:3000",
	)
	srv.retainLease(ctx, "app/myapp", lease1)

	lease2 := activator.NewTestLease(
		&compute_v1alpha.Sandbox{ID: sandbox2, Status: compute_v1alpha.RUNNING},
		10, "http://10.0.0.2:3000",
	)
	srv.retainLease(ctx, "app/myapp", lease2)

	// Kill sandbox1 only
	srv.invalidateSandboxLeases(ctx, sandbox1)

	// sandbox2's lease should still be usable
	got, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected sandbox2 lease to survive")
		return
	}
	if got.Lease.URL != "http://10.0.0.2:3000" {
		t.Errorf("expected sandbox2 URL, got %s", got.Lease.URL)
	}
	srv.releaseLease(ctx, got)

	// Only 1 release call (for sandbox1)
	aa.mu.Lock()
	defer aa.mu.Unlock()
	if aa.releaseCount != 1 {
		t.Errorf("expected 1 ReleaseLease call, got %d", aa.releaseCount)
	}
}

func TestSandboxDeathAcrossMultipleApps(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	sharedSandbox := entity.Id("sandbox/sb-shared")
	otherSandbox := entity.Id("sandbox/sb-other")

	// Two apps happen to have leases pointing at the same sandbox
	lease1 := activator.NewTestLease(
		&compute_v1alpha.Sandbox{ID: sharedSandbox, Status: compute_v1alpha.RUNNING},
		10, "http://10.0.0.1:3000",
	)
	srv.retainLease(ctx, "app/alpha", lease1)

	lease2 := activator.NewTestLease(
		&compute_v1alpha.Sandbox{ID: sharedSandbox, Status: compute_v1alpha.RUNNING},
		10, "http://10.0.0.1:3000",
	)
	srv.retainLease(ctx, "app/beta", lease2)

	// A third app has a lease for a different sandbox
	lease3 := activator.NewTestLease(
		&compute_v1alpha.Sandbox{ID: otherSandbox, Status: compute_v1alpha.RUNNING},
		10, "http://10.0.0.2:3000",
	)
	srv.retainLease(ctx, "app/gamma", lease3)

	// Kill the shared sandbox
	srv.invalidateSandboxLeases(ctx, sharedSandbox)

	// alpha and beta should be evicted
	for _, appKey := range []string{"app/alpha", "app/beta"} {
		got, err := srv.useLease(ctx, appKey)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", appKey, err)
		}
		if got != nil {
			t.Errorf("expected %s lease to be evicted, but got %s", appKey, got.Lease.URL)
		}
	}

	// gamma should survive
	got, err := srv.useLease(ctx, "app/gamma")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected app/gamma lease to survive")
		return
	}
	if got.Lease.URL != "http://10.0.0.2:3000" {
		t.Errorf("expected gamma URL, got %s", got.Lease.URL)
	}
	srv.releaseLease(ctx, got)

	aa.mu.Lock()
	defer aa.mu.Unlock()
	if aa.releaseCount != 2 {
		t.Errorf("expected 2 ReleaseLease calls, got %d", aa.releaseCount)
	}
}

// Overflow leases are never reused and go back to the activator when their
// request finishes.
func TestOverflowLeaseReleasedAfterItsRequest(t *testing.T) {
	aa := &mockActivator{}
	srv := newTestServer(aa)
	ctx := context.Background()

	overflow := srv.retainLease(ctx, "app/myapp", &activator.Lease{Size: 2, URL: "http://10.0.0.2:3000", Overflow: true})
	normal := srv.retainLease(ctx, "app/myapp", &activator.Lease{Size: 2, URL: "http://10.0.0.1:3000"})
	srv.releaseLease(ctx, normal)

	got, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if got != normal {
		t.Fatalf("overflow lease must not be reused, got %v", got)
	}
	srv.releaseLease(ctx, got)

	srv.releaseLease(ctx, overflow)

	aa.mu.Lock()
	released := append([]string(nil), aa.releasedURLs...)
	aa.mu.Unlock()
	if len(released) != 1 || released[0] != "http://10.0.0.2:3000" {
		t.Fatalf("expected only the overflow lease released to the activator, got %v", released)
	}

	srv.mu.Lock()
	leases := srv.apps["app/myapp"].leases
	srv.mu.Unlock()
	if len(leases) != 1 || leases[0] != normal {
		t.Fatalf("expected only the normal lease left in the cache, got %d leases", len(leases))
	}
}

// A cached lease serves at most Size concurrent requests. The check used to be
// Uses <= Size, which let a lease sized for two requests carry three.
func TestUseLeaseStopsAtSize(t *testing.T) {
	srv := newTestServer(&mockActivator{})
	ctx := context.Background()

	ll := srv.retainLease(ctx, "app/myapp", &activator.Lease{Size: 2, URL: "http://10.0.0.1:3000"})

	second, err := srv.useLease(ctx, "app/myapp")
	if err != nil || second != ll {
		t.Fatalf("expected the cached lease for the second request, got %v, %v", second, err)
	}

	third, err := srv.useLease(ctx, "app/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if third != nil {
		t.Fatalf("expected no cached lease past Size, got one with Uses=%d", third.Uses)
	}
}

type failingWriter struct {
	header http.Header
}

func (f *failingWriter) Header() http.Header       { return f.header }
func (f *failingWriter) WriteHeader(int)           {}
func (f *failingWriter) Write([]byte) (int, error) { return 0, errors.New("client went away") }

// When the client hangs up mid-response, ReverseProxy panics with
// http.ErrAbortHandler. The lease's use must still be handed back, or the lease
// never drains to zero uses and never expires.
func TestProxyWithLeaseReleasesOnAbort(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 64<<10)))
	}))
	defer backend.Close()

	srv := newTestServer(&mockActivator{})
	srv.transport = http.DefaultTransport.(*http.Transport).Clone()
	ctx := context.Background()

	ll := srv.retainLease(ctx, "app/myapp", &activator.Lease{Size: 2, URL: backend.URL})

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), http.ServerContextKey, &http.Server{}))

	func() {
		defer func() {
			if r := recover(); r != http.ErrAbortHandler {
				t.Fatalf("expected the ErrAbortHandler panic to propagate, got %v", r)
			}
		}()
		srv.proxyWithLease(&failingWriter{header: http.Header{}}, req, ll, "app/myapp", "myapp", true, 0)
	}()

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if ll.Uses != 0 {
		t.Fatalf("expected the aborted request's use to be released, got Uses=%d", ll.Uses)
	}
}
