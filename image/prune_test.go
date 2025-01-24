package image

import (
	"context"
	"testing"
	"time"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/jackc/pgx/v5"
	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/app"
	"miren.dev/runtime/pkg/testutils"
)

type testImageInUse struct {
	images map[string]struct{}
}

func (t *testImageInUse) ImageInUse(ctx context.Context, image string) (bool, error) {
	_, ok := t.images[image]

	return ok, nil
}

func TestPrune(t *testing.T) {
	t.Run("can calculate which images to prune", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry()
		defer cleanup()

		var aa app.AppAccess

		err := reg.Populate(&aa)
		r.NoError(err)

		reg.Register("appaccess", &aa)

		var ti testImageInUse
		ti.images = make(map[string]struct{})

		reg.Register("imageinuse", &ti)

		var ip ImagePruner

		err = reg.Populate(&ip)
		r.NoError(err)

		tx, err := aa.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		aa.UseTx(tx)

		err = aa.CreateApp(ctx, &app.AppConfig{
			Name: "test",
		})
		r.NoError(err)

		ao, err := aa.LoadApp(ctx, "test")
		r.NoError(err)

		v1 := identity.NewID()

		v1ver := &app.AppVersion{
			App:     ao,
			AppId:   ao.Id,
			Version: v1,
		}

		err = aa.CreateVersion(ctx, v1ver)
		r.NoError(err)

		vers, err := ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 0)

		v2 := identity.NewID()

		v2ver := &app.AppVersion{
			App:     ao,
			AppId:   ao.Id,
			Version: v2,
		}

		err = aa.CreateVersion(ctx, v2ver)
		r.NoError(err)

		vers, err = ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 1)

		r.Equal(v1, vers[0].Version)

		ti.images[v1ver.ImageName()] = struct{}{}

		vers, err = ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 0)

		ti.images = make(map[string]struct{})

		ctx = namespaces.WithNamespace(ctx, ip.Namespace)

		err = ip.Prune(ctx, ao)
		r.NoError(err)

		vers, err = ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 0)

		_, err = aa.LoadVersion(ctx, ao, v1)
		r.Error(err)
	})

	t.Run("keeps a certain number of inactive versions", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry()
		defer cleanup()

		var aa app.AppAccess

		err := reg.Populate(&aa)
		r.NoError(err)

		reg.Register("appaccess", &aa)

		var ti testImageInUse
		ti.images = make(map[string]struct{})

		reg.Register("imageinuse", &ti)

		var ip ImagePruner

		err = reg.Populate(&ip)
		r.NoError(err)

		ip.RollbackWindow = 2

		tx, err := aa.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		aa.UseTx(tx)

		err = aa.CreateApp(ctx, &app.AppConfig{
			Name: "test",
		})
		r.NoError(err)

		ao, err := aa.LoadApp(ctx, "test")
		r.NoError(err)

		err = aa.CreateVersion(ctx, &app.AppVersion{
			AppId:   ao.Id,
			Version: identity.NewID(),
		})
		r.NoError(err)

		vers, err := ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 0)

		v2 := identity.NewID()

		v2ver := &app.AppVersion{
			App:     ao,
			AppId:   ao.Id,
			Version: v2,
		}

		err = aa.CreateVersion(ctx, v2ver)
		r.NoError(err)

		vers, err = ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 0)

		v3 := identity.NewID()

		err = aa.CreateVersion(ctx, &app.AppVersion{
			AppId:   ao.Id,
			Version: v3,
		})
		r.NoError(err)

		vers, err = ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 0)

		v4 := identity.NewID()

		err = aa.CreateVersion(ctx, &app.AppVersion{
			AppId:   ao.Id,
			Version: v4,
		})
		r.NoError(err)

		vers, err = ip.VersionsToPrune(ctx, ao)
		r.NoError(err)

		r.Len(vers, 1)
	})
}
