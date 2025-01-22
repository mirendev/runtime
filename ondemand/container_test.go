package ondemand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/davecgh/go-spew/spew"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/app"
	"miren.dev/runtime/build"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/image"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/run"
)

func TestOndemand(t *testing.T) {
	t.Run("starts a container for an app", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(
			observability.TestInject,
			build.TestInject,
			ingress.TestInject,
			discovery.TestInject,
			run.TestInject,
			network.TestInject,
		)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		var (
			lm  observability.LogsMaintainer
			cm  health.ContainerMonitor
			mon observability.RunSCMonitor
		)

		err = reg.Populate(&lm)
		r.NoError(err)

		err = lm.Setup(ctx)
		r.NoError(err)

		err = reg.Populate(&cm)
		r.NoError(err)

		go cm.MonitorEvents(ctx)

		reg.Register("ports", observability.PortTracker(&cm))

		err = reg.Populate(&mon)
		r.NoError(err)

		mon.SetEndpoint(filepath.Join(t.TempDir(), "runsc-mon.sock"))

		runscBin, podInit := testutils.SetupRunsc(t.TempDir())

		err = mon.WritePodInit(podInit)
		r.NoError(err)

		err = mon.Monitor(ctx)
		r.NoError(err)

		defer mon.Close()

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

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, mrv.ImageName())
		r.NoError(err)

		reg.Register("app-access", &aa)

		reg.Provide(func() *discovery.Containerd {
			return &discovery.Containerd{}
		})

		var on LaunchContainer

		err = reg.Populate(&on)
		r.NoError(err)

		on.CR.RunscBinary = runscBin

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

		req, err := http.NewRequest("GET", "/", nil)
		r.NoError(err)

		var rw *httptest.ResponseRecorder

		spew.Dump(bg.Endpoint)

		r.Eventually(func() bool {
			rw = httptest.NewRecorder()
			bg.Endpoint.ServeHTTP(rw, req)

			return rw.Code == http.StatusOK

		}, 5*time.Second, time.Second)

		r.Equal(http.StatusOK, rw.Code)
	})
}
