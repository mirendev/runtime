package disk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// OrphanSweepGracePeriod is how old a BOUND/PENDING lease must be before the
// recurring orphan sweep will reclaim it. It's defense-in-depth: the sweep
// already only targets leases whose owning sandbox is DEAD or missing, but a
// young lease may belong to a sandbox that is still booting (its entity not yet
// settled) or one that just died and whose own in-band ReleaseDiskLeases is
// still in flight. Skipping young leases lets those in-band paths win the race.
// The boot-time sweep (Init) bypasses this with a zero grace, since it only ever
// sees leftovers from a prior, crashed process where nothing is mid-boot.
const OrphanSweepGracePeriod = 2 * time.Minute

// leaseInfo tracks active lease details
type leaseInfo struct {
	leaseId   string
	diskId    string
	sandboxId string
	volumeId  string // Store volume ID to avoid lookups during delete
}

// DiskLeaseController manages disk lease entities and exclusive access.
// It uses disk_mount entities to coordinate mount operations via loop devices.
type DiskLeaseController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// NodeId is the ID of this node, used for creating disk_mount entities
	NodeId compute.NodeId

	// Base path for disk mounts (e.g., /var/lib/miren/disks)
	mountBasePath string

	// Track active leases: diskId -> leaseId
	mu           sync.RWMutex
	activeLeases map[string]string
	leaseDetails map[string]*leaseInfo

	// configuredMode is the disk mode from server config ("", "auto", "universal", "accelerator")
	configuredMode string

	// diskMode determines how disk mounts are performed (universal or accelerator)
	diskMode storage_v1alpha.DiskMode
}

// NewDiskLeaseController creates a disk lease controller that uses disk_mount entities.
// The diskMode parameter comes from server config (MIREN_DISK_MODE); pass "" for auto-detection.
func NewDiskLeaseController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, nodeId compute.NodeId, diskMode string) *DiskLeaseController {
	return &DiskLeaseController{
		Log:            log.With("module", "disk-lease"),
		EAC:            eac,
		NodeId:         nodeId,
		mountBasePath:  "/var/lib/miren/disks",
		activeLeases:   make(map[string]string),
		leaseDetails:   make(map[string]*leaseInfo),
		configuredMode: diskMode,
	}
}

// ForceUniversalMode forces the controller to use disk_mount entities with
// loop devices. This is used by integration tests.
func (d *DiskLeaseController) ForceUniversalMode() {
	d.diskMode = storage_v1alpha.UNIVERSAL
}

// Init initializes the disk lease controller
func (d *DiskLeaseController) Init(ctx context.Context) error {
	d.diskMode = detectDiskMode(d.configuredMode)
	d.Log.Info("disk lease controller initialized", "mode", d.diskMode)

	// Zero grace at boot: anything we find is a leftover from a prior process,
	// not a sandbox booting in this one.
	if err := d.ReconcileOrphanLeases(ctx, 0); err != nil {
		// Orphan sweep is best-effort — log and continue. Individual
		// leases may still transition normally via their entity events.
		d.Log.Warn("orphan lease reconciliation failed", "error", err)
	}
	return nil
}

