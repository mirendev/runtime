package disk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/lbdmod"
)

// detectDiskMode determines which disk I/O mode to use.
// The configured parameter is the value from server config (MIREN_DISK_MODE).
// If empty or "auto", the mode is detected from available hardware.
func detectDiskMode(configured string) storage_v1alpha.DiskMode {
	switch configured {
	case "universal":
		return storage_v1alpha.UNIVERSAL
	case "accelerator":
		return storage_v1alpha.ACCELERATOR
	}

	// Auto-detect: use accelerator mode only if the lbd module is actually
	// loaded and drivable. lbdctl on PATH is not enough -- miren installs it
	// alongside the module, so its presence says nothing about whether the
	// module loaded.
	if lbdmod.Available(lbdmod.HostOptions("")) {
		return storage_v1alpha.ACCELERATOR
	}

	return storage_v1alpha.UNIVERSAL
}

// DiskController manages disk entities and their lifecycle.
// It uses disk_volume entities to coordinate volume operations via loop devices.
type DiskController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// NodeId is the ID of this node, used for creating volume entities
	NodeId compute.NodeId

	// isCoordinator is true if this controller runs on the coordinator node.
	// Today all disks are owned by the coordinator, so only the coordinator
	// may create, update, or delete disk_volume entities. When cross-node
	// disk migration lands, shouldManageDisk will gain smarter logic (likely
	// keyed off a target-node field on the disk entity); every caller
	// already routes through it.
	isCoordinator bool

	// Base path for disk mounts (e.g., /var/lib/miren/disks)
	mountBasePath string

	// configuredMode is the disk mode from server config ("", "auto", "universal", "accelerator")
	configuredMode string

	// diskMode determines how disks are provisioned (universal or accelerator)
	diskMode storage_v1alpha.DiskMode
}

// NewDiskController creates a disk controller that uses disk_volume entities.
// The diskMode parameter comes from server config (MIREN_DISK_MODE); pass ""
// for auto-detection. isCoordinator must be true on the primary node and
// false on distributed runners.
func NewDiskController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, nodeId compute.NodeId, diskMode string, isCoordinator bool) *DiskController {
	return &DiskController{
		Log:            log.With("module", "disk"),
		EAC:            eac,
		NodeId:         nodeId,
		isCoordinator:  isCoordinator,
		mountBasePath:  "/var/lib/miren/disks",
		configuredMode: diskMode,
	}
}

// shouldManageDisk reports whether this controller is responsible for
// reconciling the given disk's disk_volume lifecycle. Currently this is
// coordinator-only; when cross-node migration lands, this will look at
// the disk's target node.
func (d *DiskController) shouldManageDisk(disk *storage_v1alpha.Disk) bool {
	_ = disk
	return d.isCoordinator
}

// ForceUniversalMode forces the controller to use disk_volume entities with
// loop devices. This is used by integration tests.
func (d *DiskController) ForceUniversalMode() {
	d.diskMode = storage_v1alpha.UNIVERSAL
}

// Init initializes the disk controller
func (d *DiskController) Init(ctx context.Context) error {
	d.diskMode = detectDiskMode(d.configuredMode)
	d.Log.Info("disk controller initialized", "mode", d.diskMode)
	return nil
}

// Create handles creation of a new disk entity
func (d *DiskController) Create(ctx context.Context, disk *storage_v1alpha.Disk, meta *entity.Meta) error {
	d.Log.Info("Processing disk creation",
		"disk", disk.ID,
		"status", disk.Status)

	return d.reconcileDisk(ctx, disk, meta)
}

// Update handles updates to an existing disk entity
func (d *DiskController) Update(ctx context.Context, disk *storage_v1alpha.Disk, meta *entity.Meta) error {
	// Debug rather than Info: this fires on every reconcile of every disk,
	// including the periodic resync, so a steady-state cluster emits it
	// constantly while reporting no change. The handlers below log at Info when
	// they actually do something, which is the part worth seeing.
	d.Log.Debug("Processing disk update",
		"disk", disk.ID,
		"status", disk.Status)

	return d.reconcileDisk(ctx, disk, meta)
}

// Delete handles deletion of a disk entity
func (d *DiskController) Delete(ctx context.Context, id entity.Id, obj *storage_v1alpha.Disk) error {
	d.Log.Info("Processing disk deletion", "disk", id)
	// Deletion is handled through the DELETING status in reconcileDisk
	return nil
}

