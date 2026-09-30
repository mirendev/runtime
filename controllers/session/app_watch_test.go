package session

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core "miren.dev/runtime/api/core/core_v1alpha"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestAppDeployRollsSessions(t *testing.T) {
	for _, capacity := range []int64{1, 2} {
		t.Run(map[int64]string{1: "dedicated", 2: "shared"}[capacity], func(t *testing.T) {
			ctx := t.Context()
			inm, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			controller := NewController(slog.Default(), inm.EAC)
			watch := &AppWatchController{Sessions: controller}
			appID, err := inm.Client.Create(ctx, "worker", &core.App{})
			require.NoError(t, err)
			version := func(name, image, env string) entity.Id {
				t.Helper()
				cfg, err := inm.Client.Create(ctx, name+"-config", &core.ConfigVersion{
					App: appID, Spec: core.ConfigSpec{
						Variables: []core.ConfigSpecVariables{{Key: "OLD", Value: env}},
						Services:  []core.ConfigSpecServices{{Name: "worker", Image: image}},
					},
				})
				require.NoError(t, err)
				id, err := inm.Client.Create(ctx, name, &core.AppVersion{
					App: appID, ConfigVersion: cfg, ImageUrl: "base:v0", Version: name,
				})
				require.NoError(t, err)
				return id
			}
			v1 := version("v1", "example:v1", "previous")
			v2 := version("v2", "example:v2", "new")
			setActive := func(id entity.Id) {
				t.Helper()
				_, err := inm.EAC.Patch(ctx, entity.New(entity.DBId, appID,
					(&core.App{ActiveVersion: id}).Encode).Attrs(), 0)
				require.NoError(t, err)
			}
			read := func() sessionapi.Session {
				t.Helper()
				resp, err := inm.EAC.Get(ctx, "session/worker-job")
				require.NoError(t, err)
				var s sessionapi.Session
				s.Decode(resp.Entity().Entity())
				return s
			}
			reconcile := func() sessionapi.Session {
				t.Helper()
				s := read()
				require.NoError(t, controller.Reconcile(ctx, &s, &entity.Meta{}))
				return read()
			}
			setActive(v1)
			_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, entity.Id("session/worker-job"),
				(&sessionapi.Session{App: appID, Service: "worker", MaxSessionsPerSandbox: capacity,
					DesiredState: sessionapi.RUNNING}).Encode).Attrs())
			require.NoError(t, err)
			require.NoError(t, watch.Update(ctx, &core.App{ID: appID, ActiveVersion: v1}, nil))
			first := reconcile()
			old, err := controller.getSandbox(ctx, first.Sandbox)
			require.NoError(t, err)
			require.Equal(t, v1, old.Spec.Version)
			require.Equal(t, "docker.io/library/example:v1", old.Spec.Container[0].Image, "service image overrides app image")
			if capacity > 1 {
				_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, entity.Id("session/worker-second"),
					(&sessionapi.Session{App: appID, Service: "worker", MaxSessionsPerSandbox: capacity,
						DesiredState: sessionapi.RUNNING}).Encode).Attrs())
				require.NoError(t, err)
				require.NoError(t, watch.Update(ctx, &core.App{ID: appID, ActiveVersion: v1}, nil))
				resp, err := inm.EAC.Get(ctx, "session/worker-second")
				require.NoError(t, err)
				var second sessionapi.Session
				second.Decode(resp.Entity().Entity())
				require.NoError(t, controller.Reconcile(ctx, &second, &entity.Meta{}))
				resp, err = inm.EAC.Get(ctx, "session/worker-second")
				require.NoError(t, err)
				second.Decode(resp.Entity().Entity())
				require.Equal(t, first.Sandbox, second.Sandbox)
			}
			_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, first.Sandbox,
				(&compute.Sandbox{Status: compute.RUNNING}).Encode).Attrs(), 0)
			require.NoError(t, err)
			setActive(v2)
			require.NoError(t, watch.Update(ctx, &core.App{ID: appID, ActiveVersion: v2}, nil))
			require.NoError(t, watch.Update(ctx, &core.App{ID: appID, ActiveVersion: v1}, nil),
				"a stale app event must not revert the Session")
			updated := read()
			require.Equal(t, v2, updated.Version)
			require.Equal(t, "docker.io/library/example:v2", updated.Spec.Container[0].Image)
			require.True(t, slices.Contains(updated.Spec.Container[0].Env, "OLD=new"))
			require.False(t, slices.Contains(updated.Spec.Container[0].Env, "OLD=previous"))
			require.Equal(t, first.Sandbox, reconcile().Sandbox, "old host must drain before replacement")
			old, err = controller.getSandbox(ctx, first.Sandbox)
			require.NoError(t, err)
			if capacity > 1 {
				require.False(t, old.SessionInfo.ClosingAt.IsZero())
				require.Equal(t, sharedGroup(&updated), old.SessionInfo.Group)
				require.Equal(t, capacity, old.SessionInfo.Capacity)
				reconcile()
				old, err = controller.getSandbox(ctx, first.Sandbox)
				require.NoError(t, err)
			}
			require.False(t, old.ShutdownAt.IsZero())
			_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, first.Sandbox,
				(&compute.Sandbox{ShutdownAt: time.Now().Add(-time.Minute)}).Encode).Attrs(), 0)
			require.NoError(t, err)
			reconcile()
			old, err = controller.getSandbox(ctx, first.Sandbox)
			require.NoError(t, err)
			require.Equal(t, compute.STOPPED, old.Status)
			require.Equal(t, first.Sandbox, reconcile().Sandbox, "wait for runner teardown")
			_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, computeapi.TeardownID(first.Sandbox),
				(&compute.SandboxTeardown{Sandbox: first.Sandbox.String()}).Encode).Attrs())
			require.NoError(t, err)
			second := reconcile()
			require.NotEqual(t, first.Sandbox, second.Sandbox)
			next, err := controller.getSandbox(ctx, second.Sandbox)
			require.NoError(t, err)
			require.Equal(t, v2, next.Spec.Version)
			require.Equal(t, "docker.io/library/example:v2", next.Spec.Container[0].Image)
			if capacity > 1 {
				resp, err := inm.EAC.Get(ctx, "session/worker-second")
				require.NoError(t, err)
				var other sessionapi.Session
				other.Decode(resp.Entity().Entity())
				require.Equal(t, v2, other.Version)
				require.NoError(t, controller.Reconcile(ctx, &other, &entity.Meta{}))
				resp, err = inm.EAC.Get(ctx, "session/worker-second")
				require.NoError(t, err)
				other.Decode(resp.Entity().Entity())
				require.Equal(t, second.Sandbox, other.Sandbox)
			}
		})
	}
}