// ReconcileOrphanLeases releases leases whose owning sandbox no longer
// exists or is already DEAD. On a clean shutdown the sandbox controller
// releases leases via StopSandbox → ReleaseDiskLeases, but a sandbox can
// also die without that path running: a SIGKILL, or a boot that fails
// after binding the lease (the boot-failure defer marks the sandbox DEAD).
// A BOUND lease left pointing at a dead sandbox then blocks every future
// sandbox that wants the same disk. This sweep runs both at boot (Init)
// and periodically, turning a latent deadlock into a transient, self-
// healing one bounded by the sweep interval.
//
// minLeaseAge skips leases younger than the given age. The recurring sweep
// passes OrphanSweepGracePeriod so it never reclaims a lease that may still
// belong to a booting sandbox or a death whose in-band release is mid-flight;
// the boot-time sweep passes 0. See OrphanSweepGracePeriod for the rationale.
func (d *DiskLeaseController) ReconcileOrphanLeases(ctx context.Context, minLeaseAge time.Duration) error {
	// Entity-only unit tests instantiate the controller without an EAC
	// and call Init; skip the sweep cleanly in that case.
	if d.EAC == nil {
		return nil
	}

	resp, err := d.EAC.List(ctx, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease))
	if err != nil {
		return fmt.Errorf("list disk leases: %w", err)
	}

	now := time.Now()
	var released int
	for _, e := range resp.Values() {
		var lease storage_v1alpha.DiskLease
		lease.Decode(e.Entity())

		if lease.Status != storage_v1alpha.BOUND && lease.Status != storage_v1alpha.PENDING {
			continue
		}
		if lease.SandboxId == "" {
			continue
		}

		// Defense-in-depth: leave young leases alone so the in-band release
		// paths (and a settling sandbox entity) win any race against the sweep.
		if minLeaseAge > 0 && now.Sub(e.Entity().GetCreatedAt()) < minLeaseAge {
			continue
		}

		sbResp, err := d.EAC.Get(ctx, lease.SandboxId.String())
		var sandboxDead bool
		switch {
		case err == nil:
			var sb compute.Sandbox
			sb.Decode(sbResp.Entity().Entity())
			sandboxDead = sb.Status == compute.DEAD
		case errors.Is(err, cond.ErrNotFound{}):
			sandboxDead = true
		default:
			d.Log.Warn("orphan lease sweep: failed to load sandbox",
				"lease", lease.ID, "sandbox", lease.SandboxId, "error", err)
			continue
		}
		if !sandboxDead {
			continue
		}

		d.Log.Info("orphan lease sweep: releasing lease for dead/missing sandbox",
			"lease", lease.ID,
			"disk", lease.DiskId,
			"sandbox", lease.SandboxId,
			"prior_status", lease.Status,
		)

		_, patchErr := d.EAC.Patch(ctx, entity.New(
			entity.DBId, lease.ID,
			(&storage_v1alpha.DiskLease{
				Status: storage_v1alpha.RELEASED,
			}).Encode,
		).Attrs(), 0)
		if patchErr != nil {
			d.Log.Warn("orphan lease sweep: patch to RELEASED failed",
				"lease", lease.ID, "error", patchErr)
			continue
		}
		released++
	}

	if released > 0 {
		d.Log.Info("orphan lease sweep complete", "released", released)
	}
	return nil
}

// Create handles creation of a new disk lease entity
func (d *DiskLeaseController) Create(ctx context.Context, lease *storage_v1alpha.DiskLease, meta *entity.Meta) error {
	return d.reconcileLease(ctx, "Processing lease creation", lease, meta)
}

// Update handles updates to an existing disk lease entity
func (d *DiskLeaseController) Update(ctx context.Context, lease *storage_v1alpha.DiskLease, meta *entity.Meta) error {
	return d.reconcileLease(ctx, "Processing lease update", lease, meta)
}

// Delete handles deletion of a disk lease entity
func (d *DiskLeaseController) Delete(ctx context.Context, id entity.Id, obj *storage_v1alpha.DiskLease) error {
	d.Log.Info("Processing lease deletion", "lease", id)

	leaseId := id.String()

	// Get lease details before cleaning up
	d.mu.Lock()
	details, hasDetails := d.leaseDetails[leaseId]
	d.mu.Unlock()

	// Clean up disk_mount for this lease
	d.cleanupDiskMountForLease(ctx, id, leaseId)

	// Release the lease from local tracking
	d.mu.Lock()
	defer d.mu.Unlock()

	if hasDetails {
		delete(d.activeLeases, details.diskId)
		delete(d.leaseDetails, leaseId)
		d.Log.Info("Lease released and cleaned up", "lease", id, "disk", details.diskId)
	}

	return nil
}

