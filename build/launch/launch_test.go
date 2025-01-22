package launch_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/build"
	"miren.dev/runtime/build/launch"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/run"
)

func TestLaunch(t *testing.T) {
	t.Run("starts a buildkitd instance inside containerd", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(
			observability.TestInject,
			build.TestInject,
			ingress.TestInject,
			discovery.TestInject,
			run.TestInject,
			network.TestInject,
		)

		defer cleanup()

		var lbk *launch.LaunchBuildkit

		err := reg.Init(&lbk)
		r.NoError(err)

		rbk, err := lbk.Launch(ctx)
		r.NoError(err)

		defer rbk.Close(ctx)

		client, err := rbk.Client(ctx)
		r.NoError(err)

		info, err := client.Info(ctx)
		r.NoError(err)

		t.Log(info.BuildkitVersion)
	})
}
