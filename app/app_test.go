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

		reg, cleanup := testutils.Registry()
		defer cleanup()

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

		reg, cleanup := testutils.Registry()
		defer cleanup()

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

		iver := &AppVersion{
			App:   app,
			AppId: app.Id,
		}

		err = aa.CreateVersion(ctx, iver)
		r.NoError(err)

		ver, err := aa.LoadVersion(ctx, app, iver.Version)
		r.NoError(err)

		r.Equal(iver.Version, ver.Version)

		mrv, err := aa.MostRecentVersion(ctx, app)
		r.NoError(err)

		r.Equal(iver.Version, mrv.Version)
	})
}