func (d *DiskLeaseController) cleanupDiskMountForLease(ctx context.Context, leaseId entity.Id, leaseIdStr string) {
	mount, _, err := d.getDiskMountForLease(ctx, leaseId)
	if err != nil {
		d.Log.Warn("error looking up disk_mount for deleted lease", "lease", leaseIdStr, "error", err)
		return
	}
	if mount == nil {
		return
	}

	if mount.ActualState != storage_v1alpha.DM_DETACHED {
		if mount.DesiredState != storage_v1alpha.DM_WANT_UNMOUNTED {
			d.Log.Info("setting disk_mount desired_state to unmounted for deleted lease",
				"lease", leaseIdStr,
				"disk_mount", mount.ID)

			updateAttrs := []entity.Attr{
				entity.Ref(entity.DBId, mount.ID),
				entity.Ref(storage_v1alpha.DiskMountDesiredStateId, storage_v1alpha.DiskMountDesiredStateDmWantUnmountedId),
			}
			if _, err := d.EAC.Patch(ctx, updateAttrs, 0); err != nil {
				d.Log.Warn("failed to update disk_mount desired_state",
					"disk_mount", mount.ID,
					"error", err)
			}
		}
	} else {
		d.Log.Info("deleting disk_mount entity for deleted lease",
			"lease", leaseIdStr,
			"disk_mount", mount.ID)

		if _, err := d.EAC.Delete(ctx, mount.ID.String()); err != nil {
			d.Log.Warn("failed to delete disk_mount entity",
				"disk_mount", mount.ID,
				"error", err)
		}
	}
}

// reconcileLease reconciles the lease state. event names the entity change for
// logging, which happens after the node filter: the controller resyncs every
// lease every minute on every node, so logging before the filter meant each
// node narrating leases it was about to drop, once a minute, forever.
func (d *DiskLeaseController) reconcileLease(ctx context.Context, event string, lease *storage_v1alpha.DiskLease, meta *entity.Meta) error {
	// Only reconcile leases assigned to this node
	if lease.NodeId != "" && !d.NodeId.Matches(lease.NodeId) {
		return nil
	}

	d.Log.Info(event,
		"lease", lease.ID,
		"disk", lease.DiskId,
		"status", lease.Status)

	var err error

	switch lease.Status {
	case storage_v1alpha.PENDING:
		err = d.handlePendingLease(ctx, lease)
	case storage_v1alpha.RELEASED:
		err = d.handleReleasedLease(ctx, lease)
	case storage_v1alpha.BOUND:
		// Verify disk is actually mounted, mount if needed
		err = d.handleBoundLease(ctx, lease)
	case storage_v1alpha.FAILED:
		err = d.handleFailedLease(ctx, lease)
	default:
		d.Log.Warn("Unknown lease status", "lease", lease.ID, "status", lease.Status)
		return nil
	}

	// Update entity attributes if any changes
	if meta != nil {
		// Ensure meta.Entity is initialized
		if meta.Entity == nil {
			meta.Entity = entity.New(lease.Encode())
		} else {
			// Update meta.Entity with the new attributes
			meta.Entity.Update(lease.Encode())
		}
	}

	return err
}

// cleanupLeaseReservation removes a lease reservation (used when binding fails)
func (d *DiskLeaseController) cleanupLeaseReservation(diskId string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.activeLeases, diskId)
}

