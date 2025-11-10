package disk

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/stream"
)

// leaseInfo tracks active lease details
type leaseInfo struct {
	leaseId   string
	diskId    string
	sandboxId string
	volumeId  string // Store volume ID to avoid lookups during delete
}

// DiskLeaseController manages disk lease entities and exclusive access
//
// Operational flow:
// 1. Disks are created in SegmentAccess when provisioned
// 2. When a lease is bound, the disk is initialized (lsvd.NewDisk), attached to NBD, formatted, and mounted
// 3. Leases control exclusive access to these mounted volumes
// 4. The lease.Mount.Path specifies where to mount within the sandbox's filesystem
type DiskLeaseController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// LSVD client for volume operations (default client)
	lsvdClient LsvdClient

	// Separate clients for local+replica vs remote-only modes
	localReplicaClient LsvdClient
	remoteOnlyClient   LsvdClient

	// Base path for disk mounts (e.g., /var/lib/miren/disks)
	mountBasePath string

	// Track active leases: diskId -> leaseId
	mu           sync.RWMutex
	activeLeases map[string]string
	leaseDetails map[string]*leaseInfo

	// Test-only cache for disk entities (when EAC is not available)
	testDiskCache map[string]*storage_v1alpha.Disk
}

// NewDiskLeaseController creates a new disk lease controller
func NewDiskLeaseController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, lsvdClient LsvdClient) *DiskLeaseController {
	return &DiskLeaseController{
		Log:           log.With("module", "disk-lease"),
		EAC:           eac,
		lsvdClient:    lsvdClient,
		mountBasePath: "/var/lib/miren/disks", // Default mount base path
		activeLeases:  make(map[string]string),
		leaseDetails:  make(map[string]*leaseInfo),
	}
}

// NewDiskLeaseControllerWithClients creates a new disk lease controller with separate clients for local and remote-only modes
func NewDiskLeaseControllerWithClients(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, defaultClient, localReplicaClient, remoteOnlyClient LsvdClient) *DiskLeaseController {
	return &DiskLeaseController{
		Log:                log.With("module", "disk-lease"),
		EAC:                eac,
		lsvdClient:         defaultClient,
		localReplicaClient: localReplicaClient,
		remoteOnlyClient:   remoteOnlyClient,
		mountBasePath:      "/var/lib/miren/disks", // Default mount base path
		activeLeases:       make(map[string]string),
		leaseDetails:       make(map[string]*leaseInfo),
	}
}

// NewDiskLeaseControllerWithMountPath creates a new disk lease controller with custom mount path
func NewDiskLeaseControllerWithMountPath(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, lsvdClient LsvdClient, mountPath string) *DiskLeaseController {
	return &DiskLeaseController{
		Log:           log.With("module", "disk-lease"),
		EAC:           eac,
		lsvdClient:    lsvdClient,
		mountBasePath: mountPath,
		activeLeases:  make(map[string]string),
		leaseDetails:  make(map[string]*leaseInfo),
	}
}

// SetTestDisk is a test helper to set disk information when EAC is not available
func (d *DiskLeaseController) SetTestDisk(disk *storage_v1alpha.Disk) {
	if d.testDiskCache == nil {
		d.testDiskCache = make(map[string]*storage_v1alpha.Disk)
	}
	d.testDiskCache[disk.ID.String()] = disk
}

// GetTestDisk is a test helper to retrieve disk information from test cache
func (d *DiskLeaseController) GetTestDisk(diskId entity.Id) *storage_v1alpha.Disk {
	if d.testDiskCache == nil {
		return nil
	}
	return d.testDiskCache[diskId.String()]
}

// Init initializes the disk lease controller
func (d *DiskLeaseController) Init(ctx context.Context) error {
	// No special initialization needed
	return nil
}

// getClientForDisk returns the appropriate LSVD client based on disk's remote_only flag
func (d *DiskLeaseController) getClientForDisk(disk *storage_v1alpha.Disk) LsvdClient {
	if disk.RemoteOnly && d.remoteOnlyClient != nil {
		return d.remoteOnlyClient
	}
	if !disk.RemoteOnly && d.localReplicaClient != nil {
		return d.localReplicaClient
	}
	return d.lsvdClient
}

