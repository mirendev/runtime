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

// configureVolumes prepares volumes and returns a map of volume name to actual mount path
func (c *SandboxController) configureVolumes(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (map[string]string, error) {
	volumeMounts := make(map[string]string)

	for _, volume := range sb.Spec.Volume {
		switch volume.Provider {
		case "host":
			path, err := c.configureHostVolume(sb, volume)
			if err != nil {
				return nil, err
			}
			volumeMounts[volume.Name] = path
		case "miren":
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

	if err := os.Symlink(path, rawPath); err != nil {
		return "", err
	}

	return path, nil
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

	// Look up or create Disk entity using instance-specific name
	diskID, err := c.ensureDisk(ctx, actualDiskName, sizeGB, filesystem)
	if err != nil {
		return "", fmt.Errorf("failed to ensure disk exists: %w", err)
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

	// Check if there's already a lease for this disk on this node
	nodeID := entity.Id("node/" + c.NodeId)
	leaseID, err := c.findOrCreateDiskLease(ctx, diskID, nodeID, sb.ID, appID, volume.MountPath, readOnly)
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

func (c *SandboxController) ensureDisk(ctx context.Context, diskName string, sizeGB int64, filesystem string) (entity.Id, error) {
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
	}

	name := idgen.GenNS("disk")

	putResp, err := c.EAC.Create(ctx, entity.New(
		entity.DBId, "disk/"+name,
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

func (c *SandboxController) findOrCreateDiskLease(ctx context.Context, diskID entity.Id, nodeID entity.Id, sandboxID entity.Id, appID entity.Id, mountPath string, readOnly bool) (entity.Id, error) {
	// Check if there's already a lease for this disk on this node
	listResp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, storage.KindDiskLease))
	if err != nil {
		return entity.Id(""), fmt.Errorf("failed to list disk leases: %w", err)
	}

	for _, e := range listResp.Values() {
		var lease storage.DiskLease
		lease.Decode(e.Entity())

		// Check if this lease is for our disk and node
		if lease.DiskId == diskID && lease.NodeId == nodeID {
			c.Log.Info("found existing disk lease",
				"lease", lease.ID,
				"disk", diskID,
				"node", nodeID,
				"status", lease.Status)
			return lease.ID, nil
		}
	}

	// No existing lease found, create a new one
	return c.createDiskLease(ctx, diskID, sandboxID, appID, mountPath, readOnly)
}

func (c *SandboxController) createDiskLease(ctx context.Context, diskID entity.Id, sandboxID entity.Id, appID entity.Id, mountPath string, readOnly bool) (entity.Id, error) {
	c.Log.Info("creating disk lease",
		"disk", diskID,
		"sandbox", sandboxID,
		"app", appID,
		"mount_path", mountPath,
		"node_id", c.NodeId)

	nodeID := entity.Id("node/" + c.NodeId)

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

	putResp, err := c.EAC.Create(ctx, entity.New(
		entity.DBId, "disk-lease/"+name,
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
				// Lease is bound, get disk to find LSVD volume ID
				diskResp, err := c.EAC.Get(ctx, lease.DiskId.String())
				if err != nil {
					return "", fmt.Errorf("failed to get disk entity: %w", err)
				}

				var disk storage.Disk
				disk.Decode(diskResp.Entity().Entity())

				if disk.LsvdVolumeId == "" {
					return "", fmt.Errorf("disk has no LSVD volume ID")
				}

				// Disk is mounted at /var/lib/miren/disks/{lsvd_volume_id}
				diskPath := filepath.Join("/var/lib/miren/disks", disk.LsvdVolumeId)
				c.Log.Info("disk lease bound successfully",
					"lease", leaseID,
					"disk", disk.ID,
					"lsvd_volume_id", disk.LsvdVolumeId,
					"disk_path", diskPath)
				return diskPath, nil

			case storage.FAILED:
				return "", fmt.Errorf("disk lease failed: %s", lease.ErrorMessage)

			case storage.PENDING:
				// Still pending, continue waiting
				continue

			default:
				return "", fmt.Errorf("unexpected disk lease status: %s", lease.Status)
			}
		}
	}
}