// handlePendingLease attempts to bind a pending lease via disk_mount entity
func (d *DiskLeaseController) handlePendingLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	diskId := lease.DiskId.String()
	leaseId := lease.ID.String()

	// Check if disk is already leased (with lock)
	d.mu.Lock()
	if existingLease, exists := d.activeLeases[diskId]; exists && existingLease != leaseId {
		d.Log.Info("disk has active lease being released, will retry",
			"disk", diskId,
			"requested_lease", leaseId,
			"existing_lease", existingLease)
		d.mu.Unlock()
		return nil
	}

	// Reserve the lease (or confirm existing reservation)
	d.activeLeases[diskId] = leaseId
	d.mu.Unlock()

	// Get the disk entity to find the volume ID
	diskEntity, err := d.EAC.Get(ctx, diskId)
	if err != nil {
		d.Log.Error("Failed to get disk entity", "disk", diskId, "error", err)
		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf("Failed to get disk entity: %v", err)

		return nil
	}

	// Decode disk entity
	disk := &storage_v1alpha.Disk{}
	disk.Decode(diskEntity.Entity().Entity())
	if disk.ID == "" {
		d.Log.Error("Failed to decode disk entity", "disk", diskId)
		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = "Failed to decode disk entity"

		return nil
	}

	// Check disk provisioning status
	if disk.Status != storage_v1alpha.PROVISIONED {
		if disk.Status == storage_v1alpha.PROVISIONING || disk.Status == storage_v1alpha.RESTORING {
			d.cleanupLeaseReservation(diskId)
			d.Log.Info("Disk is still provisioning, lease will retry",
				"disk", diskId,
				"lease", leaseId,
				"disk_status", disk.Status)
			return nil
		}

		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf("Disk is not provisioned, status: %s", disk.Status)

		return nil
	}

	volumeId := disk.VolumeId
	if volumeId == "" {
		d.cleanupLeaseReservation(diskId)
		d.Log.Info("Disk has no volume ID yet, lease will retry",
			"disk", diskId,
			"lease", leaseId)
		return nil
	}

	// Resolve the volume before touching mount state. A lease with no NodeId
	// (the `debug disk lease` path, and anything created before leases were
	// pinned) is reconciled by every node, and only the node holding the volume
	// can mount it. Deciding ownership up front keeps a non-owner from reading
	// its own stale mount below and driving the shared lease to a terminal
	// FAILED it can never recover from (MIR-1469).
	diskVolume, err := d.getDiskVolumeForDisk(ctx, disk.ID)
	if err != nil {
		d.Log.Error("Failed to look up disk_volume", "disk", diskId, "error", err)
		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf("Failed to look up disk_volume: %v", err)
		return nil
	}

	if diskVolume == nil {
		// Recoverable, not terminal: DiskController recreates a missing volume
		// for a PROVISIONED disk on its next pass, so this is a window to wait
		// out rather than a reason to burn the lease.
		d.cleanupLeaseReservation(diskId)
		d.Log.Info("no disk_volume for disk yet, lease will retry",
			"disk", diskId,
			"lease", leaseId)
		return nil
	}

	// A volume with no node_id predates the stamp (node_id is required by the
	// schema but unenforced, same as on the lease). We can't name its owner,
	// so fall back to the lease's own pin: reaching here means the lease is
	// either pinned to us or pinned to nobody. Pinned to us, we're the
	// designated node and proceed. Pinned to nobody either, and there is no
	// information anywhere about who should mount this, so guessing is how
	// two nodes end up fighting over it. Wait instead; a resync retries.
	if diskVolume.NodeId == "" {
		if lease.NodeId == "" {
			d.cleanupLeaseReservation(diskId)
			d.Log.Warn("neither lease nor disk_volume names a node, cannot determine owner",
				"disk", diskId,
				"lease", leaseId,
				"disk_volume", diskVolume.ID)
			return nil
		}
	} else if !d.NodeId.Matches(diskVolume.NodeId) {
		d.cleanupLeaseReservation(diskId)

		// An unpinned lease reaches every node, and the volume's owner is
		// among them, so stepping aside is the whole job. Quiet by design:
		// this is the common, healthy outcome on every non-owner.
		if lease.NodeId == "" {
			d.Log.Debug("disk_volume belongs to another node, leaving lease to its owner",
				"disk", diskId,
				"lease", leaseId,
				"disk_volume", diskVolume.ID,
				"volume_node", diskVolume.NodeId,
				"my_node", d.NodeId.Id())
			return nil
		}

		// Pinned to us, but the volume lives elsewhere. No other node will
		// pick this up — they all bail at the NodeId filter — so staying quiet
		// would strand the lease in PENDING forever. Fail it with an error
		// that names the split, rather than the "volume not found in state"
		// mount failure this used to surface as.
		d.Log.Error("lease pinned to this node but its volume lives on another",
			"disk", diskId,
			"lease", leaseId,
			"disk_volume", diskVolume.ID,
			"volume_node", diskVolume.NodeId,
			"my_node", d.NodeId.Id())

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf(
			"lease is assigned to node %s but disk_volume %s lives on node %s",
			d.NodeId.Id(), diskVolume.ID, diskVolume.NodeId)
		return nil
	}

	// Check if a disk_mount entity already exists for this lease
	existingMount, _, err := d.getDiskMountForLease(ctx, lease.ID)
	if err != nil {
		d.Log.Warn("Error looking up existing disk_mount", "lease", leaseId, "error", err)
	}

	if existingMount != nil {
		d.Log.Debug("Found existing disk_mount for lease",
			"lease", leaseId,
			"disk_mount", existingMount.ID,
			"actual_state", existingMount.ActualState)

		switch existingMount.ActualState {
		case storage_v1alpha.DM_MOUNTED:
			d.mu.Lock()
			d.leaseDetails[leaseId] = &leaseInfo{
				leaseId:   leaseId,
				diskId:    diskId,
				sandboxId: lease.SandboxId.String(),
				volumeId:  volumeId,
			}
			d.mu.Unlock()

			lease.Status = storage_v1alpha.BOUND
			lease.ErrorMessage = ""
			lease.AcquiredAt = time.Now()

			d.Log.Info("Lease bound via disk_mount entity",
				"lease", leaseId,
				"disk_mount", existingMount.ID)
			return nil

		case storage_v1alpha.DM_ERROR:
			d.Log.Warn("disk_mount in error state",
				"lease", leaseId,
				"disk_mount", existingMount.ID,
				"error", existingMount.ErrorMessage)
			d.cleanupLeaseReservation(diskId)

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Mount failed: %s", existingMount.ErrorMessage)
			return nil

		case storage_v1alpha.DM_DETACHED:
			d.Log.Info("existing disk_mount in DETACHED state, deleting stale mount",
				"lease", leaseId,
				"disk_mount", existingMount.ID)
			if _, err := d.EAC.Delete(ctx, existingMount.ID.String()); err != nil {
				d.Log.Warn("failed to delete stale disk_mount, aborting mount creation",
					"disk_mount", existingMount.ID,
					"error", err)
				d.cleanupLeaseReservation(diskId)
				return nil
			}
			// Fall through to create a new mount entity

		case storage_v1alpha.DM_PENDING, storage_v1alpha.DM_ATTACHING, storage_v1alpha.DM_ATTACHED, storage_v1alpha.DM_MOUNTING, storage_v1alpha.DM_UNMOUNTING, storage_v1alpha.DM_DETACHING:
			// Mount lifecycle still in progress; wait for it to settle.
			fallthrough
		default:
			d.Log.Debug("disk_mount still in progress",
				"lease", leaseId,
				"disk_mount", existingMount.ID,
				"actual_state", existingMount.ActualState)
			return nil
		}
	}

	if diskVolume.ActualState != storage_v1alpha.DV_READY {
		d.cleanupLeaseReservation(diskId)
		d.Log.Info("disk_volume not ready, lease will retry",
			"disk", diskId,
			"disk_volume", diskVolume.ID,
			"actual_state", diskVolume.ActualState)
		return nil
	}

	// Create new disk_mount entity
	mountPath := d.getDiskMountPath(volumeId)

	diskMount := &storage_v1alpha.DiskMount{
		VolumeId:     diskVolume.ID,
		DiskLeaseId:  lease.ID,
		MountPath:    mountPath,
		ReadOnly:     lease.Mount.ReadOnly,
		DesiredState: storage_v1alpha.DM_WANT_MOUNTED,
		ActualState:  storage_v1alpha.DM_PENDING,
		NodeId:       d.NodeId.Id(),
	}

	d.Log.Info("Creating disk_mount entity",
		"lease", leaseId,
		"disk_volume", diskVolume.ID,
		"mount_path", mountPath,
		"read_only", lease.Mount.ReadOnly,
		"node_id", d.NodeId)

	mountId := idgen.GenNS("disk-mnt")
	mountEntityId := entity.Id("disk_mount/" + mountId)
	createAttrs := entity.New(
		entity.DBId, mountEntityId,
		diskMount.Encode,
	).Attrs()

	_, err = d.EAC.Create(ctx, createAttrs)
	if err != nil {
		d.Log.Error("Failed to create disk_mount entity", "error", err)
		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf("Failed to create disk_mount entity: %v", err)
		return nil
	}

	d.Log.Info("Created disk_mount entity, waiting for mount controller to mount",
		"lease", leaseId)

	return nil
}

