package lease

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
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

func TestLeaseContainer(t *testing.T) {
	t.Run("starts a container", func(t *testing.T) {
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

		dfr, err := build.MakeTar("testdata/python")
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

		defer testutils.ClearContainers(cc, on.CD.Namespace)

		r.NoError(testutils.ClearContainers(cc, on.CD.Namespace))

		lc, err := on.Lease(ctx, "test")
		r.NoError(err)

		r.NotNil(lc)
		r.True(lc.StartedWindow)

		app := lc.App

		ctx = namespaces.WithNamespace(ctx, on.CD.Namespace)

		c, err := on.CD.Client.LoadContainer(ctx, lc.Container())
		r.NoError(err)

		r.NotNil(c)

		t.Run("can lease an existing container", func(t *testing.T) {
			r := require.New(t)

			// Sleep a little so that the container racks up some cpu time
			time.Sleep(500 * time.Millisecond)

			lc2, err := on.Lease(ctx, "test")
			r.NoError(err)

			r.False(lc2.StartedWindow)
			r.NotZero(lc2.Start)
			r.NotNil(lc2)

			// Sleep a little so that the container racks up some cpu time
			time.Sleep(500 * time.Millisecond)

			res, err := on.ReleaseLease(ctx, lc2)
			r.NoError(err)

			r.NotZero(res.Usage)
		})

		t.Run("when a lease is released, the container becomes idle", func(t *testing.T) {
			r := require.New(t)

			li, err := on.ReleaseLease(ctx, lc)
			r.NoError(err)

			r.NotZero(li.Usage)

			r.False(lc.App.idle.Empty(), "container should be idle")
			r.True(lc.App.windows.Empty(), "window should be removed")
		})

		t.Run("lease operations manage the rif latency", func(t *testing.T) {
			r := require.New(t)

			val2 := on.lattrack.GetLatencyEstimate(2)
			r.NotZero(val2)
			r.False(math.IsNaN(val2))

			val1 := on.lattrack.GetLatencyEstimate(1)
			r.NotZero(val1)
			r.False(math.IsNaN(val1))

			// Exploit the above sleeps to ensure that latencies are differing
			// and are based on wall clock.
			r.Greater(val1, val2)
		})

		t.Run("closed windows emit reconds to clickhouse", func(t *testing.T) {
			r := require.New(t)

			var (
				usage  uint64
				leases uint32
			)

			err := on.DB.QueryRow(
				"SELECT usage, leases FROM container_usage WHERE app = $1", app.name,
			).Scan(&usage, &leases)

			r.NoError(err)

			r.NotZero(usage)
			r.Equal(uint32(2), leases)
		})

		t.Run("reports the current rif and latency", func(t *testing.T) {
			r := require.New(t)

			lc3, err := on.Lease(ctx, "test")
			r.NoError(err)

			rif, latency := on.LatencyEstimate()

			r.Equal(int32(1), rif)
			r.NotZero(latency)
			r.False(math.IsNaN(latency))

			val1 := on.lattrack.GetLatencyEstimate(1)
			r.Equal(val1, latency)

			on.ReleaseLease(ctx, lc3)
		})

		t.Run("idle containers are shutdown after a period of time", func(t *testing.T) {
			r := require.New(t)

			on.IdleTimeout = 0

			cnt, err := on.ShutdownIdle(ctx)
			r.NoError(err)

			r.Equal(1, cnt)

			r.True(app.idle.Empty(), "container was not destroyed")

			_, err = on.CR.CC.LoadContainer(ctx, lc.Container())
			r.Error(err)
		})
	})

	t.Run("tracks pending container launches and uses them", func(t *testing.T) {
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

		dfr, err := build.MakeTar("testdata/python")
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

		defer testutils.ClearContainers(cc, on.CD.Namespace)

		r.NoError(testutils.ClearContainers(cc, on.CD.Namespace))

		leases := make(chan *LeasedContainer, 5)
		errors := make(chan error, 5)

		for i := 0; i < 5; i++ {
			go func() {
				lc, err := on.Lease(ctx, "test")
				if err != nil {
					errors <- err
				} else {
					leases <- lc
				}
			}()
		}

		var lease *LeasedContainer

		for i := 0; i < 5; i++ {
			select {
			case err := <-errors:
				r.NoError(err)
			case cur := <-leases:
				r.NotNil(cur)

				if lease == nil {
					cur = lease
				} else {
					r.Same(lease.Window.container, cur.Window.container)
				}
			}
		}
	})

	t.Run("creates a new container if the window is full", func(t *testing.T) {
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

		dfr, err := build.MakeTar("testdata/python")
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

		on.MaxLeasesPerContainer = 2
		on.CR.RunscBinary = runscBin

		defer testutils.ClearContainers(cc, on.CD.Namespace)

		r.NoError(testutils.ClearContainers(cc, on.CD.Namespace))

		var leases []*LeasedContainer

		on.Log.Debug("starting leases")
		for i := 0; i < 5; i++ {
			lc, err := on.Lease(ctx, "test")
			r.NoError(err)

			leases = append(leases, lc)
		}

		r.Len(leases, 5)

		r.Same(leases[0].Window, leases[1].Window)
		r.Same(leases[2].Window, leases[3].Window)

		r.NotSame(leases[0].Window, leases[2].Window)
		r.NotSame(leases[2].Window, leases[4].Window)
	})
}