// Create handles creation of a new disk lease entity
func (d *DiskLeaseController) Create(ctx context.Context, lease *storage_v1alpha.DiskLease, meta *entity.Meta) error {
	d.Log.Info("Processing lease creation",
		"lease", lease.ID,
		"disk", lease.DiskId,
		"status", lease.Status)

	return d.reconcileLease(ctx, lease, meta)
}

// Update handles updates to an existing disk lease entity
func (d *DiskLeaseController) Update(ctx context.Context, lease *storage_v1alpha.DiskLease, meta *entity.Meta) error {
	d.Log.Info("Processing lease update",
		"lease", lease.ID,
		"disk", lease.DiskId,
		"status", lease.Status)

	return d.reconcileLease(ctx, lease, meta)
}

// Delete handles deletion of a disk lease entity
func (d *DiskLeaseController) Delete(ctx context.Context, id entity.Id) error {
	d.Log.Info("Processing lease deletion", "lease", id)

	// Get lease details before cleaning up
	d.mu.Lock()
	details, hasDetails := d.leaseDetails[id.String()]
	d.mu.Unlock()

	// If we have details and lsvdClient is available, unmount the disk
	if hasDetails && d.lsvdClient != nil {
		volumeId := details.volumeId

		// Only fetch disk info if we don't have the volumeId stored
		if volumeId == "" {
			// Try to get from test cache (for testing)
			disk := d.GetTestDisk(entity.Id(details.diskId))
			if disk != nil && disk.LsvdVolumeId != "" {
				volumeId = disk.LsvdVolumeId
			} else if d.EAC != nil {
				// Try to fetch from EAS
				diskEntity, err := d.EAC.Get(ctx, details.diskId)
				if err != nil {
					d.Log.Warn("Failed to fetch disk during delete",
						"lease", id,
						"disk", details.diskId,
						"error", err)
				} else {
					var disk storage_v1alpha.Disk
					disk.Decode(diskEntity.Entity().Entity())
					if disk.LsvdVolumeId != "" {
						volumeId = disk.LsvdVolumeId
					}
				}
			}
		}

		if volumeId != "" {
			// Check if volume is mounted
			mounted, err := d.lsvdClient.IsVolumeMounted(ctx, volumeId)
			if err != nil {
				d.Log.Warn("Failed to check mount status during delete",
					"lease", id,
					"disk", details.diskId,
					"volume", volumeId,
					"error", err)
			} else if mounted {
				// Unmount the volume
				d.Log.Info("Unmounting disk for deleted lease",
					"lease", id,
					"disk", details.diskId,
					"volume", volumeId)

				if err := d.lsvdClient.UnmountVolume(ctx, volumeId); err != nil {
					d.Log.Error("Failed to unmount volume during lease deletion",
						"lease", id,
						"disk", details.diskId,
						"volume", volumeId,
						"error", err)
					// Continue with cleanup even if unmount fails
				} else {
					d.Log.Info("Successfully unmounted disk",
						"lease", id,
						"disk", details.diskId,
						"volume", volumeId)
				}
			}
		}
	}

	// Release the lease
	d.mu.Lock()
	defer d.mu.Unlock()

	if details, exists := d.leaseDetails[id.String()]; exists {
		delete(d.activeLeases, details.diskId)
		delete(d.leaseDetails, id.String())
		d.Log.Info("Lease released and cleaned up", "lease", id, "disk", details.diskId)
	}

	return nil
}