// handleBoundLease verifies a bound lease has a mounted disk_mount entity
func (d *DiskLeaseController) handleBoundLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	leaseId := lease.ID.String()
	diskId := lease.DiskId.String()

	// First, ensure this bound lease is tracked as active (EAS is source of truth)
	d.mu.Lock()
	currentLease, hasLease := d.activeLeases[diskId]

	if !hasLease || currentLease != leaseId {
		if hasLease && currentLease != leaseId {
			d.mu.Unlock()

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Lease conflict detected, disk %s was leased by %s but now bound to %s", diskId, currentLease, leaseId)

			d.Log.Error("Lease conflict detected when tracking bound lease",
				"disk", diskId,
				"requested_lease", leaseId,
				"existing_lease", currentLease)

			return nil
		}

		d.activeLeases[diskId] = leaseId
		d.leaseDetails[leaseId] = &leaseInfo{
			leaseId:   leaseId,
			diskId:    diskId,
			sandboxId: lease.SandboxId.String(),
		}
	}
	d.mu.Unlock()

	diskMount, foreign, err := d.getDiskMountForLease(ctx, lease.ID)
	if err != nil {
		d.Log.Warn("Error looking up disk_mount for bound lease", "lease", leaseId, "error", err)
		return nil
	}

	if diskMount == nil {
		// An unpinned lease is reconciled here by every node. If another node
		// holds the mount, this lease is bound and healthy from its owner's
		// point of view, and knocking it back to PENDING would just start a
		// BOUND/PENDING flap between the two nodes.
		if foreign {
			d.Log.Debug("Bound lease is mounted by another node, leaving it alone",
				"lease", leaseId)
			return nil
		}

		d.Log.Warn("Bound lease has no disk_mount entity, reverting to pending",
			"lease", leaseId)
		lease.Status = storage_v1alpha.PENDING
		return nil
	}

	d.mu.Lock()
	if details, exists := d.leaseDetails[leaseId]; exists {
		details.volumeId = string(diskMount.VolumeId)
	}
	d.mu.Unlock()

	if diskMount.ActualState != storage_v1alpha.DM_MOUNTED {
		if diskMount.ActualState == storage_v1alpha.DM_ERROR {
			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Mount failed: %s", diskMount.ErrorMessage)
		} else if diskMount.ActualState == storage_v1alpha.DM_DETACHED {
			d.Log.Warn("disk_mount detached for bound lease, reverting to pending",
				"lease", leaseId,
				"disk_mount", diskMount.ID)
			lease.Status = storage_v1alpha.PENDING
		} else {
			d.Log.Debug("disk_mount not yet mounted for bound lease",
				"lease", leaseId,
				"disk_mount", diskMount.ID,
				"actual_state", diskMount.ActualState)
		}
	}

	return nil
}