// reconcileDisk reconciles the disk state
func (d *DiskController) reconcileDisk(ctx context.Context, disk *storage_v1alpha.Disk, meta *entity.Meta) error {
	if !d.shouldManageDisk(disk) {
		return nil
	}

	var err error

	switch disk.Status {
	case storage_v1alpha.PROVISIONED:
		err = d.handleProvisioned(ctx, disk)
	case storage_v1alpha.PROVISIONING:
		err = d.handleProvisioning(ctx, disk)
	case storage_v1alpha.DELETING:
		err = d.handleDeletion(ctx, disk)
	case storage_v1alpha.ATTACHED, storage_v1alpha.DETACHED:
		// These states are managed by disk lease controller
		return nil
	case storage_v1alpha.RESTORING:
		// Disk is being restored externally; ignore until restore completes
		return nil
	case storage_v1alpha.ERROR:
		// Error state is terminal, no action needed
		return nil
	default:
		// Unknown status, log warning
		d.Log.Warn("Unknown disk status", "disk", disk.ID, "status", disk.Status)
		return nil
	}

	if err != nil {
		return err
	}

	// Update entity attributes if any changes
	if meta != nil {
		// Ensure meta.Entity is initialized
		if meta.Entity == nil {
			meta.Entity = entity.New(disk.Encode())
		} else {
			// Caller does a diff so we can always send it back
			meta.Entity.Update(disk.Encode())
		}
	}

	return nil
}

// handleProvisioning provisions a new disk volume using disk_volume entities
func (d *DiskController) handleProvisioning(ctx context.Context, disk *storage_v1alpha.Disk) error {
	// Check if a disk_volume entity already exists for this disk
	existingVolume, err := d.getDiskVolumeForDisk(ctx, disk.ID)
	if err != nil {
		return fmt.Errorf("error looking up existing disk_volume for disk %s: %w", disk.ID, err)
	}

	myNodeId := d.NodeId.Id()

	if existingVolume != nil && existingVolume.NodeId != "" && !d.NodeId.Matches(existingVolume.NodeId) {
		// Orphan from a runner that created a volume it shouldn't have. Log
		// and fall through to create our own native volume; the orphan is
		// harmless here and will be cleaned up via DELETING (or left to the
		// future migration-aware controller).
		d.Log.Warn("ignoring foreign disk_volume for coordinator-owned disk",
			"disk", disk.ID,
			"disk_volume", existingVolume.ID,
			"volume_node", existingVolume.NodeId,
			"my_node", myNodeId)
		existingVolume = nil
	}

	if existingVolume != nil {
		d.Log.Debug("found existing disk_volume for disk",
			"disk", disk.ID,
			"disk_volume", existingVolume.ID,
			"actual_state", existingVolume.ActualState,
			"volume_id", existingVolume.VolumeId)

		switch existingVolume.ActualState {
		case storage_v1alpha.DV_READY:
			disk.Status = storage_v1alpha.PROVISIONED
			disk.VolumeId = existingVolume.VolumeId
			disk.Mode = d.diskMode
			d.Log.Info("disk provisioned via disk_volume entity",
				"disk", disk.ID,
				"volume_id", existingVolume.VolumeId)
			return nil

		case storage_v1alpha.DV_ERROR:
			d.Log.Warn("disk_volume in error state",
				"disk", disk.ID,
				"disk_volume", existingVolume.ID,
				"error", existingVolume.ErrorMessage)
			return nil

		case storage_v1alpha.DV_PENDING, storage_v1alpha.DV_CREATING, storage_v1alpha.DV_DELETING, storage_v1alpha.DV_DELETED:
			// Not yet ready; wait for the volume to settle.
			fallthrough
		default:
			d.Log.Debug("disk_volume still provisioning",
				"disk", disk.ID,
				"disk_volume", existingVolume.ID,
				"actual_state", existingVolume.ActualState)
			return nil
		}
	}

	// Create new disk_volume entity
	filesystem := strings.TrimPrefix(string(disk.Filesystem), "filesystem.")

	diskVolume := &storage_v1alpha.DiskVolume{
		Name:         disk.Name,
		DiskId:       disk.ID,
		SizeGb:       disk.SizeGb,
		Filesystem:   filesystem,
		VolumeMode:   diskModeToVolumeMode(d.diskMode),
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
		NodeId:       myNodeId,
	}

	volumeId := idgen.GenNS("disk-vol")

	d.Log.Info("creating disk_volume entity",
		"disk", disk.ID,
		"volume_id", volumeId,
		"size_gb", disk.SizeGb,
		"filesystem", filesystem,
		"node_id", d.NodeId)

	createAttrs := entity.New(
		entity.DBId, entity.Id("disk_volume/"+volumeId),
		diskVolume.Encode,
	).Attrs()

	_, err = d.EAC.Create(ctx, createAttrs)
	if err != nil {
		return fmt.Errorf("failed to create disk_volume entity: %w", err)
	}

	disk.Mode = d.diskMode

	d.Log.Info("created disk_volume entity, waiting for provisioning",
		"disk", disk.ID)

	return nil
}

