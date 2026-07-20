package disk

import (
	"context"
	"log/slog"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// DiskVolumeWatchController watches for disk_volume state changes and triggers
// re-reconciliation of the parent disk entity. This bridges the gap where
// the disk controller creates a disk_volume and needs to know when the
// volume controller finishes provisioning it.
type DiskVolumeWatchController struct {
	Log    *slog.Logger
	EAC    *entityserver_v1alpha.EntityAccessClient
	NodeId compute.NodeId

	DiskController *controller.ReconcileController
}

// NewDiskVolumeWatchController creates a new disk volume watch controller.
func NewDiskVolumeWatchController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, diskController *controller.ReconcileController, nodeId compute.NodeId) *DiskVolumeWatchController {
	return &DiskVolumeWatchController{
		Log:            log.With("module", "disk-volume-watch"),
		EAC:            eac,
		DiskController: diskController,
		NodeId:         nodeId,
	}
}

func (v *DiskVolumeWatchController) Init(ctx context.Context) error {
	return nil
}

func (v *DiskVolumeWatchController) Create(ctx context.Context, vol *storage_v1alpha.DiskVolume, meta *entity.Meta) error {
	return nil
}

func (v *DiskVolumeWatchController) Update(ctx context.Context, vol *storage_v1alpha.DiskVolume, meta *entity.Meta) error {
	if vol.NodeId != "" && !v.NodeId.Matches(vol.NodeId) {
		return nil
	}
	if vol.DiskId == "" {
		return nil
	}

	v.Log.Debug("disk_volume changed, re-reconciling parent disk",
		"volume", vol.ID,
		"disk", vol.DiskId,
		"actual_state", vol.ActualState)

	resp, err := v.EAC.Get(ctx, string(vol.DiskId))
	if err != nil {
		v.Log.Warn("failed to get parent disk for volume change",
			"volume", vol.ID,
			"disk", vol.DiskId,
			"error", err)
		return nil
	}

	v.DiskController.Enqueue(controller.Event{
		Type:   controller.EventUpdated,
		Id:     vol.DiskId,
		Entity: resp.Entity().Entity(),
	})

	return nil
}

func (v *DiskVolumeWatchController) Delete(ctx context.Context, id entity.Id, _ *storage_v1alpha.DiskVolume) error {
	return nil
}