// handleReleasedLease sets desired_state=DM_WANT_UNMOUNTED on the disk_mount entity
func (d *DiskLeaseController) handleReleasedLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	leaseId := lease.ID.String()
	diskId := lease.DiskId.String()

	// Check if this lease is currently active
	d.mu.Lock()
	currentLease, exists := d.activeLeases[diskId]
	isActiveForThisLease := exists && currentLease == leaseId

	if isActiveForThisLease {
		d.releaseLease(leaseId, diskId)
	}
	d.mu.Unlock()

	if !isActiveForThisLease {
		return nil
	}

	diskMount, _, err := d.getDiskMountForLease(ctx, lease.ID)
	if err != nil {
		d.Log.Warn("Error looking up disk_mount for released lease", "lease", leaseId, "error", err)
		return nil
	}

	if diskMount == nil {
		return nil
	}

	if diskMount.ActualState == storage_v1alpha.DM_DETACHED {
		d.Log.Info("disk_mount already detached, cleaning up",
			"lease", leaseId,
			"disk_mount", diskMount.ID)

		if _, err := d.EAC.Delete(ctx, diskMount.ID.String()); err != nil {
			d.Log.Warn("Failed to delete disk_mount entity",
				"disk_mount", diskMount.ID,
				"error", err)
		}
	} else if diskMount.DesiredState != storage_v1alpha.DM_WANT_UNMOUNTED {
		d.Log.Info("Setting disk_mount desired_state to unmounted",
			"lease", leaseId,
			"disk_mount", diskMount.ID)

		updateAttrs := []entity.Attr{
			entity.Ref(entity.DBId, diskMount.ID),
			entity.Ref(storage_v1alpha.DiskMountDesiredStateId, storage_v1alpha.DiskMountDesiredStateDmWantUnmountedId),
		}
		if _, err := d.EAC.Patch(ctx, updateAttrs, 0); err != nil {
			d.Log.Error("Failed to update disk_mount desired_state",
				"disk_mount", diskMount.ID,
				"error", err)
		}
	} else {
		d.Log.Debug("disk_mount already marked for unmount",
			"lease", leaseId,
			"disk_mount", diskMount.ID,
			"actual_state", diskMount.ActualState)
	}

	return nil
}

