package diskio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// OrphanMountSweepGracePeriod is how old a disk_mount must be before the orphan
// sweeper will delete it for a missing backing volume. It guards against a race
// where a mount has just been created but its volume entity hasn't been
// observed yet.
const OrphanMountSweepGracePeriod = 2 * time.Minute

// writeTracker records revisions written directly by this controller so the
// owning ReconcileController's watch can skip its own events. It is satisfied
// by *controller.ReconcileController; declared locally to keep diskio decoupled
// from pkg/controller.
type writeTracker interface {
	RecordWrite(revision int64)
}

// DiskMountController watches disk_mount entities and manages loop-device mounts.
type DiskMountController struct {
	log      *slog.Logger
	dataPath string
	nodeId   compute.NodeId
	eac      *entityserver_v1alpha.EntityAccessClient
	state    *State
	ops      DiskMountOps

	// Cloud client for lease management and segment replay (nil when cloud not configured)
	cloudClient   CloudDiskClient
	updatesClient CloudUpdatesClient

	// writeTracker records revisions this controller writes directly via eac,
	// so the reconcile watch skips the resulting self-generated events instead
	// of re-triggering reconcile in a tight loop. nil in unit tests.
	writeTracker writeTracker

	// keepMounts, when true, causes Shutdown to skip unmounting.
	// Set during reload so the new process can pick up existing mounts.
	keepMounts bool
}

func NewDiskMountController(log *slog.Logger, dataPath string, nodeId compute.NodeId, state *State, ops DiskMountOps) *DiskMountController {
	return &DiskMountController{
		log:      log.With("module", "disk-mount"),
		dataPath: dataPath,
		nodeId:   nodeId,
		state:    state,
		ops:      ops,
	}
}

func (c *DiskMountController) SetEAC(eac *entityserver_v1alpha.EntityAccessClient) {
	c.eac = eac
}

// SetCloudClient sets the cloud client for lease management and segment replay.
func (c *DiskMountController) SetCloudClient(client CloudDiskClient) {
	c.cloudClient = client
}

// SetUpdatesClient wires the generic volume update client, used to recover a
// universal-mode volume's backing image from the cloud.
func (c *DiskMountController) SetUpdatesClient(client CloudUpdatesClient) {
	c.updatesClient = client
}

// SetWriteTracker wires the reconcile controller's write tracker so that entity
// writes this controller makes directly (via updateMountState) are recorded and
// their watch events are skipped, preventing a self-retriggering reconcile loop.
func (c *DiskMountController) SetWriteTracker(wt writeTracker) {
	c.writeTracker = wt
}

// SetKeepMounts tells the controller to skip unmounting during Shutdown.
// Used during reload so the replacement process inherits the mounts.
func (c *DiskMountController) SetKeepMounts(v bool) {
	c.keepMounts = v
}

func (c *DiskMountController) Init(ctx context.Context) error {
	return nil
}

func (c *DiskMountController) Reconcile(ctx context.Context, mount *storage_v1alpha.DiskMount, meta *entity.Meta) error {
	return c.reconcileMount(ctx, mount)
}

func (c *DiskMountController) Index() entity.Attr {
	return entity.Ref(storage_v1alpha.DiskMountNodeIdId, c.nodeId.Id())
}

// Shutdown finishes releases for detached mounts. Live attachments keep their
// fencing across a server restart; stopping the daemon does not stop app tasks.
// If keepMounts is set (reload), everything is skipped.
func (c *DiskMountController) Shutdown() {
	if c.keepMounts {
		c.log.Info("keeping mounts for reload")
		return
	}

	// Release only leases whose device cleanup has already finished.
	if c.cloudClient != nil {
		for _, ms := range c.state.ListMounts() {
			// A server stopping does not imply its app tasks have stopped.
			// Keep fencing any attachment inherited by the next process.
			if ms.LeaseNonce == "" || ms.Mounted || ms.DevicePath != "" {
				continue
			}
			cloudID := c.cloudVolumeIdFor(ms)
			if cloudID == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			unlock, err := c.state.LockCloudVolume(ctx, cloudID)
			if err == nil {
				err = c.cleanupMount(ctx, ms)
				unlock()
			}
			cancel()
			if err != nil {
				c.log.Warn("failed to release lease on shutdown", "entity_id", ms.EntityId, "error", err)
			}
		}
	}
}

// cloudVolumeIdFor resolves a mount's backing cloud volume id, or "" if the
// volume is unknown or not yet registered with the cloud. Mount state's
// VolumeId is the local disk_volume entity id, which the cloud lease api never
// accepts, so every lease-release path must map through here. The cloud id
// recorded on the mount wins — it survives the VolumeState being deleted (e.g.
// orphan cleanup); otherwise fall back to the volume state for mounts persisted
// before the cloud id was recorded on the mount.
func (c *DiskMountController) cloudVolumeIdFor(ms *MountState) string {
	if ms.CloudVolumeId != "" {
		return ms.CloudVolumeId
	}
	v := c.state.GetVolume(ms.VolumeId)
	if v == nil {
		return ""
	}
	return v.CloudVolumeId
}

