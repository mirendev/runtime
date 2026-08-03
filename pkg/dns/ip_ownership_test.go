package dns

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute_v1alpha "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/slogfmt"
)

// These tests cover MIR-1511: a sandbox that lands on a recently-recycled IP could
// never obtain a workload identity token, because ipToSandbox kept naming the IP's
// previous owner and nothing in this package ever re-pointed it. The token server
// resolves a caller's identity from that map, so a wrong entry is not just a stale
// DNS answer — it is an identity the caller can never satisfy.
//
// Each test drives the map through one ordering that produced the stale entry.

// ownerOf is the question the token server asks: who owns this IP right now?
func ownerOf(t *testing.T, s *Server, ip string) (sandboxID, appName string) {
	t.Helper()
	sandboxID, appName, ok := s.LookupSandboxByIP(ip)
	require.True(t, ok, "IP %s should resolve to a sandbox", ip)
	return sandboxID, appName
}

// TestDeleteRepointsIPToSurvivingSandbox covers the delete path: when a sandbox is
// deleted but another entity still claims its IP, the mapping must be re-pointed at
// the survivor. Previously handleSandboxDeleteByID returned early in that branch
// without touching ipToSandbox, so the map kept naming the deleted sandbox forever.
func TestDeleteRepointsIPToSurvivingSandbox(t *testing.T) {
	s := newTestServer(t)

	const (
		ip     = "10.8.64.17"
		liveID = "sandbox/reviewagent-web-LIVE"
		goneID = "sandbox/db-app-web-GONE"
	)

	// The new sandbox claims the recycled IP, then a late event for the outgoing
	// sandbox re-registers it on the same IP — leaving the map naming the old one.
	s.AddSandboxMapping(liveID, ip, "reviewagent", "web")
	s.AddSandboxMapping(goneID, ip, "db-app", "web")

	s.handleSandboxDeleteByID(goneID)

	sandboxID, appName := ownerOf(t, s, ip)
	assert.Equal(t, liveID, sandboxID,
		"IP should be re-pointed at the surviving sandbox, not left naming the deleted one")
	assert.Equal(t, "reviewagent", appName,
		"app name must follow the surviving sandbox — it becomes the issued token's identity")

	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.Equal(t, "web", s.ipToService[ip], "service mapping should follow the survivor")
	assert.Contains(t, s.appServiceToIPs["reviewagent"]["web"], ip,
		"survivor's app should still advertise the IP")
	assert.NotContains(t, s.appServiceToIPs["db-app"]["web"], ip,
		"deleted sandbox's app should no longer advertise the IP")
}

// TestReassignedIPDropsPreviousOwnersServiceRecord covers the add path's other half:
// when an IP moves to a different app, the previous owner's service record has to be
// withdrawn. Otherwise db-app's web.app.miren keeps answering with an address that now
// belongs to reviewagent.
func TestReassignedIPDropsPreviousOwnersServiceRecord(t *testing.T) {
	s := newTestServer(t)

	const ip = "10.8.64.17"

	s.AddSandboxMapping("sandbox/db-app-web-OLD", ip, "db-app", "web")
	s.AddSandboxMapping("sandbox/reviewagent-web-NEW", ip, "reviewagent", "web")

	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.Contains(t, s.appServiceToIPs["reviewagent"]["web"], ip)
	assert.NotContains(t, s.appServiceToIPs["db-app"]["web"], ip,
		"an IP that moved to another app must stop being advertised by the previous app")
}

