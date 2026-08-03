package deployment

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// These tests pin the "exactly one owner per build" guarantee across CLI/server
// versions. The deployment server (which an older CLI drives through
// CreateDeployment) and the build server (which a newer CLI drives through the
// DeployRequest param) hold their locks in the same deploylifecycle namespace
// over the same entity store. A deploy driven by one must therefore contend
// with a deploy driven by the other — they cannot both hold the lock, so a
// client that somehow triggered both would produce one record, not two.
//
// The build server's ownership is modeled here by a bare deploylifecycle.Tracker
// over the same store, which is exactly what servers/build constructs; importing
// servers/build here would be circular.

func sharedStoreClientAndTracker(t *testing.T) (*deployment_v1alpha.DeploymentClient, *deploylifecycle.Tracker) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	require.NoError(t, err)

	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	buildTracker := deploylifecycle.NewTracker(log, inmem.EAC)

	return client, buildTracker
}

// Old CLI took the lock via CreateDeployment; a server-owned build for the same
// app+cluster must lose the lock rather than start a second deploy.
func TestCreateDeploymentBlocksServerOwnedBuild(t *testing.T) {
	ctx := context.Background()
	client, buildTracker := sharedStoreClientAndTracker(t)

	created, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)
	require.False(t, created.HasError() && created.Error() != "")

	_, err = buildTracker.Begin(ctx, deploylifecycle.BeginParams{AppName: "web", ClusterID: "prod"})
	require.ErrorIs(t, err, deploylifecycle.ErrLockHeld,
		"a server-owned build must not run alongside a CLI-driven deployment")

	// And exactly one in-progress record exists.
	records, err := buildTracker.Store().List(ctx, deploylifecycle.Query{
		AppName: "web", Status: deploylifecycle.StatusInProgress,
	})
	require.NoError(t, err)
	assert.Len(t, records, 1, "the blocked build must not have created a second record")
}

// Symmetric case: a server-owned build holds the lock; an old CLI's
// CreateDeployment for the same app+cluster is blocked.
func TestServerOwnedBuildBlocksCreateDeployment(t *testing.T) {
	ctx := context.Background()
	client, buildTracker := sharedStoreClientAndTracker(t)

	_, err := buildTracker.Begin(ctx, deploylifecycle.BeginParams{AppName: "web", ClusterID: "prod"})
	require.NoError(t, err)

	blocked, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err, "the block is a domain outcome, not an RPC error")
	require.True(t, blocked.HasError() && blocked.Error() != "",
		"CreateDeployment must be blocked by the server-owned build's lock")
	assert.Contains(t, blocked.Error(), "blocked")
	assert.True(t, blocked.HasLockInfo() && blocked.LockInfo() != nil)
}

// Once the CLI-driven deployment settles, the lock frees and a server-owned
// build proceeds — the two versions hand off cleanly.
func TestLockHandsOffBetweenVersions(t *testing.T) {
	ctx := context.Background()
	client, buildTracker := sharedStoreClientAndTracker(t)

	created, err := client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.NoError(t, err)

	// Old CLI settles its record (build failed).
	_, err = client.UpdateFailedDeployment(ctx, created.Deployment().Id(), "build broke", "")
	require.NoError(t, err)

	// New server-owned build now gets through.
	rec, err := buildTracker.Begin(ctx, deploylifecycle.BeginParams{AppName: "web", ClusterID: "prod"})
	require.NoError(t, err, "a settled CLI deployment must free the lock for the next build")
	assert.Equal(t, deploylifecycle.StatusInProgress, rec.Status())
}
