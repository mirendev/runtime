package diskio

import (
	"context"
	"os"
)

// DiskVolumeOps abstracts OS operations for disk volume management.
// This interface enables testing without requiring actual filesystem operations.
type DiskVolumeOps interface {
	CreateVolumeDir(path string) error
	RemoveVolumeDir(path string) error
	MoveVolumeDir(src, dst string) error
	VolumePathExists(path string) bool
	CreateDiskImage(path string, sizeBytes int64) error
	RemoveDiskImage(path string) error
}

// ActiveMount describes a mount found on the running system.
type ActiveMount struct {
	Device    string
	MountPath string
}

// LoopBacking describes what a loop device is attached to.
//
// Path and Deleted are reported separately because the two questions callers
// ask are genuinely different. "Is anything holding a file under my volumes
// directory?" wants both, so a stale device can be reclaimed. "Is this image
// already attached, so I should mount that device instead of attaching a second
// one?" wants only live attachments — a loop holding an unlinked inode is not
// this image, it is the file that used to be at this path, and mounting it
// serves data the operator replaced.
type LoopBacking struct {
	// Path is the backing file as the kernel reports it.
	Path string
	// Deleted reports that the backing inode has been unlinked. The file lives
	// on because the loop device holds a reference, but nothing can reach it by
	// name any more: something renamed or removed the path out from under it.
	Deleted bool
}

// DiskMountOps abstracts OS operations for disk mount management.
// This interface enables testing without requiring actual loop device or mount operations.
type DiskMountOps interface {
	CreateDir(path string, perm os.FileMode) error
	RemoveFile(path string) error
	LoopAttach(imagePath string) (devicePath string, err error)
	LoopDetach(devicePath string) error
	// FindLoopByBacking returns the loop device path (e.g. /dev/loop3) currently
	// backing the given image file, or "" if no loop device is attached to it.
	// Used to detect stale/double attachments of the same disk image.
	FindLoopByBacking(imagePath string) (devicePath string, err error)
	// FindAllLoopBackings returns a map of loop device path to the backing
	// file currently attached to it, for every loop device in the kernel.
	// Used by boot-time orphan reconciliation to find stale attachments.
	FindAllLoopBackings() (map[string]LoopBacking, error)
	LbdAttach(ctx context.Context, imagePath, logDir string) (devicePath string, err error)
	LbdDetach(ctx context.Context, devicePath string) error
	LbdAvailable() bool
	Mount(device, mountPath, filesystem string, readOnly bool) error
	Unmount(path string) error
	IsMounted(path string) bool
	// IsDeviceMounted reports whether device is currently mounted at any
	// path in the kernel mount table. Used as a safety check before
	// running fsck, which must never run against a live filesystem.
	// Returns an error if the mount table cannot be read — callers must
	// treat that as "unknown" and refuse the destructive operation they
	// were gating on this check.
	IsDeviceMounted(device string) (bool, error)
	IsFormatted(ctx context.Context, device, filesystem string) (bool, error)
	FormatDevice(ctx context.Context, device, filesystem string) error
	// Fsck runs a filesystem check-and-repair on device. The device must
	// not be mounted anywhere when this is called. Used to recover from
	// EUCLEAN ("Structure needs cleaning") mount failures after an
	// unclean shutdown.
	Fsck(ctx context.Context, device, filesystem string) error

	// FindMounts returns all mounts whose mount path starts with the given prefix.
	FindMounts(pathPrefix string) []ActiveMount
}
