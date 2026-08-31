package deployment

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// An older CLI calls CreateDeployment before starting its build. Once the
// server owns deployment attempts, accepting that call would create a second
// lifecycle owner that the build cannot safely join. Keep the RPC on the wire,
// but fail before publishing either a record or a lock.
func TestCreateDeploymentUpgradeErrorHasNoSideEffects(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	require.NoError(t, err)
	client := &deployment_v1alpha.DeploymentClient{
		Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server)),
	}

	_, err = client.CreateDeployment(ctx, "web", "prod", "pending-build", nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "run 'miren upgrade'"), err.Error())

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	tracker := deploylifecycle.NewTracker(log, inmem.EAC)
	records, err := tracker.Store().List(ctx, deploylifecycle.Query{
		AppName: "web", Status: deploylifecycle.StatusInProgress,
	})
	require.NoError(t, err)
	assert.Empty(t, records, "the rejected legacy call must not publish an attempt")

	_, err = tracker.Begin(ctx, deploylifecycle.BeginParams{AppName: "web", ClusterID: "prod"})
	require.NoError(t, err, "the rejected legacy call must not hold the deploy lock")
}
