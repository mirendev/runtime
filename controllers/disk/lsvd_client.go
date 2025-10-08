package disk

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/lsvd/pkg/ext4"
	"miren.dev/runtime/lsvd/pkg/nbd"
	"miren.dev/runtime/lsvd/pkg/nbdnl"
	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/units"
)

// VolumeStatus represents the state of a volume
type VolumeStatus string

const (
	// VolumeStatusNotFound indicates the volume doesn't exist
	VolumeStatusNotFound VolumeStatus = "not_found"
	// VolumeStatusOnDisk indicates the volume exists on disk but is not loaded in memory
	VolumeStatusOnDisk VolumeStatus = "on_disk"
	// VolumeStatusLoaded indicates the volume is loaded in memory but not mounted
	VolumeStatusLoaded VolumeStatus = "loaded"
	// VolumeStatusMounted indicates the volume is mounted and accessible
	VolumeStatusMounted VolumeStatus = "mounted"
)

// LsvdClient provides an interface for LSVD volume operations
type LsvdClient interface {
	// CreateVolume creates a new LSVD volume (backwards compatibility - calls both CreateVolumeInSegmentAccess and InitializeDisk)
	CreateVolume(ctx context.Context, volumeId string, sizeGb int64, filesystem string) error

	// CreateVolumeInSegmentAccess creates a volume only in SegmentAccess (no disk initialization)
	CreateVolumeInSegmentAccess(ctx context.Context, volumeId string, sizeGb int64, filesystem string) error

	// InitializeDisk initializes lsvd.NewDisk for an existing volume in SegmentAccess
	InitializeDisk(ctx context.Context, volumeId string, filesystem string) error

	// UnprovisionVolume unprovisions an LSVD volume (does not delete data)
	UnprovisionVolume(ctx context.Context, volumeId string) error

	// MountVolume mounts an LSVD volume to a mount path (requires InitializeDisk to be called first)
	MountVolume(ctx context.Context, volumeId string, mountPath string, readOnly bool) error

	// UnmountVolume unmounts an LSVD volume
	UnmountVolume(ctx context.Context, volumeId string) error

	// IsVolumeMounted checks if a volume is currently mounted
	IsVolumeMounted(ctx context.Context, volumeId string) (bool, error)

	// GetVolumeInfo returns information about a volume
	GetVolumeInfo(ctx context.Context, volumeId string) (*VolumeInfo, error)

	// ListVolumes lists all volumes
	ListVolumes(ctx context.Context) ([]string, error)
}

// VolumeInfo contains information about an LSVD volume
type VolumeInfo struct {
	ID         string
	Name       string
	SizeBytes  int64
	Filesystem string
	MountPath  string
	UUID       string
	Status     VolumeStatus
}

// lsvdClientImpl is the concrete implementation of LsvdClient
type lsvdClientImpl struct {
	log      *slog.Logger
	dataPath string

	mu      sync.RWMutex
	volumes map[string]*volumeState
	disks   map[string]*lsvd.Disk

	// Optional auth client and cloud URL for replication
	authClient    *cloudauth.AuthClient
	cloudURL      string
	enableReplica bool
	remoteOnly    bool // Use only remote storage, no local replica
}

// LsvdClientOption is a functional option for configuring LsvdClient
type LsvdClientOption func(*lsvdClientImpl)

// WithReplica enables replication to a remote DiskAPI endpoint
func WithReplica(authClient *cloudauth.AuthClient, cloudURL string) LsvdClientOption {
	return func(c *lsvdClientImpl) {
		c.authClient = authClient
		c.cloudURL = cloudURL
		c.enableReplica = authClient != nil
	}
}

// WithRemoteOnly configures the client to use only remote storage (no local replica)
func WithRemoteOnly(authClient *cloudauth.AuthClient, cloudURL string) LsvdClientOption {
	return func(c *lsvdClientImpl) {
		c.authClient = authClient
		c.cloudURL = cloudURL
		c.remoteOnly = true
	}
}

// volumeState tracks the state of a volume
type volumeState struct {
	info       VolumeInfo
	disk       *lsvd.Disk
	mounted    bool
	sa         lsvd.SegmentAccess
	nbdIndex   uint32
	nbdConn    net.Conn
	nbdCleanup func() error
	nbdCancel  context.CancelFunc
	devicePath string
}

