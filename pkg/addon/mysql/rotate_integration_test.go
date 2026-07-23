package mysql_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/addon/mysql"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/saga"
	"miren.dev/runtime/pkg/testserver"
)

func mysqlIntegrationClients(t *testing.T) (context.Context, *addon.ProviderFramework, *entityserver.Client, *entityserver_v1alpha.EntityAccessClient) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if err := diskio.EnsureLoopDevices(slog.Default()); err != nil {
		t.Skip("skipping integration test: loop devices not available:", err)
	}

	require.NoError(t, testserver.TestServer(t))

	// Wait for the system to stabilize (coordinator, runner, controllers).
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

	return ctx, fw, ec, eac
}

// mysqlPing reports whether user/password authenticates against the server.
func mysqlPing(ctx context.Context, host, user, password, database string) error {
	cfg := drivermysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:3306", host)
	cfg.DBName = database
	cfg.TLSConfig = "false"

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	return db.PingContext(ctx)
}

// TestMySQL_Integration provisions real shared and dedicated MySQL servers and
// rotates every credential each exposes, proving each rotation at the wire: the
// new password authenticates, the old one is rejected, admin state lands on the
// server entity, and (for shared root) the data disk name stays put. All flows
// run as subtests under a single coordinator so they never bind a second one on
// the same fixed port.
func TestMySQL_Integration(t *testing.T) {
	ctx, fw, ec, eac := mysqlIntegrationClients(t)
	provider := mysql.NewProvider(fw)

	// buildAssoc mirrors production: create the association, patch the provider's
	// data attrs on, and read it back as the raw entity the rotator decodes.
	buildAssoc := func(t *testing.T, variant string, attrs []entity.Attr) (entity.Id, *entity.Entity) {
		assocID, err := ec.Create(ctx, idgen.GenNS("addon-assoc"), &addon_v1alpha.AddonAssociation{
			Variant: variant,
			Status:  "active",
		})
		require.NoError(t, err)
		require.NoError(t, ec.Patch(ctx, assocID, 0, attrs...))
		resp, err := eac.Get(ctx, assocID.String())
		require.NoError(t, err)
		return assocID, resp.Entity().Entity()
	}

	envMap := func(vars []addon.Variable) map[string]string {
		m := make(map[string]string)
		for _, v := range vars {
			m[v.Key] = v.Value
		}
		return m
	}

	// --- Shared ---

	sharedProv, err := provider.Provision(ctx, addon.App{Name: "myshared-app"}, addon.Variant{Name: "shared"})
	require.NoError(t, err)
	require.NotNil(t, sharedProv)
	sharedEnv := envMap(sharedProv.EnvVars)
	sharedHost := sharedEnv["MYSQL_HOST"]
	sharedUser := sharedEnv["MYSQL_USER"]
	sharedDB := sharedEnv["MYSQL_DATABASE"]
	sharedOldUserPass := sharedEnv["MYSQL_PASSWORD"]
	require.NotEmpty(t, sharedHost)

	_, sharedAssoc := buildAssoc(t, "shared", sharedProv.Attrs)

	require.Eventually(t, func() bool {
		return mysqlPing(ctx, sharedHost, sharedUser, sharedOldUserPass, sharedDB) == nil
	}, 90*time.Second, 3*time.Second, "shared MySQL should become connectable")

	t.Run("RotateSharedUser", func(t *testing.T) {
		const newSecret = "rotated-shared-user"
		res, err := provider.RotateCredential(ctx,
			addon.AddonAssociation{Variant: "shared", Entity: sharedAssoc}, "user", newSecret)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, newSecret, envMap(res.EnvVars)["MYSQL_PASSWORD"])

		require.NoError(t, mysqlPing(ctx, sharedHost, sharedUser, newSecret, sharedDB),
			"new user password should authenticate")
		assert.Error(t, mysqlPing(ctx, sharedHost, sharedUser, sharedOldUserPass, sharedDB),
			"old user password should be rejected")
	})

	t.Run("RotateSharedRoot", func(t *testing.T) {
		var before addon_v1alpha.MysqlServer
		require.NoError(t, ec.Get(ctx, "my-shared", &before))
		oldRoot := before.RootPassword
		diskBefore := before.DiskName
		require.NotEmpty(t, oldRoot)
		require.NotEmpty(t, diskBefore, "provisioned shared server should have an explicit disk_name")

		const newSecret = "rotated-shared-root"
		res, err := provider.RotateCredential(ctx,
			addon.AddonAssociation{Variant: "shared", Entity: sharedAssoc}, "root", newSecret)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Empty(t, res.EnvVars, "root rotation redeploys nothing")

		var after addon_v1alpha.MysqlServer
		require.NoError(t, ec.Get(ctx, "my-shared", &after))
		assert.Equal(t, newSecret, after.RootPassword, "entity should record the new root password")
		assert.Equal(t, diskBefore, after.DiskName, "disk_name must stay stable across root rotation")

		require.NoError(t, mysqlPing(ctx, sharedHost, "root", newSecret, "mysql"),
			"new root password should authenticate")
		assert.Error(t, mysqlPing(ctx, sharedHost, "root", oldRoot, "mysql"),
			"old root password should be rejected")
	})

	// --- Dedicated ---

	dedProv, err := provider.Provision(ctx, addon.App{Name: "myded-app"}, addon.Variant{Name: "small"})
	require.NoError(t, err)
	require.NotNil(t, dedProv)
	dedEnv := envMap(dedProv.EnvVars)
	dedHost := dedEnv["MYSQL_HOST"]
	dedUser := dedEnv["MYSQL_USER"]
	dedDB := dedEnv["MYSQL_DATABASE"]
	dedOldUserPass := dedEnv["MYSQL_PASSWORD"]
	require.NotEmpty(t, dedHost)

	_, dedAssoc := buildAssoc(t, "small", dedProv.Attrs)

	var dedData addon_v1alpha.MysqlDedicatedData
	dedData.Decode(dedAssoc)
	require.NotEmpty(t, dedData.MysqlServer)
	require.NotEmpty(t, dedData.Username, "username should be stored on the dedicated association")
	require.NotEmpty(t, dedData.DatabaseName, "database name should be stored on the dedicated association")

	require.Eventually(t, func() bool {
		return mysqlPing(ctx, dedHost, dedUser, dedOldUserPass, dedDB) == nil
	}, 90*time.Second, 3*time.Second, "dedicated MySQL should become connectable")

	t.Run("RotateDedicatedUser", func(t *testing.T) {
		const newSecret = "rotated-ded-user"
		res, err := provider.RotateCredential(ctx,
			addon.AddonAssociation{Variant: "small", Entity: dedAssoc}, "user", newSecret)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, newSecret, envMap(res.EnvVars)["MYSQL_PASSWORD"])

		require.NoError(t, mysqlPing(ctx, dedHost, dedUser, newSecret, dedDB),
			"new user password should authenticate")
		assert.Error(t, mysqlPing(ctx, dedHost, dedUser, dedOldUserPass, dedDB),
			"old user password should be rejected")
	})

	t.Run("RotateDedicatedRoot", func(t *testing.T) {
		var before addon_v1alpha.MysqlServer
		require.NoError(t, ec.GetById(ctx, dedData.MysqlServer, &before))
		oldRoot := before.RootPassword
		require.NotEmpty(t, oldRoot)

		const newSecret = "rotated-ded-root"
		res, err := provider.RotateCredential(ctx,
			addon.AddonAssociation{Variant: "small", Entity: dedAssoc}, "root", newSecret)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Empty(t, res.EnvVars, "root rotation redeploys nothing")

		var after addon_v1alpha.MysqlServer
		require.NoError(t, ec.GetById(ctx, dedData.MysqlServer, &after))
		assert.Equal(t, newSecret, after.RootPassword, "entity should record the new root password")

		require.NoError(t, mysqlPing(ctx, dedHost, "root", newSecret, "mysql"),
			"new root password should authenticate")
		assert.Error(t, mysqlPing(ctx, dedHost, "root", oldRoot, "mysql"),
			"old root password should be rejected")
	})
}
