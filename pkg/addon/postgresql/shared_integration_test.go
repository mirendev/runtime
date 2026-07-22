package postgresql_test

import (
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/postgresql"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/testserver"
)

type integrationEnv struct {
	fw       *addon.ProviderFramework
	ec       *entityserver.Client
	eac      *entityserver_v1alpha.EntityAccessClient
	registry *saga.Registry
	executor *saga.Executor
	storage  *saga.MemoryStorage
}

func TestSharedPostgreSQL_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if err := diskio.EnsureLoopDevices(slog.Default()); err != nil {
		t.Skip("skipping integration test: loop devices not available:", err)
	}

	err := testserver.TestServer(t)
	require.NoError(t, err)

	// Wait for system to stabilize (coordinator, runner, controllers)
	time.Sleep(5 * time.Second)

	ctx := t.Context()
	log := testutils.TestDebugLogger(t)

	rs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	require.NoError(t, err)

	client, err := rs.Connect("localhost:8443", "entities")
	require.NoError(t, err)

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(log, eac)
	fw := addon.NewProviderFramework(log, ec, eac, saga.NewMemoryStorage())

	newEnv := func() *integrationEnv {
		registry := saga.NewRegistry()
		storage := saga.NewMemoryStorage()
		executor := saga.NewExecutor(storage, saga.WithRegistry(registry))
		return &integrationEnv{
			fw:       fw,
			ec:       ec,
			eac:      eac,
			registry: registry,
			executor: executor,
			storage:  storage,
		}
	}

	t.Run("EnsureSharedServerSaga", func(t *testing.T) {
		env := newEnv()

		err := postgresql.RegisterEnsureSharedServerSaga(env.registry, env.fw)
		require.NoError(t, err)

		superuserPassword := "test-superuser-pw"
		execID := "test-ensure-shared"

		err = env.executor.Start("ensure-shared-server").
			WithID(execID).
			Input("superuserpassword", superuserPassword).
			Input("diskname", "pg-shared-data-test").
			Input("variantconfig", map[string]string{
				addon.ConfigImage: postgresql.BaseImage + ":" + postgresql.DefaultVersion,
			}).
			Execute(ctx)
		require.NoError(t, err)

		result, err := env.executor.ExecutionOutputs(ctx, execID)
		require.NoError(t, err)

		var serverID entity.Id
		err = result.Get("serverid", &serverID)
		require.NoError(t, err)
		assert.NotEmpty(t, serverID, "server ID should be set")

		var serviceHost string
		err = result.Get("servicehost", &serviceHost)
		require.NoError(t, err)
		assert.NotEmpty(t, serviceHost, "service host should be set")
		t.Logf("ensure-shared-server completed: serverID=%s serviceHost=%s", serverID, serviceHost)

		var server addon_v1alpha.PostgresServer
		err = env.ec.GetById(ctx, serverID, &server)
		require.NoError(t, err)

		assert.Equal(t, "active", server.Status)
		assert.Equal(t, "shared", server.Variant)
		assert.Equal(t, postgresql.AddonName, server.AddonName)
		assert.Equal(t, superuserPassword, server.SuperuserPassword)
		assert.NotEmpty(t, server.SandboxPool, "sandbox pool ref should be set")
		assert.NotEmpty(t, server.Service, "service ref should be set")
		assert.Equal(t, int64(0), server.AssociationCount)
		assert.Equal(t, "pg-shared-data-test", server.DiskName, "supplied disk name should be persisted")

		connStr := fmt.Sprintf("postgres://postgres:%s@%s:5432/postgres?sslmode=disable",
			superuserPassword, serviceHost)

		require.Eventually(t, func() bool {
			conn, err := pgx.Connect(ctx, connStr)
			if err != nil {
				t.Logf("waiting for postgres connectivity: %v", err)
				return false
			}
			conn.Close(ctx)
			return true
		}, 60*time.Second, 2*time.Second, "PostgreSQL should become connectable")
	})

	t.Run("ProvisionSharedPostgreSQL", func(t *testing.T) {
		provider := postgresql.NewProvider(fw)

		app := addon.App{Name: "mytest-app"}
		variant := addon.Variant{Name: "shared"}

		provResult, err := provider.Provision(ctx, app, variant)
		require.NoError(t, err)
		require.NotNil(t, provResult, "provision result should be returned")
		assert.NotEmpty(t, provResult.EnvVars, "env vars should be set")

		envMap := make(map[string]string)
		for _, v := range provResult.EnvVars {
			envMap[v.Key] = v.Value
		}
		assert.Contains(t, envMap, "DATABASE_URL")
		assert.Contains(t, envMap, "PGHOST")
		assert.Contains(t, envMap, "PGPORT")
		assert.Contains(t, envMap, "PGUSER")
		assert.Contains(t, envMap, "PGPASSWORD")
		assert.Contains(t, envMap, "PGDATABASE")
		t.Logf("DATABASE_URL=%s", envMap["DATABASE_URL"])

		var server addon_v1alpha.PostgresServer
		err = ec.Get(ctx, "pg-shared", &server)
		require.NoError(t, err)

		assert.Equal(t, "active", server.Status)
		assert.Equal(t, int64(1), server.AssociationCount)

		require.Eventually(t, func() bool {
			conn, err := pgx.Connect(ctx, envMap["DATABASE_URL"])
			if err != nil {
				t.Logf("waiting for app database connectivity: %v", err)
				return false
			}
			defer conn.Close(ctx)

			var result int
			err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
			return err == nil && result == 1
		}, 60*time.Second, 2*time.Second, "app database should be connectable")
	})

	t.Run("FindOrCreateSharedServer_ExistingServer", func(t *testing.T) {
		provider := postgresql.NewProvider(fw)
		variant := addon.Variant{Name: "shared"}

		// Record server state before this subtest's provisions
		var serverBefore addon_v1alpha.PostgresServer
		err := ec.Get(ctx, "pg-shared", &serverBefore)
		require.NoError(t, err)
		countBefore := serverBefore.AssociationCount

		firstResult, err := provider.Provision(ctx, addon.App{Name: "first-app"}, variant)
		require.NoError(t, err)
		require.NotNil(t, firstResult)

		var serverAfterFirst addon_v1alpha.PostgresServer
		err = ec.Get(ctx, "pg-shared", &serverAfterFirst)
		require.NoError(t, err)
		firstPoolRef := serverAfterFirst.SandboxPool
		firstServiceRef := serverAfterFirst.Service
		assert.Equal(t, countBefore+1, serverAfterFirst.AssociationCount)

		secondResult, err := provider.Provision(ctx, addon.App{Name: "second-app"}, variant)
		require.NoError(t, err)
		require.NotNil(t, secondResult)

		var serverAfterSecond addon_v1alpha.PostgresServer
		err = ec.Get(ctx, "pg-shared", &serverAfterSecond)
		require.NoError(t, err)

		assert.Equal(t, firstPoolRef, serverAfterSecond.SandboxPool, "should reuse the same pool")
		assert.Equal(t, firstServiceRef, serverAfterSecond.Service, "should reuse the same service")
		assert.Equal(t, countBefore+2, serverAfterSecond.AssociationCount, "association count should increase by 2")

		secondEnvMap := make(map[string]string)
		for _, v := range secondResult.EnvVars {
			secondEnvMap[v.Key] = v.Value
		}
		assert.Contains(t, secondEnvMap, "DATABASE_URL")
		assert.Equal(t, "second_app", secondEnvMap["PGDATABASE"], "second app should have its own database")
	})
}