func (c *DiskMountController) reconcileMount(ctx context.Context, mount *storage_v1alpha.DiskMount) error {
	entityId := string(mount.ID)
	c.log.Info("reconciling disk mount",
		"entity_id", entityId,
		"desired_state", mount.DesiredState,
		"actual_state", mount.ActualState,
	)

	switch mount.DesiredState {
	case storage_v1alpha.DM_WANT_MOUNTED:
		return c.reconcileMountMounted(ctx, mount)
	case storage_v1alpha.DM_WANT_UNMOUNTED:
		return c.reconcileMountUnmounted(ctx, mount)
	default:
		c.log.Warn("unknown desired state", "desired_state", mount.DesiredState)
		return nil
	}
}

func (c *DiskMountController) reconcileMountMounted(ctx context.Context, mount *storage_v1alpha.DiskMount) error {
	entityId := string(mount.ID)

	switch mount.ActualState {
	case storage_v1alpha.DM_PENDING:
		return c.attachAndMount(ctx, mount)
	case storage_v1alpha.DM_ATTACHING:
		return c.attachAndMount(ctx, mount)
	case storage_v1alpha.DM_ATTACHED:
		return c.mountVolume(ctx, mount)
	case storage_v1alpha.DM_MOUNTING:
		return nil
	case storage_v1alpha.DM_MOUNTED:
		mountState := c.state.GetMount(entityId)
		if mountState == nil {
			// No local state but entity says mounted. Check if actually mounted on the system.
			if mount.MountPath != "" && c.ops.IsMounted(mount.MountPath) {
				// Actually mounted — reconstruct local state from entity fields
				c.log.Info("reconstructing mount state from entity and kernel",
					"entity_id", entityId,
					"mount_path", mount.MountPath,
					"device_path", mount.DevicePath,
				)
				volState := c.state.GetVolume(string(mount.VolumeId))
				var mode storage_v1alpha.DiskVolumeVolumeMode
				var cloudVolumeId string
				if volState != nil {
					mode = volState.Mode
					cloudVolumeId = volState.CloudVolumeId
				}
				c.state.SetMount(entityId, &MountState{
					EntityId:      entityId,
					VolumeId:      string(mount.VolumeId),
					CloudVolumeId: cloudVolumeId,
					DevicePath:    mount.DevicePath,
					MountPath:     mount.MountPath,
					Mounted:       true,
					ReadOnly:      mount.ReadOnly,
					Mode:          mode,
				})
				if err := c.state.Save(); err != nil {
					c.log.Warn("failed to save reconstructed mount state", "error", err)
				}
				return nil
			}
			// Not actually mounted — re-attach from scratch
			c.log.Info("entity says DM_MOUNTED but not found on system, recovering", "entity_id", entityId)
			return c.attachAndMount(ctx, mount)
		}
		if mountState.MountPath != "" && !c.ops.IsMounted(mountState.MountPath) {
			c.log.Warn("entity reports mounted but mount not found on system, recovering",
				"entity_id", entityId,
				"mount_path", mountState.MountPath,
			)
			return c.attachAndMount(ctx, mount)
		}
		return nil
	case storage_v1alpha.DM_ERROR:
		c.log.Info("mount in error state, attempting recovery", "entity_id", entityId)
		return c.attachAndMount(ctx, mount)
	case storage_v1alpha.DM_DETACHED:
		c.log.Info("mount detached but desired mounted, recovering", "entity_id", entityId)
		return c.attachAndMount(ctx, mount)
	case storage_v1alpha.DM_UNMOUNTING, storage_v1alpha.DM_DETACHING:
		// Tearing down while desired state is mounted; unexpected.
		fallthrough
	default:
		c.log.Warn("unexpected actual state for mounted", "actual_state", mount.ActualState)
		return nil
	}
}

func (c *DiskMountController) reconcileMountUnmounted(ctx context.Context, mount *storage_v1alpha.DiskMount) error {
	entityId := string(mount.ID)

	switch mount.ActualState {
	case storage_v1alpha.DM_DETACHED:
		if c.state.GetMount(entityId) != nil {
			return c.unmountAndDetach(ctx, mount)
		}
		return nil
	case storage_v1alpha.DM_UNMOUNTING, storage_v1alpha.DM_DETACHING:
		return c.unmountAndDetach(ctx, mount)
	case storage_v1alpha.DM_PENDING, storage_v1alpha.DM_ATTACHING, storage_v1alpha.DM_ATTACHED, storage_v1alpha.DM_MOUNTING, storage_v1alpha.DM_MOUNTED, storage_v1alpha.DM_ERROR:
		// Still attached/mounted; tear it down to reach the unmounted state.
		fallthrough
	default:
		return c.unmountAndDetach(ctx, mount)
	}
}

