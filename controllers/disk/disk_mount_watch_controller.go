package disk

import (
	"context"
	"log/slog"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// DiskMountWatchController watches for disk_mount state changes and triggers
// re-reconciliation of the parent disk_lease entity. This bridges the gap where
// the disk lease controller creates a disk_mount and needs to know when the
// mount controller finishes mounting it.
type DiskMountWatchController struct {
	Log    *slog.Logger
	NodeId compute.NodeId

	LeaseController *controller.ReconcileController
}

// NewDiskMountWatchController creates a new disk mount watch controller.
func NewDiskMountWatchController(log *slog.Logger, leaseController *controller.ReconcileController, nodeId compute.NodeId) *DiskMountWatchController {
	return &DiskMountWatchController{
		Log:             log.With("module", "disk-mount-watch"),
		LeaseController: leaseController,
		NodeId:          nodeId,
	}
}

func (m *DiskMountWatchController) Init(ctx context.Context) error {
	return nil
}

func (m *DiskMountWatchController) Create(ctx context.Context, mount *storage_v1alpha.DiskMount, meta *entity.Meta) error {
	return nil
}

func (m *DiskMountWatchController) Update(ctx context.Context, mount *storage_v1alpha.DiskMount, meta *entity.Meta) error {
	// Only process mounts assigned to this node
	if mount.NodeId != "" && !m.NodeId.Matches(mount.NodeId) {
		return nil
	}
	if mount.DiskLeaseId == "" {
		return nil
	}

	m.Log.Debug("disk_mount changed, re-reconciling parent disk lease",
		"mount", mount.ID,
		"lease", mount.DiskLeaseId,
		"actual_state", mount.ActualState)

	m.LeaseController.Enqueue(controller.Event{
		Type: controller.EventUpdated,
		Id:   mount.DiskLeaseId,
	})

	return nil
}

func (m *DiskMountWatchController) Delete(ctx context.Context, id entity.Id, _ *storage_v1alpha.DiskMount) error {
	return nil
}
