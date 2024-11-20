package app

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/testutils"
)

func TestAppAccess(t *testing.T) {
	t.Run("manipulates app configs", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry()

		var aa AppAccess

		err := reg.Populate(&aa)
		r.NoError(err)

		ctx := context.Background()

		tx, err := aa.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		aa.tx = tx

		err = aa.CreateApp(ctx, &AppConfig{
			Name: "test",
		})
		r.NoError(err)

		app, err := aa.LoadApp(ctx, "test")
		r.NoError(err)

		r.Equal("test", app.Name)

		apps, err := aa.ListApps(ctx)
		r.NoError(err)

		r.Len(apps, 1)

		r.Equal(app.Id, apps[0].Id)
	})

	t.Run("manipulatse app versions", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry()

		var aa AppAccess

		err := reg.Populate(&aa)
		r.NoError(err)

		ctx := context.Background()

		tx, err := aa.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		aa.tx = tx

		err = aa.CreateApp(ctx, &AppConfig{
			Name: "test",
		})
		r.NoError(err)

		app, err := aa.LoadApp(ctx, "test")
		r.NoError(err)

		err = aa.CreateVersion(ctx, &AppVersion{
			AppId:   app.Id,
			Version: "1.0.0",
		})
		r.NoError(err)

		ver, err := aa.LoadVersion(ctx, app, "1.0.0")
		r.NoError(err)

		r.Equal("1.0.0", ver.Version)

		mrv, err := aa.MostRecentVersion(ctx, app)
		r.NoError(err)

		r.Equal("1.0.0", mrv.Version)
	})
}
