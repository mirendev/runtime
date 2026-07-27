package diskio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/units"
)

// alwaysMount returns true if volumes with this mode should be mounted
// at creation time and stay mounted regardless of lease lifecycle.
func alwaysMount(mode storage_v1alpha.DiskVolumeVolumeMode) bool {
	return mode == storage_v1alpha.VM_UNIVERSAL
}

// DiskVolumeController watches disk_volume entities and manages sparse disk images
// using loop devices.
type DiskVolumeController struct {
	log      *slog.Logger
	dataPath string
	nodeId   compute.NodeId
	eac      *entityserver_v1alpha.EntityAccessClient
	state    *State
	ops      DiskVolumeOps
	mntOps   DiskMountOps

	// keepMounts, when true, causes Shutdown to skip unmounting volumes.
	// Set during reload (SIGUSR2) so the new process can pick them up.
	keepMounts bool

	// orphanSweepDone ensures the boot-time orphan kernel state
	// reconciliation runs at most once per controller lifetime.
	orphanSweepDone bool
}

func NewDiskVolumeController(log *slog.Logger, dataPath string, nodeId compute.NodeId, state *State, ops DiskVolumeOps, mntOps DiskMountOps) *DiskVolumeController {
	return &DiskVolumeController{
		log:      log.With("module", "disk-volume"),
		dataPath: dataPath,
		nodeId:   nodeId,
		state:    state,
		ops:      ops,
		mntOps:   mntOps,
	}
}

func (c *DiskVolumeController) SetEAC(eac *entityserver_v1alpha.EntityAccessClient) {
	c.eac = eac
}

// SetKeepMounts tells the controller to skip unmounting during Shutdown.
// Used during reload so the replacement process inherits the mounts.
func (c *DiskVolumeController) SetKeepMounts(v bool) {
	c.keepMounts = v
}

func (c *DiskVolumeController) Init(ctx context.Context) error {
	return nil
}

func (c *DiskVolumeController) Reconcile(ctx context.Context, volume *storage_v1alpha.DiskVolume, meta *entity.Meta) error {
	return c.reconcileVolume(ctx, volume)
}

func (c *DiskVolumeController) Index() entity.Attr {
	return entity.Ref(storage_v1alpha.DiskVolumeNodeIdId, c.nodeId.Id())
}

func (c *DiskVolumeController) reconcileVolume(ctx context.Context, volume *storage_v1alpha.DiskVolume) error {
	entityId := string(volume.ID)
	c.log.Info("reconciling disk volume",
		"entity_id", entityId,
		"desired_state", volume.DesiredState,
		"actual_state", volume.ActualState,
	)

	switch volume.DesiredState {
	case storage_v1alpha.DV_PRESENT:
		return c.reconcileVolumePresent(ctx, volume)
	case storage_v1alpha.DV_ABSENT:
		return c.reconcileVolumeAbsent(ctx, volume)
	default:
		c.log.Warn("unknown desired state", "desired_state", volume.DesiredState)
		return nil
	}
}

