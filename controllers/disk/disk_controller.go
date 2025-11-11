package disk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// DiskController manages disk entities and their lifecycle
type DiskController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// LSVD client for volume operations (default client)
	lsvdClient LsvdClient

	// Separate clients for local+replica vs remote-only modes
	localReplicaClient LsvdClient
	remoteOnlyClient   LsvdClient

	// Base path for disk mounts (e.g., /var/lib/miren/disks)
	mountBasePath string
}

// NewDiskController creates a new disk controller
func NewDiskController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, lsvdClient LsvdClient) *DiskController {
	return &DiskController{
		Log:           log.With("module", "disk"),
		EAC:           eac,
		lsvdClient:    lsvdClient,
		mountBasePath: "/var/lib/miren/disks", // Default mount base path
	}
}

// NewDiskControllerWithClients creates a new disk controller with separate clients for local and remote-only modes
func NewDiskControllerWithClients(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, defaultClient, localReplicaClient, remoteOnlyClient LsvdClient) *DiskController {
	return &DiskController{
		Log:                log.With("module", "disk"),
		EAC:                eac,
		lsvdClient:         defaultClient,
		localReplicaClient: localReplicaClient,
		remoteOnlyClient:   remoteOnlyClient,
		mountBasePath:      "/var/lib/miren/disks", // Default mount base path
	}
}

// NewDiskControllerWithMountPath creates a new disk controller with custom mount path
func NewDiskControllerWithMountPath(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, lsvdClient LsvdClient, mountPath string) *DiskController {
	return &DiskController{
		Log:           log.With("module", "disk"),
		EAC:           eac,
		lsvdClient:    lsvdClient,
		mountBasePath: mountPath,
	}
}

// Init initializes the disk controller
func (d *DiskController) Init(ctx context.Context) error {
	// No special initialization needed
	return nil
}

// getClientForDisk returns the appropriate LSVD client based on disk's remote_only flag
func (d *DiskController) getClientForDisk(disk *storage_v1alpha.Disk) LsvdClient {
	if disk.RemoteOnly && d.remoteOnlyClient != nil {
		return d.remoteOnlyClient
	}
	if !disk.RemoteOnly && d.localReplicaClient != nil {
		return d.localReplicaClient
	}
	return d.lsvdClient
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
	d.Log.Info("Processing disk update",
		"disk", disk.ID,
		"status", disk.Status)

	return d.reconcileDisk(ctx, disk, meta)
}

// Delete handles deletion of a disk entity
func (d *DiskController) Delete(ctx context.Context, id entity.Id) error {
	d.Log.Info("Processing disk deletion", "disk", id)
	// Deletion is handled through the DELETING status in reconcileDisk
	return nil
}

