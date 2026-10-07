package postgresql

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/saga"
)

func TestCloneSagaGraphs(t *testing.T) {
	fw := &addon.ProviderFramework{}
	require.NoError(t, registerCloneSharedSaga(saga.NewRegistry(), fw))
	require.NoError(t, registerCloneDedicatedSaga(saga.NewRegistry(), fw))
	require.NoError(t, registerCloneDedicatedToSharedSaga(saga.NewRegistry(), fw))
}

func TestDedicatedCloneCredentialOrdering(t *testing.T) {
	registry := saga.NewRegistry()
	require.NoError(t, registerCloneDedicatedSaga(registry, &addon.ProviderFramework{}))
	def, ok := registry.Get("clone-dedicated-postgresql")
	require.True(t, ok)
	order := def.ExecutionOrder()
	for _, pair := range [][2]string{
		{"generate-dedicated-clone-name", "create-postgres-server"},
		{"generate-dedicated-clone-name", "create-dedicated-pool"},
		{"decode-dedicated-clone-source", "run-base-backup"},
		{"wait-for-dedicated-service", "rotate-clone-credentials"},
		{"wait-for-dedicated-pool", "rotate-clone-credentials"},
		{"rotate-clone-credentials", "update-dedicated-server"},
	} {
		first, second := slices.Index(order, pair[0]), slices.Index(order, pair[1])
		require.NotEqual(t, -1, first)
		require.Greater(t, second, first)
	}
}

func TestDedicatedClonePasswordCheckpoint(t *testing.T) {
	registry := saga.NewRegistry()
	require.NoError(t, saga.Define("clone-password").Action(generateDedicatedCloneName).
		Undo(func(context.Context, generateDedicatedCloneNameIn, generateDedicatedCloneNameOut) error { return nil }).RegisterTo(registry))
	storage := saga.NewMemoryStorage()
	execute := func() string {
		executor := saga.NewExecutor(storage, saga.WithRegistry(registry))
		require.NoError(t, executor.Start("clone-password").WithID("preview").
			Input("appname", "app").Input("targetassociationid", "preview").Execute(t.Context()))
		out, err := executor.ExecutionOutputs(t.Context(), "preview")
		require.NoError(t, err)
		var password string
		require.NoError(t, out.Get("password", &password))
		return password
	}
	password := execute()
	require.NotEmpty(t, password)
	require.Equal(t, password, execute(), "a new executor must recover the checkpointed password")
	other, err := generateDedicatedCloneName(t.Context(), generateDedicatedCloneNameIn{AppName: "app", TargetAssociationID: "other"})
	require.NoError(t, err)
	require.NotEqual(t, password, other.Password)
}

func TestRestoreCloneImage(t *testing.T) {
	require.Equal(t, BaseImage+":"+DefaultVersion, restoreCloneImage(nil))
	require.Equal(t, "postgres:custom", restoreCloneImage(map[string]string{addon.ConfigImage: "postgres:custom"}))
}