func (c *DiskVolumeController) reconcileVolumePresent(ctx context.Context, volume *storage_v1alpha.DiskVolume) error {
	entityId := string(volume.ID)

	// Check if volume already exists in our state
	if existing := c.state.GetVolume(entityId); existing != nil {
		if volume.ActualState == storage_v1alpha.DV_READY {
			if existing.DiskPath != "" && !c.ops.VolumePathExists(existing.DiskPath) {
				c.log.Warn("volume directory missing, setting error state",
					"entity_id", entityId,
					"disk_path", existing.DiskPath,
				)
				c.setVolumeError(ctx, volume.ID, "volume directory missing")
				return fmt.Errorf("volume directory missing: %s", existing.DiskPath)
			}
			// For alwaysMount volumes, verify the mount is present
			if alwaysMount(existing.Mode) && (!existing.Mounted || !c.mntOps.IsMounted(existing.MountPath)) {
				c.log.Info("volume ready but not mounted, re-mounting", "entity_id", entityId)
				if err := c.ensureVolumeMount(ctx, entityId, existing); err != nil {
					c.log.Warn("failed to re-mount volume", "entity_id", entityId, "error", err)
					return err
				}
			}
			c.log.Debug("volume already ready", "entity_id", entityId)
			return nil
		}
		// Persisted state has a disk path and it exists on disk — reconcile entity
		if existing.DiskPath != "" && c.ops.VolumePathExists(existing.DiskPath) {
			c.log.Info("found persisted volume on disk, reconciling entity state",
				"entity_id", entityId,
				"disk_path", existing.DiskPath,
			)
			if err := c.updateVolumeState(ctx, volume.ID, storage_v1alpha.DV_READY, existing.VolumeId, ""); err != nil {
				c.log.Warn("failed to update volume state from persisted volume", "entity_id", entityId, "error", err)
			}
			// Ensure mount for alwaysMount volumes
			if alwaysMount(existing.Mode) {
				if err := c.ensureVolumeMount(ctx, entityId, existing); err != nil {
					c.log.Warn("failed to mount persisted volume", "entity_id", entityId, "error", err)
					return err
				}
			}
			return nil
		}
	}

	switch volume.ActualState {
	case storage_v1alpha.DV_PENDING:
		return c.createVolume(ctx, volume)
	case storage_v1alpha.DV_CREATING:
		c.log.Debug("volume is being created", "entity_id", entityId)
		return nil
	case storage_v1alpha.DV_READY:
		c.log.Warn("entity says DV_READY but no local state found, recovering", "entity_id", entityId)
		volumePath := c.getVolumePath(entityId)
		if !c.ops.VolumePathExists(volumePath) {
			c.log.Warn("volume directory missing despite DV_READY, resetting to pending", "entity_id", entityId)
			return c.createVolume(ctx, volume)
		}
		volumeId := volume.MountId
		if volumeId == "" {
			volumeId = entityId
			if idx := strings.LastIndex(entityId, "/"); idx != -1 {
				volumeId = entityId[idx+1:]
			}
		}
		volState := &VolumeState{
			EntityId:   entityId,
			VolumeId:   volumeId,
			Name:       volume.Name,
			DiskPath:   volumePath,
			SizeBytes:  units.GigaBytes(volume.SizeGb).Bytes().Int64(),
			Filesystem: volume.Filesystem,
			Mode:       volume.VolumeMode,
		}
		c.state.SetVolume(entityId, volState)
		if err := c.state.Save(); err != nil {
			c.log.Warn("failed to save recovered volume state", "error", err)
		}
		c.log.Info("recovered volume state from entity", "entity_id", entityId)
		// Mount the recovered volume if it's an alwaysMount volume
		if alwaysMount(volume.VolumeMode) {
			if err := c.ensureVolumeMount(ctx, entityId, volState); err != nil {
				c.log.Warn("failed to mount recovered volume", "entity_id", entityId, "error", err)
				return err
			}
		}
		return nil
	case storage_v1alpha.DV_ERROR:
		c.log.Info("volume in error state, attempting recreation", "entity_id", entityId)
		return c.createVolume(ctx, volume)
	case storage_v1alpha.DV_DELETING, storage_v1alpha.DV_DELETED:
		// Volume is being torn down while desired state is present; unexpected.
		fallthrough
	default:
		c.log.Warn("unexpected actual state for present volume", "actual_state", volume.ActualState)
		return nil
	}
}

func (c *DiskVolumeController) reconcileVolumeAbsent(ctx context.Context, volume *storage_v1alpha.DiskVolume) error {
	entityId := string(volume.ID)

	switch volume.ActualState {
	case storage_v1alpha.DV_DELETED:
		volState := c.state.GetVolume(entityId)
		if volState != nil && volState.DiskPath != "" && c.ops.VolumePathExists(volState.DiskPath) {
			c.log.Info("cleaning up local volume data", "entity_id", entityId, "disk_path", volState.DiskPath)
			if err := c.ops.RemoveVolumeDir(volState.DiskPath); err != nil {
				c.log.Warn("failed to remove volume directory", "entity_id", entityId, "error", err)
			}
		}
		c.state.DeleteVolume(entityId)
		if err := c.state.Save(); err != nil {
			c.log.Warn("failed to save state after volume deletion", "error", err)
		}
		return nil
	case storage_v1alpha.DV_DELETING:
		return nil
	case storage_v1alpha.DV_PENDING, storage_v1alpha.DV_CREATING, storage_v1alpha.DV_READY, storage_v1alpha.DV_ERROR:
		// Volume still exists; delete it to reach the absent state.
		fallthrough
	default:
		return c.deleteVolume(ctx, volume)
	}
}