// NewLsvdClient creates a new LSVD client
func NewLsvdClient(log *slog.Logger, dataPath string, opts ...LsvdClientOption) LsvdClient {
	client := &lsvdClientImpl{
		log:      log.With("module", "lsvd"),
		dataPath: dataPath,
		volumes:  make(map[string]*volumeState),
		disks:    make(map[string]*lsvd.Disk),
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	return client
}

// NewLsvdClientWithReplica creates a new LSVD client with DiskAPI replication
// Deprecated: Use NewLsvdClient with WithReplica option instead
func NewLsvdClientWithReplica(log *slog.Logger, dataPath string, authClient *cloudauth.AuthClient, cloudURL string) LsvdClient {
	return NewLsvdClient(log, dataPath, WithReplica(authClient, cloudURL))
}

// CreateVolume creates a new LSVD volume (backwards compatibility)
// This method calls both CreateVolumeInSegmentAccess and InitializeDisk
func (c *lsvdClientImpl) CreateVolume(ctx context.Context, volumeId string, sizeGb int64, filesystem string) error {
	// First create the volume in SegmentAccess (without locking since the called methods handle it)
	if err := c.CreateVolumeInSegmentAccess(ctx, volumeId, sizeGb, filesystem); err != nil {
		return err
	}

	// Then initialize the disk
	if err := c.InitializeDisk(ctx, volumeId, filesystem); err != nil {
		return err
	}

	return nil
}

// CreateVolumeInSegmentAccess creates a volume only in SegmentAccess without initializing the disk
func (c *lsvdClientImpl) CreateVolumeInSegmentAccess(ctx context.Context, volumeId string, sizeGb int64, filesystem string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var sa lsvd.SegmentAccess

	// If remoteOnly mode, use only DiskAPI without local storage
	if c.remoteOnly {
		if c.authClient == nil {
			return fmt.Errorf("remoteOnly mode requires auth client")
		}

		c.log.Info("Creating volume in remote-only mode",
			"volume", volumeId,
			"cloud_url", c.cloudURL)

		sa = lsvd.NewDiskAPISegmentAccess(c.log, c.cloudURL, c.authClient)
	} else {
		// Create volume directory
		volumePath := c.getVolumePath(volumeId)
		if err := os.MkdirAll(volumePath, 0755); err != nil {
			return fmt.Errorf("failed to create volume directory: %w", err)
		}

		// Create primary local segment access
		localSA := &lsvd.LocalFileAccess{
			Dir: volumePath,
			Log: c.log,
		}

		sa = localSA

		// If replication is enabled, wrap with ReplicaWriter
		if c.enableReplica && c.authClient != nil {
			c.log.Info("Creating volume with DiskAPI replication",
				"volume", volumeId,
				"cloud_url", c.cloudURL)

			// Create replica using DiskAPI with auth client
			replicaSA := lsvd.NewDiskAPISegmentAccess(c.log, c.cloudURL, c.authClient)

			// Wrap with ReplicaWriter
			sa = lsvd.ReplicaWriter(c.log, localSA, replicaSA)
		}
	}

	// Initialize container
	if err := sa.InitContainer(ctx); err != nil {
		return fmt.Errorf("failed to init container: %w", err)
	}

	// Check if volume already exists in segment access
	existingInfo, err := sa.GetVolumeInfo(ctx, volumeId)
	if err == nil && existingInfo != nil {
		// Volume already exists in storage, reuse it
		c.log.Info("Volume already exists in storage, reusing",
			"volume", volumeId,
			"uuid", existingInfo.UUID,
			"size", existingInfo.Size)

		// Update size if requested size is different
		if existingInfo.Size != units.GigaBytes(sizeGb).Bytes() {
			c.log.Warn("Requested size differs from existing volume",
				"requested", units.GigaBytes(sizeGb).Bytes(),
				"existing", existingInfo.Size)
		}
		return nil // Success - volume exists
	}

	// Volume doesn't exist, create it with a new UUID
	u, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate volume UUID: %w", err)
	}

	// Create volume info with UUID
	volumeInfo := &lsvd.VolumeInfo{
		Name: volumeId,
		Size: units.GigaBytes(sizeGb).Bytes(),
		UUID: u.String(),
	}

	// Initialize volume
	if err := sa.InitVolume(ctx, volumeInfo); err != nil {
		return fmt.Errorf("failed to init volume: %w", err)
	}

	if c.remoteOnly {
		c.log.Info("Created LSVD volume in SegmentAccess (remote-only)",
			"volume", volumeId,
			"uuid", u.String(),
			"size", volumeInfo.Size)
	} else {
		volumePath := c.getVolumePath(volumeId)
		c.log.Info("Created LSVD volume in SegmentAccess",
			"volume", volumeId,
			"uuid", u.String(),
			"size", volumeInfo.Size,
			"path", volumePath)
	}

	return nil
}

