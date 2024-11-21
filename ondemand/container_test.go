package ondemand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/app"
	"miren.dev/runtime/build"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/run"
)

func TestOndemand(t *testing.T) {
	t.Run("starts a container for an app", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		reg := testutils.Registry(
			observability.TestInject,
			build.TestInject,
			ingress.TestInject,
			discovery.TestInject,
			run.TestInject,
			network.TestInject,
		)

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		var lm observability.LogsMaintainer

		err = reg.Populate(&lm)
		r.NoError(err)

		err = lm.Setup(ctx)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var aa app.AppAccess

		err = reg.Populate(&aa)
		r.NoError(err)

		tx, err := aa.DB.BeginTx(ctx, pgx.TxOptions{})
		r.NoError(err)

		defer tx.Rollback(ctx)

		aa.UseTx(tx)

		err = aa.CreateApp(ctx, &app.AppConfig{
			Name: "test",
		})
		r.NoError(err)

		ac, err := aa.LoadApp(ctx, "test")
		r.NoError(err)

		err = aa.CreateVersion(ctx, &app.AppVersion{
			AppId:   ac.Id,
			Version: "aabbcc",
		})
		r.NoError(err)

		mrv, err := aa.MostRecentVersion(ctx, ac)
		r.NoError(err)

		var ii run.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, mrv.ImageName())
		r.NoError(err)

		reg.Register("app-access", &aa)

		var on LaunchContainer

		err = reg.Populate(&on)
		r.NoError(err)

		r.NoError(testutils.ClearContainers(cc, on.CD.Namespace))

		_, ch, err := on.Lookup(ctx, "test")
		r.NoError(err)

		r.NotNil(ch)

		var bg discovery.BackgroundLookup

		select {
		case <-ctx.Done():
			r.NoError(ctx.Err())
		case bg = <-ch:
			// ok
		}

		r.NoError(bg.Error)

		r.NotNil(bg.Endpoint)

		time.Sleep(time.Second)

		rw := httptest.NewRecorder()

		req, err := http.NewRequest("GET", "/", nil)
		r.NoError(err)

		bg.Endpoint.ServeHTTP(rw, req)

		r.Equal(http.StatusOK, rw.Code)
	})
}