func (c *DiskMountController) attachAndMount(ctx context.Context, mount *storage_v1alpha.DiskMount) error {
	entityId := string(mount.ID)
	volumeId := string(mount.VolumeId)

	c.log.Info("attaching and mounting disk volume",
		"entity_id", entityId,
		"volume_id", volumeId,
		"mount_path", mount.MountPath,
	)

	if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_ATTACHING, nil, nil, nil); err != nil {
		c.log.Warn("failed to update mount state to attaching", "error", err)
	}

	// Get volume state to find image path
	volState := c.state.GetVolume(volumeId)
	if volState == nil {
		c.setMountError(ctx, mount.ID, fmt.Sprintf("volume %s not found in state", volumeId))
		return fmt.Errorf("volume %s not found in state", volumeId)
	}
	unlock, err := c.state.LockCloudVolume(ctx, volState.CloudVolumeId)
	if err != nil {
		return err
	}
	defer unlock()
	current := c.state.GetVolume(volumeId)
	if current == nil {
		return fmt.Errorf("volume %s not found in state", volumeId)
	}
	if current.CloudVolumeId != volState.CloudVolumeId || current.DiskPath != volState.DiskPath {
		return fmt.Errorf("volume %s changed while waiting for cloud lock", volumeId)
	}
	volState = current
	if c.state.HasPendingCloudUpload(volState.CloudVolumeId) {
		return fmt.Errorf("volume %s has a pending cloud upload", volumeId)
	}
	// Recovery must finish the old attachment/release before acquiring a new
	// lease or overwriting its cleanup credentials.
	if previous := c.state.GetMount(entityId); previous != nil &&
		(previous.LeaseAcquisitionID == "" || previous.LeaseNonce != "" || previous.DevicePath != "") {
		if err := c.cleanupMount(ctx, previous); err != nil {
			return err
		}
	}

	// For alwaysMount volumes, the volume controller owns the mount.
	// Just set up tracking and mark DM_MOUNTED.
	if alwaysMount(volState.Mode) {
		devPath, mntPath, err := c.state.SetMountFromVolume(volumeId, &MountState{
			EntityId:      entityId,
			VolumeId:      volumeId,
			CloudVolumeId: volState.CloudVolumeId,
			Mounted:       true,
			ReadOnly:      mount.ReadOnly,
			Mode:          volState.Mode,
		})
		if err != nil {
			c.setMountError(ctx, mount.ID, err.Error())
			return err
		}

		if err := c.state.Save(); err != nil {
			c.log.Warn("failed to save state after alwaysMount tracking", "error", err)
		}

		if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_MOUNTED, &devPath, &devPath, nil); err != nil {
			c.log.Warn("failed to update mount state to mounted", "error", err)
		}

		c.log.Info("alwaysMount volume already mounted, tracking lease",
			"entity_id", entityId,
			"mount_path", mntPath,
		)
		return nil
	}

	// For accelerator mode with cloud configured, acquire lease and replay
	// segments. A volume with no cloud id has not been registered yet, so there
	// is no lease to take and nothing to replay.
	var leaseNonce string
	var acquisitionID string
	if volState.Mode == storage_v1alpha.VM_ACCELERATOR && c.cloudClient != nil && volState.CloudVolumeId != "" {
		intent := c.state.GetMount(entityId)
		if intent == nil || intent.LeaseAcquisitionID == "" {
			intent = &MountState{
				EntityId: entityId, VolumeId: volumeId, CloudVolumeId: volState.CloudVolumeId,
				Mode: volState.Mode, LeaseAcquisitionID: uuid.NewString(),
			}
		}
		if err := c.recoverMountLease(ctx, intent); err != nil {
			c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to acquire volume lease: %v", err))
			return fmt.Errorf("failed to acquire volume lease: %w", err)
		}
		leaseNonce, acquisitionID = intent.LeaseNonce, intent.LeaseAcquisitionID

		if rerr := c.replayMissingSegments(ctx, volState); rerr != nil {
			_ = c.cleanupMount(ctx, c.state.GetMount(entityId))
			c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to replay segments: %v", rerr))
			return fmt.Errorf("failed to replay segments: %w", rerr)
		}
	}

	imagePath := filepath.Join(volState.DiskPath, "disk.img")

	// Universal mode has no write log to replay, but the cloud may be holding a
	// snapshot of an image this host has lost.
	if volState.Mode == storage_v1alpha.VM_UNIVERSAL {
		if rerr := c.RestoreImageIfMissing(ctx, volState, imagePath); rerr != nil {
			c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to restore volume image: %v", rerr))
			return fmt.Errorf("failed to restore volume image: %w", rerr)
		}
	}

	// Attach device based on volume mode
	var devicePath string
	var backingIdentity *BackingIdentity
	if volState.Mode == storage_v1alpha.VM_ACCELERATOR {
		logDir := filepath.Join(volState.DiskPath, "logs")
		if err := c.ops.CreateDir(logDir, 0755); err != nil {
			if ms := c.state.GetMount(entityId); ms != nil {
				_ = c.cleanupMount(ctx, ms)
			}
			c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to create log directory: %v", err))
			return fmt.Errorf("failed to create log directory: %w", err)
		}
		devicePath, err = c.ops.LbdAttach(ctx, imagePath, logDir)
		if err != nil {
			if leaseNonce != "" {
				_ = c.cleanupMount(ctx, c.state.GetMount(entityId))
			}
			c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to attach lbd device: %v", err))
			return fmt.Errorf("failed to attach lbd device: %w", err)
		}
	} else {
		// If the backing file is already attached to a loop device in the
		// kernel — e.g. left over from a SIGKILL'd miren whose container
		// kept holding the old loop open — adopt that device rather than
		// create a second one. Attaching the same backing file to two
		// loop devices gives each one its own incoherent page cache and
		// corrupts the filesystem.
		existing, findErr := c.ops.FindLoopByBacking(imagePath)
		if findErr != nil {
			// Fail closed: if we can't see the kernel's loop state, we
			// can't tell whether attaching would double-attach. Record
			// the error and return a retriable reconcile error so the
			// next tick tries again once sysfs is healthy.
			msg := fmt.Sprintf("find loop device for backing file %s: %v", imagePath, findErr)
			c.setMountError(ctx, mount.ID, msg)
			return fmt.Errorf("find loop device for backing file %s: %w", imagePath, findErr)
		}
		if existing != "" {
			c.log.Info("adopting existing loop device for backing file",
				"entity_id", entityId,
				"image_path", imagePath,
				"device", existing,
			)
			devicePath = existing
			backingIdentity, err = verifiedLoopIdentity(c.ops, devicePath, imagePath)
			if err != nil {
				return fmt.Errorf("verify adopted loop backing: %w", err)
			}
		} else {
			devicePath, backingIdentity, err = c.ops.LoopAttach(imagePath)
			if err != nil {
				c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to attach loop device: %v", err))
				return fmt.Errorf("failed to attach loop device: %w", err)
			}
		}
	}

	c.state.SetMount(entityId, &MountState{
		EntityId:           entityId,
		VolumeId:           volumeId,
		CloudVolumeId:      volState.CloudVolumeId,
		DevicePath:         devicePath,
		BackingPath:        imagePath,
		BackingIdentity:    backingIdentity,
		MountPath:          mount.MountPath,
		Mounted:            false,
		ReadOnly:           mount.ReadOnly,
		Mode:               volState.Mode,
		LeaseNonce:         leaseNonce,
		LeaseOwned:         leaseNonce != "",
		LeaseAcquisitionID: acquisitionID,
	})

	if err := c.state.Save(); err != nil {
		return err
	}

	// Update entity with device path and loop device info
	loopDev := devicePath
	if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_ATTACHED, &devicePath, &loopDev, nil); err != nil {
		c.log.Warn("failed to update mount state to attached", "error", err)
	}

	// Now mount the volume
	if err := c.mountVolume(ctx, mount); err != nil {
		// Rollback: detach device, release lease, clean up local state
		c.log.Warn("mount failed, rolling back attach", "entity_id", entityId, "error", err)
		return errors.Join(err, c.cleanupMount(ctx, c.state.GetMount(entityId)))
	}

	return nil
}