// InitializeDisk initializes lsvd.NewDisk for an existing volume in SegmentAccess
func (c *lsvdClientImpl) InitializeDisk(ctx context.Context, volumeId string, filesystem string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already initialized
	if state, exists := c.volumes[volumeId]; exists {
		if state.disk != nil {
			// Already initialized
			return nil
		}
		// State exists but disk is not initialized, continue to initialize
	}

	var sa lsvd.SegmentAccess
	var replicaSA lsvd.SegmentAccess
	var volumePath string

	// If remoteOnly mode, use only DiskAPI without local storage
	if c.remoteOnly {
		if c.authClient == nil {
			return fmt.Errorf("remoteOnly mode requires auth client")
		}

		c.log.Info("Initializing disk in remote-only mode",
			"volume", volumeId,
			"cloud_url", c.cloudURL)

		sa = lsvd.NewDiskAPISegmentAccess(c.log, c.cloudURL, c.authClient)
		// Use a temporary path for remote-only volumes (no actual local storage)
		volumePath = filepath.Join(c.dataPath, "lsvd-cache", volumeId)
		if err := os.MkdirAll(volumePath, 0755); err != nil {
			return fmt.Errorf("failed to create cache directory: %w", err)
		}
	} else {
		// Get volume path
		volumePath = c.getVolumePath(volumeId)

		// Create segment access to check if volume exists
		localSA := &lsvd.LocalFileAccess{
			Dir: volumePath,
			Log: c.log,
		}

		sa = localSA

		// If replication is enabled, wrap with ReplicaWriter
		if c.enableReplica && c.authClient != nil {
			// Create replica using DiskAPI with auth client
			replicaSA = lsvd.NewDiskAPISegmentAccess(c.log, c.cloudURL, c.authClient)
			sa = lsvd.ReplicaWriter(c.log, localSA, replicaSA)
		}
	}

	// Get volume info from segment access
	volumeInfo, err := sa.GetVolumeInfo(ctx, volumeId)
	if err != nil {
		return fmt.Errorf("volume %s not found in segment access: %w", volumeId, err)
	}

	// Create disk instance
	disk, err := lsvd.NewDisk(ctx, c.log, volumePath,
		lsvd.WithVolumeName(volumeId),
		lsvd.WithSegmentAccess(sa),
		lsvd.EnableAutoGC,
	)
	if err != nil {
		return fmt.Errorf("failed to create disk: %w", err)
	}

	// Convert size to int64
	sizeBytes := volumeInfo.Size.Bytes().Int64()
	if sizeBytes == 0 {
		return fmt.Errorf("volume size is zero")
	}

	// Create or update volume state
	if state, exists := c.volumes[volumeId]; exists {
		// Update existing state
		state.disk = disk
		state.sa = sa
		state.info.SizeBytes = sizeBytes
		state.info.UUID = volumeInfo.UUID
		state.info.Status = VolumeStatusLoaded // Update status to loaded
		if filesystem != "" {
			state.info.Filesystem = filesystem
		}
	} else {
		// Create new state
		c.volumes[volumeId] = &volumeState{
			info: VolumeInfo{
				ID:         volumeId,
				Name:       volumeInfo.Name,
				SizeBytes:  sizeBytes,
				Filesystem: filesystem,
				UUID:       volumeInfo.UUID,
				Status:     VolumeStatusLoaded,
			},
			disk: disk,
			sa:   sa,
		}
	}

	// Add to disks map
	c.disks[volumeId] = disk

	c.log.Info("Initialized disk for volume",
		"volume_id", volumeId,
		"uuid", volumeInfo.UUID,
		"size_bytes", sizeBytes,
		"filesystem", filesystem,
		"remote_only", c.remoteOnly)

	// Start background reconciliation if using replica (not for remoteOnly mode)
	if !c.remoteOnly && c.enableReplica && replicaSA != nil {
		// Extract localSA from the ReplicaWriter wrapper
		// Since we created localSA earlier, we need to pass it to reconciliation
		// However, in remoteOnly mode, there's no localSA to reconcile
		go c.reconcileVolume(context.Background(), volumeId, sa, replicaSA)
	}

	return nil
}

// UnprovisionVolume unprovisions an LSVD volume (does not delete data)
func (c *lsvdClientImpl) UnprovisionVolume(ctx context.Context, volumeId string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.volumes[volumeId]
	if !exists {
		// Nothing to unprovision!
		return nil
	}

	// Check actual mount state from system
	actuallyMounted := state.info.MountPath != "" && c.isMounted(state.info.MountPath)

	// Update state if mount status has changed
	if actuallyMounted != state.mounted {
		c.log.Info("Mount status mismatch during unprovision, updating state",
			"volume_id", volumeId,
			"state_mounted", state.mounted,
			"actually_mounted", actuallyMounted)
		state.mounted = actuallyMounted
		if actuallyMounted {
			// System says it's mounted even though we think it's not
			state.info.Status = VolumeStatusMounted
		}
	}

	// Unmount if mounted
	if state.mounted {
		c.log.Info("Unmounting volume before deletion", "volume_id", volumeId)

		// Unmount filesystem
		if err := unix.Unmount(state.info.MountPath, unix.MNT_FORCE); err != nil {
			c.log.Error("Failed to unmount during delete", "error", err)
		}

		// Cleanup NBD
		if state.nbdCleanup != nil {
			state.nbdCleanup()
		}
		if state.nbdCancel != nil {
			state.nbdCancel()
		}

		state.mounted = false
	}

	// Close disk
	if state.disk != nil {
		if err := state.disk.Close(ctx); err != nil {
			c.log.Error("Failed to close disk", "error", err)
		}
	}

	// Remove from state
	delete(c.volumes, volumeId)
	delete(c.disks, volumeId)

	c.log.Info("Unprovisioned LSVD volume", "volume_id", volumeId)

	return nil
}

