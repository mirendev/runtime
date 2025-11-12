package disk

import (
	"context"
	"log/slog"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// DiskWatchController watches for disk state changes and triggers reconciliation of dependent leases
type DiskWatchController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// Reference to the disk lease controller to enqueue lease reconciliations
	LeaseController *controller.ReconcileController
}

// NewDiskWatchController creates a new disk watch controller
func NewDiskWatchController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, leaseController *controller.ReconcileController) *DiskWatchController {
	return &DiskWatchController{
		Log:             log.With("module", "disk-watch"),
		EAC:             eac,
		LeaseController: leaseController,
	}
}

// Init initializes the disk watch controller
func (d *DiskWatchController) Init(ctx context.Context) error {
	return nil
}

// Create handles creation of a disk entity (triggers lease reconciliation)
func (d *DiskWatchController) Create(ctx context.Context, disk *storage_v1alpha.Disk, meta *entity.Meta) error {
	// New disk created - reconcile any pending leases waiting for it
	return d.reconcileDependentLeases(ctx, disk.ID)
}

// Update handles updates to a disk entity (triggers lease reconciliation)
func (d *DiskWatchController) Update(ctx context.Context, disk *storage_v1alpha.Disk, meta *entity.Meta) error {
	// Disk state changed - reconcile dependent leases
	d.Log.Debug("Disk state changed, finding dependent leases", "disk", disk.ID)
	return d.reconcileDependentLeases(ctx, disk.ID)
}

// Delete handles deletion of a disk entity
func (d *DiskWatchController) Delete(ctx context.Context, id entity.Id) error {
	// Disk deleted - dependent leases will handle this
	return nil
}

// reconcileDependentLeases finds all leases that reference the disk and triggers their reconciliation
func (d *DiskWatchController) reconcileDependentLeases(ctx context.Context, diskId entity.Id) error {
	// Find all leases that reference this disk using the DiskId index
	listResp, err := d.EAC.List(ctx, entity.Ref(storage_v1alpha.DiskLeaseDiskIdId, diskId))
	if err != nil {
		d.Log.Error("failed to list disk leases for disk change", "disk", diskId, "error", err)
		return nil // Don't fail the disk reconciliation if we can't list leases
	}

	// Reconcile each lease that references this disk
	reconciledCount := 0
	for _, e := range listResp.Values() {
		var lease storage_v1alpha.DiskLease
		lease.Decode(e.Entity())

		d.Log.Debug("Reconciling lease due to disk state change",
			"disk", diskId,
			"lease", lease.ID,
			"leaseStatus", lease.Status)

		// Trigger reconciliation by creating an event with the full entity
		ev := controller.Event{
			Type:   controller.EventUpdated,
			Id:     lease.ID,
			Entity: e.Entity(),
		}

		// Add to the lease reconcile controller's work queue
		d.LeaseController.Enqueue(ev)
		reconciledCount++
	}

	if reconciledCount > 0 {
		d.Log.Info("Triggered reconciliation of leases due to disk state change",
			"disk", diskId,
			"leaseCount", reconciledCount)
	}

	return nil
}