// reconcileLease reconciles the lease state
func (d *DiskLeaseController) reconcileLease(ctx context.Context, lease *storage_v1alpha.DiskLease, meta *entity.Meta) error {
	var err error

	switch lease.Status {
	case storage_v1alpha.PENDING:
		err = d.handlePendingLease(ctx, lease)
	case storage_v1alpha.RELEASED:
		err = d.handleReleasedLease(ctx, lease)
	case storage_v1alpha.BOUND:
		// Verify disk is actually mounted, mount if needed
		err = d.handleBoundLease(ctx, lease)
		// Update lease details for expiry tracking
		d.updateLeaseDetails(lease)
	case storage_v1alpha.FAILED:
		// Failed state is terminal
		return nil
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

// handlePendingLease attempts to bind a pending lease
func (d *DiskLeaseController) handlePendingLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	// Note: We don't hold the lock for the entire operation since disk operations can be slow
	// We'll lock/unlock as needed for state management

	diskId := lease.DiskId.String()
	leaseId := lease.ID.String()

	// Check if disk is already leased (with lock)
	d.mu.Lock()
	// TODO when we have clustering, we can't do this so we'll need a distributed lock
	// or a CAS in EAS.
	if existingLease, exists := d.activeLeases[diskId]; exists {
		if existingLease != leaseId {
			// Conflict - disk is already leased
			d.Log.Warn("Lease conflict detected",
				"disk", diskId,
				"requested_lease", leaseId,
				"existing_lease", existingLease)

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Disk %s is already leased by %s", diskId, existingLease)
		}

		// Already bound by this lease
		d.mu.Unlock()
		return nil
	}

	// Reserve the lease immediately to prevent races
	// We'll update with full details after mounting
	d.activeLeases[diskId] = leaseId
	d.mu.Unlock()

	// Get the disk entity to find the volume ID
	var disk *storage_v1alpha.Disk

	// Check test cache first (for unit tests)
	if d.testDiskCache != nil {
		if cachedDisk, ok := d.testDiskCache[diskId]; ok {
			disk = cachedDisk
		}
	}

	// If not in test cache, get from EAC
	if disk == nil {
		diskEntity, err := d.EAC.Get(ctx, diskId)
		if err != nil {
			d.Log.Error("Failed to get disk entity", "disk", diskId, "error", err)
			d.cleanupLeaseReservation(diskId)

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Failed to get disk entity: %v", err)

			return nil
		}

		// Decode disk entity
		disk = &storage_v1alpha.Disk{}
		disk.Decode(diskEntity.Entity().Entity())
		if disk.ID == "" {
			d.Log.Error("Failed to decode disk entity", "disk", diskId, "error", err)
			d.cleanupLeaseReservation(diskId)

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Failed to decode disk entity: %v", err)

			return nil
		}
	}

	// Check disk provisioning status
	if disk.Status != storage_v1alpha.PROVISIONED {
		// If disk is still provisioning, wait for it to complete
		if disk.Status == storage_v1alpha.PROVISIONING {
			d.cleanupLeaseReservation(diskId)
			d.Log.Info("Disk is still provisioning, lease will retry",
				"disk", diskId,
				"lease", leaseId,
				"disk_status", disk.Status)
			// Leave lease in PENDING state - it will be reconciled again when disk becomes PROVISIONED
			return nil
		}

		// Disk is in failed or unexpected state
		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf("Disk is not provisioned, status: %s", disk.Status)

		return nil
	}

	volumeId := disk.LsvdVolumeId
	if volumeId == "" {
		d.cleanupLeaseReservation(diskId)

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = "Disk has no associated volume"

		return nil
	}

	// Get the appropriate client for this disk
	client := d.getClientForDisk(disk)

	// Initialize disk and mount if we have an LSVD client
	if client != nil {
		// Initialize disk (calls lsvd.NewDisk)
		filesystem := strings.TrimPrefix(string(disk.Filesystem), "filesystem.")
		if err := client.InitializeDisk(ctx, volumeId, filesystem); err != nil {
			d.Log.Error("Failed to initialize disk", "volume", volumeId, "error", err)
			d.cleanupLeaseReservation(diskId)

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Failed to initialize disk: %v", err)

			return nil
		}

		// Mount the volume
		mountPath := d.getDiskMountPath(volumeId)
		readOnly := lease.Mount.ReadOnly
		if err := client.MountVolume(ctx, volumeId, mountPath, readOnly); err != nil {
			d.Log.Error("Failed to mount volume", "volume", volumeId, "error", err)
			d.cleanupLeaseReservation(diskId)
			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Failed to mount volume: %v", err)

			return nil
		}

		d.Log.Info("Successfully mounted disk volume",
			"disk", diskId,
			"volume", volumeId,
			"mount_path", mountPath,
			"read_only", readOnly)
	}

	// Bind the lease (with lock)
	d.mu.Lock()
	// Double-check that our reservation is still valid
	if existingLease, exists := d.activeLeases[diskId]; exists && existingLease != leaseId {
		d.mu.Unlock()
		// Someone else bound it (shouldn't happen with our reservation), need to clean up
		if client != nil && volumeId != "" {
			if err := client.UnmountVolume(ctx, volumeId); err != nil {
				d.Log.Warn("Failed to unmount volume after lease conflict", "volume", volumeId, "error", err)
			}
		}

		lease.Status = storage_v1alpha.FAILED
		lease.ErrorMessage = fmt.Sprintf("Disk %s is already leased by %s", diskId, existingLease)

		return nil
	}

	// We already reserved the lease earlier, just add the details
	d.leaseDetails[leaseId] = &leaseInfo{
		leaseId:   leaseId,
		diskId:    diskId,
		sandboxId: lease.SandboxId.String(),
		volumeId:  volumeId,
	}
	d.mu.Unlock()

	d.Log.Info("Lease bound successfully",
		"lease", leaseId,
		"disk", diskId,
		"sandbox", lease.SandboxId)

	lease.Status = storage_v1alpha.BOUND
	lease.ErrorMessage = ""
	lease.AcquiredAt = time.Now()

	return nil
}

// handleBoundLease verifies a bound lease has its disk mounted and mounts if necessary
func (d *DiskLeaseController) handleBoundLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	diskId := lease.DiskId.String()
	leaseId := lease.ID.String()

	// First, ensure this bound lease is tracked as active (EAS is source of truth)
	d.mu.Lock()
	currentLease, hasLease := d.activeLeases[diskId]
	var needsSetup bool
	var existingVolumeId string

	if !hasLease || currentLease != leaseId {
		// Either no lease is tracked or a different lease is tracked
		// Since EAS says this lease is BOUND, we should track it
		d.Log.Info("Tracking bound lease from EAS",
			"lease", leaseId,
			"disk", diskId,
			"previous_lease", currentLease)

		// Clean up the old lease details if there was a different lease
		if hasLease && currentLease != leaseId {
			// Rather than try to press on in this precarious situation, let's
			// error out here instead.
			d.mu.Unlock() // Must unlock before returning

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
			volumeId:  "", // Will be filled in below when we get the disk info
		}
		needsSetup = true
	} else {
		// Lease is already tracked - check if we have volume ID and it's mounted
		if details, exists := d.leaseDetails[leaseId]; exists && details.volumeId != "" {
			existingVolumeId = details.volumeId
		}
	}
	d.mu.Unlock()

	// If lease was already tracked and we have a volume ID, check if it's mounted
	// This makes the function idempotent - if everything is already set up, we do nothing
	if !needsSetup && existingVolumeId != "" && d.lsvdClient != nil {
		mounted, err := d.lsvdClient.IsVolumeMounted(ctx, existingVolumeId)
		if err != nil {
			d.Log.Warn("Failed to check mount status for existing lease",
				"volume", existingVolumeId,
				"lease", leaseId,
				"error", err)
			// Continue to recheck setup
		} else if mounted {
			// Everything is already set up correctly, nothing to do
			d.Log.Debug("Bound lease already properly set up",
				"lease", leaseId,
				"disk", diskId,
				"volume", existingVolumeId)
			return nil
		}
		// If not mounted, we'll continue to mount it
		d.Log.Info("Bound lease exists but disk not mounted, will remount",
			"lease", leaseId,
			"disk", diskId,
			"volume", existingVolumeId)
	}

	// If we don't have an LSVD client, we can't verify/fix mounting
	if d.lsvdClient == nil {
		return nil
	}

	// Get the disk entity to find the volume ID
	var disk *storage_v1alpha.Disk

	// Check test cache first (for unit tests)
	if d.testDiskCache != nil {
		if cachedDisk, ok := d.testDiskCache[diskId]; ok {
			disk = cachedDisk
		}
	}

	// If not in test cache, get from EAC
	if disk == nil && d.EAC != nil {
		diskEntity, err := d.EAC.Get(ctx, diskId)
		if err != nil {
			d.Log.Error("Failed to get disk entity for bound lease", "disk", diskId, "error", err)
			return nil // Don't fail the lease, just log
		}

		// Decode disk entity
		disk = &storage_v1alpha.Disk{}
		disk.Decode(diskEntity.Entity().Entity())
		if disk.ID == "" {
			d.Log.Error("Failed to decode disk entity for bound lease", "disk", diskId)
			return nil // Don't fail the lease, just log
		}
	}

	if disk == nil {
		d.Log.Warn("Unable to get disk info for bound lease", "disk", diskId)
		return nil
	}

	volumeId := disk.LsvdVolumeId
	if volumeId == "" {
		d.Log.Warn("Bound lease has disk with no volume ID", "disk", diskId)
		return nil
	}

	// Update the volume ID in lease details if we have it
	d.mu.Lock()
	if details, exists := d.leaseDetails[leaseId]; exists {
		details.volumeId = volumeId
	}
	d.mu.Unlock()

	// Get the appropriate client for this disk
	client := d.getClientForDisk(disk)

	// Check if the volume is mounted
	mounted, err := client.IsVolumeMounted(ctx, volumeId)
	if err != nil {
		d.Log.Error("Failed to check mount status", "volume", volumeId, "error", err)
		return nil // Don't fail the lease
	}

	if !mounted {
		d.Log.Info("Bound lease has unmounted disk, mounting now",
			"lease", leaseId,
			"disk", diskId,
			"volume", volumeId)

		// Initialize disk (calls lsvd.NewDisk) if needed
		filesystem := strings.TrimPrefix(string(disk.Filesystem), "filesystem.")
		if err := client.InitializeDisk(ctx, volumeId, filesystem); err != nil {
			d.Log.Error("Failed to initialize disk for bound lease", "volume", volumeId, "error", err)

			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Failed to initialize disk: %v", err)

			return nil
		}

		// Mount the volume
		mountPath := d.getDiskMountPath(volumeId)
		readOnly := lease.Mount.ReadOnly
		if err := client.MountVolume(ctx, volumeId, mountPath, readOnly); err != nil {
			d.Log.Error("Failed to mount volume for bound lease", "volume", volumeId, "error", err)
			lease.Status = storage_v1alpha.FAILED
			lease.ErrorMessage = fmt.Sprintf("Failed to mount volume: %v", err)

			return nil
		}

		d.Log.Info("Successfully remounted disk volume for bound lease",
			"disk", diskId,
			"volume", volumeId,
			"mount_path", mountPath,
			"read_only", readOnly)
	}

	return nil
}