// MountVolume mounts an LSVD volume to a mount path
func (c *lsvdClientImpl) MountVolume(ctx context.Context, volumeId string, mountPath string, readOnly bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.volumes[volumeId]
	if !exists {
		// Volume must exist in cache - caller should use CreateVolume first
		return fmt.Errorf("volume %s not found in cache - call CreateVolume first", volumeId)
	}

	// Check actual mount state from system and update our state
	actuallyMounted := false
	actualMountPath := ""
	if state.info.MountPath != "" && c.isMounted(state.info.MountPath) {
		actuallyMounted = true
		actualMountPath = state.info.MountPath
	} else if mountPath != "" && c.isMounted(mountPath) {
		// Check if already mounted at the requested location
		actuallyMounted = true
		actualMountPath = mountPath
	}

	// Update state if mount status has changed
	if actuallyMounted != state.mounted {
		c.log.Info("Mount status mismatch, updating state",
			"volume_id", volumeId,
			"state_mounted", state.mounted,
			"actually_mounted", actuallyMounted,
			"actual_path", actualMountPath)
		state.mounted = actuallyMounted
		state.info.MountPath = actualMountPath
		state.info.MountPath = actualMountPath
		if actuallyMounted {
			state.info.Status = VolumeStatusMounted
		} else {
			state.info.Status = VolumeStatusLoaded
		}
	}

	// Check if already mounted based on actual state
	if state.mounted {
		// Already mounted
		if state.info.MountPath == mountPath {
			return nil
		}
		return fmt.Errorf("volume %s is already mounted at %s", volumeId, state.info.MountPath)
	}

	// Disk must be initialized by CreateVolume
	if state.disk == nil {
		return fmt.Errorf("disk not initialized for volume %s - call CreateVolume first", volumeId)
	}

	// Create mount directory
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return fmt.Errorf("failed to create mount directory: %w", err)
	}

	// Check if NBD device is already attached and still connected
	var devicePath string
	needNewNBD := true

	if state.devicePath != "" && state.nbdCleanup != nil {
		// Check if the NBD device is still connected
		status, err := nbdnl.Status(state.nbdIndex)
		if err == nil && status.Connected {
			// NBD device is still connected, reuse it
			devicePath = state.devicePath
			needNewNBD = false
			c.log.Info("Reusing existing NBD device",
				"volume_id", volumeId,
				"device", devicePath,
				"nbd_index", state.nbdIndex)
		} else {
			// NBD device is not connected anymore, clean up old state
			c.log.Info("NBD device no longer connected, will attach new one",
				"volume_id", volumeId,
				"old_device", state.devicePath,
				"nbd_index", state.nbdIndex,
				"status_error", err)
			if state.nbdCleanup != nil {
				state.nbdCleanup()
				state.nbdCleanup = nil
			}
			state.devicePath = ""
			state.nbdIndex = 0
		}
	}

	if needNewNBD {
		// Attach new NBD device
		var cleanup func() error
		var err error
		devicePath, cleanup, err = c.attachNBDDevice(ctx, state)
		if err != nil {
			return fmt.Errorf("failed to attach NBD device: %w", err)
		}
		state.devicePath = devicePath
		state.nbdCleanup = cleanup
		c.log.Info("Attached new NBD device",
			"volume_id", volumeId,
			"device", devicePath,
			"nbd_index", state.nbdIndex)
	}

	// Format if needed
	if err := c.formatDevice(devicePath, state.info.Filesystem); err != nil {
		if state.nbdCleanup != nil {
			state.nbdCleanup()
		}
		return fmt.Errorf("failed to format device: %w", err)
	}

	// Mount the filesystem
	mountOpts := ""
	if readOnly {
		mountOpts = "ro"
	}

	fsType := state.info.Filesystem
	if fsType == "" {
		fsType = "ext4"
	}

	c.log.Info("Mounting filesystem",
		"volume_id", volumeId,
		"device", devicePath,
		"mount_path", mountPath,
		"filesystem", fsType,
	)
	if err := unix.Mount(devicePath, mountPath, fsType, 0, mountOpts); err != nil {
		c.log.Error("Failed to mount filesystem", "error", err, "device", devicePath, "path", mountPath)
		if state.nbdCleanup != nil {
			state.nbdCleanup()
		}
		return fmt.Errorf("failed to mount filesystem: %w", err)
	}

	state.mounted = true
	state.info.MountPath = mountPath
	state.info.MountPath = mountPath
	state.info.Status = VolumeStatusMounted

	c.log.Info("Mounted LSVD volume",
		"volume_id", volumeId,
		"device", devicePath,
		"mount_path", mountPath,
		"filesystem", fsType,
		"read_only", readOnly)

	return nil
}

// UnmountVolume unmounts an LSVD volume
func (c *lsvdClientImpl) UnmountVolume(ctx context.Context, volumeId string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.volumes[volumeId]
	if !exists {
		// Nothing to unmount
		return nil
	}

	// Check actual mount state from system
	actuallyMounted := state.info.MountPath != "" && c.isMounted(state.info.MountPath)

	// Update state if mount status has changed
	if actuallyMounted != state.mounted {
		c.log.Info("Mount status mismatch, updating state",
			"volume_id", volumeId,
			"state_mounted", state.mounted,
			"actually_mounted", actuallyMounted)
		state.mounted = actuallyMounted
		if !actuallyMounted {
			// System says it's not mounted, update our state
			state.info.MountPath = ""
			state.info.Status = VolumeStatusLoaded
		}
	}

	if !state.mounted {
		// Not mounted, return error
		return fmt.Errorf("volume %s is not mounted", volumeId)
	}

	// Unmount the filesystem
	if err := unix.Unmount(state.info.MountPath, 0); err != nil {
		c.log.Error("Failed to unmount filesystem", "error", err, "path", state.info.MountPath)
		// Try force unmount
		if err := unix.Unmount(state.info.MountPath, unix.MNT_FORCE); err != nil {
			return fmt.Errorf("failed to unmount filesystem: %w", err)
		}
	}

	// Disconnect NBD device
	if state.nbdCleanup != nil {
		if err := state.nbdCleanup(); err != nil {
			c.log.Error("Failed to cleanup NBD device", "error", err)
		}
		state.nbdCleanup = nil
	}

	// Cancel NBD handler
	if state.nbdCancel != nil {
		state.nbdCancel()
		state.nbdCancel = nil
	}

	state.mounted = false
	state.info.MountPath = ""
	state.devicePath = ""
	state.info.MountPath = ""
	state.info.Status = VolumeStatusLoaded

	c.log.Info("Unmounted LSVD volume", "volume_id", volumeId)

	return nil
}