// handleFailedLease cleans up the disk_mount entity for a failed lease.
func (d *DiskLeaseController) handleFailedLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	leaseId := lease.ID.String()
	diskId := lease.DiskId.String()

	// Release from active tracking if still tracked
	d.mu.Lock()
	currentLease, exists := d.activeLeases[diskId]
	if exists && currentLease == leaseId {
		d.releaseLease(leaseId, diskId)
	}
	d.mu.Unlock()

	diskMount, _, err := d.getDiskMountForLease(ctx, lease.ID)
	if err != nil {
		d.Log.Warn("Error looking up disk_mount for failed lease", "lease", leaseId, "error", err)
		return nil
	}

	if diskMount == nil {
		return nil
	}

	if diskMount.ActualState == storage_v1alpha.DM_DETACHED {
		d.Log.Info("disk_mount already detached for failed lease, cleaning up",
			"lease", leaseId,
			"disk_mount", diskMount.ID)

		if _, err := d.EAC.Delete(ctx, diskMount.ID.String()); err != nil {
			d.Log.Warn("Failed to delete disk_mount entity",
				"disk_mount", diskMount.ID,
				"error", err)
		}
	} else if diskMount.DesiredState != storage_v1alpha.DM_WANT_UNMOUNTED {
		d.Log.Info("Setting disk_mount desired_state to unmounted for failed lease",
			"lease", leaseId,
			"disk_mount", diskMount.ID)

		updateAttrs := []entity.Attr{
			entity.Ref(entity.DBId, diskMount.ID),
			entity.Ref(storage_v1alpha.DiskMountDesiredStateId, storage_v1alpha.DiskMountDesiredStateDmWantUnmountedId),
		}
		if _, err := d.EAC.Patch(ctx, updateAttrs, 0); err != nil {
			d.Log.Error("Failed to update disk_mount desired_state",
				"disk_mount", diskMount.ID,
				"error", err)
		}
	}

	return nil
}

// releaseLease removes a lease from active tracking (must be called with lock held)
func (d *DiskLeaseController) releaseLease(leaseId, diskId string) {
	if currentLease, exists := d.activeLeases[diskId]; exists && currentLease == leaseId {
		delete(d.activeLeases, diskId)
		delete(d.leaseDetails, leaseId)
		d.Log.Info("Lease released", "lease", leaseId, "disk", diskId)
	}
}

