package disk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestDiskVolumeWatchEnqueuesParentByID(t *testing.T) {
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	parentID := entity.Id("disk/parent")
	inm.AddEntity(entity.New(entity.Ident, parentID))
	target, events := recordingController(t, inm, "disk-target")

	nodeID := compute.NewNodeId("node-a")
	watch := NewDiskVolumeWatchController(
		testutils.TestLogger(t),
		target,
		nodeID,
	)
	require.NoError(t, watch.Update(t.Context(), &storage_v1alpha.DiskVolume{
		ID:     "disk_volume/child",
		DiskId: parentID,
		NodeId: nodeID.Id(),
	}, nil))

	event := receiveEvent(t, events)
	assert.Equal(t, parentID, event.Id)
	assert.NotNil(t, event.Entity, "the target controller should load current parent state")
}

func TestDiskMountWatchEnqueuesParentByID(t *testing.T) {
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	parentID := entity.Id("disk_lease/parent")
	inm.AddEntity(entity.New(entity.Ident, parentID))
	target, events := recordingController(t, inm, "disk-lease-target")

	nodeID := compute.NewNodeId("node-a")
	watch := NewDiskMountWatchController(
		testutils.TestLogger(t),
		target,
		nodeID,
	)
	require.NoError(t, watch.Update(t.Context(), &storage_v1alpha.DiskMount{
		ID:          "disk_mount/child",
		DiskLeaseId: parentID,
		NodeId:      nodeID.Id(),
	}, nil))

	event := receiveEvent(t, events)
	assert.Equal(t, parentID, event.Id)
	assert.NotNil(t, event.Entity, "the target controller should load current parent state")
}

func TestDiskWatchRetriesLeaseDiscoveryFailures(t *testing.T) {
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	inm.Store.OnListIndex = func(context.Context, entity.Attr) ([]entity.Id, error) {
		return nil, errors.New("list unavailable")
	}
	watch := NewDiskWatchController(testutils.TestLogger(t), inm.EAC, nil)

	err := watch.Update(t.Context(), &storage_v1alpha.Disk{ID: "disk/parent"}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "list unavailable")
}

func recordingController(
	t *testing.T,
	inm *testutils.InMemEntityServer,
	name string,
) (*controller.ReconcileController, <-chan controller.Event) {
	t.Helper()

	events := make(chan controller.Event, 1)
	target := controller.NewReconcileController(
		name,
		testutils.TestLogger(t),
		entity.Any(entity.Type, "test/non-matching-target"),
		inm.EAC,
		func(_ context.Context, event controller.Event) ([]entity.Attr, error) {
			events <- event
			return nil, nil
		},
		0,
		1,
	)
	require.NoError(t, target.Start(t.Context()))
	t.Cleanup(target.Stop)
	return target, events
}

func receiveEvent(t *testing.T, events <-chan controller.Event) controller.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for parent reconciliation")
		return controller.Event{}
	}
}