// IsVolumeMounted checks if a volume is currently mounted
func (c *lsvdClientImpl) IsVolumeMounted(ctx context.Context, volumeId string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.volumes[volumeId]
	if !exists {
		// Volume doesn't exist or isn't loaded
		return false, nil
	}

	// Check actual mount state from system
	if state.info.MountPath != "" && c.isMounted(state.info.MountPath) {
		return true, nil
	}

	// Update state if mount status has changed
	if state.mounted && state.info.MountPath != "" && !c.isMounted(state.info.MountPath) {
		state.mounted = false
		state.info.Status = VolumeStatusLoaded
	}

	return state.mounted, nil
}

// GetVolumeInfo returns information about a volume
func (c *lsvdClientImpl) GetVolumeInfo(ctx context.Context, volumeId string) (*VolumeInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.volumes[volumeId]
	if !exists {
		// Try to load from storage
		var sa lsvd.SegmentAccess

		if c.remoteOnly {
			// For remote-only mode, check remote storage
			if c.authClient == nil {
				return nil, fmt.Errorf("remote-only mode requires auth client")
			}
			sa = lsvd.NewDiskAPISegmentAccess(c.log, c.cloudURL, c.authClient)
		} else {
			// For local/replica mode, check local storage
			volumePath := c.getVolumePath(volumeId)
			sa = &lsvd.LocalFileAccess{
				Dir: volumePath,
				Log: c.log,
			}
		}

		vi, err := sa.GetVolumeInfo(ctx, volumeId)
		if err != nil {
			return nil, fmt.Errorf("volume %s not found: %w", volumeId, err)
		}

		// Determine the status based on mount state
		status := VolumeStatusOnDisk

		// Return volume info indicating it exists on disk but not loaded in memory
		return &VolumeInfo{
			ID:        volumeId,
			Name:      vi.Name,
			SizeBytes: int64(vi.Size),
			UUID:      vi.UUID,
			Status:    status,
		}, nil
	}

	// Check the actual mount status from the system
	actualMountPath := state.info.MountPath
	actuallyMounted := c.isMounted(state.info.MountPath)

	// Update state if mount status has changed
	if actuallyMounted != state.mounted {
		c.log.Info("Mount status changed for volume",
			"volume_id", volumeId,
			"was_mounted", state.mounted,
			"is_mounted", actuallyMounted,
			"mount_path", actualMountPath)
		state.mounted = actuallyMounted
		if actuallyMounted {
			state.info.Status = VolumeStatusMounted
			state.info.MountPath = actualMountPath
		} else {
			state.info.Status = VolumeStatusLoaded
			state.info.MountPath = ""
		}
	}

	// Determine current status
	status := VolumeStatusLoaded
	mountPath := ""
	if actuallyMounted {
		status = VolumeStatusMounted
		mountPath = actualMountPath
	}

	// Return copy of volume info with live mount status
	return &VolumeInfo{
		ID:         state.info.ID,
		Name:       state.info.Name,
		SizeBytes:  state.info.SizeBytes,
		Filesystem: state.info.Filesystem,
		MountPath:  mountPath,
		UUID:       state.info.UUID,
		Status:     status,
	}, nil
}

// ListVolumes lists all volumes
func (c *lsvdClientImpl) ListVolumes(ctx context.Context) ([]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.remoteOnly {
		// For remote-only mode, list from remote storage
		if c.authClient == nil {
			return nil, fmt.Errorf("remote-only mode requires auth client")
		}

		sa := lsvd.NewDiskAPISegmentAccess(c.log, c.cloudURL, c.authClient)
		volumes, err := sa.ListVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list remote volumes: %w", err)
		}
		return volumes, nil
	}

	// List volumes from local disk
	volumesPath := c.getVolumesBasePath()
	entries, err := os.ReadDir(volumesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	var volumes []string
	for _, entry := range entries {
		if entry.IsDir() {
			volumes = append(volumes, entry.Name())
		}
	}

	return volumes, nil
}

// Helper functions for NBD operations

// nbdRange reads the NBD range from sysfs
func nbdRange() (int, error) {
	data, err := os.ReadFile("/sys/dev/block/43:0/range")
	if err != nil {
		// Fallback to default if file doesn't exist
		if os.IsNotExist(err) {
			return 32, nil
		}
		return 0, err
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 32, nil // Default fallback
	}
	return n, nil
}

// Path helper methods

// getVolumePath returns the path to the volume's storage directory
func (c *lsvdClientImpl) getVolumePath(volumeId string) string {
	return filepath.Join(c.dataPath, "lsvd-volumes", volumeId)
}

// saveNBDIndex saves the NBD device index to a file in the volume directory
func (c *lsvdClientImpl) saveNBDIndex(volumePath string, idx uint32) error {
	// Ensure directory exists
	if err := os.MkdirAll(volumePath, 0755); err != nil {
		return fmt.Errorf("failed to create volume directory: %w", err)
	}

	indexPath := filepath.Join(volumePath, "nbd-index")
	return os.WriteFile(indexPath, []byte(strconv.FormatUint(uint64(idx), 10)), 0644)
}

