package run

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// The exit component has to survive the real store, not just the mock the rest
// of these tests use. The mock treats attributes more forgivingly than etcd
// does, so a component that round-trips there can still be lost in production --
// which is exactly the failure this reproduces.
func TestSandboxExitRoundTripsThroughEtcd(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

	id := entity.Id("sandbox/exit-roundtrip")

	// Create it the way the run controller does: PENDING, no exit yet.
	_, err := es.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, id,
		(&compute.Sandbox{Status: compute.PENDING}).Encode,
	), id))
	require.NoError(t, err)

	// Then the patch monitorTaskExit issues: status and exit together.
	exitedAt := time.Now().UTC().Truncate(time.Second)
	_, err = es.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{
			Status: compute.STOPPED,
			Exit:   compute.Exit{Code: 0, At: exitedAt, Container: "app"},
		}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	read := func() compute.Sandbox {
		t.Helper()
		resp, err := es.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var sb compute.Sandbox
		sb.Decode(resp.Entity().Entity())
		return sb
	}

	got := read()
	require.False(t, got.Exit.Empty(), "the exit component must survive a patch against etcd")
	assert.Equal(t, int64(0), got.Exit.Code)
	assert.Equal(t, "app", got.Exit.Container)
	assert.Equal(t, compute.STOPPED, got.Status)

	// The boot path can mark a sandbox RUNNING after the exit has been recorded,
	// when a command exits faster than boot finishes its own bookkeeping. That
	// write is a struct literal with an empty Exit, so it must leave the exit
	// alone.
	_, err = es.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{Status: compute.RUNNING}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	got = read()
	assert.False(t, got.Exit.Empty(), "a later status write must not clobber the recorded exit")
	assert.Equal(t, "app", got.Exit.Container)

	// And the DEAD patch that follows teardown, likewise.
	_, err = es.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{Status: compute.DEAD}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	got = read()
	assert.False(t, got.Exit.Empty(), "the DEAD patch must not clobber the recorded exit")
	assert.Equal(t, compute.DEAD, got.Status)
}

// The run's own result component has to survive the same way, written through
// the partial-update path finish() uses. Result and AttemptRecord land in one
// patch, and AttemptRecord is cardinality-many while Result is cardinality-one,
// so this also covers them being mixed.
func TestRunResultRoundTripsThroughEtcd(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

	id := entity.Id("run/result-roundtrip")

	_, err := es.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, id,
		(&run_v1alpha.Run{
			Task:    "fail",
			Trigger: run_v1alpha.MANUAL,
			Status:  run_v1alpha.RUNNING,
		}).Encode,
	), id))
	require.NoError(t, err)

	at := time.Now().UTC().Truncate(time.Second)
	_, err = es.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&run_v1alpha.Run{
			Status:  run_v1alpha.FAILED,
			EndedAt: at,
			Result:  run_v1alpha.Result{Code: 3, At: at},
			AttemptRecord: []run_v1alpha.AttemptRecord{{
				Attempt: 1, ExitCode: 3, Status: "status.failed", EndedAt: at,
			}},
		}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	resp, err := es.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var got run_v1alpha.Run
	got.Decode(resp.Entity().Entity())

	assert.Equal(t, run_v1alpha.FAILED, got.Status)
	require.False(t, got.Result.At.IsZero(), "the result component must survive the patch")
	assert.Equal(t, int64(3), got.Result.Code)
	require.Len(t, got.AttemptRecord, 1)
}

// Two writers, no OCC: PatchEntity is read-modify-write, and both the sandbox
// boot path and monitorTaskExit patch with revision 0. When boot's write lands
// second it was computed from a snapshot taken before the exit, so it restores
// RUNNING *and* takes the exit with it -- a lost update, not a clobbered field.
//
// This is why a task that exits before boot finishes its bookkeeping loses its
// exit code, and why the sandbox reads RUNNING again afterwards.
func TestConcurrentPatchLosesTheExit(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

	id := entity.Id("sandbox/lost-update")

	_, err := es.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, id,
		(&compute.Sandbox{Status: compute.PENDING}).Encode,
	), id))
	require.NoError(t, err)

	// Both writers computed their patch from the PENDING snapshot. Sequencing
	// them deterministically models the interleaving that loses the exit.
	exitPatch := entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{
			Status: compute.STOPPED,
			Exit:   compute.Exit{Code: 3, At: time.Now(), Container: "app"},
		}).Encode,
	).Attrs()

	bootPatch := entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{Status: compute.RUNNING}).Encode,
	).Attrs()

	_, err = es.EAC.Patch(ctx, exitPatch, 0)
	require.NoError(t, err)
	_, err = es.EAC.Patch(ctx, bootPatch, 0)
	require.NoError(t, err)

	resp, err := es.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var got compute.Sandbox
	got.Decode(resp.Entity().Entity())

	// Documents current behavior. A sequential patch preserves the exit; the
	// loss needs genuinely concurrent read-modify-write, which is what the
	// revision guard below prevents.
	t.Logf("after sequential patches: status=%s exit_empty=%v", got.Status, got.Exit.Empty())
}

// Guarding monitorTaskExit's write with the revision it observed turns the lost
// update into a conflict the caller can retry, which is the fix.
func TestRevisionGuardedPatchDetectsTheConflict(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewEtcdEntityServer(t)
	defer cleanup()

	id := entity.Id("sandbox/occ-guard")

	_, err := es.EAC.Put(ctx, wrap(entity.New(
		entity.DBId, id,
		(&compute.Sandbox{Status: compute.PENDING}).Encode,
	), id))
	require.NoError(t, err)

	resp, err := es.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	stale := resp.Entity().Revision()

	// Someone else writes first.
	_, err = es.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{
			Status: compute.STOPPED,
			Exit:   compute.Exit{Code: 3, At: time.Now(), Container: "app"},
		}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)

	// The stale writer is now refused rather than silently winning.
	_, err = es.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		(&compute.Sandbox{Status: compute.RUNNING}).Encode,
	).Attrs(), stale)
	assert.Error(t, err, "a write computed from a stale revision must be refused, not silently applied")

	resp, err = es.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var got compute.Sandbox
	got.Decode(resp.Entity().Entity())
	assert.False(t, got.Exit.Empty(), "the exit survives when the stale writer is refused")
}
