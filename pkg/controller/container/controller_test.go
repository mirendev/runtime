package container

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/davecgh/go-spew/spew"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/build"
	"miren.dev/runtime/image"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/controller/container/v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/testutils"
)

func TestContainerd(t *testing.T) {
	t.Run("can run a container", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		reg, cleanup := testutils.Registry(observability.TestInject, build.TestInject)
		defer cleanup()

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		var (
			lm observability.LogsMaintainer
			rm observability.ResourcesMonitor
		)

		err = reg.Populate(&lm)
		r.NoError(err)

		err = lm.Setup(ctx)
		r.NoError(err)

		err = reg.Populate(&rm)
		r.NoError(err)

		err = rm.Setup(ctx)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, _, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii image.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var (
			co  ContainerController
			mon observability.RunSCMonitor
		)

		err = reg.Populate(&co)
		r.NoError(err)

		err = reg.Populate(&mon)
		r.NoError(err)

		mon.SetEndpoint(filepath.Join(t.TempDir(), "runsc-mon.sock"))

		runscBin, podInit := testutils.SetupRunsc(t.TempDir())

		co.RunscBinary = runscBin

		err = mon.WritePodInit(podInit)
		r.NoError(err)

		err = mon.Monitor(ctx)
		r.NoError(err)

		defer mon.Close()

		id := "cont-xyz"

		var container v1alpha.Container

		container.Image = "mn-nginx:latest"
		container.ID = id

		container.Label = append(container.Label, "runtime.computer/app=mn-nginx")

		cont := &entity.Entity{
			ID:    entity.Id(id),
			Attrs: container.Encode(),
		}

		var tco v1alpha.Container
		tco.Decode(cont)

		err = co.Create(ctx, &tco)
		r.NoError(err)

		r.Len(tco.Network, 1)

		ca, err := netip.ParsePrefix(tco.Network[0].Address)
		r.NoError(err)

		c, err := cc.LoadContainer(ctx, id)
		r.NoError(err)

		r.NotNil(c)

		defer testutils.ClearContainer(ctx, c)

		lbls, err := c.Labels(ctx)
		r.NoError(err)

		r.Equal("mn-nginx", lbls["runtime.computer/app"])

		task, err := c.Task(ctx, nil)
		r.NoError(err)

		cgroupPath, err := observability.CGroupPathForPid(task.Pid())
		r.NoError(err)

		go func() {
			err := rm.Monitor(ctx, "cont-xyz", cgroupPath)
			r.NoError(err)
		}()

		pr, pw, err := os.Pipe()
		r.NoError(err)

		defer pr.Close()
		defer pw.Close()

		ioc := cio.NewCreator(cio.WithStreams(os.Stdin, pw, os.Stderr))

		proc, err := task.Exec(ctx, "test", &specs.Process{
			Args: []string{"ip", "-j", "addr"},
			Env:  []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			Cwd:  "/",
		}, ioc)
		r.NoError(err)

		err = proc.Start(ctx)
		r.NoError(err)

		ch, err := proc.Wait(ctx)
		r.NoError(err)

		select {
		case <-ctx.Done():
			r.NoError(ctx.Err())
		case <-ch:
			// ok
		}

		type ai struct {
			Label   string `json:"label"`
			Address string `json:"local"`
		}

		type iface struct {
			Name      string `json:"ifname"`
			Addresses []ai   `json:"addr_info"`
		}

		var ais []iface

		err = json.NewDecoder(pr).Decode(&ais)
		r.NoError(err)

		i := slices.IndexFunc(ais, func(iface iface) bool {
			return iface.Name == "eth0"
		})

		r.NotEqual(-1, i)

		var found bool

		for _, ai := range ais[i].Addresses {
			if ai.Address == ca.Addr().String() {
				found = true
			}
		}

		r.True(found, "address wasn't assigned")

		// Let nginx startup
		time.Sleep(3 * time.Second)

		spew.Dump(os.ReadFile("/tmp/log"))

		var lr observability.PersistentLogReader

		err = reg.Populate(&lr)
		r.NoError(err)

		entries, err := lr.Read(ctx, id)
		r.NoError(err)

		r.NotEmpty(entries)

		ports, err := mon.Ports.(*observability.StatusMonitor).EntityBoundPorts(id)
		r.NoError(err)

		r.Len(ports, 2)
	})
}
