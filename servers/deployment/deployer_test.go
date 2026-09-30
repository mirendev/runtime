package deployment

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/rpc"
)

// A deploy begun under a cloud identity reports that person in history and as
// the lock holder, on the new fields and on the legacy ones older CLIs read.
func TestDeployerReportedInHistoryAndLock(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)

	deployCtx := rpc.ContextWithIdentity(ctx, &rpc.Identity{
		Subject: "usr-ada", Method: rpc.AuthMethodJWT,
		Metadata: map[string]any{"email": "ada@example.com", "name": "Ada Lovelace"},
	})
	tracker := deploylifecycle.NewTracker(slog.Default(), inmem.EAC)
	_, err := tracker.Begin(deployCtx, deploylifecycle.BeginParams{
		AppName: "web", ClusterID: "prod", Operation: deploylifecycle.OperationBuild,
	})
	require.NoError(t, err)

	list, err := client.ListDeployments(ctx, "web", "prod", "", 20)
	require.NoError(t, err)
	require.Len(t, list.Deployments(), 1)
	dep := list.Deployments()[0]
	assert.Equal(t, "usr-ada", dep.DeployedBySubject())
	assert.Equal(t, "jwt", dep.DeployedByAuthMethod())
	assert.Equal(t, "ada@example.com", dep.DeployedByEmail())
	assert.Equal(t, "Ada Lovelace", dep.DeployedByName())
	assert.Equal(t, "usr-ada", dep.DeployedByUserId())
	assert.Equal(t, "ada@example.com", dep.DeployedByUserEmail())
	assert.Equal(t, "Ada Lovelace", dep.DeployedByUserName())

	held, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	require.True(t, held.HasLockInfo() && held.LockInfo() != nil)
	assert.Equal(t, "Ada Lovelace", held.LockInfo().StartedBy())
}

// Without an identity there is nobody to name, and the lock holder says so
// rather than inventing one.
func TestAnonymousDeployerReportedAsUnknown(t *testing.T) {
	ctx := context.Background()
	client, inmem := newLockTestClient(t)
	beginLockTestDeployment(t, inmem, "web", "prod")

	list, err := client.ListDeployments(ctx, "web", "prod", "", 20)
	require.NoError(t, err)
	require.Len(t, list.Deployments(), 1)
	assert.Empty(t, list.Deployments()[0].DeployedByUserName())

	held, err := client.GetDeployLock(ctx, "web", "prod")
	require.NoError(t, err)
	assert.Equal(t, "-", held.LockInfo().StartedBy())
}
