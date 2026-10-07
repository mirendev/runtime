package postgresql

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/dbsaga"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/saga"
)

// A parent re-running find-or-create-shared-server while its own
// ensure-shared-server child is partway through must resume that child, not
// mistake the server it is building for someone else's and back off (young
// server) or tear it down (stale server).
func TestFindOrCreateSharedServer_ResumesOwnInFlightChild(t *testing.T) {
	for name, age := range map[string]time.Duration{
		"young server": time.Minute,
		"stale server": time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			es, cleanup := testutils.NewInMemEntityServer(t)
			defer cleanup()

			storage := saga.NewMemoryStorage()
			fw := addon.NewProviderFramework(testutils.TestLogger(t), es.Client, es.EAC, storage)

			registry := saga.NewRegistry()
			require.NoError(t, RegisterEnsureSharedServerSaga(registry, fw))
			require.NoError(t, saga.Define("find-or-create-parent").
				Using(fw).
				Action(FindOrCreateSharedServer).Undo(UndoFindOrCreateSharedServer).
				RegisterTo(registry))

			// The child got as far as waiting on the service before the
			// parent was interrupted, so the server is still provisioning.
			const childPassword = "child-pw"
			const childDisk = "pg-shared-data-child"
			serverID, err := fw.EC.Create(ctx, sharedServerName, &addon_v1alpha.PostgresServer{
				AddonName:         AddonName,
				Variant:           "shared",
				Status:            "provisioning",
				SuperuserPassword: childPassword,
				DiskName:          childDisk,
			})
			require.NoError(t, err)
			es.GetEntity(serverID).SetCreatedAt(time.Now().Add(-age))

			const parentID = "parent-1"
			def, ok := registry.Get("ensure-shared-server")
			require.True(t, ok)
			output := func(v any) *saga.ActionResult {
				b, err := json.Marshal(v)
				require.NoError(t, err)
				return &saga.ActionResult{Output: b, ExecutedAt: time.Now()}
			}
			child := &saga.Execution{
				ID:                saga.NestedExecutionID(parentID, "ensure-shared-server", "find-or-create-shared-server"),
				DefinitionName:    "ensure-shared-server",
				DefinitionVersion: def.Version,
				ParentExecutionID: parentID,
				InitialInputs: map[string]any{
					"superuserpassword": childPassword,
					"diskname":          childDisk,
					"variantconfig":     map[string]string{},
				},
				Status: saga.StatusRunning,
				ExecutedActions: map[string]*saga.ActionResult{
					"create-shared-server-entity": output(CreateSharedServerEntityOut{ServerID: serverID}),
					"create-shared-pool":          output(CreateSharedPoolOut{PoolID: "pool/child"}),
					"wait-for-shared-pool":        output(dbsaga.WaitForSharedPoolOut{PoolReady: true}),
					"create-shared-service":       output(dbsaga.CreateSharedServiceOut{ServiceID: "service/child"}),
					"wait-for-shared-service":     output(dbsaga.WaitForSharedServiceOut{ServiceHost: "10.0.0.5"}),
				},
				ExecutionOrder: []string{
					"create-shared-server-entity",
					"create-shared-pool",
					"wait-for-shared-pool",
					"create-shared-service",
					"wait-for-shared-service",
				},
			}
			require.NoError(t, storage.Save(ctx, child))

			executor := saga.NewExecutor(storage, saga.WithRegistry(registry))
			require.NoError(t, executor.Start("find-or-create-parent").
				Input("appname", "app").
				Input("variantconfig", map[string]string{}).
				WithID(parentID).
				Execute(ctx))

			got, err := storage.Get(ctx, child.ID)
			require.NoError(t, err)
			assert.Equal(t, saga.StatusCompleted, got.Status)

			var server addon_v1alpha.PostgresServer
			require.NoError(t, fw.EC.GetById(ctx, serverID, &server), "the child's server must survive")
			assert.Equal(t, "active", server.Status)

			out, err := executor.ExecutionOutputs(ctx, parentID)
			require.NoError(t, err)
			var gotID entity.Id
			require.NoError(t, out.Get("serverid", &gotID))
			assert.Equal(t, serverID, gotID)
			var gotPassword string
			require.NoError(t, out.Get("superuserpassword", &gotPassword))
			assert.Equal(t, childPassword, gotPassword, "the password must be the one the server was built with")
			var gotHost string
			require.NoError(t, out.Get("servicehost", &gotHost))
			assert.Equal(t, "10.0.0.5", gotHost)
		})
	}
}
