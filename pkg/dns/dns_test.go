package dns

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	compute_v1alpha "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/slogfmt"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Server{
		log:             log,
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}
}

func TestIPReuseBetweenSandboxes(t *testing.T) {
	// This test covers a race condition where:
	// 1. Sandbox A is created at IP X
	// 2. Sandbox A dies, but entity cleanup is delayed (1 hour)
	// 3. IP X is reused by new Sandbox B (after 30-min cooldown)
	// 4. Sandbox A's entity is finally deleted
	// 5. The IP should still be resolvable because Sandbox B is using it

	s := newTestServer(t)

	const (
		sharedIP     = "10.8.24.19"
		appName      = "testapp"
		serviceName  = "web"
		oldSandboxID = "sandbox/testapp-web-OLD"
		newSandboxID = "sandbox/testapp-web-NEW"
	)

	// Old sandbox (now STOPPED) was registered at IP
	s.AddSandboxMapping(oldSandboxID, sharedIP, appName, serviceName)

	// New sandbox (RUNNING) gets the same IP after cooldown
	s.AddSandboxMapping(newSandboxID, sharedIP, appName, serviceName)

	// Old sandbox entity is finally deleted (1 hour cleanup delay)
	s.handleSandboxDeleteByID(oldSandboxID)

	// The IP should still resolve because new sandbox is using it
	assert.Equal(t, appName, s.lookupAppForIP(sharedIP),
		"IP should still resolve after old sandbox deletion because new sandbox is using it")
}

func TestDeleteSandboxCleansUpWhenNoReuse(t *testing.T) {
	// When a sandbox is deleted and no other entity uses the IP,
	// the mappings should be cleaned up.

	s := newTestServer(t)

	const (
		ip        = "10.8.24.20"
		appName   = "testapp"
		service   = "web"
		sandboxID = "sandbox/testapp-web-123"
	)

	s.AddSandboxMapping(sandboxID, ip, appName, service)
	assert.Equal(t, appName, s.lookupAppForIP(ip))

	s.handleSandboxDeleteByID(sandboxID)

	assert.Empty(t, s.lookupAppForIP(ip),
		"IP should not resolve after sandbox deletion when no other sandbox uses it")
}

func TestDeleteSandboxWithDifferentIPs(t *testing.T) {
	// Deleting one sandbox should not affect another sandbox with a different IP.

	s := newTestServer(t)

	const (
		ip1        = "10.8.24.20"
		ip2        = "10.8.24.21"
		appName    = "testapp"
		service    = "web"
		sandbox1ID = "sandbox/testapp-web-1"
		sandbox2ID = "sandbox/testapp-web-2"
	)

	s.AddSandboxMapping(sandbox1ID, ip1, appName, service)
	s.AddSandboxMapping(sandbox2ID, ip2, appName, service)

	s.handleSandboxDeleteByID(sandbox1ID)

	assert.Empty(t, s.lookupAppForIP(ip1), "ip1 should not resolve after deletion")
	assert.Equal(t, appName, s.lookupAppForIP(ip2), "ip2 should still resolve")
}

func TestSandboxStatusTransitionRemovesFromDNS(t *testing.T) {
	// This test verifies that when a sandbox transitions from RUNNING to
	// STOPPED or DEAD, it is immediately removed from DNS mappings.
	// Previously, UPDATE events for non-RUNNING sandboxes were ignored,
	// causing stale IPs to remain in DNS until the entity DELETE (up to 1 hour).

	s := newTestServer(t)
	ctx := context.Background()

	const (
		ip        = "10.8.24.30"
		appName   = "testapp"
		service   = "web"
		sandboxID = "sandbox/testapp-web-status-test"
	)

	// Simulate a sandbox that was previously RUNNING and tracked
	s.AddSandboxMapping(sandboxID, ip, appName, service)
	assert.Equal(t, appName, s.lookupAppForIP(ip), "sandbox should be tracked initially")

	// Sandbox transitions to STOPPED (e.g., process exited)
	stoppedSandbox := &compute_v1alpha.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute_v1alpha.STOPPED,
	}
	s.handleSandboxUpdate(ctx, stoppedSandbox, nil)

	// The sandbox should be removed from DNS immediately
	assert.Empty(t, s.lookupAppForIP(ip),
		"sandbox should be removed from DNS when status changes to STOPPED")

	// Verify all mappings are cleaned up
	s.mu.RLock()
	_, hasEntityMapping := s.sandboxes[sandboxID]
	_, hasServiceMapping := s.ipToService[ip]
	s.mu.RUnlock()

	assert.False(t, hasEntityMapping, "the sandbox's claim should be removed")
	assert.False(t, hasServiceMapping, "ipToService mapping should be removed")
}