// loadNBDIndex loads a previously saved NBD device index
// Returns 0 and no error if the file doesn't exist
func (c *lsvdClientImpl) loadNBDIndex(volumePath string) (uint32, error) {
	indexPath := filepath.Join(volumePath, "nbd-index")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // No saved index, return 0
		}
		return 0, err
	}

	idx, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid nbd-index file: %w", err)
	}

	return uint32(idx), nil
}

// clearNBDIndex removes the saved NBD index file
func (c *lsvdClientImpl) clearNBDIndex(volumePath string) error {
	indexPath := filepath.Join(volumePath, "nbd-index")
	err := os.Remove(indexPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// getVolumesBasePath returns the base path for all volumes
func (c *lsvdClientImpl) getVolumesBasePath() string {
	return filepath.Join(c.dataPath, "lsvd-volumes")
}

func (c *lsvdClientImpl) getDevicePath(id string) string {
	return filepath.Join(c.dataPath, "devices", id)
}

// runNBDHandlerWithReconnect runs the NBD handler and automatically reconnects on disconnection
func (c *lsvdClientImpl) runNBDHandlerWithReconnect(
	ctx context.Context,
	state *volumeState,
	idx uint32,
	devicePath string,
	initialConn net.Conn,
	initialClient *os.File,
) {
	defer c.log.Warn("NBD handler exited",
		"device", devicePath,
		"index", idx)
	defer func() {
		if state.nbdCancel != nil {
			state.nbdCancel()
		}
	}()

	reconnectDelay := 10 * time.Millisecond
	maxReconnectDelay := 30 * time.Second
	attemptNum := 0

	// Use the initial connection from Loopback for the first iteration
	serverConn := initialConn
	client := initialClient

	for {
		attemptNum++

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			c.log.Info("NBD handler stopping due to context cancellation", "device", devicePath, "index", idx)
			if client != nil {
				client.Close()
			}
			if serverConn != nil {
				serverConn.Close()
			}
			return
		default:
		}

		// Only reconnect if this is not the first iteration
		if serverConn == nil || client == nil {
			// Reconnect to NBD device with new socketpair
			var err error
			serverConn, client, err = nbdnl.Reconnect(ctx, idx)
			if err != nil {
				c.log.Error("Failed to reconnect to NBD device, will retry",
					"device", devicePath,
					"index", idx,
					"attempt", attemptNum,
					"error", err,
					"retry_in", reconnectDelay)

				select {
				case <-ctx.Done():
					return
				case <-time.After(reconnectDelay):
					// Exponential backoff
					reconnectDelay *= 2
					if reconnectDelay > maxReconnectDelay {
						reconnectDelay = maxReconnectDelay
					}
					continue
				}
			}

			// Reset reconnect delay on successful connection
			reconnectDelay = 1 * time.Second

			c.log.Info("NBD handler reconnected successfully",
				"device", devicePath,
				"index", idx,
				"attempt", attemptNum)
		}

		// Update state with new connection
		// (no lock needed - state is only accessed during mount/unmount which hold the lock)
		state.nbdConn = serverConn

		if attemptNum == 1 {
			c.log.Info("NBD handler starting with initial connection",
				"device", devicePath,
				"index", idx)
		}

		// Run NBD handler
		nbdOpts := &nbd.Options{
			MinimumBlockSize:   4096,
			PreferredBlockSize: 4096,
			MaximumBlockSize:   4096,
			RawFile:            client,
		}

		err := nbd.HandleTransport(c.log, serverConn, lsvd.NBDWrapper(ctx, c.log, state.disk), nbdOpts)

		// Clean up this iteration's resources
		client.Close()
		serverConn.Close()

		// Mark as nil so we reconnect on next iteration
		client = nil
		serverConn = nil

		// Check why we exited
		if ctx.Err() != nil {
			c.log.Info("NBD handler stopping due to context cancellation", "device", devicePath, "index", idx)
			return
		}

		if err != nil {
			c.log.Warn("NBD transport error, reconnecting",
				"device", devicePath,
				"index", idx,
				"error", err,
				"retry_in", reconnectDelay)
		} else {
			c.log.Warn("NBD transport ended normally, reconnecting",
				"device", devicePath,
				"index", idx,
				"retry_in", reconnectDelay)
		}

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
			c.log.Warn("NBD handler reconnecting now",
				"device", devicePath,
				"index", idx)
			// Continue to next iteration
		}
	}
}