// handleReleasedLease processes explicit lease release
func (d *DiskLeaseController) handleReleasedLease(ctx context.Context, lease *storage_v1alpha.DiskLease) error {
	leaseId := lease.ID.String()
	diskId := lease.DiskId.String()

	// Check if this lease is currently active
	d.mu.Lock()
	currentLease, exists := d.activeLeases[diskId]
	isActiveForThisLease := exists && currentLease == leaseId

	// Get volumeId from leaseDetails if available
	var volumeId string
	if details, hasDetails := d.leaseDetails[leaseId]; hasDetails {
		volumeId = details.volumeId
	}
	d.mu.Unlock()

	// If this lease is not currently active, it's already been released - nothing to do
	if !isActiveForThisLease {
		return nil
	}

	// If we don't have volumeId from leaseDetails, try to get disk info to find volume ID
	if volumeId == "" && d.EAC != nil {
		diskEntity, err := d.EAC.Get(ctx, diskId)
		if err == nil {
			disk := &storage_v1alpha.Disk{}
			disk.Decode(diskEntity.Entity().Entity())
			if disk.ID != "" {
				volumeId = disk.LsvdVolumeId
			}
		}
	}

	// Unmount the volume if we have one
	if volumeId != "" && d.lsvdClient != nil {
		// Check if volume is actually mounted before trying to unmount
		mounted, err := d.lsvdClient.IsVolumeMounted(ctx, volumeId)
		if err != nil {
			d.Log.Warn("Failed to check mount status on lease release", "volume", volumeId, "error", err)
		} else if mounted {
			d.Log.Info("Unmounting disk volume on lease release", "volume", volumeId, "lease", leaseId)
			if err := d.lsvdClient.UnmountVolume(ctx, volumeId); err != nil {
				// Log but don't fail - best effort unmount
				d.Log.Warn("Failed to unmount volume on lease release", "volume", volumeId, "error", err)
			}
		}
	}

	// Release the lease
	d.mu.Lock()
	defer d.mu.Unlock()

	d.releaseLease(leaseId, diskId)
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

// updateLeaseDetails updates lease information
func (d *DiskLeaseController) updateLeaseDetails(lease *storage_v1alpha.DiskLease) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Currently just ensures the lease is tracked
	// Could be extended to update other lease details if needed
	_ = d.leaseDetails[lease.ID.String()]
}