func TestSandboxDeadStatusRemovesFromDNS(t *testing.T) {
	// Same as above but for DEAD status

	s := newTestServer(t)
	ctx := context.Background()

	const (
		ip        = "10.8.24.31"
		appName   = "testapp"
		service   = "api"
		sandboxID = "sandbox/testapp-api-dead-test"
	)

	s.AddSandboxMapping(sandboxID, ip, appName, service)
	assert.Equal(t, appName, s.lookupAppForIP(ip))

	// Sandbox marked as DEAD (e.g., health check failed)
	deadSandbox := &compute_v1alpha.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute_v1alpha.DEAD,
	}
	s.handleSandboxUpdate(ctx, deadSandbox, nil)

	assert.Empty(t, s.lookupAppForIP(ip),
		"sandbox should be removed from DNS when status changes to DEAD")
}

func TestResolveUnknownIPWithoutEntityClient(t *testing.T) {
	// resolveUnknownIP should return false gracefully when entityClient is nil
	s := newTestServer(t)
	assert.False(t, s.resolveUnknownIP("10.8.24.50"),
		"resolveUnknownIP should return false when entityClient is nil")
}

func TestResolveUnknownIPFindsAndRegistersSandbox(t *testing.T) {
	// When a sandbox container makes DNS queries before the entity watcher
	// processes the RUNNING status update, resolveUnknownIP should find
	// the sandbox in the entity store and register it for DNS.

	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const (
		sandboxIP   = "10.8.24.60"
		appName     = "myapp"
		serviceName = "web"
		versionName = "myapp-v1"
		sandboxName = "myapp-web-1"
	)

	// Create app entity
	app := &core_v1alpha.App{}
	appID, err := inmem.Client.Create(ctx, appName, app)
	assert.NoError(t, err)

	// Create app version entity referencing the app
	appVer := &core_v1alpha.AppVersion{App: appID}
	_, err = inmem.Client.Create(ctx, versionName, appVer)
	assert.NoError(t, err)

	// Create sandbox entity in PENDING status with network info and service label.
	// The sandbox is PENDING (not RUNNING) to test that resolveUnknownIP
	// works regardless of status — the container is clearly running if it's
	// making DNS queries.
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Network: []compute_v1alpha.Network{
			{Address: sandboxIP + "/24"},
		},
		Spec: compute_v1alpha.SandboxSpec{
			Version: entity.Id("app_version/" + versionName),
		},
	}
	_, err = inmem.Client.Create(ctx, sandboxName, sb,
		entityserver.WithLabels(types.Labels{
			{Key: "service", Value: serviceName},
		}),
	)
	assert.NoError(t, err)

	// Create DNS server with the entity client
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := &Server{
		log:             log,
		entityClient:    inmem.EAC,
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}

	// IP should not be registered yet
	assert.Empty(t, s.lookupAppForIP(sandboxIP))

	// resolveUnknownIP should find the sandbox and register it
	assert.True(t, s.resolveUnknownIP(sandboxIP),
		"resolveUnknownIP should find and register the sandbox")

	// Verify the sandbox is now registered
	assert.Equal(t, appName, s.lookupAppForIP(sandboxIP),
		"sandbox should be registered with correct app name")

	// Verify service mapping
	s.mu.RLock()
	assert.Equal(t, serviceName, s.ipToService[sandboxIP],
		"service mapping should be registered")
	assert.Contains(t, s.appServiceToIPs[appName][serviceName], sandboxIP,
		"app+service to IP mapping should be registered")
	s.mu.RUnlock()
}

func TestResolveUnknownIPNoMatchingIP(t *testing.T) {
	// resolveUnknownIP should return false when no sandbox has the queried IP.

	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	// Create a sandbox with a different IP
	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Network: []compute_v1alpha.Network{
			{Address: "10.8.24.99/24"},
		},
	}
	_, err := inmem.Client.Create(ctx, "other-sandbox", sb,
		entityserver.WithLabels(types.Labels{
			{Key: "service", Value: "web"},
		}),
	)
	assert.NoError(t, err)

	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := &Server{
		log:             log,
		entityClient:    inmem.EAC,
		ipToApp:         make(map[string]string),
		ipToSandbox:     make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		sandboxes:       make(map[string]sandboxMapping),
	}

	assert.False(t, s.resolveUnknownIP("10.8.24.100"),
		"resolveUnknownIP should return false for non-matching IP")
}

func TestStoppedStatusNeverAdded(t *testing.T) {
	// Sandboxes that are STOPPED or DEAD should never be added to DNS,
	// even if they have network info.

	s := newTestServer(t)
	ctx := context.Background()

	const (
		ip        = "10.8.24.32"
		sandboxID = "sandbox/testapp-stopped"
	)

	// A STOPPED sandbox with network info should not be tracked
	stoppedSandbox := &compute_v1alpha.Sandbox{
		ID:     entity.Id(sandboxID),
		Status: compute_v1alpha.STOPPED,
		Network: []compute_v1alpha.Network{
			{Address: ip + "/24"},
		},
	}
	s.handleSandboxUpdate(ctx, stoppedSandbox, nil)

	assert.Empty(t, s.lookupAppForIP(ip),
		"STOPPED sandbox should not be added to DNS")
}
