package discovery

import (
	"context"
	"testing"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/testutils"
)

func TestContainerd(t *testing.T) {
	t.Run("can lookup an endpoint from containerd", func(t *testing.T) {
		r := require.New(t)

		reg := testutils.Registry(TestInject)

		var cl Containerd

		err := reg.Populate(&cl)
		r.NoError(err)

		ctx := namespaces.WithNamespace(context.Background(), cl.Namespace)

		cont, err := cl.Client.NewContainer(ctx, "test", containerd.WithAdditionalContainerLabels(map[string]string{
			"miren.dev/app":       "test",
			"miren.dev/http_host": "127.0.0.1:8888",
		}))
		r.NoError(err)

		defer testutils.ClearContainer(ctx, cont)

		ep, ch, err := cl.Lookup(ctx, "test")
		r.NoError(err)

		r.Nil(ep)

		var bg BackgroundLookup

		select {
		case <-ctx.Done():
			r.NoError(ctx.Err())
		case bg = <-ch:
		}

		r.NotNil(bg.Endpoint)

		ne, ok := bg.Endpoint.(*HTTPEndpoint)
		r.True(ok)

		r.Equal("http://127.0.0.1:8888", ne.Host)
	})
}
