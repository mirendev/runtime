package build

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestApplyWorkloadRole(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	b := &Builder{Log: log, EAS: inmem.EAC, ec: entityserver.NewClient(log, inmem.EAC)}

	const appName = "myapp"
	if _, err := inmem.Client.Create(ctx, appName, &core_v1alpha.App{}); err != nil {
		t.Fatalf("create app: %v", err)
	}

	role := func() string {
		var app core_v1alpha.App
		require.NoError(t, b.ec.Get(ctx, appName, &app))
		return app.WorkloadRole
	}

	t.Run("nil or empty is a no-op", func(t *testing.T) {
		require.NoError(t, b.applyWorkloadRole(ctx, appName, nil))
		require.NoError(t, b.applyWorkloadRole(ctx, appName, &appconfig.AppConfig{}))
		require.Empty(t, role())
	})

	t.Run("app-scoped role is applied", func(t *testing.T) {
		require.NoError(t, b.applyWorkloadRole(ctx, appName, &appconfig.AppConfig{WorkloadRole: "app-admin"}))
		require.Equal(t, "app-admin", role())
	})

	// The escalation guard: app.toml is owner-controlled, so a cluster-scoped
	// value must fail the deploy rather than silently taking effect.
	t.Run("cluster-scoped role is rejected", func(t *testing.T) {
		err := b.applyWorkloadRole(ctx, appName, &appconfig.AppConfig{WorkloadRole: "cluster-admin"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cluster-scoped")
		require.Equal(t, "app-admin", role(), "the rejected value must not be written")
	})

	t.Run("unknown role is rejected", func(t *testing.T) {
		err := b.applyWorkloadRole(ctx, appName, &appconfig.AppConfig{WorkloadRole: "sorcerer"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown")
	})
}
