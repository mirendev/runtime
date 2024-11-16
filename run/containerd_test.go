package run

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"slices"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/davecgh/go-spew/spew"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/build"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
)

func TestContainerd(t *testing.T) {
	t.Run("can import an image", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		reg := testutils.Registry(observability.TestInject, build.TestInject)

		var (
			cc  *containerd.Client
			bkl *build.Buildkit
		)

		err := reg.Init(&cc, &bkl)
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		img, err := cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		r.NotNil(img)

		defer cc.ImageService().Delete(ctx, "mn-nginx:latest")

		r.Equal("mn-nginx:latest", img.Name())
	})

	t.Run("can run a container", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reg := testutils.Registry(observability.TestInject, build.TestInject)

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

		var ii ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var cr ContainerRunner

		err = reg.Populate(&cr)
		r.NoError(err)

		sa, err := netip.ParsePrefix("172.16.8.1/24")
		r.NoError(err)

		ca, err := netip.ParsePrefix("172.16.8.2/24")
		r.NoError(err)

		config := &ContainerConfig{
			Image: "mn-nginx:latest",
			IPs:   []netip.Prefix{ca},
			Subnet: &Subnet{
				Id:     "sub",
				IP:     []netip.Prefix{sa},
				OSName: "mtest",
			},
		}

		id, err := cr.RunContainer(ctx, config)
		r.NoError(err)

		spew.Dump(config)

		c, err := cc.LoadContainer(ctx, id)
		r.NoError(err)

		r.NotNil(c)

		task, err := c.Task(ctx, nil)
		r.NoError(err)

		var output bytes.Buffer

		ioc := cio.NewCreator(cio.WithStreams(os.Stdin, &output, os.Stderr))

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

		err = json.Unmarshal(output.Bytes(), &ais)
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

		var lr observability.PersistentLogReader

		err = reg.Populate(&lr)
		r.NoError(err)

		entries, err := lr.Read(ctx, id)
		r.NoError(err)

		r.NotEmpty(entries)
	})
}
