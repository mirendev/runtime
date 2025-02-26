package redis

import (
	"testing"

	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/v2/client"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	std "miren.dev/runtime/pkg/testutils/std"
)

func TestRedisAddon(t *testing.T) {
	t.Run("provisions a redis instance", func(t *testing.T) {
		r := require.New(t)

		s := std.Setup(t)

		defer s.Cleanup()

		var addon Addon
		err := s.Populate(&addon)
		r.NoError(err)

		defer s.CleanContainers(t)()

		// Test Plans
		plans := addon.Plans()
		r.Len(plans, 1)
		r.Equal("mini", plans[0].Name())

		// Test Provision
		res, err := addon.Provision(s, "testcache", plans[0])
		r.NoError(err)
		r.NotEmpty(res.Id)
		r.NotEmpty(res.Container)

		r.NotEmpty(res.Env["REDIS_URL"])
		r.NotEmpty(res.Config["Password"])

		// Verify container is running
		ctx := namespaces.WithNamespace(s, addon.CR.Namespace)
		container, err := s.CC.LoadContainer(ctx, res.Container)
		r.NoError(err)
		r.NotNil(container)

		task, err := container.Task(ctx, nil)
		r.NoError(err)
		r.NotNil(task)

		status, err := task.Status(ctx)
		r.NoError(err)
		r.Equal(client.Running, status.Status)

		// Try to connect to it
		opt, err := redis.ParseURL(res.Env["REDIS_URL"])
		r.NoError(err)

		client := redis.NewClient(opt)
		r.NoError(client.Ping(ctx).Err())

		defer client.Close()

		// Test Deprovision
		err = addon.Deprovision(ctx, res)
		r.NoError(err)

		// Verify container is removed
		_, err = s.CC.LoadContainer(ctx, res.Container)
		r.Error(err)
	})

	t.Run("handles provision failures", func(t *testing.T) {
		r := require.New(t)

		s := std.Setup(t)

		defer s.Cleanup()

		var addon Addon
		err := s.Populate(&addon)
		r.NoError(err)

		// Test with invalid bridge interface
		addon.Bridge = "nonexistent0"
		_, err = addon.Provision(s, "testcache", &Plan{size: "mini"})
		r.Error(err)
	})
}
