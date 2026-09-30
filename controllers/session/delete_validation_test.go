package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestSessionSandboxDeletionRequiresRunnerTeardown(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	inm.Server.DeleteValidators = map[entity.Id]func(context.Context, *entity.Entity, entity.Store) error{
		compute.KindSandbox: ValidateSandboxDelete,
	}
	for _, tc := range []struct {
		id      entity.Id
		sandbox compute.Sandbox
	}{
		{"sandbox/dedicated", compute.Sandbox{Status: compute.DEAD, SessionInfo: compute.SessionInfo{Owner: "session/one"}}},
		{"sandbox/shared", compute.Sandbox{Status: compute.DEAD, SessionInfo: compute.SessionInfo{Group: "group"}}},
	} {
		_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, tc.id, tc.sandbox.Encode).Attrs())
		require.NoError(t, err)
		_, err = inm.EAC.Delete(ctx, tc.id.String())
		require.Error(t, err, "DEAD is not runner teardown")
		_, err = inm.EAC.Get(ctx, tc.id.String())
		require.NoError(t, err)
		_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, computeapi.TeardownID(tc.id),
			(&compute.SandboxTeardown{Sandbox: tc.id.String()}).Encode).Attrs())
		require.NoError(t, err)
		_, err = inm.EAC.Delete(ctx, tc.id.String())
		require.NoError(t, err)
	}
}

func TestLostNodePermitsTerminalSessionSandboxReplacement(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	inm.Server.DeleteValidators = map[entity.Id]func(context.Context, *entity.Entity, entity.Store) error{
		compute.KindSandbox: ValidateSandboxDelete,
	}
	id := entity.Id("sandbox/lost-node")
	node := entity.Id("node/lost")
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, node, (&compute.Node{}).Encode).Attrs())
	require.NoError(t, err)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{Status: compute.DEAD, SessionInfo: compute.SessionInfo{Owner: "session/one"}}).Encode,
		(&compute.Schedule{Key: compute.Key{Node: node}}).Encode).Attrs())
	require.NoError(t, err)
	c := NewController(slog.Default(), inm.EAC)
	done, err := c.teardownDone(ctx, id)
	require.NoError(t, err)
	require.False(t, done)
	_, err = inm.EAC.Delete(ctx, id.String())
	require.Error(t, err)
	_, err = inm.EAC.Delete(ctx, node.String())
	require.NoError(t, err)
	done, err = c.teardownDone(ctx, id)
	require.NoError(t, err)
	require.True(t, done)
	_, err = inm.EAC.Delete(ctx, id.String())
	require.NoError(t, err)
}