func (c *DiskMountController) mountVolume(ctx context.Context, mount *storage_v1alpha.DiskMount) error {
	entityId := string(mount.ID)

	mountState := c.state.GetMount(entityId)
	if mountState == nil {
		return fmt.Errorf("mount state not found for %s", entityId)
	}

	if mountState.Mounted {
		c.log.Info("already mounted, skipping", "entity_id", entityId)
		return nil
	}

	c.log.Info("mounting filesystem",
		"entity_id", entityId,
		"device", mountState.DevicePath,
		"mount_path", mount.MountPath,
	)

	if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_MOUNTING, nil, nil, nil); err != nil {
		c.log.Warn("failed to update mount state to mounting", "error", err)
	}

	if err := c.ops.CreateDir(mount.MountPath, 0755); err != nil {
		c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to create mount point: %v", err))
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	volState := c.state.GetVolume(mountState.VolumeId)
	filesystem := "ext4"
	if volState != nil && volState.Filesystem != "" {
		filesystem = volState.Filesystem
	}

	formatted, err := c.ops.IsFormatted(ctx, mountState.DevicePath, filesystem)
	if err != nil {
		c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to check if formatted: %v", err))
		return fmt.Errorf("failed to check if formatted: %w", err)
	}

	if !formatted {
		c.log.Info("formatting device", "device", mountState.DevicePath, "filesystem", filesystem)

		formatDeadline := time.Now().Add(1 * time.Minute)
		backoff := 1 * time.Second
		maxBackoff := 10 * time.Second
		attempt := 0

		for {
			attempt++
			err := c.ops.FormatDevice(ctx, mountState.DevicePath, filesystem)
			if err == nil {
				c.log.Info("device formatted successfully", "device", mountState.DevicePath, "attempt", attempt)
				break
			}

			c.log.Error("format device failed, will retry",
				"device", mountState.DevicePath,
				"filesystem", filesystem,
				"attempt", attempt,
				"error", err,
			)

			if time.Now().After(formatDeadline) {
				c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to format device, will retry on next reconcile: %v", err))
				return fmt.Errorf("failed to format device after retries: %w", err)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}

	if err := mountWithFsckRetry(ctx, c.log, c.ops, mountState.DevicePath, mount.MountPath, filesystem, mount.ReadOnly); err != nil {
		c.setMountError(ctx, mount.ID, fmt.Sprintf("failed to mount: %v", err))
		return fmt.Errorf("failed to mount: %w", err)
	}

	mountState.Mounted = true
	c.state.SetMount(entityId, mountState)
	if err := c.state.Save(); err != nil {
		return err
	}

	c.log.Info("filesystem mounted",
		"entity_id", entityId,
		"mount_path", mount.MountPath,
	)

	if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_MOUNTED, nil, nil, nil); err != nil {
		c.log.Warn("failed to update mount state to mounted", "error", err)
	}

	return nil
}

func (c *DiskMountController) unmountAndDetach(ctx context.Context, mount *storage_v1alpha.DiskMount) error {
	entityId := string(mount.ID)

	c.log.Info("unmounting and detaching",
		"entity_id", entityId,
		"mount_path", mount.MountPath,
	)

	if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_UNMOUNTING, nil, nil, nil); err != nil {
		c.log.Warn("failed to update mount state to unmounting", "error", err)
	}

	mountState := c.state.GetMount(entityId)
	if mountState == nil {
		c.log.Warn("mount state not found", "entity_id", entityId)
		clearPath := ""
		clearErr := ""
		if err := c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_DETACHED, &clearPath, &clearPath, &clearErr); err != nil {
			c.log.Warn("failed to update mount state to detached", "error", err)
		}
		return nil
	}

	unlock, err := c.state.LockCloudVolume(ctx, c.cloudVolumeIdFor(mountState))
	if err != nil {
		return err
	}
	defer unlock()
	if err := c.cleanupMount(ctx, mountState); err != nil {
		c.setMountError(ctx, mount.ID, err.Error())
		return err
	}
	clearPath, clearErr := "", ""
	return c.updateMountState(ctx, mount.ID, storage_v1alpha.DM_DETACHED, &clearPath, &clearPath, &clearErr)
}

