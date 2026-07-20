package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// ConfigureVolumes prepares volumes and returns a map of volume name to actual mount path
func (c *SandboxController) ConfigureVolumes(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (map[string]string, error) {
	volumeMounts := make(map[string]string)

	for _, volume := range sb.Spec.Volume {
		switch volume.Provider {
		case "host":
			path, err := c.configureHostVolume(sb, volume)
			if err != nil {
				return nil, err
			}
			volumeMounts[volume.Name] = path
		case "local":
			path, err := c.configureLocalVolume(ctx, sb, volume)
			if err != nil {
				return nil, err
			}
			volumeMounts[volume.Name] = path
		case "miren", "":
			path, err := c.configureMirenVolume(ctx, sb, volume, meta)
			if err != nil {
				return nil, err
			}
			volumeMounts[volume.Name] = path
		default:
			return nil, fmt.Errorf("unsupported volume provider: %s", volume.Provider)
		}
	}

	return volumeMounts, nil
}

func (c *SandboxController) configureHostVolume(sb *compute.Sandbox, volume compute.SandboxSpecVolume) (string, error) {
	rawPath := c.sandboxPath(sb, "volumes", volume.Name)
	err := os.MkdirAll(filepath.Dir(rawPath), 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create volume directory: %w", err)
	}

	path, ok := volume.Labels.Get("path")
	if !ok {
		if name, ok := volume.Labels.Get("name"); ok {
			path = filepath.Join(c.DataPath, "host-volumes", name)
			err = os.MkdirAll(path, 0755)
			if err != nil {
				return "", fmt.Errorf("failed to create named host volume directory: %w", err)
			}
		} else {
			return "", fmt.Errorf("missing path or name label for host volume")
		}
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if create, ok := volume.Labels.Get("create"); ok && create == "true" {
			if err := os.MkdirAll(path, 0755); err != nil {
				return "", fmt.Errorf("failed to create host path: %w", err)
			}
		} else {
			return "", fmt.Errorf("host path does not exist: %s", path)
		}
	}

	c.Log.Debug("creating host volume symlink", "path", path, "host-path", rawPath)

	if existing, err := os.Readlink(rawPath); err == nil {
		if existing == path {
			return path, nil
		}
		return "", fmt.Errorf("host volume symlink %s already exists but points to %s, expected %s", rawPath, existing, path)
	}

	if err := os.Symlink(path, rawPath); err != nil {
		return "", err
	}

	return path, nil
}

func (c *SandboxController) configureLocalVolume(ctx context.Context, sb *compute.Sandbox, volume compute.SandboxSpecVolume) (string, error) {
	if volume.MountPath == "" {
		return "", fmt.Errorf("missing mount_path for local volume %q", volume.Name)
	}

	// Resolve app ID from sandbox version for per-app isolation
	appKey := volume.Name
	if sb.Spec.Version != "" {
		res, err := c.EAC.Get(ctx, sb.Spec.Version.String())
		if err != nil {
			return "", fmt.Errorf("resolve app version %s for local volume %q: %w", sb.Spec.Version, volume.Name, err)
		}
		var appVer core_v1alpha.AppVersion
		appVer.Decode(res.Entity().Entity())
		if appVer.App == "" {
			return "", fmt.Errorf("app version %s has empty app reference for local volume %q", sb.Spec.Version, volume.Name)
		}
		appKey = appVer.App.String()
	}

	localPath := filepath.Join(c.DataPath, "data", "local", appKey)
	if err := os.MkdirAll(localPath, 0777); err != nil {
		return "", fmt.Errorf("failed to create local storage directory: %w", err)
	}
	// Explicitly chmod since MkdirAll respects umask
	if err := os.Chmod(localPath, 0777); err != nil {
		c.Log.Warn("failed to set permissions on local storage directory", "path", localPath, "error", err)
	}

	c.Log.Info("configured local storage volume", "volume", volume.Name, "path", localPath)
	return localPath, nil
}

func (c *SandboxController) configureMirenVolume(ctx context.Context, sb *compute.Sandbox, volume compute.SandboxSpecVolume, meta *entity.Meta) (string, error) {
	if volume.DiskName == "" {
		return "", fmt.Errorf("missing disk_name for miren volume")
	}

	if volume.MountPath == "" {
		return "", fmt.Errorf("missing mount_path for miren volume")
	}

	c.Log.Info("configuring miren volume",
		"sandbox", sb.ID,
		"disk_name", volume.DiskName,
		"mount_path", volume.MountPath)

	// Append instance number to disk name to ensure each instance gets its own disk
	actualDiskName := volume.DiskName

	// TODO: the mechanism here is to try to allocate a unique disk by using the instance
	// number. But we're too loosey-goosey with how the instance numbers are setup!
	// So instead, for now, just use the disk name as-is.
	/*
		var md core_v1alpha.Metadata
		md.Decode(meta)

		if instanceStr, ok := md.Labels.Get("instance"); ok {
			actualDiskName = fmt.Sprintf("%s-%s", volume.DiskName, instanceStr)
			c.Log.Info("appended instance number to disk name",
				"sandbox_id", sb.ID,
				"original_disk_name", volume.DiskName,
				"actual_disk_name", actualDiskName,
				"instance", instanceStr)
		}
	*/

	// Use configuration from volume fields
	readOnly := volume.ReadOnly
	sizeGB := volume.SizeGb

	filesystem := volume.Filesystem
	if filesystem == "" {
		filesystem = "ext4"
	}

	leaseTimeout := 5 * time.Minute
	if volume.LeaseTimeout != "" {
		duration, err := time.ParseDuration(volume.LeaseTimeout)
		if err != nil {
			return "", fmt.Errorf("invalid lease_timeout value: %w", err)
		}
		leaseTimeout = duration
	}

	// Resolve version to app ID if set
	var appID entity.Id
	if sb.Spec.Version != "" {
		versionResp, err := c.EAC.Get(ctx, sb.Spec.Version.String())
		if err != nil {
			c.Log.Warn("failed to get app version for disk lease",
				"version", sb.Spec.Version,
				"error", err)
		} else {
			var version core_v1alpha.AppVersion
			version.Decode(versionResp.Entity().Entity())
			appID = version.App
		}
	}

	// Look up or create Disk entity using instance-specific name
	diskID, err := c.ensureDisk(ctx, actualDiskName, sizeGB, filesystem, appID)
	if err != nil {
		return "", fmt.Errorf("failed to ensure disk exists: %w", err)
	}

	// Acquire a lease for this disk on this node (creates new or takes over existing)
	nodeID := c.NodeId.Id()
	leaseID, err := c.acquireDiskLease(ctx, diskID, nodeID, sb.ID, appID, volume.MountPath, readOnly)
	if err != nil {
		return "", fmt.Errorf("failed to get or create disk lease: %w", err)
	}

	// Wait for lease to become BOUND
	diskMountPath, err := c.waitForLeaseBound(ctx, leaseID, leaseTimeout)
	if err != nil {
		return "", fmt.Errorf("failed to acquire disk lease: %w", err)
	}

	c.Log.Info("disk lease bound",
		"lease", leaseID,
		"disk_mount_path", diskMountPath)

	// Return the disk mount path so it can be used directly in the container spec
	return diskMountPath, nil
}

func (c *SandboxController) ensureDisk(ctx context.Context, diskName string, sizeGB int64, filesystem string, appID entity.Id) (entity.Id, error) {
	// Search for existing disk by name using the name index
	listResp, err := c.EAC.List(ctx, entity.String(storage.DiskNameId, diskName))
	if err != nil {
		return entity.Id(""), fmt.Errorf("failed to query disks by name: %w", err)
	}

	if len(listResp.Values()) > 0 {
		// Found an existing disk
		e := listResp.Values()[0]
		var disk storage.Disk
		disk.Decode(e.Entity())

		c.Log.Info("found existing disk", "disk", disk.ID, "name", diskName, "status", disk.Status)
		// Don't wait here - let the lease controller handle provisioning
		// If there's an existing lease, it's already waiting
		// If we create a new lease, the lease controller will wait
		return disk.ID, nil
	}

	// Disk doesn't exist, create it if size is specified
	if sizeGB <= 0 {
		return entity.Id(""), fmt.Errorf("disk %q does not exist and no size specified for auto-creation", diskName)
	}

	c.Log.Info("creating new disk",
		"name", diskName,
		"size_gb", sizeGB,
		"filesystem", filesystem)

	// Convert filesystem string to DiskFilesystem type
	var fs storage.DiskFilesystem
	switch filesystem {
	case "ext4":
		fs = storage.EXT4
	case "xfs":
		fs = storage.XFS
	case "btrfs":
		fs = storage.BTRFS
	default:
		fs = storage.EXT4
	}

	disk := &storage.Disk{
		Name:       diskName,
		SizeGb:     sizeGB,
		Filesystem: fs,
		Status:     storage.PROVISIONING,
		CreatedBy:  appID,
	}

	name := idgen.GenNS("disk")

	putResp, err := c.EAC.Create(ctx, entity.New(
		entity.DBId, entity.Id("disk/"+name),
		disk.Encode,
	).Attrs())
	if err != nil {
		return entity.Id(""), fmt.Errorf("failed to create disk entity: %w", err)
	}

	diskID := entity.Id(putResp.Id())
	c.Log.Info("created disk", "disk", diskID, "name", diskName, "status", "provisioning")
	// Don't wait for provisioning here - let the lease controller handle it
	// This allows multiple sandboxes to share the same lease while disk is provisioning

	return diskID, nil
}

// acquireDiskLease gets or creates a disk lease for the given sandbox.
// If this sandbox already has a lease for this disk on this node (e.g., from a
// previous attempt), that lease is returned. Otherwise a new lease is created.
// Note: Old sandbox leases are released by stopSandbox() when sandboxes die,
// so we don't need transfer logic here.
func (c *SandboxController) acquireDiskLease(ctx context.Context, diskID entity.Id, nodeID entity.Id, sandboxID entity.Id, appID entity.Id, mountPath string, readOnly bool) (entity.Id, error) {
	// Check if we already have a lease for this disk on this node
	listResp, err := c.EAC.List(ctx, entity.Ref(storage.DiskLeaseDiskIdId, diskID))
	if err != nil {
		return entity.Id(""), fmt.Errorf("failed to list disk leases: %w", err)
	}

	for _, e := range listResp.Values() {
		var lease storage.DiskLease
		lease.Decode(e.Entity())

		// Index already filters by diskID; also check nodeID
		if lease.NodeId == nodeID {
			// If the lease is owned by this sandbox, return it (retry case)
			if lease.SandboxId == sandboxID {
				c.Log.Info("found existing disk lease for this sandbox",
					"lease", lease.ID,
					"disk", diskID,
					"node", nodeID,
					"status", lease.Status)
				return lease.ID, nil
			}

			// If there's an active lease (PENDING or BOUND) owned by another sandbox,
			// that sandbox hasn't been cleaned up yet - the new sandbox shouldn't start
			if lease.Status == storage.PENDING || lease.Status == storage.BOUND {
				c.Log.Warn("disk lease still active for another sandbox",
					"lease", lease.ID,
					"disk", diskID,
					"owner", lease.SandboxId,
					"requester", sandboxID,
					"status", lease.Status)
				return entity.Id(""), fmt.Errorf("disk %s has an active lease (%s) for sandbox %s", diskID, lease.Status, lease.SandboxId)
			}

			// Lease is in a terminal state (RELEASED, FAILED) - ignore it
			// and create a new one. The disk controller will clean up old leases.
			c.Log.Debug("ignoring lease in terminal state",
				"lease", lease.ID,
				"status", lease.Status)
		}
	}

	// No usable lease found, create a new one
	return c.createDiskLease(ctx, diskID, sandboxID, appID, mountPath, readOnly)
}

func (c *SandboxController) createDiskLease(ctx context.Context, diskID entity.Id, sandboxID entity.Id, appID entity.Id, mountPath string, readOnly bool) (entity.Id, error) {
	c.Log.Info("creating disk lease",
		"disk", diskID,
		"sandbox", sandboxID,
		"app", appID,
		"mount_path", mountPath,
		"node_id", c.NodeId)

	nodeID := c.NodeId.Id()

	lease := &storage.DiskLease{
		DiskId:    diskID,
		SandboxId: sandboxID,
		AppId:     appID,
		Status:    storage.PENDING,
		Mount: storage.Mount{
			Path:     mountPath,
			ReadOnly: readOnly,
			Options:  "rw",
		},
		NodeId: nodeID,
	}

	if readOnly {
		lease.Mount.Options = "ro"
	}

	name := idgen.GenNS("disk-lease")
	leaseEntityID := entity.Id("disk-lease/" + name)

	putResp, err := c.EAC.Create(ctx, entity.New(
		entity.DBId, leaseEntityID,
		lease.Encode,
	).Attrs())
	if err != nil {
		return entity.Id(""), fmt.Errorf("failed to create disk lease entity: %w", err)
	}

	leaseID := entity.Id(putResp.Id())
	c.Log.Info("created disk lease", "lease", leaseID)

	return leaseID, nil
}

func (c *SandboxController) waitForLeaseBound(ctx context.Context, leaseID entity.Id, timeout time.Duration) (string, error) {
	c.Log.Info("waiting for disk lease to become bound",
		"lease", leaseID,
		"timeout", timeout)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Poll for lease status changes
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for disk lease to become bound")
		case <-ticker.C:
			// Get current lease status
			leaseResp, err := c.EAC.Get(ctx, leaseID.String())
			if err != nil {
				return "", fmt.Errorf("failed to get disk lease: %w", err)
			}

			var lease storage.DiskLease
			lease.Decode(leaseResp.Entity().Entity())

			c.Log.Debug("disk lease status update",
				"lease", leaseID,
				"status", lease.Status)

			switch lease.Status {
			case storage.BOUND:
				// Lease is bound, get disk to find volume ID
				diskResp, err := c.EAC.Get(ctx, lease.DiskId.String())
				if err != nil {
					return "", fmt.Errorf("failed to get disk entity: %w", err)
				}

				var disk storage.Disk
				disk.Decode(diskResp.Entity().Entity())

				volumeId := disk.VolumeId
				if volumeId == "" {
					return "", fmt.Errorf("disk has no volume ID")
				}

				// Disk is mounted at /var/lib/miren/disks/{volume_id}
				diskPath := filepath.Join("/var/lib/miren/disks", volumeId)
				c.Log.Info("disk lease bound successfully",
					"lease", leaseID,
					"disk", disk.ID,
					"volume_id", volumeId,
					"disk_path", diskPath)
				return diskPath, nil

			case storage.FAILED:
				return "", fmt.Errorf("disk lease failed: %s", lease.ErrorMessage)

			case storage.PENDING:
				// Still pending, continue waiting
				continue

			case storage.RELEASED:
				// Lease released out from under us while waiting to bind.
				fallthrough
			default:
				return "", fmt.Errorf("unexpected disk lease status: %s", lease.Status)
			}
		}
	}
}