func TestLogicalCloneOrdering(t *testing.T) {
	for _, tc := range []struct {
		name, saga string
		register   func(*saga.Registry, *addon.ProviderFramework) error
		pairs      [][2]string
	}{
		{"shared to shared", "clone-shared-postgresql", registerCloneSharedSaga, [][2]string{
			{"create-clone-shared-user", "create-clone-shared-database"},
			{"decode-shared-clone-source", "restore-shared-clone"},
			{"create-clone-shared-database", "restore-shared-clone"},
			{"restore-shared-clone", "release-restore-sandbox"},
		}},
		{"dedicated to shared", "clone-dedicated-to-shared-postgresql", registerCloneDedicatedToSharedSaga, [][2]string{
			{"create-clone-shared-user", "create-clone-shared-database"},
			{"decode-dedicated-clone-source", "restore-shared-clone"},
			{"create-clone-shared-database", "restore-shared-clone"},
			{"restore-shared-clone", "release-restore-sandbox"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := saga.NewRegistry()
			require.NoError(t, tc.register(registry, &addon.ProviderFramework{}))
			def, ok := registry.Get(tc.saga)
			require.True(t, ok)
			order := def.ExecutionOrder()
			for _, pair := range tc.pairs {
				first, second := slices.Index(order, pair[0]), slices.Index(order, pair[1])
				require.NotEqual(t, -1, first)
				require.Greater(t, second, first)
			}
		})
	}
}

func TestLogicalCloneRetainedSandboxCleanup(t *testing.T) {
	for _, exitCode := range []int64{0, 1} {
		t.Run(map[int64]string{0: "success", 1: "failed restore"}[exitCode], func(t *testing.T) {
			server, cleanup := testutils.NewInMemEntityServer(t)
			defer cleanup()
			ctx := t.Context()
			id := entity.Id("sandbox/pg-clone-test-restore")
			_, err := server.EAC.Create(ctx, entity.New(entity.DBId, id, (&compute_v1alpha.Sandbox{
				Status: compute_v1alpha.STOPPED, Exit: compute_v1alpha.Exit{At: time.Now(), Code: exitCode},
			}).Encode).Attrs())
			require.NoError(t, err)
			storage := saga.NewMemoryStorage()
			fw := addon.NewProviderFramework(testutils.TestLogger(t), server.Client, server.EAC, storage)
			registry := saga.NewRegistry()
			require.NoError(t, saga.Define("restore-test").Using(fw).
				Action(restoreSharedClone).Undo(undoRestoreSharedClone).
				Action(releaseRestoreSandbox).Undo(undoReleaseRestoreSandbox).RegisterTo(registry))
			executor := saga.NewExecutor(storage, saga.WithRegistry(registry))
			start := executor.Start("restore-test").WithID("restore-test")
			for key, value := range map[string]any{
				"sourcehost": "source", "databasename": "source-db", "username": "primary-user", "source_password": "source-password",
				"servicehost": "target", "sharedusername": "preview", "sharedpassword": "preview-password",
				"shareddatabasename": "preview-db", "variantconfig": map[string]string{addon.ConfigImage: "postgres:18"},
				"targetassociationid": "addon_association/clone-test", "appname": "test", "database_created": true,
			} {
				start.Input(key, value)
			}
			err = start.Execute(ctx)
			if exitCode == 0 {
				require.NoError(t, err)
				require.NoError(t, start.Execute(ctx), "completed saga must not recreate the deleted restore sandbox")
			} else {
				require.ErrorContains(t, err, "exited with code 1")
			}
			_, err = server.EAC.Get(ctx, id.String())
			require.Error(t, err, "restore sandbox must be cleaned up on success and failure")
		})
	}
}

func TestGenerateDedicatedCloneName(t *testing.T) {
	first, err := generateDedicatedCloneName(context.Background(), generateDedicatedCloneNameIn{
		AppName: "my-app", TargetAssociationID: "addon_association/clone-0123456789abcdef",
	})
	require.NoError(t, err)
	require.Equal(t, "pg-my-app-456789abcdef", first.ServerName)
	require.Equal(t, "my-app-postgresql-456789abcdef", first.ServiceName)

	second, err := generateDedicatedCloneName(context.Background(), generateDedicatedCloneNameIn{
		AppName: "my-app", TargetAssociationID: "addon_association/clone-fedcba9876543210",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.ServerName, second.ServerName)
	require.NotEqual(t, first.ServiceName, second.ServiceName)
}

func TestFailedBaseBackupCleansResourcesWithoutActionOutputs(t *testing.T) {
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	ctx := t.Context()
	fw := addon.NewProviderFramework(testutils.TestLogger(t), server.Client, server.EAC, saga.NewMemoryStorage())
	registry := saga.NewRegistry()
	var sandboxID, diskID entity.Id
	require.NoError(t, saga.Define("failed-backup").Using(fw).
		Action(generateDedicatedCloneName).Undo(undoGenerateDedicatedCloneName).
		Action("failed-backup", func(ctx context.Context, in struct{ ServerName string }) (struct{}, error) {
			var err error
			sandboxID, err = server.Client.Create(ctx, in.ServerName+"-basebackup", &compute_v1alpha.Sandbox{})
			if err != nil {
				return struct{}{}, err
			}
			diskID, err = server.Client.Create(ctx, "target-disk", &storage_v1alpha.Disk{Name: "pg-" + in.ServerName + "-data"})
			if err != nil {
				return struct{}{}, err
			}
			return struct{}{}, errors.New("backup failed before checkpoint")
		}).Undo(func(context.Context, struct{ ServerName string }, struct{}) error { return nil }).RegisterTo(registry))
	executor := saga.NewExecutor(fw.Storage, saga.WithRegistry(registry))
	require.ErrorContains(t, executor.Start("failed-backup").WithID("failed-backup").
		Input("appname", "source").Input("targetassociationid", "addon_association/clone-123456789012").Execute(ctx), "backup failed before checkpoint")
	_, err := server.EAC.Get(ctx, sandboxID.String())
	require.Error(t, err, "cleanup must not require failed action outputs")
	_, err = server.EAC.Get(ctx, diskID.String())
	require.Error(t, err)
}