// handleProvisioned verifies a provisioned disk has a ready volume entity
func (d *DiskController) handleProvisioned(ctx context.Context, disk *storage_v1alpha.Disk) error {
	if disk.VolumeId == "" {
		d.Log.Warn("provisioned disk has no volume ID, re-provisioning", "disk", disk.ID)
		disk.Status = storage_v1alpha.PROVISIONING
		return d.handleProvisioning(ctx, disk)
	}

	volume, err := d.getDiskVolumeForDisk(ctx, disk.ID)
	if err != nil {
		return fmt.Errorf("error looking up disk_volume for provisioned disk %s: %w", disk.ID, err)
	}

	if volume == nil {
		d.Log.Info("provisioned disk has no disk_volume entity, creating one",
			"disk", disk.ID,
			"volume_id", disk.VolumeId)
		disk.VolumeId = ""
		disk.Status = storage_v1alpha.PROVISIONING
		return d.handleProvisioning(ctx, disk)
	}

	if volume.ActualState != storage_v1alpha.DV_READY {
		d.Log.Warn("disk_volume not ready for provisioned disk",
			"disk", disk.ID,
			"disk_volume", volume.ID,
			"actual_state", volume.ActualState)
		disk.Status = storage_v1alpha.PROVISIONING
		disk.VolumeId = ""
		return nil
	}

	return nil
}

// handleDeletion sets desired_state=absent on the volume entity
func (d *DiskController) handleDeletion(ctx context.Context, disk *storage_v1alpha.Disk) error {
	volume, err := d.getDiskVolumeForDisk(ctx, disk.ID)
	if err != nil {
		d.Log.Warn("error looking up disk_volume for deletion",
			"disk", disk.ID,
			"error", err)
		return err
	}

	if volume != nil {
		if volume.ActualState == storage_v1alpha.DV_DELETED {
			d.Log.Info("disk_volume already deleted, cleaning up disk",
				"disk", disk.ID,
				"disk_volume", volume.ID)

			if _, err := d.EAC.Delete(ctx, volume.ID.String()); err != nil {
				d.Log.Warn("failed to delete disk_volume entity",
					"disk_volume", volume.ID,
					"error", err)
				return err
			}
		} else if volume.DesiredState != storage_v1alpha.DV_ABSENT {
			d.Log.Info("setting disk_volume desired_state to absent",
				"disk", disk.ID,
				"disk_volume", volume.ID)

			updateAttrs := []entity.Attr{
				entity.Ref(entity.DBId, volume.ID),
				entity.Ref(storage_v1alpha.DiskVolumeDesiredStateId, storage_v1alpha.DiskVolumeDesiredStateDvAbsentId),
			}
			if _, err := d.EAC.Patch(ctx, updateAttrs, 0); err != nil {
				d.Log.Error("failed to update disk_volume desired_state",
					"disk_volume", volume.ID,
					"error", err)
				return err
			}

			return nil
		} else {
			d.Log.Debug("disk_volume already marked for deletion",
				"disk", disk.ID,
				"disk_volume", volume.ID,
				"actual_state", volume.ActualState)
			return nil
		}
	}

	// No disk_volume or it's been deleted - delete the disk entity
	if d.EAC != nil {
		if _, err := d.EAC.Delete(ctx, disk.ID.String()); err != nil {
			d.Log.Error("failed to delete disk entity", "disk", disk.ID, "error", err)
			return err
		}
	}

	return nil
}

// getDiskVolumeForDisk finds the disk_volume entity for a disk. When more
// than one exists (e.g. an orphan left behind by a past violation of the
// primary-only invariant, or — in the future — a volume belonging to a
// migration peer), prefer the one owned by this controller's node so the
// controller reconciles its own volume and ignores foreign ones.
func (d *DiskController) getDiskVolumeForDisk(ctx context.Context, diskId entity.Id) (*storage_v1alpha.DiskVolume, error) {
	if d.EAC == nil {
		return nil, nil
	}

	indexAttr := entity.Ref(storage_v1alpha.DiskVolumeDiskIdId, diskId)

	resp, err := d.EAC.List(ctx, indexAttr)
	if err != nil {
		return nil, fmt.Errorf("failed to list disk_volume entities: %w", err)
	}

	values := resp.Values()
	if len(values) == 0 {
		return nil, nil
	}

	var chosen *storage_v1alpha.DiskVolume
	for _, v := range values {
		var volume storage_v1alpha.DiskVolume
		volume.Decode(v.Entity())

		if d.NodeId.Matches(volume.NodeId) {
			return &volume, nil
		}

		if chosen == nil {
			vol := volume
			chosen = &vol
		}
	}

	return chosen, nil
}

func diskModeToVolumeMode(mode storage_v1alpha.DiskMode) storage_v1alpha.DiskVolumeVolumeMode {
	switch mode {
	case storage_v1alpha.ACCELERATOR:
		return storage_v1alpha.VM_ACCELERATOR
	case storage_v1alpha.UNIVERSAL:
		// Universal is also the default for an unspecified mode.
		fallthrough
	default:
		return storage_v1alpha.VM_UNIVERSAL
	}
}

// Close gracefully shuts down the disk controller
func (d *DiskController) Close() error {
	d.Log.Info("Shutting down disk controller")
	return nil
}

// Start starts the disk controller
func (d *DiskController) Start(ctx context.Context) error {
	// Create reconcile controller using AdaptController
	rc := controller.NewReconcileController(
		"disk",
		d.Log,
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk),
		d.EAC,
		controller.AdaptController(d),
		0, // No resync period
		1, // Single worker for now
	)

	return rc.Start(ctx)
}