// attachNBDDevice attaches an LSVD disk to an NBD device
func (c *lsvdClientImpl) attachNBDDevice(ctx context.Context, state *volumeState) (string, func() error, error) {
	volumePath := c.getVolumePath(state.info.ID)

	// Try to load saved NBD index for crash recovery
	savedIdx, err := c.loadNBDIndex(volumePath)
	if err != nil {
		c.log.Warn("Failed to load saved NBD index", "volume", state.info.ID, "error", err)
		savedIdx = 0
	}
	if savedIdx != 0 {
		c.log.Info("Attempting to reconnect to saved NBD device", "volume", state.info.ID, "index", savedIdx)
	}

	preferredIdx := nbdnl.IndexAny
	if savedIdx != 0 {
		preferredIdx = savedIdx
	}

	// Setup NBD loopback
	idx, conn, client, cleanup, err := nbdnl.Loopback(ctx, uint64(state.info.SizeBytes), preferredIdx)
	if err != nil {
		return "", nil, fmt.Errorf("failed to setup NBD loopback: %w", err)
	}

	// Save the NBD index for crash recovery
	if err := c.saveNBDIndex(volumePath, idx); err != nil {
		c.log.Warn("Failed to save NBD index", "volume", state.info.ID, "error", err)
	}

	// Get NBD range for device path calculation
	nbdRng, err := nbdRange()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to get NBD range: %w", err)
	}

	// Create device node
	devicePath := c.getDevicePath(strings.ReplaceAll(state.info.ID, "/", "-"))

	dir := filepath.Dir(devicePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to create device directory: %w", err)
	}

	os.Remove(devicePath) // Remove stale device if exists

	// Check if device exists, create if not
	// Device should be created by kernel, but if not, create it
	devNum := int(unix.Mkdev(43, idx*uint32(nbdRng)))
	if err := unix.Mknod(devicePath, unix.S_IFBLK|0660, devNum); err != nil && !os.IsExist(err) {
		cleanup()
		return "", nil, fmt.Errorf("failed to create device node: %w", err)
	}

	c.log.Info("Attaching NBD device",
		"device", devicePath,
		"index", idx,
		"dev-num", devNum,
		"size_bytes", state.info.SizeBytes)

	// Wrap cleanup to also clear NBD index
	wrappedCleanup := func() error {
		err := cleanup()
		if clearErr := c.clearNBDIndex(volumePath); clearErr != nil {
			c.log.Warn("Failed to clear NBD index", "volume", state.info.ID, "error", clearErr)
		}
		return err
	}

	// Create NBD handler context
	nbdCtx, cancel := context.WithCancel(ctx)
	state.nbdCancel = cancel
	state.nbdConn = conn
	state.nbdIndex = idx

	c.log.Info("Starting NBD handler with auto-reconnect",
		"device", devicePath,
		"index", idx)

	// Start NBD handler with auto-reconnect in background, using initial connection from Loopback
	go c.runNBDHandlerWithReconnect(nbdCtx, state, idx, devicePath, conn, client)

	// Wait for NBD device to be ready (timeout after 10 seconds)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// Check if the device is ready using nbdnl.Status
		status, err := nbdnl.Status(idx)
		if err == nil {
			// Device is ready
			c.log.Debug("NBD device is ready", "device", devicePath, "index", idx, "status", status)
			return devicePath, wrappedCleanup, nil
		}

		c.log.Warn("Waiting for NBD device to become ready", "device", devicePath, "index", idx, "error", err)

		// Wait a short time before retrying
		select {
		case <-ctx.Done():
			wrappedCleanup()
			return "", nil, fmt.Errorf("context cancelled while waiting for NBD device")
		case <-time.After(50 * time.Millisecond):
			// Continue polling
		}
	}

	// Timeout reached
	wrappedCleanup()
	return "", nil, fmt.Errorf("timeout waiting for NBD device %s to become ready", devicePath)
}

// formatDevice formats a device with the specified filesystem if needed
func (c *lsvdClientImpl) formatDevice(devicePath string, filesystem string) error {
	// Default to ext4 if not specified
	if filesystem == "" {
		filesystem = "ext4"
	}

	// Check if already formatted
	formatted := false

	switch filesystem {
	case "ext4":
		if _, err := ext4.ReadExt4SuperBlock(devicePath); err == nil {
			formatted = true
			// Run fsck to ensure filesystem is clean
			cmd := exec.Command("e2fsck", "-f", "-y", devicePath)
			if output, err := cmd.CombinedOutput(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					switch ee.ExitCode() {
					case 1, 2:
						c.log.Info("fsck detected and corrected issues", "device", devicePath, "output", string(output))
						return nil
					case 4:
						c.log.Info("fsck detected filesystem errors", "device", devicePath, "output", string(output))
						return fmt.Errorf("e2fsck found filesystem errors on %s: %s", devicePath, string(output))
					case 8:
						c.log.Info("fsck detected operational error", "device", devicePath, "output", string(output))
						return fmt.Errorf("e2fsck operational error on %s: %s", devicePath, string(output))
					case 16:
						c.log.Info("fsck detected usage or syntax error", "device", devicePath, "output", string(output))
						return fmt.Errorf("e2fsck usage error on %s: %s", devicePath, string(output))
					case 32:
						c.log.Info("fsck detected system error", "device", devicePath, "output", string(output))
						return fmt.Errorf("e2fsck system error on %s: %s", devicePath, string(output))
					}
				}
				// fsck failure is a critical error - don't reformat
				return fmt.Errorf("e2fsck failed on %s: %w (output: %s)", devicePath, err, string(output))
			}
		} else {
			c.log.Info("Device is not ext4", "device", devicePath, "error", err)
		}

	case "xfs":
		// Check for XFS filesystem
		cmd := exec.Command("xfs_db", "-r", "-c", "sb 0", "-c", "p magicnum", devicePath)
		if output, err := cmd.CombinedOutput(); err == nil && strings.Contains(string(output), "0x58465342") {
			formatted = true
			// Run xfs_repair to ensure filesystem is clean (read-only check)
			repairCmd := exec.Command("xfs_repair", "-n", devicePath)
			if output, err := repairCmd.CombinedOutput(); err != nil {
				// xfs_repair failure is a critical error - don't reformat
				return fmt.Errorf("xfs_repair check failed on %s: %w (output: %s)", devicePath, err, string(output))
			}
		}

	case "btrfs":
		// Check for Btrfs filesystem
		cmd := exec.Command("btrfs", "filesystem", "show", devicePath)
		if _, err := cmd.CombinedOutput(); err == nil {
			formatted = true
			// Run btrfs check to ensure filesystem is clean
			checkCmd := exec.Command("btrfs", "check", "--readonly", devicePath)
			if output, err := checkCmd.CombinedOutput(); err != nil {
				// btrfs check failure is a critical error - don't reformat
				return fmt.Errorf("btrfs check failed on %s: %w (output: %s)", devicePath, err, string(output))
			}
		}
	}

	// Format if needed
	if !formatted {
		c.log.Info("Formatting device", "device", devicePath, "filesystem", filesystem)

		var cmd *exec.Cmd
		switch filesystem {
		case "ext4":
			cmd = exec.Command("mkfs.ext4", "-F", devicePath)
		case "xfs":
			cmd = exec.Command("mkfs.xfs", "-f", devicePath)
		case "btrfs":
			cmd = exec.Command("mkfs.btrfs", "-f", devicePath)
		default:
			return fmt.Errorf("unsupported filesystem: %s", filesystem)
		}

		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to format %s: %w (output: %s)", filesystem, err, string(output))
		}

		c.log.Info("Successfully formatted device", "device", devicePath, "filesystem", filesystem)
	} else {
		c.log.Info("Device already formatted", "device", devicePath, "filesystem", filesystem)
	}

	return nil
}