// cleanupMount persists each successful step independently so retries, orphan
// cleanup and attach rollback cannot repeat a completed release or detach.
// The caller holds the cloud-volume lock.
func (c *DiskMountController) cleanupMount(ctx context.Context, mountState *MountState) error {
	entityId := mountState.EntityId
	// The record may have progressed while this caller waited for an upload.
	mountState = c.state.GetMount(entityId)
	if mountState == nil {
		return nil
	}
	cloudID := c.cloudVolumeIdFor(mountState)
	if c.state.HasPendingCloudUpload(cloudID) {
		// A failed final drain must be resumable even if the volume entity
		// vanished and the periodic watcher no longer enumerates it.
		if mountState.DevicePath != "" || mountState.Mounted || !mountState.LeaseOwned {
			return fmt.Errorf("volume %s has a pending cloud upload", cloudID)
		}
		if err := c.drainMountLogs(ctx, mountState); err != nil {
			return err
		}
		if c.state.HasPendingCloudUpload(cloudID) {
			return fmt.Errorf("volume %s has a pending cloud upload", cloudID)
		}
	}
	if mountState.LeaseNonce == "" && mountState.LeaseAcquisitionID != "" {
		if err := c.recoverMountLease(ctx, mountState); err != nil && !errors.Is(err, ErrLeaseAcquisitionEnded) {
			return err
		}
	}
	// For alwaysMount volumes, skip actual unmount/detach — volume controller owns the mount
	if alwaysMount(mountState.Mode) {
		return c.state.DeleteMountAndSave(entityId)
	}

	// Persisted names are claims, not proof of kernel ownership. In particular,
	// neither an old lease-only record nor map iteration order owns a device.
	mountedDevice, err := c.ops.MountDevice(mountState.MountPath)
	if err != nil {
		return fmt.Errorf("inspect mount before cleanup: %w", err)
	}
	if mountState.DevicePath != "" {
		expected := mountState.BackingPath
		if expected == "" {
			if volume := c.state.GetVolume(mountState.VolumeId); volume != nil {
				expected = filepath.Join(volume.DiskPath, "disk.img")
			}
		}
		owned, absent := false, false
		if mountState.Mode != storage_v1alpha.VM_ACCELERATOR {
			identity, err := c.ops.LoopIdentity(mountState.DevicePath)
			if err != nil {
				return fmt.Errorf("inspect loop before cleanup: %w", err)
			}
			absent = identity == nil
			if !absent {
				if mountState.BackingIdentity == nil {
					// Legacy ownership must be established while the known file
					// still exists, not from sysfs path text after rename/unlink.
					identity, err = legacyLoopIdentity(c.ops, c.state, mountState.VolumeId, expected, identity)
					if err != nil {
						return fmt.Errorf("establish legacy loop ownership: %w", err)
					}
					if identity != nil {
						mountState.BackingIdentity = identity
						if err := c.state.SetLegacyMountIdentityAndSave(entityId, mountState.DevicePath, identity); err != nil {
							return err
						}
					}
				}
				owned = identity != nil && *identity == *mountState.BackingIdentity
			}
		} else {
			backing, err := c.ops.DeviceBacking(mountState.DevicePath)
			if err != nil {
				return fmt.Errorf("inspect device before cleanup: %w", err)
			}
			// lbdctl canonicalizes the submitted path; unlike loops, the
			// kernel stores this path rather than following inode renames.
			if backing != expected && expected != "" {
				if canonical, err := filepath.EvalSymlinks(expected); err == nil {
					if canonical, err := filepath.Abs(canonical); err == nil {
						expected = canonical
					}
				}
			}
			if backing != "" && expected == "" {
				return fmt.Errorf("cannot establish backing ownership for %s", entityId)
			}
			absent, owned = backing == "", backing != "" && backing == expected
		}
		if owned {
			for _, other := range c.state.ListMounts() {
				if other.EntityId == entityId || other.DevicePath != mountState.DevicePath || other.MountPath == "" {
					continue
				}
				otherDevice, err := c.ops.MountDevice(other.MountPath)
				if err != nil {
					return err
				}
				if otherDevice == mountState.DevicePath {
					return fmt.Errorf("ambiguous attachment ownership for %s and %s", entityId, other.EntityId)
				}
			}
		}
		if absent || !owned {
			if mountedDevice == mountState.DevicePath {
				return fmt.Errorf("mounted device %s has absent or mismatched backing ownership", mountedDevice)
			}
			mountState.DevicePath = ""
			mountState.Mounted = false
			mountState.MountPath = ""
		}
	}
	if mountedDevice == "" || mountedDevice != mountState.DevicePath {
		mountState.Mounted = false
		mountState.MountPath = ""
	} else {
		mountState.Mounted = true
	}
	for _, other := range c.state.ListMounts() {
		if other.EntityId != entityId && mountState.LeaseNonce != "" &&
			other.LeaseNonce == mountState.LeaseNonce && c.cloudVolumeIdFor(other) == cloudID &&
			(other.Mounted || other.LeaseOwned) {
			return fmt.Errorf("ambiguous lease ownership for %s", entityId)
		}
	}
	// Unmount if mounted
	if mountState.Mounted && mountState.MountPath != "" {
		if err := c.ops.Unmount(mountState.MountPath); err != nil {
			return fmt.Errorf("failed to unmount: %w", err)
		}
		mountState.Mounted = false
	}
	// Unmount does not end the lease, including recovery after a crash between
	// the syscall and this checkpoint. Keep the original nonce for tail drain.
	mountState.LeaseOwned = mountState.LeaseNonce != ""
	if err := c.state.SetMountAndSave(entityId, mountState); err != nil {
		return err
	}

	// Detach device based on mode (use persisted mode from MountState)
	var detachErr error
	if mountState.DevicePath != "" {
		mounted, err := c.ops.IsDeviceMounted(mountState.DevicePath)
		if err != nil {
			return fmt.Errorf("check device before detach: %w", err)
		} else if mounted {
			return fmt.Errorf("device %s is still mounted", mountState.DevicePath)
		} else if mountState.Mode == storage_v1alpha.VM_ACCELERATOR {
			if err := c.ops.LbdDetach(ctx, mountState.DevicePath); err != nil {
				c.log.Warn("failed to detach lbd device", "error", err)
				detachErr = fmt.Errorf("failed to detach lbd device: %w", err)
			}
		} else {
			identity, err := c.ops.LoopIdentity(mountState.DevicePath)
			if err != nil || identity == nil || mountState.BackingIdentity == nil || *identity != *mountState.BackingIdentity {
				return fmt.Errorf("loop ownership changed before detach: %s (%v)", mountState.DevicePath, err)
			}
			if err := c.ops.LoopDetach(mountState.DevicePath); err != nil {
				c.log.Warn("failed to detach loop device", "error", err)
				detachErr = fmt.Errorf("failed to detach loop device: %w", err)
			}
		}
		if detachErr == nil {
			mountState.DevicePath = ""
			if err := c.state.SetMountAndSave(entityId, mountState); err != nil {
				return err
			}
		}
	}

	if detachErr != nil {
		return detachErr
	}
	if err := c.drainMountLogs(ctx, mountState); err != nil {
		return err
	}

	// Release cloud lease if one was acquired
	var leaseErr error
	if mountState.LeaseNonce != "" && c.cloudClient != nil {
		if cloudID != "" {
			if err := c.cloudClient.ReleaseLease(ctx, cloudID, mountState.LeaseNonce); err != nil {
				c.log.Warn("failed to release volume lease", "entity_id", entityId, "error", err)
				leaseErr = fmt.Errorf("failed to release volume lease: %w", err)
			} else {
				mountState.LeaseNonce = ""
				mountState.LeaseOwned = false
				mountState.LeaseAcquisitionID = ""
				if err := c.state.SetMountAndSave(entityId, mountState); err != nil {
					return err
				}
			}
		}
	}

	if leaseErr != nil {
		return leaseErr
	}
	if mountState.LeaseNonce != "" {
		return fmt.Errorf("cannot release lease for %s without cloud client and volume id", entityId)
	}
	return c.state.DeleteMountAndSave(entityId)
}

