package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/build"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/run"
)

func TestContainer(t *testing.T) {
	t.Run("runs a container and routes an http request to it", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		reg := testutils.Registry(observability.TestInject, build.TestInject, ingress.TestInject, discovery.TestInject)

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

		var ii run.ImageImporter

		err = reg.Populate(&ii)
		r.NoError(err)

		err = ii.ImportImage(ctx, o, "mn-nginx:latest")
		r.NoError(err)

		ctx = namespaces.WithNamespace(ctx, ii.Namespace)

		_, err = cc.GetImage(ctx, "mn-nginx:latest")
		r.NoError(err)

		var cr run.ContainerRunner

		err = reg.Populate(&cr)
		r.NoError(err)

		sa, err := netip.ParsePrefix("172.16.8.1/24")
		r.NoError(err)

		ca, err := netip.ParsePrefix("172.16.8.2/24")
		r.NoError(err)

		config := &run.ContainerConfig{
			App:   "mn-nginx",
			Image: "mn-nginx:latest",
			IPs:   []netip.Prefix{ca},
			Subnet: &run.Subnet{
				Id:     "sub",
				IP:     []netip.Prefix{sa},
				OSName: "mtest",
			},
		}

		id, err := cr.RunContainer(ctx, config)
		r.NoError(err)

		// Let it boot up
		time.Sleep(time.Second)

		c, err := cc.LoadContainer(ctx, id)
		r.NoError(err)

		r.NotNil(c)

		defer c.Delete(ctx, containerd.WithSnapshotCleanup)

		var (
			contDisc discovery.Containerd
			ingress  ingress.HTTP
		)

		err = reg.Populate(&contDisc)
		r.NoError(err)

		err = reg.Populate(&ingress)
		r.NoError(err)

		ingress.Lookup = &contDisc

		req, err := http.NewRequest("GET", "/", strings.NewReader(""))
		r.NoError(err)

		req.Host = "mn-nginx.miren.test"

		rw := httptest.NewRecorder()

		ingress.ServeHTTP(rw, req)

		r.Equal(http.StatusOK, rw.Code)

		spew.Dump(rw.Body.String())

	})
}
