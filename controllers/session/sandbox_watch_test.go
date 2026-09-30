package session

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestSharedHostChangesWakeBoundSessions(t *testing.T) {
	for _, event := range []string{"status", "teardown"} {
		t.Run(event, func(t *testing.T) {
			ctx := t.Context()
			inm, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			host := entity.Id("sandbox/shared")
			ids := []entity.Id{"session/one", "session/two"}
			for _, id := range ids {
				_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
					(&sessionapi.Session{App: "app/one"}).Encode).Attrs())
				require.NoError(t, err)
				_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, shared.BindingID(id),
					(&sessionapi.Binding{Session: id.String(), Sandbox: host.String()}).Encode).Attrs())
				require.NoError(t, err)
			}
			events := make(chan entity.Id, len(ids))
			target := controller.NewReconcileController("session-wake-test", slog.Default(),
				entity.Any(entity.Type, "test/non-matching-target"), inm.EAC,
				func(_ context.Context, e controller.Event) ([]entity.Attr, error) {
					events <- e.Id
					return nil, nil
				}, 0, 1)
			require.NoError(t, target.Start(ctx))
			t.Cleanup(target.Stop)
			watch := NewSandboxWatchController(target, inm.EAC)
			if event == "status" {
				require.NoError(t, watch.Update(ctx, &compute.Sandbox{ID: host}, nil))
			} else {
				require.NoError(t, (&TeardownWatchController{SandboxWatchController: *watch}).Update(ctx,
					&compute.SandboxTeardown{Sandbox: host.String()}, nil))
			}
			seen := make(map[entity.Id]bool)
			for range ids {
				select {
				case id := <-events:
					seen[id] = true
				case <-time.After(time.Second):
					t.Fatal("bound Session was not woken")
				}
			}
			for _, id := range ids {
				require.True(t, seen[id], "missing wake for %s", id)
			}
		})
	}
}