func (c *DiskMountController) drainMountLogs(ctx context.Context, mount *MountState) error {
	if mount.Mode != storage_v1alpha.VM_ACCELERATOR || mount.LeaseNonce == "" || c.cloudClient == nil {
		return nil
	}
	diskPath := ""
	if volume := c.state.GetVolume(mount.VolumeId); volume != nil {
		diskPath = volume.DiskPath
	}
	if diskPath == "" && mount.BackingPath != "" {
		diskPath = filepath.Dir(mount.BackingPath)
	}
	if diskPath == "" {
		return fmt.Errorf("cannot establish log directory for lease release of %s", mount.EntityId)
	}
	uploader := NewCloudSegmentUploaderWithClient(c.log, c.updatesClient, c.state)
	return uploader.drainSegmentsLocked(ctx, c.cloudVolumeIdFor(mount), diskPath)
}

// recoverMountLease resolves a durable acquisition intent. A terminal response
// retires only that identity; a later reconcile may explicitly start a new one.
// The caller holds the cloud-volume lock.
func (c *DiskMountController) recoverMountLease(ctx context.Context, ms *MountState) error {
	if c.cloudClient == nil {
		return fmt.Errorf("cannot recover lease acquisition without cloud client")
	}
	if err := c.state.SetMountAndSave(ms.EntityId, ms); err != nil {
		return err
	}
	nonce, err := c.cloudClient.AcquireLeaseWithIdentity(ctx, c.cloudVolumeIdFor(ms), ms.LeaseAcquisitionID)
	if errors.Is(err, ErrLeaseAcquisitionEnded) {
		ms.LeaseAcquisitionID = ""
		if saveErr := c.state.SetMountAndSave(ms.EntityId, ms); saveErr != nil {
			return saveErr
		}
		return err
	}
	if err != nil {
		return err
	}
	ms.LeaseNonce, ms.LeaseOwned = nonce, true
	return c.state.SetMountAndSave(ms.EntityId, ms)
}