// TestTrackedSandboxReassertsStolenIP covers the `tracked` early return. A live
// sandbox that is already tracked never re-asserted ownership of its own IP, so once
// another registration overwrote ipToSandbox there was no writer left that could fix
// it — the map stayed wrong until the runner restarted.
func TestTrackedSandboxReassertsStolenIP(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const ip = "10.8.64.17"

	s := newEntityBackedServer(t, inmem)
	sb, en := createSandbox(t, inmem, sandboxFixture{
		app:     "reviewagent",
		version: "reviewagent-v1",
		name:    "reviewagent-web-live",
		service: "web",
		ip:      ip,
		status:  compute_v1alpha.RUNNING,
	})

	// The live sandbox is registered and tracked.
	s.handleSandboxUpdate(ctx, sb, en)
	sandboxID, _ := ownerOf(t, s, ip)
	require.Equal(t, sb.ID.String(), sandboxID, "live sandbox should own its IP to begin with")

	// A stale registration for the IP's previous owner steals the mapping.
	s.AddSandboxMapping("sandbox/db-app-web-STALE", ip, "db-app", "web")

	// Any subsequent event for the live sandbox must reclaim it.
	s.handleSandboxUpdate(ctx, sb, en)

	sandboxID, appName := ownerOf(t, s, ip)
	assert.Equal(t, sb.ID.String(), sandboxID,
		"a live sandbox must reclaim its own IP even though it is already tracked")
	assert.Equal(t, "reviewagent", appName)
}

// TestTrackedSandboxFollowsIPChange pins the coherence between the per-sandbox record
// and the IP-keyed maps: if a tracked sandbox's address changes, the old IP must stop
// resolving to it.
func TestTrackedSandboxFollowsIPChange(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const (
		oldIP = "10.8.64.17"
		newIP = "10.8.64.18"
	)

	s := newEntityBackedServer(t, inmem)
	sb, en := createSandbox(t, inmem, sandboxFixture{
		app:     "reviewagent",
		version: "reviewagent-v1",
		name:    "reviewagent-web-moving",
		service: "web",
		ip:      newIP,
		status:  compute_v1alpha.RUNNING,
	})

	// The sandbox was tracked at its previous address.
	s.AddSandboxMapping(sb.ID.String(), oldIP, "reviewagent", "web")

	s.handleSandboxUpdate(ctx, sb, en)

	sandboxID, _ := ownerOf(t, s, newIP)
	assert.Equal(t, sb.ID.String(), sandboxID, "sandbox should be reachable at its current address")

	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.NotContains(t, s.ipToSandbox, oldIP, "the sandbox's previous address should be released")
	assert.Equal(t, newIP, s.sandboxes[sb.ID.String()].ip)
}

// TestResolveUnknownIPPrefersLiveSandbox covers the lazy fallback. It scanned every
// sandbox entity and took the first address match regardless of status, so a dead
// sandbox whose entity had not been garbage collected yet could be installed as the
// owner of an IP a live sandbox was already using.
func TestResolveUnknownIPPrefersLiveSandbox(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const ip = "10.8.64.17"

	// Both entities exist at once, which is the normal state for a recycled IP: the
	// outgoing sandbox keeps its entity, address and all, well after its container is
	// gone. Which one a List hands back first is not defined, so before the fix the
	// answer here depended on map iteration order — the point is that it no longer can.
	createSandbox(t, inmem, sandboxFixture{
		app:     "db-app",
		version: "db-app-v1",
		name:    "db-app-web-dead",
		service: "web",
		ip:      ip,
		status:  compute_v1alpha.DEAD,
	})
	live, _ := createSandbox(t, inmem, sandboxFixture{
		app:     "reviewagent",
		version: "reviewagent-v1",
		name:    "reviewagent-web-live",
		service: "web",
		ip:      ip,
		status:  compute_v1alpha.RUNNING,
	})

	s := newEntityBackedServer(t, inmem)

	require.True(t, s.resolveUnknownIP(ip))

	sandboxID, appName := ownerOf(t, s, ip)
	assert.Equal(t, live.ID.String(), sandboxID,
		"a dead sandbox must never outrank a live one for the same IP")
	assert.Equal(t, "reviewagent", appName)
}

