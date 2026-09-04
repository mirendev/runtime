package snapshot

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

const (
	StatusDeleting = "DELETING"

	LeaseStatusBound = "BOUND"
)

// DiskNotFoundError reports that no disk answers to a name.
//
// It is a distinct type because "there is no such disk" and "I could not find
// out" call for opposite responses. Callers act on the first by creating the
// disk, or by reporting that there is nothing to recover. Acting on the second
// the same way turns a moment of entity-store trouble into a duplicate disk.
type DiskNotFoundError struct {
	Name string
}

func (e DiskNotFoundError) Error() string {
	return fmt.Sprintf("disk %q not found", e.Name)
}

// DiskState holds the state of a disk entity as returned by a DiskResolver.
type DiskState struct {
	ID         string
	Name       string
	Status     string
	Filesystem string
}

// VolumeState holds the state of a disk volume entity.
type VolumeState struct {
	// VolumeID is the node-local identifier. It names the volume's directory,
	// so it is deliberately separate from the cloud's id for the same volume.
	VolumeID string
	// CloudVolumeID identifies this volume in miren.cloud, empty until the disk
	// controller has registered it there.
	CloudVolumeID string
	ImagePath     string
}

// LeaseState holds the state of a disk lease entity.
type LeaseState struct {
	ID     string
	Status string
}

// DiskResolver resolves disk-related entities from the entity store.
type DiskResolver interface {
	FindDisk(ctx context.Context, name string) (*DiskState, error)
	FindVolume(ctx context.Context, diskID string) (*VolumeState, error)
	FindLeases(ctx context.Context, diskID string) ([]LeaseState, error)
}

// DiskCreator can create disk and volume entities for restore.
type DiskCreator interface {
	CreateDiskAndVolume(ctx context.Context, name string, sizeBytes int64, filesystem string, dataPath string) (*RestoreTarget, error)
}

// BackupTarget contains resolved and validated information needed to
// perform a disk backup.
type BackupTarget struct {
	Name       string
	Filesystem string
	ImagePath  string
	// VolumeID is the node-local volume identifier.
	VolumeID string
	// CloudVolumeID identifies the volume in miren.cloud, for backups sent
	// there. Empty means the disk has not been registered with a cloud, so
	// there is nowhere to upload to.
	CloudVolumeID string
}

// RestoreTarget contains resolved and validated information needed to
// perform a disk restore.
type RestoreTarget struct {
	Name      string
	ImagePath string
	Created   bool                            // true if the disk was freshly created (no --force needed)
	Finalize  func(ctx context.Context) error // called after image is written to complete entity setup
	Cleanup   func(ctx context.Context) error // called on failure to remove entities created during restore
}

// PrepareBackup resolves disk entities and validates the disk is in a
// state suitable for backup.
func PrepareBackup(ctx context.Context, resolver DiskResolver, name string, dataPath string) (*BackupTarget, error) {
	disk, err := resolver.FindDisk(ctx, name)
	if err != nil {
		return nil, err
	}

	if disk.Status == StatusDeleting {
		return nil, fmt.Errorf("disk %q is being deleted, cannot backup", name)
	}

	vol, err := resolver.FindVolume(ctx, disk.ID)
	if err != nil {
		return nil, err
	}

	return &BackupTarget{
		Name:       name,
		Filesystem: disk.Filesystem,
		ImagePath:  resolveImagePath(vol, dataPath),

		VolumeID:      vol.VolumeID,
		CloudVolumeID: vol.CloudVolumeID,
	}, nil
}

// PrepareRestore resolves disk entities and validates the disk is in a
// state suitable for restore (not deleting, no bound leases).
// If the disk doesn't exist and creator is non-nil, it creates the disk
// and volume entities. If creator is nil and the disk doesn't exist,
// it returns an error.
func PrepareRestore(ctx context.Context, resolver DiskResolver, name string, dataPath string, opts ...RestoreOption) (*RestoreTarget, error) {
	var cfg restoreConfig
	for _, o := range opts {
		o(&cfg)
	}

	disk, err := resolver.FindDisk(ctx, name)
	if err != nil {
		var notFound DiskNotFoundError
		if cfg.creator == nil || !errors.As(err, &notFound) {
			// Only an actual absence means "create it". A lookup that failed
			// says nothing about whether the disk exists, and creating one on
			// that basis is how a brief entity-store outage turns into two
			// disks answering to the same name.
			return nil, err
		}
		return cfg.creator.CreateDiskAndVolume(ctx, name, cfg.sizeBytes, cfg.filesystem, dataPath)
	}

	if disk.Status == StatusDeleting {
		return nil, fmt.Errorf("disk %q is being deleted, cannot restore", name)
	}

	leases, err := resolver.FindLeases(ctx, disk.ID)
	if err != nil {
		return nil, err
	}

	for _, lease := range leases {
		if lease.Status == LeaseStatusBound {
			return nil, fmt.Errorf("disk %q has an active lease (ID: %s), cannot restore while mounted", name, lease.ID)
		}
	}

	vol, err := resolver.FindVolume(ctx, disk.ID)
	if err != nil {
		return nil, err
	}

	return &RestoreTarget{
		Name:      name,
		ImagePath: resolveImagePath(vol, dataPath),
	}, nil
}

type restoreConfig struct {
	creator    DiskCreator
	sizeBytes  int64
	filesystem string
}

// RestoreOption configures PrepareRestore behavior.
type RestoreOption func(*restoreConfig)

// WithCreator enables auto-creation of disk entities if they don't exist.
func WithCreator(c DiskCreator, sizeBytes int64, filesystem string) RestoreOption {
	return func(cfg *restoreConfig) {
		cfg.creator = c
		cfg.sizeBytes = sizeBytes
		cfg.filesystem = filesystem
	}
}

func resolveImagePath(vol *VolumeState, dataPath string) string {
	if vol.ImagePath != "" {
		return vol.ImagePath
	}
	return filepath.Join(dataPath, "disk-data", "volumes", vol.VolumeID, "disk.img")
}