func (c *DiskVolumeController) createVolume(ctx context.Context, volume *storage_v1alpha.DiskVolume) error {
	entityId := string(volume.ID)

	c.log.Info("creating disk volume",
		"entity_id", entityId,
		"size_gb", volume.SizeGb,
		"filesystem", volume.Filesystem,
	)

	if err := c.updateVolumeState(ctx, volume.ID, storage_v1alpha.DV_CREATING, "", ""); err != nil {
		c.log.Warn("failed to update volume state to creating", "error", err)
	}

	// Create volume directory
	volumePath := c.getVolumePath(entityId)
	if err := c.ops.CreateVolumeDir(volumePath); err != nil {
		c.setVolumeError(ctx, volume.ID, fmt.Sprintf("failed to create volume directory: %v", err))
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	// Create sparse disk image. Skip when one is already present: a restored
	// volume (moved back into place by `disk undelete`) already carries its
	// disk.img, and reimaging it here would truncate away the recovered data.
	// Volume entity IDs are unique, so a genuine fresh create never collides
	// with an existing image — only the restore path lands here idempotently.
	imagePath := filepath.Join(volumePath, "disk.img")
	sizeBytes := units.GigaBytes(volume.SizeGb).Bytes().Int64()

	if c.ops.VolumePathExists(imagePath) {
		c.log.Info("disk image already present, preserving it (restore/recovery)",
			"entity_id", entityId,
			"image_path", imagePath,
		)
	} else if err := c.ops.CreateDiskImage(imagePath, sizeBytes); err != nil {
		c.setVolumeError(ctx, volume.ID, fmt.Sprintf("failed to create disk image: %v", err))
		return fmt.Errorf("failed to create disk image: %w", err)
	}

	// Create log directory for accelerator volumes
	if volume.VolumeMode == storage_v1alpha.VM_ACCELERATOR {
		logDir := filepath.Join(volumePath, "logs")
		if err := c.ops.CreateVolumeDir(logDir); err != nil {
			c.setVolumeError(ctx, volume.ID, fmt.Sprintf("failed to create log directory: %v", err))
			return fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	// Use MountId if set, otherwise entity suffix
	volumeId := volume.MountId
	if volumeId == "" {
		volumeId = entityId
		if idx := strings.LastIndex(entityId, "/"); idx != -1 {
			volumeId = entityId[idx+1:]
		}
	}

	// Update state
	volState := &VolumeState{
		EntityId:   entityId,
		VolumeId:   volumeId,
		Name:       volume.Name,
		DiskPath:   volumePath,
		SizeBytes:  sizeBytes,
		Filesystem: volume.Filesystem,
		Mode:       volume.VolumeMode,
	}
	c.state.SetVolume(entityId, volState)

	if err := c.state.Save(); err != nil {
		c.log.Warn("failed to save state after volume creation", "error", err)
	}

	// For alwaysMount volumes, attach and mount immediately
	if alwaysMount(volume.VolumeMode) {
		if err := c.ensureVolumeMount(ctx, entityId, volState); err != nil {
			c.setVolumeError(ctx, volume.ID, fmt.Sprintf("failed to mount volume: %v", err))
			return fmt.Errorf("failed to mount volume: %w", err)
		}
	}

	c.log.Info("disk volume created",
		"entity_id", entityId,
		"volume_id", volumeId,
		"image_path", imagePath,
	)

	if err := c.updateVolumeState(ctx, volume.ID, storage_v1alpha.DV_READY, volumeId, ""); err != nil {
		c.log.Warn("failed to update volume state to ready", "error", err)
	}

	// Also update the image_path in the entity
	if c.eac != nil {
		attrs := []entity.Attr{
			entity.Ref(entity.DBId, volume.ID),
			entity.String(storage_v1alpha.DiskVolumeImagePathId, imagePath),
		}
		if _, err := c.eac.Patch(ctx, attrs, 0); err != nil {
			c.log.Warn("failed to update image_path in entity", "error", err)
		}
	}

	return nil
}

func (c *DiskVolumeController) deleteVolume(ctx context.Context, volume *storage_v1alpha.DiskVolume) error {
	entityId := string(volume.ID)

	c.log.Info("deleting disk volume", "entity_id", entityId)

	if err := c.updateVolumeState(ctx, volume.ID, storage_v1alpha.DV_DELETING, "", ""); err != nil {
		c.log.Warn("failed to update volume state to deleting", "error", err)
	}

	volState := c.state.GetVolume(entityId)
	if volState == nil {
		c.log.Warn("volume not found in state", "entity_id", entityId)
		if err := c.updateVolumeState(ctx, volume.ID, storage_v1alpha.DV_DELETED, "", ""); err != nil {
			c.log.Warn("failed to update volume state to deleted", "error", err)
		}
		return nil
	}

	// Unmount before deleting if alwaysMount
	if alwaysMount(volState.Mode) && volState.Mounted {
		c.unmountVolume(volState)
	}

	if volState.DiskPath != "" {
		if err := c.softDeleteVolume(ctx, volume, volState); err != nil {
			c.log.Warn("soft-delete failed, falling back to hard delete",
				"entity_id", entityId, "error", err)
			if err := c.ops.RemoveVolumeDir(volState.DiskPath); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("failed to remove volume directory %s: %w", volState.DiskPath, err)
				}
			}
		}
	}

	c.state.DeleteVolume(entityId)
	if err := c.state.Save(); err != nil {
		c.log.Warn("failed to save state after volume deletion", "error", err)
	}

	c.log.Info("disk volume deleted", "entity_id", entityId)

	if err := c.updateVolumeState(ctx, volume.ID, storage_v1alpha.DV_DELETED, "", ""); err != nil {
		c.log.Warn("failed to update volume state to deleted", "error", err)
	}

	return nil
}

// softDeleteVolume moves the volume directory to the deleted-volumes holding area
// and writes metadata so it can be restored later. Metadata is written into the
// source directory before the move so the rename carries both data and metadata
// atomically.
func (c *DiskVolumeController) softDeleteVolume(ctx context.Context, volume *storage_v1alpha.DiskVolume, volState *VolumeState) error {
	diskPath := volState.DiskPath
	dirName := filepath.Base(diskPath)
	destPath := filepath.Join(c.dataPath, deletedVolumesDir, dirName)

	// Avoid collision if the destination already exists (e.g., from a prior failed attempt)
	if _, err := os.Stat(destPath); err == nil {
		destPath = fmt.Sprintf("%s-%d", destPath, time.Now().UnixMilli())
	}

	meta := &DeletedVolumeMetadata{
		DiskID:     string(volume.DiskId),
		DiskName:   volume.Name,
		SizeGb:     volume.SizeGb,
		Filesystem: volume.Filesystem,
		VolumeID:   volState.VolumeId,
		VolumeMode: string(volume.VolumeMode),
		NodeID:     compute.NewNodeId(string(volume.NodeId)),
		DeletedAt:  time.Now(),
	}

	// Try to enrich metadata from the disk entity
	if c.eac != nil && volume.DiskId != "" {
		result, err := c.eac.Get(ctx, string(volume.DiskId))
		if err == nil && result.Entity() != nil {
			var disk storage_v1alpha.Disk
			disk.Decode(result.Entity().Entity())
			meta.DiskName = disk.Name
			meta.CreatedBy = string(disk.CreatedBy)
		}
	}

	// Write metadata before the move so the rename carries both the volume
	// data and the metadata atomically (same filesystem).  If the write
	// fails we abort — moving without metadata would leave an invisible,
	// un-restorable directory.
	if err := SaveDeletedVolumeMetadata(diskPath, meta); err != nil {
		return fmt.Errorf("writing deleted volume metadata: %w", err)
	}

	if err := c.ops.MoveVolumeDir(diskPath, destPath); err != nil {
		_ = os.Remove(filepath.Join(diskPath, metadataFilename))
		return fmt.Errorf("moving volume to deleted-volumes: %w", err)
	}

	c.log.Info("volume soft-deleted",
		"from", diskPath,
		"to", destPath,
		"disk_name", meta.DiskName)

	return nil
}

func (c *DiskVolumeController) getVolumePath(volumeEntityId string) string {
	dirName := volumeEntityId
	if idx := strings.LastIndex(volumeEntityId, "/"); idx != -1 {
		dirName = volumeEntityId[idx+1:]
	}
	return filepath.Join(c.dataPath, "volumes", dirName)
}

func diskVolumeActualStateToId(state storage_v1alpha.DiskVolumeActualState) entity.Id {
	switch state {
	case storage_v1alpha.DV_PENDING:
		return storage_v1alpha.DiskVolumeActualStateDvPendingId
	case storage_v1alpha.DV_CREATING:
		return storage_v1alpha.DiskVolumeActualStateDvCreatingId
	case storage_v1alpha.DV_READY:
		return storage_v1alpha.DiskVolumeActualStateDvReadyId
	case storage_v1alpha.DV_DELETING:
		return storage_v1alpha.DiskVolumeActualStateDvDeletingId
	case storage_v1alpha.DV_DELETED:
		return storage_v1alpha.DiskVolumeActualStateDvDeletedId
	case storage_v1alpha.DV_ERROR:
		return storage_v1alpha.DiskVolumeActualStateDvErrorId
	default:
		return storage_v1alpha.DiskVolumeActualStateDvPendingId
	}
}

func (c *DiskVolumeController) updateVolumeState(ctx context.Context, id entity.Id, state storage_v1alpha.DiskVolumeActualState, volumeId, errorMsg string) error {
	if c.eac == nil {
		return nil
	}

	stateId := diskVolumeActualStateToId(state)

	attrs := []entity.Attr{
		entity.Ref(entity.DBId, id),
		entity.Ref(storage_v1alpha.DiskVolumeActualStateId, stateId),
	}

	if volumeId != "" {
		attrs = append(attrs, entity.String(storage_v1alpha.DiskVolumeVolumeIdId, volumeId))
	}

	attrs = append(attrs, entity.String(storage_v1alpha.DiskVolumeErrorMessageId, errorMsg))

	_, err := c.eac.Patch(ctx, attrs, 0)
	return err
}

func (c *DiskVolumeController) setVolumeError(ctx context.Context, id entity.Id, errorMsg string) {
	if err := c.updateVolumeState(ctx, id, storage_v1alpha.DV_ERROR, "", errorMsg); err != nil {
		c.log.Warn("failed to set volume error state", "entity_id", id, "error", err)
	}
}

// diskMountBasePath is the standard path where disk volumes are mounted,
// matching the path used by the DiskLeaseController.
const diskMountBasePath = "/var/lib/miren/disks"

// getMountPath returns the mount path for a volume.
func (c *DiskVolumeController) getMountPath(volumeId string) string {
	return filepath.Join(diskMountBasePath, volumeId)
}

// ensureVolumeMount loop-attaches, formats if needed, and mounts a volume.
// It updates the VolumeState with mount info and persists state.
// If the volume is already mounted, this is a no-op.
func (c *DiskVolumeController) ensureVolumeMount(ctx context.Context, entityId string, volState *VolumeState) error {
	mountPath := c.getMountPath(volState.VolumeId)

	// Already mounted — nothing to do
	if c.mntOps.IsMounted(mountPath) {
		if !volState.Mounted || volState.MountPath != mountPath {
			volState.MountPath = mountPath
			volState.Mounted = true
			c.state.SetVolume(entityId, volState)
			c.state.Save()
		}
		return nil
	}

	imagePath := filepath.Join(volState.DiskPath, "disk.img")

	c.log.Info("mounting volume",
		"entity_id", entityId,
		"image_path", imagePath,
		"mount_path", mountPath,
	)

	// Check whether this backing file is already attached to a loop device
	// in the kernel. If it is — e.g. left over from a SIGKILL'd miren whose
	// container kept holding the old loop open — adopt that device rather
	// than detach and re-attach. Detaching a live loop device and then
	// attaching the same backing file to a new one produces two loop
	// devices with incoherent page caches, which corrupts the filesystem.
	// Adopting the existing device sidesteps the problem entirely: the
	// kernel state is already consistent, we just need to (re)mount on
	// our target path. We now own it — rollback cleanup on failure will
	// detach it like any other device we created.
	var devicePath string
	existing, findErr := c.mntOps.FindLoopByBacking(imagePath)
	if findErr != nil {
		// Fail closed: if we can't see the kernel's loop state, we
		// can't tell whether attaching would double-attach. Return a
		// retriable error so the next reconcile tick tries again once
		// sysfs is healthy.
		return fmt.Errorf("find loop device for backing file %s: %w", imagePath, findErr)
	}
	if existing != "" {
		c.log.Info("adopting existing loop device for backing file",
			"entity_id", entityId,
			"image_path", imagePath,
			"device", existing,
		)
		devicePath = existing
	} else {
		// No existing loop. Any stale volState.DevicePath is meaningless
		// (the kernel has no loop backing this image), so we don't touch
		// it — the loop index it names may have been reallocated to some
		// other volume in the meantime.
		volState.DevicePath = ""

		var err error
		devicePath, err = c.mntOps.LoopAttach(imagePath)
		if err != nil {
			return fmt.Errorf("failed to attach loop device: %w", err)
		}
	}

	// rollbackDetach releases the loop device, logging any failure
	// rather than silently discarding it so unclean cleanup is visible.
	rollbackDetach := func(reason string) {
		if derr := c.mntOps.LoopDetach(devicePath); derr != nil {
			c.log.Warn("rollback: failed to detach loop device",
				"entity_id", entityId, "device", devicePath, "reason", reason, "error", derr)
		}
	}

	filesystem := volState.Filesystem
	if filesystem == "" {
		filesystem = "ext4"
	}

	formatted, err := c.mntOps.IsFormatted(ctx, devicePath, filesystem)
	if err != nil {
		rollbackDetach("IsFormatted failed")
		return fmt.Errorf("failed to check if formatted: %w", err)
	}

	if !formatted {
		c.log.Info("formatting device", "device", devicePath, "filesystem", filesystem)
		formatDeadline := time.Now().Add(1 * time.Minute)
		backoff := 1 * time.Second
		maxBackoff := 10 * time.Second

		for {
			err := c.mntOps.FormatDevice(ctx, devicePath, filesystem)
			if err == nil {
				break
			}

			c.log.Error("format device failed, will retry", "device", devicePath, "error", err)

			if time.Now().After(formatDeadline) {
				rollbackDetach("format device retries exhausted")
				return fmt.Errorf("failed to format device after retries: %w", err)
			}

			select {
			case <-ctx.Done():
				rollbackDetach("context canceled during format")
				return ctx.Err()
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}

	if err := c.mntOps.CreateDir(mountPath, 0755); err != nil {
		rollbackDetach("CreateDir failed")
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	if err := mountWithFsckRetry(ctx, c.log, c.mntOps, devicePath, mountPath, filesystem, false); err != nil {
		rollbackDetach("Mount failed")
		return fmt.Errorf("failed to mount: %w", err)
	}

	// Update state with mount info
	volState.DevicePath = devicePath
	volState.MountPath = mountPath
	volState.Mounted = true
	c.state.SetVolume(entityId, volState)

	if err := c.state.Save(); err != nil {
		c.log.Warn("failed to save state after volume mount", "error", err)
	}

	c.log.Info("volume mounted",
		"entity_id", entityId,
		"device", devicePath,
		"mount_path", mountPath,
	)

	return nil
}

// unmountVolume unmounts and detaches a loop device for an alwaysMount volume.
func (c *DiskVolumeController) unmountVolume(volState *VolumeState) {
	if volState.MountPath != "" && c.mntOps.IsMounted(volState.MountPath) {
		if err := c.mntOps.Unmount(volState.MountPath); err != nil {
			c.log.Warn("failed to unmount volume", "entity_id", volState.EntityId, "error", err)
		}
	}
	if volState.DevicePath != "" {
		if err := c.mntOps.LoopDetach(volState.DevicePath); err != nil {
			c.log.Warn("failed to detach loop device", "entity_id", volState.EntityId, "error", err)
		}
	}
	volState.Mounted = false
	volState.DevicePath = ""
	volState.MountPath = ""
}

// Shutdown unmounts all disk volumes and detaches their backing devices.
// It uses the actual kernel mount table rather than trusting persisted state,
// finding all mounts under diskMountBasePath and tearing them down.
// If keepMounts is set (reload), everything is left in place for the new process.
func (c *DiskVolumeController) Shutdown() {
	if c.keepMounts {
		c.log.Info("keeping volumes mounted for reload")
		return
	}

	// Scan the actual kernel mount table for our mounts
	activeMounts := c.mntOps.FindMounts(diskMountBasePath)

	for _, am := range activeMounts {
		c.log.Info("shutting down disk mount",
			"mount_path", am.MountPath,
			"device", am.Device,
		)

		if err := c.mntOps.Unmount(am.MountPath); err != nil {
			c.log.Warn("failed to unmount on shutdown", "mount_path", am.MountPath, "error", err)
			continue
		}

		if strings.HasPrefix(am.Device, "/dev/lbd") {
			if err := c.mntOps.LbdDetach(context.Background(), am.Device); err != nil {
				c.log.Warn("failed to detach lbd on shutdown", "device", am.Device, "error", err)
			}
		} else if strings.HasPrefix(am.Device, "/dev/loop") {
			if err := c.mntOps.LoopDetach(am.Device); err != nil {
				c.log.Warn("failed to detach loop on shutdown", "device", am.Device, "error", err)
			}
		}
	}

	// Update persisted state to reflect unmounted volumes
	for _, vol := range c.state.ListVolumes() {
		if vol.Mounted {
			vol.Mounted = false
			vol.DevicePath = ""
			vol.MountPath = ""
			c.state.SetVolume(vol.EntityId, vol)
		}
	}

	if err := c.state.Save(); err != nil {
		c.log.Warn("failed to save state after shutdown", "error", err)
	}
}

// reconcileOrphanKernelState runs once at boot and tears down any kernel
// loop devices or mounts that are rooted in miren's volumes directory but
// that no longer correspond to a known volume in local state.
//
// This is a belt-and-suspenders complement to the adopt-existing-loop
// logic in ensureVolumeMount. Adoption catches loops we want to reuse;
// this sweep catches the opposite case — a stale loop (or mount) left in
// the kernel after an unclean shutdown, whose volume is no longer present
// or has been deleted. Without it, such loops leak forever and pin
// kernel state that should have been cleaned up.
//
// Must run AFTER the entity walk and restart-recovery mount step in
// ReconcileWithEntities, so that every legitimate loop device has
// already been adopted into some volState.DevicePath and will not be
// mistaken for an orphan.
func (c *DiskVolumeController) reconcileOrphanKernelState() {
	volumesDir := filepath.Join(c.dataPath, "volumes")

	// First, unmount any active mount rooted under diskMountBasePath
	// that doesn't correspond to a mounted volState. This has to happen
	// BEFORE the loop detach step below: an orphan mount backed by an
	// orphan loop device pins the loop, so detaching the loop first
	// returns EBUSY and leaves both the mount and the device behind.
	knownMounts := make(map[string]struct{})
	for _, vol := range c.state.ListVolumes() {
		if vol.Mounted && vol.MountPath != "" {
			knownMounts[vol.MountPath] = struct{}{}
		}
	}
	for _, am := range c.mntOps.FindMounts(diskMountBasePath) {
		if _, ok := knownMounts[am.MountPath]; ok {
			continue
		}
		c.log.Warn("orphan sweep: unmounting stale mount",
			"mount_path", am.MountPath,
			"device", am.Device,
		)
		if err := c.mntOps.Unmount(am.MountPath); err != nil {
			c.log.Warn("orphan sweep: Unmount failed",
				"mount_path", am.MountPath,
				"error", err)
		}
	}

	// Build the set of loop devices we legitimately own.
	known := make(map[string]struct{})
	for _, vol := range c.state.ListVolumes() {
		if vol.DevicePath != "" {
			known[vol.DevicePath] = struct{}{}
		}
	}

	backings, err := c.mntOps.FindAllLoopBackings()
	if err != nil {
		c.log.Warn("orphan sweep: FindAllLoopBackings failed", "error", err)
		return
	}

	for dev, backing := range backings {
		if _, ok := known[dev]; ok {
			continue
		}
		// Only touch loops backing files inside miren's volumes dir.
		// Anything else is not ours to manage.
		if !strings.HasPrefix(backing, volumesDir+string(filepath.Separator)) &&
			!strings.HasPrefix(backing, volumesDir+"/") {
			continue
		}

		c.log.Warn("orphan sweep: detaching stale loop device",
			"device", dev,
			"backing_file", backing,
		)
		if err := c.mntOps.LoopDetach(dev); err != nil {
			c.log.Warn("orphan sweep: LoopDetach failed",
				"device", dev,
				"backing_file", backing,
				"error", err)
		}
	}
}

// ReconcileWithEntities reconciles local state with entity server
func (c *DiskVolumeController) ReconcileWithEntities(ctx context.Context) error {
	if c.eac == nil {
		return fmt.Errorf("entity access client not set; call SetEAC before reconciling")
	}

	nodeIdRef := c.nodeId.Id()
	indexAttr := entity.Ref(storage_v1alpha.DiskVolumeNodeIdId, nodeIdRef)

	resp, err := c.eac.List(ctx, indexAttr)
	if err != nil {
		return fmt.Errorf("failed to list disk_volume entities: %w", err)
	}

	values := resp.Values()

	entityIds := make(map[string]struct{}, len(values))

	for _, entResp := range values {
		var volume storage_v1alpha.DiskVolume
		volume.Decode(entResp.Entity())

		entityIds[string(volume.ID)] = struct{}{}

		if !c.nodeId.Matches(volume.NodeId) {
			continue
		}

		if err := c.reconcileVolume(ctx, &volume); err != nil {
			c.log.Error("failed to reconcile disk volume",
				"entity_id", volume.ID,
				"error", err,
			)
		}
	}

	// Clean up orphaned volumes
	orphanCleaned := false
	for _, volState := range c.state.ListVolumes() {
		if !strings.HasPrefix(volState.EntityId, "disk_volume/") {
			continue
		}
		if _, exists := entityIds[volState.EntityId]; exists {
			continue
		}

		c.log.Info("cleaning up orphaned disk volume", "entity_id", volState.EntityId)

		// Unmount orphaned alwaysMount volumes before removing
		if alwaysMount(volState.Mode) && volState.Mounted {
			c.unmountVolume(volState)
		}

		if volState.DiskPath != "" {
			if err := c.ops.RemoveVolumeDir(volState.DiskPath); err != nil {
				c.log.Warn("failed to remove orphaned volume directory", "entity_id", volState.EntityId, "error", err)
			}
		}

		c.state.DeleteVolume(volState.EntityId)
		orphanCleaned = true
	}

	if orphanCleaned {
		if err := c.state.Save(); err != nil {
			c.log.Warn("failed to save state after orphan cleanup", "error", err)
		}
	}

	// Re-mount any alwaysMount volumes that are DV_READY but not currently mounted (restart recovery)
	for _, volState := range c.state.ListVolumes() {
		if !alwaysMount(volState.Mode) {
			continue
		}
		if !volState.Mounted || !c.mntOps.IsMounted(volState.MountPath) {
			c.log.Info("re-mounting alwaysMount volume after reconcile", "entity_id", volState.EntityId)
			if err := c.ensureVolumeMount(ctx, volState.EntityId, volState); err != nil {
				c.log.Warn("failed to re-mount volume", "entity_id", volState.EntityId, "error", err)
			}
		}
	}

	// Boot-time orphan sweep: runs once per controller lifetime, after
	// every legitimate volume has had a chance to adopt its existing
	// kernel state. Anything left over in /proc/mounts or /sys/block/loop*
	// that points at our volumes dir but no known volume is stale and
	// gets torn down. Without this, loops from uncleanly-shut-down volumes
	// leak forever.
	if !c.orphanSweepDone {
		c.reconcileOrphanKernelState()
		c.orphanSweepDone = true
	}

	return nil
}