// TestRefreshSandboxByIPCorrectsStaleMapping covers the escape hatch the token server
// uses when a caller's secret contradicts the identity an address resolved to: the
// mapping is re-derived from the entity store and replaced.
func TestRefreshSandboxByIPCorrectsStaleMapping(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const ip = "10.8.64.17"

	live, _ := createSandbox(t, inmem, sandboxFixture{
		app:     "reviewagent",
		version: "reviewagent-v1",
		name:    "reviewagent-web-live",
		service: "web",
		ip:      ip,
		status:  compute_v1alpha.RUNNING,
	})

	s := newEntityBackedServer(t, inmem)
	s.AddSandboxMapping("sandbox/db-app-web-STALE", ip, "db-app", "web")

	sandboxID, appName, ok := s.RefreshSandboxByIP(ip)
	require.True(t, ok)
	assert.Equal(t, live.ID.String(), sandboxID, "refresh should return the address's real owner")
	assert.Equal(t, "reviewagent", appName)

	cached, _ := ownerOf(t, s, ip)
	assert.Equal(t, live.ID.String(), cached, "and should leave the corrected mapping behind")
}

// TestRefreshSandboxByIPKeepsMappingWhenStoreCannotAnswer pins the non-destructive half:
// a refresh that finds nothing must leave the existing mapping alone rather than blanking
// out an address because one request failed to authenticate.
func TestRefreshSandboxByIPKeepsMappingWhenStoreCannotAnswer(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const ip = "10.8.64.17"

	s := newEntityBackedServer(t, inmem)
	s.AddSandboxMapping("sandbox/reviewagent-web-LIVE", ip, "reviewagent", "web")

	_, _, ok := s.RefreshSandboxByIP(ip)
	assert.False(t, ok, "the store has no sandbox at this address")

	sandboxID, _ := ownerOf(t, s, ip)
	assert.Equal(t, "sandbox/reviewagent-web-LIVE", sandboxID, "the existing mapping should survive")
}

// TestResolveUnknownIPSkipsDeadSandbox is the same rule with no live claimant: an
// address that only a dead sandbox holds should not resolve at all. Registering it
// would hand the next sandbox on that IP an identity it can never authenticate as.
func TestResolveUnknownIPSkipsDeadSandbox(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	const ip = "10.8.64.17"

	createSandbox(t, inmem, sandboxFixture{
		app:     "db-app",
		version: "db-app-v1",
		name:    "db-app-web-dead",
		service: "web",
		ip:      ip,
		status:  compute_v1alpha.DEAD,
	})

	s := newEntityBackedServer(t, inmem)

	assert.False(t, s.resolveUnknownIP(ip),
		"an IP held only by a dead sandbox should not be registered")
}

// --- fixtures ---

type sandboxFixture struct {
	app     string
	version string
	name    string
	service string
	ip      string
	status  compute_v1alpha.SandboxStatus
}

func newEntityBackedServer(t *testing.T, inmem *testutils.InMemEntityServer) *Server {
	t.Helper()
	s := newTestServer(t)
	s.log = slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s.entityClient = inmem.EAC
	return s
}

// createSandbox builds the app → version → sandbox chain handleSandboxUpdate walks to
// derive an app name, and returns the decoded sandbox plus its entity.
func createSandbox(t *testing.T, inmem *testutils.InMemEntityServer, f sandboxFixture) (*compute_v1alpha.Sandbox, *entity.Entity) {
	t.Helper()
	ctx := context.Background()

	versionID := entity.Id("app_version/" + f.version)
	if _, err := inmem.EAC.Get(ctx, versionID.String()); err != nil {
		appID, err := inmem.Client.Create(ctx, f.app, &core_v1alpha.App{})
		require.NoError(t, err)
		_, err = inmem.Client.Create(ctx, f.version, &core_v1alpha.AppVersion{App: appID})
		require.NoError(t, err)
	}

	id, err := inmem.Client.Create(ctx, f.name, &compute_v1alpha.Sandbox{
		Status:  f.status,
		Network: []compute_v1alpha.Network{{Address: f.ip + "/24"}},
		Spec:    compute_v1alpha.SandboxSpec{Version: versionID},
	}, entityserver.WithLabels(types.Labels{
		{Key: "service", Value: f.service},
	}))
	require.NoError(t, err)

	resp, err := inmem.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	en := resp.Entity().Entity()

	var sb compute_v1alpha.Sandbox
	sb.Decode(en)
	return &sb, en
}
