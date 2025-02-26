package postgres

import (
	"testing"

	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/v2/client"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	std "miren.dev/runtime/pkg/testutils/std"
)

func TestPostgresAddon(t *testing.T) {
	t.Run("provisions a postgres instance", func(t *testing.T) {
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
		res, err := addon.Provision(s, "testdb", plans[0])
		r.NoError(err)
		r.NotEmpty(res.Id)
		r.NotEmpty(res.Container)

		r.NotEmpty(res.Env["DATABASE_URL"])
		r.NotEmpty(res.Config["Password"])
		r.NotEmpty(res.Config["User"])
		r.NotEmpty(res.Config["Database"])

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

		// Try to connect to it.

		conn, err := pgx.Connect(ctx, res.Env["DATABASE_URL"])
		r.NoError(err)

		r.NoError(conn.Ping(ctx))

		defer conn.Close(ctx)

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
		_, err = addon.Provision(s, "testdb", &Plan{size: "mini"})
		r.Error(err)
	})
}
