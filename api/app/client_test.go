package app_test

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestCreatePreservesFirstCreator(t *testing.T) {
	inmem, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	_, err := inmem.Client.Create(t.Context(), "default", &core_v1alpha.Project{})
	require.NoError(t, err)
	client := app.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))

	const callers = 8
	start := make(chan struct{})
	var workers sync.WaitGroup
	results := make([]*core_v1alpha.App, callers)
	errs := make([]error, callers)
	for i := range callers {
		workers.Go(func() {
			ctx := rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: fmt.Sprintf("creator-%d", i), Method: rpc.AuthMethodJWT})
			<-start
			results[i], errs[i] = client.Create(ctx, "shared")
		})
	}
	close(start)
	workers.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	stored, err := client.GetByName(t.Context(), "shared")
	require.NoError(t, err)
	require.Regexp(t, `^creator-[0-7]$`, stored.CreatedBy)
	for _, result := range results {
		assert.Equal(t, stored.ID, result.ID)
		assert.Equal(t, stored.CreatedBy, result.CreatedBy)
	}
}

func TestCreateDoesNotClaimUnownedApps(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	client := app.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))
	_, err := inmem.Client.Create(t.Context(), "legacy", &core_v1alpha.App{WorkloadRole: "app-deployer"})
	require.NoError(t, err)
	for _, name := range []string{"legacy", "anonymous", "unattributed"} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			if name == "anonymous" {
				ctx = rpc.ContextWithIdentity(ctx, &rpc.Identity{Subject: "anonymous", Method: rpc.AuthMethodAnonymous})
			}
			_, err := client.Create(ctx, name)
			require.NoError(t, err)
			ctx = rpc.ContextWithIdentity(t.Context(), &rpc.Identity{Subject: "later-deployer", Method: rpc.AuthMethodJWT})
			record, err := client.Create(ctx, name)
			require.NoError(t, err)
			assert.Empty(t, record.CreatedBy)
			stored, err := client.GetByName(t.Context(), name)
			require.NoError(t, err)
			assert.Empty(t, stored.CreatedBy)
			if name == "legacy" {
				assert.Equal(t, "app-deployer", stored.WorkloadRole)
			}
		})
	}
}