// reconcileDisk reconciles the disk state
func (d *DiskController) reconcileDisk(ctx context.Context, disk *storage_v1alpha.Disk, meta *entity.Meta) error {
	var err error

	switch disk.Status {
	case storage_v1alpha.PROVISIONED:
		// Verify the disk is actually provisioned and mounted
		err = d.handleProvisioned(ctx, disk)
	case storage_v1alpha.PROVISIONING:
		err = d.handleProvisioning(ctx, disk)
	case storage_v1alpha.DELETING:
		err = d.handleDeletion(ctx, disk)
	case storage_v1alpha.ATTACHED, storage_v1alpha.DETACHED:
		// These states are managed by disk lease controller
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

// handleProvisioning provisions a new LSVD volume or attaches to an existing one
func (d *DiskController) handleProvisioning(ctx context.Context, disk *storage_v1alpha.Disk) error {
	// Check if user specified an existing volume ID to attach to
	if disk.LsvdVolumeId != "" {
		// Attach mode: verify the volume exists in SegmentAccess
		return d.attachToExistingVolume(ctx, disk)
	}

	// Create mode: provision a new volume
	volumeId, err := d.provisionVolume(ctx, disk)
	if err != nil {
		d.Log.Error("Failed to provision volume", "disk", disk.ID, "error", err)
		// We don't set the status to error here so that in the future, we can retry
		// error is a terminal state.
		return nil
	}

	// Update status to PROVISIONED (volume exists in SegmentAccess but not mounted)
	disk.Status = storage_v1alpha.PROVISIONED
	disk.LsvdVolumeId = volumeId

	d.Log.Info("Disk provisioned successfully",
		"disk", disk.ID,
		"volume", volumeId)

	return nil
}

// attachToExistingVolume attaches a disk entity to an existing LSVD volume
func (d *DiskController) attachToExistingVolume(ctx context.Context, disk *storage_v1alpha.Disk) error {
	volumeId := disk.LsvdVolumeId

	d.Log.Info("Attaching disk to existing volume",
		"disk", disk.ID,
		"volume", volumeId)

	// Get the appropriate client for this disk
	client := d.getClientForDisk(disk)
	if client == nil {
		return fmt.Errorf("no LSVD client available for disk %s", disk.ID)
	}

	// Verify the volume exists in SegmentAccess
	volumeInfo, err := client.GetVolumeInfo(ctx, volumeId)
	if err != nil {
		d.Log.Error("Failed to attach to volume - volume not found",
			"disk", disk.ID,
			"volume", volumeId,
			"error", err)
		return fmt.Errorf("volume %s does not exist in SegmentAccess: %w", volumeId, err)
	}

	d.Log.Info("Verified existing volume",
		"disk", disk.ID,
		"volume", volumeId,
		"volume_size", volumeInfo.SizeBytes)

	// Update status to PROVISIONED
	disk.Status = storage_v1alpha.PROVISIONED

	d.Log.Info("Disk attached to existing volume successfully",
		"disk", disk.ID,
		"volume", volumeId)

	return nil
}

// handleProvisioned verifies a provisioned disk exists in SegmentAccess
func (d *DiskController) handleProvisioned(ctx context.Context, disk *storage_v1alpha.Disk) error {
	// Check if volume ID exists
	if disk.LsvdVolumeId == "" {
		d.Log.Warn("Provisioned disk has no volume ID, re-provisioning", "disk", disk.ID)
		// Re-provision the disk
		return d.handleProvisioning(ctx, disk)
	}

	// Get the appropriate client for this disk
	client := d.getClientForDisk(disk)
	if client == nil {
		return nil
	}

	// Verify the volume exists in SegmentAccess (not checking mount status)
	volumeInfo, err := client.GetVolumeInfo(ctx, disk.LsvdVolumeId)
	if err != nil {
		d.Log.Warn("Volume not found for provisioned disk, re-provisioning",
			"disk", disk.ID,
			"volume", disk.LsvdVolumeId,
			"error", err)
		// Clear the volume ID so handleProvisioning creates a new volume
		disk.LsvdVolumeId = ""
		// Volume doesn't exist, need to re-provision
		return d.handleProvisioning(ctx, disk)
	}

	d.Log.Debug("Provisioned disk volume exists",
		"disk", disk.ID,
		"volume", disk.LsvdVolumeId,
		"status", volumeInfo.Status)

	// No updates needed - disk is provisioned and volume exists
	return nil
}

// handleDeletion deletes the LSVD volume and removes the disk entity
func (d *DiskController) handleDeletion(ctx context.Context, disk *storage_v1alpha.Disk) error {
	// Note: Unmounting is handled by the Disk Lease Controller when releasing leases
	// We only need to unprovision the volume from SegmentAccess

	// Unprovision the volume if it exists
	if disk.LsvdVolumeId != "" {
		if err := d.deleteVolumeData(ctx, disk.LsvdVolumeId); err != nil {
			d.Log.Error("Failed to unprovision volume", "disk", disk.ID, "volume", disk.LsvdVolumeId, "error", err)
			disk.Status = storage_v1alpha.ERROR
			return nil
		}
	}

	// Delete the disk entity
	if d.EAC != nil {
		if _, err := d.EAC.Delete(ctx, disk.ID.String()); err != nil {
			d.Log.Error("Failed to delete disk entity", "disk", disk.ID, "error", err)
			return err
		}
	}

	return nil
}

// provisionVolume creates a new LSVD volume in SegmentAccess only
func (d *DiskController) provisionVolume(ctx context.Context, disk *storage_v1alpha.Disk) (string, error) {
	// Validate disk size
	if disk.SizeGb <= 0 {
		return "", fmt.Errorf("invalid disk size: %d GB", disk.SizeGb)
	}

	// Generate volume ID
	volumeId := idgen.Gen("lsvd-vol-")

	// Get the appropriate client for this disk
	client := d.getClientForDisk(disk)

	// Create volume in SegmentAccess only (no disk initialization)
	if client != nil {
		d.Log.Info("Creating LSVD volume in SegmentAccess",
			"volume", volumeId,
			"size_gb", disk.SizeGb,
			"filesystem", disk.Filesystem,
			"remote_only", disk.RemoteOnly)

		err := client.CreateVolumeInSegmentAccess(ctx, volumeId, disk.SizeGb, strings.TrimPrefix(string(disk.Filesystem), "filesystem."))
		if err != nil {
			return "", fmt.Errorf("failed to create LSVD volume in SegmentAccess: %w", err)
		}
	} else {
		// Fallback for testing without LSVD
		d.Log.Info("Creating mock LSVD volume (no client)",
			"volume", volumeId,
			"size_gb", disk.SizeGb,
			"filesystem", disk.Filesystem)
	}

	return volumeId, nil
}

// deleteVolumeData unprovisions an LSVD volume
func (d *DiskController) deleteVolumeData(ctx context.Context, volumeId string) error {
	d.Log.Warn("Unable to delete volume data currently, segment delete not implemented", "volume", volumeId)
	return nil
}

// Close gracefully shuts down the disk controller
func (d *DiskController) Close() error {
	d.Log.Info("Shutting down disk controller")

	// Close all LSVD clients
	var errs []error

	// Close default client
	if closer, ok := d.lsvdClient.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			d.Log.Error("Failed to close default LSVD client", "error", err)
			errs = append(errs, err)
		}
	}

	// Close local+replica client if different from default
	if d.localReplicaClient != nil && d.localReplicaClient != d.lsvdClient {
		if closer, ok := d.localReplicaClient.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				d.Log.Error("Failed to close local+replica LSVD client", "error", err)
				errs = append(errs, err)
			}
		}
	}

	// Close remote-only client
	if d.remoteOnlyClient != nil {
		if closer, ok := d.remoteOnlyClient.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				d.Log.Error("Failed to close remote-only LSVD client", "error", err)
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}

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
