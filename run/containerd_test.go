package run

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	buildkit "github.com/moby/buildkit/client"
	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/build"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/testutils"
)

func TestContainerd(t *testing.T) {
	t.Run("can import an image", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry()

		cc, err := asm.Pick[*containerd.Client](reg)
		r.NoError(err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cl, err := buildkit.New(ctx, "docker-container://test-buildkit")
		r.NoError(err)

		bkl, err := build.NewBuildkit(ctx, cl, t.TempDir())
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		f, err := os.Create("../tmp/nginx.tar")
		r.NoError(err)

		defer f.Close()

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

		reg := testutils.Registry()

		cc, err := asm.Pick[*containerd.Client](reg)
		r.NoError(err)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cl, err := buildkit.New(ctx, "docker-container://test-buildkit")
		r.NoError(err)

		bkl, err := build.NewBuildkit(ctx, cl, t.TempDir())
		r.NoError(err)

		dfr, err := build.MakeTar("testdata/nginx")
		r.NoError(err)

		f, err := os.Create("../tmp/nginx.tar")
		r.NoError(err)

		defer f.Close()

		datafs, err := build.TarFS(dfr, t.TempDir())
		r.NoError(err)

		o, err := bkl.Transform(ctx, datafs)
		r.NoError(err)

		var ii ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
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

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		c, err := cc.LoadContainer(ctx, id)
		r.NoError(err)

		r.NotNil(c)
	})
}
