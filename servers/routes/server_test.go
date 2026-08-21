package routes

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

const testHost = "app.example.com"

func newTestServer(t *testing.T) (*ingress_v1alpha.RoutesClient, *ingress.Client) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	entities := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
	ic := ingress.NewClient(slog.Default(), entities)

	_, err := ic.SetRoute(context.Background(), testHost, entity.Id("app/web"))
	require.NoError(t, err)

	srv := NewServer(slog.Default(), ic)

	return ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(srv))), ic
}

// operatorContext stands in for a human operator on the cert plane, which is
// the only caller shape allowed to reach these methods.
func operatorContext(subject string) context.Context {
	return rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject: subject,
		Method:  rpc.AuthMethodCert,
	})
}

func TestSetMaintenanceStampsTheAuthenticatedOperator(t *testing.T) {
	client, ic := newTestServer(t)

	ctx := operatorContext("evan@miren.dev")

	res, err := client.SetMaintenance(ctx, testHost, false, "Upgrading the database", "")
	require.NoError(t, err)

	assert.Equal(t, testHost, res.Route())
	assert.Equal(t, "Upgrading the database", res.Reason())
	assert.Equal(t, "evan@miren.dev", res.StartedBy(),
		"started_by must come from the authenticated identity, not from anything the caller sent")
	assert.NotEmpty(t, res.StartedAt())

	route, err := ic.Lookup(ctx, testHost)
	require.NoError(t, err)
	assert.Equal(t, "evan@miren.dev", route.Maintenance.StartedBy)
	assert.Equal(t, "Upgrading the database", route.Maintenance.Reason)
}

func TestSetMaintenanceKeepsTheOriginalStartOnRepeat(t *testing.T) {
	client, _ := newTestServer(t)

	first, err := client.SetMaintenance(operatorContext("evan@miren.dev"), testHost, false, "Migrating", "")
	require.NoError(t, err)

	// Revising the reason mid-window must not restart the clock, and must not
	// reassign the window to whoever happened to run the second command.
	// Derived rather than hardcoded: back_at has to be in the future, so a
	// fixed date would turn this into a test that starts failing on its own.
	backAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)

	second, err := client.SetMaintenance(operatorContext("someone-else@miren.dev"),
		testHost, false, "Migrating, taking longer than expected", backAt)
	require.NoError(t, err)

	assert.Equal(t, first.StartedAt(), second.StartedAt())
	assert.Equal(t, "evan@miren.dev", second.StartedBy())
	assert.Equal(t, "Migrating, taking longer than expected", second.Reason())
	assert.Equal(t, backAt, second.BackAt())
}

// TestSetMaintenanceKeepsTheFirstOpenerUnderInterleaving covers two operators
// opening a window at the same moment.
//
// Both read the route while it is still serving, so both believe they are the
// first and both carry a fresh stamp. One write lands; the other must notice
// the route is now in maintenance and keep the first operator's stamp rather
// than its own. The stale route value below is exactly what the losing caller
// holds: a snapshot taken before the other write landed.
func TestSetMaintenanceKeepsTheFirstOpenerUnderInterleaving(t *testing.T) {
	_, ic := newTestServer(t)

	ctx := context.Background()

	stale, err := ic.Lookup(ctx, testHost)
	require.NoError(t, err)
	require.True(t, stale.Maintenance.Empty(), "both callers start from a serving route")

	_, err = ic.SetRouteMaintenance(ctx, stale,
		"first operator's window", "", "2026-08-12T10:00:00Z", "evan@miren.dev")
	require.NoError(t, err)

	// The second caller writes from its pre-existing snapshot, still showing an
	// empty component, and proposes its own stamp.
	second, err := ic.SetRouteMaintenance(ctx, stale,
		"second operator's window", "", "2026-08-12T10:00:05Z", "someone-else@miren.dev")
	require.NoError(t, err)

	assert.Equal(t, "evan@miren.dev", second.Maintenance.StartedBy,
		"the window belongs to whoever opened it, not to whoever wrote last")
	assert.Equal(t, "2026-08-12T10:00:00Z", second.Maintenance.StartedAt,
		"the clock started when the first write landed")

	// The reason is not part of the preservation: a later call is how an
	// operator revises it, so the newest one wins.
	assert.Equal(t, "second operator's window", second.Maintenance.Reason)

	stored, err := ic.Lookup(ctx, testHost)
	require.NoError(t, err)
	assert.Equal(t, "evan@miren.dev", stored.Maintenance.StartedBy)
	assert.Equal(t, "2026-08-12T10:00:00Z", stored.Maintenance.StartedAt)
}