// getDiskMountPath returns the standard mount path for a disk volume
func (d *DiskLeaseController) getDiskMountPath(volumeId string) string {
	return filepath.Join(d.mountBasePath, volumeId)
}

// getDiskMountForLease finds the disk_mount entity for a lease
func (d *DiskLeaseController) getDiskMountForLease(ctx context.Context, leaseId entity.Id) (*storage_v1alpha.DiskMount, bool, error) {
	if d.EAC == nil {
		return nil, false, nil
	}

	indexAttr := entity.Ref(storage_v1alpha.DiskMountDiskLeaseIdId, leaseId)

	resp, err := d.EAC.List(ctx, indexAttr)
	if err != nil {
		return nil, false, fmt.Errorf("failed to list disk_mount entities: %w", err)
	}

	values := resp.Values()
	if len(values) == 0 {
		return nil, false, nil
	}

	// A lease can carry more than one disk_mount when several nodes reconciled
	// it, which is what an unpinned lease invites. Only this node's mount
	// describes state this controller can act on, so match on NodeId rather
	// than trusting index order; picking blind was what let another node's
	// failed mount drive a lease to terminal FAILED (MIR-1469). Mounts
	// predating the NodeId stamp have none, so fall back to those.
	//
	// foreign reports that some other node holds a mount for this lease. A
	// caller that finds nothing of its own needs that to tell "this lease has
	// no mount" apart from "its mount isn't mine", since only the former means
	// the lease has genuinely lost its mount.
	var (
		unowned *storage_v1alpha.DiskMount
		foreign bool
	)

	for _, value := range values {
		var mount storage_v1alpha.DiskMount
		mount.Decode(value.Entity())

		switch {
		case d.NodeId.Matches(mount.NodeId):
			return &mount, false, nil
		case mount.NodeId == "":
			if unowned == nil {
				unowned = &mount
			}
		default:
			foreign = true
		}
	}

	return unowned, unowned == nil && foreign, nil
}

// getDiskVolumeForDisk finds the disk_volume entity for a disk
func (d *DiskLeaseController) getDiskVolumeForDisk(ctx context.Context, diskId entity.Id) (*storage_v1alpha.DiskVolume, error) {
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

	var volume storage_v1alpha.DiskVolume
	volume.Decode(values[0].Entity())

	return &volume, nil
}

// CleanupOldReleasedLeases deletes released leases that haven't been updated for over 1 hour
func (d *DiskLeaseController) CleanupOldReleasedLeases(ctx context.Context) error {
	if d.EAC == nil {
		return nil
	}

	// List all disk lease entities
	ref := entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease)
	results, err := d.EAC.List(ctx, ref)
	if err != nil {
		d.Log.Error("Failed to list disk leases for cleanup", "error", err)
		return err
	}

	now := time.Now()
	cutoffTime := now.Add(-1 * time.Hour) // 1 hour ago
	deletedCount := 0

	for _, e := range results.Values() {
		// Decode the lease to check its status
		var lease storage_v1alpha.DiskLease
		lease.Decode(e.Entity())

		if lease.Status == storage_v1alpha.RELEASED && e.Entity().GetUpdatedAt().Before(cutoffTime) {
			updatedAtTime := e.Entity().GetUpdatedAt()
			age := time.Since(updatedAtTime)
			d.Log.Info("Deleting old released lease",
				"lease", lease.ID,
				"disk", lease.DiskId,
				"age", age.Round(time.Second),
				"updated_at", updatedAtTime.Format(time.RFC3339))

			ec := entityserver.NewClient(d.Log, d.EAC)
			if err := ec.Delete(ctx, lease.ID); err != nil {
				d.Log.Error("Failed to delete old released lease",
					"lease", lease.ID,
					"error", err)
				continue
			}

			deletedCount++
		}
	}

	if deletedCount > 0 {
		d.Log.Info("Cleaned up old released leases", "count", deletedCount)
	}

	return nil
}