// getDiskMountPath returns the standard mount path for a disk volume
func (d *DiskLeaseController) getDiskMountPath(volumeId string) string {
	return filepath.Join(d.mountBasePath, volumeId)
}

// CleanupOldReleasedLeases deletes released leases that haven't been updated for over 1 hour
func (d *DiskLeaseController) CleanupOldReleasedLeases(ctx context.Context) error {
	if d.EAC == nil {
		// No EAC available (test mode), skip cleanup
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

		// Only delete if:
		// 1. Status is RELEASED
		// 2. UpdatedAt is more than 1 hour ago
		if lease.Status == storage_v1alpha.RELEASED && e.Entity().GetUpdatedAt().Before(cutoffTime) {
			updatedAtTime := e.Entity().GetUpdatedAt()
			age := time.Since(updatedAtTime)
			d.Log.Info("Deleting old released lease",
				"lease", lease.ID,
				"disk", lease.DiskId,
				"age", age.Round(time.Second),
				"updated_at", updatedAtTime.Format(time.RFC3339))

			// Use entity server client to delete the entity
			ec := entityserver.NewClient(d.Log, d.EAC)
			if err := ec.Delete(ctx, lease.ID); err != nil {
				d.Log.Error("Failed to delete old released lease",
					"lease", lease.ID,
					"error", err)
				// Continue with other leases even if one fails
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

// Start starts the disk lease controller
func (d *DiskLeaseController) Start(ctx context.Context) error {
	// Create reconcile controller for lease entities using AdaptController
	// Use a 10-second resync period to check pending leases waiting for disk provisioning
	rc := controller.NewReconcileController(
		"disk-lease",
		d.Log,
		entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease),
		d.EAC,
		controller.AdaptController(d),
		10*time.Second, // Resync period for pending leases waiting for disk provisioning
		1,              // Single worker
	)

	// Set up periodic cleanup of old released leases (every 5 minutes)
	rc.SetPeriodic(5*time.Minute, func(ctx context.Context) error {
		return d.CleanupOldReleasedLeases(ctx)
	})

	// Start a separate watcher for disk entities to reconcile dependent leases
	go d.watchDiskChanges(ctx, rc)

	return rc.Start(ctx)
}

// watchDiskChanges watches for disk state changes and triggers reconciliation of dependent leases
func (d *DiskLeaseController) watchDiskChanges(ctx context.Context, rc *controller.ReconcileController) {
	d.Log.Info("Starting disk change watcher")
	defer d.Log.Info("Disk change watcher stopped")

	// Watch disk entities
	_, err := d.EAC.WatchIndex(ctx, entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		// Only care about updates (state changes)
		if op.OperationType() != entityserver_v1alpha.EntityOperationUpdate {
			return nil
		}

		diskId := entity.Id(op.EntityId())
		d.Log.Debug("Disk state changed, finding dependent leases", "disk", diskId)

		// Find all leases that reference this disk
		listResp, err := d.EAC.List(ctx, entity.Ref(entity.EntityKind, storage_v1alpha.KindDiskLease))
		if err != nil {
			d.Log.Error("failed to list disk leases for disk change", "disk", diskId, "error", err)
			return nil
		}

		// Reconcile each lease that references this disk
		reconciledCount := 0
		for _, e := range listResp.Values() {
			var lease storage_v1alpha.DiskLease
			lease.Decode(e.Entity())

			if lease.DiskId == diskId {
				d.Log.Debug("Reconciling lease due to disk state change",
					"disk", diskId,
					"lease", lease.ID,
					"leaseStatus", lease.Status)

				// Trigger reconciliation by creating an event
				ev := controller.Event{
					Type: controller.EventUpdated,
					Id:   lease.ID,
				}

				// Add to the reconcile controller's work queue
				rc.Enqueue(ev)
				reconciledCount++
			}
		}

		if reconciledCount > 0 {
			d.Log.Info("Triggered reconciliation of leases due to disk state change",
				"disk", diskId,
				"leaseCount", reconciledCount)
		}

		return nil
	}))

	if err != nil {
		d.Log.Error("Failed to watch disk changes", "error", err)
	}
}
