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
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/postgresql"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/idgen"
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

func TestPostgreSQL_Integration(t *testing.T) {
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

		provResult, err := provider.Provision(ctx, addon.AddonAssociation{ID: entity.Id("assoc-" + app.Name)}, app, variant)
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

		firstResult, err := provider.Provision(ctx, addon.AddonAssociation{ID: "assoc-first"}, addon.App{Name: "first-app"}, variant)
		require.NoError(t, err)
		require.NotNil(t, firstResult)

		var serverAfterFirst addon_v1alpha.PostgresServer
		err = ec.Get(ctx, "pg-shared", &serverAfterFirst)
		require.NoError(t, err)
		firstPoolRef := serverAfterFirst.SandboxPool
		firstServiceRef := serverAfterFirst.Service
		assert.Equal(t, countBefore+1, serverAfterFirst.AssociationCount)

		secondResult, err := provider.Provision(ctx, addon.AddonAssociation{ID: "assoc-second"}, addon.App{Name: "second-app"}, variant)
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

	// Provisions a dedicated server and rotates its single role live, proving the
	// rotation at the wire: the new password authenticates, the old one is
	// rejected, the entity records the new secret, and the pool is never
	// relaunched. Runs as a subtest so it shares this coordinator rather than
	// binding a second one on the same fixed port.
	t.Run("RotateDedicatedCredential", func(t *testing.T) {
		provider := postgresql.NewProvider(fw)

		provResult, err := provider.Provision(ctx, addon.AddonAssociation{ID: "assoc-dedrot"}, addon.App{Name: "dedrot-app"}, addon.Variant{Name: "small"})
		require.NoError(t, err)
		require.NotNil(t, provResult)

		provEnv := make(map[string]string)
		for _, v := range provResult.EnvVars {
			provEnv[v.Key] = v.Value
		}
		oldPassword := provEnv["PGPASSWORD"]
		host := provEnv["PGHOST"]
		require.NotEmpty(t, oldPassword)
		require.NotEmpty(t, host)

		// Build the association the way production does: create it, then patch the
		// provider's data attrs on. Reading it back gives the raw entity the
		// rotator decodes (server ref + the newly-stored username/database).
		assocID, err := ec.Create(ctx, idgen.GenNS("addon-assoc"), &addon_v1alpha.AddonAssociation{
			Variant: "small",
			Status:  "active",
		})
		require.NoError(t, err)
		require.NoError(t, ec.Patch(ctx, assocID, 0, provResult.Attrs...))

		resp, err := eac.Get(ctx, assocID.String())
		require.NoError(t, err)
		rawAssoc := resp.Entity().Entity()

		var data addon_v1alpha.PostgresqlDedicatedData
		data.Decode(rawAssoc)
		require.NotEmpty(t, data.PostgresServer, "server ref should be stored on the association")
		require.NotEmpty(t, data.Username, "username should be stored on the association")
		require.NotEmpty(t, data.DatabaseName, "database name should be stored on the association")

		var serverBefore addon_v1alpha.PostgresServer
		require.NoError(t, ec.GetById(ctx, data.PostgresServer, &serverBefore))
		require.Equal(t, oldPassword, serverBefore.SuperuserPassword)
		poolBefore := serverBefore.SandboxPool

		// The freshly-provisioned server should accept the provisioned credential.
		oldConnStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
			data.Username, oldPassword, host, data.DatabaseName)
		require.Eventually(t, func() bool {
			conn, err := pgx.Connect(ctx, oldConnStr)
			if err != nil {
				t.Logf("waiting for dedicated postgres connectivity: %v", err)
				return false
			}
			conn.Close(ctx)
			return true
		}, 60*time.Second, 2*time.Second, "dedicated PostgreSQL should become connectable")

		const newSecret = "rotated-dedicated-secret"
		rotResult, err := provider.RotateCredential(ctx,
			addon.AddonAssociation{ID: assocID, Variant: "small", Entity: rawAssoc},
			"user", newSecret)
		require.NoError(t, err)
		require.NotNil(t, rotResult)

		rotEnv := make(map[string]string)
		for _, v := range rotResult.EnvVars {
			rotEnv[v.Key] = v.Value
		}
		assert.Equal(t, newSecret, rotEnv["PGPASSWORD"], "result should carry the new password")
		assert.Contains(t, rotEnv["DATABASE_URL"], newSecret, "connection URL should embed the new password")

		// The server entity records the new password, and the pool is untouched (a
		// live ALTER, not a relaunch).
		var serverAfter addon_v1alpha.PostgresServer
		require.NoError(t, ec.GetById(ctx, data.PostgresServer, &serverAfter))
		assert.Equal(t, newSecret, serverAfter.SuperuserPassword, "entity should record the new password")
		assert.Equal(t, poolBefore, serverAfter.SandboxPool, "dedicated rotation must not relaunch the pool")

		// Wire-level proof: the new password authenticates.
		newConnStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
			data.Username, newSecret, host, data.DatabaseName)
		conn, err := pgx.Connect(ctx, newConnStr)
		require.NoError(t, err, "new password should authenticate")
		var one int
		require.NoError(t, conn.QueryRow(ctx, "SELECT 1").Scan(&one))
		assert.Equal(t, 1, one)
		conn.Close(ctx)

		// And the old password is rejected.
		if rejected, err := pgx.Connect(ctx, oldConnStr); err == nil {
			rejected.Close(ctx)
			t.Error("old password should be rejected after rotation")
		}
	})

	// A dedicated association provisioned before the username/database attrs
	// existed carries only the server ref plus an app ref. CaptureDedicatedConnInfo
	// must fall back to the app's active ConfigVersion (PGUSER/PGDATABASE) to stay
	// rotatable. The fast-path subtest above always populates the attrs, so this
	// branch never runs there. Here we build the legacy shape — a real app whose
	// active config carries the connection vars, and an association with the
	// conn-info attrs stripped off — and prove the fallback resolves and the
	// rotation still lands at the wire.
	t.Run("RotateDedicatedCredential_LegacyFallback", func(t *testing.T) {
		provider := postgresql.NewProvider(fw)

		provResult, err := provider.Provision(ctx, addon.AddonAssociation{ID: "assoc-dedlegacy"}, addon.App{Name: "dedlegacy-app"}, addon.Variant{Name: "small"})
		require.NoError(t, err)
		require.NotNil(t, provResult)

		provEnv := make(map[string]string)
		for _, v := range provResult.EnvVars {
			provEnv[v.Key] = v.Value
		}
		user := provEnv["PGUSER"]
		database := provEnv["PGDATABASE"]
		oldPassword := provEnv["PGPASSWORD"]
		host := provEnv["PGHOST"]
		require.NotEmpty(t, user)
		require.NotEmpty(t, database)
		require.NotEmpty(t, oldPassword)
		require.NotEmpty(t, host)

		// Keep only the server ref from the provisioned attrs, dropping the
		// username/database attrs so the rotator is forced down the legacy path.
		var serverAttrs []entity.Attr
		for _, a := range provResult.Attrs {
			if a.ID == addon_v1alpha.PostgresqlDedicatedDataPostgresServerId {
				serverAttrs = append(serverAttrs, a)
			}
		}
		require.Len(t, serverAttrs, 1, "provision should record the server ref attr")

		// Stand up the app whose active config carries the connection vars. The
		// fallback reads user/database from here; PGPASSWORD rides along as the
		// value a per-app rollback would restore.
		cvID, err := ec.Create(ctx, idgen.GenNS("config-version"), &core_v1alpha.ConfigVersion{
			Spec: core_v1alpha.ConfigSpec{
				Variables: []core_v1alpha.ConfigSpecVariables{
					{Key: "PGUSER", Value: user},
					{Key: "PGDATABASE", Value: database},
					{Key: "PGPASSWORD", Value: oldPassword},
				},
			},
		})
		require.NoError(t, err)
		avID, err := ec.Create(ctx, idgen.GenNS("app-version"), &core_v1alpha.AppVersion{
			ConfigVersion: cvID,
		})
		require.NoError(t, err)
		appID, err := ec.Create(ctx, idgen.GenNS("app"), &core_v1alpha.App{
			ActiveVersion: avID,
		})
		require.NoError(t, err)

		// Build the legacy-shaped association: app ref set, server ref patched on,
		// username/database attrs absent.
		assocID, err := ec.Create(ctx, idgen.GenNS("addon-assoc"), &addon_v1alpha.AddonAssociation{
			Variant: "small",
			Status:  "active",
			App:     appID,
		})
		require.NoError(t, err)
		require.NoError(t, ec.Patch(ctx, assocID, 0, serverAttrs...))

		resp, err := eac.Get(ctx, assocID.String())
		require.NoError(t, err)
		rawAssoc := resp.Entity().Entity()

		// Confirm the shape we intend to exercise: server ref present, conn info absent.
		var data addon_v1alpha.PostgresqlDedicatedData
		data.Decode(rawAssoc)
		require.NotEmpty(t, data.PostgresServer, "server ref should be stored on the association")
		require.Empty(t, data.Username, "username attr should be absent (legacy shape)")
		require.Empty(t, data.DatabaseName, "database attr should be absent (legacy shape)")

		oldConnStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
			user, oldPassword, host, database)
		require.Eventually(t, func() bool {
			conn, err := pgx.Connect(ctx, oldConnStr)
			if err != nil {
				t.Logf("waiting for dedicated postgres connectivity: %v", err)
				return false
			}
			conn.Close(ctx)
			return true
		}, 60*time.Second, 2*time.Second, "dedicated PostgreSQL should become connectable")

		const newSecret = "rotated-legacy-secret"
		rotResult, err := provider.RotateCredential(ctx,
			addon.AddonAssociation{ID: assocID, Variant: "small", Entity: rawAssoc},
			"user", newSecret)
		require.NoError(t, err)
		require.NotNil(t, rotResult)

		rotEnv := make(map[string]string)
		for _, v := range rotResult.EnvVars {
			rotEnv[v.Key] = v.Value
		}
		// (a) The fallback resolved user/database from active config: with the attrs
		// absent, a correct result env can only have come from the ConfigVersion.
		assert.Equal(t, user, rotEnv["PGUSER"], "user should resolve from active config")
		assert.Equal(t, database, rotEnv["PGDATABASE"], "database should resolve from active config")
		assert.Equal(t, newSecret, rotEnv["PGPASSWORD"], "result should carry the new password")

		// (b) Wire-level proof: the new password authenticates.
		newConnStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
			user, newSecret, host, database)
		conn, err := pgx.Connect(ctx, newConnStr)
		require.NoError(t, err, "new password should authenticate")
		var one int
		require.NoError(t, conn.QueryRow(ctx, "SELECT 1").Scan(&one))
		assert.Equal(t, 1, one)
		conn.Close(ctx)

		// And the old password is rejected.
		if rejected, err := pgx.Connect(ctx, oldConnStr); err == nil {
			rejected.Close(ctx)
			t.Error("old password should be rejected after rotation")
		}
	})
}
