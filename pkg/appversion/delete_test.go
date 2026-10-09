package appversion

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestDeleteRetainsVersionUntilFailedCloneIsCleanedUp(t *testing.T) {
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := t.Context()
	configID, err := server.Client.Create(ctx, "preview-config", &core_v1alpha.ConfigVersion{})
	require.NoError(t, err)
	version := &core_v1alpha.AppVersion{ConfigVersion: configID}
	version.ID, err = server.Client.Create(ctx, "preview", version)
	require.NoError(t, err)
	cloneID, err := server.Client.Create(ctx, "failed-clone", &addon_v1alpha.AddonAssociation{
		AppVersion: version.ID, Status: "error",
	})
	require.NoError(t, err)
	log := testutils.TestLogger(t)
	require.ErrorContains(t, DeleteWithPools(ctx, server.EAC, version, log), "retaining version")
	var clone addon_v1alpha.AddonAssociation
	require.NoError(t, server.Client.GetById(ctx, cloneID, &clone))
	require.Equal(t, "deprovisioning", clone.Status)
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, DeleteWithPoolsAndWait(waitCtx, server.EAC, version, log), context.DeadlineExceeded)
	_, err = server.EAC.Get(ctx, version.ID.String())
	require.NoError(t, err)
	_, err = server.EAC.Get(ctx, configID.String())
	require.NoError(t, err)
	require.ErrorContains(t, Delete(ctx, server.EAC, version, log), "retaining version")
	require.NoError(t, server.Client.Delete(ctx, cloneID))
	require.NoError(t, DeleteWithPools(ctx, server.EAC, version, log))
	_, err = server.EAC.Get(ctx, version.ID.String())
	require.Error(t, err)
	_, err = server.EAC.Get(ctx, configID.String())
	require.Error(t, err)
}