func TestSetMaintenanceStampsAfreshAfterClearing(t *testing.T) {
	client, _ := newTestServer(t)

	_, err := client.SetMaintenance(operatorContext("evan@miren.dev"), testHost, false, "First window", "")
	require.NoError(t, err)

	_, err = client.ClearMaintenance(operatorContext("evan@miren.dev"), testHost, false)
	require.NoError(t, err)

	second, err := client.SetMaintenance(operatorContext("someone-else@miren.dev"), testHost, false, "Second window", "")
	require.NoError(t, err)

	// Deliberately not asserting the timestamp moved: RFC 3339 is second
	// resolution and both windows open in the same second here, so that
	// comparison would be a coin flip. The identity is the part that carries
	// the meaning, and it only changes on a genuinely new window.
	assert.Equal(t, "someone-else@miren.dev", second.StartedBy(),
		"a new window belongs to whoever opened it")
}

func TestClearMaintenanceIsIdempotent(t *testing.T) {
	client, ic := newTestServer(t)

	ctx := operatorContext("evan@miren.dev")

	_, err := client.SetMaintenance(ctx, testHost, false, "Migrating", "")
	require.NoError(t, err)

	first, err := client.ClearMaintenance(ctx, testHost, false)
	require.NoError(t, err)
	assert.True(t, first.Changed())

	second, err := client.ClearMaintenance(ctx, testHost, false)
	require.NoError(t, err)
	assert.False(t, second.Changed(), "clearing an already-serving route reports no change rather than failing")

	route, err := ic.Lookup(ctx, testHost)
	require.NoError(t, err)
	assert.True(t, route.Maintenance.Empty(),
		"clearing drops the whole record, so a stale reason can't linger on a serving route")
}

func TestMaintenanceRejectsAppScopedCallers(t *testing.T) {
	client, _ := newTestServer(t)

	// A workload token is bound to one app. Maintenance takes a hostname out of
	// service for everyone, so an app must not be able to reach it.
	ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject:  "org:o:app:web:sandbox:sb-1",
		Method:   rpc.AuthMethodWorkload,
		Metadata: map[string]any{"app": "web"},
	})

	_, err := client.SetMaintenance(ctx, testHost, false, "Migrating", "")
	assert.Error(t, err)

	_, err = client.ClearMaintenance(ctx, testHost, false)
	assert.Error(t, err)
}

func TestMaintenanceReportsUnknownRoutes(t *testing.T) {
	client, _ := newTestServer(t)

	ctx := operatorContext("evan@miren.dev")

	_, err := client.SetMaintenance(ctx, "nope.example.com", false, "Migrating", "")
	assert.ErrorContains(t, err, "nope.example.com")

	_, err = client.SetMaintenance(ctx, "", true, "Cluster upgrade", "")
	assert.ErrorContains(t, err, "no default route configured")
}

func TestSetMaintenanceRejectsUnusableReturnTimes(t *testing.T) {
	client, ic := newTestServer(t)

	ctx := operatorContext("evan@miren.dev")

	_, err := client.SetMaintenance(ctx, testHost, false, "Migrating", "2020-01-01T00:00:00Z")
	assert.ErrorContains(t, err, "must be in the future")

	_, err = client.SetMaintenance(ctx, testHost, false, "Migrating", "half past three")
	assert.ErrorContains(t, err, "RFC 3339")

	// A rejected call must not have opened a window on its way to failing.
	route, err := ic.Lookup(ctx, testHost)
	require.NoError(t, err)
	assert.True(t, route.Maintenance.Empty())
}

func TestSetMaintenanceNormalizesTheReturnTime(t *testing.T) {
	client, _ := newTestServer(t)

	// An offset the caller happened to send, rather than UTC. Storing it as
	// given would leave route show and the holding page rendering the same
	// instant differently depending on who set it. Both sides are derived from
	// the same future instant so this cannot expire.
	future := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	sent := future.In(time.FixedZone("PDT", -7*60*60)).Format(time.RFC3339)

	res, err := client.SetMaintenance(operatorContext("evan@miren.dev"),
		testHost, false, "Migrating", sent)
	require.NoError(t, err)

	assert.NotEqual(t, sent, res.BackAt(), "the offset form should not be what gets stored")
	assert.Equal(t, future.UTC().Format(time.RFC3339), res.BackAt())
}

func TestValidateBackAt(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 30, 0, 0, time.UTC)

	got, err := validateBackAt("", now)
	require.NoError(t, err, "no estimate is a legitimate window")
	assert.Empty(t, got)

	_, err = validateBackAt(now.Format(time.RFC3339), now)
	assert.Error(t, err, "a return time of exactly now is not in the future")
}