// isMounted checks if a path is a mount point
func (c *lsvdClientImpl) isMounted(path string) bool {
	if path == "" {
		return false
	}

	// Read /proc/mounts to check if path is mounted
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		c.log.Debug("Failed to read /proc/mounts", "error", err)
		return false
	}

	// Check each line for our mount path
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == path {
			return true
		}
	}
	return false
}

// reconcileVolume performs background segment reconciliation for a volume
func (c *lsvdClientImpl) reconcileVolume(ctx context.Context, volumeId string, primarySA, replicaSA lsvd.SegmentAccess) {
	c.log.Info("Starting background segment reconciliation", "volume", volumeId)

	// Open primary and replica volumes
	primaryVol, err := primarySA.OpenVolume(ctx, volumeId)
	if err != nil {
		c.log.Error("Failed to open primary volume for reconciliation", "volume", volumeId, "error", err)
		return
	}

	replicaVol, err := replicaSA.OpenVolume(ctx, volumeId)
	if err != nil {
		c.log.Error("Failed to open replica volume for reconciliation", "volume", volumeId, "error", err)
		return
	}

	// Create reconciler
	reconciler := lsvd.NewSegmentReconciler(c.log, primaryVol, replicaVol)

	// Perform reconciliation
	result, err := reconciler.Reconcile(ctx)
	if err != nil {
		c.log.Error("Failed to reconcile volume", "volume", volumeId, "error", err)
		return
	}

	c.log.Info("Segment reconciliation complete",
		"volume", volumeId,
		"total_primary", result.TotalPrimary,
		"total_replica", result.TotalReplica,
		"missing", result.Missing,
		"uploaded", result.Uploaded,
		"failed", result.Failed)

	if result.Failed > 0 {
		c.log.Warn("Some segments failed to reconcile",
			"volume", volumeId,
			"failed_count", result.Failed,
			"failed_segments", result.FailedSegments)
	}
}

// Close gracefully shuts down the LSVD client and unmounts all volumes
func (c *lsvdClientImpl) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.log.Info("Closing LSVD client, unmounting all volumes")

	var errors []error

	// Unmount all mounted volumes
	for volumeId, state := range c.volumes {
		if state.mounted {
			c.log.Info("Unmounting volume on shutdown", "volume_id", volumeId, "mount_path", state.info.MountPath)

			// Force unmount the filesystem
			if err := unix.Unmount(state.info.MountPath, unix.MNT_FORCE); err != nil {
				c.log.Error("Failed to unmount volume", "volume_id", volumeId, "error", err)
				errors = append(errors, fmt.Errorf("unmount %s: %w", volumeId, err))
			}

			// Cleanup NBD device
			if state.nbdCleanup != nil {
				if err := state.nbdCleanup(); err != nil {
					c.log.Error("Failed to cleanup NBD device", "volume_id", volumeId, "error", err)
					errors = append(errors, fmt.Errorf("NBD cleanup %s: %w", volumeId, err))
				}
			}

			// Cancel NBD handler
			if state.nbdCancel != nil {
				state.nbdCancel()
			}
		}

		// Close disk
		if state.disk != nil {
			if err := state.disk.Close(context.Background()); err != nil {
				c.log.Error("Failed to close disk", "volume_id", volumeId, "error", err)
				errors = append(errors, fmt.Errorf("close disk %s: %w", volumeId, err))
			}
		}
	}

	// Clear state
	c.volumes = make(map[string]*volumeState)
	c.disks = make(map[string]*lsvd.Disk)

	if len(errors) > 0 {
		return fmt.Errorf("multiple errors during shutdown: %v", errors)
	}

	c.log.Info("LSVD client closed successfully")
	return nil
}