func diskMountActualStateToId(state storage_v1alpha.DiskMountActualState) entity.Id {
	switch state {
	case storage_v1alpha.DM_PENDING:
		return storage_v1alpha.DiskMountActualStateDmPendingId
	case storage_v1alpha.DM_ATTACHING:
		return storage_v1alpha.DiskMountActualStateDmAttachingId
	case storage_v1alpha.DM_ATTACHED:
		return storage_v1alpha.DiskMountActualStateDmAttachedId
	case storage_v1alpha.DM_MOUNTING:
		return storage_v1alpha.DiskMountActualStateDmMountingId
	case storage_v1alpha.DM_MOUNTED:
		return storage_v1alpha.DiskMountActualStateDmMountedId
	case storage_v1alpha.DM_UNMOUNTING:
		return storage_v1alpha.DiskMountActualStateDmUnmountingId
	case storage_v1alpha.DM_DETACHING:
		return storage_v1alpha.DiskMountActualStateDmDetachingId
	case storage_v1alpha.DM_DETACHED:
		return storage_v1alpha.DiskMountActualStateDmDetachedId
	case storage_v1alpha.DM_ERROR:
		return storage_v1alpha.DiskMountActualStateDmErrorId
	default:
		return storage_v1alpha.DiskMountActualStateDmPendingId
	}
}

// updateMountState updates the actual_state and optionally other fields.
// Use nil pointers to leave fields unchanged.
func (c *DiskMountController) updateMountState(ctx context.Context, id entity.Id, state storage_v1alpha.DiskMountActualState, devicePath, loopDevice, errorMsg *string) error {
	if c.eac == nil {
		return nil
	}

	stateId := diskMountActualStateToId(state)

	attrs := []entity.Attr{
		entity.Ref(entity.DBId, id),
		entity.Ref(storage_v1alpha.DiskMountActualStateId, stateId),
	}

	if devicePath != nil {
		attrs = append(attrs, entity.String(storage_v1alpha.DiskMountDevicePathId, *devicePath))
	}

	if loopDevice != nil {
		attrs = append(attrs, entity.String(storage_v1alpha.DiskMountLoopDeviceId, *loopDevice))
	}

	if errorMsg != nil {
		attrs = append(attrs, entity.String(storage_v1alpha.DiskMountErrorMessageId, *errorMsg))
	}

	result, err := c.eac.Patch(ctx, attrs, 0)
	if err != nil {
		return err
	}

	// Record the revision we just wrote so the reconcile watch skips this
	// self-generated event rather than re-enqueuing the same entity — the
	// mechanism that stops a stuck mount from self-retriggering at full
	// throughput (MIR-1345).
	if c.writeTracker != nil && result != nil && result.HasRevision() {
		c.writeTracker.RecordWrite(result.Revision())
	}

	return nil
}

func (c *DiskMountController) setMountError(ctx context.Context, id entity.Id, errorMsg string) {
	if err := c.updateMountState(ctx, id, storage_v1alpha.DM_ERROR, nil, nil, &errorMsg); err != nil {
		c.log.Warn("failed to set mount error state", "entity_id", id, "error", err)
	}
}

