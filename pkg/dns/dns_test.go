package dns

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/slogfmt"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Server{
		log:             log,
		ipToApp:         make(map[string]string),
		ipToService:     make(map[string]string),
		appServiceToIPs: make(map[string]map[string][]string),
		entityToIP:      make(map[string]string),
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
	s.addSandboxMapping(oldSandboxID, sharedIP, appName, serviceName)

	// New sandbox (RUNNING) gets the same IP after cooldown
	s.addSandboxMapping(newSandboxID, sharedIP, appName, serviceName)

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

	s.addSandboxMapping(sandboxID, ip, appName, service)
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

	s.addSandboxMapping(sandbox1ID, ip1, appName, service)
	s.addSandboxMapping(sandbox2ID, ip2, appName, service)

	s.handleSandboxDeleteByID(sandbox1ID)

	assert.Empty(t, s.lookupAppForIP(ip1), "ip1 should not resolve after deletion")
	assert.Equal(t, appName, s.lookupAppForIP(ip2), "ip2 should still resolve")
}