// ReconcileWithEntities reconciles local state with entity server
func (c *DiskMountController) ReconcileWithEntities(ctx context.Context) error {
	if c.eac == nil {
		return fmt.Errorf("entity access client not set; call SetEAC before reconciling")
	}

	nodeIdRef := c.nodeId.Id()
	indexAttr := entity.Ref(storage_v1alpha.DiskMountNodeIdId, nodeIdRef)

	resp, err := c.eac.List(ctx, indexAttr)
	if err != nil {
		return fmt.Errorf("failed to list disk_mount entities: %w", err)
	}

	values := resp.Values()

	entityIds := make(map[string]struct{}, len(values))

	for _, entResp := range values {
		var mount storage_v1alpha.DiskMount
		mount.Decode(entResp.Entity())

		if !c.nodeId.Matches(mount.NodeId) {
			continue
		}

		entityIds[string(mount.ID)] = struct{}{}

		if err := c.reconcileMount(ctx, &mount); err != nil {
			c.log.Error("failed to reconcile disk mount",
				"entity_id", mount.ID,
				"error", err,
			)
		}
	}

	// Clean up orphaned mounts
	orphanCleaned := false
	for _, mountState := range c.state.ListMounts() {
		if !strings.HasPrefix(mountState.EntityId, "disk_mount/") {
			continue
		}
		if _, exists := entityIds[mountState.EntityId]; exists {
			continue
		}

		c.log.Info("cleaning up orphaned disk mount", "entity_id", mountState.EntityId)
		c.teardownLocalMount(ctx, mountState)
		orphanCleaned = true
	}

	if orphanCleaned {
		if err := c.state.Save(); err != nil {
			c.log.Warn("failed to save state after orphan cleanup", "error", err)
		}
	}

	return nil
}

// teardownLocalMount unmounts, detaches, and releases the lease for a locally
// tracked mount, retaining incomplete cleanup for retry. It does not touch the entity.
// alwaysMount volumes are owned by
// the volume controller, so their mount/detach is skipped.
func (c *DiskMountController) teardownLocalMount(ctx context.Context, mountState *MountState) {
	unlock, err := c.state.LockCloudVolume(ctx, c.cloudVolumeIdFor(mountState))
	if err == nil {
		err = c.cleanupMount(ctx, mountState)
		unlock()
	}
	if err != nil {
		c.log.Warn("failed to clean up orphaned mount", "entity_id", mountState.EntityId, "error", err)
	}
}

// ReconcileOrphanMounts deletes disk_mount entities on this node whose backing
// disk_volume no longer exists. Such a mount can never reach its desired state
// and, before writes are deduped, self-retriggers reconcile indefinitely
// (MIR-1345). Deleting the entity via the entity server also removes its
// secondary-index keys, which a raw etcd delete would leave dangling.
//
// minMountAge guards against deleting a just-created mount whose volume entity
// hasn't been observed yet; pass OrphanMountSweepGracePeriod in production.
func (c *DiskMountController) ReconcileOrphanMounts(ctx context.Context, minMountAge time.Duration) error {
	if c.eac == nil {
		return fmt.Errorf("entity access client not set; call SetEAC before reconciling")
	}

	resp, err := c.eac.List(ctx, c.Index())
	if err != nil {
		return fmt.Errorf("list disk_mount entities: %w", err)
	}

	now := time.Now()
	var deleted, localCleaned int
	for _, e := range resp.Values() {
		var mount storage_v1alpha.DiskMount
		mount.Decode(e.Entity())

		if mount.VolumeId == "" {
			continue
		}

		// Leave young mounts alone so a mount created before its volume was
		// observed doesn't get swept in a race.
		if minMountAge > 0 && now.Sub(e.Entity().GetCreatedAt()) < minMountAge {
			continue
		}

		// Is the backing volume gone? Only a definitive not-found counts as
		// orphaned; transient errors are skipped so we fail safe.
		_, verr := c.eac.Get(ctx, string(mount.VolumeId))
		if verr == nil {
			continue
		}
		if !errors.Is(verr, cond.ErrNotFound{}) {
			c.log.Warn("orphan mount sweep: failed to load backing volume",
				"entity_id", mount.ID, "volume_id", mount.VolumeId, "error", verr)
			continue
		}

		c.log.Info("orphan mount sweep: deleting mount with missing backing volume",
			"entity_id", mount.ID,
			"volume_id", mount.VolumeId,
			"disk_lease_id", mount.DiskLeaseId,
			"actual_state", mount.ActualState,
		)

		// Best-effort local teardown before removing the entity.
		if mountState := c.state.GetMount(string(mount.ID)); mountState != nil {
			c.teardownLocalMount(ctx, mountState)
			localCleaned++
		}

		if _, derr := c.eac.Delete(ctx, string(mount.ID)); derr != nil {
			c.log.Warn("orphan mount sweep: failed to delete mount entity",
				"entity_id", mount.ID, "error", derr)
			continue
		}
		deleted++
	}

	if localCleaned > 0 {
		if err := c.state.Save(); err != nil {
			c.log.Warn("failed to save state after orphan mount sweep", "error", err)
		}
	}

	if deleted > 0 {
		c.log.Info("orphan mount sweep complete", "deleted", deleted)
	}

	return nil
}
